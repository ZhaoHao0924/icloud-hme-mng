// Package account 实现多账号管理器。
//
// 负责账号 CRUD、Cookie 解析(Header String / JSON)、持久化到 accounts.json,
// 以及创建 HME 客户端和邮件客户端。对应原 Python 项目 account_manager.py。
package account

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	netmail "net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"icloud-hme/internal/hme"
	"icloud-hme/internal/mail"
)

// Account 描述一个 iCloud 账号。
type Account struct {
	ID                   string                 `json:"id"`
	Name                 string                 `json:"name"`
	RealEmail            string                 `json:"real_email"`
	ICloudEmail          string                 `json:"icloud_email"`
	Cookies              map[string]string      `json:"cookies"`
	Host                 string                 `json:"host"`
	Proxy                string                 `json:"proxy,omitempty"` // HTTP/SOCKS5 代理
	AppPassword          string                 `json:"app_password,omitempty"`
	Status               string                 `json:"status"` // pending / active / error
	AliasTotal           int                    `json:"alias_total"`
	AliasActive          int                    `json:"alias_active"`
	LastValidated        string                 `json:"last_validated"`
	LastError            string                 `json:"last_error,omitempty"`
	CreatedAt            string                 `json:"created_at"`
	AliasAutomation      *AliasAutomation       `json:"alias_automation,omitempty"`
	AliasCreationHistory []AliasCreationHistory `json:"alias_creation_history,omitempty"`
}

type accountConfig struct {
	Accounts  []*Account `json:"accounts"`
	UpdatedAt string     `json:"updated_at,omitempty"`
}

var (
	// ErrPersistence 表示账户配置未能安全写入磁盘。
	ErrPersistence = errors.New("账户配置持久化失败")
	// ErrAccountNotFound 表示登录目标账户不存在。
	ErrAccountNotFound = errors.New("账号不存在")
	// ErrLoginEmailMissing 表示账户尚未配置可用于 Apple 登录的邮箱。
	ErrLoginEmailMissing = errors.New("账号未设置邮箱地址")
	// ErrLoginSessionInvalid 表示两阶段登录会话已消费或被关闭。
	ErrLoginSessionInvalid = errors.New("登录会话无效或已使用")
)

// MaxCookieCount 是单个账户允许保存的最大 Cookie 数量。
const MaxCookieCount = 128

type passwordLoginClient interface {
	StartLogin(username, password string) (*hme.LoginChallenge, error)
	VerifyLogin(challenge *hme.LoginChallenge, otp string) error
	Validate() (bool, error)
	SessionCookies() map[string]string
	Close()
}

type passwordLoginClientFactory func(cookies map[string]string, host, proxy string, verbose bool) (passwordLoginClient, error)

func defaultPasswordLoginClientFactory(cookies map[string]string, host, proxy string, verbose bool) (passwordLoginClient, error) {
	return hme.NewClient(cookies, host, proxy, verbose)
}

// Manager 管理多个 iCloud 账号,线程安全。
type Manager struct {
	mu                     sync.RWMutex
	accounts               map[string]*Account
	dataDir                string
	dataFile               string
	newPasswordLoginClient passwordLoginClientFactory
}

// NewManager 创建管理器。dataDir 用于存放 accounts.json。
func NewManager(dataDir string) (*Manager, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	m := &Manager{
		accounts:               make(map[string]*Account),
		dataDir:                dataDir,
		dataFile:               filepath.Join(dataDir, "accounts.json"),
		newPasswordLoginClient: defaultPasswordLoginClientFactory,
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// DataDir returns the manager-owned local data directory.
func (m *Manager) DataDir() string {
	return m.dataDir
}

// Reload 重新加载 accounts.json 配置文件。
func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.load()
}

// ConfigAvailable 检查数据目录和磁盘配置是否仍可读取并满足账户 schema。
// 配置文件尚未创建时表示空配置可用；具体路径和解析错误不会暴露给调用方。
func (m *Manager) ConfigAvailable() bool {
	m.mu.RLock()
	dataDir := m.dataDir
	dataFile := m.dataFile
	m.mu.RUnlock()

	info, err := os.Stat(dataDir)
	if err != nil || !info.IsDir() {
		return false
	}
	raw, err := os.ReadFile(dataFile)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	_, err = decodeAccountConfig(raw)
	return err == nil
}

func (m *Manager) load() error {
	raw, err := os.ReadFile(m.dataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	accounts, err := decodeAccountConfig(raw)
	if err != nil {
		return err
	}
	m.accounts = accounts
	return nil
}

func (m *Manager) save() error {
	accounts := make([]*Account, 0, len(m.accounts))
	for _, acc := range m.accounts {
		accounts = append(accounts, acc)
	}
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].ID < accounts[j].ID
	})

	wrapper := accountConfig{
		Accounts:  accounts,
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
	raw, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: 序列化配置: %w", ErrPersistence, err)
	}
	if err := writeFileAtomic(m.dataFile, raw); err != nil {
		return fmt.Errorf("%w: %w", ErrPersistence, err)
	}
	return nil
}

