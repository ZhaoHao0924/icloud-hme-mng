package account

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAccountDTOContainsOnlySafeFields(t *testing.T) {
	const (
		cookieName    = "X-SECRET-COOKIE"
		cookieValue   = "secret-cookie-value"
		appPassword   = "secret-app-password"
		proxyPassword = "secret-proxy-password"
		proxyURL      = "http://proxy-user:" + proxyPassword + "@proxy.example:8080"
	)

	acc := &Account{
		ID:            "acc_safe",
		Name:          "主账号",
		RealEmail:     "user@example.com",
		ICloudEmail:   "user@icloud.com",
		Cookies:       map[string]string{cookieName: cookieValue},
		Host:          "icloud.com",
		Proxy:         proxyURL,
		AppPassword:   appPassword,
		Status:        "error",
		AliasTotal:    10,
		AliasActive:   8,
		LastValidated: "2026-07-31T10:00:00+08:00",
		LastError:     "request failed with " + cookieName + "=" + cookieValue + " via " + proxyURL,
		CreatedAt:     "2026-07-31T09:00:00+08:00",
	}

	raw, err := json.Marshal(newAccountDTO(acc))
	if err != nil {
		t.Fatalf("marshal account DTO: %v", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode account DTO: %v", err)
	}
	assertAccountDTOFields(t, fields)

	for _, secret := range []string{cookieName, cookieValue, appPassword, proxyPassword, proxyURL} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("account DTO contains sensitive value %q: %s", secret, raw)
		}
	}
	if got := fields["has_cookies"]; got != true {
		t.Errorf("has_cookies = %v, want true", got)
	}
	if got := fields["has_app_password"]; got != true {
		t.Errorf("has_app_password = %v, want true", got)
	}
	if got := fields["proxy_configured"]; got != true {
		t.Errorf("proxy_configured = %v, want true", got)
	}
	if got := fields["last_error"]; got != redactedAccountError {
		t.Errorf("last_error = %v, want %q", got, redactedAccountError)
	}
}

func TestAccountDTOKeepsSafeLastError(t *testing.T) {
	const message = "Cookie 校验失败: HTTP 401"
	dto := newAccountDTO(&Account{LastError: message})
	if dto.LastError != message {
		t.Fatalf("last_error = %q, want %q", dto.LastError, message)
	}
}

func TestAccountOperationErrorRedactsApplePassword(t *testing.T) {
	const applePassword = "secret-apple-password"
	err := safeAccountOperationError(
		errors.New("upstream login failed with "+applePassword),
		applePassword,
	)
	if err.Error() != redactedAccountOperationError {
		t.Fatalf("error = %q, want %q", err, redactedAccountOperationError)
	}
	if strings.Contains(err.Error(), applePassword) {
		t.Fatalf("error contains Apple password: %q", err)
	}
}

func TestAccountOperationErrorKeepsSafeMessage(t *testing.T) {
	want := errors.New("用户名或密码错误")
	if got := safeAccountOperationError(want); got != want {
		t.Fatalf("error = %q, want original error", got)
	}
}

func assertAccountDTOFields(t *testing.T, fields map[string]any) {
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
