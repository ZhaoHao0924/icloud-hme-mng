package hme

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"

	http "github.com/bogdanfinn/fhttp"
)

type scriptedAuthResponse struct {
	status int
	body   string
	header http.Header
}

type capturedAuthRequest struct {
	method string
	url    string
	header http.Header
	body   []byte
}

type scriptedAuthClient struct {
	responses []scriptedAuthResponse
	requests  []capturedAuthRequest
	cookies   []*http.Cookie
	closed    bool
}

func authTestHeader(values ...string) http.Header {
	header := make(http.Header)
	for i := 0; i+1 < len(values); i += 2 {
		header.Set(values[i], values[i+1])
	}
	return header
}

func (c *scriptedAuthClient) Do(req *http.Request) (*http.Response, error) {
	if len(c.responses) == 0 {
		return nil, errors.New("unexpected authentication request")
	}
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
	}
	c.requests = append(c.requests, capturedAuthRequest{
		method: req.Method,
		url:    req.URL.String(),
		header: req.Header.Clone(),
		body:   body,
	})
	response := c.responses[0]
	c.responses = c.responses[1:]
	header := response.header
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{
		StatusCode: response.status,
		Header:     header.Clone(),
		Body:       io.NopCloser(strings.NewReader(response.body)),
	}, nil
}

func (c *scriptedAuthClient) GetCookies(*url.URL) []*http.Cookie {
	return c.cookies
}

func (c *scriptedAuthClient) SetCookies(*url.URL, []*http.Cookie) {}

func (c *scriptedAuthClient) CloseIdleConnections() {
	c.closed = true
}

func TestStagedLoginUsesServerChallengeAndLatestTwoFactorHeaders(t *testing.T) {
	httpClient := &scriptedAuthClient{
		responses: []scriptedAuthResponse{
			{status: 200, header: authTestHeader("X-Apple-Auth-Attributes", "auth-attributes")},
			{status: 200},
			{status: 200, body: `{"iteration":1,"salt":"AA==","b":"Ag==","c":"server-srp-challenge"}`},
			{status: 409, header: authTestHeader(
				"X-Apple-ID-Session-Id", "apple-session-id",
				"scnt", "initial-scnt",
			)},
			{status: 200, header: authTestHeader("scnt", "options-scnt")},
			{status: 204, header: authTestHeader("scnt", "verified-scnt")},
			{status: 204, header: authTestHeader(
				"X-Apple-Session-Token", "auth-token",
				"X-Apple-TwoSV-Trust-Token", "trust-token",
			)},
			{status: 200, body: `{"dsInfo":{"dsid":"12345"}}`},
		},
		cookies: []*http.Cookie{{Name: "session", Value: "cookie-value"}},
	}
	client := &Client{Host: "icloud.com", httpc: httpClient}

	challenge, err := client.StartLogin("user@icloud.com", "apple-password")
	if err != nil {
		t.Fatalf("StartLogin() error = %v", err)
	}
	if challenge == nil {
		t.Fatal("StartLogin() challenge = nil, want 2FA challenge")
	}
	if len(httpClient.requests) != 5 {
		t.Fatalf("request count after StartLogin() = %d, want 5", len(httpClient.requests))
	}

	var completeBody map[string]any
	if err := json.Unmarshal(httpClient.requests[3].body, &completeBody); err != nil {
		t.Fatalf("decode auth complete body: %v", err)
	}
	if got := completeBody["c"]; got != "server-srp-challenge" {
		t.Errorf("auth complete c = %v, want server challenge", got)
	}
	if got := httpClient.requests[1].header.Get("X-Apple-Auth-Attributes"); got != "auth-attributes" {
		t.Errorf("federate auth attributes = %q, want auth-attributes", got)
	}
	if got := httpClient.requests[3].header.Get("X-Apple-Widget-Key"); got != OAuthClientID {
		t.Errorf("auth complete widget key = %q, want OAuth client ID", got)
	}
	if got := httpClient.requests[3].header.Get("X-Apple-Oauth-State"); !strings.HasPrefix(got, "auth-") {
		t.Errorf("auth complete OAuth state = %q, want auth-*", got)
	}
	if got := httpClient.requests[4].header.Get("scnt"); got != "initial-scnt" {
		t.Errorf("auth options scnt = %q, want initial-scnt", got)
	}
	if got := httpClient.requests[4].header.Get("X-Apple-ID-Session-Id"); got != "apple-session-id" {
		t.Errorf("auth options session ID = %q, want apple-session-id", got)
	}

	if err := client.VerifyLogin(challenge, "123456"); err != nil {
		t.Fatalf("VerifyLogin() error = %v", err)
	}
	if got := httpClient.requests[5].header.Get("scnt"); got != "options-scnt" {
		t.Errorf("OTP request scnt = %q, want options-scnt", got)
	}
	var otpBody struct {
		SecurityCode struct {
			Code string `json:"code"`
		} `json:"securityCode"`
	}
	if err := json.Unmarshal(httpClient.requests[5].body, &otpBody); err != nil {
		t.Fatalf("decode OTP body: %v", err)
	}
	if otpBody.SecurityCode.Code != "123456" {
		t.Errorf("OTP code = %q, want 123456", otpBody.SecurityCode.Code)
	}
	if got := client.SessionCookies()["session"]; got != "cookie-value" {
		t.Errorf("session cookie = %q, want cookie-value", got)
	}

	requestCount := len(httpClient.requests)
	if err := client.VerifyLogin(challenge, "123456"); !errors.Is(err, ErrLoginChallengeInvalid) {
		t.Fatalf("second VerifyLogin() error = %v, want ErrLoginChallengeInvalid", err)
	}
	if len(httpClient.requests) != requestCount {
		t.Errorf("reused challenge issued another request")
	}
}

