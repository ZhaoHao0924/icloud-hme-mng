// Package mail 实现 iCloud 邮件 IMAP 读取客户端。
//
// 通过 Apple 应用专用密码连接 imap.mail.me.com:993,
// 拉取隐私邮箱别名收到的邮件。对应原 Python 项目 icloud_mail.py。
package mail

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/charset"
)

const (
	IMAPServer           = "imap.mail.me.com"
	IMAPPort             = 993
	maxPreviewBytes      = 4 << 10
	maxPreviewFetchBytes = 64 << 10
)

type imapSession interface {
	Login(username, password string) error
	Logout() error
	Terminate() error
	Select(name string, readOnly bool) (*imap.MailboxStatus, error)
	UidSearch(criteria *imap.SearchCriteria) ([]uint32, error)
	UidFetch(seqset *imap.SeqSet, items []imap.FetchItem, ch chan *imap.Message) error
}

type imapDialer func(addr string) (imapSession, error)

func dialIMAPTLS(addr string) (imapSession, error) {
	return client.DialTLS(addr, nil)
}

// Message 是一封邮件的摘要信息。
type Message struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Preview string `json:"preview"`
}

// FullMessage 是一封邮件的完整内容(含正文)。
type FullMessage struct {
	Message
	Body        string `json:"body"`
	ContentType string `json:"content_type"`
}

// Client 是 iCloud 邮件 IMAP 客户端。
type Client struct {
	appleID     string
	appPassword string
	cli         imapSession
	dial        imapDialer
	now         func() time.Time
}

// NewClient 创建 IMAP 客户端。需在调用其它方法前先 Connect。
func NewClient(appleID, appPassword string) *Client {
	return &Client{
		appleID:     appleID,
		appPassword: appPassword,
		dial:        dialIMAPTLS,
		now:         time.Now,
	}
}

// Connect 连接并登录 IMAP 服务器。
func (c *Client) Connect() error {
	if c.cli != nil {
		return nil
	}
	addr := fmt.Sprintf("%s:%d", IMAPServer, IMAPPort)
	dial := c.dial
	if dial == nil {
		dial = dialIMAPTLS
	}
	cli, err := dial(addr)
	if err != nil {
		return fmt.Errorf("IMAP 连接失败: %w", err)
	}
	if err := cli.Login(c.appleID, c.appPassword); err != nil {
		loginErr := fmt.Errorf("IMAP 登录失败 — 请检查: 1) 应用专用密码是否正确 2) Apple ID: %s — %w", c.appleID, err)
		if closeErr := cli.Terminate(); closeErr != nil {
			return errors.Join(loginErr, fmt.Errorf("关闭失败的 IMAP 连接: %w", closeErr))
		}
		return loginErr
	}
	c.cli = cli
	return nil
}

// Disconnect 登出并关闭连接。
func (c *Client) Disconnect() {
	if c.cli == nil {
		return
	}
	cli := c.cli
	c.cli = nil
	if err := cli.Logout(); err != nil {
		_ = cli.Terminate()
	}
}

func (c *Client) currentTime() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// InboxCount 返回收件箱邮件总数。
func (c *Client) InboxCount() (int, error) {
	if c.cli == nil {
		return 0, fmt.Errorf("未连接")
	}
	mbox, err := c.cli.Select("INBOX", false)
	if err != nil {
		return 0, err
	}
	return int(mbox.Messages), nil
}

// ListInbox 拉取收件箱最近 limit 封邮件摘要。
//
// days 用于过滤只看近 N 天的邮件(0 表示不限制)。
// 返回按时间倒序排列。
func (c *Client) ListInbox(limit int, days int) ([]Message, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	if limit <= 0 {
		limit = 50
	}

	if _, err := c.cli.Select("INBOX", true); err != nil {
		return nil, err
	}
	criteria := imap.NewSearchCriteria()
	if days > 0 {
		criteria.Since = c.currentTime().AddDate(0, 0, -days)
	}
	uids, err := c.cli.UidSearch(criteria)
	if err != nil {
		return nil, err
	}
	return c.fetchByUIDs(uids, limit)
}

