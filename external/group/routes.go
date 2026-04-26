package group

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// GroupHandler interface defines expected methods for valid group handler
type GroupHandler interface {
	CreateGroup(w http.ResponseWriter, r *http.Request)
	GetGroupByID(w http.ResponseWriter, r *http.Request)
	GetGroupLineage(w http.ResponseWriter, r *http.Request)
	GetGroupDescendants(w http.ResponseWriter, r *http.Request)
	GetGroupByNanoID(w http.ResponseWriter, r *http.Request)
	GetGroupByName(w http.ResponseWriter, r *http.Request)
	UpdateGroup(w http.ResponseWriter, r *http.Request)
	DeleteGroup(w http.ResponseWriter, r *http.Request)
	GetGroups(w http.ResponseWriter, r *http.Request)
	AddMember(w http.ResponseWriter, r *http.Request)
	RemoveMember(w http.ResponseWriter, r *http.Request)
	UpdateMemberRole(w http.ResponseWriter, r *http.Request)
	GetGroupMembers(w http.ResponseWriter, r *http.Request)
	UpdateLeadership(w http.ResponseWriter, r *http.Request)
	RepairInvalidMembers(w http.ResponseWriter, r *http.Request)
	ArchiveGroup(w http.ResponseWriter, r *http.Request)
	RestoreGroup(w http.ResponseWriter, r *http.Request)
	GetGroupStats(w http.ResponseWriter, r *http.Request)
	GetGroupsStats(w http.ResponseWriter, r *http.Request)
	GetGroupsConfig(w http.ResponseWriter, r *http.Request)
	ValidateGroupName(w http.ResponseWriter, r *http.Request)
}

// APIGroupsV1Prefix base URI prefix for all v1 groups routes
const APIGroupsV1Prefix = "/api/v1/groups"

// AttachRoutesRequest holds everything needed to attach group routes to router
type AttachRoutesRequest struct {
	// Router main router being served by API
	Router *router.Router

	// Handler valid group handler
	Handler GroupHandler

	// AdminOnlyMiddleware middleware used to lock endpoints down to admin only
	AdminOnlyMiddleware mux.MiddlewareFunc

	// AuthenticatedMiddleware middleware used for authenticated users
	AuthenticatedMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches group handler to corresponding routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	// Admin-only routes for full group management
	groupsAdminOnlyRoutes := httpRouter.PathPrefix(APIGroupsV1Prefix).Subrouter()
	groupsAdminOnlyRoutes.HandleFunc("", request.Handler.CreateGroup).Methods(http.MethodPost, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("", request.Handler.GetGroups).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/stats", request.Handler.GetGroupsStats).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/configs", request.Handler.GetGroupsConfig).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/validate-name", request.Handler.ValidateGroupName).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/repairs/members", request.Handler.RepairInvalidMembers).Methods(http.MethodPost, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}", request.Handler.GetGroupByID).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}/lineage", request.Handler.GetGroupLineage).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}/descendants", request.Handler.GetGroupDescendants).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}", request.Handler.UpdateGroup).Methods(http.MethodPatch, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}", request.Handler.DeleteGroup).Methods(http.MethodDelete, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/nano/{groupNanoID}", request.Handler.GetGroupByNanoID).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/search", request.Handler.GetGroupByName).Methods(http.MethodGet, http.MethodOptions)

	// Group status operations
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}/archive", request.Handler.ArchiveGroup).Methods(http.MethodPost, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}/restore", request.Handler.RestoreGroup).Methods(http.MethodPost, http.MethodOptions)

	// Member management
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}/members", request.Handler.GetGroupMembers).Methods(http.MethodGet, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}/members", request.Handler.AddMember).Methods(http.MethodPost, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}/members/{memberID}", request.Handler.RemoveMember).Methods(http.MethodDelete, http.MethodOptions)
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}/members/{memberID}/role", request.Handler.UpdateMemberRole).Methods(http.MethodPut, http.MethodOptions)

	// Leadership management
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}/leadership", request.Handler.UpdateLeadership).Methods(http.MethodPut, http.MethodOptions)

	// Statistics
	groupsAdminOnlyRoutes.HandleFunc("/{groupID}/stats", request.Handler.GetGroupStats).Methods(http.MethodGet, http.MethodOptions)

	groupsAdminOnlyRoutes.Use(request.AdminOnlyMiddleware)

	// Authenticated routes (if needed for self-service operations)
	// Uncomment and customise as needed:
	// groupsAuthenticatedRoutes := httpRouter.PathPrefix(APIGroupsV1Prefix).Subrouter()
	// groupsAuthenticatedRoutes.HandleFunc("/my-groups", request.Handler.GetMyGroups).Methods(http.MethodGet, http.MethodOptions)
	// groupsAuthenticatedRoutes.Use(request.AuthenticatedMiddleware)
}
