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
	"icloud-hme/internal/auditlog"
	"icloud-hme/internal/hme"
	"icloud-hme/internal/mail"
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

func TestPlatformAuthSetupLoginAndLogoutProtectBusinessRoutes(t *testing.T) {
	srv := newPlatformAuthTestServer(t, "")

	blocked := httptest.NewRecorder()
	srv.Handler().ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))
	assertPlatformAuthFailure(t, blocked, platformAuthSetupRequiredCode)

	statusBeforeSetup := httptest.NewRecorder()
	srv.Handler().ServeHTTP(statusBeforeSetup, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))
	if statusBeforeSetup.Code != http.StatusOK {
		t.Fatalf("session before setup status = %d, body = %s", statusBeforeSetup.Code, statusBeforeSetup.Body.String())
	}
	if status := decodePlatformAuthStatus(t, statusBeforeSetup); status.Configured || status.Authenticated {
		t.Fatalf("status before setup = %+v, want unconfigured and unauthenticated", status)
	}

	const username = "admin"
	const password = "correct-horse-battery-staple"
	setup := httptest.NewRecorder()
	setupReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/setup",
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`),
	)
	setupReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(setup, setupReq)
	if setup.Code != http.StatusOK {
		t.Fatalf("setup status = %d, body = %s", setup.Code, setup.Body.String())
	}
	if strings.Contains(setup.Body.String(), password) {
		t.Fatalf("setup response leaked password: %s", setup.Body.String())
	}
	setupStatus := decodePlatformAuthStatus(t, setup)
	if !setupStatus.Configured || !setupStatus.Authenticated || setupStatus.Username != username {
		t.Fatalf("setup status = %+v", setupStatus)
	}
	setupCookie := requiredPlatformSessionCookie(t, setup)
	setCookie := setup.Header().Get("Set-Cookie")
	for _, expected := range []string{"HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(setCookie, expected) {
			t.Errorf("Set-Cookie = %q, want %q", setCookie, expected)
		}
	}

	authorized := httptest.NewRecorder()
	authorizedReq := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	authorizedReq.AddCookie(setupCookie)
	srv.Handler().ServeHTTP(authorized, authorizedReq)
	if authorized.Code != http.StatusOK {
		t.Fatalf("session-authenticated accounts status = %d, body = %s", authorized.Code, authorized.Body.String())
	}

	logout := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.AddCookie(setupCookie)
	srv.Handler().ServeHTTP(logout, logoutReq)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status = %d, body = %s", logout.Code, logout.Body.String())
	}
	if !strings.Contains(logout.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("logout Set-Cookie = %q, want expiry", logout.Header().Get("Set-Cookie"))
	}

	revoked := httptest.NewRecorder()
	revokedReq := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	revokedReq.AddCookie(setupCookie)
	srv.Handler().ServeHTTP(revoked, revokedReq)
	assertPlatformAuthFailure(t, revoked, platformAuthRequiredCode)

	invalidLogin := httptest.NewRecorder()
	invalidLoginReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/login",
		strings.NewReader(`{"username":"`+username+`","password":"incorrect-password"}`),
	)
	invalidLoginReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(invalidLogin, invalidLoginReq)
	assertPlatformAuthFailure(t, invalidLogin, platformAuthInvalidCode)

	login := httptest.NewRecorder()
	loginReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/login",
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`),
	)
	loginReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(login, loginReq)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	if strings.Contains(login.Body.String(), password) {
		t.Fatalf("login response leaked password: %s", login.Body.String())
	}
	if !decodePlatformAuthStatus(t, login).Authenticated {
		t.Fatalf("login status = %s", login.Body.String())
	}
}

