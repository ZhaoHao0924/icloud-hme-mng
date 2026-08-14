package server

import (
	"context"
	"strings"
	"testing"

	"icloud-hme/internal/upstream"
)

func TestClassifyAPIErrorMatrix(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantStatus int
		retryable bool
	}{
		{name: "session", err: upstream.ClassifyResponse(401, nil), wantCode: apiErrorSessionExpired, wantStatus: 401},
		{name: "generic 403", err: upstream.ClassifyResponse(403, nil), wantCode: apiErrorUpstreamRejected, wantStatus: 502},
		{name: "entitlement", err: upstream.ClassifyResponse(403, []byte(`{"error":{"code":"ICLOUD_PLUS_REQUIRED"}}`)), wantCode: apiErrorEntitlementRequired, wantStatus: 403},
		{name: "alias limit", err: upstream.ClassifyResponse(507, nil), wantCode: apiErrorAliasLimit, wantStatus: 409},
		{name: "rate limit", err: upstream.ClassifyResponse(429, nil), wantCode: apiErrorRateLimited, wantStatus: 429},
		{name: "trust", err: upstream.ClassifyResponse(421, []byte(`{"trustTokens":["cookie-secret"]}`)), wantCode: apiErrorDeviceTrust, wantStatus: 403},
		{name: "state", err: upstream.ClassifyResponse(409, nil), wantCode: apiErrorStateConflict, wantStatus: 409},
		{name: "service", err: upstream.ClassifyResponse(504, nil), wantCode: apiErrorServiceUnavailable, wantStatus: 504, retryable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract, status, _ := classifyAPIError(tt.err)
			if contract.Code != tt.wantCode || status != tt.wantStatus || contract.Retryable != tt.retryable {
				t.Fatalf("classifyAPIError() = %+v, status %d; want code %q status %d retryable %v", contract, status, tt.wantCode, tt.wantStatus, tt.retryable)
			}
			if strings.Contains(contract.Message, "cookie-secret") || strings.Contains(contract.Message, "Cookie") && tt.wantCode != apiErrorSessionExpired {
				t.Fatalf("contract message exposed sensitive or misleading data: %q", contract.Message)
			}
		})
	}
}

func TestClassifyAPIErrorTimeoutIsRetryable(t *testing.T) {
	contract, status, _ := classifyAPIError(context.DeadlineExceeded)
	if contract.Code != apiErrorServiceUnavailable || status != 504 || !contract.Retryable {
		t.Fatalf("timeout contract = %+v status %d", contract, status)
	}
}

func TestIsSessionErrorDoesNotTreatForbiddenAsExpiry(t *testing.T) {
	if isSessionError("HTTP 403") {
		t.Fatal("generic HTTP 403 was classified as a session expiry")
	}
	if isSessionError("iCloud session trust is no longer valid (HTTP 421)") {
		t.Fatal("device trust challenge was classified as Cookie expiry")
	}
}
