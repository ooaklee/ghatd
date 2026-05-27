package emailprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/router"
)

func TestAttachLocalInboxRoutes(t *testing.T) {
	provider := NewLoggingEmailProvider(&LoggingEmailProviderConfig{
		TimeProvider: func() time.Time {
			return time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
		},
	})
	result, err := provider.Send(context.Background(), &Email{
		To:       "dev@example.com",
		From:     "noreply@example.com",
		Subject:  "Login",
		HTMLBody: `<html><body><a href="https://app.example.com/login?t=token">Sign in</a></body></html>`,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	httpRouter := router.NewRouter(nil, nil)
	if err := AttachLocalInboxRoutes(&AttachLocalInboxRoutesRequest{
		Router:      httpRouter,
		Provider:    provider,
		Prefix:      "/dev/emails",
		AllowRemote: true,
	}); err != nil {
		t.Fatalf("AttachLocalInboxRoutes() error = %v", err)
	}

	index := httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/dev/emails", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("index status = %d, want %d", index.Code, http.StatusOK)
	}
	if got := index.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("index Cache-Control = %q, want no-store", got)
	}
	if body := index.Body.String(); !strings.Contains(body, "GHATD Local Email Inbox") || !strings.Contains(body, result.MessageID) {
		t.Fatalf("index body did not include inbox title and message ID: %s", body)
	}

	detail := httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/dev/emails/"+result.MessageID, nil))
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d", detail.Code, http.StatusOK)
	}
	if body := detail.Body.String(); !strings.Contains(body, "Open rendered email") || !strings.Contains(body, `sandbox=""`) || !strings.Contains(body, "https://app.example.com/login?t=token") {
		t.Fatalf("detail body did not include rendered action and link: %s", body)
	}

	raw := httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(raw, httptest.NewRequest(http.MethodGet, "/dev/emails/"+result.MessageID+"/html", nil))
	if raw.Code != http.StatusOK {
		t.Fatalf("raw status = %d, want %d", raw.Code, http.StatusOK)
	}
	if got := raw.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("raw Cache-Control = %q, want no-store", got)
	}
	if got := raw.Header().Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") || strings.Contains(got, "allow-scripts") {
		t.Fatalf("raw Content-Security-Policy = %q, want sandbox without script allowance", got)
	}
	if body := raw.Body.String(); !strings.Contains(body, `<a href="https://app.example.com/login?t=token">`) {
		t.Fatalf("raw body did not include original HTML: %s", body)
	}

	api := httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(api, httptest.NewRequest(http.MethodGet, "/dev/emails/api/emails", nil))
	if api.Code != http.StatusOK {
		t.Fatalf("api status = %d, want %d", api.Code, http.StatusOK)
	}
	if got := api.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("api Cache-Control = %q, want no-store", got)
	}
	var summaries []localInboxEmailSummary
	if err := json.Unmarshal(api.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("api JSON error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].MessageID != result.MessageID {
		t.Fatalf("api summaries = %+v, want captured message", summaries)
	}
}

func TestExtractEmailLinksOnlyIncludesWebLinks(t *testing.T) {
	got := extractEmailLinks(`
		<a href="https://app.example.com/login?t=token&amp;next=/dashboard">Sign in</a>
		<a href='http://localhost:3000/auth?code=123'>Code</a>
		<a href="/local/auth?token=abc">Relative</a>
		<a href="javascript:alert(1)">Script</a>
		<a href="mailto:support@example.com">Mail</a>
		<a href="//example.com/protocol-relative">Protocol relative</a>
		<a href="https://app.example.com/login?t=token&amp;next=/dashboard">Duplicate</a>
	`)
	want := []string{
		"https://app.example.com/login?t=token&next=/dashboard",
		"http://localhost:3000/auth?code=123",
		"/local/auth?token=abc",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractEmailLinks() = %#v, want %#v", got, want)
	}
}

func TestAttachLocalInboxRoutesRejectsRemoteByDefault(t *testing.T) {
	provider := NewLoggingEmailProvider(nil)
	httpRouter := router.NewRouter(nil, nil)
	if err := AttachLocalInboxRoutes(&AttachLocalInboxRoutesRequest{
		Router:   httpRouter,
		Provider: provider,
	}); err != nil {
		t.Fatalf("AttachLocalInboxRoutes() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, DefaultLocalInboxRoutePrefix, nil)
	request.RemoteAddr = "203.0.113.10:52222"
	response := httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("remote status = %d, want %d", response.Code, http.StatusForbidden)
	}

	request = httptest.NewRequest(http.MethodGet, DefaultLocalInboxRoutePrefix, nil)
	request.RemoteAddr = "127.0.0.1:52222"
	response = httptest.NewRecorder()
	httpRouter.GetRouter().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("localhost status = %d, want %d", response.Code, http.StatusOK)
	}
}
