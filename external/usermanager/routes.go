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
	GetUserByID(w http.ResponseWriter, r *http.Request)
	GetUsers(w http.ResponseWriter, r *http.Request)
	GetUserMicroProfile(w http.ResponseWriter, r *http.Request)
	DeleteUserPermanently(w http.ResponseWriter, r *http.Request)
	CreateComms(w http.ResponseWriter, r *http.Request)
	GetComms(w http.ResponseWriter, r *http.Request)
	UpdateComms(w http.ResponseWriter, r *http.Request)
	GetCommsStats(w http.ResponseWriter, r *http.Request)
	// Group/Team management methods
	GetEnrichedUserProfile(w http.ResponseWriter, r *http.Request)
	GetUserGroupMembershipsRequest(w http.ResponseWriter, r *http.Request)
	GetUserGroups(w http.ResponseWriter, r *http.Request)
	GetLatestNotificationOverviews(w http.ResponseWriter, r *http.Request)
	GetMyGroupInvitations(w http.ResponseWriter, r *http.Request)
	AcceptMyGroupInvitation(w http.ResponseWriter, r *http.Request)
	RejectMyGroupInvitation(w http.ResponseWriter, r *http.Request)
	GetGroupDetail(w http.ResponseWriter, r *http.Request)
	GetGroupStats(w http.ResponseWriter, r *http.Request)
	CreateGroup(w http.ResponseWriter, r *http.Request)
	UpdateGroup(w http.ResponseWriter, r *http.Request)
	DeleteGroup(w http.ResponseWriter, r *http.Request)
	// Group management methods
	AddGroupMember(w http.ResponseWriter, r *http.Request)
	RemoveGroupMember(w http.ResponseWriter, r *http.Request)
	UpdateGroupOwner(w http.ResponseWriter, r *http.Request)
	GetGroupsByUserID(w http.ResponseWriter, r *http.Request)
	GetGroupsConfig(w http.ResponseWriter, r *http.Request)
	GetGroupLineage(w http.ResponseWriter, r *http.Request)
	GetGroupDescendants(w http.ResponseWriter, r *http.Request)
	ValidateGroupName(w http.ResponseWriter, r *http.Request)
}

const (
	// APIUserManagerV1Prefix base URI prefix for all usermanager routes
	APIUserManagerV1Prefix = "/api/v1/ums"
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

	userManagerOpenRoutes := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	userManagerOpenRoutes.HandleFunc("/comms", request.Handler.CreateComms).Methods(http.MethodPost, http.MethodOptions)
	userManagerOpenRoutes.Use(request.RateLimitOrActiveMiddleware)

	usermanagerActiveOnlyRoutesPre := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	usermanagerActiveOnlyRoutesPre.HandleFunc("/groups/config", request.Handler.GetGroupsConfig).Methods(http.MethodGet, http.MethodOptions)
	usermanagerActiveOnlyRoutesPre.Use(request.ActiveValidApiTokenOrJWTMiddleware)

	usermanagerAuthenticatedRoutes := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	usermanagerAuthenticatedRoutes.HandleFunc("/me", request.Handler.GetUserProfile).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me", request.Handler.DeleteUserPermanently).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/micro", request.Handler.GetUserMicroProfile).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/enriched", request.Handler.GetEnrichedUserProfile).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/memberships", request.Handler.GetUserGroupMembershipsRequest).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/groups", request.Handler.GetUserGroups).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/invitations", request.Handler.GetMyGroupInvitations).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/invitations/{groupID}/accept", request.Handler.AcceptMyGroupInvitation).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/invitations/{groupID}/reject", request.Handler.RejectMyGroupInvitation).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/notifications/latest", request.Handler.GetLatestNotificationOverviews).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/users", request.Handler.GetUsers).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/users/{userId}", request.Handler.GetUserByID).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/users/{userId}/groups", request.Handler.GetGroupsByUserID).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/validate-name", request.Handler.ValidateGroupName).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/{groupID}", request.Handler.GetGroupDetail).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/{groupID}/lineage", request.Handler.GetGroupLineage).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/{groupID}/stats", request.Handler.GetGroupStats).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/{groupID}/descendants", request.Handler.GetGroupDescendants).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.Use(request.ValidApiTokenOrJWTMiddleware)

	usermanagerAdminRoutes := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	usermanagerAdminRoutes.HandleFunc("/comms", request.Handler.GetComms).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/comms/stats", request.Handler.GetCommsStats).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/comms/{id}", request.Handler.UpdateComms).Methods(http.MethodPut, http.MethodOptions)
	usermanagerAdminRoutes.Use(request.AdminOnlyMiddleware)

	usermanagerActiveOnlyRoutes := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	usermanagerActiveOnlyRoutes.HandleFunc("/groups", request.Handler.CreateGroup).Methods(http.MethodPost, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}", request.Handler.UpdateGroup).Methods(http.MethodPatch, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}", request.Handler.DeleteGroup).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}/owner", request.Handler.UpdateGroupOwner).Methods(http.MethodPut, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}/members", request.Handler.AddGroupMember).Methods(http.MethodPost, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}/members/{memberID}", request.Handler.RemoveGroupMember).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/me", request.Handler.UpdateUserProfile).Methods(http.MethodPatch, http.MethodOptions)
	usermanagerActiveOnlyRoutes.Use(request.ActiveValidApiTokenOrJWTMiddleware)
}