func TestStartLoginCompletesWithoutTwoFactor(t *testing.T) {
	httpClient := &scriptedAuthClient{
		responses: []scriptedAuthResponse{
			{status: 200},
			{status: 200},
			{status: 200, body: `{"iteration":1,"salt":"AA==","b":"Ag==","c":"server-srp-challenge"}`},
			{status: 200},
			{status: 204, header: authTestHeader(
				"X-Apple-Session-Token", "auth-token",
				"X-Apple-TwoSV-Trust-Token", "trust-token",
			)},
			{status: 200, body: `{"dsInfo":{"dsid":"12345"}}`},
		},
		cookies: []*http.Cookie{{Name: "session", Value: "cookie-value"}},
	}
	client := &Client{Host: "icloud.com", httpc: httpClient}

	challenge, err := client.StartLogin("user@icloud.com", "apple-password")
	if err != nil {
		t.Fatalf("StartLogin() error = %v", err)
	}
	if challenge != nil {
		t.Fatal("StartLogin() returned unexpected 2FA challenge")
	}
	if len(httpClient.requests) != 6 {
		t.Errorf("request count = %d, want 6", len(httpClient.requests))
	}
}

func TestVerifyLoginRejectsInvalidCodeWithoutConsumingChallenge(t *testing.T) {
	challenge := &LoginChallenge{}
	client := &Client{}
	challenge.client = client
	challenge.state = &authState{}

	if err := client.VerifyLogin(challenge, "12 456"); !errors.Is(err, ErrInvalidOTP) {
		t.Fatalf("VerifyLogin() error = %v, want ErrInvalidOTP", err)
	}
	challenge.mu.Lock()
	consumed := challenge.consumed
	challenge.mu.Unlock()
	if consumed {
		t.Error("invalid OTP format consumed challenge")
	}
}

func TestClientCloseReleasesIdleConnections(t *testing.T) {
	httpClient := &scriptedAuthClient{}
	client := &Client{httpc: httpClient}
	client.Close()
	if !httpClient.closed {
		t.Error("Close() did not release idle connections")
	}
}
