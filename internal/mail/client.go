// Package mail 实现 iCloud 邮件 IMAP 读取客户端。
//
// 通过 Apple 应用专用密码连接 imap.mail.me.com:993,
// 拉取隐私邮箱别名收到的邮件。对应原 Python 项目 icloud_mail.py。
package mail

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/charset"
	"golang.org/x/net/html"
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

// MessagePage is one stable page of inbox messages. NextBeforeUID is zero
// when there are no older messages matching the current filter.
type MessagePage struct {
	Messages      []Message
	NextBeforeUID uint32
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
	selected    string
	selectedRO  bool
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
	c.resetSelectedMailbox()
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
		c.resetSelectedMailbox()
		return
	}
	cli := c.cli
	c.cli = nil
	c.resetSelectedMailbox()
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
		c.resetSelectedMailbox()
		return 0, err
	}
	c.selected = "INBOX"
	c.selectedRO = false
	return int(mbox.Messages), nil
}

// ListInbox 拉取收件箱最近 limit 封邮件摘要。
//
// days 用于过滤只看近 N 天的邮件(0 表示不限制)。
// 返回按时间倒序排列。
func (c *Client) ListInbox(limit int, days int) ([]Message, error) {
	page, err := c.ListInboxPage(limit, days, 0)
	return page.Messages, err
}

// ListInboxSummaries 仅拉取邮件头，不读取正文。用于低延迟展示收件箱列表。
func (c *Client) ListInboxSummaries(limit int, days int) ([]Message, error) {
	page, err := c.ListInboxSummariesPage(limit, days, 0)
	return page.Messages, err
}

// ListInboxPage 拉取由 beforeUID 划定的一页收件箱邮件。
func (c *Client) ListInboxPage(limit int, days int, beforeUID uint32) (MessagePage, error) {
	return c.listInboxPage(limit, days, beforeUID, true)
}

// ListInboxSummariesPage 仅拉取一页邮件头，不读取正文。
func (c *Client) ListInboxSummariesPage(limit int, days int, beforeUID uint32) (MessagePage, error) {
	return c.listInboxPage(limit, days, beforeUID, false)
}

func (c *Client) listInboxPage(limit int, days int, beforeUID uint32, includePreview bool) (MessagePage, error) {
	if c.cli == nil {
		return MessagePage{}, fmt.Errorf("未连接")
	}
	if limit <= 0 {
		limit = 50
	}

	if err := c.ensureSelectedMailbox("INBOX", true); err != nil {
		return MessagePage{}, err
	}
	criteria := imap.NewSearchCriteria()
	if days > 0 {
		criteria.Since = c.currentTime().AddDate(0, 0, -days)
	}
	uids, err := c.cli.UidSearch(criteria)
	if err != nil {
		c.resetSelectedMailbox()
		return MessagePage{}, err
	}
	return c.fetchUIDPage(uids, limit, beforeUID, includePreview)
}

// FindByRecipient 查找发给指定隐私邮箱别名的邮件。
//
// 先尝试 IMAP TO 搜索;失败则拉取收件箱后本地过滤。
func (c *Client) FindByRecipient(recipient string, limit int, days int) ([]Message, error) {
	page, err := c.FindByRecipientPage(recipient, limit, days, 0)
	return page.Messages, err
}

// FindByRecipientSummaries 按收件人筛选邮件，但仅拉取邮件头。
func (c *Client) FindByRecipientSummaries(recipient string, limit int, days int) ([]Message, error) {
	page, err := c.FindByRecipientSummariesPage(recipient, limit, days, 0)
	return page.Messages, err
}

// FindByRecipientPage 查找由 beforeUID 划定的一页指定收件人邮件。
func (c *Client) FindByRecipientPage(recipient string, limit int, days int, beforeUID uint32) (MessagePage, error) {
	return c.findByRecipientPage(recipient, limit, days, beforeUID, true)
}

// FindByRecipientSummariesPage 查找一页指定收件人邮件头。
func (c *Client) FindByRecipientSummariesPage(
	recipient string,
	limit int,
	days int,
	beforeUID uint32,
) (MessagePage, error) {
	return c.findByRecipientPage(recipient, limit, days, beforeUID, false)
}

