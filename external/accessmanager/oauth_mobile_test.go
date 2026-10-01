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

func TestMobileOAuthConfigurationAndDisabledDiscovery(t *testing.T) {
	store := &mobileTestStore{}
	h := &Handler{Service: &mobileTestService{}}
	out := httptest.NewRecorder()
	h.MobileOAuthProviders(out, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, 200, out.Code)
	require.Contains(t, out.Body.String(), `"providers":[]`)
	require.Contains(t, out.Body.String(), `"redirect_uris":[]`)
	for _, origin := range []string{"", "http://app.example", "https://user@app.example", "https://app.example/path", "https://app.example?query", "https://app.example#fragment", "https://localhost", "https://127.0.0.1"} {
		require.Error(t, h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: origin, RedirectURIs: []string{"boasi.io.bedrock:/oauth/callback"}, Store: store}), origin)
	}
	for _, uri := range []string{"", "https://evil.example/callback", "javascript:alert(1)", "bedrock:/oauth/callback", "boasi.io.bedrock://oauth/callback", "boasi.io.bedrock:///oauth/callback", "boasi.io.bedrock:/other", "boasi.io.bedrock:/oauth/callback?", "boasi.io.bedrock:/oauth/callback#x", "boasi.io.bedrock:/oauth/%63allback", " boasi.io.bedrock:/oauth/callback"} {
		require.Error(t, h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: []string{uri}, Store: store}), uri)
	}
	require.Error(t, h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: []string{"boasi.io.bedrock:/oauth/callback"}}))
	uris := []string{"boasi.io.bedrock:/oauth/callback"}
	require.NoError(t, h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: uris, Store: store}))
	uris[0] = "evil.example:/oauth/callback"
	require.True(t, h.mobileRedirectAllowed("boasi.io.bedrock:/oauth/callback"))
	require.False(t, h.mobileRedirectAllowed(uris[0]))
	require.NoError(t, h.ConfigureMobileOAuth(MobileOAuthConfig{}))
	require.False(t, h.mobileRedirectAllowed("boasi.io.bedrock:/oauth/callback"))
}

func TestMobileOAuthInitiationRejectsAmbiguousAndBrowserRequests(t *testing.T) {
	body := `{"redirect_uri":"boasi.io.bedrock:/oauth/callback","state":"` + strings.Repeat("s", 43) + `","code_challenge":"` + strings.Repeat("c", 43) + `","code_challenge_method":"S256"}`
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
		{name: "unregistered app", body: strings.ReplaceAll(body, "boasi.io.bedrock", "evil.example"), contentType: "application/json", want: 400},
		{name: "plain PKCE", body: strings.ReplaceAll(body, "S256", "plain"), contentType: "application/json", want: 400},
		{name: "oversized body", body: body + strings.Repeat(" ", 8192), contentType: "application/json", want: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &mobileTestStore{}
			h := &Handler{Service: &mobileTestService{}}
			require.NoError(t, h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: []string{"boasi.io.bedrock:/oauth/callback"}, Store: store}))
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
	ticket := strings.Repeat("t", 43)
	service := &mobileTestService{}
	flow := oauth.MobileFlowContext{RedirectURI: "boasi.io.bedrock:/oauth/callback", State: strings.Repeat("s", 43), Challenge: strings.Repeat("c", 43)}
	payload, err := json.Marshal(mobileOAuthStart{Provider: "google", Flow: flow, ExpiresAt: time.Now().Add(-time.Second)})
	require.NoError(t, err)
	h := &Handler{Service: service}
	require.NoError(t, h.ConfigureMobileOAuth(MobileOAuthConfig{Origin: "https://app.example", RedirectURIs: []string{flow.RedirectURI}, Store: &mobileTestStore{payload: payload}}))
	for _, query := range []string{"ticket=" + ticket, "ticket=" + ticket + "&ticket=" + ticket, "ticket=" + ticket + "&extra=1", "ticket=bad"} {
		out := httptest.NewRecorder()
		h.MobileOAuthStart(out, httptest.NewRequest(http.MethodGet, "/start?"+query, nil))
		require.Equal(t, 400, out.Code)
		require.Empty(t, out.Header().Get("Location"))
	}
	require.Zero(t, service.starts)
}
