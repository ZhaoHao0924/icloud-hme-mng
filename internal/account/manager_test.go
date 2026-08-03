package account

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"icloud-hme/internal/hme"
)

func TestManagerLoadsCanonicalAccountConfig(t *testing.T) {
	dataDir := t.TempDir()
	writeAccountConfig(t, dataDir, `{
  "accounts": [
    {
      "id": "acc_1",
      "name": "主号",
      "real_email": "user@example.com",
      "icloud_email": "user@icloud.com",
      "host": "icloud.com",
      "cookies": {"session": "cookie-value"},
      "proxy": "http://proxy.example:8080",
      "app_password": "xxxx-xxxx-xxxx-xxxx",
      "status": "active",
      "alias_total": 10,
      "alias_active": 8,
      "last_validated": "2026-07-31T10:00:00+08:00",
      "last_error": "",
      "created_at": "2026-07-31T09:00:00+08:00"
    }
  ],
  "updated_at": "2026-07-31T10:00:00+08:00"
}`)

	mgr, err := NewManager(dataDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	acc := mgr.accounts["acc_1"]
	if acc == nil {
		t.Fatal("canonical account acc_1 was not loaded")
	}
	if acc.ICloudEmail != "user@icloud.com" {
		t.Errorf("ICloudEmail = %q, want user@icloud.com", acc.ICloudEmail)
	}
	if acc.Cookies["session"] != "cookie-value" {
		t.Errorf("session cookie = %q, want cookie-value", acc.Cookies["session"])
	}
	if acc.AppPassword != "xxxx-xxxx-xxxx-xxxx" {
		t.Errorf("AppPassword = %q, want configured password", acc.AppPassword)
	}
}

func TestConfigAvailable(t *testing.T) {
	t.Run("empty configuration", func(t *testing.T) {
		mgr, err := NewManager(t.TempDir())
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}
		if !mgr.ConfigAvailable() {
			t.Error("ConfigAvailable() = false, want true for an empty data directory")
		}
	})

	t.Run("valid configuration", func(t *testing.T) {
		mgr := newManagerWithCookies(t)
		if !mgr.ConfigAvailable() {
			t.Error("ConfigAvailable() = false, want true for valid configuration")
		}
	})

	t.Run("invalid on-disk configuration", func(t *testing.T) {
		dataDir := t.TempDir()
		mgr, err := NewManager(dataDir)
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, "accounts.json"), []byte(`{"accounts":[`), 0600); err != nil {
			t.Fatalf("write invalid configuration: %v", err)
		}
		if mgr.ConfigAvailable() {
			t.Error("ConfigAvailable() = true, want false for invalid configuration")
		}
	})

	t.Run("configuration target is a directory", func(t *testing.T) {
		dataDir := t.TempDir()
		mgr, err := NewManager(dataDir)
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}
		if err := os.Mkdir(filepath.Join(dataDir, "accounts.json"), 0700); err != nil {
			t.Fatalf("create invalid configuration target: %v", err)
		}
		if mgr.ConfigAvailable() {
			t.Error("ConfigAvailable() = true, want false for unreadable target")
		}
	})
}

