package hme

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestNewClientCopiesCookies(t *testing.T) {
	input := map[string]string{"session": "original"}
	client, err := NewClient(input, "icloud.com", "", false)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	input["session"] = "caller-mutated"
	if got := client.Cookies["session"]; got != "original" {
		t.Errorf("client session cookie = %q, want original", got)
	}
	client.Cookies["client-only"] = "value"
	if _, exists := input["client-only"]; exists {
		t.Error("client mutation leaked into caller cookies")
	}
}

func TestParseAliasListReturnsEmptySliceForNoAliases(t *testing.T) {
	aliases := parseAliasList(`{"result":{"hmeEmails":[]}}`)
	if aliases == nil {
		t.Fatal("parseAliasList() returned nil, want empty slice")
	}
	if len(aliases) != 0 {
		t.Fatalf("parseAliasList() length = %d, want 0", len(aliases))
	}
}

func TestAliasActionsRejectUnconfirmedResponses(t *testing.T) {
	tests := []struct {
		name   string
		action func(*Client, string) (bool, error)
		path   string
	}{
		{name: "deactivate", action: (*Client).DeactivateHME, path: "/v1/hme/deactivate"},
		{name: "reactivate", action: (*Client).ReactivateHME, path: "/v1/hme/reactivate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpClient := &scriptedAuthClient{
				responses: []scriptedAuthResponse{{status: 200, body: `{"success":false}`}},
			}
			client := &Client{
				Cookies:    map[string]string{},
				Host:       "icloud.com",
				httpc:      httpClient,
				serviceURL: "https://service.example.test",
			}

			success, err := tt.action(client, "temporary-alias-id")
			if err == nil {
				t.Fatal("alias action error = nil, want confirmation failure")
			}
			if success {
				t.Fatal("alias action success = true, want false")
			}
			if len(httpClient.requests) != 1 {
				t.Fatalf("request count = %d, want 1", len(httpClient.requests))
			}
			if got := httpClient.requests[0].url; !strings.Contains(got, tt.path+"?") {
				t.Errorf("request URL = %q, want path %q", got, tt.path)
			}
		})
	}
}

func TestAliasActionConfirmationIncludesOnlySafeErrorCodes(t *testing.T) {
	_, err := aliasActionConfirmation(`{"success":false,"error":{"errorCode":"INVALID_ALIAS"}}`, "停用")
	if err == nil || !strings.Contains(err.Error(), "INVALID_ALIAS") {
		t.Fatalf("safe error code was not included: %v", err)
	}

	_, err = aliasActionConfirmation(`{"success":false,"error":{"errorCode":"alias@example.com"}}`, "停用")
	if err == nil {
		t.Fatal("unsafe error code response returned nil error")
	}
	if strings.Contains(err.Error(), "alias@example.com") {
		t.Errorf("unsafe error code leaked into error: %v", err)
	}
}

func TestVerboseLogsExcludeRequestCredentials(t *testing.T) {
	const (
		applePassword  = "fe206-apple-password"
		clientID       = "fe206-client-id"
		cookieIdentity = "fe206-cookie-identity"
		cookieName     = "fe206-cookie-name"
		cookieValue    = "fe206-cookie-value"
		dsid           = "fe206-dsid"
		proxyPassword  = "fe206-proxy-password"
		querySecret    = "fe206-query-secret"
		upstreamSecret = "fe206-upstream-cookie-secret"
	)
	validateBody := `{"webservices":{"premiummailsettings":{"url":"https://maildomainws.icloud.com:443"}},"dsInfo":{}}`
	httpClient := &scriptedAuthClient{responses: []scriptedAuthResponse{
		{status: 200, body: `{}`},
		{status: 200, body: validateBody},
		{status: 401, body: upstreamSecret},
	}}
	client := &Client{
		Cookies: map[string]string{
			"aosappleid": cookieIdentity,
			cookieName:   cookieValue,
		},
		Host:     "icloud.com",
		Proxy:    "http://proxy-user:" + proxyPassword + "@proxy.example:8080",
		Verbose:  true,
		httpc:    httpClient,
		clientID: clientID,
		dsid:     dsid,
	}

	var requestErr, validateErr, rejectedValidateErr error
	output := captureStdout(t, func() {
		_, requestErr = client.request(
			"POST",
			"https://service.example.test/account?password="+querySecret,
			map[string]string{"password": applePassword},
			0,
			1,
		)
		validateErr = client.ValidateSession()
		rejectedValidateErr = client.ValidateSession()
	})
	if requestErr != nil {
		t.Fatalf("request() error = %v", requestErr)
	}
	if validateErr != nil {
		t.Fatalf("first ValidateSession() error = %v", validateErr)
	}
	if rejectedValidateErr == nil {
		t.Fatal("second ValidateSession() error = nil, want upstream rejection")
	}

	for _, secret := range []string{
		applePassword,
		clientID,
		cookieIdentity,
		cookieName,
		cookieValue,
		dsid,
		proxyPassword,
		querySecret,
		upstreamSecret,
	} {
		if strings.Contains(output, secret) {
			t.Errorf("verbose output contains sensitive value %q: %s", secret, output)
		}
	}
	for _, safeMetadata := range []string{
		">>> POST https://service.example.test/account",
		">>> Cookie 数量: 2",
		">>> 请求头:",
		"会话有效",
		"校验失败",
	} {
		if !strings.Contains(output, safeMetadata) {
			t.Errorf("verbose output does not contain safe metadata %q: %s", safeMetadata, output)
		}
	}
	if len(httpClient.requests) != 3 {
		t.Fatalf("request count = %d, want 3", len(httpClient.requests))
	}
	if got := string(httpClient.requests[0].body); !strings.Contains(got, applePassword) {
		t.Errorf("request body = %q, want supplied password", got)
	}
	if got := httpClient.requests[0].header.Get("Cookie"); !strings.Contains(got, cookieValue) {
		t.Errorf("request Cookie header = %q, want supplied Cookie", got)
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	previous := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = previous
		_ = writer.Close()
		_ = reader.Close()
	}()

	run()
	os.Stdout = previous
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(output)
}
