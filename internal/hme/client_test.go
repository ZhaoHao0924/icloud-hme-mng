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