func TestManagerLoadsLegacyAccountMapAndSavesCanonicalArray(t *testing.T) {
	dataDir := t.TempDir()
	writeAccountConfig(t, dataDir, `{
  "accounts": {
    "acc_legacy": {
      "name": "旧配置",
      "host": "icloud.com",
      "cookies": {}
    }
  }
}`)

	mgr, err := NewManager(dataDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if acc := mgr.accounts["acc_legacy"]; acc == nil || acc.ID != "acc_legacy" {
		t.Fatalf("legacy account = %#v, want ID acc_legacy", acc)
	}
	if got := mgr.accounts["acc_legacy"].Status; got != "pending" {
		t.Errorf("legacy account status = %q, want pending", got)
	}
	if err := mgr.save(); err != nil {
		t.Fatalf("save() error = %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir, "accounts.json"))
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	var wrapper struct {
		Accounts json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	if accounts := bytes.TrimSpace(wrapper.Accounts); len(accounts) == 0 || accounts[0] != '[' {
		t.Fatalf("saved accounts = %s, want canonical array", wrapper.Accounts)
	}
}

func TestManagerSaveSortsCanonicalAccountsByID(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mgr.accounts = map[string]*Account{
		"acc_z": {ID: "acc_z", Name: "后创建"},
		"acc_a": {ID: "acc_a", Name: "先写出"},
	}
	if err := mgr.save(); err != nil {
		t.Fatalf("save() error = %v", err)
	}

	raw, err := os.ReadFile(mgr.dataFile)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	var config accountConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	if len(config.Accounts) != 2 {
		t.Fatalf("account count = %d, want 2", len(config.Accounts))
	}
	if config.Accounts[0].ID != "acc_a" || config.Accounts[1].ID != "acc_z" {
		t.Errorf("account order = [%s, %s], want [acc_a, acc_z]", config.Accounts[0].ID, config.Accounts[1].ID)
	}
	if config.UpdatedAt == "" {
		t.Error("updated_at is empty")
	}
}

func TestManagerRejectsDuplicateCanonicalAccountIDs(t *testing.T) {
	dataDir := t.TempDir()
	writeAccountConfig(t, dataDir, `{
  "accounts": [
    {"id": "acc_duplicate", "name": "账号一"},
    {"id": "acc_duplicate", "name": "账号二"}
  ]
}`)

	_, err := NewManager(dataDir)
	if err == nil {
		t.Fatal("NewManager() error = nil, want duplicate ID error")
	}
	if !strings.Contains(err.Error(), "id \"acc_duplicate\" 重复") {
		t.Fatalf("NewManager() error = %q, want duplicate ID detail", err)
	}
}

func TestAddPendingAccountPersistsICloudEmail(t *testing.T) {
	dataDir := t.TempDir()
	mgr, err := NewManager(dataDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	dto, err := mgr.AddAccount("待登录账号", "  User.Name@ICLOUD.COM  ", "", "", "")
	if err != nil {
		t.Fatalf("AddAccount() error = %v", err)
	}
	if dto.ICloudEmail != "User.Name@icloud.com" {
		t.Errorf("ICloudEmail = %q, want User.Name@icloud.com", dto.ICloudEmail)
	}
	if dto.Status != "pending" {
		t.Errorf("Status = %q, want pending", dto.Status)
	}
	if dto.HasCookies {
		t.Error("HasCookies = true, want false")
	}

	acc, ok := mgr.accountSnapshot(dto.ID)
	if !ok {
		t.Fatalf("account %s was not stored", dto.ID)
	}
	if got := accountLoginEmail(acc); got != "User.Name@icloud.com" {
		t.Errorf("accountLoginEmail() = %q, want User.Name@icloud.com", got)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir, "accounts.json"))
	if err != nil {
		t.Fatalf("read accounts.json: %v", err)
	}
	var config accountConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode accounts.json: %v", err)
	}
	if len(config.Accounts) != 1 {
		t.Fatalf("persisted account count = %d, want 1", len(config.Accounts))
	}
	persisted := config.Accounts[0]
	if persisted.ICloudEmail != "User.Name@icloud.com" || persisted.Status != "pending" || len(persisted.Cookies) != 0 {
		t.Errorf("persisted account = %#v, want pending account with normalized email and no cookies", persisted)
	}
}

func TestAddAccountValidatesICloudEmail(t *testing.T) {
	valid := map[string]string{
		"icloud": "user@icloud.com",
		"me":     "user@me.com",
		"mac":    "user@mac.com",
		"trim":   "User@icloud.com",
	}
	for name, want := range valid {
		t.Run("accepts "+name, func(t *testing.T) {
			input := want
			if name == "trim" {
				input = "  User@ICLOUD.COM  "
			}
			mgr, err := NewManager(t.TempDir())
			if err != nil {
				t.Fatalf("NewManager() error = %v", err)
			}
			dto, err := mgr.AddAccount("账号", input, "", "", "")
			if err != nil {
				t.Fatalf("AddAccount() error = %v", err)
			}
			if dto.ICloudEmail != want {
				t.Errorf("ICloudEmail = %q, want %q", dto.ICloudEmail, want)
			}
		})
	}

	invalid := []string{
		"not-an-email",
		"user@example.com",
		"user@icloud.com.evil.example",
		"User <user@icloud.com>",
	}
	for _, input := range invalid {
		t.Run("rejects "+input, func(t *testing.T) {
			mgr, err := NewManager(t.TempDir())
			if err != nil {
				t.Fatalf("NewManager() error = %v", err)
			}
			if _, err := mgr.AddAccount("账号", input, "", "", ""); err == nil || !strings.Contains(err.Error(), "icloud_email") {
				t.Fatalf("AddAccount() error = %v, want icloud_email validation error", err)
			}
			if accounts := mgr.ListAccounts(); len(accounts) != 0 {
				t.Errorf("rejected email persisted accounts: %+v", accounts)
			}
		})
	}
}

func TestAccountTemplateUsesCanonicalSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "accounts.json.template"))
	if err != nil {
		t.Fatalf("read accounts.json.template: %v", err)
	}
	accounts, err := decodeAccountConfig(raw)
	if err != nil {
		t.Fatalf("decode accounts.json.template: %v", err)
	}
	acc := accounts["acc_1"]
	if acc == nil {
		t.Fatal("template does not contain acc_1")
	}
	if acc.Host != "icloud.com" {
		t.Errorf("template host = %q, want icloud.com", acc.Host)
	}
	if acc.ICloudEmail == "" || acc.AppPassword == "" || len(acc.Cookies) == 0 {
		t.Errorf("template credential fields do not use canonical schema: %#v", acc)
	}
}

