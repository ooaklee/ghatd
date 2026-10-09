package accessmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/stretchr/testify/require"
)

type mobileTestStore struct {
	starts  int
	payload []byte
}

func (s *mobileTestStore) SaveStart(_ context.Context, _ string, payload []byte, _ time.Duration) error {
	s.starts++
	s.payload = payload
	return nil
}
func (s *mobileTestStore) ConsumeStart(context.Context, string) ([]byte, error) {
	return s.payload, nil
}
func (*mobileTestStore) SaveGrant(context.Context, string, string, string, string, []byte, time.Duration) error {
	return nil
}
func (*mobileTestStore) ConsumeGrant(context.Context, string, string, string, string) ([]byte, error) {
	return nil, oauth.ErrSecureTransactionNotFound
}

type mobileTestService struct {
	AccessmanagerService
	starts int
}

func (*mobileTestService) OAuthProviders() []string { return []string{"google", "apple"} }
func (*mobileTestService) mobileOAuthLinkProof(context.Context, string) (*oauth.LinkProof, error) {
	return nil, ErrOAuthReauthenticationRequired
}
func (*mobileTestService) completeMobileOAuth(context.Context, *mobileOAuthGrant, string) (*OauthCallbackResponse, error) {
	panic("unexpected exchange")
}
func (s *mobileTestService) OauthLogin(context.Context, *OauthLoginRequest) (*OauthLoginResponse, error) {
	s.starts++
	return &OauthLoginResponse{CookieCore: &http.Cookie{Name: "state", Value: "test"}, ProviderAuthCodeUrl: "https://accounts.google.com/authorize"}, nil
}

func TestMobileOAuthDiscovery(t *testing.T) {
	type discoveryCase struct {
		name    string
		enabled bool
	}
	for _, test := range []discoveryCase{{"disabled", false}, {"enabled", true}} {
		t.Run(test.name, func(t *testing.T) {
			h := &Handler{Service: &mobileTestService{}}
			if test.enabled {
				require.NoError(t, h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: []string{"com.example.client:/oauth/callback"}, Store: &mobileTestStore{}}))
			}
			out := httptest.NewRecorder()
			h.MobileOAuthProviders(out, httptest.NewRequest(http.MethodGet, "/", nil))
			require.Equal(t, 200, out.Code)
			if test.enabled {
				require.Contains(t, out.Body.String(), `"providers":["google","apple"]`)
				require.Contains(t, out.Body.String(), `"redirect_uris":["com.example.client:/oauth/callback"]`)
			} else {
				require.Contains(t, out.Body.String(), `"providers":[]`)
				require.Contains(t, out.Body.String(), `"redirect_uris":[]`)
			}
		})
	}
}

func TestMobileOAuthInitiationRejectsAmbiguousAndBrowserRequests(t *testing.T) {
	body := `{"redirect_uri":"com.example.client:/oauth/callback","state":"` + strings.Repeat("s", 43) + `","code_challenge":"` + strings.Repeat("c", 43) + `","code_challenge_method":"S256"}`
	for _, test := range []struct {
		name, body, contentType, origin string
		originPresent                   bool
		want                            int
	}{
		{name: "native JSON", body: body, contentType: "application/json", want: 200},
		{name: "browser origin", body: body, contentType: "application/json", origin: "https://app.example", originPresent: true, want: 400},
		{name: "null origin", body: body, contentType: "application/json", origin: "null", originPresent: true, want: 400},
		{name: "empty origin header", body: body, contentType: "application/json", originPresent: true, want: 400},
		{name: "simple form", body: body, contentType: "application/x-www-form-urlencoded", want: 400},
		{name: "missing content type", body: body, want: 400},
		{name: "unknown field", body: strings.TrimSuffix(body, "}") + `,"user_id":"victim"}`, contentType: "application/json", want: 400},
		{name: "trailing JSON", body: body + "{}", contentType: "application/json", want: 400},
		{name: "unregistered app", body: strings.ReplaceAll(body, "com.example.client", "evil.example"), contentType: "application/json", want: 400},
		{name: "plain PKCE", body: strings.ReplaceAll(body, "S256", "plain"), contentType: "application/json", want: 400},
		{name: "oversized body", body: body + strings.Repeat(" ", 8192), contentType: "application/json", want: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &mobileTestStore{}
			h := &Handler{Service: &mobileTestService{}}
			require.NoError(t, h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: []string{"com.example.client:/oauth/callback"}, Store: store}))
			req := httptest.NewRequest(http.MethodPost, "https://untrusted-host.example/", strings.NewReader(test.body))
			req = mux.SetURLVars(req, map[string]string{"provider": "google"})
			req.Header.Set("Content-Type", test.contentType)
			if test.originPresent {
				req.Header["Origin"] = []string{test.origin}
			}
			out := httptest.NewRecorder()
			h.MobileOAuthLogin(out, req)
			require.Equal(t, test.want, out.Code, out.Body.String())
			require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
			require.Equal(t, "no-referrer", out.Header().Get("Referrer-Policy"))
			require.Empty(t, out.Result().Cookies())
			if test.want == 200 {
				require.Equal(t, 1, store.starts)
				require.Contains(t, out.Body.String(), "https://app.example/api/v1/ams/oauth/mobile/start?")
				require.NotContains(t, out.Body.String(), "untrusted-host")
			} else {
				require.Zero(t, store.starts)
			}
		})
	}
}

func TestMobileOAuthExpiredAndAmbiguousStartIsRejected(t *testing.T) {
	type startCase struct{ name, query string }
	ticket := strings.Repeat("t", 43)
	for _, test := range []startCase{
		{"expired ticket", "ticket=" + ticket}, {"duplicate ticket", "ticket=" + ticket + "&ticket=" + ticket},
		{"unknown parameter", "ticket=" + ticket + "&extra=1"}, {"malformed ticket", "ticket=bad"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &mobileTestService{}
			flow := oauth.MobileFlowContext{RedirectURI: "com.example.client:/oauth/callback", State: strings.Repeat("s", 43), Challenge: strings.Repeat("c", 43)}
			payload, err := json.Marshal(mobileOAuthStart{Provider: "google", Flow: flow, ExpiresAt: time.Now().Add(-time.Second)})
			require.NoError(t, err)
			h := &Handler{Service: service}
			require.NoError(t, h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: []string{flow.RedirectURI}, Store: &mobileTestStore{payload: payload}}))
			out := httptest.NewRecorder()
			h.MobileOAuthStart(out, httptest.NewRequest(http.MethodGet, "/start?"+test.query, nil))
			require.Equal(t, 400, out.Code)
			require.Empty(t, out.Header().Get("Location"))
			require.Zero(t, service.starts)
		})
	}
}
