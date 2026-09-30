package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// GoogleSecureIssuer is Google's canonical ID token issuer.
	GoogleSecureIssuer        = "https://accounts.google.com"
	googleSecureJWKSURL       = "https://www.googleapis.com/oauth2/v3/certs"
	googleSecureTokenEndpoint = "https://oauth2.googleapis.com/token"
)

// secureProvider shares transaction handling without exposing legacy login.
type secureProvider struct {
	name, clientID, redirectURL, authoriseURL, tokenURL, issuer, cookieKey string
	clientSecret                                                           func() (string, error)
	httpClient                                                             *http.Client
	jwks                                                                   *jwksCache
	store                                                                  SecureTransactionStore
}

// GoogleSecureProvider validates Google sign-in using nonce and S256 PKCE.
type GoogleSecureProvider struct{ *secureProvider }

// NewGoogleSecureProviderRequest supplies a complete, explicitly enabled provider.
type NewGoogleSecureProviderRequest struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Scopes is retained for compatibility; required identity scopes are always used.
	Scopes     []string
	HTTPClient *http.Client
	Store      SecureTransactionStore
}

// NewGoogleSecureProvider rejects incomplete or unsafe callback configuration.
func NewGoogleSecureProvider(r *NewGoogleSecureProviderRequest) (*GoogleSecureProvider, error) {
	if r == nil || r.Store == nil || strings.TrimSpace(r.ClientID) == "" || strings.TrimSpace(r.ClientSecret) == "" || !validRedirectURL(r.RedirectURL, false) {
		return nil, ErrSecureProviderIncompleteConfig
	}
	client := r.HTTPClient
	if client == nil {
		client = NewSecureHTTPClient(30 * time.Second)
	}
	p := &secureProvider{name: "google", clientID: r.ClientID, redirectURL: r.RedirectURL, authoriseURL: GoogleSecureIssuer + "/o/oauth2/v2/auth", tokenURL: googleSecureTokenEndpoint, issuer: GoogleSecureIssuer, cookieKey: "oauth_google_state", httpClient: client, store: r.Store}
	secret := r.ClientSecret
	p.clientSecret = func() (string, error) { return secret, nil }
	p.jwks = newJWKSCache(googleSecureJWKSURL, client, time.Now)
	return &GoogleSecureProvider{p}, nil
}

// validRedirectURL permits HTTP only for Google's loopback development callback.
func validRedirectURL(raw string, apple bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return !apple && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}

// ProviderGetName identifies the provider used for the stored transaction.
func (p *secureProvider) ProviderGetName() string { return p.name }

// ProviderGetCookieKey keeps simultaneous provider flows in separate cookies.
func (p *secureProvider) ProviderGetCookieKey() string { return p.cookieKey }

// ProviderGenerateProtectionToken preserves the legacy interface without enabling it.
func (p *secureProvider) ProviderGenerateProtectionToken() string { return "" }

// ProviderGenerateAuthCodeUrl fails closed for legacy initiation.
func (p *secureProvider) ProviderGenerateAuthCodeUrl(string) string { return "" }

// ProviderGetUserData requires completion through the stored transaction flow.
func (p *secureProvider) ProviderGetUserData(context.Context, url.Values) (OauthUserInfo, error) {
	return nil, ErrSecureProviderIncompleteConfig
}

// ProviderVerifyRequestIsAuthentic prevents legacy email-based completion.
func (p *secureProvider) ProviderVerifyRequestIsAuthentic(url.Values, *http.Cookie) (string, bool) {
	return p.cookieKey, false
}

// BeginSecureTransaction starts an API flow without linking context.
func (p *secureProvider) BeginSecureTransaction(ctx context.Context, path string) (*SecureTransaction, error) {
	return p.BeginSecureTransactionWithOptions(ctx, path, SecureFlowOptions{})
}

// BeginSecureTransactionWithOptions captures server-authenticated completion context.
func (p *secureProvider) BeginSecureTransactionWithOptions(ctx context.Context, path string, opts SecureFlowOptions) (*SecureTransaction, error) {
	path, err := ValidateSecureReturnPath(path)
	if err != nil {
		return nil, err
	}
	state, err := NewOpaqueTransactionID()
	if err != nil {
		return nil, err
	}
	nonce, err := NewOpaqueTransactionID()
	if err != nil {
		return nil, err
	}
	txn := &StoredTransaction{State: state, Nonce: nonce, Provider: p.name, ReturnPath: path, Options: opts, ExpiresAt: time.Now().Add(TransactionTTL)}
	q := url.Values{"client_id": {p.clientID}, "redirect_uri": {p.redirectURL}, "response_type": {"code"}, "scope": {"openid email profile"}, "state": {state}, "nonce": {nonce}}
	if p.name == "google" {
		verifier, challenge, err := MintPKCEPair()
		if err != nil {
			return nil, err
		}
		txn.PKCEVerifier = verifier
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	} else {
		q.Set("scope", "name email")
		q.Set("response_mode", "form_post")
	}
	if err := p.store.Save(ctx, txn); err != nil {
		return nil, err
	}
	return &SecureTransaction{TransactionID: state, AuthorisationURL: p.authoriseURL + "?" + q.Encode()}, nil
}

// callbackValue rejects ambiguous, empty or oversized security parameters.
func callbackValue(q url.Values, key string, required bool) (string, error) {
	values := q[key]
	if len(values) == 0 && !required {
		return "", nil
	}
	if len(values) != 1 || values[0] == "" || len(values[0]) > 16384 {
		return "", ErrSecureTransactionInvalidState
	}
	return values[0], nil
}