func TestHMEClientUsesIndependentCookieSnapshot(t *testing.T) {
	mgr := newManagerWithCookies(t)
	client, err := mgr.HMEClient("acc_cookie", false)
	if err != nil {
		t.Fatalf("HMEClient() error = %v", err)
	}

	client.Cookies["session"] = "refreshed-by-client"
	client.Cookies["new-cookie"] = "client-only"
	acc, ok := mgr.accountSnapshot("acc_cookie")
	if !ok {
		t.Fatal("account acc_cookie does not exist")
	}
	if got := acc.Cookies["session"]; got != "stored-cookie" {
		t.Errorf("stored session cookie = %q, want stored-cookie", got)
	}
	if _, exists := acc.Cookies["new-cookie"]; exists {
		t.Error("client mutation leaked into stored cookies")
	}
}

func TestSaveCookiesCopiesInput(t *testing.T) {
	mgr := newManagerWithCookies(t)
	input := map[string]string{"session": "saved-cookie"}
	if err := mgr.SaveCookies("acc_cookie", input); err != nil {
		t.Fatalf("SaveCookies() error = %v", err)
	}

	input["session"] = "caller-mutated"
	input["new-cookie"] = "caller-only"
	acc, _ := mgr.accountSnapshot("acc_cookie")
	if got := acc.Cookies["session"]; got != "saved-cookie" {
		t.Errorf("stored session cookie = %q, want saved-cookie", got)
	}
	if _, exists := acc.Cookies["new-cookie"]; exists {
		t.Error("caller mutation leaked into stored cookies")
	}
}

func TestCookieCountBoundary(t *testing.T) {
	allowed := makeTestCookies(MaxCookieCount)
	rawAllowed, err := json.Marshal(allowed)
	if err != nil {
		t.Fatalf("marshal allowed cookies: %v", err)
	}
	parsed, err := ParseCookieInput(string(rawAllowed))
	if err != nil {
		t.Fatalf("ParseCookieInput() at limit error = %v", err)
	}
	if len(parsed) != MaxCookieCount {
		t.Fatalf("parsed cookie count = %d, want %d", len(parsed), MaxCookieCount)
	}

	overLimit := makeTestCookies(MaxCookieCount + 1)
	rawOverLimit, err := json.Marshal(overLimit)
	if err != nil {
		t.Fatalf("marshal over-limit cookies: %v", err)
	}
	if _, err := ParseCookieInput(string(rawOverLimit)); err == nil || !strings.Contains(err.Error(), "Cookie 数量不能超过") {
		t.Fatalf("ParseCookieInput() error = %v, want cookie count error", err)
	}

	mgr := newManagerWithCookies(t)
	if err := mgr.SaveCookies("acc_cookie", overLimit); err == nil || !strings.Contains(err.Error(), "Cookie 数量不能超过") {
		t.Fatalf("SaveCookies() error = %v, want cookie count error", err)
	}
	acc, _ := mgr.accountSnapshot("acc_cookie")
	if got := acc.Cookies["session"]; got != "stored-cookie" {
		t.Errorf("stored cookie changed after rejected save: %q", got)
	}
}

