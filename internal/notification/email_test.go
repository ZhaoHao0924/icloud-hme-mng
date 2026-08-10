package notification

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func Test163ConfigurationIsPersistedAndAuthorizationCodeIsNotPublic(t *testing.T) {
	dataDir := t.TempDir()
	notifier, err := New(dataDir)
	if err != nil {
		t.Fatalf("create notifier: %v", err)
	}
	defer notifier.Close()

	const authorizationCode = "qq-authorization-code"
	public, err := notifier.Update(UpdateInput{
		Enabled:           true,
		SenderEmail:       "sender@163.com",
		AuthorizationCode: authorizationCode,
		RecipientEmail:    "recipient@qq.com",
	})
	if err != nil {
		t.Fatalf("update notifier: %v", err)
	}
	if !public.Enabled || !public.Configured || public.Provider != Provider163 {
		t.Fatalf("public config = %+v", public)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal public config: %v", err)
	}
	if strings.Contains(string(encoded), authorizationCode) {
		t.Fatalf("public config leaked authorization code: %s", encoded)
	}

	storedPath := filepath.Join(dataDir, configFileName)
	stored, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	if !strings.Contains(string(stored), authorizationCode) {
		t.Fatalf("stored config did not contain authorization code")
	}
	if info, err := os.Stat(storedPath); runtime.GOOS != "windows" && err == nil && info.Mode().Perm()&0077 != 0 {
		t.Errorf("stored config permissions = %o, want no group/other permissions", info.Mode().Perm())
	}

	if _, err := notifier.Update(UpdateInput{
		Enabled:        true,
		SenderEmail:    "sender@163.com",
		RecipientEmail: "recipient@qq.com",
	}); err != nil {
		t.Fatalf("update without replacing authorization code: %v", err)
	}
	if notifier.Config().AuthorizationCode != authorizationCode {
		t.Fatal("empty authorization code unexpectedly cleared the saved code")
	}
}

func Test163ConfigurationRejectsNon163Sender(t *testing.T) {
	notifier := newNotifier("", Config{}, SenderFunc(func(Config, Message) error { return nil }))
	defer notifier.Close()

	_, err := notifier.Update(UpdateInput{
		Enabled:           true,
		SenderEmail:       "sender@qq.com",
		AuthorizationCode: "secret",
		RecipientEmail:    "recipient@qq.com",
	})
	if err == nil || !strings.Contains(err.Error(), "163") {
		t.Fatalf("update error = %v, want 163 sender validation error", err)
	}
}

func Test163ConfigurationRejectsNonQQRecipient(t *testing.T) {
	notifier := newNotifier("", Config{}, SenderFunc(func(Config, Message) error { return nil }))
	defer notifier.Close()

	_, err := notifier.Update(UpdateInput{
		Enabled:           true,
		SenderEmail:       "sender@163.com",
		AuthorizationCode: "secret",
		RecipientEmail:    "recipient@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "QQ") {
		t.Fatalf("update error = %v, want QQ recipient validation error", err)
	}
}

func TestNotifierRetriesQueuedEventWithoutBlockingCaller(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	delivered := make(chan struct{})
	notifier := newNotifier("", Config{
		Enabled:           true,
		SenderEmail:       "sender@163.com",
		AuthorizationCode: "secret",
		RecipientEmail:    "recipient@qq.com",
	}, SenderFunc(func(config Config, message Message) error {
		mu.Lock()
		attempts++
		current := attempts
		mu.Unlock()
		if current < 3 {
			return errors.New("temporary SMTP failure")
		}
		close(delivered)
		return nil
	}))
	notifier.retryDelay = 0
	defer notifier.Close()

	started := time.Now()
	if !notifier.Notify(Event{AccountID: "acc_private", Kind: "alias_automation", Status: "success"}) {
		t.Fatal("Notify returned false for a configured notifier")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Notify blocked for %s", elapsed)
	}

	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("queued notification was not delivered")
	}
	mu.Lock()
	gotAttempts := attempts
	mu.Unlock()
	if gotAttempts != maxAttempts {
		t.Fatalf("send attempts = %d, want %d", gotAttempts, maxAttempts)
	}
}

func TestNotifierSkipsDisabledConfig(t *testing.T) {
	called := false
	notifier := newNotifier("", Config{
		SenderEmail:       "sender@163.com",
		AuthorizationCode: "secret",
		RecipientEmail:    "recipient@qq.com",
	}, SenderFunc(func(Config, Message) error {
		called = true
		return nil
	}))
	defer notifier.Close()

	if notifier.Notify(Event{Kind: "alias_automation"}) {
		t.Fatal("Notify returned true while disabled")
	}
	time.Sleep(10 * time.Millisecond)
	if called {
		t.Fatal("disabled notifier sent a message")
	}
}

func TestRenderEventRedactsAccountAndNormalizesError(t *testing.T) {
	at := time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC)
	message := renderEvent(Event{
		AccountID: "acc_12345678",
		Complete:  false,
		Error:     "line one\nline two",
		Kind:      "session_expired",
		Status:    "error",
	}, at)

	if message.Subject == "" || !strings.Contains(message.Subject, "iCloud 会话失效") {
		t.Fatalf("subject = %q", message.Subject)
	}
	if strings.Contains(message.Subject, "acc_12345678") {
		t.Fatalf("subject leaked the raw account id: %q", message.Subject)
	}
	for name, body := range map[string]string{"text": message.Text, "html": message.HTML} {
		if !strings.Contains(body, "acc_...5678") {
			t.Errorf("%s body did not redact the account id: %q", name, body)
		}
		if strings.Contains(body, "acc_12345678") {
			t.Errorf("%s body leaked the raw account id: %q", name, body)
		}
		if strings.Contains(body, "line one\nline two") {
			t.Errorf("%s body contains a raw newline in the error summary: %q", name, body)
		}
		if !strings.Contains(body, "line one line two") {
			t.Errorf("%s body dropped the error summary: %q", name, body)
		}
	}
	// An expired session carries no alias counters, so the block is omitted.
	if strings.Contains(message.Text, "创建统计") {
		t.Errorf("session_expired text body included the counter section: %q", message.Text)
	}
	if !strings.Contains(message.Text, "建议操作") {
		t.Errorf("session_expired text body omitted the recommended action: %q", message.Text)
	}
}

