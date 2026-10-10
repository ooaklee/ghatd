package accessmanager

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/router"
)

// AccessmanagerHandler expected methods for valid accessmanager handler
type AccessmanagerHandler interface {
	// CreateUser handles user creation requests by mapping the request, delegating
	// to the service, and writing the created user response.
	CreateUser(w http.ResponseWriter, r *http.Request)
	// ValidateEmailVerificationCode handles one-use email proof admission,
	// publishing session cookies only for a complete validated session.
	ValidateEmailVerificationCode(w http.ResponseWriter, r *http.Request)
	// CreateInitalLoginOrVerificationTokenEmail handles initial login or
	// verification email requests, returning an enumeration-resistant accepted
	// receipt after valid mapping.
	CreateInitalLoginOrVerificationTokenEmail(w http.ResponseWriter, r *http.Request)
	// LoginUser handles email-proof login, exchanging a valid token or code for
	// session cookies after the service activates provisioned accounts.
	LoginUser(w http.ResponseWriter, r *http.Request)
	// RefreshToken handles refresh-credential rotation, publishing rotated cookies
	// and clearing them for known credential rejections.
	RefreshToken(w http.ResponseWriter, r *http.Request)
	// LogoutUser handles logout by clearing client cookies and delegating verified
	// session-record removal to the service.
	LogoutUser(w http.ResponseWriter, r *http.Request)
	// CreateUserAPIToken handles API token creation requests, publishing a
	// validated creation-only secret on success.
	CreateUserAPIToken(w http.ResponseWriter, r *http.Request)
	// DeleteUserAPIToken handles owner-bound API token deletion requests, returning
	// the established accepted blank response on success.
	DeleteUserAPIToken(w http.ResponseWriter, r *http.Request)
	// ActivateUserAPIToken handles API token activation requests by delegating the
	// status update and returning the accepted blank response on success.
	ActivateUserAPIToken(w http.ResponseWriter, r *http.Request)
	// RevokeUserAPIToken handles API token revocation requests by delegating the
	// status update and returning the accepted blank response on success.
	RevokeUserAPIToken(w http.ResponseWriter, r *http.Request)
	// GetSpecificUserAPITokens handles token listing requests, returning
	// owner-checked display rows without plaintext secrets.
	GetSpecificUserAPITokens(w http.ResponseWriter, r *http.Request)
	// GetUserAPITokenThreshold handles threshold queries, writing the owner's
	// current token display limits; a missing result is unavailable rather than
	// unlimited.
	GetUserAPITokenThreshold(w http.ResponseWriter, r *http.Request)
	// OauthLogin handles validated OAuth login requests, redirecting browsers to
	// the provider authorization URL while setting the transaction cookie.
	OauthLogin(w http.ResponseWriter, r *http.Request)
	// OauthCallback serves the OAuth provider redirect endpoint: it maps the HTTP
	// request, invokes the service callback, and completes browser redirects,
	// mobile flows, cookies, or token responses according to the returned result.
	OauthCallback(w http.ResponseWriter, r *http.Request)
	// LogoutUserOthers handles the HTTP endpoint that ends a user's other sessions:
	// it maps and validates the request, delegates the owner-scoped sweep to the
	// service, and writes an accepted response on success.
	LogoutUserOthers(w http.ResponseWriter, r *http.Request)
	// UpdateUserEmail handles the email-change endpoint: it maps the request, calls
	// the service, and returns the mutation receipt with post-commit delivery
	// flags, clearing caller cookies only for a valid self-change.
	UpdateUserEmail(w http.ResponseWriter, r *http.Request)
}

