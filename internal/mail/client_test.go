package mail

import (
	"bytes"
	"errors"
	"net/mail"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap"
)

type fakeIMAPSession struct {
	loginErr       error
	logoutErr      error
	terminateErr   error
	searchErr      error
	fetchErr       error
	searchUIDs     []uint32
	fetchMessages  []*imap.Message
	criteria       []*imap.SearchCriteria
	fetchItems     []imap.FetchItem
	fetchSet       string
	selectCalls    []fakeIMAPSelectCall
	loginCalls     int
	logoutCalls    int
	terminateCalls int
}

type fakeIMAPSelectCall struct {
	name     string
	readOnly bool
}

func messageIDs(messages []Message) []string {
	ids := make([]string, len(messages))
	for i := range messages {
		ids[i] = messages[i].ID
	}
	return ids
}

func (f *fakeIMAPSession) Login(_, _ string) error {
	f.loginCalls++
	return f.loginErr
}

func (f *fakeIMAPSession) Logout() error {
	f.logoutCalls++
	return f.logoutErr
}

func (f *fakeIMAPSession) Terminate() error {
	f.terminateCalls++
	return f.terminateErr
}

func (f *fakeIMAPSession) Select(name string, readOnly bool) (*imap.MailboxStatus, error) {
	f.selectCalls = append(f.selectCalls, fakeIMAPSelectCall{name: name, readOnly: readOnly})
	return &imap.MailboxStatus{Name: name, ReadOnly: readOnly}, nil
}

func (f *fakeIMAPSession) UidSearch(criteria *imap.SearchCriteria) ([]uint32, error) {
	f.criteria = append(f.criteria, criteria)
	return append([]uint32(nil), f.searchUIDs...), f.searchErr
}

func (f *fakeIMAPSession) UidFetch(seqset *imap.SeqSet, items []imap.FetchItem, ch chan *imap.Message) error {
	f.fetchSet = seqset.String()
	f.fetchItems = append([]imap.FetchItem(nil), items...)
	defer close(ch)
	for _, msg := range f.fetchMessages {
		if seqset.Contains(msg.Uid) {
			ch <- msg
		}
	}
	return f.fetchErr
}

func TestConnectTerminatesSessionAfterLoginFailure(t *testing.T) {
	loginErr := errors.New("bad credentials")
	session := &fakeIMAPSession{loginErr: loginErr}
	client := NewClient("user@example.com", "app-password")
	client.dial = func(addr string) (imapSession, error) {
		if addr != "imap.mail.me.com:993" {
			t.Fatalf("dial address = %q", addr)
		}
		return session, nil
	}

	err := client.Connect()
	if !errors.Is(err, loginErr) {
		t.Fatalf("Connect() error = %v, want login error", err)
	}
	if session.terminateCalls != 1 {
		t.Fatalf("Terminate calls = %d, want 1", session.terminateCalls)
	}
	if client.cli != nil {
		t.Fatal("failed session retained on client")
	}
}

func TestDisconnectTerminatesSessionWhenLogoutFails(t *testing.T) {
	session := &fakeIMAPSession{logoutErr: errors.New("connection lost")}
	client := NewClient("user@example.com", "app-password")
	client.cli = session

	client.Disconnect()

	if session.logoutCalls != 1 {
		t.Fatalf("Logout calls = %d, want 1", session.logoutCalls)
	}
	if session.terminateCalls != 1 {
		t.Fatalf("Terminate calls = %d, want 1", session.terminateCalls)
	}
	if client.cli != nil {
		t.Fatal("disconnected session retained on client")
	}
}

func TestListInboxUsesDaysSearchAndReturnsNewestFirst(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	session := &fakeIMAPSession{
		searchUIDs: []uint32{3, 1, 2},
		fetchMessages: []*imap.Message{
			newTestIMAPMessage(2, now.Add(-2*time.Hour), "second"),
			newTestIMAPMessage(1, now.Add(-30*time.Minute), "not fetched"),
			newTestIMAPMessage(3, now.Add(-time.Hour), "first"),
		},
	}
	client := NewClient("user@example.com", "app-password")
	client.cli = session
	client.now = func() time.Time { return now }

	messages, err := client.ListInbox(2, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.criteria) != 1 {
		t.Fatalf("search calls = %d, want 1", len(session.criteria))
	}
	wantSince := now.AddDate(0, 0, -7)
	if !session.criteria[0].Since.Equal(wantSince) {
		t.Fatalf("SINCE = %v, want %v", session.criteria[0].Since, wantSince)
	}
	if session.fetchSet != "2:3" {
		t.Fatalf("UID fetch set = %q, want %q", session.fetchSet, "2:3")
	}
	if !containsFetchItem(session.fetchItems, "BODY.PEEK[]<0.65536>") {
		t.Fatalf("fetch items = %v, want bounded BODY.PEEK", session.fetchItems)
	}
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if messages[0].ID != "3" || messages[1].ID != "2" {
		t.Fatalf("message order = [%s, %s], want [3, 2]", messages[0].ID, messages[1].ID)
	}
}

