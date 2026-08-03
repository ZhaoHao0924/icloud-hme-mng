package notification

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	webhookConfigFileName = "webhook-notification.json"
	webhookQueueSize      = 32
	webhookMaxAttempts    = 3
	webhookTimeout        = 15 * time.Second
	webhookMaxURLLength   = 2048
	webhookMaxSecretLen   = 512
)

var (
	ErrWebhookNotConfigured = errors.New("webhook 通知尚未配置")
	ErrWebhookPersistence   = errors.New("webhook 通知配置持久化失败")
)

// WebhookConfig is the in-memory outbound webhook configuration.
type WebhookConfig struct {
	Enabled bool
	URL     string
	Secret  string
}

// WebhookPublicConfig is safe to return to the browser. Secret is deliberately
// represented only by Configured and never returned as a value.
type WebhookPublicConfig struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	URL        string `json:"url"`
}

// WebhookUpdateInput is accepted by the configuration API. An empty secret
// keeps the currently stored secret so the UI can edit the URL or switch
// without asking the user to paste it again.
type WebhookUpdateInput struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Secret  string `json:"secret"`
}

// WebhookSender is injectable so delivery and retry behavior can be tested
// without making a network request.
type WebhookSender interface {
	Send(config WebhookConfig, event Event) error
}

type WebhookSenderFunc func(config WebhookConfig, event Event) error

func (f WebhookSenderFunc) Send(config WebhookConfig, event Event) error {
	return f(config, event)
}

type storedWebhookConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Secret  string `json:"secret"`
}

type queuedWebhookEvent struct {
	config WebhookConfig
	event  Event
}

// WebhookNotifier owns the persisted webhook configuration and a bounded
// asynchronous delivery queue. A slow endpoint never blocks an iCloud task.
type WebhookNotifier struct {
	mu     sync.RWMutex
	path   string
	config WebhookConfig
	closed bool

	sender     WebhookSender
	queue      chan queuedWebhookEvent
	stop       chan struct{}
	done       chan struct{}
	retryDelay time.Duration
	logger     func(string, ...any)
	workerOnce sync.Once
	closeOnce  sync.Once
}

// NewWebhook loads the webhook configuration below dataDir and starts its
// delivery worker.
func NewWebhook(dataDir string) (*WebhookNotifier, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("webhook notification data directory is required")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create webhook notification data directory: %w", err)
	}

	notifier := newWebhookNotifier(
		filepath.Join(dataDir, webhookConfigFileName),
		WebhookConfig{},
		webhookHTTPSender{client: &http.Client{
			Timeout: webhookTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("webhook redirects are not allowed")
			},
		}},
	)
	if err := notifier.load(); err != nil {
		notifier.Close()
		return nil, err
	}
	notifier.start()
	return notifier, nil
}

func newWebhookNotifier(path string, config WebhookConfig, sender WebhookSender) *WebhookNotifier {
	if sender == nil {
		sender = webhookHTTPSender{client: &http.Client{Timeout: webhookTimeout}}
	}
	notifier := &WebhookNotifier{
		path:       path,
		config:     config,
		sender:     sender,
		queue:      make(chan queuedWebhookEvent, webhookQueueSize),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		retryDelay: time.Millisecond,
		logger:     func(string, ...any) {},
	}
	notifier.start()
	return notifier
}

func (n *WebhookNotifier) start() {
	n.workerOnce.Do(func() { go n.run() })
}

func (n *WebhookNotifier) run() {
	defer close(n.done)
	for {
		select {
		case queued := <-n.queue:
			n.deliver(queued)
		case <-n.stop:
			return
		}
	}
}

// Close stops future deliveries. In-flight requests are bounded by the HTTP
// client timeout and finish before the worker exits.
func (n *WebhookNotifier) Close() {
	if n == nil {
		return
	}
	n.closeOnce.Do(func() {
		n.mu.Lock()
		n.closed = true
		n.mu.Unlock()
		close(n.stop)
		<-n.done
	})
}

