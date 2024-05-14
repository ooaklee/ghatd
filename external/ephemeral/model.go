package ephemeral

import (
	"time"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// TokenDetailsAuth holds methods for a passing valid
// auth token
type TokenDetailsAuth interface {
	GetTokenAccessUuid() string
	GetTokenRefreshUuid() string

	GetTokenAccessTimeToLive() time.Duration
	GetTokenRefreshTimeToLive() time.Duration
}

// TokenDetailsAccess holds methods for a passing valid
// token access details
type TokenDetailsAccess interface {
	GetTokenAccessUuid() string
	GetUserId() string
	IsUserAdmin() bool
	IsUserAuthorized() bool
}

// TokenDetails holds the token definitions
type TokenDetails struct {
	AccessToken string
	// AccessUuid uuid used to identify the access token in the store
	AccessUuid string
	// AtExpires the expiry time for the access token
	AtExpires int64
	// AtTtl declares the tokens' time to live
	AtTtl time.Duration

	RefreshToken string
	// RefreshUuid uuid used to identify the refresh token in the store
	RefreshUuid string
	// RtExpires the expiry time for the refresh token
	RtExpires int64
	// RtTtl declares the tokens' time to live
	RtTtl time.Duration

	// EphemeralToken short living token used to initiate login
	EphemeralToken string
	// EphemeralUuid uuid used to identify the emphemeral token in the store
	EphemeralUuid string
	// EtExpires the expiry time for the emphemeral token
	EtExpires int64
	// EtTtl declares the tokens' time to live
	EtTtl time.Duration

	// EmailVerificationToken token used to verify user's email
	EmailVerificationToken string
	// EmailVerificationUuid uuid used to identify the email verification token in the store
	EmailVerificationUuid string
	// EvExpires the expiry time for the verification token
	EvExpires int64
	// EvTtl declares the tokens' time to live
	EvTtl time.Duration
}

// GetTokenAccessUuidreturns the access token's uuid
func (t *TokenDetails) GetTokenAccessUuid() string {
	return t.AccessUuid
}

// GetTokenRefreshUuid returns the refresh token's uuid
func (t *TokenDetails) GetTokenRefreshUuid() string {
	return t.RefreshToken
}

// GetTokenAccessTimeToLive returns the access token's time to live
func (t *TokenDetails) GetTokenAccessTimeToLive() time.Duration {
	return t.AtTtl
}

// GetTokenAccessTimeToLive returns the refresh token's time to live
func (t *TokenDetails) GetTokenRefreshTimeToLive() time.Duration {
	return t.RtTtl
}

// GenerateEmailVerificationUuid generates an Uuidv4 for email verification token
func (t *TokenDetails) GenerateEmailVerificationUuid() *TokenDetails {

	t.EmailVerificationUuid = toolbox.GenerateUuidV4()

	return t
}

// GenerateEphemeralUuid generates an Uuidv4 for ephemeral token
func (t *TokenDetails) GenerateEphemeralUuid() *TokenDetails {

	t.EphemeralUuid = toolbox.GenerateUuidV4()

	return t
}

// GenerateRefreshUuid generates an Uuidv4 for refresh  token
func (t *TokenDetails) GenerateRefreshUuid() *TokenDetails {

	t.RefreshUuid = toolbox.GenerateUuidV4()

	return t
}

// GenerateAccessUuid generates an Uuidv4 for access token
func (t *TokenDetails) GenerateAccessUuid() *TokenDetails {

	t.AccessUuid = toolbox.GenerateUuidV4()

	return t
}

// TokenAccessDetails holds information relating to
// token and its owner
type TokenAccessDetails struct {
	AccessUuid string
	UserId     string
	IsAdmin    bool

	// IsAuthorized is true if user account is active
	// during time of token generation
	IsAuthorized bool
}

// GetTokenAccessUuid returns the access token's uuid
func (t *TokenAccessDetails) GetTokenAccessUuid() string {
	return t.AccessUuid
}

// GetTokenAccessUuid returns the user id of the token's owner
func (t *TokenAccessDetails) GetUserId() string {
	return t.UserId
}

// GetTokenAccessUuid returns whether the token's owner is an admin
func (t *TokenAccessDetails) IsUserAdmin() bool {
	return t.IsAdmin
}

// GetTokenAccessUuid returns whether the owner's account is in an active state
func (t *TokenAccessDetails) IsUserAuthorized() bool {
	return t.IsAuthorized
}
