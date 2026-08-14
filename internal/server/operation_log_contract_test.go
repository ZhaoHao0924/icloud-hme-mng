package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"icloud-hme/internal/auditlog"
	"icloud-hme/internal/mail"
)

func TestQA009AuditContractForSuccessfulOperation(t *testing.T) {
	srv := newTestServer(t, "")
	const clientRequestID = "qa009-client-request-id"
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	req.Header.Set("X-Request-ID", clientRequestID)
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("accounts status = %d, want %d", res.Code, http.StatusOK)
	}
	requestID := res.Header().Get("X-Request-ID")
	if len(requestID) != 32 || requestID == clientRequestID {
		t.Errorf("response request ID = %q", requestID)
	}
	entry := qa009OnlyAuditEntry(t, srv)
	if entry.SchemaVersion != auditlog.SchemaVersion || entry.RequestID != requestID || entry.OperationType != "accounts" {
		t.Errorf("successful audit identity = %+v", entry)
	}
	if entry.ErrorCode != "" || entry.RetryCount != 0 || entry.Level != auditlog.LevelInfo {
		t.Errorf("successful audit outcome = %+v", entry)
	}
	if entry.Request.Source != auditlog.RequestSourceAPI || entry.Request.BodyPresent || entry.Request.AliasFilterApplied || entry.Request.PaginationRequested {
		t.Errorf("successful request snapshot = %+v", entry.Request)
	}
	if !entry.Response.Success || entry.Response.CreatedCount != 0 || entry.Response.FailedCount != 0 {
		t.Errorf("successful response snapshot = %+v", entry.Response)
	}
}

