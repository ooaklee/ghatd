package accessmanager

import (
	"github.com/ooaklee/ghatd/external/oauth"
	"net/http"
	"net/url"

	"github.com/ooaklee/ghatd/external/apitoken"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
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
	User *userv2.UniversalUser

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
	Token string `query:"t" validate:"omitempty,min=128"`

	// Code the 8-character alphanumeric code provided instead of the token
	Code string `query:"c" validate:"omitempty,len=8,alphanum"`
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
	// UserType carries trusted signed context, never a value taken from JSON input.
	UserType      string `json:"-"`
	EmailRevision int64
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
	Token string `query:"t" validate:"omitempty,min=128"`

	// Code the 8-character alphanumeric code provided instead of the token
	Code string `query:"c" validate:"omitempty,len=8,alphanum"`
}

// CreateUserAPITokenRequest holds the data required for creating an api token
type CreateUserAPITokenRequest struct {
	// ActorID comes only from verified session context, never request data.
	ActorID string `json:"-" query:"-"`
	// UserID selects the owner and must equal ActorID; administrators cannot
	// create another account's credentials through this self-service command.
	UserID string `json:"-" query:"-"`

	// Ttl is the time to live on the access token
	Ttl int64 `json:"ttl"`

	// Description is a reminder about or name for this token
	// that will be created. If left empty, a random codename string
	// will be generated and assigned as the token's description
	Description string `json:"description,omitempty"`
}

// DeleteUserAPITokenRequest holds the data required for deleting an api token
type DeleteUserAPITokenRequest struct {
	// ActorID is the verified session caller, independent of target selection.
	ActorID string `json:"-" query:"-"`
	// UserID selects the owner and must match ActorID.
	UserID string `json:"-" query:"-"`

	// APITokenID the apitoken ID that will be deleted
	APITokenID string `json:"-" query:"-"`
}

// UserAPITokenStatusRequest holds the data required for updating an api token's status
type UserAPITokenStatusRequest struct {
	// ActorID is the verified session caller, not an API credential owner claim.
	ActorID string `json:"-" query:"-"`
	// UserID is the target owner; both mapper and manager enforce self-service.
	UserID string `json:"-" query:"-"`
	// Status is chosen by the route, never decoded from the request body.
	Status string `json:"-" query:"-"`

	// APITokenID the apitoken ID that will have its status updated
	APITokenID string `json:"-" query:"-"`
}

// GetSpecificUserAPITokensRequest holds the data required for get user's an api tokens
type GetSpecificUserAPITokensRequest struct {
	// ActorID comes from authenticated session context.
	ActorID string `json:"-" query:"-"`
	// UserID selects the target owner and must equal ActorID.
	UserID string `json:"-" query:"-"`

	// GetAPITokensForRequest carries display filters. The manager snapshots them
	// and replaces embedded ID/NanoId/TotalCount rather than trusting selectors.
	*apitoken.GetAPITokensForRequest `json:"-" query:"-"`
}

// GetUserAPITokenThresholdRequest holds the data required for getting
// user's an api tokens threshold based on their role
type GetUserAPITokenThresholdRequest struct {
	// ActorID comes from authenticated session context.
	ActorID string `json:"-" query:"-"`
	// UserID selects the owner whose current policy is displayed, not authority
	// to issue a future credential. It must equal ActorID.
	UserID string `json:"-" query:"-"`
}

// OauthLoginRequest hold the data required for inititing a
// oauth provider login
type OauthLoginRequest struct {
	// Mobile is accepted only from a consumed server-side native start ticket.
	Mobile *oauth.MobileFlowContext
	// Browser opts into completion by redirect rather than an API token response.
	Browser bool
	// Link is accepted only from server-authenticated initiation.
	Link *oauth.LinkProof
	// The name of the provider the route belongs to
	Provider string

	// RequestUrl where the user should be redirected to once
	// signed in
	RequestUrl string `query:"request_url"`
}

// OauthCallbackRequest hold the data required for handling a
// oauth provider callback
type OauthCallbackRequest struct {
	Method string
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

// UpdateUserEmailRequest separates verified caller identity from account selection.
// The HTTP mapper decodes only Email; no cookies or request objects cross this port.
type UpdateUserEmailRequest struct {
	// ActorID is supplied by authentication middleware or a trusted service caller.
	ActorID string `json:"-"`
	// TargetUserID is selected from the route and independently authorized.
	TargetUserID string `json:"-"`
	// Email is the requested new mailbox; the user domain normalizes and validates it.
	Email string `json:"email"`
}
