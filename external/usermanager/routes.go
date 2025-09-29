package usermanager

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/router"
)

// UsermanagerHandler expected methods for valid usermanager handler
type UsermanagerHandler interface {
	UpdateUserProfile(w http.ResponseWriter, r *http.Request)
	GetUserProfile(w http.ResponseWriter, r *http.Request)
	GetUserMicroProfile(w http.ResponseWriter, r *http.Request)
	DeleteUserPermanently(w http.ResponseWriter, r *http.Request)
	CreateComms(w http.ResponseWriter, r *http.Request)
	GetComms(w http.ResponseWriter, r *http.Request)
}

const (
	// APIUserManagerPrefix base URI prefix for all usermanager routes
	APIUserManagerPrefix = common.ApiV1UriPrefix + "/ums"

	// APIUserManagerMe URI section used for actions related to requestor
	APIUserManagerMe = "/me"

	// APIUserManagerInsights URI section used for insights related calls
	APIUserManagerInsights = "/insights"
)

var (
	// APIUserManagerIDVariable URI variable used to get usermanager ID out of URI
	APIUserManagerIDVariable = fmt.Sprintf("/{%s}", UserManagerURIVariableID)

	// APIUserManagerMeMicro URI section used for getting requestor's micro account
	APIUserManagerMeMicro = APIUserManagerMe + "/micro"
)

// AttachRoutesRequest holds everything needed to attach usermanager
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by API
	Router *router.Router

	// Handler valid usermanager handler
	Handler UsermanagerHandler

	// AuthenticatedMiddleware middleware used to lock endpoints down to users that have been authenticated
	AuthenticatedMiddleware mux.MiddlewareFunc

	// ActiveOnlyMiddleware middleware used to lock endpoints down to active users only
	ActiveOnlyMiddleware mux.MiddlewareFunc

	// AdminOnlyMiddleware middleware used to lock endpoints down to admin only
	AdminOnlyMiddleware mux.MiddlewareFunc

	// ActiveValidApiTokenOrJWTMiddleware is middleware that is used to lock
	// down endpoints to either tokens or JWT (active)
	ActiveValidApiTokenOrJWTMiddleware mux.MiddlewareFunc

	// ValidApiTokenOrJWTMiddleware is middleware that is used to lock
	// down endpoints to either tokens or JWT (authenticated)
	ValidApiTokenOrJWTMiddleware mux.MiddlewareFunc

	// RateLimitOrActiveMiddleware middleware used to open endpoints up (with rate limite) or active users only
	RateLimitOrActiveMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches usermanager handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	userManagerOpenRoutes := httpRouter.PathPrefix(APIUserManagerPrefix).Subrouter()
	userManagerOpenRoutes.HandleFunc("/comms", request.Handler.CreateComms).Methods(http.MethodPost, http.MethodOptions)
	userManagerOpenRoutes.Use(request.RateLimitOrActiveMiddleware)

	usermanagerAuthenticatedRoutes := httpRouter.PathPrefix(APIUserManagerPrefix).Subrouter()
	usermanagerAuthenticatedRoutes.HandleFunc(APIUserManagerMe, request.Handler.GetUserProfile).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc(APIUserManagerMe, request.Handler.DeleteUserPermanently).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc(APIUserManagerMeMicro, request.Handler.GetUserMicroProfile).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.Use(request.ValidApiTokenOrJWTMiddleware)

	usermanagerAdminRoutes := httpRouter.PathPrefix(APIUserManagerPrefix).Subrouter()
	usermanagerAdminRoutes.HandleFunc("/comms", request.Handler.GetComms).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.Use(request.AdminOnlyMiddleware)

	usermanagerActiveOnlyRoutes := httpRouter.PathPrefix(APIUserManagerPrefix).Subrouter()
	usermanagerActiveOnlyRoutes.HandleFunc(APIUserManagerMe, request.Handler.UpdateUserProfile).Methods(http.MethodPatch, http.MethodOptions)
	usermanagerActiveOnlyRoutes.Use(request.ActiveValidApiTokenOrJWTMiddleware)
}
