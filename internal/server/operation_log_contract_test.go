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
	req := httptest.NewRequest(http.MethodGet, "/api/accounts?include=all%20accounts", nil)
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
	if entry.Request.Source != auditlog.RequestSourceAPI || entry.Request.Method != http.MethodGet || entry.Request.Path != "/api/accounts" || entry.Request.RawQuery != "include=all%20accounts" || len(entry.Request.PathParams) != 0 || entry.Request.BodyPresent || entry.Request.AliasFilterApplied || entry.Request.PaginationRequested {
		t.Errorf("successful request snapshot = %+v", entry.Request)
	}
	if entry.Request.Body.Present || entry.Request.Body.Value != "" {
		t.Errorf("successful request body = %+v", entry.Request.Body)
	}
	if !entry.Response.Success || entry.Response.CreatedCount != 0 || entry.Response.FailedCount != 0 || !entry.Response.Body.Present || entry.Response.Body.Encoding != auditlog.PayloadEncodingUTF8 || entry.Response.Body.Value != res.Body.String() {
		t.Errorf("successful response snapshot = %+v", entry.Response)
	}
}

func TestQA009AuditContractPersistsValidationRequestAndResponseData(t *testing.T) {
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
	if entry.RequestID == requestID || entry.Request.Method != http.MethodPost || entry.Request.Path != "/api/accounts" || entry.Request.RawQuery != "" || len(entry.Request.PathParams) != 0 || !entry.Request.BodyPresent || entry.Request.AliasFilterApplied || entry.Request.PaginationRequested {
		t.Errorf("validation audit snapshots = request:%+v response:%+v", entry.Request, entry.Response)
	}
	if entry.Request.Body.ContentType != "application/json" || entry.Request.Body.Encoding != auditlog.PayloadEncodingUTF8 || entry.Request.Body.Value != body {
		t.Errorf("validation request body = %+v", entry.Request.Body)
	}
	if entry.Response.Success || entry.Response.Body.Encoding != auditlog.PayloadEncodingUTF8 || entry.Response.Body.Value != res.Body.String() {
		t.Errorf("validation response snapshot = %+v", entry.Response)
	}

	logsReq := httptest.NewRequest(http.MethodGet, "/api/logs?limit=10", nil)
	logsRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(logsRes, logsReq)
	if logsRes.Code != http.StatusOK || logsRes.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("logs API status/cache = %d/%q", logsRes.Code, logsRes.Header().Get("Cache-Control"))
	}
	qa009AssertContainsValues(t, []string{
		logsRes.Body.String(),
		qa009AuditFileContent(t, srv),
	}, accountID, cookie, password)
	qa009AssertNoValue(t, []string{
		res.Body.String(),
		logsRes.Body.String(),
		qa009AuditFileContent(t, srv),
	}, token, requestID)
	entriesAfterRead, err := srv.operationLogs.List(10)
	if err != nil {
		t.Fatalf("list operation logs after read: %v", err)
	}
	if len(entriesAfterRead) != 1 {
		t.Fatalf("reading operation logs created another entry: %d", len(entriesAfterRead))
	}
}

func TestQA009AuditContractClassifiesUpstreamFailuresAndCapturesAPIResponse(t *testing.T) {
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
			if entry.Request.Source != auditlog.RequestSourceAPI || entry.Request.Method != http.MethodGet || entry.Request.Path != "/api/inbox/messages/7" || entry.Request.RawQuery != "account_id=qa009-private-account" || entry.Request.PathParams["id"] != "7" || entry.Request.BodyPresent || entry.Request.AliasFilterApplied || entry.Request.PaginationRequested || entry.Response.Success {
				t.Errorf("upstream audit snapshots = request:%+v response:%+v", entry.Request, entry.Response)
			}
			if entry.Response.Body.Encoding != auditlog.PayloadEncodingUTF8 || entry.Response.Body.Value != res.Body.String() {
				t.Errorf("upstream management API response body = %+v", entry.Response.Body)
			}
			qa009AssertContainsValues(t, []string{qa009AuditFileContent(t, srv)}, "qa009-private-account")
			qa009AssertNoValue(t, []string{res.Body.String(), qa009AuditFileContent(t, srv)}, upstreamSecret)
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

func qa009AssertContainsValues(t *testing.T, surfaces []string, values ...string) {
	t.Helper()
	for _, surface := range surfaces {
		for _, value := range values {
			if !strings.Contains(surface, value) {
				t.Fatalf("operation log surface does not contain %q", value)
			}
		}
	}
}

func qa009AssertNoValue(t *testing.T, surfaces []string, values ...string) {
	t.Helper()
	for _, surface := range surfaces {
		for _, value := range values {
			if strings.Contains(surface, value) {
				t.Fatalf("operation log surface unexpectedly contains %q", value)
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
