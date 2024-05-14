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
	AccessUUID string
	UserID     string
	IsAdmin    bool

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
	RefreshUUID string
	UserID      string
}

// TokenEmailVerificationDetails holds information relating to
// email verification token and its owner
type TokenEmailVerificationDetails struct {
	EmailVerificationUUID string
	UserID                string
}
