package oauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"strings"
	"time"
)

// AppleIssuer identifies Apple's signed identities.
const AppleIssuer = "https://appleid.apple.com"

// AppleProvider supports Apple's web form_post flow and first-only profile.
type AppleProvider struct{ *secureProvider }

// NewAppleProviderRequest supplies a Services ID and dedicated signing key.
type NewAppleProviderRequest struct {
	// ClientID is the Services ID registered for web sign-in.
	ClientID string
	// TeamID identifies the Apple developer team issuing client assertions.
	TeamID string
	// KeyID identifies the dedicated Sign in with Apple signing key.
	KeyID string
	// PrivateKeyPEM contains the P-256 private key used for client assertions.
	// Keep this server-side; never embed it in browser or mobile applications.
	PrivateKeyPEM []byte
	// RedirectURL is the exact HTTPS return URL registered for the Services ID.
	RedirectURL string
	// HTTPClient performs provider requests; nil selects a traced 30-second client.
	HTTPClient *http.Client
	// Store is required and must consume transactions atomically and once only.
	Store SecureTransactionStore
}

// NewAppleProvider validates the P-256 key and HTTPS callback configuration.
// The host must register the exact callback with Apple before using the provider.
func NewAppleProvider(r *NewAppleProviderRequest) (*AppleProvider, error) {
	if r == nil || r.Store == nil || strings.TrimSpace(r.ClientID) == "" || strings.TrimSpace(r.TeamID) == "" || strings.TrimSpace(r.KeyID) == "" || !validRedirectURL(r.RedirectURL, true) {
		return nil, ErrSecureProviderIncompleteConfig
	}
	raw, err := ParsePrivateKeyPEM(r.PrivateKeyPEM)
	if err != nil {
		return nil, ErrSecureProviderIncompleteConfig
	}
	key, ok := raw.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() || key.D == nil || key.D.Sign() <= 0 {
		return nil, ErrSecureProviderIncompleteConfig
	}
	client := r.HTTPClient
	if client == nil {
		client = NewSecureHTTPClient(30 * time.Second)
	}
	p := &secureProvider{name: "apple", clientID: r.ClientID, redirectURL: r.RedirectURL, authoriseURL: AppleIssuer + "/auth/authorize", tokenURL: AppleIssuer + "/auth/token", issuer: AppleIssuer, cookieKey: "oauth_apple_state", httpClient: client, store: r.Store}
	// Generate a short-lived client secret for each exchange; no expiry rotation job is needed.
	teamID, clientID, keyID := r.TeamID, r.ClientID, r.KeyID
	p.clientSecret = func() (string, error) { return signAppleClientSecret(teamID, clientID, keyID, key, time.Now()) }
	p.jwks = newJWKSCache(AppleIssuer+"/auth/keys", client, time.Now)
	return &AppleProvider{p}, nil
}

// signAppleClientSecret signs an ES256 client assertion with the dedicated key.
func signAppleClientSecret(teamID, clientID, keyID string, key *ecdsa.PrivateKey, now time.Time) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{Issuer: teamID, Subject: clientID, Audience: jwt.ClaimStrings{AppleIssuer}, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute))})
	token.Header["kid"] = keyID
	return token.SignedString(key)
}