func writeFileAtomic(filename string, raw []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(filename), "."+filepath.Base(filename)+".*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时配置文件: %w", err)
	}
	tmpName := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := tmp.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("关闭临时配置文件: %w", closeErr))
			}
		}
		if err != nil {
			if removeErr := os.Remove(tmpName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("清理临时配置文件: %w", removeErr))
			}
		}
	}()

	if err = tmp.Chmod(0600); err != nil {
		return fmt.Errorf("设置临时配置文件权限: %w", err)
	}
	if _, err = tmp.Write(raw); err != nil {
		return fmt.Errorf("写入临时配置文件: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("同步临时配置文件: %w", err)
	}
	if err = tmp.Close(); err != nil {
		closed = true
		return fmt.Errorf("关闭临时配置文件: %w", err)
	}
	closed = true
	if err = os.Rename(tmpName, filename); err != nil {
		return fmt.Errorf("替换账户配置: %w", err)
	}
	return nil
}

func decodeAccountConfig(raw []byte) (map[string]*Account, error) {
	var wrapper struct {
		Accounts json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("解析账户配置: %w", err)
	}

	encodedAccounts := bytes.TrimSpace(wrapper.Accounts)
	if len(encodedAccounts) == 0 || string(encodedAccounts) == "null" {
		return make(map[string]*Account), nil
	}

	var accounts []*Account
	switch encodedAccounts[0] {
	case '[':
		if err := json.Unmarshal(encodedAccounts, &accounts); err != nil {
			return nil, fmt.Errorf("解析账户配置 accounts: %w", err)
		}
	case '{':
		// 兼容旧版本写出的 {"accounts":{"acc_id":{...}}} 格式。
		var legacy map[string]*Account
		if err := json.Unmarshal(encodedAccounts, &legacy); err != nil {
			return nil, fmt.Errorf("解析旧版账户配置 accounts: %w", err)
		}
		accounts = make([]*Account, 0, len(legacy))
		for id, acc := range legacy {
			if acc != nil && strings.TrimSpace(acc.ID) == "" {
				acc.ID = id
			}
			accounts = append(accounts, acc)
		}
	default:
		return nil, fmt.Errorf("解析账户配置 accounts: 必须是数组")
	}

	byID := make(map[string]*Account, len(accounts))
	for i, acc := range accounts {
		if acc == nil {
			return nil, fmt.Errorf("解析账户配置 accounts[%d]: 账户不能为空", i)
		}
		acc.ID = strings.TrimSpace(acc.ID)
		if acc.ID == "" {
			return nil, fmt.Errorf("解析账户配置 accounts[%d]: id 不能为空", i)
		}
		if _, exists := byID[acc.ID]; exists {
			return nil, fmt.Errorf("解析账户配置 accounts[%d]: id %q 重复", i, acc.ID)
		}
		acc.Name = strings.TrimSpace(acc.Name)
		if acc.Name == "" {
			return nil, fmt.Errorf("解析账户配置 accounts[%d]: name 不能为空", i)
		}
		acc.Host = strings.TrimSpace(acc.Host)
		if acc.Host == "" {
			acc.Host = "icloud.com"
		}
		if acc.Status == "" {
			acc.Status = "pending"
		}
		if acc.Status != "pending" && acc.Status != "active" && acc.Status != "error" {
			return nil, fmt.Errorf("解析账户配置 accounts[%d]: status %q 无效", i, acc.Status)
		}
		if acc.Cookies == nil {
			acc.Cookies = make(map[string]string)
		}
		if err := validateCookieCount(acc.Cookies); err != nil {
			return nil, fmt.Errorf("解析账户配置 accounts[%d]: %w", i, err)
		}
		if acc.AliasAutomation != nil {
			automation, err := normalizeStoredAliasAutomation(*acc.AliasAutomation)
			if err != nil {
				return nil, fmt.Errorf("解析账户配置 accounts[%d] alias_automation: %w", i, err)
			}
			acc.AliasAutomation = &automation
		}
		history, err := normalizeStoredAliasCreationHistory(acc.AliasCreationHistory)
		if err != nil {
			return nil, fmt.Errorf("解析账户配置 accounts[%d] alias_creation_history: %w", i, err)
		}
		acc.AliasCreationHistory = history
		byID[acc.ID] = acc
	}
	return byID, nil
}

