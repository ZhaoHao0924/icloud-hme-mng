package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"icloud-hme/internal/account"
	"icloud-hme/internal/hme"
	"icloud-hme/internal/webui"
)

func TestAPITokenMiddleware(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	srv := newTestServer(t, token)

	tests := []struct {
		name          string
		authorization string
		wantStatus    int
	}{
		{name: "missing token", wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", authorization: "Basic " + token, wantStatus: http.StatusUnauthorized},
		{name: "wrong token", authorization: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "valid token", authorization: "Bearer " + token, wantStatus: http.StatusOK},
		{name: "case insensitive scheme", authorization: "bearer " + token, wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			res := httptest.NewRecorder()

			srv.Handler().ServeHTTP(res, req)

			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, tt.wantStatus, res.Body.String())
			}
			if tt.wantStatus == http.StatusUnauthorized {
				if got := res.Header().Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate = %q, want Bearer", got)
				}
				var body apiResp
				if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if body.Success || body.Code != apiTokenInvalidCode || body.Message == "" {
					t.Errorf("unexpected unauthorized response: %+v", body)
				}
			}
		})
	}
}

func TestAPITokenMiddlewareDisabled(t *testing.T) {
	srv := newTestServer(t, "")
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
}

func TestEmbeddedFrontendServesRootAndSPAPaths(t *testing.T) {
	srv := newTestServer(t, "")
	for _, path := range []string{"/", "/accounts/acc_primary/aliases"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "text/html")
			res := httptest.NewRecorder()

			srv.Handler().ServeHTTP(res, req)

			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; location = %q; body = %s", res.Code, http.StatusOK, res.Header().Get("Location"), res.Body.String())
			}
			if contentType := res.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", contentType)
			}
			if !strings.Contains(res.Body.String(), "<title>iCloud HME</title>") {
				t.Errorf("response does not contain embedded index: %s", res.Body.String())
			}
		})
	}
}

func TestUnknownAPIAndAssetPathsDoNotUseSPAFallback(t *testing.T) {
	srv := newTestServer(t, "")
	for _, path := range []string{"/api/unknown", "/assets/missing.js", "/missing.js"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "text/html")
			res := httptest.NewRecorder()

			srv.Handler().ServeHTTP(res, req)

			if res.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusNotFound, res.Body.String())
			}
			if strings.Contains(res.Body.String(), "Frontend assets are not built") {
				t.Errorf("response unexpectedly used SPA fallback: %s", res.Body.String())
			}
		})
	}
}

func TestEmbeddedBuiltAssetIsServed(t *testing.T) {
	entries, err := fs.ReadDir(webui.Assets(), "assets")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("frontend build assets are not present in the source tree")
	}
	if err != nil {
		t.Fatalf("read embedded assets: %v", err)
	}

	assetName := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			assetName = entry.Name()
			break
		}
	}
	if assetName == "" {
		t.Fatal("frontend build did not produce a top-level asset")
	}

	srv := newTestServer(t, "")
	req := httptest.NewRequest(http.MethodGet, "/assets/"+assetName, nil)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if cacheControl := res.Header().Get("Cache-Control"); cacheControl != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable asset cache policy", cacheControl)
	}
}

func TestFrontendAndAPIResponsesHaveExpectedCacheAndSecurityHeaders(t *testing.T) {
	srv := newTestServer(t, "")
	headers := map[string]string{
		"Content-Security-Policy":    "default-src 'self'",
		"Cross-Origin-Opener-Policy": "same-origin",
		"Permissions-Policy":         "camera=(), geolocation=(), microphone=()",
		"Referrer-Policy":            "no-referrer",
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
	}
	for _, testCase := range []struct {
		name      string
		path      string
		accept    string
		wantCache string
	}{
		{name: "frontend html", path: "/accounts", accept: "text/html", wantCache: "no-cache"},
		{name: "api", path: "/api/health", wantCache: "no-store"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			if testCase.accept != "" {
				req.Header.Set("Accept", testCase.accept)
			}
			res := httptest.NewRecorder()
			srv.Handler().ServeHTTP(res, req)

			if cacheControl := res.Header().Get("Cache-Control"); cacheControl != testCase.wantCache {
				t.Errorf("Cache-Control = %q, want %q", cacheControl, testCase.wantCache)
			}
			for name, expected := range headers {
				if value := res.Header().Get(name); !strings.Contains(value, expected) {
					t.Errorf("%s = %q, want it to contain %q", name, value, expected)
				}
			}
		})
	}
}

