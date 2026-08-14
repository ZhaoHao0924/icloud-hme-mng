package upstream

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyResponseMatrix(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantKind  Kind
		retryable bool
	}{
		{name: "confirmed session expiry", status: 401, wantKind: KindSessionExpired},
		{name: "generic forbidden is unknown", status: 403, wantKind: KindUnknownRejected},
		{name: "entitlement code", status: 403, body: `{"error":{"errorCode":"ICLOUD_PLUS_REQUIRED"}}`, wantKind: KindEntitlementRequired},
		{name: "alias total limit", status: 403, body: `{"error":{"code":"HME_ALIAS_LIMIT_REACHED"}}`, wantKind: KindAliasLimitReached},
		{name: "daily limit", status: 403, body: `{"errorCode":"HME_DAILY_QUOTA_EXCEEDED"}`, wantKind: KindAliasDailyLimit},
		{name: "rate limit", status: 429, wantKind: KindRateLimited},
		{name: "device trust", status: 421, body: `{"trustTokens":["secret-token"]}`, wantKind: KindDeviceTrustRequired},
		{name: "state conflict", status: 409, wantKind: KindStateConflict},
		{name: "validation", status: 422, wantKind: KindValidationFailed},
		{name: "service unavailable", status: 504, wantKind: KindServiceUnavailable, retryable: true},
		{name: "structured service unavailable", status: 403, body: `{"code":"TEMPORARY_UNAVAILABLE"}`, wantKind: KindServiceUnavailable, retryable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyResponse(tt.status, []byte(tt.body))
			if got.Kind != tt.wantKind || got.Retryable != tt.retryable {
				t.Fatalf("ClassifyResponse() = kind %q retryable %v, want %q %v", got.Kind, got.Retryable, tt.wantKind, tt.retryable)
			}
			if strings.Contains(got.Error(), "secret-token") {
				t.Fatal("classified error exposed an upstream token")
			}
		})
	}
}

func TestTransportErrorKeepsCauseWithoutExposingIt(t *testing.T) {
	secret := errors.New("cookie=secret-token")
	classified := TransportError(secret)
	if classified.Error() != "iCloud upstream request failed" {
		t.Fatalf("TransportError() = %q", classified.Error())
	}
	if !errors.Is(classified, secret) {
		t.Fatal("transport cause was not preserved for server-side classification")
	}
	if strings.Contains(classified.Error(), "secret-token") {
		t.Fatal("transport error exposed the cause")
	}
}