// CompleteSecureTransaction compares browser state before atomically consuming it.
func (p *secureProvider) CompleteSecureTransaction(ctx context.Context, r *SecureCallbackRequest) (*SecureCallbackResult, error) {
	if r == nil || r.TransactionID == "" {
		return nil, ErrSecureTransactionNotFound
	}
	if (p.name == "apple" && r.Method != http.MethodPost) || (p.name == "google" && r.Method != http.MethodGet) {
		return nil, ErrSecureTransactionInvalidState
	}
	state, err := callbackValue(r.Query, "state", true)
	if err != nil || !ConstantTimeEquals(state, r.TransactionID) {
		return nil, ErrSecureTransactionInvalidState
	}
	code, err := callbackValue(r.Query, "code", false)
	if err != nil {
		return nil, err
	}
	providerError, err := callbackValue(r.Query, "error", false)
	if err != nil || (code == "" && providerError == "") || (code != "" && providerError != "") {
		return nil, ErrSecureTransactionInvalidState
	}
	txn, err := p.store.Consume(ctx, r.TransactionID)
	if err != nil {
		return nil, err
	}
	if txn.Provider != p.name || !ConstantTimeEquals(txn.State, state) || txn.Nonce == "" {
		return nil, ErrSecureTransactionInvalidState
	}
	if !time.Now().Before(txn.ExpiresAt) {
		return nil, ErrSecureTransactionExpired
	}
	result := &SecureCallbackResult{Provider: p.name, ReturnPath: txn.ReturnPath, Transaction: txn}
	if providerError != "" {
		return result, ErrProviderCancelled
	}
	token, err := p.exchangeCode(ctx, code, txn.PKCEVerifier)
	if err != nil {
		return result, err
	}
	claims, err := ValidateIDToken(ctx, p.jwks, token, &IDTokenExpectations{Issuer: p.issuer, Audience: p.clientID, Nonce: txn.Nonce, RequireAZPMatching: p.name == "google"})
	if err != nil {
		return result, err
	}
	info := &GoogleSecureUserInfo{Subject: claims.Subject, Issuer: claims.Issuer, Email: claims.Email, VerifiedEmail: claims.EmailVerified, FullName: claims.FullName, FirstName: claims.GivenName, FamilyName: claims.FamilyName}
	if p.name == "apple" && len(r.Query["user"]) > 0 {
		raw, err := callbackValue(r.Query, "user", false)
		if err != nil {
			return result, err
		}
		var profile struct {
			Name struct {
				FirstName string `json:"firstName"`
				LastName  string `json:"lastName"`
			} `json:"name"`
		}
		if len(raw) > 4096 || json.Unmarshal([]byte(raw), &profile) != nil || len(profile.Name.FirstName) > 256 || len(profile.Name.LastName) > 256 {
			return result, ErrSecureIDTokenInvalid
		}
		info.FirstName = strings.TrimSpace(profile.Name.FirstName)
		info.FamilyName = strings.TrimSpace(profile.Name.LastName)
		info.FullName = strings.TrimSpace(info.FirstName + " " + info.FamilyName)
	}
	result.UserInfo = info
	return result, nil
}

// exchangeCode sends only server-held nonce/PKCE context to the token endpoint.
func (p *secureProvider) exchangeCode(ctx context.Context, code, verifier string) (string, error) {
	secret, err := p.clientSecret()
	if err != nil {
		return "", ErrProviderCodeExchangeIncorrect
	}
	form := url.Values{"code": {code}, "client_id": {p.clientID}, "client_secret": {secret}, "grant_type": {"authorization_code"}, "redirect_uri": {p.redirectURL}}
	if p.name == "google" {
		if verifier == "" {
			return "", ErrSecureProviderInvalidPKCEConfig
		}
		form.Set("code_verifier", verifier)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", ErrProviderCodeExchangeIncorrect
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := p.httpClient.Do(req)
	if err != nil {
		return "", ErrProviderCodeExchangeIncorrect
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", ErrProviderCodeExchangeIncorrect
	}
	var body struct {
		IDToken string `json:"id_token"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body) != nil || body.IDToken == "" {
		return "", ErrProviderCodeExchangeIncorrect
	}
	return body.IDToken, nil
}

// GoogleSecureUserInfo carries a verified stable identity and optional profile.
type GoogleSecureUserInfo struct {
	Subject, Issuer, Email, FullName, FirstName, FamilyName string `json:"-"`
	VerifiedEmail                                           bool   `json:"-"`
}

// GetUserEmail returns the signed email claim, which may be absent on repeat Apple login.
func (g *GoogleSecureUserInfo) GetUserEmail() string { return g.Email }

// GetUserFirstName returns optional profile data.
func (g *GoogleSecureUserInfo) GetUserFirstName() string { return g.FirstName }

// GetUserLastName returns optional profile data.
func (g *GoogleSecureUserInfo) GetUserLastName() string { return g.FamilyName }

// GetUserFullName returns optional profile data.
func (g *GoogleSecureUserInfo) GetUserFullName() string { return g.FullName }

// IsUserEmailVerifiedByProvider reports verification of the signed email claim.
func (g *GoogleSecureUserInfo) IsUserEmailVerifiedByProvider() bool { return g.VerifiedEmail }

// GetProviderSubject returns the stable, signed provider subject.
func (g *GoogleSecureUserInfo) GetProviderSubject() string { return g.Subject }

// GetProviderIssuer returns the canonical, signed provider issuer.
func (g *GoogleSecureUserInfo) GetProviderIssuer() string { return g.Issuer }