const (
	// APIAccessManagerPrefix base URI prefix for all accessmanager routes
	APIAccessManagerPrefix = common.ApiV1UriPrefix + "/ams"

	// APIAccessManagerUserSignUp URI section used for user signup
	APIAccessManagerUserSignUp = "/signup"

	// APIAccessManagerUserLogin URI section used for user login
	APIAccessManagerUserLogin = "/login"

	// APIAccessManagerUserLogout URI section used for user login
	APIAccessManagerUserLogout = "/logout"

	// APIAccessManagerUserVerify URI section used for user verification calls
	APIAccessManagerUserVerify = "/verify"

	// APIAccessManagerUserToken URI section used for user tokens related calls
	APIAccessManagerUserToken = "/tokens"

	// APIAccessManagerUser URI section used for user api tokens related calls
	APIAccessManagerUser = "/users"

	// APIAccessManagerOauth URI section used for oauth related calls
	APIAccessManagerOauth = "/oauth"

	// APIAccessManagerOauthGoogle URI section used for google oauth related calls
	APIAccessManagerOauthGoogle = "/google"

	// APIAccessManagerOauthLogin URI section used for oauth login related calls
	APIAccessManagerOauthLogin = "/login"

	// APIAccessManagerOauthCallback URI section used for oauth callback related calls
	APIAccessManagerOauthCallback = "/callback"

	// APIAccessManagerUserAPITokenActivate URI section used for calls to activate user api token
	APIAccessManagerUserAPITokenActivate = "/activate"

	// APIAccessManagerUserAPITokenRevoke URI section used for calls to revoke user api token
	APIAccessManagerUserAPITokenRevoke = "/revoke"

	// APIAccessManagerUserAPITokenThresholds URI section used for calls to manage user's api token thresholds
	APIAccessManagerUserAPITokenThresholds = "/thresholds"

	// APIAccessManagerUserEmail URI section used for user email verification calls
	APIAccessManagerUserEmail = APIAccessManagerUserVerify + "/email"

	// APIAccessManagerUserRefreshToken URI section used for user refresh token regeneration calls
	APIAccessManagerUserRefreshToken = APIAccessManagerUserToken + "/refresh"

	// APIAccessManagerOauthGoogleLogin URI section used for managing user's google oath login requests
	APIAccessManagerOauthGoogleLogin = APIAccessManagerOauth + APIAccessManagerOauthGoogle + APIAccessManagerOauthLogin

	// APIAccessManagerOauthGoogleCallback URI section used for managing user's google oath callback request
	APIAccessManagerOauthGoogleCallback = APIAccessManagerOauth + APIAccessManagerOauthGoogle + APIAccessManagerOauthCallback
)

var (
	// APIAccessManagerIDVariable URI variable used to get accessmanager ID out of URI
	APIAccessManagerIDVariable = fmt.Sprintf("/{%s}", AccessManagerURIVariableID)

	// APIAccessManagerUserIDVariable URI variable used to get user ID out of URI
	APIAccessManagerUserIDVariable = fmt.Sprintf("/{%s}", UserURIVariableID)

	// APIAccessManagerAPITokenIDVariable URI variable used to get api token ID out of URI
	APIAccessManagerAPITokenIDVariable = fmt.Sprintf("/{%s}", APITokenURIVariableID)

	// APIAccessManagerUserIDAPIToken URI used for managing user API token calls
	APIAccessManagerUserIDAPIToken = APIAccessManagerUser + APIAccessManagerUserIDVariable + APIAccessManagerUserToken

	// APIAccessManagerUserIDAPITokenSpecific URI  used for managing user API token calls for specific token
	APIAccessManagerUserIDAPITokenSpecific = APIAccessManagerUser + APIAccessManagerUserIDVariable + APIAccessManagerUserToken + APIAccessManagerAPITokenIDVariable

	// APIAccessManagerUserIDAPITokenSpecificActivate URI used for activating user API token
	APIAccessManagerUserIDAPITokenSpecificActivate = APIAccessManagerUser + APIAccessManagerUserIDVariable + APIAccessManagerUserToken + APIAccessManagerAPITokenIDVariable + APIAccessManagerUserAPITokenActivate

	// APIAccessManagerUserIDAPITokenSpecificRevoke URI used for revoking user API token
	APIAccessManagerUserIDAPITokenSpecificRevoke = APIAccessManagerUser + APIAccessManagerUserIDVariable + APIAccessManagerUserToken + APIAccessManagerAPITokenIDVariable + APIAccessManagerUserAPITokenRevoke

	// APIAccessManagerUserIDAPITokenThreshold URI used for managing user API token threshold calls
	APIAccessManagerUserIDAPITokenThreshold = APIAccessManagerUser + APIAccessManagerUserIDVariable + APIAccessManagerUserToken + APIAccessManagerUserAPITokenThresholds

	// APIAccessManagerLogoutOtherSessions is the route to log out other sessions for a user
	APIAccessManagerLogoutOtherSessions = APIAccessManagerUserLogout + "/other-sessions"
)