func (n *WebhookNotifier) Config() WebhookConfig {
	if n == nil {
		return WebhookConfig{}
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.config
}

func (n *WebhookNotifier) PublicConfig() WebhookPublicConfig {
	return publicWebhookConfig(n.Config())
}

// Update validates and atomically persists a new webhook configuration.
func (n *WebhookNotifier) Update(input WebhookUpdateInput) (WebhookPublicConfig, error) {
	if n == nil {
		return WebhookPublicConfig{}, errors.New("webhook notification service is unavailable")
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return WebhookPublicConfig{}, errors.New("webhook notification service is closed")
	}

	next := n.config
	next.Enabled = input.Enabled
	if strings.TrimSpace(input.URL) != "" {
		next.URL = strings.TrimSpace(input.URL)
	}
	if strings.TrimSpace(input.Secret) != "" {
		next.Secret = strings.TrimSpace(input.Secret)
	}
	if err := validateWebhookConfig(next); err != nil {
		return WebhookPublicConfig{}, err
	}
	if err := writeWebhookConfigAtomic(n.path, next); err != nil {
		return WebhookPublicConfig{}, err
	}
	n.config = next
	return publicWebhookConfig(next), nil
}

// Notify queues a privacy-safe event only when webhook delivery is enabled
// and configured. It returns false when disabled or when the queue is full.
func (n *WebhookNotifier) Notify(event Event) bool {
	if n == nil {
		return false
	}
	n.mu.RLock()
	if n.closed || !n.config.Enabled || !isWebhookConfigured(n.config) {
		n.mu.RUnlock()
		return false
	}
	queued := queuedWebhookEvent{config: n.config, event: event}
	queue := n.queue
	stop := n.stop
	n.mu.RUnlock()

	select {
	case queue <- queued:
		return true
	case <-stop:
		return false
	default:
		if n.logger != nil {
			n.logger("webhook notification queue is full; dropping event kind=%s", event.Kind)
		}
		return false
	}
}

// SendTest sends a synchronous signed test payload using the saved
// configuration.
func (n *WebhookNotifier) SendTest() error {
	if n == nil {
		return errors.New("webhook notification service is unavailable")
	}
	config := n.Config()
	if !isWebhookConfigured(config) {
		return ErrWebhookNotConfigured
	}
	return n.sendWithRetry(config, Event{
		Complete: true,
		Kind:     "webhook_test",
		Status:   "test",
		Trigger:  "system",
	})
}

func (n *WebhookNotifier) deliver(queued queuedWebhookEvent) {
	if err := n.sendWithRetry(queued.config, queued.event); err != nil && n.logger != nil {
		n.logger("webhook notification failed after %d attempts: %v", webhookMaxAttempts, err)
	}
}

func (n *WebhookNotifier) sendWithRetry(config WebhookConfig, event Event) error {
	var lastErr error
	for attempt := 0; attempt < webhookMaxAttempts; attempt++ {
		if err := n.sender.Send(config, event); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt+1 < webhookMaxAttempts && n.retryDelay > 0 {
			timer := time.NewTimer(n.retryDelay)
			select {
			case <-timer.C:
			case <-n.stop:
				if !timer.Stop() {
					<-timer.C
				}
				return lastErr
			}
		}
	}
	return lastErr
}

func (n *WebhookNotifier) load() error {
	raw, err := os.ReadFile(n.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read webhook notification configuration: %w", err)
	}
	var stored storedWebhookConfig
	if err := json.Unmarshal(raw, &stored); err != nil {
		return fmt.Errorf("decode webhook notification configuration: %w", err)
	}
	config := WebhookConfig{Enabled: stored.Enabled, URL: stored.URL, Secret: stored.Secret}
	if err := validateWebhookConfig(config); err != nil {
		return fmt.Errorf("invalid webhook notification configuration: %w", err)
	}
	n.config = config
	return nil
}

func validateWebhookConfig(config WebhookConfig) error {
	config.URL = strings.TrimSpace(config.URL)
	config.Secret = strings.TrimSpace(config.Secret)
	if config.URL == "" && config.Secret == "" && !config.Enabled {
		return nil
	}
	if err := validateWebhookURL(config.URL); err != nil {
		return err
	}
	if config.Secret == "" {
		return errors.New("webhook 签名密钥不能为空")
	}
	if len(config.Secret) > webhookMaxSecretLen {
		return fmt.Errorf("webhook 签名密钥不能超过 %d 个字符", webhookMaxSecretLen)
	}
	if config.Enabled && !isWebhookConfigured(config) {
		return ErrWebhookNotConfigured
	}
	return nil
}

func validateWebhookURL(value string) error {
	if value == "" {
		return errors.New("webhook URL 不能为空")
	}
	if len(value) > webhookMaxURLLength || strings.ContainsAny(value, "\r\n") {
		return errors.New("webhook URL 无效")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("webhook URL 必须是有效的 HTTPS 地址")
	}
	if parsed.Hostname() == "" {
		return errors.New("webhook URL 必须包含主机名")
	}
	return nil
}

func isWebhookConfigured(config WebhookConfig) bool {
	return strings.TrimSpace(config.URL) != "" && strings.TrimSpace(config.Secret) != ""
}

func publicWebhookConfig(config WebhookConfig) WebhookPublicConfig {
	return WebhookPublicConfig{
		Enabled:    config.Enabled,
		Configured: isWebhookConfigured(config),
		URL:        config.URL,
	}
}

func writeWebhookConfigAtomic(path string, config WebhookConfig) (err error) {
	raw, err := json.Marshal(storedWebhookConfig{
		Enabled: config.Enabled,
		URL:     config.URL,
		Secret:  config.Secret,
	})
	if err != nil {
		return fmt.Errorf("%w: encode configuration: %v", ErrWebhookPersistence, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".webhook-notification-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: create temporary file: %v", ErrWebhookPersistence, err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(0600); err != nil {
		return fmt.Errorf("%w: set temporary file permissions: %v", ErrWebhookPersistence, err)
	}
	if _, err = temporary.Write(raw); err != nil {
		return fmt.Errorf("%w: write configuration: %v", ErrWebhookPersistence, err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("%w: sync configuration: %v", ErrWebhookPersistence, err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("%w: close configuration: %v", ErrWebhookPersistence, err)
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("%w: replace configuration: %v", ErrWebhookPersistence, err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("%w: set configuration permissions: %v", ErrWebhookPersistence, err)
	}
	return nil
}

type webhookEventPayload struct {
	Event       string `json:"event"`
	AccountID   string `json:"account_id,omitempty"`
	Complete    bool   `json:"complete"`
	Created     int    `json:"created"`
	Error       string `json:"error,omitempty"`
	Failed      int    `json:"failed"`
	PauseReason string `json:"pause_reason,omitempty"`
	Requested   int    `json:"requested"`
	Status      string `json:"status"`
	Trigger     string `json:"trigger"`
}

func webhookPayload(event Event) webhookEventPayload {
	kind := event.Kind
	if kind == "" {
		kind = "alias_automation"
	}
	status := event.Status
	if status == "" {
		status = "unknown"
	}
	accountID := ""
	if strings.TrimSpace(event.AccountID) != "" {
		accountID = redactAccountID(event.AccountID)
	}
	return webhookEventPayload{
		Event:       oneLine(kind),
		AccountID:   accountID,
		Complete:    event.Complete,
		Created:     event.Created,
		Error:       oneLine(event.Error),
		Failed:      event.Failed,
		PauseReason: oneLine(event.PauseReason),
		Requested:   event.Requested,
		Status:      oneLine(status),
		Trigger:     statusValue(event.Trigger),
	}
}

type webhookHTTPSender struct {
	client *http.Client
}

func (s webhookHTTPSender) Send(config WebhookConfig, event Event) error {
	if err := validateWebhookURL(config.URL); err != nil {
		return err
	}
	payload, err := json.Marshal(webhookPayload(event))
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	deliveryID, err := randomDeliveryID()
	if err != nil {
		return fmt.Errorf("create webhook delivery id: %w", err)
	}
	signature := signWebhookPayload(config.Secret, timestamp, payload)
	request, err := http.NewRequest(http.MethodPost, config.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "icloud-hme-webhook/1")
	request.Header.Set("X-iCloud-HME-Delivery", deliveryID)
	request.Header.Set("X-iCloud-HME-Timestamp", timestamp)
	request.Header.Set("X-iCloud-HME-Signature", "sha256="+signature)
	client := s.client
	if client == nil {
		client = &http.Client{Timeout: webhookTimeout}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("send webhook request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("webhook endpoint returned HTTP %d", response.StatusCode)
	}
	return nil
}

func signWebhookPayload(secret, timestamp string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func randomDeliveryID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
