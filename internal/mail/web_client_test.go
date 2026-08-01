package mail

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	http "github.com/bogdanfinn/fhttp"
)

type webCookieWrite struct {
	url     string
	cookies map[string]string
}

type fakeWebHTTPClient struct {
	responses    []*http.Response
	requests     []*http.Request
	cookieWrites []webCookieWrite
}

func (f *fakeWebHTTPClient) Do(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return nil, fmt.Errorf("unexpected request: %s", req.URL)
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeWebHTTPClient) SetCookies(u *url.URL, cookies []*http.Cookie) {
	values := make(map[string]string, len(cookies))
	for _, cookie := range cookies {
		values[cookie.Name] = cookie.Value
	}
	f.cookieWrites = append(f.cookieWrites, webCookieWrite{url: u.String(), cookies: values})
}

func TestNewWebClientCopiesCookies(t *testing.T) {
	input := map[string]string{"session": "original"}
	client := NewWebClient(input, "dsid", "icloud.com")

	input["session"] = "caller-mutated"
	if got := client.cookies["session"]; got != "original" {
		t.Errorf("client session cookie = %q, want original", got)
	}
	client.cookies["client-only"] = "value"
	if _, exists := input["client-only"]; exists {
		t.Error("client mutation leaked into caller cookies")
	}
}

func TestWebClientAttachesCookiesToResolvedGateway(t *testing.T) {
	fake := &fakeWebHTTPClient{responses: []*http.Response{
		webJSONResponse(200, map[string]any{
			"webservices": map[string]any{
				"mccgateway": map[string]any{"url": "p999-mccgateway.icloud.com:443/"},
			},
		}),
		webJSONResponse(200, map[string]any{"success": true, "threadList": []any{}}),
	}}
	client := newWebClient(map[string]string{"session": "secret", "trust": "token"}, "12345", "icloud.com", fake)
	client.clientID = "fixed-client-id"

	messages, err := client.ListInbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("messages = %v, want empty", messages)
	}
	if client.mccGatewayURL != "https://p999-mccgateway.icloud.com" {
		t.Fatalf("gateway URL = %q", client.mccGatewayURL)
	}
	write, ok := findWebCookieWrite(fake.cookieWrites, client.mccGatewayURL)
	if !ok {
		t.Fatalf("dynamic gateway missing from cookie writes: %#v", fake.cookieWrites)
	}
	if write.cookies["session"] != "secret" || write.cookies["trust"] != "token" {
		t.Fatalf("gateway cookies = %#v", write.cookies)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(fake.requests))
	}
	if got := fake.requests[1].URL.Host; got != "p999-mccgateway.icloud.com" {
		t.Fatalf("search host = %q", got)
	}
	query := fake.requests[1].URL.Query()
	if query.Get("clientBuildNumber") != WebClientBuildNumber || query.Get("dsid") != "12345" {
		t.Fatalf("search query = %v", query)
	}
}

func TestWebClientRejectsUntrustedGatewayBeforeCookieAttachment(t *testing.T) {
	fake := &fakeWebHTTPClient{responses: []*http.Response{
		webJSONResponse(200, map[string]any{
			"webservices": map[string]any{
				"mccgateway": map[string]any{"url": "https://attacker.example:443"},
			},
		}),
	}}
	client := newWebClient(map[string]string{"session": "secret"}, "12345", "icloud.com", fake)

	_, err := client.ListInbox(10)
	if err == nil || !strings.Contains(err.Error(), "不受信任") {
		t.Fatalf("ListInbox() error = %v, want untrusted gateway error", err)
	}
	if _, exists := findWebCookieWrite(fake.cookieWrites, "https://attacker.example"); exists {
		t.Fatal("cookies attached to untrusted gateway")
	}
	if len(fake.requests) != 1 {
		t.Fatalf("request count = %d, want validate only", len(fake.requests))
	}
}