func cloneCookies(cookies map[string]string) map[string]string {
	cloned := make(map[string]string, len(cookies))
	for name, value := range cookies {
		cloned[name] = value
	}
	return cloned
}

func validateCookieCount(cookies map[string]string) error {
	if len(cookies) > MaxCookieCount {
		return fmt.Errorf("Cookie 数量不能超过 %d", MaxCookieCount)
	}
	return nil
}

func cloneAccount(acc *Account) *Account {
	if acc == nil {
		return nil
	}
	cloned := *acc
	cloned.Cookies = cloneCookies(acc.Cookies)
	cloned.AliasAutomation = cloneAliasAutomation(acc.AliasAutomation)
	cloned.AliasCreationHistory = cloneAliasCreationHistory(acc.AliasCreationHistory)
	return &cloned
}

func (m *Manager) accountSnapshot(id string) (*Account, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	acc, ok := m.accounts[id]
	if !ok {
		return nil, false
	}
	return cloneAccount(acc), true
}

// ParseCookieInput 解析 Cookie 输入,支持两种格式:
//   - Header String: "name1=value1; name2=value2; ..."
//   - JSON: {"name1":"value1","name2":"value2"}
//
// 空输入返回错误。
func ParseCookieInput(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("空白输入 — 请粘贴 Cookie Header String 或 JSON")
	}

	// JSON 格式
	if strings.HasPrefix(raw, "{") {
		var cookies map[string]string
		if err := json.Unmarshal([]byte(raw), &cookies); err == nil && cookies != nil {
			out := make(map[string]string, len(cookies))
			for k, v := range cookies {
				if v != "" {
					out[k] = v
				}
			}
			if len(out) > 0 {
				if err := validateCookieCount(out); err != nil {
					return nil, err
				}
				return out, nil
			}
		}
	}

	// Header String 格式
	cookies := make(map[string]string)
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		idx := strings.Index(part, "=")
		if idx <= 0 {
			continue
		}
		name := strings.TrimSpace(part[:idx])
		value := strings.TrimSpace(part[idx+1:])
		if name != "" {
			cookies[name] = value
		}
	}
	if len(cookies) == 0 {
		return nil, fmt.Errorf("无法解析 Cookie 输入,请提供 Header String 或 JSON 格式")
	}
	if err := validateCookieCount(cookies); err != nil {
		return nil, err
	}
	return cookies, nil
}

