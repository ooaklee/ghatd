package serverhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/emailmanager"
	emailhelper "github.com/ooaklee/ghatd/external/emailmanager/helper"
	"github.com/ooaklee/ghatd/external/emailprovider"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

type reviewTransport struct{ calls int }

func (t *reviewTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("external delivery forbidden")
}

type remoteReviewProvider struct{ emailprovider.EmailProvider }

func reviewManager(t *testing.T) (*emailmanager.EmailManager, *reviewTransport) {
	t.Helper()
	transport := &reviewTransport{}
	services, err := emailhelper.BuildServices(emailhelper.Config{CaptureLocally: true, Providers: []emailhelper.ProviderConfig{{ID: "bird", Vendor: "bird", Preferences: []emailprovider.MailType{emailprovider.Transactional}}, {ID: "postmark", Vendor: "postmark", MarketingStream: "broadcasts", Preferences: []emailprovider.MailType{emailprovider.Marketing}}}, HTTPClient: &http.Client{Transport: transport}})
	require.NoError(t, err)
	manager, err := emailmanager.NewStandardEmailManager(&emailmanager.NewStandardEmailManagerRequest{Provider: services.Default, Routing: services.Routing, FrontendBaseURL: "http://localhost:4000", EmailVerificationFullEndpoint: "http://localhost:4000/verify", DashboardVerificationURIPath: "http://localhost:4000/verify", Environment: "local", BusinessEntityName: "Example", BusinessEntityWebsite: "https://example.test", FromEmailAddress: "from@example.test", NoReplyEmailAddress: "from@example.test", LoginEmailSubject: "Login", WelcomeEmailSubject: "Verify", Config: &emailmanager.Config{ShouldSendEmail: true}})
	require.NoError(t, err)
	return manager, transport
}

func TestEmailReviewCapturesFixedMessagesOnly(t *testing.T) {
	for _, process := range []string{"login", "verification", "transactional", "marketing", "rendered"} {
		t.Run(process, func(t *testing.T) {
			manager, transport := reviewManager(t)
			routes := router.NewRouter(nil, nil)
			require.NoError(t, AttachLocalEmailReview(routes, EmailReviewConfig{Environment: "local", FrontendOrigin: "http://localhost:4000", FromAddress: "from@example.test", Manager: manager}))
			r := httptest.NewRequest(http.MethodPost, "http://localhost:4000/_ghatd/local/emails/review", strings.NewReader(url.Values{"process": {process}, "to": {"attacker@example.test"}, "body": {"private-canary"}, "provider": {"attacker-provider"}}.Encode()))
			r.RemoteAddr = "127.0.0.1:1234"
			r.Header.Set("Origin", "http://localhost:4000")
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, r)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "captured")
			require.NotContains(t, w.Body.String(), "private-canary")
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Zero(t, transport.calls)
		})
	}
}

func TestEmailReviewRejectsRemotePeersAndOrigins(t *testing.T) {
	for _, tc := range []struct {
		name, peer, origin, body string
		status                   int
	}{
		{"remote peer", "192.0.2.1:1234", "http://localhost:4000", "process=login", 403},
		{"missing origin", "127.0.0.1:1234", "", "process=login", 403},
		{"public origin", "127.0.0.1:1234", "https://public.example.test", "process=login", 403},
		{"suffix origin", "127.0.0.1:1234", "http://localhost.attacker.test:4000", "process=login", 403},
		{"wrong port", "127.0.0.1:1234", "http://localhost:5000", "process=login", 403},
		{"credential origin", "127.0.0.1:1234", "http://user:secret@localhost:4000", "process=login", 403},
		{"unknown process", "127.0.0.1:1234", "http://localhost:4000", "process=other", 400},
		{"oversized body", "127.0.0.1:1234", "http://localhost:4000", "process=login&extra=" + strings.Repeat("a", 2048), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager, transport := reviewManager(t)
			routes := router.NewRouter(nil, nil)
			require.NoError(t, AttachLocalEmailReview(routes, EmailReviewConfig{Environment: "local", FrontendOrigin: "http://localhost:4000", Manager: manager}))
			r := httptest.NewRequest(http.MethodPost, "http://localhost:4000/_ghatd/local/emails/review", strings.NewReader(tc.body))
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code)
			require.Zero(t, transport.calls)
		})
	}
}

func TestEmailReviewRequiresLocalCapturedManager(t *testing.T) {
	for _, name := range []string{"nil routes", "nil manager", "production", "remote manager"} {
		t.Run(name, func(t *testing.T) {
			manager, _ := reviewManager(t)
			routes := router.NewRouter(nil, nil)
			original := routes
			cfg := EmailReviewConfig{Environment: "local", Manager: manager}
			switch name {
			case "nil routes":
				routes = nil
			case "nil manager":
				cfg.Manager = nil
			case "production":
				cfg.Environment = "production"
			case "remote manager":
				cfg.Manager = emailmanager.NewEmailManager(nil, &remoteReviewProvider{}, nil, nil)
			}
			require.Error(t, AttachLocalEmailReview(routes, cfg))
			w := httptest.NewRecorder()
			original.GetRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/_ghatd/local/emails/review", nil))
			require.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func TestEmailReviewPrivateNetworkRequiresExplicitPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, peer, origin string
		allowPrivate       bool
		status             int
	}{
		{"private denied by default", "172.18.0.1:1234", "http://localhost:4000", false, 403},
		{"container peer opted in", "172.18.0.1:1234", "http://localhost:4000", true, 200},
		{"private still requires origin", "192.168.1.2:1234", "", true, 403},
		{"public never allowed", "192.0.2.1:1234", "http://localhost:4000", true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager, transport := reviewManager(t)
			routes := router.NewRouter(nil, nil)
			require.NoError(t, AttachLocalEmailReview(routes, EmailReviewConfig{Environment: "local", FrontendOrigin: "http://localhost:4000", Manager: manager, AllowPrivateNetwork: tc.allowPrivate}))
			r := httptest.NewRequest(http.MethodPost, "http://localhost:4000/_ghatd/local/emails/review", strings.NewReader("process=login"))
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code)
			require.Zero(t, transport.calls)
		})
	}
}