func TestPersistenceFailureRollsBackAndCleansTemporaryFile(t *testing.T) {
	mgr := newManagerWithCookies(t)
	blockedTarget := filepath.Join(mgr.dataDir, "blocked-target")
	if err := os.Mkdir(blockedTarget, 0700); err != nil {
		t.Fatalf("create blocked target: %v", err)
	}
	mgr.dataFile = blockedTarget

	err := mgr.SaveCookies("acc_cookie", map[string]string{"session": "not-persisted"})
	if !errors.Is(err, ErrPersistence) {
		t.Fatalf("SaveCookies() error = %v, want ErrPersistence", err)
	}
	acc, _ := mgr.accountSnapshot("acc_cookie")
	if got := acc.Cookies["session"]; got != "stored-cookie" {
		t.Errorf("stored session cookie after failed save = %q, want stored-cookie", got)
	}

	removed, err := mgr.RemoveAccount("acc_cookie")
	if removed || !errors.Is(err, ErrPersistence) {
		t.Fatalf("RemoveAccount() = (%v, %v), want (false, ErrPersistence)", removed, err)
	}
	if _, ok := mgr.accountSnapshot("acc_cookie"); !ok {
		t.Error("failed removal was not rolled back")
	}

	temporaryFiles, err := filepath.Glob(filepath.Join(mgr.dataDir, ".blocked-target.*.tmp"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(temporaryFiles) != 0 {
		t.Errorf("temporary files were not cleaned up: %v", temporaryFiles)
	}
}

func TestManagerConcurrentCookieSnapshotsAndSaves(t *testing.T) {
	mgr := newManagerWithCookies(t)
	const iterations = 12
	errCh := make(chan error, iterations*3)
	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			cookies := map[string]string{"session": fmt.Sprintf("saved-%d", i)}
			if err := mgr.SaveCookies("acc_cookie", cookies); err != nil {
				errCh <- err
				return
			}
			cookies["session"] = "caller-mutated"
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			client, err := mgr.HMEClient("acc_cookie", false)
			if err != nil {
				errCh <- err
				return
			}
			client.Cookies["session"] = fmt.Sprintf("client-%d", i)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations*4; i++ {
			accounts := mgr.ListAccounts()
			if len(accounts) != 1 {
				errCh <- fmt.Errorf("account count = %d, want 1", len(accounts))
				return
			}
		}
	}()

	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent manager operation: %v", err)
	}

	raw, err := os.ReadFile(mgr.dataFile)
	if err != nil {
		t.Fatalf("read final account config: %v", err)
	}
	if _, err := decodeAccountConfig(raw); err != nil {
		t.Fatalf("final account config is incomplete: %v", err)
	}
}

type fakePasswordLoginClient struct {
	requiresOTP   bool
	startErr      error
	verifyErr     error
	cookies       map[string]string
	username      string
	password      string
	otp           string
	challenge     *hme.LoginChallenge
	verifyCalls   int
	validateErr   error
	validateCalls int
	closeCalls    int
}

func (c *fakePasswordLoginClient) StartLogin(username, password string) (*hme.LoginChallenge, error) {
	c.username = username
	c.password = password
	if c.startErr != nil {
		return nil, c.startErr
	}
	if !c.requiresOTP {
		return nil, nil
	}
	c.challenge = &hme.LoginChallenge{}
	return c.challenge, nil
}

func (c *fakePasswordLoginClient) VerifyLogin(challenge *hme.LoginChallenge, otp string) error {
	c.verifyCalls++
	c.otp = otp
	if challenge != c.challenge {
		return errors.New("unexpected login challenge")
	}
	return c.verifyErr
}

func (c *fakePasswordLoginClient) Validate() (bool, error) {
	c.validateCalls++
	if c.validateErr != nil {
		return false, c.validateErr
	}
	return true, nil
}

func (c *fakePasswordLoginClient) SessionCookies() map[string]string {
	out := make(map[string]string, len(c.cookies))
	for name, value := range c.cookies {
		out[name] = value
	}
	return out
}

func (c *fakePasswordLoginClient) Close() {
	c.closeCalls++
}

