// Package notification sends privacy-safe event notifications through 163 Mail
// to a QQ Mail recipient.
package notification

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	netmail "net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	Provider163 = "163"
	SMTPHost    = "smtp.163.com"
	SMTPPort    = 465

	configFileName = "email-notification.json"
	queueSize      = 32
	maxAttempts    = 3
)

var (
	ErrNotConfigured = errors.New("邮件通知尚未配置")
	ErrDisabled      = errors.New("邮件通知未启用")
	ErrPersistence   = errors.New("邮件通知配置持久化失败")
)

// Config is the in-memory 163 Mail configuration. AuthorizationCode never
// leaves this package through an API response.
type Config struct {
	Enabled           bool
	SenderEmail       string
	AuthorizationCode string
	RecipientEmail    string
}

// PublicConfig is safe to return to the browser.
type PublicConfig struct {
	Enabled        bool   `json:"enabled"`
	Configured     bool   `json:"configured"`
	Provider       string `json:"provider"`
	SMTPHost       string `json:"smtp_host"`
	SMTPPort       int    `json:"smtp_port"`
	SenderEmail    string `json:"sender_email"`
	RecipientEmail string `json:"recipient_email"`
}

// UpdateInput is accepted by the configuration API. An empty authorization
// code keeps the currently stored code, which lets the UI edit safe fields
// without asking the user to paste the secret again.
type UpdateInput struct {
	Enabled           bool   `json:"enabled"`
	SenderEmail       string `json:"sender_email"`
	AuthorizationCode string `json:"authorization_code"`
	RecipientEmail    string `json:"recipient_email"`
}

// Event is a redacted event summary. It intentionally has no alias address,
// cookie, password, or mail content.
type Event struct {
	AccountID   string
	Error       string
	Failed      int
	PauseReason string
	Requested   int
	Created     int
	Complete    bool
	Status      string
	Trigger     string
	Kind        string
}

type storedConfig struct {
	Enabled           bool   `json:"enabled"`
	SenderEmail       string `json:"sender_email"`
	AuthorizationCode string `json:"authorization_code"`
	RecipientEmail    string `json:"recipient_email"`
}

type Sender interface {
	Send(config Config, subject, body string) error
}

type SenderFunc func(config Config, subject, body string) error

func (f SenderFunc) Send(config Config, subject, body string) error {
	return f(config, subject, body)
}

type smtpSender struct {
	dialTimeout time.Duration
}

type queuedEvent struct {
	config Config
	event  Event
}

// Notifier owns the persisted 163 configuration and a small asynchronous
// delivery queue. Queueing is deliberately best-effort: a full queue never
// blocks an iCloud operation.
type Notifier struct {
	mu     sync.RWMutex
	path   string
	config Config
	closed bool

	sender     Sender
	queue      chan queuedEvent
	stop       chan struct{}
	done       chan struct{}
	retryDelay time.Duration
	logger     func(string, ...any)
	workerOnce sync.Once
	closeOnce  sync.Once
}

// New loads the 163 configuration below dataDir and starts the delivery worker.
func New(dataDir string) (*Notifier, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("notification data directory is required")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create notification data directory: %w", err)
	}

	notifier := &Notifier{
		path:       filepath.Join(dataDir, configFileName),
		sender:     smtpSender{dialTimeout: 15 * time.Second},
		queue:      make(chan queuedEvent, queueSize),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		retryDelay: 250 * time.Millisecond,
		logger:     log.Printf,
	}
	if err := notifier.load(); err != nil {
		return nil, err
	}
	notifier.start()
	return notifier, nil
}

func newNotifier(path string, config Config, sender Sender) *Notifier {
	if sender == nil {
		sender = SenderFunc(func(config Config, subject, body string) error {
			return (smtpSender{dialTimeout: 15 * time.Second}).Send(config, subject, body)
		})
	}
	notifier := &Notifier{
		path:       path,
		config:     config,
		sender:     sender,
		queue:      make(chan queuedEvent, queueSize),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		retryDelay: time.Millisecond,
		logger:     func(string, ...any) {},
	}
	notifier.start()
	return notifier
}

func (n *Notifier) start() {
	n.workerOnce.Do(func() {
		go n.run()
	})
}