func TestQA009AuditContractRedactsValidationRequestData(t *testing.T) {
	srv := newTestServer(t, "")
	const (
		accountID = "qa009-private-account"
		cookie    = "qa009-private-cookie"
		token     = "qa009-private-api-token"
		password  = "qa009-private-proxy-password"
		requestID = "qa009-client-request-id"
	)
	body := `{"icloud_email":"` + accountID + `@example.test","cookies":"` + cookie + `","proxy":"http://user:` + password + `@proxy.example.test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/accounts", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "qa009_session="+cookie)
	req.Header.Set("X-Request-ID", requestID)
	res := httptest.NewRecorder()

	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("validation status = %d, want %d", res.Code, http.StatusBadRequest)
	}
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("validation Cache-Control = %q", res.Header().Get("Cache-Control"))
	}
	entry := qa009OnlyAuditEntry(t, srv)
	if entry.OperationType != "accounts" || entry.ErrorCode != auditlog.ErrorCodeValidationFailed || entry.Level != auditlog.LevelWarning {
		t.Errorf("validation audit outcome = %+v", entry)
	}
	if entry.RequestID == requestID || !entry.Request.BodyPresent || entry.Request.AliasFilterApplied || entry.Request.PaginationRequested {
		t.Errorf("validation audit snapshots = request:%+v response:%+v", entry.Request, entry.Response)
	}
	if entry.Response.Success {
		t.Errorf("validation response success = true")
	}

	logsReq := httptest.NewRequest(http.MethodGet, "/api/logs?limit=10", nil)
	logsRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(logsRes, logsReq)
	if logsRes.Code != http.StatusOK || logsRes.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("logs API status/cache = %d/%q", logsRes.Code, logsRes.Header().Get("Cache-Control"))
	}
	qa009AssertNoSensitiveValue(t, []string{
		res.Body.String(),
		logsRes.Body.String(),
		qa009AuditFileContent(t, srv),
	}, accountID, cookie, token, password, requestID)
}

func TestQA009AuditContractClassifiesAndRedactsUpstreamFailures(t *testing.T) {
	const upstreamSecret = "qa009-private-upstream-response"
	tests := []struct {
		name          string
		err           error
		wantErrorCode string
		wantStatus    int
	}{
		{
			name:          "apple upstream rejection",
			err:           errors.New(upstreamSecret),
			wantErrorCode: auditlog.ErrorCodeUpstreamRejected,
			wantStatus:    http.StatusBadGateway,
		},
		{
			name:          "apple upstream timeout",
			err:           fmt.Errorf("%s: %w", upstreamSecret, context.DeadlineExceeded),
			wantErrorCode: auditlog.ErrorCodeUpstreamTimeout,
			wantStatus:    http.StatusGatewayTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, "")
			srv.newInboxIMAPClient = func(string) (inboxIMAPClient, error) {
				return &qa009InboxIMAPClient{fullErr: tt.err}, nil
			}
			req := httptest.NewRequest(http.MethodGet, "/api/inbox/messages/7?account_id=qa009-private-account", nil)
			res := httptest.NewRecorder()

			srv.Handler().ServeHTTP(res, req)

			if res.Code != tt.wantStatus {
				t.Fatalf("inbox message status = %d, want %d", res.Code, tt.wantStatus)
			}
			entry := qa009OnlyAuditEntry(t, srv)
			if entry.OperationType != "inbox_messages_id" || entry.ErrorCode != tt.wantErrorCode || entry.Level != auditlog.LevelError {
				t.Errorf("upstream audit outcome = %+v", entry)
			}
			if entry.Request.Source != auditlog.RequestSourceAPI || entry.Request.BodyPresent || entry.Request.AliasFilterApplied || entry.Request.PaginationRequested || entry.Response.Success {
				t.Errorf("upstream audit snapshots = request:%+v response:%+v", entry.Request, entry.Response)
			}
			qa009AssertNoSensitiveValue(t, []string{res.Body.String(), qa009AuditFileContent(t, srv)}, upstreamSecret, "qa009-private-account")
		})
	}
}

func qa009OnlyAuditEntry(t *testing.T, srv *Server) auditlog.Entry {
	t.Helper()
	entries, err := srv.operationLogs.List(10)
	if err != nil {
		t.Fatalf("list operation logs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("operation log count = %d, want 1", len(entries))
	}
	return entries[0]
}

func qa009AuditFileContent(t *testing.T, srv *Server) string {
	t.Helper()
	dir := filepath.Join(srv.mgr.DataDir(), "operation-logs")
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read operation log directory: %v", err)
	}
	var content strings.Builder
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			t.Fatalf("read operation log file: %v", err)
		}
		content.Write(raw)
	}
	return content.String()
}

func qa009AssertNoSensitiveValue(t *testing.T, surfaces []string, sensitiveValues ...string) {
	t.Helper()
	for _, surface := range surfaces {
		for _, value := range sensitiveValues {
			if strings.Contains(surface, value) {
				t.Fatal("sensitive request or upstream data leaked into an audited surface")
			}
		}
	}
}

type qa009InboxIMAPClient struct {
	fullErr error
}

func (c *qa009InboxIMAPClient) Connect() error { return nil }

func (c *qa009InboxIMAPClient) Disconnect() {}

func (c *qa009InboxIMAPClient) FindByRecipientPage(string, int, int, uint32) (mail.MessagePage, error) {
	return mail.MessagePage{}, nil
}

func (c *qa009InboxIMAPClient) FindByRecipientSummariesPage(string, int, int, uint32) (mail.MessagePage, error) {
	return mail.MessagePage{}, nil
}

func (c *qa009InboxIMAPClient) GetFull(uint32) (*mail.FullMessage, error) {
	return nil, c.fullErr
}

func (c *qa009InboxIMAPClient) GetPreview(uint32) (*mail.Message, error) {
	return nil, nil
}

func (c *qa009InboxIMAPClient) ListInboxPage(int, int, uint32) (mail.MessagePage, error) {
	return mail.MessagePage{}, nil
}

func (c *qa009InboxIMAPClient) ListInboxSummariesPage(int, int, uint32) (mail.MessagePage, error) {
	return mail.MessagePage{}, nil
}