func TestStartPasswordLoginPersistsDirectSuccess(t *testing.T) {
	mgr := newPendingLoginManager(t)
	client := &fakePasswordLoginClient{cookies: map[string]string{"session": "direct-cookie"}}
	mgr.newPasswordLoginClient = func(map[string]string, string, string, bool) (passwordLoginClient, error) {
		return client, nil
	}

	dto, session, err := mgr.StartPasswordLogin("acc_login", "apple-password")
	if err != nil {
		t.Fatalf("StartPasswordLogin() error = %v", err)
	}
	if session != nil {
		t.Fatal("StartPasswordLogin() returned unexpected OTP session")
	}
	if client.username != "User@icloud.com" || client.password != "apple-password" {
		t.Errorf("login credentials = (%q, %q), want configured email and supplied password", client.username, client.password)
	}
	if dto.Status != "active" || !dto.HasCookies || dto.LastValidated == "" {
		t.Errorf("direct login DTO = %+v, want active account with cookies", dto)
	}
	if client.closeCalls != 1 {
		t.Errorf("client close calls = %d, want 1", client.closeCalls)
	}
	if client.validateCalls != 1 {
		t.Errorf("client validate calls = %d, want 1", client.validateCalls)
	}
	acc, _ := mgr.accountSnapshot("acc_login")
	if got := acc.Cookies["session"]; got != "direct-cookie" {
		t.Errorf("persisted session cookie = %q, want direct-cookie", got)
	}
}

func TestPasswordLoginSessionVerifiesOnceAndPersists(t *testing.T) {
	mgr := newPendingLoginManager(t)
	client := &fakePasswordLoginClient{
		requiresOTP: true,
		cookies:     map[string]string{"session": "otp-cookie"},
	}
	mgr.newPasswordLoginClient = func(map[string]string, string, string, bool) (passwordLoginClient, error) {
		return client, nil
	}

	_, session, err := mgr.StartPasswordLogin("acc_login", "apple-password")
	if err != nil {
		t.Fatalf("StartPasswordLogin() error = %v", err)
	}
	if session == nil {
		t.Fatal("StartPasswordLogin() session = nil, want OTP session")
	}
	before, _ := mgr.accountSnapshot("acc_login")
	if before.Status != "pending" || len(before.Cookies) != 0 {
		t.Errorf("password stage changed account before OTP: %#v", before)
	}

	dto, err := session.Verify("123456")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if dto.Status != "active" || !dto.HasCookies {
		t.Errorf("Verify() DTO = %+v, want active account with cookies", dto)
	}
	if client.otp != "123456" || client.verifyCalls != 1 || client.closeCalls != 1 {
		t.Errorf("verify client state = otp %q, verify %d, close %d", client.otp, client.verifyCalls, client.closeCalls)
	}
	if client.validateCalls != 1 {
		t.Errorf("verify client validate calls = %d, want 1", client.validateCalls)
	}
	if _, err := session.Verify("123456"); !errors.Is(err, ErrLoginSessionInvalid) {
		t.Fatalf("second Verify() error = %v, want ErrLoginSessionInvalid", err)
	}
	if client.verifyCalls != 1 {
		t.Errorf("consumed session called upstream %d times, want 1", client.verifyCalls)
	}
}

func TestStartPasswordLoginDoesNotPersistUnvalidatedSession(t *testing.T) {
	mgr := newPendingLoginManager(t)
	client := &fakePasswordLoginClient{
		cookies:     map[string]string{"session": "unvalidated-cookie"},
		validateErr: errors.New("validation failed"),
	}
	mgr.newPasswordLoginClient = func(map[string]string, string, string, bool) (passwordLoginClient, error) {
		return client, nil
	}

	_, session, err := mgr.StartPasswordLogin("acc_login", "apple-password")
	if err == nil {
		t.Fatal("StartPasswordLogin() error = nil, want validation failure")
	}
	if session != nil {
		t.Fatal("StartPasswordLogin() session != nil")
	}
	if client.validateCalls != 1 || client.closeCalls != 1 {
		t.Errorf("client state = validate %d, close %d", client.validateCalls, client.closeCalls)
	}
	acc, _ := mgr.accountSnapshot("acc_login")
	if acc.Status != "pending" || len(acc.Cookies) != 0 || acc.LastValidated != "" {
		t.Errorf("unvalidated login changed account: %#v", acc)
	}
}

func TestPasswordLoginPersistenceFailureRollsBack(t *testing.T) {
	mgr := newPendingLoginManager(t)
	client := &fakePasswordLoginClient{
		requiresOTP: true,
		cookies:     map[string]string{"session": "not-persisted"},
	}
	mgr.newPasswordLoginClient = func(map[string]string, string, string, bool) (passwordLoginClient, error) {
		return client, nil
	}
	_, session, err := mgr.StartPasswordLogin("acc_login", "apple-password")
	if err != nil {
		t.Fatalf("StartPasswordLogin() error = %v", err)
	}

	if err := os.Remove(mgr.dataFile); err != nil {
		t.Fatalf("remove account config: %v", err)
	}
	if err := os.Mkdir(mgr.dataFile, 0700); err != nil {
		t.Fatalf("block account config target: %v", err)
	}
	if _, err := session.Verify("123456"); !errors.Is(err, ErrPersistence) {
		t.Fatalf("Verify() error = %v, want ErrPersistence", err)
	}
	acc, _ := mgr.accountSnapshot("acc_login")
	if acc.Status != "pending" || len(acc.Cookies) != 0 || acc.LastValidated != "" {
		t.Errorf("failed login persistence was not rolled back: %#v", acc)
	}
}