// FindByRecipient 查找发给指定隐私邮箱别名的邮件。
//
// 先尝试 IMAP TO 搜索;失败则拉取收件箱后本地过滤。
func (c *Client) FindByRecipient(recipient string, limit int, days int) ([]Message, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	if limit <= 0 {
		limit = 20
	}

	// 先尝试服务端 TO 搜索
	if _, err := c.cli.Select("INBOX", true); err != nil {
		return nil, err
	}
	criteria := imap.NewSearchCriteria()
	criteria.Header.Add("To", recipient)
	if days > 0 {
		criteria.Since = c.currentTime().AddDate(0, 0, -days)
	}
	uids, err := c.cli.UidSearch(criteria)
	if err == nil && len(uids) > 0 {
		return c.fetchByUIDs(uids, limit)
	}

	// fallback: 拉取收件箱后本地过滤
	all, err := c.ListInbox(limit*3, days)
	if err != nil {
		return nil, err
	}
	recipient = strings.ToLower(recipient)
	var out []Message
	for _, m := range all {
		if strings.Contains(strings.ToLower(m.To), recipient) {
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (c *Client) fetchByUIDs(uids []uint32, limit int) ([]Message, error) {
	if len(uids) == 0 {
		return []Message{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	uids = mostRecentUIDs(uids, limit)
	seqset := new(imap.SeqSet)
	for _, uid := range uids {
		seqset.AddNum(uid)
	}

	section := previewBodySection()
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate, section.FetchItem()}
	messages := make(chan *imap.Message, len(uids))
	done := make(chan error, 1)
	go func() {
		done <- c.cli.UidFetch(seqset, items, messages)
	}()

	var out []Message
	for msg := range messages {
		out = append(out, toMessageWithBody(msg, section))
	}
	if err := <-done; err != nil {
		return nil, err
	}
	sortMessagesNewestFirst(out)
	return out, nil
}

func mostRecentUIDs(uids []uint32, limit int) []uint32 {
	ordered := append([]uint32(nil), uids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	if limit > 0 && len(ordered) > limit {
		ordered = ordered[len(ordered)-limit:]
	}
	return ordered
}

func previewBodySection() *imap.BodySectionName {
	return &imap.BodySectionName{
		Peek:    true,
		Partial: []int{0, maxPreviewFetchBytes},
	}
}

func sortMessagesNewestFirst(messages []Message) {
	sort.SliceStable(messages, func(i, j int) bool {
		leftTime, leftHasTime := parseMessageDate(messages[i].Date)
		rightTime, rightHasTime := parseMessageDate(messages[j].Date)
		if leftHasTime != rightHasTime {
			return leftHasTime
		}
		if leftHasTime && !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}

		leftUID, leftErr := strconv.ParseUint(messages[i].ID, 10, 32)
		rightUID, rightErr := strconv.ParseUint(messages[j].ID, 10, 32)
		if (leftErr == nil) != (rightErr == nil) {
			return leftErr == nil
		}
		if leftErr == nil && leftUID != rightUID {
			return leftUID > rightUID
		}
		return messages[i].ID > messages[j].ID
	})
}

func parseMessageDate(raw string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, raw)
	return t, err == nil
}

// GetFull 获取单封邮件的完整内容(含正文)。
func (c *Client) GetFull(uid uint32) (*FullMessage, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	if _, err := c.cli.Select("INBOX", true); err != nil {
		return nil, err
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uid)

	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate, imap.FetchRFC822}
	messages := make(chan *imap.Message, 1)
	done := make(chan error, 1)
	go func() {
		done <- c.cli.UidFetch(seqset, items, messages)
	}()

	msg := <-messages
	if err := <-done; err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, fmt.Errorf("邮件不存在 (uid=%d)", uid)
	}

	full := &FullMessage{Message: toMessage(msg)}
	// 解析正文
	if r := msg.GetBody(&imap.BodySectionName{}); r != nil {
		if em, err := mail.ReadMessage(r); err == nil {
			body, _ := readBody(em)
			full.Body = body
			full.ContentType = em.Header.Get("Content-Type")
		}
	}
	return full, nil
}

// ---- 解析工具 ----

func toMessage(msg *imap.Message) Message {
	m := Message{}
	if msg.Uid > 0 {
		m.ID = fmt.Sprintf("%d", msg.Uid)
	}
	if msg.Envelope != nil {
		if len(msg.Envelope.From) > 0 {
			m.From = msg.Envelope.From[0].Address()
		}
		if len(msg.Envelope.To) > 0 {
			addrs := make([]string, 0, len(msg.Envelope.To))
			for _, a := range msg.Envelope.To {
				addrs = append(addrs, a.Address())
			}
			m.To = strings.Join(addrs, ", ")
		}
		m.Subject = decodeHeader(msg.Envelope.Subject)
		if !msg.Envelope.Date.IsZero() {
			m.Date = msg.Envelope.Date.Format(time.RFC3339)
		}
	}
	if m.Date == "" && !msg.InternalDate.IsZero() {
		m.Date = msg.InternalDate.Format(time.RFC3339)
	}
	if m.From != "" {
		m.From = decodeHeader(m.From)
	}
	if m.To != "" {
		m.To = decodeHeader(m.To)
	}
	return m
}

// toMessageWithBody 在 toMessage 基础上解析正文填充 Preview(供 OTP 提取)。
func toMessageWithBody(msg *imap.Message, section *imap.BodySectionName) Message {
	m := toMessage(msg)
	if r := msg.GetBody(section); r != nil {
		if em, err := mail.ReadMessage(io.LimitReader(r, maxPreviewFetchBytes)); err == nil {
			if body, err := readBody(em); err == nil {
				m.Preview = truncatePreview(body)
			}
		}
	}
	return m
}

func truncatePreview(body string) string {
	preview := strings.ToValidUTF8(strings.TrimSpace(body), "")
	if len(preview) <= maxPreviewBytes {
		return preview
	}
	end := maxPreviewBytes
	for end > 0 && !utf8.RuneStart(preview[end]) {
		end--
	}
	return preview[:end]
}

// decodeHeader 解码 RFC 2047 编码的邮件头(如 =?UTF-8?B?xxx?=)。
func decodeHeader(s string) string {
	if s == "" {
		return ""
	}
	dec := mime.WordDecoder{CharsetReader: charset.Reader}
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

var htmlTag = regexp.MustCompile(`<[^>]+>`)

// readBody 读取邮件正文,优先 text/plain,其次从 HTML 提取纯文本。
func readBody(msg *mail.Message) (string, error) {
	ct := msg.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/html") {
		raw, _ := io.ReadAll(msg.Body)
		// quoted-printable 解码
		if strings.Contains(msg.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
			r := quotedprintable.NewReader(strings.NewReader(string(raw)))
			raw, _ = io.ReadAll(r)
		}
		return stripHTML(string(raw)), nil
	}
	// 默认当 text/plain
	raw, err := io.ReadAll(msg.Body)
	if err != nil {
		return "", err
	}
	if strings.Contains(msg.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
		r := quotedprintable.NewReader(strings.NewReader(string(raw)))
		raw, _ = io.ReadAll(r)
	}
	return string(raw), nil
}

// stripHTML 粗略剥离 HTML 标签,保留可读文本。
func stripHTML(html string) string {
	// 换行标签转换行
	html = strings.ReplaceAll(html, "<br>", "\n")
	html = strings.ReplaceAll(html, "<br/>", "\n")
	html = strings.ReplaceAll(html, "<br />", "\n")
	html = strings.ReplaceAll(html, "</p>", "\n")
	html = strings.ReplaceAll(html, "</div>", "\n")
	html = strings.ReplaceAll(html, "</tr>", "\n")
	html = strings.ReplaceAll(html, "<li>", "\n- ")
	// 去掉所有标签
	html = htmlTag.ReplaceAllString(html, "")
	// 反转义常见实体
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	// 压缩多余空白
	lines := strings.Split(html, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
