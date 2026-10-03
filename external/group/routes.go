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
	UpdateGroup(w http.ResponseWriter, r *http.Request)
	DeleteGroup(w http.ResponseWriter, r *http.Request)
	GetGroups(w http.ResponseWriter, r *http.Request)
	GetGroupsByUserID(w http.ResponseWriter, r *http.Request)
	GetGroupsAwaitingAnswerForInvitationsByMemberID(w http.ResponseWriter, r *http.Request)
	AddMember(w http.ResponseWriter, r *http.Request)
	InviteUser(w http.ResponseWriter, r *http.Request)
	UninviteUser(w http.ResponseWriter, r *http.Request)
	AcceptInvite(w http.ResponseWriter, r *http.Request)
	RejectInvite(w http.ResponseWriter, r *http.Request)
	RemoveMember(w http.ResponseWriter, r *http.Request)
	UpdateMemberRole(w http.ResponseWriter, r *http.Request)
	GetGroupMembers(w http.ResponseWriter, r *http.Request)
	UpdateOwner(w http.ResponseWriter, r *http.Request)
	RepairInvalidMembers(w http.ResponseWriter, r *http.Request)
	ArchiveGroup(w http.ResponseWriter, r *http.Request)
	RestoreGroup(w http.ResponseWriter, r *http.Request)
	GetGroupStats(w http.ResponseWriter, r *http.Request)
	GetGroupsStats(w http.ResponseWriter, r *http.Request)
	GetGroupsConfig(w http.ResponseWriter, r *http.Request)
	ValidateGroupName(w http.ResponseWriter, r *http.Request)
	EnableGroupAutoJoinByEmailDomain(w http.ResponseWriter, r *http.Request)
	DisableGroupAutoJoinByEmailDomain(w http.ResponseWriter, r *http.Request)
	EnableGroupAutoInviteByEmailDomain(w http.ResponseWriter, r *http.Request)
	DisableGroupAutoInviteByEmailDomain(w http.ResponseWriter, r *http.Request)
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

	// AuthenticatedMiddleware is retained for source compatibility; this
	// attachment registers only administrator-session routes and does not use it.
	AuthenticatedMiddleware mux.MiddlewareFunc
}

// AttachRoutes registers administrator-session group management routes.
// AdminOnlyMiddleware is required; validate the registry before serving.
func AttachRoutes(request *AttachRoutesRequest) {

	// Admin-only routes for full group management
	groupsAdminOnlyRoutes := request.Router.NewRouteGroup(APIGroupsV1Prefix, router.AdminSession, request.AdminOnlyMiddleware)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "", Operation: "group.CreateGroup", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateGroup)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "", Operation: "group.GetGroups", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroups)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/users/{userID}", Operation: "group.GetGroupsByUserID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupsByUserID)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/stats", Operation: "group.GetGroupsStats", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupsStats)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/configs", Operation: "group.GetGroupsConfig", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupsConfig)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/validate-name", Operation: "group.ValidateGroupName", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ValidateGroupName)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/repairs/members", Operation: "group.RepairInvalidMembers", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.RepairInvalidMembers)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/invitations/{memberID}", Operation: "group.GetGroupsAwaitingAnswerForInvitationsByMemberID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupsAwaitingAnswerForInvitationsByMemberID)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}", Operation: "group.GetGroupByID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupByID)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/lineage", Operation: "group.GetGroupLineage", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupLineage)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/descendants", Operation: "group.GetGroupDescendants", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupDescendants)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}", Operation: "group.UpdateGroup", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateGroup)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}", Operation: "group.DeleteGroup", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteGroup)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/nano/{groupNanoID}", Operation: "group.GetGroupByNanoID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupByNanoID)

	// Group status operations
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/archive", Operation: "group.ArchiveGroup", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.ArchiveGroup)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/restore", Operation: "group.RestoreGroup", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.RestoreGroup)

	// Member management
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/members", Operation: "group.GetGroupMembers", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupMembers)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/members", Operation: "group.AddMember", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.AddMember)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/invitations", Operation: "group.InviteUser", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.InviteUser)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/invitations", Operation: "group.UninviteUser", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.UninviteUser)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/invitations/accept", Operation: "group.AcceptInvite", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.AcceptInvite)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/invitations/reject", Operation: "group.RejectInvite", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.RejectInvite)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/members/{memberID}", Operation: "group.RemoveMember", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.RemoveMember)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/members/{memberID}/role", Operation: "group.UpdateMemberRole", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.UpdateMemberRole)

	// Ownership management
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/owner", Operation: "group.UpdateOwner", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.UpdateOwner)

	// Auto-join/auto-invite configuration
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/auto-join/enable", Operation: "group.EnableGroupAutoJoinByEmailDomain", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.EnableGroupAutoJoinByEmailDomain)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/auto-join/disable", Operation: "group.DisableGroupAutoJoinByEmailDomain", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.DisableGroupAutoJoinByEmailDomain)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/auto-invite/enable", Operation: "group.EnableGroupAutoInviteByEmailDomain", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.EnableGroupAutoInviteByEmailDomain)
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/auto-invite/disable", Operation: "group.DisableGroupAutoInviteByEmailDomain", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.DisableGroupAutoInviteByEmailDomain)

	// Statistics
	groupsAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{groupID}/stats", Operation: "group.GetGroupStats", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupStats)

}
