package accessmanager

import (
	"net/http"

	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/user"
)

// CreateUserAPITokenResponse  holds response data for CreateUserAPIToken request
type CreateUserAPITokenResponse struct {
	// UserAPIToken represents the apitoken created on the platform
	UserAPIToken apitoken.UserAPIToken
}

// CreateUserResponse holds response data for CreateUserResponse request
type CreateUserResponse struct {
	// User represents the user created on the platform
	User user.User
}

// TokenAsStringValidatorResponse holds the response for TokenAsStringValidator request
type TokenAsStringValidatorResponse struct {
	// UserID represents the user ID pulled from the token
	UserID string

	// TokenID is the ID used to identify the token in the ephemeral store
	TokenID string
}

// ValidateEmailVerificationCodeResponse hold the response for ValidateEmailVerificationCode request
type ValidateEmailVerificationCodeResponse struct {
	// AccessToken represents the access token for the verified user
	AccessToken string

	// RefreshToken represents the access token for the verified user
	RefreshToken string

	// AccessToken represents the time the access token for the verified user expires
	AccessTokenExpiresAt int64

	// RefreshToken represents the time the access token for the verified user expires
	RefreshTokenExpiresAt int64
}

// LoginUserResponse hold the response for LoginUser request
type LoginUserResponse struct {
	// AccessToken represents the access token for the logged in user
	AccessToken string

	// RefreshToken represents the access token for the logged in user
	RefreshToken string

	// AccessToken represents the time the access token for the verified user expires
	AccessTokenExpiresAt int64

	// RefreshToken represents the time the access token for the verified user expires
	RefreshTokenExpiresAt int64
}

// RefreshTokenResponse hold the response for RefreshToken request
type RefreshTokenResponse struct {
	// AccessToken represents the refreshed access token for the logged in user
	AccessToken string

	// RefreshToken represents the refreshed refresh token for the logged in user
	RefreshToken string

	// AccessToken represents the time the access token for the verified user expires
	AccessTokenExpiresAt int64

	// RefreshToken represents the time the access token for the verified user expires
	RefreshTokenExpiresAt int64
}

// GetSpecificUserAPITokensResponse the response for GetSpecificUserAPITokens request
type GetSpecificUserAPITokensResponse struct {
	// UserAPITokens represents the apitokens owned by user
	UserAPITokens []apitoken.UserAPIToken
}

// GetUserAPITokenThresholdResponse holds the data returned to
// represent a user's an api tokens threshold based on their role
type GetUserAPITokenThresholdResponse struct {
	PermanentUserTokenLimit     int64 `json:"permanent_token_limit"`
	EphemeralUserTokenLimit     int64 `json:"ephemeral_token_limit"`
	EphemeralMinimumAllowedTime int64 `json:"ephemeral_minimum_allowed_time"`
	EphemeralMaximumAllowedTime int64 `json:"ephemeral_maximum_allowed_time"`
	EphemeralMinimumIncrements  int64 `json:"ephemeral_minimum_increment"`
}

// OauthLoginResponse hold the data returned when inititing a
// oauth provider login
type OauthLoginResponse struct {

	// CookieCore is a cookie with some of the information
	// needed for creating intial cookie
	CookieCore *http.Cookie

	// ProviderAuthCodeUrl is the Url for going to the providers'
	// portal for verifying user account
	ProviderAuthCodeUrl string
}

// OauthCallbackResponse hold the data returned when handling a
// oauth provider callback
type OauthCallbackResponse struct {

	// AccessToken represents the access token for the logged in user
	AccessToken string

	// RefreshToken represents the access token for the logged in user
	RefreshToken string

	// AccessToken represents the time the access token for the verified user expires
	AccessTokenExpiresAt int64

	// RefreshToken represents the time the access token for the verified user expires
	RefreshTokenExpiresAt int64

	// ProviderStateCookieKey is the name of the cookie used to hold the protection state (token)
	ProviderStateCookieKey string

	// RequestUrl where the user should be redirected to once
	// signed in
	RequestUrl string
}