func TestPlatformAuthAllowsExistingBearerTokenAndProtectsRemoteSetup(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	srv := newPlatformAuthTestServer(t, token)

	invalidBearerBusinessRequest := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	invalidBearerBusinessRequest.Header.Set("Authorization", "Bearer invalid-token")
	invalidBearerBusinessResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(invalidBearerBusinessResponse, invalidBearerBusinessRequest)
	if invalidBearerBusinessResponse.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer business request status = %d, body = %s", invalidBearerBusinessResponse.Code, invalidBearerBusinessResponse.Body.String())
	}
	var invalidBearer apiResp
	if err := json.Unmarshal(invalidBearerBusinessResponse.Body.Bytes(), &invalidBearer); err != nil {
		t.Fatalf("decode invalid bearer business response: %v", err)
	}
	if invalidBearer.Code != apiTokenInvalidCode {
		t.Fatalf("invalid bearer business response code = %q, want %q", invalidBearer.Code, apiTokenInvalidCode)
	}

	setupWithoutToken := httptest.NewRecorder()
	setupWithoutTokenReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/setup",
		strings.NewReader(`{"username":"admin","password":"correct-horse-battery-staple"}`),
	)
	setupWithoutTokenReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(setupWithoutToken, setupWithoutTokenReq)
	if setupWithoutToken.Code != http.StatusUnauthorized {
		t.Fatalf("setup without token status = %d, body = %s", setupWithoutToken.Code, setupWithoutToken.Body.String())
	}
	var unauthorized apiResp
	if err := json.Unmarshal(setupWithoutToken.Body.Bytes(), &unauthorized); err != nil {
		t.Fatalf("decode setup without token response: %v", err)
	}
	if unauthorized.Code != apiTokenInvalidCode {
		t.Fatalf("setup without token code = %q, want %q", unauthorized.Code, apiTokenInvalidCode)
	}

	setupWithToken := httptest.NewRecorder()
	setupWithTokenReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/setup",
		strings.NewReader(`{"username":"admin","password":"correct-horse-battery-staple"}`),
	)
	setupWithTokenReq.Header.Set("Authorization", "Bearer "+token)
	setupWithTokenReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(setupWithToken, setupWithTokenReq)
	if setupWithToken.Code != http.StatusOK {
		t.Fatalf("setup with token status = %d, body = %s", setupWithToken.Code, setupWithToken.Body.String())
	}

	bearerRequest := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	bearerRequest.Header.Set("Authorization", "Bearer "+token)
	bearerResponse := httptest.NewRecorder()
	srv.Handler().ServeHTTP(bearerResponse, bearerRequest)
	if bearerResponse.Code != http.StatusOK {
		t.Fatalf("bearer accounts status = %d, body = %s", bearerResponse.Code, bearerResponse.Body.String())
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
		"Content-Security-Policy":    "style-src 'self' 'unsafe-inline'",
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
		{method: http.MethodGet, path: "/api/logs"},
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

func TestOperationLogsAreAvailableAndDoNotPersistRequestValues(t *testing.T) {
	srv := newTestServer(t, "")
	const accountID = "account-private-value"
	const alias = "private-alias@example.test"
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/inbox?account_id="+accountID+"&alias="+alias,
		nil,
	)
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("inbox status = %d, want %d; body = %s", res.Code, http.StatusBadRequest, res.Body.String())
	}

	entries, err := srv.operationLogs.List(10)
	if err != nil {
		t.Fatalf("list operation logs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("log count = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Operation != "读取收件箱" || entry.Level != auditlog.LevelWarning || entry.Status != res.Code {
		t.Errorf("operation log = %+v", entry)
	}
	if entry.SchemaVersion != auditlog.SchemaVersion || entry.RequestID == "" || entry.OperationType != "inbox" || entry.ErrorCode != auditlog.ErrorCodeValidationFailed {
		t.Errorf("operation log audit contract = %+v", entry)
	}
	if entry.Request.Source != auditlog.RequestSourceAPI || entry.Request.BodyPresent || !entry.Request.AliasFilterApplied || entry.Request.PaginationRequested || entry.Response.Success {
		t.Errorf("operation log snapshots = request:%+v response:%+v", entry.Request, entry.Response)
	}

	logsReq := httptest.NewRequest(http.MethodGet, "/api/logs?limit=10", nil)
	logsRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(logsRes, logsReq)
	if logsRes.Code != http.StatusOK {
		t.Fatalf("logs status = %d, want %d; body = %s", logsRes.Code, http.StatusOK, logsRes.Body.String())
	}
	if strings.Contains(logsRes.Body.String(), accountID) || strings.Contains(logsRes.Body.String(), alias) {
		t.Errorf("logs response contains request value: %s", logsRes.Body.String())
	}
	entriesAfterRead, err := srv.operationLogs.List(10)
	if err != nil {
		t.Fatalf("list operation logs after read: %v", err)
	}
	if len(entriesAfterRead) != len(entries) {
		t.Errorf("viewing logs recorded another entry: before=%d after=%d", len(entries), len(entriesAfterRead))
	}
}

func Test163EmailNotificationAPIStoresOnlyAProtectedAuthorizationCode(t *testing.T) {
	srv := newTestServer(t, "")
	get := httptest.NewRecorder()
	srv.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/notifications/email", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("initial notification status = %d, body = %s", get.Code, get.Body.String())
	}
	if strings.Contains(get.Body.String(), "authorization_code") {
		t.Fatalf("initial notification response exposed authorization_code: %s", get.Body.String())
	}

	const authorizationCode = "163-test-authorization-code"
	update := httptest.NewRecorder()
	updateReq := httptest.NewRequest(
		http.MethodPut,
		"/api/notifications/email",
		strings.NewReader(`{"enabled":true,"sender_email":"sender@163.com","authorization_code":"`+authorizationCode+`","recipient_email":"recipient@qq.com"}`),
	)
	updateReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(update, updateReq)
	if update.Code != http.StatusOK {
		t.Fatalf("update notification status = %d, body = %s", update.Code, update.Body.String())
	}
	if strings.Contains(update.Body.String(), authorizationCode) || strings.Contains(update.Body.String(), "authorization_code") {
		t.Fatalf("update response exposed authorization code: %s", update.Body.String())
	}
	if !strings.Contains(update.Body.String(), "sender@163.com") {
		t.Fatalf("update response omitted 163 sender: %s", update.Body.String())
	}

	invalid := httptest.NewRecorder()
	invalidReq := httptest.NewRequest(
		http.MethodPut,
		"/api/notifications/email",
		strings.NewReader(`{"enabled":true,"sender_email":"sender@qq.com","authorization_code":"secret","recipient_email":"recipient@qq.com"}`),
	)
	invalidReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(invalid, invalidReq)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid notification status = %d, want %d; body = %s", invalid.Code, http.StatusBadRequest, invalid.Body.String())
	}
}

