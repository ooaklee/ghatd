package accessmanager

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

const mobileOAuthStartTTL = 2 * time.Minute
const mobileOAuthGrantTTL = time.Minute
const mobileOAuthStartPath = common.ApiV1UriPrefix + "/ams/oauth/mobile/start"

var mobileOpaquePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var mobileVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var mobileSchemePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9-]+)+$`)

// MobileOAuthConfig enables only explicitly registered native applications.
// Configure it once before serving requests. An empty allowlist disables it.
type MobileOAuthConfig struct {
	Origin       string
	RedirectURIs []string
	Store        MobileOAuthStore
}

// ConfigureMobileOAuth validates and snapshots the native handoff configuration.
func (h *Handler) ConfigureMobileOAuth(config MobileOAuthConfig) error {
	h.mobileOAuth = nil
	if len(config.RedirectURIs) == 0 {
		return nil
	}
	origin, err := url.Parse(config.Origin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Opaque != "" || origin.Path != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || config.Store == nil || origin.String() != config.Origin || origin.Hostname() == "localhost" || net.ParseIP(origin.Hostname()) != nil {
		return oauth.ErrSecureProviderIncompleteConfig
	}
	allowed := make([]string, 0, len(config.RedirectURIs))
	seen := map[string]bool{}
	for _, raw := range config.RedirectURIs {
		if !validMobileRedirect(raw) || seen[raw] {
			return oauth.ErrSecureProviderIncompleteConfig
		}
		seen[raw] = true
		allowed = append(allowed, raw)
	}
	config.RedirectURIs = allowed
	h.mobileOAuth = &config
	return nil
}

func validMobileRedirect(raw string) bool {
	uri, err := url.Parse(raw)
	return err == nil && len(raw) <= 512 && mobileSchemePattern.MatchString(uri.Scheme) && uri.Host == "" && uri.User == nil && uri.Opaque == "" && uri.Path == "/oauth/callback" && uri.RawPath == "" && uri.RawQuery == "" && !uri.ForceQuery && uri.Fragment == "" && uri.String() == raw && raw == uri.Scheme+":/oauth/callback"
}

func (h *Handler) mobileRedirectAllowed(raw string) bool {
	if h.mobileOAuth == nil {
		return false
	}
	for _, allowed := range h.mobileOAuth.RedirectURIs {
		if raw == allowed {
			return true
		}
	}
	return false
}

type mobileOAuthStart struct {
	Provider  string                  `json:"provider"`
	Flow      oauth.MobileFlowContext `json:"flow"`
	Link      *oauth.LinkProof        `json:"link,omitempty"`
	ExpiresAt time.Time               `json:"expires_at"`
}

type mobileOAuthProfile struct {
	Issuer        string `json:"issuer"`
	Subject       string `json:"subject"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
}

func (p mobileOAuthProfile) GetProviderIssuer() string           { return p.Issuer }
func (p mobileOAuthProfile) GetProviderSubject() string          { return p.Subject }
func (p mobileOAuthProfile) GetUserEmail() string                { return p.Email }
func (p mobileOAuthProfile) IsUserEmailVerifiedByProvider() bool { return p.EmailVerified }
func (p mobileOAuthProfile) GetUserFirstName() string            { return p.FirstName }
func (p mobileOAuthProfile) GetUserLastName() string             { return p.LastName }

type mobileOAuthGrant struct {
	Provider  string                  `json:"provider"`
	Profile   mobileOAuthProfile      `json:"profile"`
	Flow      oauth.MobileFlowContext `json:"flow"`
	Link      *oauth.LinkProof        `json:"link,omitempty"`
	ExpiresAt time.Time               `json:"expires_at"`
}

type mobileOAuthService interface {
	OAuthProviders() []string
	mobileOAuthLinkProof(context.Context, string) (*oauth.LinkProof, error)
	completeMobileOAuth(context.Context, *mobileOAuthGrant, string) (*OauthCallbackResponse, error)
}

func (s *Service) mobileOAuthLinkProof(ctx context.Context, token string) (*oauth.LinkProof, error) {
	if token == "" {
		return nil, ErrOAuthReauthenticationRequired
	}
	details, err := s.AuthService.ExtractAccessTokenMetadataByString(ctx, token)
	if err != nil || details == nil || !details.IsAuthorized {
		return nil, ErrOAuthReauthenticationRequired
	}
	proof := &oauth.LinkProof{UserID: details.UserID, AccessUUID: details.AccessUUID, AuthenticationTime: details.AuthenticationTime, EmailRevision: details.EmailRevision}
	if err := s.validateOAuthLinkProof(ctx, proof); err != nil {
		return nil, err
	}
	return proof, nil
}