// AddAccount 添加一个账号。cookieInput 可为空,后续可通过 /login 获取。
//
// cookieInput 支持 Header String 或 JSON。校验失败仍会保存账号(status=error),
// 方便用户后续修正 Cookie 后重新校验。
func (m *Manager) AddAccount(name, icloudEmail, cookieInput, host, proxy string) (AccountDTO, error) {
	normalizedEmail, err := normalizeICloudEmail(icloudEmail)
	if err != nil {
		return AccountDTO{}, err
	}

	var cookies map[string]string
	if cookieInput != "" {
		var err error
		cookies, err = ParseCookieInput(cookieInput)
		if err != nil {
			return AccountDTO{}, err
		}
	} else {
		cookies = make(map[string]string)
	}
	if host == "" {
		host = "icloud.com"
	}

	acc := &Account{
		ID:          "acc_" + uuid.New().String()[:8],
		Name:        name,
		ICloudEmail: normalizedEmail,
		Cookies:     cookies,
		Host:        host,
		Proxy:       proxy,
		Status:      "pending", // 无 Cookie 时为 pending
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	// 有 Cookie 才校验会话
	if len(cookies) > 0 {
		client, err := hme.NewClient(cookies, host, proxy, false)
		if err != nil {
			return AccountDTO{}, safeAccountOperationError(err, accountSensitiveValues(acc)...)
		}
		if err := client.ValidateSession(); err != nil {
			acc.Status = "error"
			acc.LastError = truncate(err.Error(), 300)
		} else {
			acc.Status = "active"
			if info := client.AccountInfo(); info != nil {
				acc.RealEmail = firstNonEmpty(info.AppleID, info.PrimaryEmail)
				if acc.ICloudEmail == "" {
					acc.ICloudEmail = deriveICloudEmail(info)
				}
			}
			if aliases, err := client.ListAliases(); err == nil {
				acc.AliasTotal = len(aliases)
				for _, a := range aliases {
					if a.Active {
						acc.AliasActive++
					}
				}
			}
			acc.LastValidated = time.Now().Format(time.RFC3339)
		}
		if err := validateCookieCount(client.Cookies); err != nil {
			return AccountDTO{}, err
		}
		acc.Cookies = cloneCookies(client.Cookies)
	}

	m.mu.Lock()
	m.accounts[acc.ID] = acc
	if err := m.save(); err != nil {
		delete(m.accounts, acc.ID)
		m.mu.Unlock()
		return AccountDTO{}, err
	}
	dto := newAccountDTO(acc)
	m.mu.Unlock()
	return dto, nil
}

// RemoveAccount 删除账号。
func (m *Manager) RemoveAccount(id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return false, nil
	}
	delete(m.accounts, id)
	if err := m.save(); err != nil {
		m.accounts[id] = acc
		return false, err
	}
	return true, nil
}

// ListAccounts 返回所有账号的脱敏摘要。
func (m *Manager) ListAccounts() []AccountDTO {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]AccountDTO, 0, len(m.accounts))
	for _, acc := range m.accounts {
		out = append(out, newAccountDTO(acc))
	}
	return out
}

// HMEClient 为指定账号创建一个新的 HME 客户端。
// 必须有有效的 Cookie 才能使用 HME 功能。
func (m *Manager) HMEClient(id string, verbose bool) (*hme.Client, error) {
	acc, ok := m.accountSnapshot(id)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	if len(acc.Cookies) == 0 {
		return nil, fmt.Errorf("账号未配置 Cookie，无法使用 HME 功能")
	}
	return hme.NewClient(acc.Cookies, acc.Host, acc.Proxy, verbose)
}

// PasswordLoginSession 保存等待 OTP 的 Apple 登录会话。它不持有账号密码，
// 只能验证一次；调用方在过期或放弃时应调用 Close。
type PasswordLoginSession struct {
	mu        sync.Mutex
	manager   *Manager
	accountID string
	client    passwordLoginClient
	challenge *hme.LoginChallenge
	consumed  bool
}

// StartPasswordLogin 执行密码和 SRP 阶段。无需 2FA 时直接返回账户摘要；
// 需要 2FA 时返回 PasswordLoginSession，账户摘要为零值。
func (m *Manager) StartPasswordLogin(id, password string) (AccountDTO, *PasswordLoginSession, error) {
	acc, ok := m.accountSnapshot(id)
	secrets := accountSensitiveValues(acc)
	if !ok {
		return AccountDTO{}, nil, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}

	email := accountLoginEmail(acc)
	if email == "" {
		return AccountDTO{}, nil, ErrLoginEmailMissing
	}

	factory := m.newPasswordLoginClient
	if factory == nil {
		factory = defaultPasswordLoginClientFactory
	}
	client, err := factory(nil, acc.Host, acc.Proxy, true)
	if err != nil {
		return AccountDTO{}, nil, safeAccountOperationError(err, append(secrets, password)...)
	}

	challenge, err := client.StartLogin(email, password)
	if err != nil {
		client.Close()
		return AccountDTO{}, nil, safeAccountOperationError(err, append(secrets, password)...)
	}
	if challenge != nil {
		return AccountDTO{}, &PasswordLoginSession{
			manager:   m,
			accountID: id,
			client:    client,
			challenge: challenge,
		}, nil
	}

	defer client.Close()
	dto, err := m.persistPasswordLogin(id, client)
	return dto, nil, safeAccountOperationError(err, append(secrets, password)...)
}

