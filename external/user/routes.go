package user

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/router"
)

// UserHandler expected methods for valid user handler
type UserHandler interface {
	CreateUser(w http.ResponseWriter, r *http.Request)
	GetUsers(w http.ResponseWriter, r *http.Request)
	GetUserByID(w http.ResponseWriter, r *http.Request)
	UpdateUser(w http.ResponseWriter, r *http.Request)
	DeleteUser(w http.ResponseWriter, r *http.Request)
	GetMicroProfile(w http.ResponseWriter, r *http.Request)
	GetProfile(w http.ResponseWriter, r *http.Request)
}

const (
	// APIUsersPrefix base URI prefix for all users routes
	APIUsersPrefix = common.ApiV1UriPrefix + "/users"

	// APIUsersIDVariable URI variable used to get user ID out of URI
	APIUsersIDVariable = "/{userID}"
)

var (

	// APIUserProfile URI prefix for user profile calls
	APIUserProfile = APIUsersIDVariable + "/profile"

	// APIUserMicroProfile URI prefix for user micro profile calls
	APIUserMicroProfile = APIUsersIDVariable + "/micro"
)

// AttachRoutesRequest holds everything needed to attach user
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by API
	Router *router.Router

	// Handler valid user handler
	Handler UserHandler

	// AdminOnlyMiddleware middleware used to lock endpoints down to admin only
	AdminOnlyMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches user handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	usersAdminOnlyRoutes := httpRouter.PathPrefix(APIUsersPrefix).Subrouter()
	usersAdminOnlyRoutes.HandleFunc("", request.Handler.CreateUser).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("", request.Handler.GetUsers).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersIDVariable, request.Handler.GetUserByID).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersIDVariable, request.Handler.UpdateUser).Methods(http.MethodPatch, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersIDVariable, request.Handler.DeleteUser).Methods(http.MethodDelete, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUserProfile, request.Handler.GetProfile).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUserMicroProfile, request.Handler.GetMicroProfile).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.Use(request.AdminOnlyMiddleware)
}
