package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/observability"
)

// TransactionTTL is the server-side lifetime of a sign-in transaction.
// The cookie carries an opaque handle; transaction security material stays
// in the server-side store.
const TransactionTTL = 10 * time.Minute

// SecureFlowProvider is an optional capability interface that providers may
// implement to opt into the hardened sign-in flow. Implementing it is
// source-compatible with the legacy OauthService contract: callers must probe
// for the capability and fail closed when it is absent, rather than falling
// back to email-only login that bypasses transaction and identity checks.
type SecureFlowProvider interface {
	// ProviderGetName returns the canonical provider identifier.
	ProviderGetName() string
	// ProviderGetCookieKey returns the cookie name carrying the opaque
	// transaction handle.
	ProviderGetCookieKey() string
	// BeginSecureTransaction mints a server-side transaction (state, nonce,
	// provider-specific PKCE verifier) and returns the authorisation URL plus the opaque
	// transaction identifier to place in the cookie.
	BeginSecureTransaction(ctx context.Context, returnPath string) (*SecureTransaction, error)
	// CompleteSecureTransaction validates the browser callback against the
	// stored transaction, consumes the transaction atomically, exchanges the
	// code and returns verified user information.
	CompleteSecureTransaction(ctx context.Context, r *SecureCallbackRequest) (*SecureCallbackResult, error)
}

// SecureTransaction represents a freshly minted sign-in transaction.
type SecureTransaction struct {
	// TransactionID is the high-entropy opaque handle stored in the cookie.
	TransactionID string

	// AuthorisationURL is the fully formed provider authorisation URL
	// carrying state and nonce, plus an S256 PKCE challenge for Google.
	AuthorisationURL string
}

// SecureCallbackRequest captures the browser callback inputs required by the
// hardened flow.
type SecureCallbackRequest struct {
	// TransactionID is the opaque handle recovered from the cookie.
	TransactionID string

	// Query holds the callback query parameters (code, state and, for Apple,
	// form_post user data).
	Query url.Values

	// Method is the HTTP verb used for the callback (Apple uses POST).
	Method string
}

// SecureCallbackResult is the verified outcome of a hardened callback.
type SecureCallbackResult struct {
	// UserInfo is the provider-verified profile.
	UserInfo OauthUserInfo

	// Provider is the canonical provider name.
	Provider string

	// ReturnPath is the operator-trusted return path captured at
	// initiation, never taken from the callback.
	ReturnPath string
	// Transaction is the consumed server-side browser and linking context.
	Transaction *StoredTransaction
}

// SecureFlowOptions contains server-authenticated initiation context.
type SecureFlowOptions struct {
	Browser bool               `json:"browser"`
	Link    *LinkProof         `json:"link,omitempty"`
	Mobile  *MobileFlowContext `json:"mobile,omitempty"`
}

// MobileFlowContext is trusted initiation context, stored server-side only.
// The native verifier is independent from any provider-side PKCE verifier.
type MobileFlowContext struct {
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
	Challenge   string `json:"challenge"`
}

// LinkProof binds linking to fresh, signed session evidence checked by the host.
type LinkProof struct {
	EmailRevision      int64     `json:"email_revision,omitempty"`
	UserID             string    `json:"user_id"`
	AccessUUID         string    `json:"access_uuid"`
	AuthenticationTime time.Time `json:"authentication_time"`
}

// ContextualFlowProvider accepts browser completion and authenticated linking.
type ContextualFlowProvider interface {
	SecureFlowProvider
	// BeginSecureTransactionWithOptions captures host-validated browser, linking
	// and native completion context in the stored transaction.
	BeginSecureTransactionWithOptions(context.Context, string, SecureFlowOptions) (*SecureTransaction, error)
}

// IdentityUserInfo exposes signed stable identity independently of profile email.
type IdentityUserInfo interface {
	OauthUserInfo
	// GetProviderSubject returns the stable subject verified in the ID token.
	GetProviderSubject() string
	// GetProviderIssuer returns the verified issuer that scopes the subject.
	GetProviderIssuer() string
}

// SecureTransactionStore is the server-side store for sign-in transactions.
// Implementations must make consume atomic and single-use.
type SecureTransactionStore interface {
	// Save persists a transaction under its opaque handle.
	Save(ctx context.Context, txn *StoredTransaction) error
	// Consume atomically deletes and returns the stored transaction. A
	// missing entry returns ErrSecureTransactionNotFound, making replays fail.
	Consume(ctx context.Context, transactionID string) (*StoredTransaction, error)
}

