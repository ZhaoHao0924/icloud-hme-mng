// Package mail - iCloud Web 邮件客户端
//
// 使用 Cookie 认证通过 iCloud Web API 读取邮件，
// 无需 App Password。基于 mccgateway 服务。
package mail

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	netmail "net/mail"
	"net/url"
	"sort"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/google/uuid"
)

// WebClientBuildNumber 是与浏览器一致的 mccgateway 邮件接口构建号。
const WebClientBuildNumber = "2624Build13"

const sessionTrustChallengeStatus = 421

// ErrWebRecipientUnavailable 表示 Web 邮件响应没有足够信息进行可靠的别名筛选。
var ErrWebRecipientUnavailable = errors.New("Web 邮件响应缺少可验证的收件人")

type webHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
	SetCookies(u *url.URL, cookies []*http.Cookie)
}

// WebClient 是 iCloud Web 邮件客户端。
type WebClient struct {
	cookies       map[string]string
	dsid          string
	clientID      string
	mccGatewayURL string
	host          string // "icloud.com" 或 "icloud.com.cn"
	httpc         webHTTPClient
}

// NewWebClient 创建一个 Web 邮件客户端。
func NewWebClient(cookies map[string]string, dsid, host string) *WebClient {
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.Chrome_146),
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
		tls_client.WithNotFollowRedirects(),
	}

	httpc, _ := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	return newWebClient(cookies, dsid, host, httpc)
}

func newWebClient(cookies map[string]string, dsid, host string, httpc webHTTPClient) *WebClient {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		host = "icloud.com"
	}

	c := &WebClient{
		cookies:  cloneWebCookies(cookies),
		dsid:     dsid,
		clientID: uuid.New().String(),
		host:     host,
		httpc:    httpc,
	}

	if isSupportedICloudHost(host) {
		_ = c.setCookiesForURL("https://setup." + host)
		_ = c.setCookiesForURL("https://www." + host)
	}

	return c
}

func cloneWebCookies(cookies map[string]string) map[string]string {
	cloned := make(map[string]string, len(cookies))
	for name, value := range cookies {
		cloned[name] = value
	}
	return cloned
}

func isSupportedICloudHost(host string) bool {
	return host == "icloud.com" || host == "icloud.com.cn"
}

func (c *WebClient) setCookiesForURL(rawURL string) error {
	if len(c.cookies) == 0 {
		return nil
	}
	if c.httpc == nil {
		return fmt.Errorf("Web HTTP 客户端未初始化")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return fmt.Errorf("无效的 Cookie 目标 URL: %q", rawURL)
	}
	names := make([]string, 0, len(c.cookies))
	for name := range c.cookies {
		names = append(names, name)
	}
	sort.Strings(names)
	httpCookies := make([]*http.Cookie, 0, len(names))
	for _, name := range names {
		httpCookies = append(httpCookies, &http.Cookie{
			Name:  name,
			Value: c.cookies[name],
			Path:  "/",
		})
	}
	c.httpc.SetCookies(u, httpCookies)
	return nil
}

// origin 返回当前账号对应的 Web Origin。
func (c *WebClient) origin() string {
	return "https://www." + c.host
}

// setCommonHeaders 设置与浏览器一致的通用请求头。
func (c *WebClient) setCommonHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.origin())
	req.Header.Set("Referer", c.origin()+"/")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-site")
}