func newPendingLoginManager(t *testing.T) *Manager {
	t.Helper()
	dataDir := t.TempDir()
	writeAccountConfig(t, dataDir, `{
  "accounts": [
    {
      "id": "acc_login",
      "name": "待登录账号",
      "icloud_email": "User@icloud.com",
      "host": "icloud.com",
      "cookies": {},
      "status": "pending"
    }
  ]
}`)
	mgr, err := NewManager(dataDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return mgr
}

func newManagerWithCookies(t *testing.T) *Manager {
	t.Helper()
	dataDir := t.TempDir()
	writeAccountConfig(t, dataDir, `{
  "accounts": [
    {
      "id": "acc_cookie",
      "name": "Cookie 账号",
      "host": "icloud.com",
      "cookies": {"session": "stored-cookie"},
      "status": "active"
    }
  ]
}`)
	mgr, err := NewManager(dataDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return mgr
}

func makeTestCookies(count int) map[string]string {
	cookies := make(map[string]string, count)
	for i := 0; i < count; i++ {
		cookies[fmt.Sprintf("cookie-%03d", i)] = "value"
	}
	return cookies
}

func writeAccountConfig(t *testing.T, dataDir, config string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dataDir, "accounts.json"), []byte(config), 0600); err != nil {
		t.Fatalf("write accounts.json: %v", err)
	}
}

func TestAliasAutomationPersistsConfigurationAndRunStatus(t *testing.T) {
	mgr := newManagerWithCookies(t)
	now := time.Date(2026, time.August, 2, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	automation := DefaultAliasAutomation()
	automation.Enabled = true
	automation.IntervalMinutes = 30
	automation.ScheduledBatchSize = 2
	automation.MinimumActive = 5
	automation.TargetActive = 8
	automation.MaxBatchSize = 4
	automation.LabelPrefix = "  自动创建  "

	saved, err := mgr.SetAliasAutomation("acc_cookie", automation, now)
	if err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}
	if saved.LabelPrefix != "自动创建" {
		t.Errorf("LabelPrefix = %q, want trimmed value", saved.LabelPrefix)
	}
	if saved.NextRunAt != now.Add(30*time.Minute).Format(time.RFC3339) {
		t.Errorf("NextRunAt = %q, want %q", saved.NextRunAt, now.Add(30*time.Minute).Format(time.RFC3339))
	}

	runAt := now.Add(5 * time.Minute)
	run, err := mgr.RecordAliasAutomationRun("acc_cookie", AliasAutomationRun{
		ActiveAliases: 3,
		Created:       4,
		Error:         "上游创建失败，已停止后续创建",
		Status:        AliasAutomationStatusPartial,
	}, runAt)
	if err != nil {
		t.Fatalf("RecordAliasAutomationRun() error = %v", err)
	}
	if run.LastStatus != AliasAutomationStatusPartial || run.LastActive != 3 || run.LastCreated != 4 {
		t.Errorf("unexpected run status: %+v", run)
	}
	if run.NextRunAt != runAt.Add(60*time.Minute).Format(time.RFC3339) {
		t.Errorf("run NextRunAt = %q, want %q", run.NextRunAt, runAt.Add(60*time.Minute).Format(time.RFC3339))
	}

	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	reloaded, err := mgr.GetAliasAutomation("acc_cookie")
	if err != nil {
		t.Fatalf("GetAliasAutomation() error = %v", err)
	}
	if !reflect.DeepEqual(reloaded, run) {
		t.Errorf("reloaded automation = %+v, want %+v", reloaded, run)
	}
}

