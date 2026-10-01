package oauth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// IDTokenClaims are the verified claims extracted from a provider ID token.
type IDTokenClaims struct {
	// Issuer is the canonical https issuer.
	Issuer string

	// Subject is the stable provider user identifier.
	Subject string

	// Audience holds every aud value on the token.
	Audience []string

	// Email is the provider-reported email.
	Email string

	// EmailVerified reports provider verification of the email.
	EmailVerified bool

	// Nonce is the nonce bound into the token.
	Nonce string

	// Expiry is the token expiry.
	Expiry time.Time

	// FullName is the optional display name claim.
	FullName string

	// GivenName is the optional given name claim.
	GivenName string

	// FamilyName is the optional family name claim.
	FamilyName string

	// RawProfile holds Apple's optional first-only user object JSON.
	RawProfile string
}

// IDTokenExpectations captures the values a valid ID token must present.
type IDTokenExpectations struct {
	// Issuer is the canonical https issuer, compared after normalisation.
	Issuer string

	// Audience is the required client identifier.
	Audience string

	// Nonce is the server-held nonce bound into the token.
	Nonce string

	// RequireAZPMatching enables Google's multi-audience azp check: when the
	// token lists multiple audiences, azp must equal Audience.
	RequireAZPMatching bool
}

// validate enforces issuer, audience, expiry, subject and nonce expectations.
func (e *IDTokenExpectations) validate(claims *jwt.RegisteredClaims, profile *IDTokenProfileClaims) error {
	if !ConstantTimeEquals(canonicalIssuer(claims.Issuer), canonicalIssuer(e.Issuer)) {
		return ErrSecureIDTokenInvalid
	}
	audienceOK := false
	for _, aud := range claims.Audience {
		if ConstantTimeEquals(aud, e.Audience) {
			audienceOK = true
			break
		}
	}
	if len(claims.Audience) == 1 {
		audienceOK = ConstantTimeEquals(claims.Audience[0], e.Audience)
	}
	if !audienceOK {
		return ErrSecureIDTokenInvalid
	}
	if e.RequireAZPMatching && (len(claims.Audience) > 1 || profile.AuthorisedParty != "") && !ConstantTimeEquals(profile.AuthorisedParty, e.Audience) {
		return ErrSecureIDTokenInvalid
	}
	if claims.ExpiresAt == nil || claims.Subject == "" {
		return ErrSecureIDTokenInvalid
	}
	if !ConstantTimeEquals(profile.Nonce, e.Nonce) {
		return ErrSecureIDTokenInvalid
	}
	return nil
}

// IDTokenProfileClaims are the non-registered claims read from the token.
type IDTokenProfileClaims struct {
	// Email is the provider-reported email.
	Email string `json:"email"`

	// EmailVerified reports provider verification of the email.
	EmailVerified verifiedClaim `json:"email_verified"`

	// Nonce is the nonce bound into the token.
	Nonce string `json:"nonce"`

	// Name is the optional display name.
	Name string `json:"name"`

	// GivenName is the optional given name.
	GivenName string `json:"given_name"`

	// FamilyName is the optional family name.
	FamilyName string `json:"family_name"`

	// AuthorisedParty is the azp claim used by Google for multi-audience
	// tokens.
	AuthorisedParty string `json:"azp"`

	// RawUserObject carries Apple's optional first-only user object.
	RawUserObject string `json:"user"`
}

// canonicalIssuer normalises issuer values for comparison, accepting only Google's documented issuer alias.
func canonicalIssuer(issuer string) string {
	if issuer == "accounts.google.com" {
		return "https://accounts.google.com"
	}
	return issuer
}

// verifiedClaim accepts Apple's documented string or Google's boolean claim.
type verifiedClaim bool

// UnmarshalJSON rejects values outside the provider's boolean representation.
func (v *verifiedClaim) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "true", `"true"`:
		*v = true
	case "false", `"false"`:
		*v = false
	default:
		return ErrSecureIDTokenInvalid
	}
	return nil
}

