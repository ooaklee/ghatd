package auth

import (
	"time"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// TokenDetails holds the token definitions
type TokenDetails struct {
	AccessToken string
	// AccessUUID uuid used to identify the access token in the store
	AccessUUID string
	// AtExpires the expiry time for the access token
	AtExpires int64
	// AtTTL declares the tokens' time to live
	AtTTL time.Duration

	RefreshToken string
	// RefreshUUID uuid used to identify the refresh token in the store
	RefreshUUID string
	// RtExpires the expiry time for the refresh token
	RtExpires int64
	// RtTTL declares the tokens' time to live
	RtTTL time.Duration

	// EphemeralToken short living token used to initiate login
	EphemeralToken string
	// EphemeralUUID uuid used to identify the emphemeral token in the store
	EphemeralUUID string
	// EtExpires the expiry time for the emphemeral token
	EtExpires int64
	// EtTTL declares the tokens' time to live
	EtTTL time.Duration

	// EmailVerificationToken token used to verify user's email
	EmailVerificationToken string
	// EmailVerificationUUID uuid used to identify the email verification token in the store
	EmailVerificationUUID string
	// EvExpires the expiry time for the verification token
	EvExpires int64
	// EvTTL declares the tokens' time to live
	EvTTL time.Duration
}

// GenerateEmailVerificationUUID generates an UUIDv4 for email verification token
func (t *TokenDetails) GenerateEmailVerificationUUID() *TokenDetails {

	t.EmailVerificationUUID = toolbox.GenerateUuidV4()

	return t
}

// GenerateEphemeralUUID generates an UUIDv4 for ephemeral token
func (t *TokenDetails) GenerateEphemeralUUID() *TokenDetails {

	t.EphemeralUUID = toolbox.GenerateUuidV4()

	return t
}

// GenerateRefreshUUID generates an UUIDv4 for refresh  token
func (t *TokenDetails) GenerateRefreshUUID() *TokenDetails {

	t.RefreshUUID = toolbox.GenerateUuidV4()

	return t
}

// GenerateAccessUUID generates an UUIDv4 for access token
func (t *TokenDetails) GenerateAccessUUID() *TokenDetails {

	t.AccessUUID = toolbox.GenerateUuidV4()

	return t
}

// GetTokenAccessUuid returns the Uuid for the access token
func (t *TokenDetails) GetTokenAccessUuid() string {
	return t.AccessUUID

}

// GetTokenAccessUuid returns the Uuid for the access token
func (t *TokenDetails) GetTokenRefreshUuid() string {
	return t.RefreshUUID
}

// GetTokenAccessTimeToLive returns the access token's time to live
func (t *TokenDetails) GetTokenAccessTimeToLive() time.Duration {
	return t.AtTTL
}

// GetTokenRefreshTimeToLive returns the refresh token's time to live
func (t *TokenDetails) GetTokenRefreshTimeToLive() time.Duration {
	return t.RtTTL
}

// TokenAccessDetails holds information relating to
// token and its owner
type TokenAccessDetails struct {
	// UserType is the signed account-configuration type, not a role or resource kind.
	UserType string
	// TokenUse distinguishes session access from login/email-verification proofs.
	TokenUse string
	// Issuer is the signed issuer; trust requires an explicitly configured policy.
	Issuer string
	// Audience contains only the signed JWT audience values. Hosts must apply
	// their own exact audience policy; missing audiences remain empty.
	Audience []string
	// SigningAlgorithm identifies the verified JWT algorithm, never a client header.
	SigningAlgorithm string
	// EmailRevision snapshots the account's security-sensitive email revision.
	EmailRevision int64
	// AuthenticationTime is signed session freshness; zero for legacy sessions.
	AuthenticationTime time.Time
	// AccessUUID identifies the live-store record; it is not the raw credential.
	AccessUUID string
	// UserID is the immutable signed subject.
	UserID string
	// IsAdmin is the issuance-time role snapshot, not a current permission check.
	IsAdmin bool

	// IsAuthorized is true if user account is active
	// during time of token generation
	IsAuthorized bool
}

// GetTokenAccessUuid returns the access token's uuid
func (t *TokenAccessDetails) GetTokenAccessUuid() string {
	return t.AccessUUID
}

// GetTokenAccessUuid returns the user id the  token belongs to
func (t *TokenAccessDetails) GetUserId() string {
	return t.UserID
}

// IsUserAdmin returns whether the user is an admin
func (t *TokenAccessDetails) IsUserAdmin() bool {
	return t.IsAdmin
}

// IsUserAuthorized returns  whether the user's account is activated/ in an authorised state
func (t *TokenAccessDetails) IsUserAuthorized() bool {
	return t.IsAuthorized
}

// TokenRefreshDetails holds information relating to
// refresh token and its owner
type TokenRefreshDetails struct {
	// UserType preserves the signed account type for the live pre-rotation check.
	UserType string
	// TokenUse is refresh for new tokens; empty means an untyped legacy credential.
	TokenUse string
	// EmailRevision is compared with the current account before rotation.
	EmailRevision int64
	// AuthenticationTime preserves the initial login time through refresh.
	AuthenticationTime time.Time
	// RefreshUUID identifies the single-use rotation record.
	RefreshUUID string
	// UserID is the immutable signed subject, not a mutable account handle.
	UserID string
}

// TokenEmailVerificationDetails holds information relating to
// email verification token and its owner
type TokenEmailVerificationDetails struct {
	EmailVerificationUUID string
	UserID                string
}
