package usermanager

import (
	"net/http"

	"github.com/gorilla/mux"
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
	// Group/Team management methods
	GetEnrichedUserProfile(w http.ResponseWriter, r *http.Request)
	GetUserGroups(w http.ResponseWriter, r *http.Request)
	GetUserTeamMemberships(w http.ResponseWriter, r *http.Request)
	UpdateUserTeamMembership(w http.ResponseWriter, r *http.Request)
	RemoveUserFromGroup(w http.ResponseWriter, r *http.Request)
	FindUserInfo(w http.ResponseWriter, r *http.Request)
	BulkUpdateUserGroupMemberships(w http.ResponseWriter, r *http.Request)
	GetGroupsByType(w http.ResponseWriter, r *http.Request)
	CreateGroup(w http.ResponseWriter, r *http.Request)
}

const (
	// APIUserManagerPrefix base URI prefix for all usermanager routes
	APIUserManagerV2Prefix = "/api/v1/ums"
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

	userManagerOpenRoutes := httpRouter.PathPrefix(APIUserManagerV2Prefix).Subrouter()
	userManagerOpenRoutes.HandleFunc("/comms", request.Handler.CreateComms).Methods(http.MethodPost, http.MethodOptions)
	userManagerOpenRoutes.Use(request.RateLimitOrActiveMiddleware)

	usermanagerAuthenticatedRoutes := httpRouter.PathPrefix(APIUserManagerV2Prefix).Subrouter()
	usermanagerAuthenticatedRoutes.HandleFunc("/me", request.Handler.GetUserProfile).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me", request.Handler.DeleteUserPermanently).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/micro", request.Handler.GetUserMicroProfile).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/enriched", request.Handler.GetEnrichedUserProfile).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/groups", request.Handler.GetUserGroups).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/teams/memberships", request.Handler.GetUserTeamMemberships).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/{"+UserManagerURIVariableGroupType+"}", request.Handler.GetGroupsByType).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.Use(request.ValidApiTokenOrJWTMiddleware)

	usermanagerAdminRoutes := httpRouter.PathPrefix(APIUserManagerV2Prefix).Subrouter()
	usermanagerAdminRoutes.HandleFunc("/comms", request.Handler.GetComms).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/admin/users/{id}/groups/bulk", request.Handler.BulkUpdateUserGroupMemberships).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/admin/groups", request.Handler.CreateGroup).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAdminRoutes.Use(request.AdminOnlyMiddleware)

	usermanagerActiveOnlyRoutes := httpRouter.PathPrefix(APIUserManagerV2Prefix).Subrouter()
	usermanagerActiveOnlyRoutes.HandleFunc("/me", request.Handler.UpdateUserProfile).Methods(http.MethodPatch, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/me/teams/memberships/{"+UserManagerURIVariableGroupID+"}", request.Handler.UpdateUserTeamMembership).Methods(http.MethodPut, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/me/teams/memberships/{"+UserManagerURIVariableGroupID+"}", request.Handler.RemoveUserFromGroup).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerActiveOnlyRoutes.Use(request.ActiveValidApiTokenOrJWTMiddleware)

	// User info lookup - available to both admin and active users
	usermanagerUserInfoRoutes := httpRouter.PathPrefix(APIUserManagerV2Prefix).Subrouter()
	usermanagerUserInfoRoutes.HandleFunc("/users/info", request.Handler.FindUserInfo).Methods(http.MethodGet, http.MethodOptions)
	usermanagerUserInfoRoutes.Use(request.ValidApiTokenOrJWTMiddleware)
}
