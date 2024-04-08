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
	CreateUser(w http.ResponseWriter, r *http.Request)
	ValidateEmailVerificationCode(w http.ResponseWriter, r *http.Request)
	CreateInitalLoginOrVerificationTokenEmail(w http.ResponseWriter, r *http.Request)
	LoginUser(w http.ResponseWriter, r *http.Request)
	RefreshToken(w http.ResponseWriter, r *http.Request)
	LogoutUser(w http.ResponseWriter, r *http.Request)
	CreateUserAPIToken(w http.ResponseWriter, r *http.Request)
	DeleteUserAPIToken(w http.ResponseWriter, r *http.Request)
	ActivateUserAPIToken(w http.ResponseWriter, r *http.Request)
	RevokeUserAPIToken(w http.ResponseWriter, r *http.Request)
	GetSpecificUserAPITokens(w http.ResponseWriter, r *http.Request)
	GetUserAPITokenThreshold(w http.ResponseWriter, r *http.Request)
	GetSpecificEphemeralUserAPITokens(w http.ResponseWriter, r *http.Request)
	OauthLogin(w http.ResponseWriter, r *http.Request)
	OauthCallback(w http.ResponseWriter, r *http.Request)
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

	// APIAccessManagerUserAPITokenSlat URI section used for calls to Short lived user api token
	APIAccessManagerUserAPITokenSlat = "/slat"

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

	// APIAccessManagerUserIDAPIShortLivedToken URI used for managing user Ephemeral API token calls
	APIAccessManagerUserIDAPIShortLivedToken = APIAccessManagerUser + APIAccessManagerUserIDVariable + APIAccessManagerUserToken + APIAccessManagerUserAPITokenSlat

	// APIAccessManagerUserIDAPITokenThreshold URI used for managing user API token threshold calls
	APIAccessManagerUserIDAPITokenThreshold = APIAccessManagerUser + APIAccessManagerUserIDVariable + APIAccessManagerUserToken + APIAccessManagerUserAPITokenThresholds
)

// AttachRoutesRequest holds everything needed to attach accessmanager
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by API
	Router *router.Router

	// Handler valid accessmanager handler
	Handler AccessmanagerHandler

	// ActiveOnlyMiddleware middleware used to lock endpoints down to active users only
	ActiveOnlyMiddleware mux.MiddlewareFunc

	// ActiveValidApiTokenOrJWTMiddleware is middleware that is used to lock
	// down endpoints to either tokens or JWT
	ActiveValidApiTokenOrJWTMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches accessmanager handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	accessmanagerRoutes := httpRouter.PathPrefix(APIAccessManagerPrefix).Subrouter()
	accessmanagerRoutes.HandleFunc(APIAccessManagerUserSignUp, request.Handler.CreateUser).Methods(http.MethodPost, http.MethodOptions)
	accessmanagerRoutes.HandleFunc(APIAccessManagerUserLogin, request.Handler.CreateInitalLoginOrVerificationTokenEmail).Methods(http.MethodPost, http.MethodOptions)
	accessmanagerRoutes.HandleFunc(APIAccessManagerUserLogin, request.Handler.LoginUser).Methods(http.MethodGet, http.MethodOptions)
	accessmanagerRoutes.HandleFunc(APIAccessManagerUserLogout, request.Handler.LogoutUser).Methods(http.MethodGet, http.MethodOptions)
	accessmanagerRoutes.HandleFunc(APIAccessManagerUserEmail, request.Handler.ValidateEmailVerificationCode).Methods(http.MethodGet, http.MethodOptions)
	accessmanagerRoutes.HandleFunc(APIAccessManagerUserRefreshToken, request.Handler.RefreshToken).Methods(http.MethodPost, http.MethodOptions)
	accessmanagerRoutes.HandleFunc(APIAccessManagerOauthGoogleCallback, request.Handler.OauthCallback).Methods(http.MethodGet, http.MethodOptions)
	accessmanagerRoutes.HandleFunc(APIAccessManagerOauthGoogleLogin, request.Handler.OauthLogin).Methods(http.MethodGet, http.MethodOptions)

	accessmanagerActiveOnlyRoutes := httpRouter.PathPrefix(APIAccessManagerPrefix).Subrouter()
	accessmanagerActiveOnlyRoutes.HandleFunc(APIAccessManagerUserIDAPIToken, request.Handler.CreateUserAPIToken).Methods(http.MethodPost, http.MethodOptions)
	accessmanagerActiveOnlyRoutes.HandleFunc(APIAccessManagerUserIDAPIToken, request.Handler.GetSpecificUserAPITokens).Methods(http.MethodGet, http.MethodOptions)
	accessmanagerActiveOnlyRoutes.HandleFunc(APIAccessManagerUserIDAPITokenSpecific, request.Handler.DeleteUserAPIToken).Methods(http.MethodDelete, http.MethodOptions)
	accessmanagerActiveOnlyRoutes.HandleFunc(APIAccessManagerUserIDAPITokenSpecificActivate, request.Handler.ActivateUserAPIToken).Methods(http.MethodPut, http.MethodOptions)
	accessmanagerActiveOnlyRoutes.HandleFunc(APIAccessManagerUserIDAPITokenSpecificRevoke, request.Handler.RevokeUserAPIToken).Methods(http.MethodPut, http.MethodOptions)
	accessmanagerActiveOnlyRoutes.HandleFunc(APIAccessManagerUserIDAPIShortLivedToken, request.Handler.GetSpecificEphemeralUserAPITokens).Methods(http.MethodGet, http.MethodOptions)
	accessmanagerActiveOnlyRoutes.HandleFunc(APIAccessManagerUserIDAPITokenThreshold, request.Handler.GetUserAPITokenThreshold).Methods(http.MethodGet, http.MethodOptions)

	accessmanagerActiveOnlyRoutes.Use(request.ActiveValidApiTokenOrJWTMiddleware)

}