func TestWebhookNotificationAPIStoresOnlyRedactedConfiguration(t *testing.T) {
	srv := newTestServer(t, "")
	get := httptest.NewRecorder()
	srv.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/notifications/webhook", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("initial webhook status = %d, body = %s", get.Code, get.Body.String())
	}
	if strings.Contains(get.Body.String(), "secret") {
		t.Fatalf("initial webhook response exposed secret: %s", get.Body.String())
	}

	const secret = "webhook-api-secret"
	update := httptest.NewRecorder()
	updateReq := httptest.NewRequest(
		http.MethodPut,
		"/api/notifications/webhook",
		strings.NewReader(`{"enabled":true,"url":"https://hooks.example.test/icloud","secret":"`+secret+`"}`),
	)
	updateReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(update, updateReq)
	if update.Code != http.StatusOK {
		t.Fatalf("update webhook status = %d, body = %s", update.Code, update.Body.String())
	}
	if strings.Contains(update.Body.String(), secret) || strings.Contains(update.Body.String(), "secret") {
		t.Fatalf("update webhook response exposed secret: %s", update.Body.String())
	}

	invalid := httptest.NewRecorder()
	invalidReq := httptest.NewRequest(
		http.MethodPut,
		"/api/notifications/webhook",
		strings.NewReader(`{"enabled":true,"url":"http://hooks.example.test/icloud","secret":"secret"}`),
	)
	invalidReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(invalid, invalidReq)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid webhook status = %d, want %d; body = %s", invalid.Code, http.StatusBadRequest, invalid.Body.String())
	}
}