// AttachRoutesRequest holds everything needed to attach accessmanager
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by API
	Router *router.Router

	// Handler valid accessmanager handler
	Handler AccessmanagerHandler

	// ActiveOnlyMiddleware must verify a live, active user session, not an API
	// credential. Credential-management routes fail closed when it is absent.
	ActiveOnlyMiddleware mux.MiddlewareFunc

	// ActiveValidApiTokenOrJWTMiddleware is retained for source compatibility.
	// Credential management now uses ActiveOnlyMiddleware; this field is unused
	// by these routes and must not substitute for a session-only verifier.
	ActiveValidApiTokenOrJWTMiddleware mux.MiddlewareFunc

	// HardenedRateLimitMiddleware protects code verification endpoints from brute-force attacks
	HardenedRateLimitMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches accessmanager handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {

	accessmanagerRoutes := request.Router.NewRouteGroup(APIAccessManagerPrefix, router.Public, nil)
	accessmanagerRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserSignUp, Operation: "accessmanager.CreateUser", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateUser)
	accessmanagerRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserLogin, Operation: "accessmanager.CreateInitalLoginOrVerificationTokenEmail", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateInitalLoginOrVerificationTokenEmail)
	accessmanagerRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserLogout, Operation: "accessmanager.LogoutUser", Methods: []string{http.MethodGet, http.MethodOptions}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "session-removal-only"}}, request.Handler.LogoutUser)
	accessmanagerRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserRefreshToken, Operation: "accessmanager.RefreshToken", Methods: []string{http.MethodPost, http.MethodOptions}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "refresh-token-and-live-rotation"}}, request.Handler.RefreshToken)
	accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/{provider:google|apple}/callback", Operation: "accessmanager.OauthCallback", Methods: []string{http.MethodGet, http.MethodPost, http.MethodOptions}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "oauth-state-pkce-provider-identity"}}, request.Handler.OauthCallback)
	accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/{provider:google|apple}/login", Operation: "accessmanager.OauthLogin", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.OauthLogin)
	if mobile, ok := request.Handler.(interface {
		MobileOAuthProviders(http.ResponseWriter, *http.Request)
		MobileOAuthLogin(http.ResponseWriter, *http.Request)
		MobileOAuthLink(http.ResponseWriter, *http.Request)
		MobileOAuthStart(http.ResponseWriter, *http.Request)
		MobileOAuthExchange(http.ResponseWriter, *http.Request)
	}); ok {
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/mobile/providers", Operation: "accessmanager.MobileOAuthProviders", Methods: []string{http.MethodGet}}, mobile.MobileOAuthProviders)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/{provider:google|apple}/mobile/login", Operation: "accessmanager.MobileOAuthLogin", Methods: []string{http.MethodPost}}, mobile.MobileOAuthLogin)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/{provider:google|apple}/mobile/link", Operation: "accessmanager.MobileOAuthLink", Methods: []string{http.MethodPost}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "live-session-and-recent-login"}}, mobile.MobileOAuthLink)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/mobile/start", Operation: "accessmanager.MobileOAuthStart", Methods: []string{http.MethodGet}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "one-time-launch-proof"}}, mobile.MobileOAuthStart)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/mobile/exchange", Operation: "accessmanager.MobileOAuthExchange", Methods: []string{http.MethodPost}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "one-time-grant-and-pkce"}}, mobile.MobileOAuthExchange)
	}
	if optional, ok := request.Handler.(interface {
		OAuthProviders(http.ResponseWriter, *http.Request)
		OAuthLink(http.ResponseWriter, *http.Request)
	}); ok {
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/providers", Operation: "accessmanager.OAuthProviders", Methods: []string{http.MethodGet, http.MethodOptions}}, optional.OAuthProviders)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/{provider:google|apple}/link", Operation: "accessmanager.OAuthLink", Methods: []string{http.MethodPost, http.MethodOptions}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "live-session-and-recent-login"}}, optional.OAuthLink)
	}

	if optional, ok := request.Handler.(interface {
		OAuthConnections(http.ResponseWriter, *http.Request)
		StartOAuthDisconnect(http.ResponseWriter, *http.Request)
		ConfirmOAuthDisconnect(http.ResponseWriter, *http.Request)
	}); ok {
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/connections", Operation: "accessmanager.OAuthConnections", Methods: []string{http.MethodGet}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "live-session"}}, optional.OAuthConnections)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/connections/{provider:google|apple}/disconnect", Operation: "accessmanager.StartOAuthDisconnect", Methods: []string{http.MethodPost}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "live-session-and-recent-login"}}, optional.StartOAuthDisconnect)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/connections/{provider:google|apple}/disconnect/confirm", Operation: "accessmanager.ConfirmOAuthDisconnect", Methods: []string{http.MethodPost}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "session-and-one-time-challenge"}}, optional.ConfirmOAuthDisconnect)
	}
	if optional, ok := request.Handler.(interface {
		ReviewOAuthDisconnectChallenge(http.ResponseWriter, *http.Request)
	}); ok {
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/connections/{provider:google|apple}/disconnect/challenges/{challengeID:[A-Za-z0-9_-]{43}}", Operation: "accessmanager.ReviewOAuthDisconnectChallenge", Methods: []string{http.MethodGet}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "session-and-one-time-challenge"}}, optional.ReviewOAuthDisconnectChallenge)
	}

	if optional, ok := request.Handler.(interface {
		OAuthConnectionVerification(http.ResponseWriter, *http.Request)
	}); ok {
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/connections/{provider:google|apple}/reauthenticate", Operation: "accessmanager.OAuthConnectionVerification", Methods: []string{http.MethodPost}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "session-and-provider-challenge"}}, optional.OAuthConnectionVerification)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/connections/{provider:google|apple}/reauthenticate/confirm", Operation: "accessmanager.OAuthConnectionVerification", Methods: []string{http.MethodPost}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "session-and-provider-challenge"}}, optional.OAuthConnectionVerification)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/connections/{provider:google|apple}/reauthenticate/challenges/{challengeID:[A-Za-z0-9_-]{43}}", Operation: "accessmanager.OAuthConnectionVerification", Methods: []string{http.MethodGet}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "session-and-provider-challenge"}}, optional.OAuthConnectionVerification)
	}

	if native, ok := request.Handler.(interface {
		MobileOAuthConnections(http.ResponseWriter, *http.Request)
		StartMobileOAuthDisconnect(http.ResponseWriter, *http.Request)
		ConfirmMobileOAuthDisconnect(http.ResponseWriter, *http.Request)
		ReviewMobileOAuthDisconnectChallenge(http.ResponseWriter, *http.Request)
	}); ok {
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/mobile/connections", Operation: "accessmanager.MobileOAuthConnections", Methods: []string{http.MethodGet}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "live-native-session"}}, native.MobileOAuthConnections)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/mobile/connections/{provider:google|apple}/disconnect", Operation: "accessmanager.StartMobileOAuthDisconnect", Methods: []string{http.MethodPost}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "native-session-and-recent-login"}}, native.StartMobileOAuthDisconnect)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/mobile/connections/{provider:google|apple}/disconnect/confirm", Operation: "accessmanager.ConfirmMobileOAuthDisconnect", Methods: []string{http.MethodPost}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "native-session-and-one-time-challenge"}}, native.ConfirmMobileOAuthDisconnect)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/mobile/connections/{provider:google|apple}/disconnect/challenges/{challengeID:[A-Za-z0-9_-]{43}}", Operation: "accessmanager.ReviewMobileOAuthDisconnectChallenge", Methods: []string{http.MethodGet}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "native-session-and-one-time-challenge"}}, native.ReviewMobileOAuthDisconnectChallenge)
	}

	if native, ok := request.Handler.(interface {
		MobileOAuthConnectionVerification(http.ResponseWriter, *http.Request)
	}); ok {
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/mobile/connections/{provider:google|apple}/reauthenticate", Operation: "accessmanager.MobileOAuthConnectionVerification", Methods: []string{http.MethodPost}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "native-session-and-provider-challenge"}}, native.MobileOAuthConnectionVerification)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/mobile/connections/{provider:google|apple}/reauthenticate/confirm", Operation: "accessmanager.MobileOAuthConnectionVerification", Methods: []string{http.MethodPost}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "native-session-and-provider-challenge"}}, native.MobileOAuthConnectionVerification)
		accessmanagerRoutes.Handle(router.RouteDefinition{Path: "/oauth/mobile/connections/{provider:google|apple}/reauthenticate/challenges/{challengeID:[A-Za-z0-9_-]{43}}", Operation: "accessmanager.MobileOAuthConnectionVerification", Methods: []string{http.MethodGet}, Access: router.HandlerVerified, Policy: router.RoutePolicy{Proof: "native-session-and-provider-challenge"}}, native.MobileOAuthConnectionVerification)
	}

	codeVerifyRoutes := request.Router.NewRouteGroup(APIAccessManagerPrefix, router.PublicRateLimited, request.HardenedRateLimitMiddleware)
	codeVerifyRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserLogin, Operation: "accessmanager.LoginUser", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.LoginUser)
	codeVerifyRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserEmail, Operation: "accessmanager.ValidateEmailVerificationCode", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ValidateEmailVerificationCode)

	accessmanagerCredentialManagementRoutes := request.Router.NewRouteGroup(APIAccessManagerPrefix, router.ActiveSession, request.ActiveOnlyMiddleware)
	accessmanagerCredentialManagementRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserIDAPIToken, Operation: "accessmanager.CreateUserAPIToken", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateUserAPIToken)
	accessmanagerCredentialManagementRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserIDAPIToken, Operation: "accessmanager.GetSpecificUserAPITokens", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetSpecificUserAPITokens)
	accessmanagerCredentialManagementRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserIDAPITokenSpecific, Operation: "accessmanager.DeleteUserAPIToken", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteUserAPIToken)
	accessmanagerCredentialManagementRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserIDAPITokenSpecificActivate, Operation: "accessmanager.ActivateUserAPIToken", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.ActivateUserAPIToken)
	accessmanagerCredentialManagementRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserIDAPITokenSpecificRevoke, Operation: "accessmanager.RevokeUserAPIToken", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.RevokeUserAPIToken)
	accessmanagerCredentialManagementRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerUserIDAPITokenThreshold, Operation: "accessmanager.GetUserAPITokenThreshold", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserAPITokenThreshold)
	accessmanagerCredentialManagementRoutes.Handle(router.RouteDefinition{Path: APIAccessManagerLogoutOtherSessions, Operation: "accessmanager.LogoutUserOthers", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.LogoutUserOthers)

	accessmanagerActiveOnlyRoutes := request.Router.NewRouteGroup(APIAccessManagerPrefix, router.ActiveSession, request.ActiveOnlyMiddleware)
	accessmanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/users/{userID}/email", Operation: "accessmanager.UpdateUserEmail", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateUserEmail)

}