// Verify 提交 OTP 并原子保存登录生成的 Cookie。
func (s *PasswordLoginSession) Verify(otp string) (AccountDTO, error) {
	if s == nil {
		return AccountDTO{}, ErrLoginSessionInvalid
	}
	s.mu.Lock()
	if s.consumed || s.client == nil || s.challenge == nil || s.manager == nil {
		s.mu.Unlock()
		return AccountDTO{}, ErrLoginSessionInvalid
	}
	manager := s.manager
	accountID := s.accountID
	client := s.client
	challenge := s.challenge
	s.consumed = true
	s.manager = nil
	s.client = nil
	s.challenge = nil
	s.mu.Unlock()
	defer client.Close()

	acc, ok := manager.accountSnapshot(accountID)
	if !ok {
		return AccountDTO{}, fmt.Errorf("%w: %s", ErrAccountNotFound, accountID)
	}
	if err := client.VerifyLogin(challenge, otp); err != nil {
		secrets := append(accountSensitiveValues(acc), otp)
		return AccountDTO{}, safeAccountOperationError(err, secrets...)
	}
	dto, err := manager.persistPasswordLogin(accountID, client)
	return dto, safeAccountOperationError(err, append(accountSensitiveValues(acc), otp)...)
}

// Close 放弃登录会话并释放连接。重复调用是安全的。
func (s *PasswordLoginSession) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	client := s.client
	s.consumed = true
	s.manager = nil
	s.client = nil
	s.challenge = nil
	s.mu.Unlock()
	if client != nil {
		client.Close()
	}
}

func (m *Manager) persistPasswordLogin(id string, client passwordLoginClient) (AccountDTO, error) {
	valid, err := client.Validate()
	if err != nil {
		return AccountDTO{}, fmt.Errorf("登录会话校验失败: %w", err)
	}
	if !valid {
		return AccountDTO{}, errors.New("登录会话校验失败")
	}
	cookies := client.SessionCookies()
	if err := validateCookieCount(cookies); err != nil {
		return AccountDTO{}, err
	}

	m.mu.Lock()
	current, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return AccountDTO{}, fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	previousCookies := current.Cookies
	previousStatus := current.Status
	previousLastError := current.LastError
	previousLastValidated := current.LastValidated
	current.Cookies = cloneCookies(cookies)
	current.Status = "active"
	current.LastError = ""
	current.LastValidated = time.Now().Format(time.RFC3339)
	if err := m.save(); err != nil {
		current.Cookies = previousCookies
		current.Status = previousStatus
		current.LastError = previousLastError
		current.LastValidated = previousLastValidated
		m.mu.Unlock()
		return AccountDTO{}, err
	}
	dto := newAccountDTO(current)
	m.mu.Unlock()
	return dto, nil
}

// LoginWithPassword 保留单次调用兼容入口；新 HTTP API 使用 StartPasswordLogin
// 和 PasswordLoginSession.Verify 暴露可交互的两阶段流程。
func (m *Manager) LoginWithPassword(id, password string, otpProvider hme.OTPProvider) (AccountDTO, error) {
	dto, session, err := m.StartPasswordLogin(id, password)
	if err != nil || session == nil {
		return dto, err
	}
	if otpProvider == nil {
		session.Close()
		return AccountDTO{}, hme.ErrOTPRequired
	}
	otp, err := otpProvider()
	if err != nil {
		session.Close()
		return AccountDTO{}, fmt.Errorf("获取 2FA 验证码失败: %w", err)
	}
	return session.Verify(otp)
}

// MailClient 为指定账号创建 IMAP 邮件客户端。
// 需要事先设置 iCloud 邮箱和 App 专用密码。
func (m *Manager) MailClient(id string) (*mail.Client, error) {
	acc, ok := m.accountSnapshot(id)
	if !ok {
		return nil, fmt.Errorf("账号不存在: %s", id)
	}
	imapEmail := acc.ICloudEmail
	if imapEmail == "" {
		imapEmail = acc.RealEmail
	}
	if !isICloudDomain(imapEmail) {
		return nil, fmt.Errorf("账号未设置 iCloud 邮箱 (当前: %s)", imapEmail)
	}
	if acc.AppPassword == "" {
		return nil, fmt.Errorf("账号未设置 App 专用密码")
	}
	return mail.NewClient(imapEmail, acc.AppPassword), nil
}