func TestScheduledAliasAutomationRunIsWrittenToOperationLog(t *testing.T) {
	srv := newTestServer(t, "")
	srv.recordScheduledAliasAutomationRun(aliasAutomationScheduledRun{
		Created:  2,
		Duration: 125 * time.Millisecond,
		Failed:   1,
		Status:   account.AliasAutomationStatusPartial,
	})

	entries, err := srv.operationLogs.List(10)
	if err != nil {
		t.Fatalf("list operation logs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("log count = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Operation != "定时执行别名自动化" || entry.Level != auditlog.LevelWarning || entry.Status != http.StatusPartialContent || entry.DurationMS != 125 {
		t.Errorf("scheduled automation log = %+v", entry)
	}
	if entry.SchemaVersion != auditlog.SchemaVersion || entry.RequestID == "" || entry.OperationType != "scheduled_alias_automation" || entry.ErrorCode != auditlog.ErrorCodePartialResult {
		t.Errorf("scheduled automation audit contract = %+v", entry)
	}
	if entry.Request.Source != auditlog.RequestSourceScheduler || entry.Request.BodyPresent || entry.Response.CreatedCount != 2 || entry.Response.FailedCount != 1 || !entry.Response.Success {
		t.Errorf("scheduled automation snapshots = request:%+v response:%+v", entry.Request, entry.Response)
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
		{name: "cursor zero", query: "before_uid=0", parameter: "before_uid"},
		{name: "cursor not integer", query: "before_uid=older", parameter: "before_uid"},
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

func TestInboxListReturnsIMAPCursor(t *testing.T) {
	srv := newTestServer(t, "")
	client := &fakeInboxIMAPClient{
		listSummaries:              []mail.Message{{ID: "1042", Subject: "newest"}, {ID: "1041", Subject: "older"}},
		listSummariesNextBeforeUID: 1041,
	}
	srv.newInboxIMAPClient = func(accountID string) (inboxIMAPClient, error) {
		if accountID != "acc_main" {
			t.Errorf("account ID = %q, want acc_main", accountID)
		}
		return client, nil
	}
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/inbox?account_id=acc_main&include_preview=false&before_uid=1043",
		nil,
	)
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if client.lastListSummariesBeforeUID != 1043 {
		t.Errorf("received cursor = %d, want 1043", client.lastListSummariesBeforeUID)
	}
	var body struct {
		Data struct {
			Count      int    `json:"count"`
			HasMore    bool   `json:"has_more"`
			Method     string `json:"method"`
			NextCursor string `json:"next_cursor"`
		} `json:"data"`
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || body.Data.Count != 2 || !body.Data.HasMore || body.Data.NextCursor != "1041" {
		t.Errorf("page response = %+v", body)
	}
	if body.Data.Method != "imap" {
		t.Errorf("method = %q, want imap", body.Data.Method)
	}
}

func TestInboxRejectsInvalidPreviewModes(t *testing.T) {
	for _, parameter := range []string{"include_preview", "first_preview"} {
		t.Run(parameter, func(t *testing.T) {
			srv := newTestServer(t, "")
			req := httptest.NewRequest(
				http.MethodGet,
				"/api/inbox?account_id=missing&"+parameter+"=maybe",
				nil,
			)
			res := httptest.NewRecorder()

			srv.Handler().ServeHTTP(res, req)

			if res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusBadRequest, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), parameter) {
				t.Errorf("response does not report invalid %s mode: %s", parameter, res.Body.String())
			}
		})
	}
}

func TestInboxMessageValidatesAccountAndUID(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "missing account", target: "/api/inbox/messages/7", want: "account_id"},
		{name: "invalid UID", target: "/api/inbox/messages/not-a-uid?account_id=missing", want: "UID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, "")
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			res := httptest.NewRecorder()

			srv.Handler().ServeHTTP(res, req)

			if res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusBadRequest, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), tt.want) {
				t.Errorf("response = %s, want %q", res.Body.String(), tt.want)
			}
		})
	}
}

func TestLoadFirstInboxPreviewFetchesOnceAndCaches(t *testing.T) {
	srv := newTestServer(t, "")
	messages := []mail.Message{{ID: "7", Subject: "summary"}}
	loader := &stubInboxPreviewLoader{message: &mail.Message{
		ID:      "7",
		Subject: "summary",
		Preview: "selected message body",
	}}

	srv.loadFirstInboxPreview("acc_main", messages, loader)

	if loader.calls != 1 || loader.lastUID != 7 {
		t.Fatalf("preview loader = %d calls for UID %d, want one call for UID 7", loader.calls, loader.lastUID)
	}
	if messages[0].Preview != "selected message body" {
		t.Errorf("first preview = %q", messages[0].Preview)
	}
	if cached, found := srv.inboxPreviews.Get("acc_main", "7"); !found || cached.Preview != "selected message body" {
		t.Errorf("cached preview = (%+v, %v)", cached, found)
	}

	messages[0].Preview = ""
	srv.loadFirstInboxPreview("acc_main", messages, loader)
	if loader.calls != 1 {
		t.Fatalf("cached preview made %d loader calls, want 1", loader.calls)
	}
	if messages[0].Preview != "selected message body" {
		t.Errorf("cached first preview = %q", messages[0].Preview)
	}
}

func TestLoadFirstInboxPreviewFailureKeepsSummary(t *testing.T) {
	srv := newTestServer(t, "")
	messages := []mail.Message{{ID: "7", Subject: "summary"}}
	loader := &stubInboxPreviewLoader{err: errors.New("preview unavailable")}

	srv.loadFirstInboxPreview("acc_main", messages, loader)

	if messages[0].Subject != "summary" || messages[0].Preview != "" {
		t.Errorf("summary changed after preview failure: %+v", messages[0])
	}
	if _, found := srv.inboxPreviews.Get("acc_main", "7"); found {
		t.Fatal("failed preview was cached")
	}
}

func TestInboxMessageReturnsCachedFullMessageWithoutAccountLookup(t *testing.T) {
	srv := newTestServer(t, "")
	srv.inboxPreviews.SetFull("acc_cached", mail.FullMessage{
		Message:     mail.Message{ID: "7", Preview: "cached body"},
		Body:        `<p>cached <a href="https://example.test">body</a></p>`,
		ContentType: "text/html",
	})
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/inbox/messages/007?account_id=acc_cached",
		nil,
	)
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "content_type") || !strings.Contains(res.Body.String(), "https://example.test") {
		t.Errorf("response does not contain cached full message: %s", res.Body.String())
	}
}