// StoredTransaction holds the security material for one sign-in attempt.
type StoredTransaction struct {
	// State is the CSRF value echoed by the provider.
	State string `bson:"state" json:"state"`

	// Nonce is bound into the provider ID token.
	Nonce string `bson:"nonce" json:"nonce"`

	// PKCEVerifier is the S256 code verifier.
	PKCEVerifier string `bson:"pkce_verifier" json:"pkce_verifier"`

	// Provider is the provider that minted the transaction.
	Provider string `bson:"provider" json:"provider"`

	// ReturnPath is the validated browser return path.
	ReturnPath string `bson:"return_path" json:"return_path"`

	Options SecureFlowOptions `json:"options"`

	// ExpiresAt marks when the transaction becomes invalid.
	ExpiresAt time.Time `bson:"expires_at" json:"expires_at"`
}

// Secure flow error keys surfaced to callers without leaking upstream detail.
const (
	// ErrKeySecureTransactionNotFound identifies a missing or already-consumed transaction.
	ErrKeySecureTransactionNotFound = "SecureTransactionNotFound"
	// ErrKeySecureTransactionInvalidState identifies a callback state mismatch.
	ErrKeySecureTransactionInvalidState = "SecureTransactionInvalidState"
	// ErrKeySecureTransactionExpired identifies an expired transaction.
	ErrKeySecureTransactionExpired = "SecureTransactionExpired"
	// ErrKeySecureIDTokenInvalid identifies invalid signed identity claims.
	ErrKeySecureIDTokenInvalid = "SecureIDTokenInvalid"
	// ErrKeySecureIDTokenUnverified identifies an unverified provider email.
	ErrKeySecureIDTokenUnverified = "SecureIDTokenUnverified"
	// ErrKeySecureProviderIncompleteConfig identifies unavailable provider configuration.
	ErrKeySecureProviderIncompleteConfig = "SecureProviderIncompleteConfig"
	// ErrKeySecureProviderInvalidPKCEConfig identifies malformed PKCE material.
	ErrKeySecureProviderInvalidPKCEConfig = "SecureProviderInvalidPKCEConfig"
)

var (
	// ErrSecureTransactionNotFound is returned when the transaction handle
	// is unknown or has already been consumed.
	ErrSecureTransactionNotFound = errors.New(ErrKeySecureTransactionNotFound)
	// ErrSecureTransactionInvalidState is returned when the state echoed by
	// the provider does not match the stored transaction.
	ErrSecureTransactionInvalidState = errors.New(ErrKeySecureTransactionInvalidState)
	// ErrSecureTransactionExpired is returned when the transaction is past
	// its expiry.
	ErrSecureTransactionExpired = errors.New(ErrKeySecureTransactionExpired)
	// ErrSecureIDTokenInvalid covers signature, issuer, audience, subject,
	// nonce, algorithm or key pinning failures.
	ErrSecureIDTokenInvalid = errors.New(ErrKeySecureIDTokenInvalid)
	// ErrSecureIDTokenUnverified is returned when the provider reports the
	// email as unverified and the flow requires verified identities.
	ErrSecureIDTokenUnverified = errors.New(ErrKeySecureIDTokenUnverified)
	// ErrSecureProviderIncompleteConfig is returned when required provider
	// configuration is missing or disabled.
	ErrSecureProviderIncompleteConfig = errors.New(ErrKeySecureProviderIncompleteConfig)
	// ErrSecureProviderInvalidPKCEConfig is returned when PKCE material is
	// malformed before it can be used.
	ErrSecureProviderInvalidPKCEConfig = errors.New(ErrKeySecureProviderInvalidPKCEConfig)
	// ErrSecureReturnPathInvalid rejects unsafe browser completion targets.
	ErrSecureReturnPathInvalid = errors.New("SecureReturnPathInvalid")
	// ErrProviderCancelled reports a provider-declared cancellation.
	ErrProviderCancelled = errors.New("OAuthCancelled")
)

// NewOpaqueTransactionID mints a high-entropy, URL-safe opaque handle. The
// cookie value carries no security material: it is only a lookup key.
func NewOpaqueTransactionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// MintPKCEPair returns a fresh S256 code verifier and its challenge.
func MintPKCEPair() (verifier string, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	digest := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(digest[:])
	return verifier, challenge, nil
}

// ConstantTimeEquals compares equal-length string contents in constant time.
// Different lengths return false immediately.
func ConstantTimeEquals(a string, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// NewSecureHTTPClient returns a private traced client for provider traffic so
// shared transports never inherit unrelated deadlines or tracing policy.
func NewSecureHTTPClient(timeout time.Duration) *http.Client {
	return observability.NewHTTPClient(http.DefaultTransport, timeout)
}

// ValidateSecureReturnPath enforces the browser-completion return-path policy:
// rooted local paths with query/fragment, no scheme, authority or backslash.
func ValidateSecureReturnPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "/app", nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || len(trimmed) > 4096 || !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") || strings.ContainsAny(trimmed, "\\\r\n\x00") || parsed.Scheme != "" || parsed.Host != "" || parsed.Opaque != "" || strings.HasPrefix(parsed.Path, "//") || strings.ContainsAny(parsed.Path, "\\\r\n\x00") {
		return "", ErrSecureReturnPathInvalid
	}
	return trimmed, nil
}