func (s *Service) completeMobileOAuth(ctx context.Context, grant *mobileOAuthGrant, token string) (*OauthCallbackResponse, error) {
	if grant == nil || !time.Now().Before(grant.ExpiresAt) {
		return nil, oauth.ErrSecureTransactionExpired
	}
	if _, err := s.secureOAuthProvider(grant.Provider); err != nil {
		return nil, err
	}
	if grant.Link != nil {
		proof, err := s.mobileOAuthLinkProof(ctx, token)
		if err != nil {
			return nil, err
		}
		if proof.UserID != grant.Link.UserID || proof.AccessUUID != grant.Link.AccessUUID || !proof.AuthenticationTime.Equal(grant.Link.AuthenticationTime) {
			return nil, ErrOAuthReauthenticationRequired
		}
	}
	return s.finalizeOAuthIdentity(ctx, grant.Provider, grant.Profile, grant.Link, &OauthCallbackResponse{RequestUrl: "/app"})
}

// MobileOAuthProviders exposes native capability independently of the web flow.
func (h *Handler) MobileOAuthProviders(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	providers, redirects := []string{}, []string{}
	if service, ok := h.Service.(mobileOAuthService); ok && h.mobileOAuth != nil {
		providers = service.OAuthProviders()
		redirects = append(redirects, h.mobileOAuth.RedirectURIs...)
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, map[string]interface{}{"providers": providers, "redirect_uris": redirects})
}

func mobileCookie(r *http.Request, name string) string {
	token := ""
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			if token != "" || cookie.Value == "" {
				return ""
			}
			token = cookie.Value
		}
	}
	return token
}

func (h *Handler) mobileError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := oauthErrorCode(err)
	if code == "unavailable" {
		status = http.StatusServiceUnavailable
	}
	if code == "reauth_required" || code == "restricted" {
		status = http.StatusForbidden
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, status, map[string]string{"error": code})
}

func decodeMobileJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	if r.Method != http.MethodPost || len(r.Header.Values("Origin")) != 0 {
		return false
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil && decoder.Decode(&struct{}{}) == io.EOF
}

func randomMobileCode() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// MobileOAuthLogin prepares the app-bound start ticket; it sets no cookies.
func (h *Handler) MobileOAuthLogin(w http.ResponseWriter, r *http.Request) {
	h.mobileInitiate(w, r, false)
}

// MobileOAuthLink binds a start ticket to the existing fresh native session.
func (h *Handler) MobileOAuthLink(w http.ResponseWriter, r *http.Request) {
	h.mobileInitiate(w, r, true)
}

func (h *Handler) mobileInitiate(w http.ResponseWriter, r *http.Request, linking bool) {
	oauthHeaders(w)
	service, ok := h.Service.(mobileOAuthService)
	if !ok || h.mobileOAuth == nil {
		h.mobileError(w, user.ErrOAuthUnsupported)
		return
	}
	var body struct {
		RedirectURI string `json:"redirect_uri"`
		State       string `json:"state"`
		Challenge   string `json:"code_challenge"`
		Method      string `json:"code_challenge_method"`
	}
	if !decodeMobileJSON(w, r, &body) || !h.mobileRedirectAllowed(body.RedirectURI) || !mobileOpaquePattern.MatchString(body.State) || !mobileOpaquePattern.MatchString(body.Challenge) || body.Method != "S256" {
		h.mobileError(w, ErrBadRequest)
		return
	}
	provider, err := getProviderNameFromURI(r)
	if err != nil {
		h.mobileError(w, ErrBadRequest)
		return
	}
	available := false
	for _, name := range service.OAuthProviders() {
		if name == provider {
			available = true
		}
	}
	if !available {
		h.mobileError(w, user.ErrOAuthUnsupported)
		return
	}
	start := mobileOAuthStart{Provider: provider, Flow: oauth.MobileFlowContext{RedirectURI: body.RedirectURI, State: body.State, Challenge: body.Challenge}, ExpiresAt: time.Now().Add(mobileOAuthStartTTL)}
	if linking {
		proof, err := service.mobileOAuthLinkProof(r.Context(), mobileCookie(r, h.CookiePrefixAuthToken))
		if err != nil {
			h.mobileError(w, err)
			return
		}
		start.Link = proof
	}
	ticket, err := randomMobileCode()
	if err != nil {
		h.mobileError(w, err)
		return
	}
	payload, err := json.Marshal(start)
	if err == nil {
		err = h.mobileOAuth.Store.SaveStart(r.Context(), ticket, payload, mobileOAuthStartTTL)
	}
	if err != nil {
		h.mobileError(w, err)
		return
	}
	authorization := h.mobileOAuth.Origin + mobileOAuthStartPath + "?" + url.Values{"ticket": {ticket}}.Encode()
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, map[string]string{"authorization_url": authorization})
}