func TestInboxMessageLoadsAndCachesFullMessage(t *testing.T) {
	srv := newTestServer(t, "")
	client := &fakeInboxIMAPClient{fullMessage: &mail.FullMessage{
		Message:     mail.Message{ID: "7", Subject: "HTML message", Preview: "Open account"},
		Body:        `<a href="https://example.test/account">Open account</a>`,
		ContentType: "text/html",
	}}
	srv.newInboxIMAPClient = func(accountID string) (inboxIMAPClient, error) {
		if accountID != "acc_main" {
			t.Errorf("account ID = %q, want acc_main", accountID)
		}
		return client, nil
	}

	for range 2 {
		req := httptest.NewRequest(
			http.MethodGet,
			"/api/inbox/messages/7?account_id=acc_main",
			nil,
		)
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
		}
		if !strings.Contains(res.Body.String(), "text/html") || !strings.Contains(res.Body.String(), "example.test/account") {
			t.Errorf("response does not contain full HTML message: %s", res.Body.String())
		}
	}
	if client.getFullCalls != 1 || client.lastFullUID != 7 {
		t.Fatalf("GetFull calls = %d for UID %d, want one call for UID 7", client.getFullCalls, client.lastFullUID)
	}
}

func TestInboxListReusesAccountIMAPSession(t *testing.T) {
	srv := newTestServer(t, "")
	client := &fakeInboxIMAPClient{
		listSummaries: []mail.Message{{ID: "7", Subject: "summary"}},
	}
	factoryCalls := 0
	srv.newInboxIMAPClient = func(accountID string) (inboxIMAPClient, error) {
		if accountID != "acc_main" {
			t.Errorf("account ID = %q, want acc_main", accountID)
		}
		factoryCalls++
		return client, nil
	}

	for range 2 {
		req := httptest.NewRequest(
			http.MethodGet,
			"/api/inbox?account_id=acc_main&include_preview=false&first_preview=false",
			nil,
		)
		res := httptest.NewRecorder()
		srv.Handler().ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
		}
	}

	if factoryCalls != 1 || client.connectCalls != 1 || client.listSummariesCalls != 2 {
		t.Fatalf(
			"factory calls = %d, connect calls = %d, list calls = %d; want 1, 1, 2",
			factoryCalls,
			client.connectCalls,
			client.listSummariesCalls,
		)
	}
	srv.Close()
	if client.disconnectCalls != 1 {
		t.Fatalf("disconnect calls = %d, want 1 on server close", client.disconnectCalls)
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
	srv := New(mgr, false, token)
	t.Cleanup(srv.Close)
	return srv
}

func newPlatformAuthTestServer(t *testing.T, token string) *Server {
	t.Helper()
	mgr, err := account.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("create account manager: %v", err)
	}
	srv, err := NewWithPlatformAuth(mgr, false, token, "test")
	if err != nil {
		t.Fatalf("create platform auth server: %v", err)
	}
	t.Cleanup(srv.Close)
	return srv
}

func assertPlatformAuthFailure(t *testing.T, res *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", res.Code, http.StatusUnauthorized, res.Body.String())
	}
	var body apiResp
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode platform auth response: %v", err)
	}
	if body.Success || body.Code != wantCode || body.Message == "" {
		t.Fatalf("platform auth response = %+v, want code %q", body, wantCode)
	}
}

func decodePlatformAuthStatus(t *testing.T, res *httptest.ResponseRecorder) platformAuthStatus {
	t.Helper()
	var body struct {
		Data    platformAuthStatus `json:"data"`
		Success bool               `json:"success"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode platform auth status: %v", err)
	}
	if !body.Success {
		t.Fatalf("platform auth status success = false: %s", res.Body.String())
	}
	return body.Data
}

func requiredPlatformSessionCookie(t *testing.T, res *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == platformSessionCookieName {
			return cookie
		}
	}
	t.Fatalf("response has no %q cookie: %v", platformSessionCookieName, res.Header().Values("Set-Cookie"))
	return nil
}

type stubInboxPreviewLoader struct {
	calls   int
	err     error
	lastUID uint32
	message *mail.Message
}

func (s *stubInboxPreviewLoader) GetPreview(uid uint32) (*mail.Message, error) {
	s.calls++
	s.lastUID = uid
	return s.message, s.err
}