// jwksDocument is the subset of a JWKS response this package needs.
type jwksDocument struct {
	Keys []jwksKey `json:"keys"`
}

// jwksKey is a single JSON Web Key.
type jwksKey struct {
	KID string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// jwksCache is a bounded, mutex-guarded JWKS cache with throttled refresh for
// unknown kids so unauthenticated callbacks cannot hammer the provider.
type jwksCache struct {
	jwksURL string

	mu            sync.Mutex
	keys          map[string]crypto.PublicKey
	fetchedAt     time.Time
	lastRefresh   time.Time
	refreshing    chan struct{}
	minRefreshGap time.Duration
	maxAge        time.Duration
	httpClient    *http.Client
	nowFunc       func() time.Time
}

// newJWKSCache builds a cache for the given JWKS URL.
func newJWKSCache(jwksURL string, httpClient *http.Client, now func() time.Time) *jwksCache {
	return &jwksCache{
		jwksURL:       jwksURL,
		keys:          map[string]crypto.PublicKey{},
		minRefreshGap: 30 * time.Second,
		maxAge:        15 * time.Minute,
		httpClient:    httpClient,
		nowFunc:       now,
	}
}

// jwksPublicKey derives a verifying key from a JWK, pinning supported
// algorithms to RS256.
func jwksPublicKey(key *jwksKey) (crypto.PublicKey, error) {
	if key.Kty != "RSA" || (key.Alg != "" && key.Alg != "RS256") {
		return nil, errors.New("unsupported-key")
	}
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nBytes)
	exponent := new(big.Int).SetBytes(eBytes)
	if n.BitLen() < 2048 || n.BitLen() > 8192 || !exponent.IsInt64() || exponent.Int64() < 3 || exponent.Int64() > 2147483647 || exponent.Int64()%2 == 0 {
		return nil, errors.New("invalid-rsa-key")
	}
	return &rsa.PublicKey{N: n, E: int(exponent.Int64())}, nil
}

// fetch downloads and parses the JWKS document, bounding the response size.
func (c *jwksCache) fetch(ctx context.Context) (map[string]crypto.PublicKey, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.New("jwks-unexpected-status")
	}
	limited := io.LimitReader(response.Body, 1<<20)
	var document jwksDocument
	if err := json.NewDecoder(limited).Decode(&document); err != nil {
		return nil, err
	}
	keys := make(map[string]crypto.PublicKey, len(document.Keys))
	for i := range document.Keys {
		k := &document.Keys[i]
		if k.KID == "" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		publicKey, err := jwksPublicKey(k)
		if err != nil {
			continue
		}
		keys[k.KID] = publicKey
	}
	return keys, nil
}