func TestWebClientMapsStableMessageFieldsNewestFirst(t *testing.T) {
	newTime := time.Date(2026, time.July, 31, 12, 30, 0, 0, time.UTC)
	oldTime := newTime.Add(-time.Hour)
	fake := newFakeWebMailClient(map[string]any{
		"success": true,
		"threadList": []any{
			map[string]any{
				"threadId":  "old",
				"subject":   "Old subject",
				"senders":   []string{"Old Sender <old@example.com>"},
				"preview":   "old preview",
				"timestamp": oldTime.UnixMilli(),
				"recipients": map[string]any{
					"to": []any{map[string]any{"address": "other@icloud.com"}},
				},
			},
			map[string]any{
				"threadId":  "new",
				"subject":   "New subject",
				"senders":   []string{"Sender <SENDER@example.com>"},
				"preview":   strings.Repeat("界", maxPreviewBytes),
				"timestamp": newTime.UnixMilli(),
				"toRecipients": []any{
					map[string]any{"emailAddress": "Alias@iCloud.com"},
					"cc@example.com",
				},
			},
		},
	})

	messages, err := fake.client.ListInbox(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].ID != "new" || messages[1].ID != "old" {
		t.Fatalf("message order = %#v", messages)
	}
	got := messages[0]
	if got.From != "Sender <SENDER@example.com>" {
		t.Errorf("from = %q", got.From)
	}
	if got.To != "alias@icloud.com, cc@example.com" {
		t.Errorf("to = %q", got.To)
	}
	if got.Date != "2026-07-31T12:30:00Z" {
		t.Errorf("date = %q", got.Date)
	}
	if len(got.Preview) > maxPreviewBytes {
		t.Errorf("preview bytes = %d, max %d", len(got.Preview), maxPreviewBytes)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "from", "to", "subject", "date", "preview"} {
		if _, exists := fields[field]; !exists {
			t.Errorf("stable message field %q is missing: %s", field, raw)
		}
	}
}

func TestFindByAliasUsesExactRecipientOnly(t *testing.T) {
	fake := newFakeWebMailClient(map[string]any{
		"success": true,
		"threadList": []any{
			map[string]any{
				"threadId": "match", "subject": "normal", "senders": []string{"sender@example.com"},
				"timestamp": 2, "to": []string{"Alias@iCloud.com"},
			},
			map[string]any{
				"threadId": "subject-only", "subject": "alias@icloud.com", "senders": []string{"alias@icloud.com"},
				"timestamp": 1, "to": []string{"other@icloud.com"},
			},
		},
	})

	messages, err := fake.client.FindByAlias("ALIAS@ICLOUD.COM", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != "match" {
		t.Fatalf("filtered messages = %#v", messages)
	}
}

func TestFindByAliasFailsWhenRecipientIsUnavailable(t *testing.T) {
	fake := newFakeWebMailClient(map[string]any{
		"success": true,
		"threadList": []any{
			map[string]any{
				"threadId": "unknown", "subject": "alias@icloud.com", "senders": []string{"sender@example.com"},
				"timestamp": 1,
			},
		},
	})

	_, err := fake.client.FindByAlias("alias@icloud.com", 10)
	if !errors.Is(err, ErrWebRecipientUnavailable) {
		t.Fatalf("FindByAlias() error = %v, want ErrWebRecipientUnavailable", err)
	}
}

type fakeWebMailClient struct {
	client *WebClient
	http   *fakeWebHTTPClient
}

func newFakeWebMailClient(searchResponse any) fakeWebMailClient {
	fakeHTTP := &fakeWebHTTPClient{responses: []*http.Response{
		webJSONResponse(200, map[string]any{
			"webservices": map[string]any{
				"mccgateway": map[string]any{"url": "https://p321-mccgateway.icloud.com:443"},
			},
		}),
		webJSONResponse(200, searchResponse),
	}}
	client := newWebClient(map[string]string{"session": "secret"}, "12345", "icloud.com", fakeHTTP)
	client.clientID = "fixed-client-id"
	return fakeWebMailClient{client: client, http: fakeHTTP}
}

func webJSONResponse(status int, value any) *http.Response {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(raw))),
	}
}

func findWebCookieWrite(writes []webCookieWrite, wantURL string) (webCookieWrite, bool) {
	for _, write := range writes {
		if strings.TrimRight(write.url, "/") == strings.TrimRight(wantURL, "/") {
			return write, true
		}
	}
	return webCookieWrite{}, false
}