func TestAliasAutomationValidationAndDisabledSchedule(t *testing.T) {
	mgr := newManagerWithCookies(t)
	now := time.Date(2026, time.August, 2, 9, 30, 0, 0, time.UTC)
	invalid := DefaultAliasAutomation()
	invalid.Enabled = true
	if _, err := mgr.SetAliasAutomation("acc_cookie", invalid, now); err == nil {
		t.Fatal("SetAliasAutomation() error = nil, want enabled rule validation error")
	}

	valid := DefaultAliasAutomation()
	valid.MinimumActive = 2
	valid.TargetActive = 4
	saved, err := mgr.SetAliasAutomation("acc_cookie", valid, now)
	if err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}
	if saved.NextRunAt != "" {
		t.Errorf("disabled NextRunAt = %q, want empty", saved.NextRunAt)
	}

	configured, err := mgr.GetAliasAutomation("acc_cookie")
	if err != nil {
		t.Fatalf("GetAliasAutomation() error = %v", err)
	}
	if configured.MinimumActive != 2 || configured.TargetActive != 4 || configured.Enabled {
		t.Errorf("unexpected stored disabled configuration: %+v", configured)
	}
}

func TestAliasAutomationScheduleValidationAndDeferral(t *testing.T) {
	base := DefaultAliasAutomation()
	base.Enabled = true
	base.ScheduledBatchSize = 1
	tests := []struct {
		name string
		edit func(*AliasAutomation)
	}{
		{
			name: "weekday outside range",
			edit: func(rule *AliasAutomation) { rule.AllowedWeekdays = []int{7} },
		},
		{
			name: "duplicate weekday",
			edit: func(rule *AliasAutomation) { rule.AllowedWeekdays = []int{1, 1} },
		},
		{
			name: "partial window",
			edit: func(rule *AliasAutomation) { rule.ExecutionWindowStart = "09:00" },
		},
		{
			name: "invalid clock",
			edit: func(rule *AliasAutomation) {
				rule.ExecutionWindowStart = "9:00"
				rule.ExecutionWindowEnd = "17:00"
			},
		},
		{
			name: "window cannot cross midnight",
			edit: func(rule *AliasAutomation) {
				rule.ExecutionWindowStart = "17:00"
				rule.ExecutionWindowEnd = "09:00"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := base
			test.edit(&rule)
			if err := ValidateAliasAutomation(rule); err == nil {
				t.Fatal("ValidateAliasAutomation() error = nil, want schedule validation error")
			}
		})
	}

	mgr := newManagerWithCookies(t)
	sunday := time.Date(2026, time.August, 2, 17, 0, 0, 0, time.UTC)
	rule := base
	rule.AllowedWeekdays = []int{1, 2, 3, 4, 5}
	rule.ExecutionWindowStart = "09:00"
	rule.ExecutionWindowEnd = "17:00"
	saved, err := mgr.SetAliasAutomation("acc_cookie", rule, sunday)
	if err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}
	wantMondayStart := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if saved.NextRunAt != wantMondayStart || fmt.Sprint(saved.AllowedWeekdays) != "[1 2 3 4 5]" {
		t.Errorf("saved schedule = %+v, want next run %q", saved, wantMondayStart)
	}

	mondayAfterWindow := time.Date(2026, time.August, 3, 17, 0, 0, 0, time.UTC)
	deferred, err := mgr.DeferAliasAutomationToNextAllowedTime("acc_cookie", mondayAfterWindow)
	if err != nil {
		t.Fatalf("DeferAliasAutomationToNextAllowedTime() error = %v", err)
	}
	wantTuesdayStart := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if deferred.NextRunAt != wantTuesdayStart || deferred.LastRunAt != "" {
		t.Errorf("deferred schedule = %+v, want next run %q without a recorded run", deferred, wantTuesdayStart)
	}
}