func TestListInboxSummariesSkipsBodyFetch(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	session := &fakeIMAPSession{
		searchUIDs: []uint32{1},
		fetchMessages: []*imap.Message{
			newTestIMAPMessage(1, now, "body that must not be fetched"),
		},
	}
	client := NewClient("user@example.com", "app-password")
	client.cli = session

	messages, err := client.ListInboxSummaries(20, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if messages[0].Preview != "" {
		t.Fatalf("preview = %q, want empty summary preview", messages[0].Preview)
	}
	for _, item := range session.fetchItems {
		if strings.HasPrefix(string(item), "BODY") || item == imap.FetchRFC822 {
			t.Fatalf("summary fetch items = %v, unexpectedly request a body", session.fetchItems)
		}
	}
}

func TestListInboxSummariesPageUsesUIDCursor(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	session := &fakeIMAPSession{
		searchUIDs: []uint32{5, 1, 4, 2, 3},
		fetchMessages: []*imap.Message{
			newTestIMAPMessage(1, now.Add(-4*time.Hour), "one"),
			newTestIMAPMessage(2, now.Add(-3*time.Hour), "two"),
			newTestIMAPMessage(3, now.Add(-2*time.Hour), "three"),
			newTestIMAPMessage(4, now.Add(-time.Hour), "four"),
			newTestIMAPMessage(5, now, "five"),
		},
	}
	client := NewClient("user@example.com", "app-password")
	client.cli = session

	first, err := client.ListInboxSummariesPage(2, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	if session.fetchSet != "4:5" {
		t.Fatalf("first fetch set = %q, want 4:5", session.fetchSet)
	}
	if first.NextBeforeUID != 4 {
		t.Fatalf("first next cursor = %d, want 4", first.NextBeforeUID)
	}
	if got := messageIDs(first.Messages); !slices.Equal(got, []string{"5", "4"}) {
		t.Fatalf("first messages = %v, want [5 4]", got)
	}

	second, err := client.ListInboxSummariesPage(2, 7, first.NextBeforeUID)
	if err != nil {
		t.Fatal(err)
	}
	if session.fetchSet != "2:3" {
		t.Fatalf("second fetch set = %q, want 2:3", session.fetchSet)
	}
	if second.NextBeforeUID != 2 {
		t.Fatalf("second next cursor = %d, want 2", second.NextBeforeUID)
	}
	if got := messageIDs(second.Messages); !slices.Equal(got, []string{"3", "2"}) {
		t.Fatalf("second messages = %v, want [3 2]", got)
	}

	third, err := client.ListInboxSummariesPage(2, 7, second.NextBeforeUID)
	if err != nil {
		t.Fatal(err)
	}
	if session.fetchSet != "1" {
		t.Fatalf("third fetch set = %q, want 1", session.fetchSet)
	}
	if third.NextBeforeUID != 0 {
		t.Fatalf("third next cursor = %d, want 0", third.NextBeforeUID)
	}
	if got := messageIDs(third.Messages); !slices.Equal(got, []string{"1"}) {
		t.Fatalf("third messages = %v, want [1]", got)
	}
}

func TestGetPreviewFetchesOnlyTheRequestedMessage(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	session := &fakeIMAPSession{
		fetchMessages: []*imap.Message{
			newTestIMAPMessage(7, now, "selected message body"),
			newTestIMAPMessage(8, now.Add(time.Minute), "other message body"),
		},
	}
	client := NewClient("user@example.com", "app-password")
	client.cli = session

	message, err := client.GetPreview(7)
	if err != nil {
		t.Fatal(err)
	}
	if session.fetchSet != "7" {
		t.Fatalf("UID fetch set = %q, want 7", session.fetchSet)
	}
	if message.ID != "7" || message.Preview != "selected message body" {
		t.Fatalf("message = %+v, want selected UID and preview", message)
	}
	if !containsFetchItem(session.fetchItems, "BODY.PEEK[]<0.65536>") {
		t.Fatalf("fetch items = %v, want bounded preview body", session.fetchItems)
	}
}

func TestSelectedInboxIsReusedAcrossSummaryAndPreviewFetches(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	session := &fakeIMAPSession{
		searchUIDs: []uint32{7},
		fetchMessages: []*imap.Message{
			newTestIMAPMessage(7, now, "selected message body"),
		},
	}
	client := NewClient("user@example.com", "app-password")
	client.cli = session

	if _, err := client.ListInboxSummaries(20, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetPreview(7); err != nil {
		t.Fatal(err)
	}

	if len(session.selectCalls) != 1 {
		t.Fatalf("SELECT calls = %+v, want one INBOX selection", session.selectCalls)
	}
	if got := session.selectCalls[0]; got.name != "INBOX" || !got.readOnly {
		t.Fatalf("SELECT call = %+v, want INBOX read-only", got)
	}
}

func TestDisconnectResetsSelectedInbox(t *testing.T) {
	session := &fakeIMAPSession{}
	client := NewClient("user@example.com", "app-password")
	client.cli = session
	client.selected = "INBOX"
	client.selectedRO = true

	client.Disconnect()

	if client.selected != "" || client.selectedRO {
		t.Fatalf("selected state = (%q, %v), want reset", client.selected, client.selectedRO)
	}
}

func TestIMAPPreviewIsBoundedAtUTF8Boundary(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	session := &fakeIMAPSession{
		searchUIDs: []uint32{1},
		fetchMessages: []*imap.Message{
			newTestIMAPMessage(1, now, strings.Repeat("界", maxPreviewBytes)),
		},
	}
	client := NewClient("user@example.com", "app-password")
	client.cli = session

	messages, err := client.ListInbox(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	preview := messages[0].Preview
	if len(preview) > maxPreviewBytes {
		t.Fatalf("preview bytes = %d, max %d", len(preview), maxPreviewBytes)
	}
	if !utf8.ValidString(preview) {
		t.Fatal("preview ends with invalid UTF-8")
	}
	if preview == "" {
		t.Fatal("preview unexpectedly empty")
	}
}

func TestReadBodyRemovesNonVisibleHTMLContent(t *testing.T) {
	raw := "Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		`<!doctype html><html><head><style>@font-face { font-family: Test; src: url(https://tracker.example/font.woff2); }</style></head>` +
		`<body><h1>你的 ChatGPT 临时验证码</h1><p>验证码：<strong>123456</strong></p>` +
		`<p>安全 &amp; 隐私</p><script>sendSecretToTracker()</script></body></html>`
	message, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	body, err := readBody(message)
	if err != nil {
		t.Fatal(err)
	}
	for _, visible := range []string{"你的 ChatGPT 临时验证码", "验证码：123456", "安全 & 隐私"} {
		if !strings.Contains(body, visible) {
			t.Errorf("body = %q, want visible text %q", body, visible)
		}
	}
	for _, hidden := range []string{"@font-face", "font.woff2", "sendSecretToTracker"} {
		if strings.Contains(body, hidden) {
			t.Errorf("body = %q, contains hidden HTML content %q", body, hidden)
		}
	}
}

func TestReadBodyPrefersDecodedPlainTextFromMultipartAlternative(t *testing.T) {
	raw := "Content-Type: multipart/alternative; boundary=mail-boundary\r\n" +
		"\r\n" +
		"--mail-boundary\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"Readable=20message=20=26=20code=20123456\r\n" +
		"--mail-boundary\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<style>@font-face { src: url(tracker.woff2); }</style><p>HTML fallback</p>\r\n" +
		"--mail-boundary--\r\n"
	message, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	body, err := readBody(message)
	if err != nil {
		t.Fatal(err)
	}
	if body != "Readable message & code 123456" {
		t.Fatalf("body = %q, want decoded plain-text alternative", body)
	}
}

func TestReadRenderableBodyPreservesHTMLAndBuildsSafePreview(t *testing.T) {
	raw := "Content-Type: multipart/alternative; boundary=mail-boundary\r\n" +
		"\r\n" +
		"--mail-boundary\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Open the account page\r\n" +
		"--mail-boundary\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		`<style>.action { color: red; }</style><a class="action" href="https://example.test/account">Open</a>` + "\r\n" +
		"--mail-boundary--\r\n"
	message, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	body, contentType, preview, err := readRenderableBody(message)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "text/html" {
		t.Fatalf("content type = %q, want text/html", contentType)
	}
	for _, expected := range []string{"<style>", "https://example.test/account"} {
		if !strings.Contains(body, expected) {
			t.Errorf("body = %q, want %q", body, expected)
		}
	}
	if preview != "Open the account page" {
		t.Fatalf("preview = %q, want plain-text alternative", preview)
	}
}

func containsFetchItem(items []imap.FetchItem, want string) bool {
	for _, item := range items {
		if string(item) == want {
			return true
		}
	}
	return false
}

func newTestIMAPMessage(uid uint32, date time.Time, body string) *imap.Message {
	responseSection, err := imap.ParseBodySectionName("BODY[]<0>")
	if err != nil {
		panic(err)
	}
	raw := "From: sender@example.com\r\n" +
		"To: alias@icloud.com\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" + body
	return &imap.Message{
		Uid:          uid,
		InternalDate: date,
		Envelope: &imap.Envelope{
			Date:    date,
			Subject: "subject",
		},
		Body: map[*imap.BodySectionName]imap.Literal{
			responseSection: bytes.NewBufferString(raw),
		},
	}
}
