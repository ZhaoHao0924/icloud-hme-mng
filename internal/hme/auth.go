// Package hme - iCloud 认证模块
//
// 基于 Go-iClient 项目实现完整的 SRP (Secure Remote Password) 登录流程,
// 支持双重认证 (2FA),登录成功后提取 session token Cookie。
package hme

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/crypto/pbkdf2"

	http "github.com/bogdanfinn/fhttp"
	"icloud-hme/internal/srp"
)

// AuthEndpoints iCloud 认证 API 端点
const (
	OAuthClientID = "d39ba9916b7251055b22c7f910e2ea796ee65e98b2ddecea8f5dde8d9d1a815d"

	authStartFmt    = "https://idmsa.apple.com/appleauth/auth/authorize/signin?frame_id=auth-%s&language=en_US&skVersion=7&iframeId=auth-%s&client_id=%s&redirect_uri=https://www.icloud.com&response_type=code&response_mode=web_message&state=auth-%s&authVersion=latest"
	authFederate    = "https://idmsa.apple.com/appleauth/auth/federate?isRememberMeEnabled=true"
	authInit        = "https://idmsa.apple.com/appleauth/auth/signin/init"
	authComplete    = "https://idmsa.apple.com/appleauth/auth/signin/complete?isRememberMeEnabled=true"
	authOptions     = "https://idmsa.apple.com/appleauth/auth"
	submitSecurity  = "https://idmsa.apple.com/appleauth/auth/verify/%s/securitycode"
	authTrust       = "https://idmsa.apple.com/appleauth/auth/2sv/trust"
	authWebFmt      = "https://setup.icloud.com/setup/ws/1/accountLogin"
	authValidateFmt = "https://setup.icloud.com/setup/ws/1/validate?clientBuildNumber=%s&clientMasteringNumber=%s&clientId=%s"
)

// OTPProvider 双重认证回调函数,返回 2FA 验证码
type OTPProvider func() (string, error)

var (
	// ErrOTPRequired 表示密码阶段成功，但账号要求双重认证。
	ErrOTPRequired = errors.New("账号需要双重认证验证码")
	// ErrInvalidCredentials 表示 Apple 拒绝了账号密码。
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	// ErrPrivacyTermsRequired 表示账号需要先确认 Apple 隐私条款。
	ErrPrivacyTermsRequired = errors.New("需要先在 appleid.apple.com 同意隐私条款")
	// ErrInvalidOTP 表示双重认证验证码格式错误或被 Apple 拒绝。
	ErrInvalidOTP = errors.New("双重认证验证码无效")
	// ErrLoginChallengeInvalid 表示登录 challenge 已消费或不属于当前客户端。
	ErrLoginChallengeInvalid = errors.New("登录 challenge 无效或已使用")
)

// authState 保存认证过程中的状态
type authState struct {
	username   string
	frameId    string
	clientId   string
	authAttr   string
	sessionID  string
	scnt       string
	authToken  string
	trustToken string
	dsid       string
}

// LoginChallenge 保存继续双重认证所需的内存状态，不包含账号密码。
// challenge 只能由创建它的 Client 消费一次。
type LoginChallenge struct {
	mu       sync.Mutex
	client   *Client
	state    *authState
	consumed bool
}

// Login 使用 iCloud 账号密码登录,获取 session token Cookie。
//
// 登录成功后,可以通过 client.SessionCookies() 获取 Cookie 副本。
// 启用 2FA 时,会调用 otpProvider 获取验证码。
func (c *Client) Login(username, password string, otpProvider OTPProvider) error {
	challenge, err := c.StartLogin(username, password)
	if err != nil {
		return err
	}
	if challenge == nil {
		return nil
	}
	if otpProvider == nil {
		return ErrOTPRequired
	}
	otp, err := otpProvider()
	if err != nil {
		return fmt.Errorf("获取 2FA 验证码失败: %w", err)
	}
	return c.VerifyLogin(challenge, otp)
}