// refresh replaces the cached keys, respecting the throttle window so unknown
// kids cannot force unbounded upstream traffic.
func (c *jwksCache) refresh(ctx context.Context) error {
	c.mu.Lock()
	if pending := c.refreshing; pending != nil {
		c.mu.Unlock()
		select {
		case <-pending:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	now := c.nowFunc()
	if !c.lastRefresh.IsZero() && now.Sub(c.lastRefresh) < c.minRefreshGap {
		c.mu.Unlock()
		return nil
	}
	pending := make(chan struct{})
	c.refreshing = pending
	c.lastRefresh = now
	c.mu.Unlock()
	keys, err := c.fetch(ctx)
	c.mu.Lock()
	if err == nil && len(keys) > 0 {
		c.keys = keys
		c.fetchedAt = c.nowFunc()
	}
	c.refreshing = nil
	close(pending)
	c.mu.Unlock()
	return err
}

// lookup shares a bounded refresh and fails closed when cached keys are stale.
func (c *jwksCache) lookup(ctx context.Context, kid string) (crypto.PublicKey, error) {
	c.mu.Lock()
	key, ok := c.keys[kid]
	fresh := !c.fetchedAt.IsZero() && c.nowFunc().Sub(c.fetchedAt) < c.maxAge
	c.mu.Unlock()
	if ok && fresh {
		return key, nil
	}
	if err := c.refresh(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key, ok = c.keys[kid]
	if !ok || c.fetchedAt.IsZero() || c.nowFunc().Sub(c.fetchedAt) >= c.maxAge {
		return nil, errors.New("jwks-key-unavailable")
	}
	return key, nil
}

// ParsePrivateKeyPEM extracts the first private key from PEM data, as found
// in an Apple .p8 file.
func ParsePrivateKeyPEM(data []byte) (crypto.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("pem-block-not-found")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// tokenKID extracts and validates the token's key identifier.
func tokenKID(token *jwt.Token) (string, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" || len(kid) > 256 {
		return "", errors.New("kid-missing-or-oversized")
	}
	return kid, nil
}

// decodeClaimsInto decodes the token payload into the target struct.
func decodeClaimsInto(rawToken string, target *IDTokenProfileClaims) error {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return errors.New("malformed-token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}

// logIDTokenFailure emits a fixed, non-sensitive diagnostic for a rejected
// token; raw upstream errors, tokens and profile data never reach logs.
func logIDTokenFailure(ctx context.Context, reason string) {
	log := logger.AcquirePackageFrom(ctx, "external/oauth")
	log.Warn("id-token-rejected", zap.String("reason", reason))
}

// ValidateIDToken verifies the provider ID token: signature via pinned key,
// canonical issuer and audience, presence of expiry and subject, expected
// nonce (constant time), algorithm pinning to RS256, and Google azp for
// multi-audience tokens. Context cancellation propagates into JWKS refreshes.
func ValidateIDToken(ctx context.Context, cache *jwksCache, rawToken string, expected *IDTokenExpectations) (*IDTokenClaims, error) {
	if cache == nil || expected == nil || rawToken == "" || len(rawToken) > 65536 || expected.Audience == "" || expected.Nonce == "" {
		return nil, ErrSecureIDTokenInvalid
	}

	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"}))
	unverifiedToken, _, err := parser.ParseUnverified(rawToken, &jwt.RegisteredClaims{})
	if err != nil {
		return nil, ErrSecureIDTokenInvalid
	}
	kid, err := tokenKID(unverifiedToken)
	if err != nil {
		return nil, ErrSecureIDTokenInvalid
	}
	publicKey, err := cache.lookup(ctx, kid)
	if err != nil {
		logIDTokenFailure(ctx, "jwks-key-lookup-failed")
		return nil, ErrSecureIDTokenInvalid
	}

	claims := &jwt.RegisteredClaims{}
	verifiedToken, err := jwt.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (interface{}, error) {
		return publicKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(30*time.Second), jwt.WithTimeFunc(time.Now))
	if err != nil || verifiedToken == nil || !verifiedToken.Valid {
		logIDTokenFailure(ctx, "id-token-signature-invalid")
		return nil, ErrSecureIDTokenInvalid
	}

	profileClaims := &IDTokenProfileClaims{}
	if err := decodeClaimsInto(rawToken, profileClaims); err != nil {
		logIDTokenFailure(ctx, "id-token-profile-decode-failed")
		return nil, ErrSecureIDTokenInvalid
	}

	if err := expected.validate(claims, profileClaims); err != nil {
		logIDTokenFailure(ctx, "id-token-expectations-unmet")
		return nil, err
	}

	return &IDTokenClaims{
		Issuer:        canonicalIssuer(claims.Issuer),
		Subject:       claims.Subject,
		Audience:      claims.Audience,
		Email:         profileClaims.Email,
		EmailVerified: bool(profileClaims.EmailVerified),
		Nonce:         profileClaims.Nonce,
		Expiry:        claims.ExpiresAt.Time,
		FullName:      profileClaims.Name,
		GivenName:     profileClaims.GivenName,
		FamilyName:    profileClaims.FamilyName,
		RawProfile:    profileClaims.RawUserObject,
	}, nil
}
