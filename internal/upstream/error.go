// Package upstream defines the privacy-safe classification shared by Apple
// service clients. It intentionally retains no upstream response body.
package upstream

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// Kind is a stable, user-safe category for an Apple upstream failure.
type Kind string

const (
	KindSessionExpired       Kind = "session_expired"
	KindEntitlementRequired  Kind = "entitlement_required"
	KindAliasLimitReached    Kind = "alias_limit_reached"
	KindAliasDailyLimit      Kind = "alias_daily_limit_reached"
	KindRateLimited          Kind = "rate_limited"
	KindDeviceTrustRequired  Kind = "device_trust_required"
	KindValidationFailed     Kind = "validation_failed"
	KindStateConflict        Kind = "state_conflict"
	KindServiceUnavailable   Kind = "service_unavailable"
	KindUnknownRejected      Kind = "unknown_rejected"
)

// Error carries only safe metadata. The source response body is deliberately
// excluded because Apple payloads can contain session and trust material.
type Error struct {
	Status    int
	Kind      Kind
	Retryable bool
	cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return "iCloud upstream request failed"
	}
	if e.Kind == KindDeviceTrustRequired && e.Status > 0 {
		// Keep this established diagnostic stable without exposing token data.
		return fmt.Sprintf("iCloud session trust is no longer valid (HTTP %d)", e.Status)
	}
	if e.Status > 0 {
		return fmt.Sprintf("HTTP %d", e.Status)
	}
	return "iCloud upstream request failed"
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// New returns a classified upstream error with no source payload attached.
func New(status int, kind Kind, retryable bool) *Error {
	return &Error{Status: status, Kind: kind, Retryable: retryable}
}

// TransportError represents a connection or local timeout while reaching
// Apple. The original cause remains unwrap-able for server-side auditing only.
func TransportError(cause error) *Error {
	return &Error{Kind: KindServiceUnavailable, Retryable: true, cause: cause}
}

// ClassifyResponse maps HTTP status and allowlisted structured error codes to
// a stable category. A bare 403 is intentionally left as unknown_rejected.
func ClassifyResponse(status int, body []byte) *Error {
	kind := classify(status, structuredErrorCode(body), hasTrustTokens(body))
	return New(status, kind, kind == KindServiceUnavailable)
}

func classify(status int, code string, trustChallenge bool) Kind {
	if trustChallenge {
		return KindDeviceTrustRequired
	}

	if kind, found := kindForStructuredCode(code); found {
		return kind
	}

	switch status {
	case 401:
		return KindSessionExpired
	case 408, 425, 500, 502, 503, 504:
		return KindServiceUnavailable
	case 409:
		return KindStateConflict
	case 429:
		return KindRateLimited
	case 507:
		return KindAliasLimitReached
	case 400, 422:
		return KindValidationFailed
	default:
		return KindUnknownRejected
	}
}

func kindForStructuredCode(code string) (Kind, bool) {
	if code == "" {
		return "", false
	}
	containsAny := func(parts ...string) bool {
		for _, part := range parts {
			if strings.Contains(code, part) {
				return true
			}
		}
		return false
	}

	switch {
	case containsAny("SESSION_EXPIRED", "SESSION_INVALID", "AUTH_SESSION_EXPIRED"):
		return KindSessionExpired, true
	case containsAny("ICLOUD_PLUS", "ICLOUDPLUS", "ENTITLEMENT", "PREMIUM_REQUIRED", "NOT_ELIGIBLE", "HME_NOT_AVAILABLE"):
		return KindEntitlementRequired, true
	case containsAny("DAILY_LIMIT", "DAILY_QUOTA"):
		return KindAliasDailyLimit, true
	case containsAny("ALIAS_LIMIT", "HME_LIMIT", "ALIAS_QUOTA", "HME_QUOTA", "ALIAS_CAPACITY"):
		return KindAliasLimitReached, true
	case containsAny("RATE_LIMIT", "THROTTL", "TOO_MANY", "RISK_LIMIT", "RISK_CONTROL"):
		return KindRateLimited, true
	case containsAny("TRUST", "TWO_FACTOR", "2FA", "DEVICE_VERIFICATION"):
		return KindDeviceTrustRequired, true
	case containsAny("CONFLICT", "ALREADY_EXISTS", "STATE_CONFLICT"):
		return KindStateConflict, true
	case containsAny("VALIDATION", "INVALID_REQUEST", "INVALID_PARAMETER"):
		return KindValidationFailed, true
	case containsAny("UNAVAILABLE", "TIMEOUT", "TEMPORARY"):
		return KindServiceUnavailable, true
	default:
		return "", false
	}
}

func hasTrustTokens(body []byte) bool {
	var payload struct {
		TrustTokens json.RawMessage `json:"trustTokens"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	tokens := strings.TrimSpace(string(payload.TrustTokens))
	return tokens != "" && tokens != "null" && tokens != "[]"
}

func structuredErrorCode(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	for _, path := range [][]string{
		{"error", "errorCode"},
		{"error", "code"},
		{"error", "reason"},
		{"errorCode"},
		{"code"},
		{"reason"},
	} {
		if value, found := nestedString(payload, path...); found {
			return normalizeCode(value)
		}
	}
	return ""
}

func nestedString(value map[string]any, path ...string) (string, bool) {
	var current any = value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = object[part]
		if !ok {
			return "", false
		}
	}
	result, ok := current.(string)
	return result, ok
}

func normalizeCode(value string) string {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" || len(value) > 120 {
		return ""
	}
	for _, r := range value {
		if !(unicode.IsUpper(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			return ""
		}
	}
	return value
}