// withParams 给 URL 追加 clientBuildNumber / clientId / dsid 查询参数。
func (c *WebClient) withParams(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("clientBuildNumber", WebClientBuildNumber)
	query.Set("clientMasteringNumber", WebClientBuildNumber)
	query.Set("clientId", c.clientID)
	query.Set("dsid", c.dsid)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func isSessionTrustChallenge(status int, body []byte) bool {
	if status != sessionTrustChallengeStatus {
		return false
	}
	var payload struct {
		TrustTokens json.RawMessage `json:"trustTokens"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	tokens := bytes.TrimSpace(payload.TrustTokens)
	return len(tokens) > 0 && !bytes.Equal(tokens, []byte("null"))
}

func webUpstreamError(operation string, status int, body []byte) error {
	if isSessionTrustChallenge(status, body) {
		return fmt.Errorf("iCloud session trust is no longer valid (HTTP %d)", status)
	}
	// Apple can include session and trust tokens in failure payloads.
	return fmt.Errorf("%s: HTTP %d", operation, status)
}

// resolveMccGateway 从 validate 响应中获取 mccgateway URL。
func (c *WebClient) resolveMccGateway() error {
	if c.mccGatewayURL != "" {
		return nil
	}
	if !isSupportedICloudHost(c.host) {
		return fmt.Errorf("不支持的 iCloud host: %s", c.host)
	}
	if c.httpc == nil {
		return fmt.Errorf("Web HTTP 客户端未初始化")
	}

	setupURL := "https://setup." + c.host + "/setup/ws/1/validate"
	setupURL, err := c.withParams(setupURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", setupURL, nil)
	if err != nil {
		return err
	}
	c.setCommonHeaders(req)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取 validate 响应失败: %w", err)
	}
	if resp.StatusCode != 200 {
		return webUpstreamError("validate failed", resp.StatusCode, body)
	}

	var parsed struct {
		Webservices struct {
			Mccgateway struct {
				URL string `json:"url"`
			} `json:"mccgateway"`
		} `json:"webservices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("解析 validate 响应失败: %w", err)
	}

	rawMccURL := parsed.Webservices.Mccgateway.URL
	if rawMccURL == "" {
		return fmt.Errorf("未找到 mccgateway URL,响应: %s", truncate(string(body), 200))
	}
	mccURL, err := normalizeMccGatewayURL(rawMccURL, c.host)
	if err != nil {
		return err
	}
	if err := c.setCookiesForURL(mccURL); err != nil {
		return fmt.Errorf("设置 mccgateway Cookie: %w", err)
	}
	c.mccGatewayURL = mccURL
	return nil
}

func normalizeMccGatewayURL(rawURL, accountHost string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("解析 mccgateway URL 失败: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil {
		return "", fmt.Errorf("mccgateway URL 必须使用 HTTPS 且不能包含用户信息")
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	baseHost := strings.ToLower(strings.TrimSuffix(accountHost, "."))
	if !isAllowedMccGatewayHost(hostname, baseHost) {
		return "", fmt.Errorf("validate 返回了不受信任的 mccgateway host: %s", hostname)
	}
	parsed.Scheme = "https"
	parsed.Host = hostname
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isAllowedMccGatewayHost(hostname, accountHost string) bool {
	if !isSupportedICloudHost(accountHost) {
		return false
	}
	suffix := "." + accountHost
	if !strings.HasSuffix(hostname, suffix) {
		return false
	}
	service := strings.TrimSuffix(hostname, suffix)
	return service == "mccgateway" ||
		(!strings.Contains(service, ".") && strings.HasSuffix(service, "-mccgateway"))
}

type threadSearchResp struct {
	Success    *bool           `json:"success"`
	ThreadList json.RawMessage `json:"threadList"`
}

type webThread struct {
	ThreadID       string
	Subject        string
	Senders        []string
	Preview        string
	Timestamp      int64
	Recipients     []string
	RecipientKnown bool
}

func (t *webThread) UnmarshalJSON(data []byte) error {
	var wire struct {
		ThreadID  string          `json:"threadId"`
		ID        string          `json:"id"`
		GUID      string          `json:"guid"`
		Subject   string          `json:"subject"`
		Senders   json.RawMessage `json:"senders"`
		Preview   string          `json:"preview"`
		Timestamp int64           `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	t.ThreadID = firstNonEmptyString(wire.ThreadID, wire.ID, wire.GUID)
	t.Subject = wire.Subject
	t.Senders = decodeWebSenders(wire.Senders)
	t.Preview = wire.Preview
	t.Timestamp = wire.Timestamp
	t.Recipients = extractWebRecipients(data)
	t.RecipientKnown = len(t.Recipients) > 0
	return nil
}

type webSearchResult struct {
	message        Message
	recipients     []string
	recipientKnown bool
	timestamp      int64
}

// search 执行 thread/search 请求,返回带过滤元数据的稳定邮件摘要。
func (c *WebClient) search(payload []byte) ([]webSearchResult, error) {
	if err := c.resolveMccGateway(); err != nil {
		return nil, err
	}

	searchURL, err := c.withParams(c.mccGatewayURL + "/mailws2/v1/thread/search")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", searchURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	c.setCommonHeaders(req)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取邮件响应失败: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, webUpstreamError("read inbox failed", resp.StatusCode, body)
	}

	var result threadSearchResp
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析邮件响应失败: %w", err)
	}
	if result.Success != nil && !*result.Success {
		return nil, fmt.Errorf("获取邮件失败: %s", truncate(string(body), 300))
	}
	if len(result.ThreadList) == 0 || bytes.Equal(bytes.TrimSpace(result.ThreadList), []byte("null")) {
		return nil, fmt.Errorf("解析邮件响应失败: 缺少 threadList 数组")
	}
	var threads []webThread
	if err := json.Unmarshal(result.ThreadList, &threads); err != nil {
		return nil, fmt.Errorf("解析 threadList 失败: %w", err)
	}

	messages := make([]webSearchResult, 0, len(threads))
	for i, thread := range threads {
		if thread.ThreadID == "" {
			return nil, fmt.Errorf("解析邮件响应失败: threadList[%d] 缺少 threadId", i)
		}
		from := ""
		if len(thread.Senders) > 0 {
			from = thread.Senders[0]
		}
		date := ""
		if thread.Timestamp > 0 {
			date = time.UnixMilli(thread.Timestamp).UTC().Format(time.RFC3339)
		}
		messages = append(messages, webSearchResult{
			message: Message{
				ID:      thread.ThreadID,
				From:    from,
				To:      strings.Join(thread.Recipients, ", "),
				Subject: thread.Subject,
				Preview: truncatePreview(thread.Preview),
				Date:    date,
			},
			recipients:     append([]string(nil), thread.Recipients...),
			recipientKnown: thread.RecipientKnown,
			timestamp:      thread.Timestamp,
		})
	}
	sort.SliceStable(messages, func(i, j int) bool {
		if messages[i].timestamp != messages[j].timestamp {
			return messages[i].timestamp > messages[j].timestamp
		}
		return messages[i].message.ID > messages[j].message.ID
	})
	return messages, nil
}

// ListInbox 列出收件箱邮件。
func (c *WebClient) ListInbox(limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 20
	}
	results, err := c.searchInbox("", limit, true)
	if err != nil {
		return nil, err
	}
	return publicWebMessages(results, limit), nil
}

// SearchMails 搜索邮件。query 为空时等价于 ListInbox。
func (c *WebClient) SearchMails(query string, limit int) ([]Message, error) {
	if query == "" {
		return c.ListInbox(limit)
	}
	if limit <= 0 {
		limit = 20
	}
	results, err := c.searchInbox(query, limit, false)
	if err != nil {
		return nil, err
	}
	return publicWebMessages(results, limit), nil
}

func (c *WebClient) searchInbox(query string, limit int, includeFolderStatus bool) ([]webSearchResult, error) {
	payload := map[string]any{
		"responseType":        "THREAD_DIGEST",
		"includeFolderStatus": includeFolderStatus,
		"maxResults":          limit,
		"sessionHeaders": map[string]any{
			"folder":     "INBOX",
			"condstore":  1,
			"qresync":    1,
			"threadmode": 1,
		},
	}
	if includeFolderStatus {
		headers := payload["sessionHeaders"].(map[string]any)
		headers["modseq"] = nil
		headers["threadmodseq"] = nil
	}
	if query != "" {
		payload["query"] = query
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return c.search(raw)
}

func publicWebMessages(results []webSearchResult, limit int) []Message {
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	messages := make([]Message, len(results))
	for i := range results {
		messages[i] = results[i].message
	}
	return messages
}

// FindByAlias 仅使用响应中明确的收件人地址进行本地精确过滤。
func (c *WebClient) FindByAlias(alias string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 20
	}
	batchSize := limit * 2
	if batchSize < 50 {
		batchSize = 50
	}
	raw, err := c.searchInbox("", batchSize, true)
	if err != nil {
		return nil, err
	}
	if len(raw) > batchSize {
		raw = raw[:batchSize]
	}

	unknownRecipients := 0
	alias = canonicalWebAddress(alias)
	filtered := make([]Message, 0, limit)
	for _, result := range raw {
		if !result.recipientKnown {
			unknownRecipients++
			continue
		}
		if containsWebAddress(result.recipients, alias) && len(filtered) < limit {
			filtered = append(filtered, result.message)
		}
	}
	if unknownRecipients > 0 {
		return nil, fmt.Errorf("%w: thread/search 中 %d/%d 封邮件未提供收件人", ErrWebRecipientUnavailable, unknownRecipients, len(raw))
	}
	return filtered, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func extractWebRecipients(data []byte) []string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	var addresses []string
	walkWebRecipientFields(value, &addresses)
	return uniqueWebAddresses(addresses)
}

func walkWebRecipientFields(value any, addresses *[]string) {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			walkWebRecipientFields(item, addresses)
		}
	case map[string]any:
		for key, item := range value {
			if isWebRecipientKey(key) {
				collectWebAddressValues(item, addresses)
				continue
			}
			walkWebRecipientFields(item, addresses)
		}
	}
}

func isWebRecipientKey(key string) bool {
	switch normalizeWebJSONKey(key) {
	case "to", "tos", "toaddress", "toaddresses", "torecipient", "torecipients",
		"recipient", "recipients", "recipientaddress", "recipientaddresses",
		"cc", "ccrecipients", "bcc", "bccrecipients", "receiver", "receivers":
		return true
	default:
		return false
	}
}

func normalizeWebJSONKey(key string) string {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", "")
	return strings.ReplaceAll(key, "-", "")
}

func decodeWebAddresses(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	var addresses []string
	collectWebAddressValues(value, &addresses)
	return uniqueWebAddresses(addresses)
}

func decodeWebSenders(raw json.RawMessage) []string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	var senders []string
	collectWebSenderValues(value, &senders)
	return senders
}

func collectWebSenderValues(value any, senders *[]string) {
	switch value := value.(type) {
	case string:
		if value = strings.TrimSpace(value); value != "" {
			*senders = append(*senders, value)
		}
	case []any:
		for _, item := range value {
			collectWebSenderValues(item, senders)
		}
	case map[string]any:
		var addresses []string
		collectWebAddressValues(value, &addresses)
		*senders = append(*senders, uniqueWebAddresses(addresses)...)
	}
}

func collectWebAddressValues(value any, addresses *[]string) {
	switch value := value.(type) {
	case string:
		*addresses = append(*addresses, parseWebAddressString(value)...)
	case []any:
		for _, item := range value {
			collectWebAddressValues(item, addresses)
		}
	case map[string]any:
		for key, item := range value {
			switch normalizeWebJSONKey(key) {
			case "address", "email", "emailaddress", "value":
				collectWebAddressValues(item, addresses)
			default:
				if isWebRecipientKey(key) {
					collectWebAddressValues(item, addresses)
				}
			}
		}
	}
}

func parseWebAddressString(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := netmail.ParseAddressList(raw)
	if err != nil {
		return nil
	}
	addresses := make([]string, 0, len(parsed))
	for _, address := range parsed {
		if normalized := canonicalWebAddress(address.Address); normalized != "" {
			addresses = append(addresses, normalized)
		}
	}
	return addresses
}

func canonicalWebAddress(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func uniqueWebAddresses(addresses []string) []string {
	seen := make(map[string]struct{}, len(addresses))
	unique := make([]string, 0, len(addresses))
	for _, address := range addresses {
		address = canonicalWebAddress(address)
		if address == "" {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		unique = append(unique, address)
	}
	return unique
}

func containsWebAddress(addresses []string, want string) bool {
	for _, address := range addresses {
		if strings.EqualFold(address, want) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