func TestAliasAutomationCumulativeTargetPreservesProgressAndStops(t *testing.T) {
	mgr := newManagerWithCookies(t)
	now := time.Date(2026, time.August, 2, 9, 30, 0, 0, time.UTC)
	rule := DefaultAliasAutomation()
	rule.Enabled = true
	rule.IntervalMinutes = 60
	rule.MaxBatchSize = 5
	rule.TargetCreated = 5

	if _, err := mgr.SetAliasAutomation("acc_cookie", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}
	partial, err := mgr.RecordAliasAutomationRun("acc_cookie", AliasAutomationRun{
		ActiveAliases: 3,
		Created:       3,
		Status:        AliasAutomationStatusSuccess,
	}, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("RecordAliasAutomationRun() partial error = %v", err)
	}
	if partial.CreatedTotal != 3 || !partial.Enabled {
		t.Errorf("partial progress = %+v, want created_total 3 and enabled", partial)
	}

	resaved, err := mgr.SetAliasAutomation("acc_cookie", rule, now.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("SetAliasAutomation() same target error = %v", err)
	}
	if resaved.CreatedTotal != 3 || !resaved.Enabled {
		t.Errorf("same target save = %+v, want preserved progress", resaved)
	}

	completed, err := mgr.RecordAliasAutomationRun("acc_cookie", AliasAutomationRun{
		ActiveAliases: 5,
		Created:       2,
		Status:        AliasAutomationStatusSuccess,
	}, now.Add(15*time.Minute))
	if err != nil {
		t.Fatalf("RecordAliasAutomationRun() completion error = %v", err)
	}
	if completed.CreatedTotal != 5 || completed.Enabled || completed.NextRunAt != "" {
		t.Errorf("completed target = %+v, want disabled rule without next run", completed)
	}

	reopened, err := mgr.SetAliasAutomation("acc_cookie", rule, now.Add(20*time.Minute))
	if err != nil {
		t.Fatalf("SetAliasAutomation() completed target error = %v", err)
	}
	if reopened.Enabled || reopened.CreatedTotal != 5 {
		t.Errorf("same completed target = %+v, want preserved completed progress", reopened)
	}

	newTarget := rule
	newTarget.TargetCreated = 6
	reset, err := mgr.SetAliasAutomation("acc_cookie", newTarget, now.Add(25*time.Minute))
	if err != nil {
		t.Fatalf("SetAliasAutomation() new target error = %v", err)
	}
	if !reset.Enabled || reset.CreatedTotal != 0 {
		t.Errorf("new target = %+v, want enabled rule with reset progress", reset)
	}
}

func TestAliasAutomationFailureBackoffPausesAndResumes(t *testing.T) {
	mgr := newManagerWithCookies(t)
	now := time.Date(2026, time.August, 2, 9, 30, 0, 0, time.UTC)
	rule := DefaultAliasAutomation()
	rule.Enabled = true
	rule.IntervalMinutes = 30
	rule.ScheduledBatchSize = 1
	if _, err := mgr.SetAliasAutomation("acc_cookie", rule, now); err != nil {
		t.Fatalf("SetAliasAutomation() error = %v", err)
	}

	first, err := mgr.RecordAliasAutomationRun("acc_cookie", AliasAutomationRun{
		ActiveAliases: 0,
		Error:         "创建别名失败，请稍后重试",
		Status:        AliasAutomationStatusError,
	}, now.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("first RecordAliasAutomationRun() error = %v", err)
	}
	if first.ConsecutiveFailure != 1 || !first.Enabled || first.NextRunAt != now.Add(90*time.Minute).Format(time.RFC3339) {
		t.Errorf("first failure state = %+v, want one failure and two-interval retry", first)
	}

	second, err := mgr.RecordAliasAutomationRun("acc_cookie", AliasAutomationRun{
		ActiveAliases: 0,
		Error:         "创建别名失败，请稍后重试",
		Status:        AliasAutomationStatusError,
	}, now.Add(90*time.Minute))
	if err != nil {
		t.Fatalf("second RecordAliasAutomationRun() error = %v", err)
	}
	if second.ConsecutiveFailure != 2 || !second.Enabled || second.NextRunAt != now.Add(210*time.Minute).Format(time.RFC3339) {
		t.Errorf("second failure state = %+v, want two failures and four-interval retry", second)
	}

	paused, err := mgr.RecordAliasAutomationRun("acc_cookie", AliasAutomationRun{
		ActiveAliases: 0,
		Error:         "创建别名失败，请稍后重试",
		Status:        AliasAutomationStatusError,
	}, now.Add(210*time.Minute))
	if err != nil {
		t.Fatalf("third RecordAliasAutomationRun() error = %v", err)
	}
	if paused.ConsecutiveFailure != 3 || paused.Enabled || paused.PauseReason != AliasAutomationPauseReasonFailureLimit || paused.NextRunAt != "" {
		t.Errorf("paused failure state = %+v, want automatic failure pause", paused)
	}

	resumed, err := mgr.SetAliasAutomation("acc_cookie", rule, now.Add(220*time.Minute))
	if err != nil {
		t.Fatalf("SetAliasAutomation() resume error = %v", err)
	}
	if !resumed.Enabled || resumed.ConsecutiveFailure != 0 || resumed.PauseReason != "" || resumed.NextRunAt != now.Add(250*time.Minute).Format(time.RFC3339) {
		t.Errorf("resumed state = %+v, want clean retry state", resumed)
	}
}
