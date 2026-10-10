package group

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// GroupHandler interface defines expected methods for valid group handler
type GroupHandler interface {
	// CreateGroup serves HTTP group creation requests: the Handler maps and
	// validates the request, delegates to the GroupService, and responds with
	// status 201 and the created group.
	CreateGroup(w http.ResponseWriter, r *http.Request)
	// GetGroupByID serves HTTP requests to fetch a single group by its ID: the
	// Handler maps and validates the request, delegates to the GroupService, and
	// responds with the group.
	GetGroupByID(w http.ResponseWriter, r *http.Request)
	// GetGroupLineage serves HTTP requests for a group's root-first ancestry: the
	// Handler delegates to the GroupService and responds with the lineage including
	// the group itself.
	GetGroupLineage(w http.ResponseWriter, r *http.Request)
	// GetGroupDescendants serves HTTP requests for a group's descendants grouped by
	// depth level: the Handler delegates to the GroupService and responds with
	// direct children at index 0, grandchildren next, and so on.
	GetGroupDescendants(w http.ResponseWriter, r *http.Request)
	// GetGroupByNanoID serves HTTP requests fetching a single group by its nano ID;
	// the Handler implementation maps and validates the request, delegates to the
	// group service, and writes the found group or an error response.
	GetGroupByNanoID(w http.ResponseWriter, r *http.Request)
	// UpdateGroup serves HTTP requests updating an existing group; the Handler
	// implementation validates the request, delegates to the service which merges
	// changes and persists them, and responds with the updated group.
	UpdateGroup(w http.ResponseWriter, r *http.Request)
	// DeleteGroup serves HTTP requests deleting a group; the Handler implementation
	// validates the request, delegates to the service which cascades soft or hard
	// deletion over descendants, and responds with no content on success.
	DeleteGroup(w http.ResponseWriter, r *http.Request)
	// GetGroups serves HTTP requests listing groups with filters and pagination;
	// the Handler implementation validates the request, delegates to the service,
	// and returns the page of groups optionally with pagination metadata.
	GetGroups(w http.ResponseWriter, r *http.Request)
	// GetGroupsByUserID serves HTTP requests listing groups where a user is
	// referenced as owner or member; the Handler implementation validates the
	// request, delegates to the service, and writes the matching groups (and
	// optional descendants) as the response.
	GetGroupsByUserID(w http.ResponseWriter, r *http.Request)
	// GetGroupsAwaitingAnswerForInvitationsByMemberID serves HTTP requests listing
	// groups with pending invitations for a member ID; the Handler implementation
	// validates the request, delegates to the service, and returns the filtered
	// groups.
	GetGroupsAwaitingAnswerForInvitationsByMemberID(w http.ResponseWriter, r *http.Request)
	// AddMember serves HTTP requests adding a member to a group; the Handler
	// implementation validates the request, delegates to the service which enforces
	// role and membership rules and persists the change, and responds with the
	// updated group.
	AddMember(w http.ResponseWriter, r *http.Request)
	// InviteUser serves HTTP requests adding a pending email invitation to a
	// top-level group; the Handler implementation validates the request, delegates
	// to the service, and responds with the updated group.
	InviteUser(w http.ResponseWriter, r *http.Request)
	// UninviteUser serves HTTP requests revoking a pending invitation from a
	// top-level group; the Handler implementation validates the request, delegates
	// to the service which removes the pending invite, and responds with the
	// updated group.
	UninviteUser(w http.ResponseWriter, r *http.Request)
	// AcceptInvite serves HTTP requests accepting a pending group invitation; the
	// Handler implementation validates the request, delegates to the service which
	// materialises membership, and responds with the updated group.
	AcceptInvite(w http.ResponseWriter, r *http.Request)
	// RejectInvite serves HTTP requests rejecting a pending group invitation; the
	// Handler implementation validates the request, delegates to the service which
	// removes the pending invite, and responds with the updated group.
	RejectInvite(w http.ResponseWriter, r *http.Request)
	// RemoveMember serves HTTP requests removing a member from a group; the Handler
	// implementation validates the request, delegates to the service which persists
	// removal and cascades to descendants for root groups, and responds with the
	// updated group.
	RemoveMember(w http.ResponseWriter, r *http.Request)
	// UpdateMemberRole serves HTTP requests changing a member's role; the Handler
	// implementation validates the request, delegates to the service which
	// validates the role against the group type configuration and persists it, and
	// responds with the updated group.
	UpdateMemberRole(w http.ResponseWriter, r *http.Request)
	// GetGroupMembers serves HTTP requests listing a group's members; the Handler
	// implementation validates the request, delegates to the service which filters
	// by member type and role, and returns the members.
	GetGroupMembers(w http.ResponseWriter, r *http.Request)
	// UpdateOwner serves HTTP requests changing a group's owner; the Handler
	// implementation validates the request, delegates to the service which promotes
	// or adds the owner as an admin member and persists the change, and responds
	// with the updated group.
	UpdateOwner(w http.ResponseWriter, r *http.Request)
	// RepairInvalidMembers serves HTTP requests repairing groups containing members
	// with empty or null IDs; the Handler implementation delegates to the service
	// and returns the affected group IDs before and after the repair.
	RepairInvalidMembers(w http.ResponseWriter, r *http.Request)
	// ArchiveGroup serves HTTP requests archiving a group; the Handler
	// implementation validates the request, delegates to the service which sets the
	// group status to archived and persists it, and responds with the updated
	// group.
	ArchiveGroup(w http.ResponseWriter, r *http.Request)
	// RestoreGroup serves HTTP requests restoring an archived group; the Handler
	// implementation validates the request, delegates to the service which sets the
	// group status back to active and persists it, and responds with the updated
	// group.
	RestoreGroup(w http.ResponseWriter, r *http.Request)
	// GetGroupStats serves HTTP requests for a single group's statistics; the
	// Handler implementation validates the request, delegates to the service which
	// computes member and subgroup counts, and writes the statistics response.
	GetGroupStats(w http.ResponseWriter, r *http.Request)
	// GetGroupsStats serves HTTP requests for aggregate statistics across all
	// groups; the Handler implementation delegates to the service with an empty
	// request and writes the aggregated counts response.
	GetGroupsStats(w http.ResponseWriter, r *http.Request)
	// GetGroupsConfig serves HTTP requests for the group service configuration; the
	// Handler implementation delegates to the service and writes the configured
	// group capabilities as the response.
	GetGroupsConfig(w http.ResponseWriter, r *http.Request)
	// ValidateGroupName serves HTTP requests previewing how a proposed group name
	// resolves without persisting anything; the Handler implementation validates
	// the request, delegates to the service, and returns the resolved name,
	// availability and hint.
	ValidateGroupName(w http.ResponseWriter, r *http.Request)
	// EnableGroupAutoJoinByEmailDomain handles the HTTP request that enables
	// email-domain based auto-join for a group. As a GroupHandler port it maps the
	// request, delegates to the service, and writes a data or error HTTP response.
	EnableGroupAutoJoinByEmailDomain(w http.ResponseWriter, r *http.Request)
	// DisableGroupAutoJoinByEmailDomain handles the HTTP request that disables
	// email-domain based auto-join for a group. As a GroupHandler port it maps the
	// request, delegates to the service, and writes a data or error HTTP response.
	DisableGroupAutoJoinByEmailDomain(w http.ResponseWriter, r *http.Request)
	// EnableGroupAutoInviteByEmailDomain handles the HTTP request that enables
	// email-domain based auto-invite for a group. As a GroupHandler port it maps
	// the request, delegates to the service, and writes a data or error HTTP
	// response.
	EnableGroupAutoInviteByEmailDomain(w http.ResponseWriter, r *http.Request)
	// DisableGroupAutoInviteByEmailDomain handles the HTTP request that disables
	// email-domain based auto-invite for a group. As a GroupHandler port it maps
	// the request, delegates to the service, and writes a data or error HTTP
	// response.
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