// WebMailClient 为指定账号创建 Web 邮件客户端。
// 使用 Cookie 认证，无需 App Password。
func (m *Manager) WebMailClient(id string) (*mail.WebClient, error) {
	acc, ok := m.accountSnapshot(id)
	if !ok {
		return nil, fmt.Errorf("账号不存在: %s", id)
	}
	if len(acc.Cookies) == 0 {
		return nil, fmt.Errorf("账号未配置 Cookie，无法读取邮件")
	}
	// 从 cookies 中获取 dsid
	dsid := ""
	if v, ok := acc.Cookies["X-APPLE-WEBAUTH-USER"]; ok {
		// 解析 "v=1:s=1:d=22789132008" 格式
		parts := strings.Split(v, ":d=")
		if len(parts) == 2 {
			dsid = parts[1]
		}
	}
	return mail.NewWebClient(acc.Cookies, dsid, acc.Host), nil
}

// SetAppPassword 设置 iCloud 邮箱和 App 专用密码,并测试 IMAP 连接。
func (m *Manager) SetAppPassword(id, icloudEmail, appPassword string) (AccountDTO, error) {
	acc, ok := m.accountSnapshot(id)
	secrets := accountSensitiveValues(acc)
	if !ok {
		return AccountDTO{}, fmt.Errorf("账号不存在: %s", id)
	}
	if icloudEmail == "" {
		return AccountDTO{}, fmt.Errorf("iCloud 邮箱不能为空")
	}
	if appPassword == "" {
		return AccountDTO{}, fmt.Errorf("App 专用密码不能为空")
	}

	// 测试连接
	mc := mail.NewClient(icloudEmail, appPassword)
	if err := mc.Connect(); err != nil {
		return AccountDTO{}, safeAccountOperationError(err, append(secrets, appPassword)...)
	}
	_, err := mc.InboxCount()
	mc.Disconnect()
	if err != nil {
		return AccountDTO{}, safeAccountOperationError(err, append(secrets, appPassword)...)
	}

	m.mu.Lock()
	current, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return AccountDTO{}, fmt.Errorf("账号不存在: %s", id)
	}
	previousEmail := current.ICloudEmail
	previousPassword := current.AppPassword
	current.ICloudEmail = icloudEmail
	current.AppPassword = appPassword
	if err := m.save(); err != nil {
		current.ICloudEmail = previousEmail
		current.AppPassword = previousPassword
		m.mu.Unlock()
		return AccountDTO{}, err
	}
	dto := newAccountDTO(current)
	m.mu.Unlock()
	return dto, nil
}

// SaveCookies 保存指定账号的最新 Cookie（HMEClient 操作后刷新的 token）。
// 用于客户端 validate/操作过程中从 Set-Cookie 获取了新 token 后持久化。
func (m *Manager) SaveCookies(id string, cookies map[string]string) error {
	if err := validateCookieCount(cookies); err != nil {
		return err
	}
	clonedCookies := cloneCookies(cookies)
	m.mu.Lock()
	defer m.mu.Unlock()
	acc, ok := m.accounts[id]
	if !ok {
		return fmt.Errorf("账号不存在: %s", id)
	}
	previousCookies := acc.Cookies
	acc.Cookies = clonedCookies
	if err := m.save(); err != nil {
		acc.Cookies = previousCookies
		return err
	}
	return nil
}

