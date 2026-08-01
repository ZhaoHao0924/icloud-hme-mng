package account

import (
	"errors"
	"net/url"
	"strings"
)

const redactedAccountError = "账户验证失败，详细错误已脱敏"
const redactedAccountOperationError = "凭据操作失败，详细错误已脱敏"

// AccountDTO 是账户 API 使用的脱敏账户摘要。
type AccountDTO struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	RealEmail       string `json:"real_email"`
	ICloudEmail     string `json:"icloud_email"`
	Host            string `json:"host"`
	Status          string `json:"status"`
	AliasTotal      int    `json:"alias_total"`
	AliasActive     int    `json:"alias_active"`
	LastValidated   string `json:"last_validated"`
	LastError       string `json:"last_error"`
	CreatedAt       string `json:"created_at"`
	HasCookies      bool   `json:"has_cookies"`
	HasAppPassword  bool   `json:"has_app_password"`
	ProxyConfigured bool   `json:"proxy_configured"`
}

func newAccountDTO(acc *Account) AccountDTO {
	return AccountDTO{
		ID:              acc.ID,
		Name:            acc.Name,
		RealEmail:       acc.RealEmail,
		ICloudEmail:     acc.ICloudEmail,
		Host:            acc.Host,
		Status:          acc.Status,
		AliasTotal:      acc.AliasTotal,
		AliasActive:     acc.AliasActive,
		LastValidated:   acc.LastValidated,
		LastError:       safeAccountError(acc),
		CreatedAt:       acc.CreatedAt,
		HasCookies:      len(acc.Cookies) > 0,
		HasAppPassword:  strings.TrimSpace(acc.AppPassword) != "",
		ProxyConfigured: strings.TrimSpace(acc.Proxy) != "",
	}
}

func safeAccountError(acc *Account) string {
	if acc.LastError == "" {
		return ""
	}
	if containsAccountSecret(acc.LastError, accountSensitiveValues(acc)...) {
		return redactedAccountError
	}
	return acc.LastError
}

func safeAccountOperationError(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	if containsAccountSecret(err.Error(), secrets...) {
		return errors.New(redactedAccountOperationError)
	}
	return err
}

func accountSensitiveValues(acc *Account) []string {
	if acc == nil {
		return nil
	}
	sensitiveValues := []string{acc.AppPassword, acc.Proxy}
	for name, value := range acc.Cookies {
		sensitiveValues = append(sensitiveValues, name, value)
	}
	if proxyURL, err := url.Parse(acc.Proxy); err == nil && proxyURL.User != nil {
		if password, ok := proxyURL.User.Password(); ok {
			sensitiveValues = append(sensitiveValues, password)
		}
	}
	return sensitiveValues
}

func containsAccountSecret(message string, secrets ...string) bool {
	for _, secret := range secrets {
		if secret != "" && strings.Contains(message, secret) {
			return true
		}
	}
	return false
}