// StartLogin 执行账号密码和 SRP 阶段。无 2FA 时直接完成登录并返回 nil；
// 需要 2FA 时返回只能在同一 Client 上使用一次的 LoginChallenge。
func (c *Client) StartLogin(username, password string) (*LoginChallenge, error) {
	state := &authState{username: username}

	// 1. 初始化 frameId 和 clientId
	if err := c.authStart(state); err != nil {
		return nil, fmt.Errorf("auth start: %w", err)
	}

	// 2. 提交用户名
	if err := c.authFederate(state); err != nil {
		return nil, fmt.Errorf("auth federate: %w", err)
	}

	// 3. SRP 协议初始化
	params := srp.GetParams(2048)
	params.NoUserNameInX = true
	srpClient := srp.NewSRPClient(params, nil)

	// 4. 获取 salt 和 B
	authInitResp, err := c.authInit(state, base64.StdEncoding.EncodeToString(srpClient.GetABytes()))
	if err != nil {
		return nil, fmt.Errorf("auth init: %w", err)
	}

	// 5. 解码 salt 和 B
	bDec, err := base64.StdEncoding.DecodeString(authInitResp.B)
	if err != nil {
		return nil, fmt.Errorf("decode B: %w", err)
	}
	saltDec, err := base64.StdEncoding.DecodeString(authInitResp.Salt)
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}

	// 6. 生成密码密钥
	passHash := sha256.Sum256([]byte(password))
	passKey := pbkdf2.Key(passHash[:], saltDec, authInitResp.Iteration, 32, sha256.New)

	// 7. 处理挑战
	srpClient.ProcessClientChanllenge([]byte(username), passKey, saltDec, bDec)

	// 8. 提交 SRP 响应 (可能触发 2FA)
	requiresOTP, err := c.authComplete(
		state,
		authInitResp.C,
		base64.StdEncoding.EncodeToString(srpClient.M1),
		base64.StdEncoding.EncodeToString(srpClient.M2),
	)
	if err != nil {
		return nil, fmt.Errorf("auth complete: %w", err)
	}
	if requiresOTP {
		return &LoginChallenge{client: c, state: state}, nil
	}

	if err := c.finishLogin(state); err != nil {
		return nil, err
	}
	return nil, nil
}

// VerifyLogin 提交双重认证验证码并完成登录。无论 Apple 是否接受验证码，
// challenge 在提交时都会被消费，重试需要重新调用 StartLogin。
func (c *Client) VerifyLogin(challenge *LoginChallenge, otp string) error {
	otp = strings.TrimSpace(otp)
	if !isSixDigitCode(otp) {
		return ErrInvalidOTP
	}
	if challenge == nil {
		return ErrLoginChallengeInvalid
	}

	challenge.mu.Lock()
	if challenge.consumed || challenge.client != c || challenge.state == nil {
		challenge.mu.Unlock()
		return ErrLoginChallengeInvalid
	}
	state := challenge.state
	challenge.consumed = true
	challenge.client = nil
	challenge.state = nil
	challenge.mu.Unlock()

	if err := c.submitTwoFactor(state, otp); err != nil {
		return fmt.Errorf("提交 2FA 验证码: %w", err)
	}
	return c.finishLogin(state)
}

func (c *Client) finishLogin(state *authState) error {
	if err := c.getTrust(state); err != nil {
		return fmt.Errorf("get trust: %w", err)
	}

	// 10. 获取 iCloud Web 服务 Cookie
	if err := c.authenticateWeb(state); err != nil {
		return fmt.Errorf("authenticate web: %w", err)
	}

	// 11. 保存 Cookie 到 Client
	cookies := c.extractSessionCookies()
	c.Cookies = cookies
	c.log("登录成功,获取到 %d 个 Cookie", len(cookies))
	return nil
}

func isSixDigitCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// --- 认证流程的各步骤 ---