func (n *Notifier) run() {
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

// Close stops future deliveries. In-flight SMTP calls are bounded by the
// dial timeout and are allowed to finish before the worker exits.
func (n *Notifier) Close() {
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

func (n *Notifier) Config() Config {
	if n == nil {
		return Config{}
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.config
}

func (n *Notifier) PublicConfig() PublicConfig {
	config := n.Config()
	return PublicConfig{
		Enabled:        config.Enabled,
		Configured:     isConfigured(config),
		Provider:       Provider163,
		SMTPHost:       SMTPHost,
		SMTPPort:       SMTPPort,
		SenderEmail:    config.SenderEmail,
		RecipientEmail: config.RecipientEmail,
	}
}

// Update validates and atomically persists a new 163-to-QQ configuration.
func (n *Notifier) Update(input UpdateInput) (PublicConfig, error) {
	if n == nil {
		return PublicConfig{}, errors.New("notification service is unavailable")
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return PublicConfig{}, errors.New("notification service is closed")
	}

	next := n.config
	next.Enabled = input.Enabled
	if strings.TrimSpace(input.SenderEmail) != "" {
		next.SenderEmail = strings.TrimSpace(input.SenderEmail)
	}
	if strings.TrimSpace(input.RecipientEmail) != "" {
		next.RecipientEmail = strings.TrimSpace(input.RecipientEmail)
	}
	if strings.TrimSpace(input.AuthorizationCode) != "" {
		next.AuthorizationCode = strings.TrimSpace(input.AuthorizationCode)
	}
	if err := validateConfig(next); err != nil {
		return PublicConfig{}, err
	}
	if err := writeConfigAtomic(n.path, next); err != nil {
		return PublicConfig{}, err
	}
	n.config = next
	return publicConfig(next), nil
}

// Notify queues an event only when email notifications are enabled and complete.
// It returns false when disabled or when the queue is full.
func (n *Notifier) Notify(event Event) bool {
	if n == nil {
		return false
	}
	n.mu.RLock()
	if n.closed || !n.config.Enabled || !isConfigured(n.config) {
		n.mu.RUnlock()
		return false
	}
	queued := queuedEvent{config: n.config, event: event}
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
			n.logger("email notification queue is full; dropping event kind=%s", event.Kind)
		}
		return false
	}
}

// SendTest sends a synchronous test message using the saved configuration.
func (n *Notifier) SendTest() error {
	if n == nil {
		return errors.New("notification service is unavailable")
	}
	config := n.Config()
	if !isConfigured(config) {
		return ErrNotConfigured
	}
	return n.sendWithRetry(config, "iCloud HME email notification test", "163 Mail notification configuration is active.\r\n\r\nThis message was sent by the local iCloud HME service.")
}

func (n *Notifier) deliver(queued queuedEvent) {
	subject, body := eventMessage(queued.event)
	if err := n.sendWithRetry(queued.config, subject, body); err != nil && n.logger != nil {
		n.logger("email notification failed after %d attempts: %v", maxAttempts, err)
	}
}

func (n *Notifier) sendWithRetry(config Config, subject, body string) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := n.sender.Send(config, subject, body); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt+1 < maxAttempts && n.retryDelay > 0 {
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

func (n *Notifier) load() error {
	raw, err := os.ReadFile(n.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read email notification configuration: %w", err)
	}
	var stored storedConfig
	if err := unmarshalStoredConfig(raw, &stored); err != nil {
		return err
	}
	config := Config{
		Enabled:           stored.Enabled,
		SenderEmail:       stored.SenderEmail,
		AuthorizationCode: stored.AuthorizationCode,
		RecipientEmail:    stored.RecipientEmail,
	}
	if err := validateConfig(config); err != nil {
		return fmt.Errorf("invalid email notification configuration: %w", err)
	}
	n.config = config
	return nil
}

func unmarshalStoredConfig(raw []byte, stored *storedConfig) error {
	if err := json.Unmarshal(raw, stored); err != nil {
		return fmt.Errorf("decode email notification configuration: %w", err)
	}
	return nil
}

func validateConfig(config Config) error {
	config.SenderEmail = strings.TrimSpace(config.SenderEmail)
	config.RecipientEmail = strings.TrimSpace(config.RecipientEmail)
	config.AuthorizationCode = strings.TrimSpace(config.AuthorizationCode)
	if config.SenderEmail == "" && config.RecipientEmail == "" && config.AuthorizationCode == "" && !config.Enabled {
		return nil
	}
	if err := validate163SenderEmail(config.SenderEmail); err != nil {
		return fmt.Errorf("163 发件邮箱无效: %w", err)
	}
	if err := validateQQRecipientEmail(config.RecipientEmail); err != nil {
		return fmt.Errorf("QQ 收件邮箱无效: %w", err)
	}
	if config.AuthorizationCode == "" {
		return errors.New("163 邮箱授权码不能为空")
	}
	if config.Enabled && !isConfigured(config) {
		return ErrNotConfigured
	}
	return nil
}

func isConfigured(config Config) bool {
	return config.SenderEmail != "" && config.RecipientEmail != "" && config.AuthorizationCode != ""
}

func publicConfig(config Config) PublicConfig {
	return PublicConfig{
		Enabled:        config.Enabled,
		Configured:     isConfigured(config),
		Provider:       Provider163,
		SMTPHost:       SMTPHost,
		SMTPPort:       SMTPPort,
		SenderEmail:    config.SenderEmail,
		RecipientEmail: config.RecipientEmail,
	}
}

func validate163SenderEmail(value string) error {
	if err := validateEmail(value); err != nil {
		return err
	}
	address := strings.ToLower(strings.TrimSpace(value))
	_, domain, ok := strings.Cut(address, "@")
	if !ok {
		return errors.New("必须使用 163 邮箱地址")
	}
	if domain != "163.com" {
		return errors.New("必须使用 @163.com 地址")
	}
	return nil
}

func validateQQRecipientEmail(value string) error {
	if err := validateEmail(value); err != nil {
		return err
	}
	address := strings.ToLower(strings.TrimSpace(value))
	_, domain, ok := strings.Cut(address, "@")
	if !ok {
		return errors.New("必须使用 QQ 邮箱地址")
	}
	switch domain {
	case "qq.com", "vip.qq.com", "foxmail.com":
		return nil
	default:
		return errors.New("必须使用 @qq.com、@vip.qq.com 或 @foxmail.com 地址")
	}
}

func validateEmail(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 254 {
		return errors.New("邮箱地址不能为空且不能超过 254 个字符")
	}
	parsed, err := netmail.ParseAddress(value)
	if err != nil || parsed.Address != value || parsed.Name != "" {
		return errors.New("邮箱地址格式无效")
	}
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("邮箱地址不能包含换行符")
	}
	return nil
}

func writeConfigAtomic(path string, config Config) (err error) {
	raw, err := json.Marshal(storedConfig{
		Enabled:           config.Enabled,
		SenderEmail:       config.SenderEmail,
		AuthorizationCode: config.AuthorizationCode,
		RecipientEmail:    config.RecipientEmail,
	})
	if err != nil {
		return fmt.Errorf("%w: encode configuration: %v", ErrPersistence, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".email-notification-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: create temporary file: %v", ErrPersistence, err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(0600); err != nil {
		return fmt.Errorf("%w: set temporary file permissions: %v", ErrPersistence, err)
	}
	if _, err = temporary.Write(raw); err != nil {
		return fmt.Errorf("%w: write configuration: %v", ErrPersistence, err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("%w: sync configuration: %v", ErrPersistence, err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("%w: close configuration: %v", ErrPersistence, err)
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("%w: replace configuration: %v", ErrPersistence, err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("%w: set configuration permissions: %v", ErrPersistence, err)
	}
	return nil
}

func eventMessage(event Event) (string, string) {
	kind := event.Kind
	if kind == "" {
		kind = "alias_automation"
	}
	status := event.Status
	if status == "" {
		status = "unknown"
	}
	body := strings.Builder{}
	body.WriteString("iCloud HME 自动化通知\r\n\r\n")
	fmt.Fprintf(&body, "事件: %s\r\n", kind)
	fmt.Fprintf(&body, "账户: %s\r\n", redactAccountID(event.AccountID))
	fmt.Fprintf(&body, "触发方式: %s\r\n", statusValue(event.Trigger))
	fmt.Fprintf(&body, "状态: %s\r\n", status)
	fmt.Fprintf(&body, "请求创建: %d\r\n", event.Requested)
	fmt.Fprintf(&body, "成功创建: %d\r\n", event.Created)
	fmt.Fprintf(&body, "失败数量: %d\r\n", event.Failed)
	fmt.Fprintf(&body, "是否完成: %t\r\n", event.Complete)
	if event.PauseReason != "" {
		fmt.Fprintf(&body, "暂停原因: %s\r\n", event.PauseReason)
	}
	if event.Error != "" {
		fmt.Fprintf(&body, "错误摘要: %s\r\n", oneLine(event.Error))
	}
	return "iCloud HME 自动化通知", body.String()
}

func redactAccountID(value string) string {
	value = oneLine(value)
	if len(value) <= 8 {
		return "***"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func statusValue(value string) string {
	if value == "" {
		return "unknown"
	}
	return oneLine(value)
}

func oneLine(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func (s smtpSender) Send(config Config, subject, body string) error {
	if s.dialTimeout <= 0 {
		s.dialTimeout = 15 * time.Second
	}
	address := net.JoinHostPort(SMTPHost, fmt.Sprintf("%d", SMTPPort))
	dialer := net.Dialer{Timeout: s.dialTimeout}
	connection, err := tls.DialWithDialer(&dialer, "tcp", address, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: SMTPHost,
	})
	if err != nil {
		return fmt.Errorf("connect 163 SMTP: %w", err)
	}
	client, err := smtp.NewClient(connection, SMTPHost)
	if err != nil {
		_ = connection.Close()
		return fmt.Errorf("create 163 SMTP client: %w", err)
	}
	defer client.Close()

	if err := client.Auth(smtp.PlainAuth("", config.SenderEmail, config.AuthorizationCode, SMTPHost)); err != nil {
		return fmt.Errorf("authenticate 163 SMTP: %w", err)
	}
	if err := client.Mail(config.SenderEmail); err != nil {
		return fmt.Errorf("set 163 SMTP sender: %w", err)
	}
	if err := client.Rcpt(config.RecipientEmail); err != nil {
		return fmt.Errorf("set QQ SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open 163 SMTP message: %w", err)
	}
	message := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n",
		config.SenderEmail,
		config.RecipientEmail,
		mime.QEncoding.Encode("UTF-8", subject),
		strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"),
	)
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write 163 SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish QQ SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("close QQ SMTP session: %w", err)
	}
	return nil
}