func (c *Client) findByRecipientPage(
	recipient string,
	limit int,
	days int,
	beforeUID uint32,
	includePreview bool,
) (MessagePage, error) {
	if c.cli == nil {
		return MessagePage{}, fmt.Errorf("未连接")
	}
	if limit <= 0 {
		limit = 20
	}

	// 先尝试服务端 TO 搜索
	if err := c.ensureSelectedMailbox("INBOX", true); err != nil {
		return MessagePage{}, err
	}
	criteria := imap.NewSearchCriteria()
	criteria.Header.Add("To", recipient)
	if days > 0 {
		criteria.Since = c.currentTime().AddDate(0, 0, -days)
	}
	uids, err := c.cli.UidSearch(criteria)
	if err == nil && len(uids) > 0 {
		return c.fetchUIDPage(uids, limit, beforeUID, includePreview)
	}
	if err != nil {
		c.resetSelectedMailbox()
	}

	return c.findByRecipientFallbackPage(recipient, limit, days, beforeUID, includePreview)
}

// findByRecipientFallbackPage scans headers in bounded chunks when the IMAP
// server does not return TO search results. The cursor remains a mailbox UID,
// so nonmatching messages never create duplicates or gaps between pages.
func (c *Client) findByRecipientFallbackPage(
	recipient string,
	limit int,
	days int,
	beforeUID uint32,
	includePreview bool,
) (MessagePage, error) {
	if err := c.ensureSelectedMailbox("INBOX", true); err != nil {
		return MessagePage{}, err
	}
	criteria := imap.NewSearchCriteria()
	if days > 0 {
		criteria.Since = c.currentTime().AddDate(0, 0, -days)
	}
	uids, err := c.cli.UidSearch(criteria)
	if err != nil {
		c.resetSelectedMailbox()
		return MessagePage{}, err
	}

	candidates := eligibleInboxUIDs(uids, beforeUID)
	if len(candidates) == 0 {
		return MessagePage{Messages: []Message{}}, nil
	}
	scanLimit := limit * 3
	if scanLimit < 50 {
		scanLimit = 50
	}
	recipient = strings.ToLower(recipient)
	matchedUIDs := make([]uint32, 0, limit+1)
	matchedHeaders := make(map[uint32]Message, limit+1)
	for end := len(candidates); end > 0 && len(matchedUIDs) <= limit; {
		start := end - scanLimit
		if start < 0 {
			start = 0
		}
		headers, fetchErr := c.fetchSelectedUIDs(candidates[start:end], false)
		if fetchErr != nil {
			return MessagePage{}, fetchErr
		}
		headersByUID := make(map[uint32]Message, len(headers))
		for _, header := range headers {
			uid, parseErr := strconv.ParseUint(header.ID, 10, 32)
			if parseErr == nil && uid > 0 {
				headersByUID[uint32(uid)] = header
			}
		}
		for index := end - 1; index >= start && len(matchedUIDs) <= limit; index-- {
			uid := candidates[index]
			header, found := headersByUID[uid]
			if found && strings.Contains(strings.ToLower(header.To), recipient) {
				matchedUIDs = append(matchedUIDs, uid)
				matchedHeaders[uid] = header
			}
		}
		end = start
	}

	hasMore := len(matchedUIDs) > limit
	if hasMore {
		matchedUIDs = matchedUIDs[:limit]
	}
	messages := make([]Message, 0, len(matchedUIDs))
	if includePreview {
		messages, err = c.fetchSelectedUIDs(matchedUIDs, true)
		if err != nil {
			return MessagePage{}, err
		}
	} else {
		for _, uid := range matchedUIDs {
			messages = append(messages, matchedHeaders[uid])
		}
		sortMessagesNewestFirst(messages)
	}
	page := MessagePage{Messages: messages}
	if hasMore && len(matchedUIDs) > 0 {
		page.NextBeforeUID = smallestUID(matchedUIDs)
	}
	return page, nil
}

func (c *Client) fetchByUIDs(uids []uint32, limit int, includePreview bool) ([]Message, error) {
	if limit <= 0 {
		limit = 20
	}
	uids = mostRecentUIDs(uids, limit)
	return c.fetchSelectedUIDs(uids, includePreview)
}

func (c *Client) fetchUIDPage(uids []uint32, limit int, beforeUID uint32, includePreview bool) (MessagePage, error) {
	pageUIDs, nextBeforeUID := inboxPageUIDs(uids, limit, beforeUID)
	if len(pageUIDs) == 0 {
		return MessagePage{Messages: []Message{}}, nil
	}
	messages, err := c.fetchSelectedUIDs(pageUIDs, includePreview)
	if err != nil {
		return MessagePage{}, err
	}
	return MessagePage{Messages: messages, NextBeforeUID: nextBeforeUID}, nil
}

