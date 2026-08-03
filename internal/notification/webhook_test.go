package notification

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebhookConfigurationIsPersistedAndSecretIsNotPublic(t *testing.T) {
	dataDir := t.TempDir()
	notifier, err := NewWebhook(dataDir)
	if err != nil {
		t.Fatalf("create webhook notifier: %v", err)
	}
	defer notifier.Close()

	const secret = "webhook-test-secret"
	public, err := notifier.Update(WebhookUpdateInput{
		Enabled: true,
		URL:     "https://hooks.example.test/icloud",
		Secret:  secret,
	})
	if err != nil {
		t.Fatalf("update webhook notifier: %v", err)
	}
	if !public.Enabled || !public.Configured || public.URL == "" {
		t.Fatalf("public config = %+v", public)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal public config: %v", err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "secret") {
		t.Fatalf("public config leaked webhook secret: %s", encoded)
	}

	storedPath := dataDir + "\\" + webhookConfigFileName
	stored, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	if !strings.Contains(string(stored), secret) {
		t.Fatal("stored config did not contain webhook secret")
	}

	if _, err := notifier.Update(WebhookUpdateInput{
		Enabled: true,
		URL:     "https://hooks.example.test/updated",
	}); err != nil {
		t.Fatalf("update without replacing webhook secret: %v", err)
	}
	if notifier.Config().Secret != secret {
		t.Fatal("empty webhook secret unexpectedly cleared the saved secret")
	}
}

func TestWebhookConfigurationRequiresHTTPS(t *testing.T) {
	notifier := newWebhookNotifier("", WebhookConfig{}, WebhookSenderFunc(func(WebhookConfig, Event) error { return nil }))
	defer notifier.Close()

	_, err := notifier.Update(WebhookUpdateInput{
		Enabled: true,
		URL:     "http://hooks.example.test/icloud",
		Secret:  "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("update error = %v, want HTTPS validation error", err)
	}
}

func TestWebhookRetriesQueuedEventWithoutBlockingCaller(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	delivered := make(chan struct{})
	notifier := newWebhookNotifier("", WebhookConfig{
		Enabled: true,
		URL:     "https://hooks.example.test/icloud",
		Secret:  "secret",
	}, WebhookSenderFunc(func(config WebhookConfig, event Event) error {
		mu.Lock()
		attempts++
		current := attempts
		mu.Unlock()
		if current < webhookMaxAttempts {
			return errors.New("temporary webhook failure")
		}
		close(delivered)
		return nil
	}))
	notifier.retryDelay = 0
	defer notifier.Close()

	started := time.Now()
	if !notifier.Notify(Event{AccountID: "acc_private", Kind: "alias_automation", Status: "success"}) {
		t.Fatal("Notify returned false for a configured webhook")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Notify blocked for %s", elapsed)
	}

	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("queued webhook was not delivered")
	}
	mu.Lock()
	gotAttempts := attempts
	mu.Unlock()
	if gotAttempts != webhookMaxAttempts {
		t.Fatalf("send attempts = %d, want %d", gotAttempts, webhookMaxAttempts)
	}
}

func TestWebhookHTTPSenderSignsRedactedPayload(t *testing.T) {
	const secret = "signing-secret"
	requestSeen := make(chan *http.Request, 1)
	bodySeen := make(chan []byte, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}
		requestSeen <- r
		bodySeen <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender := webhookHTTPSender{client: server.Client()}
	if err := sender.Send(WebhookConfig{URL: server.URL, Secret: secret}, Event{
		AccountID: "account-private-value",
		Complete:  false,
		Error:     "line one\nline two",
		Kind:      "session_expired",
		Status:    "error",
	}); err != nil {
		t.Fatalf("send webhook: %v", err)
	}

	request := <-requestSeen
	body := <-bodySeen
	if request.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content type = %q", request.Header.Get("Content-Type"))
	}
	timestamp := request.Header.Get("X-iCloud-HME-Timestamp")
	actualSignature := request.Header.Get("X-iCloud-HME-Signature")
	expectedSignature := "sha256=" + signWebhookPayload(secret, timestamp, body)
	if actualSignature != expectedSignature {
		t.Errorf("signature = %q, want %q", actualSignature, expectedSignature)
	}
	var payload webhookEventPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.AccountID != "acco...alue" {
		t.Errorf("account_id = %q", payload.AccountID)
	}
	if strings.Contains(string(body), "line one\nline two") {
		t.Errorf("payload contains raw error newline: %s", body)
	}
}
