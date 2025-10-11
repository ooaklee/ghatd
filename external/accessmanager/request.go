package accessmanager

import (
	"net/http"
	"net/url"

	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/user"
)

// RefreshTokenRequest holds refresh token which will be used to
// generate more tokens
type RefreshTokenRequest struct {
	// RefreshToken token used for regenerating tokens
	RefreshToken string `json:"refresh_token" validate:"min=128"`

	// AccessToken this token is a by product and is not needed,
	// However if detected when making a request to refresh the refresh
	// token it should be removed so that it's not hanging
	AccessToken string
}

// CreateUserRequest holds everything needed to create user on platform
type CreateUserRequest struct {
	// FirstName user's first name
	FirstName string `json:"first_name" validate:"min=2"`

	// LastName user's last / family/ sur name
	LastName string `json:"last_name" validate:"min=2"`

	// Email user's email address that will be used for receiving
	// correspondence & signing into platform
	Email string `json:"email" validate:"min=2"`

	// Mobile whether the request originates from mobile portal
	Mobile bool `json:"mobile"`

	// RequestUrl where the user should be redirected to once
	// signed in
	RequestUrl string `json:"request_url"`

	// DisableVerificationEmail whether to disable sending
	// verification email to user after account creation
	DisableVerificationEmail bool `json:"disable_verification_email"`
}

// CreateEmailVerificationTokenRequest holds the data required for a user request
type CreateEmailVerificationTokenRequest struct {
	// User to create and send a verification token to
	User user.User

	// IsDashboardRequest whether the request originates from
	// our dashboard portal
	IsDashboardRequest bool

	// IsMobileRequest whether the request originates from
	// our mobile app
	IsMobileRequest bool

	// RequestUrl where the user should be redirected to once
	// signed in
	RequestUrl string `json:"request_url"`
}

// ValidateEmailVerificationCodeRequest holds the data required for validating user's
// email
type ValidateEmailVerificationCodeRequest struct {
	// Token the token sent embedded in the email to verify user's email
	Token string `query:"t" validate:"min=128"`
}

// TokenAsStringValidatorRequest holds the data used to validate the token as
// string passed is valid
type TokenAsStringValidatorRequest struct {
	// Token the token in string format
	Token string `query:"t" validate:"min=128"`

	// Type defines the token type so the correct parse can be carried out
	// TODO: Implement
	Type string
}

// UserEmailVerificationRevisionsRequest holds information needed to make revision on
// system to show email verification was successful
type UserEmailVerificationRevisionsRequest struct {
	// UserID the user ID the token was successfully validated for
	UserID string
}

// CreateInitalLoginOrVerificationTokenEmailRequest holds data used for generating respective
// Inital Login Or Verification Token Email
type CreateInitalLoginOrVerificationTokenEmailRequest struct {
	// Email user's registered email address
	Email string `json:"email"`

	// Dashboard whether the request originates from dashboard portal
	Dashboard bool `json:"dashboard"`

	// Mobile whether the request originates from mobile portal
	Mobile bool `json:"mobile"`

	// RequestUrl where the user should be redirected to once
	// signed in
	RequestUrl string `json:"request_url"`
}

// LoginUserRequest holds the data required for login in a user
type LoginUserRequest struct {
	// Token the token sent embedded in the email to give user authorisation on to platform
	Token string `query:"t" validate:"min=128"`
}

// CreateUserAPITokenRequest holds the data required for creating an api token
type CreateUserAPITokenRequest struct {
	// UserID the user ID the token will be created for
	UserID string

	// Ttl is the time to live on the access token
	Ttl int64 `json:"ttl"`
}

// DeleteUserAPITokenRequest holds the data required for deleting an api token
type DeleteUserAPITokenRequest struct {
	// UserID the user ID the token belongs to
	UserID string

	// APITokenID the apitoken ID that will be deleted
	APITokenID string
}

// UserAPITokenStatusRequest holds the data required for updating an api token's status
type UserAPITokenStatusRequest struct {
	// Status the desired status
	Status string

	// APITokenID the apitoken ID that will have its status updated
	APITokenID string
}

// GetSpecificUserAPITokensRequest holds the data required for get user's an api tokens
type GetSpecificUserAPITokensRequest struct {
	// UserID the user ID the tokens belongs to
	UserID string

	*apitoken.GetAPITokensForRequest
}

// GetUserAPITokenThresholdRequest holds the data required for getting
// user's an api tokens threshold based on their role
type GetUserAPITokenThresholdRequest struct {
	// UserID the user ID the tokens threshold will apply to
	UserId string
}

// OauthLoginRequest hold the data required for inititing a
// oauth provider login
type OauthLoginRequest struct {
	// The name of the provider the route belongs to
	Provider string

	// RequestUrl where the user should be redirected to once
	// signed in
	RequestUrl string `query:"request_url"`
}

// OauthCallbackRequest hold the data required for handling a
// oauth provider callback
type OauthCallbackRequest struct {
	// The name of the provider the route belongs to
	Provider string

	// UrlUri is the uri values passed back in the callback request
	UrlUri url.Values

	// RequestCookies is the cookies passed with the callback request
	RequestCookies []*http.Cookie
}

// LogoutUserOthersRequest handles logging out all other sessions for a user
type LogoutUserOthersRequest struct {

	// UserId the user ID the tokens will be deleted for
	UserId string

	// RefreshToken the current refresh token of the user that will be preserved
	// after logging out all other sessions
	RefreshToken string

	// AuthToken the current auth token of the user that will be preserved
	AuthToken string
}

// UpdateUserEmailRequest holds all the data needed to change a user's  email
type UpdateUserEmailRequest struct {

	// UserId the ID of the user making the request
	UserId string `json:"-"`

	// TargetUserId the ID of the user to update the email for
	TargetUserId string `json:"-"`

	// Email the new email to assign to the user
	Email string `json:"email"`

	// RefreshToken the current refresh token of the user
	RefreshToken string

	// AuthToken the current access token of the user
	AuthToken string

	// Request the request that triggered the update request
	Request *http.Request
}