func TestRenderEventSummarizesAutomationCounters(t *testing.T) {
	at := time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC)
	message := renderEvent(Event{
		Complete:  true,
		Created:   3,
		Kind:      "alias_automation",
		Requested: 5,
		Failed:    2,
		Status:    "partial",
		Trigger:   "scheduled",
	}, at)

	if !strings.Contains(message.Subject, "新增 3/5") {
		t.Errorf("subject omitted the created/requested counts: %q", message.Subject)
	}
	if !strings.Contains(message.Subject, "部分成功") {
		t.Errorf("subject omitted the localized status: %q", message.Subject)
	}
	for _, want := range []string{"创建统计", "计划创建：5 个", "成功创建：3 个", "创建失败：2 个", "定时触发"} {
		if !strings.Contains(message.Text, want) {
			t.Errorf("text body omitted %q: %q", want, message.Text)
		}
	}
	if message.Date != at {
		t.Errorf("message date = %s, want %s", message.Date, at)
	}
}

func TestBuildMIMEMessageProducesMultipartAlternative(t *testing.T) {
	config := Config{
		SenderEmail:       "sender@163.com",
		AuthorizationCode: "secret",
		RecipientEmail:    "recipient@qq.com",
	}
	message := renderEvent(Event{Kind: "alias_automation", Status: "success", Created: 1, Requested: 1}, time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC))

	raw, err := buildMIMEMessage(config, message)
	if err != nil {
		t.Fatalf("build MIME message: %v", err)
	}
	headers, body, found := strings.Cut(raw, "\r\n\r\n")
	if !found {
		t.Fatalf("message has no header/body separator: %q", raw)
	}
	for _, want := range []string{
		"From: ",
		"To: recipient@qq.com\r\n",
		"Subject: ",
		"Date: ",
		"Message-ID: <",
		"@163.com>",
		"Auto-Submitted: auto-generated\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: multipart/alternative; boundary=\"",
	} {
		if !strings.Contains(headers+"\r\n", want) {
			t.Errorf("headers omitted %q: %q", want, headers)
		}
	}
	if strings.Contains(headers, config.AuthorizationCode) {
		t.Error("headers leaked the authorization code")
	}
	if count := strings.Count(body, "Content-Transfer-Encoding: base64"); count != 2 {
		t.Errorf("base64 part count = %d, want 2 (text and html)", count)
	}
	if !strings.Contains(body, "Content-Type: text/plain; charset=UTF-8") ||
		!strings.Contains(body, "Content-Type: text/html; charset=UTF-8") {
		t.Errorf("body is missing an alternative part: %q", body)
	}
	// Non-ASCII subjects must be encoded, never sent as raw UTF-8 header bytes.
	if strings.Contains(headers, "别名自动化") {
		t.Errorf("subject header was not RFC 2047 encoded: %q", headers)
	}

	const boundaryPrefix = "boundary=\""
	start := strings.Index(headers, boundaryPrefix)
	if start < 0 {
		t.Fatalf("headers omitted the multipart boundary: %q", headers)
	}
	rest := headers[start+len(boundaryPrefix):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("unterminated multipart boundary in %q", headers)
	}
	boundary := rest[:end]
	if !strings.HasPrefix(boundary, "=_hme_") || boundary == "=_hme_" {
		t.Errorf("boundary = %q, want the %q prefix plus a random suffix", boundary, "=_hme_")
	}
	if !strings.HasSuffix(strings.TrimRight(raw, "\r\n"), "--"+boundary+"--") {
		t.Errorf("message does not end with the closing boundary: %q", raw)
	}
	for _, line := range strings.Split(raw, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line exceeds the SMTP limit (%d bytes): %q", len(line), line)
		}
	}
}

func TestSendTestDeliversRenderedTestMessage(t *testing.T) {
	delivered := make(chan Message, 1)
	notifier := newNotifier("", Config{
		Enabled:           true,
		SenderEmail:       "sender@163.com",
		AuthorizationCode: "secret",
		RecipientEmail:    "recipient@qq.com",
	}, SenderFunc(func(config Config, message Message) error {
		delivered <- message
		return nil
	}))
	defer notifier.Close()

	if err := notifier.SendTest(); err != nil {
		t.Fatalf("send test: %v", err)
	}
	message := <-delivered
	if !strings.Contains(message.Subject, "邮件通知测试") {
		t.Errorf("test subject = %q", message.Subject)
	}
	for _, want := range []string{"sender@163.com", "recipient@qq.com", SMTPHost} {
		if !strings.Contains(message.Text, want) {
			t.Errorf("test body omitted %q: %q", want, message.Text)
		}
	}
	if strings.Contains(message.Text, "secret") || strings.Contains(message.HTML, "secret") {
		t.Error("test message leaked the authorization code")
	}
}