func TestAPIResponsesDisableBrowserCaching(t *testing.T) {
	srv := newTestServer(t, "")
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/accounts"},
		{method: http.MethodPost, path: "/api/accounts", body: `{}`},
		{method: http.MethodPost, path: "/api/accounts/missing/password", body: `{}`},
		{method: http.MethodPut, path: "/api/accounts/missing/cookies", body: `{}`},
		{method: http.MethodPost, path: "/api/accounts/missing/login/start", body: `{}`},
		{method: http.MethodPost, path: "/api/accounts/missing/login/verify", body: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}
			req := httptest.NewRequest(tt.method, tt.path, body)
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			res := httptest.NewRecorder()

			srv.Handler().ServeHTTP(res, req)

			if got := res.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestHealthReturnsVersionAndConfigStatus(t *testing.T) {
	dataDir := t.TempDir()
	mgr, err := account.NewManager(dataDir)
	if err != nil {
		t.Fatalf("create account manager: %v", err)
	}
	srv := NewWithVersion(mgr, false, "", " v1.2.3 ")
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if got := res.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success {
		t.Fatalf("health response success = false: %s", res.Body.String())
	}
	want := map[string]any{
		"service":          serviceName,
		"version":          "v1.2.3",
		"status":           "ok",
		"config_available": true,
	}
	if len(body.Data) != len(want) {
		t.Errorf("health field count = %d, want %d; fields = %v", len(body.Data), len(want), body.Data)
	}
	for name, value := range want {
		if got := body.Data[name]; got != value {
			t.Errorf("health %s = %v, want %v", name, got, value)
		}
	}
	if strings.Contains(res.Body.String(), dataDir) {
		t.Errorf("health response contains data directory: %s", res.Body.String())
	}
}

func TestHealthReportsDegradedWithoutConfigDetails(t *testing.T) {
	dataDir := t.TempDir()
	mgr, err := account.NewManager(dataDir)
	if err != nil {
		t.Fatalf("create account manager: %v", err)
	}
	srv := NewWithVersion(mgr, false, "", "")
	configPath := filepath.Join(dataDir, "accounts.json")
	const secret = "secret-cookie-value"
	if err := os.WriteFile(configPath, []byte(`{"accounts":[{"cookies":{"session":"`+secret+`"}}`), 0600); err != nil {
		t.Fatalf("write invalid configuration: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var body struct {
		Success bool       `json:"success"`
		Data    healthData `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || body.Data.Status != "degraded" || body.Data.ConfigAvailable {
		t.Errorf("unexpected degraded response: %+v", body)
	}
	if body.Data.Version != defaultServiceVersion {
		t.Errorf("version = %q, want %q", body.Data.Version, defaultServiceVersion)
	}
	for _, sensitive := range []string{dataDir, configPath, secret, "解析", "accounts.json"} {
		if strings.Contains(res.Body.String(), sensitive) {
			t.Errorf("health response contains sensitive/internal value %q: %s", sensitive, res.Body.String())
		}
	}
}

func TestHealthRequiresConfiguredAPIToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	srv := newTestServer(t, token)

	unauthorizedRequests := []struct {
		name          string
		authorization string
	}{
		{name: "missing token"},
		{name: "invalid bearer token", authorization: "Bearer " + token + ".invalid"},
	}
	for _, tt := range unauthorizedRequests {
		t.Run(tt.name, func(t *testing.T) {
			unauthorized := httptest.NewRequest(http.MethodGet, "/api/health", nil)
			if tt.authorization != "" {
				unauthorized.Header.Set("Authorization", tt.authorization)
			}
			unauthorizedRes := httptest.NewRecorder()
			srv.Handler().ServeHTTP(unauthorizedRes, unauthorized)

			if unauthorizedRes.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", unauthorizedRes.Code, http.StatusUnauthorized)
			}
			if got := unauthorizedRes.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Errorf("WWW-Authenticate = %q, want Bearer", got)
			}
			var body apiResp
			if err := json.Unmarshal(unauthorizedRes.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Success || body.Code != apiTokenInvalidCode || body.Message == "" {
				t.Errorf("unexpected unauthorized response: %+v", body)
			}
		})
	}

	authorized := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	authorized.Header.Set("Authorization", "Bearer "+token)
	authorizedRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(authorizedRes, authorized)
	if authorizedRes.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want %d; body = %s", authorizedRes.Code, http.StatusOK, authorizedRes.Body.String())
	}
}

func TestRequestBodyLimit(t *testing.T) {
	payload := `{"name":"oversized","padding":"` + strings.Repeat("x", int(maxRequestBodyBytes)) + `"}`
	tests := []struct {
		name          string
		contentLength int64
	}{
		{name: "known content length", contentLength: int64(len(payload))},
		{name: "streamed body", contentLength: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, "")
			req := httptest.NewRequest(http.MethodPost, "/api/accounts", strings.NewReader(payload))
			req.ContentLength = tt.contentLength
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()

			srv.Handler().ServeHTTP(res, req)

			if res.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusRequestEntityTooLarge, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), fmt.Sprintf("%d", maxRequestBodyBytes)) {
				t.Errorf("response does not include body limit: %s", res.Body.String())
			}
			if accounts := srv.mgr.ListAccounts(); len(accounts) != 0 {
				t.Errorf("oversized request changed accounts: %+v", accounts)
			}
		})
	}
}

func TestParseBoundedInt(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "default", raw: "", want: 20},
		{name: "minimum", raw: "1", want: 1},
		{name: "maximum", raw: "100", want: 100},
		{name: "trim spaces", raw: " 25 ", want: 25},
		{name: "below minimum", raw: "0", wantErr: true},
		{name: "above maximum", raw: "101", wantErr: true},
		{name: "not an integer", raw: "many", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBoundedInt(tt.raw, 20, 1, 100)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseBoundedInt(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseBoundedInt(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestInboxQueryBounds(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		parameter string
	}{
		{name: "limit below minimum", query: "limit=0", parameter: "limit"},
		{name: "limit above maximum", query: "limit=101", parameter: "limit"},
		{name: "limit not integer", query: "limit=many", parameter: "limit"},
		{name: "days below minimum", query: "days=0", parameter: "days"},
		{name: "days above maximum", query: "days=366", parameter: "days"},
		{name: "days not integer", query: "days=many", parameter: "days"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, "")
			req := httptest.NewRequest(http.MethodGet, "/api/inbox?account_id=missing&"+tt.query, nil)
			res := httptest.NewRecorder()

			srv.Handler().ServeHTTP(res, req)

			if res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusBadRequest, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), tt.parameter) || !strings.Contains(res.Body.String(), "整数") {
				t.Errorf("response does not report %s boundary: %s", tt.parameter, res.Body.String())
			}
		})
	}
}

func TestIsSessionErrorRecognizesTrustChallenge(t *testing.T) {
	if !isSessionError("iCloud session trust is no longer valid (HTTP 421)") {
		t.Fatal("session trust challenge was not classified as a session error")
	}
}

func TestAliasLabelCharacterLimit(t *testing.T) {
	tests := []struct {
		name       string
		labelRunes int
		wantStatus int
	}{
		{name: "at limit", labelRunes: maxAliasLabelRunes, wantStatus: http.StatusNotFound},
		{name: "over limit", labelRunes: maxAliasLabelRunes + 1, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, "")
			body, err := json.Marshal(createReq{AccountID: "missing", Label: strings.Repeat("界", tt.labelRunes)})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/create", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()

			srv.Handler().ServeHTTP(res, req)

			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, tt.wantStatus, res.Body.String())
			}
			if tt.wantStatus == http.StatusBadRequest && !strings.Contains(res.Body.String(), "label 不能超过") {
				t.Errorf("response does not report label boundary: %s", res.Body.String())
			}
		})
	}
}

func TestUpdateCookiesCountLimit(t *testing.T) {
	cookies := make(map[string]string, account.MaxCookieCount+1)
	for i := 0; i <= account.MaxCookieCount; i++ {
		cookies[fmt.Sprintf("cookie-%03d", i)] = "value"
	}
	body, err := json.Marshal(updateCookiesReq{Cookies: cookies})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	srv := newTestServer(t, "")
	req := httptest.NewRequest(http.MethodPut, "/api/accounts/missing/cookies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusBadRequest, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), fmt.Sprintf("%d", account.MaxCookieCount)) {
		t.Errorf("response does not report Cookie count limit: %s", res.Body.String())
	}
}

func TestListAccountsReturnsSanitizedDTO(t *testing.T) {
	const (
		cookieName    = "X-SECRET-COOKIE"
		cookieValue   = "secret-cookie-value"
		appPassword   = "secret-app-password"
		proxyPassword = "secret-proxy-password"
		proxyURL      = "http://proxy-user:" + proxyPassword + "@proxy.example:8080"
	)

	dataDir := t.TempDir()
	config := `{
  "accounts": {
    "acc_safe": {
      "id": "acc_safe",
      "name": "主账号",
      "real_email": "user@example.com",
      "icloud_email": "user@icloud.com",
      "cookies": {"` + cookieName + `": "` + cookieValue + `"},
      "host": "icloud.com",
      "proxy": "` + proxyURL + `",
      "app_password": "` + appPassword + `",
      "status": "error",
      "alias_total": 10,
      "alias_active": 8,
      "last_validated": "2026-07-31T10:00:00+08:00",
      "last_error": "request failed with ` + cookieName + `=` + cookieValue + ` via ` + proxyURL + `",
      "created_at": "2026-07-31T09:00:00+08:00"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dataDir, "accounts.json"), []byte(config), 0600); err != nil {
		t.Fatalf("write account fixture: %v", err)
	}
	mgr, err := account.NewManager(dataDir)
	if err != nil {
		t.Fatalf("create account manager: %v", err)
	}
	srv := New(mgr, false, "")

	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var body struct {
		Success bool             `json:"success"`
		Data    []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || len(body.Data) != 1 {
		t.Fatalf("unexpected account response: %+v", body)
	}
	assertSafeAccountDTO(t, body.Data[0])
	if got := body.Data[0]["has_cookies"]; got != true {
		t.Errorf("has_cookies = %v, want true", got)
	}
	if got := body.Data[0]["has_app_password"]; got != true {
		t.Errorf("has_app_password = %v, want true", got)
	}
	if got := body.Data[0]["proxy_configured"]; got != true {
		t.Errorf("proxy_configured = %v, want true", got)
	}
	for _, secret := range []string{cookieName, cookieValue, appPassword, proxyPassword, proxyURL} {
		if strings.Contains(res.Body.String(), secret) {
			t.Errorf("response contains sensitive value %q: %s", secret, res.Body.String())
		}
	}
}

func TestAddAccountReturnsSanitizedDTO(t *testing.T) {
	const proxyURL = "http://proxy-user:secret-proxy-password@proxy.example:8080"
	srv := newTestServer(t, "")
	req := httptest.NewRequest(http.MethodPost, "/api/accounts", bytes.NewBufferString(`{
  "name": "新账号",
  "icloud_email": "  New.User@ICLOUD.COM  ",
  "host": "icloud.com",
  "proxy": "`+proxyURL+`"
}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var body struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success {
		t.Fatalf("unexpected account response: %+v", body)
	}
	assertSafeAccountDTO(t, body.Data)
	if got := body.Data["has_cookies"]; got != false {
		t.Errorf("has_cookies = %v, want false", got)
	}
	if got := body.Data["has_app_password"]; got != false {
		t.Errorf("has_app_password = %v, want false", got)
	}
	if got := body.Data["proxy_configured"]; got != true {
		t.Errorf("proxy_configured = %v, want true", got)
	}
	if got := body.Data["icloud_email"]; got != "New.User@icloud.com" {
		t.Errorf("icloud_email = %v, want New.User@icloud.com", got)
	}
	if got := body.Data["status"]; got != "pending" {
		t.Errorf("status = %v, want pending", got)
	}
	accounts := srv.mgr.ListAccounts()
	if len(accounts) != 1 || accounts[0].ICloudEmail != "New.User@icloud.com" {
		t.Errorf("persisted accounts = %+v, want normalized icloud_email", accounts)
	}
	if strings.Contains(res.Body.String(), proxyURL) || strings.Contains(res.Body.String(), "secret-proxy-password") {
		t.Errorf("response contains proxy credentials: %s", res.Body.String())
	}
}

func TestAddAccountRejectsInvalidICloudEmail(t *testing.T) {
	srv := newTestServer(t, "")
	req := httptest.NewRequest(http.MethodPost, "/api/accounts", bytes.NewBufferString(`{
  "name": "无效邮箱账号",
  "icloud_email": "user@icloud.com.evil.example"
}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusBadRequest, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "icloud_email") {
		t.Errorf("response does not describe icloud_email validation: %s", res.Body.String())
	}
	if accounts := srv.mgr.ListAccounts(); len(accounts) != 0 {
		t.Errorf("invalid request persisted accounts: %+v", accounts)
	}
}

func TestAddAccountReturnsInternalServerErrorWhenPersistenceFails(t *testing.T) {
	dataDir := t.TempDir()
	mgr, err := account.NewManager(dataDir)
	if err != nil {
		t.Fatalf("create account manager: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dataDir, "accounts.json"), 0700); err != nil {
		t.Fatalf("block accounts.json target: %v", err)
	}
	srv := New(mgr, false, "")
	req := httptest.NewRequest(http.MethodPost, "/api/accounts", bytes.NewBufferString(`{"name":"无法保存"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusInternalServerError, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "账户配置持久化失败") {
		t.Errorf("response does not report persistence failure: %s", res.Body.String())
	}
	if accounts := mgr.ListAccounts(); len(accounts) != 0 {
		t.Errorf("failed addition was not rolled back: %+v", accounts)
	}
}

func TestRemoveAccountReturnsInternalServerErrorAndRollsBack(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "accounts.json")
	config := `{"accounts":[{"id":"acc_remove","name":"待删除","host":"icloud.com","cookies":{},"status":"pending"}]}`
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatalf("write account fixture: %v", err)
	}
	mgr, err := account.NewManager(dataDir)
	if err != nil {
		t.Fatalf("create account manager: %v", err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatalf("remove account fixture: %v", err)
	}
	if err := os.Mkdir(configPath, 0700); err != nil {
		t.Fatalf("block accounts.json target: %v", err)
	}
	srv := New(mgr, false, "")
	req := httptest.NewRequest(http.MethodDelete, "/api/accounts/acc_remove", nil)
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusInternalServerError, res.Body.String())
	}
	accounts := mgr.ListAccounts()
	if len(accounts) != 1 || accounts[0].ID != "acc_remove" {
		t.Errorf("failed removal was not rolled back: %+v", accounts)
	}
}

func TestLoginStartCompletesWithoutTwoFactor(t *testing.T) {
	srv := newTestServer(t, "")
	wantAccount := account.AccountDTO{
		ID:         "acc_login",
		Name:       "登录账号",
		Status:     "active",
		HasCookies: true,
	}
	srv.startPasswordLogin = func(id, password string) (account.AccountDTO, *account.PasswordLoginSession, error) {
		if id != "acc_login" || password != "apple-password" {
			t.Errorf("StartPasswordLogin(%q, %q), want acc_login and supplied password", id, password)
		}
		return wantAccount, nil, nil
	}

	res := performJSONRequest(t, srv, http.MethodPost, "/api/accounts/acc_login/login/start", `{"password":"apple-password"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if got := res.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Success bool               `json:"success"`
		Data    account.AccountDTO `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || body.Data.ID != wantAccount.ID || body.Data.Status != wantAccount.Status {
		t.Errorf("unexpected direct login response: %+v", body)
	}
	if strings.Contains(res.Body.String(), "apple-password") {
		t.Errorf("response contains Apple password: %s", res.Body.String())
	}

	legacy := performJSONRequest(t, srv, http.MethodPost, "/api/accounts/acc_login/login", `{"password":"apple-password"}`)
	if legacy.Code != http.StatusNotFound {
		t.Errorf("legacy login status = %d, want %d", legacy.Code, http.StatusNotFound)
	}
}

func TestTwoStageLoginChallengeIsBoundAndSingleUse(t *testing.T) {
	srv := newTestServer(t, "")
	srv.loginChallenges.newID = func() (string, error) { return "challenge-one", nil }
	session := &account.PasswordLoginSession{}
	srv.startPasswordLogin = func(string, string) (account.AccountDTO, *account.PasswordLoginSession, error) {
		return account.AccountDTO{}, session, nil
	}
	verifyCalls := 0
	srv.verifyPasswordLogin = func(gotSession *account.PasswordLoginSession, otp string) (account.AccountDTO, error) {
		verifyCalls++
		if gotSession != session || otp != "123456" {
			t.Errorf("verify arguments = (%p, %q), want (%p, 123456)", gotSession, otp, session)
		}
		return account.AccountDTO{ID: "acc_login", Status: "active", HasCookies: true}, nil
	}

	start := performJSONRequest(t, srv, http.MethodPost, "/api/accounts/acc_login/login/start", `{"password":"apple-password"}`)
	if start.Code != http.StatusOK {
		t.Fatalf("start status = %d, want %d; body = %s", start.Code, http.StatusOK, start.Body.String())
	}
	var startBody struct {
		Success bool `json:"success"`
		Data    struct {
			Status      string `json:"status"`
			ChallengeID string `json:"challenge_id"`
			ExpiresIn   int    `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(start.Body.Bytes(), &startBody); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if !startBody.Success || startBody.Data.Status != "otp_required" || startBody.Data.ChallengeID != "challenge-one" || startBody.Data.ExpiresIn != 300 {
		t.Errorf("unexpected start response: %+v", startBody)
	}

	wrongAccount := performJSONRequest(t, srv, http.MethodPost, "/api/accounts/other/login/verify", `{"challenge_id":"challenge-one","otp_code":"123456"}`)
	if wrongAccount.Code != http.StatusGone {
		t.Errorf("wrong-account status = %d, want %d", wrongAccount.Code, http.StatusGone)
	}
	invalidOTP := performJSONRequest(t, srv, http.MethodPost, "/api/accounts/acc_login/login/verify", `{"challenge_id":"challenge-one","otp_code":"12 456"}`)
	if invalidOTP.Code != http.StatusBadRequest {
		t.Errorf("invalid-OTP status = %d, want %d", invalidOTP.Code, http.StatusBadRequest)
	}

	verify := performJSONRequest(t, srv, http.MethodPost, "/api/accounts/acc_login/login/verify", `{"challenge_id":"challenge-one","otp_code":"123456"}`)
	if verify.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want %d; body = %s", verify.Code, http.StatusOK, verify.Body.String())
	}
	if verifyCalls != 1 {
		t.Errorf("verify calls = %d, want 1", verifyCalls)
	}

	replay := performJSONRequest(t, srv, http.MethodPost, "/api/accounts/acc_login/login/verify", `{"challenge_id":"challenge-one","otp_code":"123456"}`)
	if replay.Code != http.StatusGone {
		t.Errorf("replay status = %d, want %d", replay.Code, http.StatusGone)
	}
	if verifyCalls != 1 {
		t.Errorf("replay called verifier; calls = %d", verifyCalls)
	}
}

func TestLoginChallengeExpires(t *testing.T) {
	srv := newTestServer(t, "")
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	srv.loginChallenges.now = func() time.Time { return now }
	srv.loginChallenges.newID = func() (string, error) { return "expiring-challenge", nil }
	srv.startPasswordLogin = func(string, string) (account.AccountDTO, *account.PasswordLoginSession, error) {
		return account.AccountDTO{}, &account.PasswordLoginSession{}, nil
	}
	verifyCalls := 0
	srv.verifyPasswordLogin = func(*account.PasswordLoginSession, string) (account.AccountDTO, error) {
		verifyCalls++
		return account.AccountDTO{}, nil
	}

	start := performJSONRequest(t, srv, http.MethodPost, "/api/accounts/acc_login/login/start", `{"password":"apple-password"}`)
	if start.Code != http.StatusOK {
		t.Fatalf("start status = %d, want %d; body = %s", start.Code, http.StatusOK, start.Body.String())
	}
	now = now.Add(loginChallengeTTL)
	verify := performJSONRequest(t, srv, http.MethodPost, "/api/accounts/acc_login/login/verify", `{"challenge_id":"expiring-challenge","otp_code":"123456"}`)
	if verify.Code != http.StatusGone {
		t.Fatalf("expired verify status = %d, want %d; body = %s", verify.Code, http.StatusGone, verify.Body.String())
	}
	if verifyCalls != 0 {
		t.Errorf("expired challenge called verifier %d times", verifyCalls)
	}
}

func TestLoginErrorsUseStableStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "invalid credentials", err: hme.ErrInvalidCredentials, wantStatus: http.StatusUnauthorized},
		{name: "missing account", err: account.ErrAccountNotFound, wantStatus: http.StatusNotFound},
		{name: "missing login email", err: account.ErrLoginEmailMissing, wantStatus: http.StatusBadRequest},
		{name: "privacy terms", err: hme.ErrPrivacyTermsRequired, wantStatus: http.StatusForbidden},
		{name: "persistence", err: account.ErrPersistence, wantStatus: http.StatusInternalServerError},
		{name: "upstream", err: errors.New("upstream unavailable"), wantStatus: http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, "")
			srv.startPasswordLogin = func(string, string) (account.AccountDTO, *account.PasswordLoginSession, error) {
				return account.AccountDTO{}, nil, tt.err
			}
			res := performJSONRequest(t, srv, http.MethodPost, "/api/accounts/acc_login/login/start", `{"password":"apple-password"}`)
			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, tt.wantStatus, res.Body.String())
			}
			if strings.Contains(res.Body.String(), "apple-password") {
				t.Errorf("error response contains Apple password: %s", res.Body.String())
			}
		})
	}
}

func performJSONRequest(t *testing.T, srv *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	return res
}

func assertSafeAccountDTO(t *testing.T, fields map[string]any) {
	t.Helper()
	want := map[string]struct{}{
		"id": {}, "name": {}, "real_email": {}, "icloud_email": {},
		"host": {}, "status": {}, "alias_total": {}, "alias_active": {},
		"last_validated": {}, "last_error": {}, "created_at": {},
		"has_cookies": {}, "has_app_password": {}, "proxy_configured": {},
	}
	if len(fields) != len(want) {
		t.Errorf("account DTO field count = %d, want %d; fields = %v", len(fields), len(want), fields)
	}
	for name := range want {
		if _, ok := fields[name]; !ok {
			t.Errorf("account DTO is missing field %q", name)
		}
	}
	for name := range fields {
		if _, ok := want[name]; !ok {
			t.Errorf("account DTO contains unexpected field %q", name)
		}
	}
}

func newTestServer(t *testing.T, token string) *Server {
	t.Helper()
	mgr, err := account.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("create account manager: %v", err)
	}
	return New(mgr, false, token)
}