// MobileOAuthStart enters the existing cookie-bound browser provider flow.
func (h *Handler) MobileOAuthStart(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	if h.mobileOAuth == nil {
		h.mobileError(w, user.ErrOAuthUnsupported)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if r.Method != http.MethodGet || err != nil || len(query) != 1 || len(query["ticket"]) != 1 || !mobileOpaquePattern.MatchString(query.Get("ticket")) {
		h.mobileError(w, ErrBadRequest)
		return
	}
	payload, err := h.mobileOAuth.Store.ConsumeStart(r.Context(), query.Get("ticket"))
	if err != nil {
		h.mobileError(w, err)
		return
	}
	var start mobileOAuthStart
	if json.Unmarshal(payload, &start) != nil || !h.mobileRedirectAllowed(start.Flow.RedirectURI) || !time.Now().Before(start.ExpiresAt) || !mobileOpaquePattern.MatchString(start.Flow.State) || !mobileOpaquePattern.MatchString(start.Flow.Challenge) {
		h.mobileError(w, ErrBadRequest)
		return
	}
	response, err := h.Service.OauthLogin(r.Context(), &OauthLoginRequest{Provider: start.Provider, RequestUrl: "/app", Mobile: &start.Flow, Link: start.Link})
	if err != nil {
		h.mobileRedirect(w, r, &start.Flow, "", err)
		return
	}
	h.setOAuthCookie(w, response.CookieCore, start.Provider)
	http.Redirect(w, r, response.ProviderAuthCodeUrl, http.StatusFound)
}

func (h *Handler) mobileRedirect(w http.ResponseWriter, r *http.Request, flow *oauth.MobileFlowContext, code string, err error) {
	if flow == nil || !h.mobileRedirectAllowed(flow.RedirectURI) {
		h.mobileError(w, ErrBadRequest)
		return
	}
	query := url.Values{"state": {flow.State}}
	if err != nil {
		query.Set("error", oauthErrorCode(err))
	} else {
		query.Set("code", code)
	}
	http.Redirect(w, r, flow.RedirectURI+"?"+query.Encode(), http.StatusSeeOther)
}

// finishMobileCallback stores only verified identity data, never browser tokens.
func (h *Handler) finishMobileCallback(w http.ResponseWriter, r *http.Request, response *OauthCallbackResponse, callbackErr error) {
	if h.mobileOAuth == nil || !h.mobileRedirectAllowed(response.Mobile.RedirectURI) {
		h.mobileError(w, user.ErrOAuthUnsupported)
		return
	}
	if callbackErr != nil {
		h.mobileRedirect(w, r, response.Mobile, "", callbackErr)
		return
	}
	grant := response.MobileGrant
	if grant == nil {
		h.mobileRedirect(w, r, response.Mobile, "", ErrBadRequest)
		return
	}
	code, err := randomMobileCode()
	if err == nil {
		var payload []byte
		payload, err = json.Marshal(grant)
		if err == nil {
			err = h.mobileOAuth.Store.SaveGrant(r.Context(), code, grant.Flow.Challenge, grant.Flow.RedirectURI, grant.Flow.State, payload, time.Until(grant.ExpiresAt))
		}
	}
	h.mobileRedirect(w, r, response.Mobile, code, err)
}

// MobileOAuthExchange sets normal app cookies only after one-use S256 proof.
func (h *Handler) MobileOAuthExchange(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	service, ok := h.Service.(mobileOAuthService)
	if !ok || h.mobileOAuth == nil {
		h.mobileError(w, user.ErrOAuthUnsupported)
		return
	}
	var body struct {
		Code        string `json:"code"`
		Verifier    string `json:"code_verifier"`
		RedirectURI string `json:"redirect_uri"`
		State       string `json:"state"`
	}
	if !decodeMobileJSON(w, r, &body) || !mobileOpaquePattern.MatchString(body.Code) || !mobileVerifierPattern.MatchString(body.Verifier) || !mobileOpaquePattern.MatchString(body.State) || !h.mobileRedirectAllowed(body.RedirectURI) {
		h.mobileError(w, ErrBadRequest)
		return
	}
	digest := sha256.Sum256([]byte(body.Verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	payload, err := h.mobileOAuth.Store.ConsumeGrant(r.Context(), body.Code, challenge, body.RedirectURI, body.State)
	if err != nil {
		h.mobileError(w, err)
		return
	}
	var grant mobileOAuthGrant
	if json.Unmarshal(payload, &grant) != nil || grant.Flow.RedirectURI != body.RedirectURI || grant.Flow.State != body.State || !oauth.ConstantTimeEquals(grant.Flow.Challenge, challenge) || !time.Now().Before(grant.ExpiresAt) {
		h.mobileError(w, ErrBadRequest)
		return
	}
	response, err := service.completeMobileOAuth(r.Context(), &grant, mobileCookie(r, h.CookiePrefixAuthToken))
	if err != nil {
		h.mobileError(w, err)
		return
	}
	if !response.Linked {
		h.AddAuthCookies(w, response.AccessToken, response.AccessTokenExpiresAt, response.RefreshToken, response.RefreshTokenExpiresAt)
		toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, response.AccessTokenExpiresAt, response.RefreshTokenExpiresAt)
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, map[string]interface{}{"provider": grant.Provider, "linked": response.Linked})
}