// authStart 初始化 frameId 和 clientId
func (c *Client) authStart(state *authState) error {
	state.frameId = strings.ToLower(uuid.New().String())
	state.clientId = OAuthClientID

	req, err := http.NewRequest("GET", fmt.Sprintf(authStartFmt, state.frameId, state.frameId, state.clientId, state.frameId), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	state.authAttr = resp.Header.Get("X-Apple-Auth-Attributes")
	return nil
}

// authFederate 提交用户名
func (c *Client) authFederate(state *authState) error {
	data, err := json.Marshal(map[string]interface{}{
		"accountName": state.username,
		"rememberMe":  true,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", authFederate, bytes.NewReader([]byte(data)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

// authInitResp authInit 响应
type authInitResp struct {
	Iteration int    `json:"iteration"`
	Salt      string `json:"salt"`
	Protocol  string `json:"protocol"`
	B         string `json:"b"`
	C         string `json:"c"`
}

// authInit 初始化 SRP 认证
func (c *Client) authInit(state *authState, a string) (*authInitResp, error) {
	reqBody := map[string]interface{}{
		"a":           a,
		"accountName": state.username,
		"protocols":   []string{"s2k", "s2k_fo"},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", authInit, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result authInitResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Iteration <= 0 || result.Salt == "" || result.B == "" || result.C == "" {
		return nil, errors.New("auth init 响应缺少 SRP 挑战字段")
	}
	return &result, nil
}

// authComplete 提交 SRP 响应
func (c *Client) authComplete(state *authState, serverChallenge, m1, m2 string) (bool, error) {
	reqBody := map[string]interface{}{
		"accountName": state.username,
		"rememberMe":  true,
		"trustTokens": []string{},
		"m1":          m1,
		"c":           serverChallenge,
		"m2":          m2,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequest("POST", authComplete, bytes.NewReader(data))
	if err != nil {
		return false, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	updateAuthStateHeaders(state, resp)

	switch resp.StatusCode {
	case 200:
		return false, nil
	case 409:
		if err := c.prepareTwoFactor(state); err != nil {
			return false, err
		}
		return true, nil
	case 401, 403:
		return false, ErrInvalidCredentials
	case 412:
		return false, ErrPrivacyTermsRequired
	default:
		return false, fmt.Errorf("auth complete 失败: HTTP %d", resp.StatusCode)
	}
}

func updateAuthStateHeaders(state *authState, resp *http.Response) {
	if sessionID := resp.Header.Get("X-Apple-ID-Session-Id"); sessionID != "" {
		state.sessionID = sessionID
	}
	if scnt := resp.Header.Get("scnt"); scnt != "" {
		state.scnt = scnt
	}
}

// prepareTwoFactor 获取认证选项，并保存 Apple 返回的最新 scnt。
func (c *Client) prepareTwoFactor(state *authState) error {
	if state.sessionID == "" || state.scnt == "" {
		return errors.New("2FA 响应缺少会话标识")
	}
	req, err := http.NewRequest("GET", authOptions, nil)
	if err != nil {
		return err
	}
	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	updateAuthStateHeaders(state, resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("获取 2FA 选项失败: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) submitTwoFactor(state *authState, otp string) error {
	reqBody := map[string]interface{}{
		"securityCode": map[string]string{"code": otp},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", fmt.Sprintf(submitSecurity, "trusteddevice"), bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	updateAuthStateHeaders(state, resp)

	if resp.StatusCode != 204 {
		if resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 403 {
			return ErrInvalidOTP
		}
		return fmt.Errorf("2FA 验证失败: HTTP %d", resp.StatusCode)
	}
	return nil
}

// getTrust 获取 trust token
func (c *Client) getTrust(state *authState) error {
	req, err := http.NewRequest("GET", authTrust, nil)
	if err != nil {
		return err
	}

	req.Header = c.updateAuthHeaders(req.Header, state)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		return fmt.Errorf("trust 失败: HTTP %d", resp.StatusCode)
	}

	state.authToken = resp.Header.Get("X-Apple-Session-Token")
	state.trustToken = resp.Header.Get("X-Apple-TwoSV-Trust-Token")
	return nil
}

// authenticateWeb 认证 iCloud Web 服务
func (c *Client) authenticateWeb(state *authState) error {
	body := fmt.Sprintf(`{"dsWebAuthToken":"%s","accountCountryCode":"USA","extended_login":true,"trustToken":"%s"}`,
		state.authToken, state.trustToken)

	req, err := http.NewRequest("POST", authWebFmt, bytes.NewReader([]byte(body)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.Origin())
	req.Header.Set("Accept", "*/*")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("auth web 失败: HTTP %d", resp.StatusCode)
	}

	var result struct {
		DsInfo struct {
			Dsid string `json:"dsid"`
		} `json:"dsInfo"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	state.dsid = result.DsInfo.Dsid

	// 复制 idmsa.apple.com 的 Cookie 到当前 Web Origin
	idmsaURL, _ := url.Parse("https://idmsa.apple.com")
	webURL, _ := url.Parse(c.Origin())
	c.httpc.SetCookies(webURL, c.httpc.GetCookies(idmsaURL))

	return nil
}

// extractSessionCookies 提取 session token Cookie
func (c *Client) extractSessionCookies() map[string]string {
	cookies := make(map[string]string)
	u, _ := url.Parse(c.Origin())
	for _, cookie := range c.httpc.GetCookies(u) {
		cookies[cookie.Name] = cookie.Value
	}
	return cookies
}

// updateAuthHeaders 更新认证请求所需的头部
func (c *Client) updateAuthHeaders(header http.Header, state *authState) http.Header {
	if state.scnt != "" {
		header.Set("scnt", state.scnt)
	}
	if state.sessionID != "" {
		header.Set("X-Apple-ID-Session-Id", state.sessionID)
	}

	header.Set("X-Requested-With", "XMLHttpRequest")
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	header.Set("Referer", "https://idmsa.apple.com/")
	header.Set("Origin", "https://idmsa.apple.com")
	header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	header.Set("X-Apple-I-Require-UE", "true")
	header.Set("X-Apple-Auth-Attributes", state.authAttr)
	header.Set("X-Apple-Widget-Key", state.clientId)
	header.Set("X-Apple-Mandate-Security-Upgrade", "0")
	header.Set("X-Apple-Oauth-Client-Id", state.clientId)
	header.Set("X-Apple-I-FD-Client-Info", `{"U":"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36","L":"en-US","Z":"GMT-04:00","V":"1.1","F":".ta44j1e3NlY5BNlY5BSs5uQ32SCVgdI.AqWJ4EKKw0fVD_DJhCizgzH_y3EjNklY_ia4WFL264HRe4FSr_JzC1zJ6rgNNlY5BNp55BNlan0Os5Apw.BS1"}`)
	header.Set("X-Apple-Oauth-Client-Type", "firstPartyAuth")
	header.Set("X-Apple-Oauth-Redirect-URI", "https://www.icloud.com")
	header.Set("X-Apple-Oauth-Require-Grant-Code", "true")
	header.Set("X-Apple-Oauth-Response-Mode", "web_message")
	header.Set("X-Apple-Oauth-Response-Type", "code")
	header.Set("X-Apple-Oauth-State", "auth-"+state.frameId)
	header.Set("X-Apple-Offer-Security-Upgrade", "1")
	header.Set("X-Apple-Frame-Id", "auth-"+state.frameId)

	return header
}

// Validate 验证当前 Cookie 是否有效
func (c *Client) Validate() (bool, error) {
	if len(c.Cookies) == 0 {
		return false, fmt.Errorf("无 Cookie")
	}
	// 简单实现：尝试调用 validate 端点
	err := c.ValidateSession()
	if err != nil {
		return false, err
	}
	return true, nil
}
