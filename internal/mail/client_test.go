package mail

import (
	"bytes"
	"errors"
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
	loginCalls     int
	logoutCalls    int
	terminateCalls int
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