func (c *Client) fetchSelectedUIDs(uids []uint32, includePreview bool) ([]Message, error) {
	if len(uids) == 0 {
		return []Message{}, nil
	}
	seqset := new(imap.SeqSet)
	for _, uid := range uids {
		seqset.AddNum(uid)
	}

	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate}
	var section *imap.BodySectionName
	if includePreview {
		section = previewBodySection()
		items = append(items, section.FetchItem())
	}
	messages := make(chan *imap.Message, len(uids))
	done := make(chan error, 1)
	go func() {
		done <- c.cli.UidFetch(seqset, items, messages)
	}()

	var out []Message
	for msg := range messages {
		if includePreview {
			out = append(out, toMessageWithBody(msg, section))
		} else {
			out = append(out, toMessage(msg))
		}
	}
	if err := <-done; err != nil {
		return nil, err
	}
	sortMessagesNewestFirst(out)
	return out, nil
}

// GetPreview 获取单封邮件的安全文本预览，正文读取量受 maxPreviewFetchBytes 限制。
func (c *Client) GetPreview(uid uint32) (*Message, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	if uid == 0 {
		return nil, fmt.Errorf("邮件 UID 无效")
	}
	if err := c.ensureSelectedMailbox("INBOX", true); err != nil {
		return nil, err
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uid)
	section := previewBodySection()
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate, section.FetchItem()}
	messages := make(chan *imap.Message, 1)
	done := make(chan error, 1)
	go func() {
		done <- c.cli.UidFetch(seqset, items, messages)
	}()

	var fetched *imap.Message
	for message := range messages {
		fetched = message
	}
	if err := <-done; err != nil {
		c.resetSelectedMailbox()
		return nil, err
	}
	if fetched == nil {
		return nil, fmt.Errorf("邮件不存在 (uid=%d)", uid)
	}
	preview := toMessageWithBody(fetched, section)
	return &preview, nil
}

func (c *Client) ensureSelectedMailbox(name string, readOnly bool) error {
	if c.cli == nil {
		return fmt.Errorf("未连接")
	}
	if c.selected == name && c.selectedRO == readOnly {
		return nil
	}
	if _, err := c.cli.Select(name, readOnly); err != nil {
		c.resetSelectedMailbox()
		return err
	}
	c.selected = name
	c.selectedRO = readOnly
	return nil
}

func (c *Client) resetSelectedMailbox() {
	c.selected = ""
	c.selectedRO = false
}

func mostRecentUIDs(uids []uint32, limit int) []uint32 {
	page, _ := inboxPageUIDs(uids, limit, 0)
	return page
}

func inboxPageUIDs(uids []uint32, limit int, beforeUID uint32) ([]uint32, uint32) {
	if limit <= 0 {
		limit = 20
	}
	ordered := eligibleInboxUIDs(uids, beforeUID)
	if len(ordered) <= limit {
		return ordered, 0
	}
	first := len(ordered) - limit
	return ordered[first:], ordered[first]
}

