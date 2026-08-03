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
	notifier := newNotifier("", Config{}, SenderFunc(func(Config, string, string) error { return nil }))
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
	notifier := newNotifier("", Config{}, SenderFunc(func(Config, string, string) error { return nil }))
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
	}, SenderFunc(func(config Config, subject, body string) error {
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
	}, SenderFunc(func(Config, string, string) error {
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

func TestEventMessageRedactsAccountAndNormalizesError(t *testing.T) {
	subject, body := eventMessage(Event{
		AccountID: "acc_12345678",
		Complete:  false,
		Error:     "line one\nline two",
		Kind:      "session_expired",
		Status:    "error",
	})
	if subject == "" || !strings.Contains(body, "acc_...5678") {
		t.Fatalf("event body = %q", body)
	}
	if strings.Contains(body, "line one\nline two") {
		t.Fatalf("event body contains a raw newline in error summary: %q", body)
	}
}