// UpdateCookies 更新指定账号的 Cookie,并自动校验会话有效性。
func (m *Manager) UpdateCookies(id string, cookies map[string]string) (AccountDTO, error) {
	if len(cookies) == 0 {
		return AccountDTO{}, fmt.Errorf("cookies 不能为空")
	}
	if err := validateCookieCount(cookies); err != nil {
		return AccountDTO{}, err
	}
	acc, ok := m.accountSnapshot(id)
	if !ok {
		return AccountDTO{}, fmt.Errorf("账号不存在: %s", id)
	}

	// 自动校验 Cookie 是否有效
	acc.Cookies = cloneCookies(cookies)
	if acc.Host == "" {
		acc.Host = "icloud.com"
	}
	client, err := hme.NewClient(acc.Cookies, acc.Host, acc.Proxy, false)
	if err != nil {
		acc.Status = "error"
		acc.LastError = "创建客户端失败: " + err.Error()
		secrets := accountSensitiveValues(acc)
		if _, saveErr := m.commitCookieValidation(id, acc); saveErr != nil {
			return AccountDTO{}, saveErr
		}
		return AccountDTO{}, safeAccountOperationError(err, secrets...)
	}
	if err := client.ValidateSession(); err != nil {
		acc.Status = "error"
		acc.LastError = "Cookie 校验失败: " + err.Error()
	} else {
		acc.Status = "active"
		acc.LastValidated = time.Now().Format(time.RFC3339)
		acc.LastError = ""
		if info := client.AccountInfo(); info != nil {
			acc.RealEmail = firstNonEmpty(info.AppleID, info.PrimaryEmail)
			if acc.ICloudEmail == "" {
				acc.ICloudEmail = deriveICloudEmail(info)
			}
		}
	}
	acc.Cookies = cloneCookies(client.Cookies)
	return m.commitCookieValidation(id, acc)
}

func (m *Manager) commitCookieValidation(id string, result *Account) (AccountDTO, error) {
	if err := validateCookieCount(result.Cookies); err != nil {
		return AccountDTO{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.accounts[id]
	if !ok {
		return AccountDTO{}, fmt.Errorf("账号不存在: %s", id)
	}
	previous := cloneAccount(current)
	current.Cookies = cloneCookies(result.Cookies)
	current.Status = result.Status
	current.LastValidated = result.LastValidated
	current.LastError = result.LastError
	current.RealEmail = result.RealEmail
	if current.ICloudEmail == "" && result.ICloudEmail != "" {
		current.ICloudEmail = result.ICloudEmail
	}
	if err := m.save(); err != nil {
		m.accounts[id] = previous
		return AccountDTO{}, err
	}
	dto := newAccountDTO(current)
	return dto, nil
}

// ---- 辅助函数 ----

// deriveICloudEmail 从账号身份推导 iCloud 邮箱地址(用于 IMAP 登录)。
//
// 规则:
//  1. primaryEmail 是 @icloud.com/@me.com/@mac.com → 直接用
//  2. appleId 是上述域名 → 直接用
//  3. appleId 是第三方邮箱(如 @qq.com) → 取 local part 拼 @icloud.com
func deriveICloudEmail(info *hme.AccountInfo) string {
	primary := strings.TrimSpace(info.PrimaryEmail)
	appleID := strings.TrimSpace(info.AppleID)

	if normalized, err := normalizeICloudEmail(primary); err == nil && normalized != "" {
		return normalized
	}
	if normalized, err := normalizeICloudEmail(appleID); err == nil && normalized != "" {
		return normalized
	}
	if strings.Contains(appleID, "@") {
		local := strings.SplitN(appleID, "@", 2)[0]
		return local + "@icloud.com"
	}
	return firstNonEmpty(primary, appleID)
}

func isICloudDomain(email string) bool {
	normalized, err := normalizeICloudEmail(email)
	return err == nil && normalized != ""
}

func normalizeICloudEmail(raw string) (string, error) {
	email := strings.TrimSpace(raw)
	if email == "" {
		return "", nil
	}

	address, err := netmail.ParseAddress(email)
	if err != nil || address.Name != "" || address.Address != email {
		return "", fmt.Errorf("icloud_email 必须是有效邮箱地址")
	}
	at := strings.LastIndexByte(address.Address, '@')
	if at <= 0 || at == len(address.Address)-1 {
		return "", fmt.Errorf("icloud_email 必须是有效邮箱地址")
	}
	domain := strings.ToLower(address.Address[at+1:])
	switch domain {
	case "icloud.com", "me.com", "mac.com":
		return address.Address[:at] + "@" + domain, nil
	default:
		return "", fmt.Errorf("icloud_email 必须使用 @icloud.com、@me.com 或 @mac.com")
	}
}

func accountLoginEmail(acc *Account) string {
	if acc == nil {
		return ""
	}
	return firstNonEmpty(strings.TrimSpace(acc.ICloudEmail), strings.TrimSpace(acc.RealEmail))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