func eligibleInboxUIDs(uids []uint32, beforeUID uint32) []uint32 {
	ordered := append([]uint32(nil), uids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	filtered := make([]uint32, 0, len(ordered))
	var previous uint32
	for _, uid := range ordered {
		if uid == 0 || (beforeUID > 0 && uid >= beforeUID) || uid == previous {
			continue
		}
		filtered = append(filtered, uid)
		previous = uid
	}
	return filtered
}

func smallestUID(uids []uint32) uint32 {
	if len(uids) == 0 {
		return 0
	}
	smallest := uids[0]
	for _, uid := range uids[1:] {
		if uid < smallest {
			smallest = uid
		}
	}
	return smallest
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
	if err := c.ensureSelectedMailbox("INBOX", true); err != nil {
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
		c.resetSelectedMailbox()
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

type mailHeader interface {
	Get(string) string
}

type mailBodyCandidates struct {
	html  string
	plain string
}

// readBody 读取邮件正文。multipart 邮件优先采用 text/plain，仅有 HTML 时提取可见文本。
func readBody(msg *mail.Message) (string, error) {
	candidates, err := readBodyCandidates(msg.Header, msg.Body)
	if plain := normalizePlainText(candidates.plain); plain != "" {
		return plain, nil
	}
	if candidates.html != "" {
		return stripHTML(candidates.html), nil
	}
	return "", err
}

func readBodyCandidates(header mailHeader, body io.Reader) (mailBodyCandidates, error) {
	mediaType, params := parseMailMediaType(header.Get("Content-Type"))
	var disposition string
	var dispositionParams map[string]string
	if rawDisposition := strings.TrimSpace(header.Get("Content-Disposition")); rawDisposition != "" {
		disposition, dispositionParams = parseMailMediaType(rawDisposition)
	}
	if disposition == "attachment" || dispositionParams["filename"] != "" {
		return mailBodyCandidates{}, nil
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return mailBodyCandidates{}, fmt.Errorf("multipart 邮件缺少 boundary")
		}
		reader := multipart.NewReader(body, boundary)
		var candidates mailBodyCandidates
		var firstErr error
		for {
			part, partErr := reader.NextRawPart()
			if errors.Is(partErr, io.EOF) {
				break
			}
			if partErr != nil {
				if firstErr == nil {
					firstErr = partErr
				}
				break
			}

			partCandidates, candidateErr := readBodyCandidates(part.Header, part)
			_ = part.Close()
			if candidateErr != nil && firstErr == nil {
				firstErr = candidateErr
			}
			if candidates.plain == "" && strings.TrimSpace(partCandidates.plain) != "" {
				candidates.plain = partCandidates.plain
			}
			if candidates.html == "" && strings.TrimSpace(partCandidates.html) != "" {
				candidates.html = partCandidates.html
			}
		}
		return candidates, firstErr
	}

	if mediaType != "text/plain" && mediaType != "text/html" {
		return mailBodyCandidates{}, nil
	}
	decoded, err := decodeMailBody(header, params, body)
	if err != nil {
		return mailBodyCandidates{}, err
	}
	raw, readErr := io.ReadAll(decoded)
	if readErr != nil && len(raw) == 0 {
		return mailBodyCandidates{}, readErr
	}
	text := strings.ToValidUTF8(string(raw), "")
	if mediaType == "text/html" || looksLikeHTML(text) {
		return mailBodyCandidates{html: text}, nil
	}
	return mailBodyCandidates{plain: text}, nil
}

func parseMailMediaType(value string) (string, map[string]string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "text/plain", nil
	}
	mediaType, params, err := mime.ParseMediaType(value)
	if err == nil {
		return strings.ToLower(mediaType), params
	}
	mediaType = strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	return mediaType, nil
}

func decodeMailBody(header mailHeader, params map[string]string, body io.Reader) (io.Reader, error) {
	var decoded io.Reader = body
	switch strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding"))) {
	case "base64":
		decoded = base64.NewDecoder(base64.StdEncoding, decoded)
	case "quoted-printable":
		decoded = quotedprintable.NewReader(decoded)
	}

	charsetName := strings.TrimSpace(params["charset"])
	if charsetName == "" || strings.EqualFold(charsetName, "utf-8") || strings.EqualFold(charsetName, "us-ascii") {
		return decoded, nil
	}
	return charset.Reader(charsetName, decoded)
}

func looksLikeHTML(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "<!doctype html") ||
		strings.Contains(lower, "<html") ||
		strings.Contains(lower, "<body")
}

func normalizePlainText(text string) string {
	text = strings.ToValidUTF8(text, "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\u00a0", " ")
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" && (len(cleaned) == 0 || cleaned[len(cleaned)-1] == "") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	for len(cleaned) > 0 && cleaned[len(cleaned)-1] == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}
	return strings.Join(cleaned, "\n")
}

// stripHTML 提取浏览器中实际可见的文本，忽略样式、脚本和其他非正文节点。
func stripHTML(rawHTML string) string {
	document, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return ""
	}
	var text strings.Builder
	appendHTMLText(&text, document)
	return normalizeHTMLText(text.String())
}

func appendHTMLText(text *strings.Builder, node *html.Node) {
	if node.Type == html.ElementNode && isHiddenHTMLElement(node.Data) {
		return
	}
	if node.Type == html.TextNode {
		text.WriteString(node.Data)
		return
	}

	isBlock := node.Type == html.ElementNode && isBlockHTMLElement(node.Data)
	if isBlock {
		text.WriteByte('\n')
		if node.Data == "li" {
			text.WriteString("- ")
		}
	}
	if node.Type == html.ElementNode && (node.Data == "br" || node.Data == "hr") {
		text.WriteByte('\n')
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		appendHTMLText(text, child)
	}
	if node.Type == html.ElementNode && (node.Data == "td" || node.Data == "th") {
		text.WriteByte(' ')
	}
	if isBlock {
		text.WriteByte('\n')
	}
}

func isHiddenHTMLElement(name string) bool {
	switch name {
	case "canvas", "head", "noscript", "script", "style", "svg", "template":
		return true
	default:
		return false
	}
}

func isBlockHTMLElement(name string) bool {
	switch name {
	case "address", "article", "aside", "blockquote", "body", "dd", "div", "dl", "dt",
		"fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4",
		"h5", "h6", "header", "html", "li", "main", "nav", "ol", "p", "pre",
		"section", "table", "tr", "ul":
		return true
	default:
		return false
	}
}

func normalizeHTMLText(text string) string {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	lines := strings.Split(strings.ReplaceAll(text, "\r", "\n"), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" && (len(cleaned) == 0 || cleaned[len(cleaned)-1] == "") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	for len(cleaned) > 0 && cleaned[len(cleaned)-1] == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}
	return strings.Join(cleaned, "\n")
}
