package usermanager

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// UsermanagerHandler expected methods for valid usermanager handler
type UsermanagerHandler interface {
	// UpdateUserProfile serves HTTP requests updating the authenticated caller's
	// own profile name attributes. It maps and validates the request, invokes the
	// manager service, and writes the updated user response.
	UpdateUserProfile(w http.ResponseWriter, r *http.Request)
	// GetUserProfile serves HTTP requests fetching a user's profile. It maps and
	// validates the request, delegates to the manager service, which resolves the
	// profile by actor with admin override for other users, and writes the profile
	// response.
	GetUserProfile(w http.ResponseWriter, r *http.Request)
	// GetUserByID serves HTTP requests fetching a user by identifier. It maps and
	// validates the request and writes the user returned by the manager service,
	// which permits self-access and admin access to other accounts.
	GetUserByID(w http.ResponseWriter, r *http.Request)
	// GetUsers serves HTTP requests listing users. It maps and validates the
	// request and writes the user list, optionally with pagination metadata; the
	// manager service restricts non-admin callers to their accessible group
	// memberships.
	GetUsers(w http.ResponseWriter, r *http.Request)
	// GetUserMicroProfile serves HTTP requests fetching the authenticated caller's
	// micro profile. It maps and validates the request and writes the compact
	// profile returned by the manager service.
	GetUserMicroProfile(w http.ResponseWriter, r *http.Request)
	// DeleteUserPermanently serves HTTP requests to delete the caller's account and
	// platform resources. It maps and validates the request, delegates to the
	// manager service, and clears authentication cookies on success and failure.
	DeleteUserPermanently(w http.ResponseWriter, r *http.Request)
	// CreateComms serves HTTP requests creating a comms conversation. It maps and
	// validates the request, delegates to the manager service, and writes the
	// creation receipt with Created status.
	CreateComms(w http.ResponseWriter, r *http.Request)
	// GetComms serves HTTP requests listing comms. It maps and validates the
	// request, delegates to the manager service, and writes the comms list,
	// including pagination metadata when requested.
	GetComms(w http.ResponseWriter, r *http.Request)
	// UpdateComms serves HTTP requests updating a comms conversation. It maps and
	// validates the request, delegates to the manager service, and writes the
	// updated comms.
	UpdateComms(w http.ResponseWriter, r *http.Request)
	// GetCommsStats serves HTTP requests fetching comms statistics. It maps and
	// validates the request, delegates to the manager service, and writes the
	// returned stats.
	GetCommsStats(w http.ResponseWriter, r *http.Request)
	// GetAvailableCommsTypes serves public discovery of configured contact
	// categories. It delegates to the manager service and writes the comms types,
	// exposing configuration only and no comms records.
	GetAvailableCommsTypes(w http.ResponseWriter, r *http.Request)
	// Group/Team management methods
	GetEnrichedUserProfile(w http.ResponseWriter, r *http.Request)
	// GetUserGroupMembershipsRequest serves HTTP requests fetching a user's team
	// memberships. It maps and validates the request, delegates to the manager
	// service, and writes the membership response.
	GetUserGroupMembershipsRequest(w http.ResponseWriter, r *http.Request)
	// GetUserGroups serves HTTP requests fetching a user's group memberships with
	// filtering. It maps and validates the request, delegates to the manager
	// service, and writes group summaries, optionally with pagination metadata.
	GetUserGroups(w http.ResponseWriter, r *http.Request)
	// GetLatestNotificationOverviews serves HTTP requests fetching the latest
	// notification overviews. The manager service pins the recipient to the trusted
	// actor for self-service queries and requires an active administrator for
	// AdminView selections.
	GetLatestNotificationOverviews(w http.ResponseWriter, r *http.Request)
	// GetNotifierConfig serves HTTP requests fetching the public notifier
	// configuration describing available push channels and subscription keys. It
	// maps and validates the request and writes the non-user-specific config.
	GetNotifierConfig(w http.ResponseWriter, r *http.Request)
	// RegisterNotificationAddress serves HTTP requests registering a push
	// destination for the authenticated user. It maps and validates the request and
	// writes the sanitised address summary with Created status.
	RegisterNotificationAddress(w http.ResponseWriter, r *http.Request)
	// ListNotificationAddresses serves HTTP requests listing registered push
	// destinations. It maps and validates the request and writes the sanitised
	// address list, optionally with pagination metadata; the service chooses admin
	// or per-user listing by view.
	ListNotificationAddresses(w http.ResponseWriter, r *http.Request)
	// DeleteNotificationAddress serves HTTP requests removing one registered push
	// destination. It maps and validates the request, delegates to the manager
	// service, and writes a blank success response.
	DeleteNotificationAddress(w http.ResponseWriter, r *http.Request)
	// GetNotificationPreferences serves HTTP requests fetching the user's
	// notification preferences. It maps and validates the request and writes the
	// global and per-channel toggles returned by the manager service.
	GetNotificationPreferences(w http.ResponseWriter, r *http.Request)
	// UpdateNotificationPreferences serves HTTP requests changing the user's
	// notification settings. It maps and validates the request, delegates the
	// update to the manager service, and writes the resulting preferences.
	UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request)
	// NotifyUser serves admin or service notification sends to a target user's
	// active addresses. It maps and validates the request, delegates to the manager
	// service, and writes the per-delivery results.
	NotifyUser(w http.ResponseWriter, r *http.Request)
	// NotifyUsers serves admin notification dispatches to multiple users across
	// channels. It maps and validates the request, delegates to the manager
	// service, and writes the per-dispatch results.
	NotifyUsers(w http.ResponseWriter, r *http.Request)
	// GetMyGroupInvitations handles the HTTP request returning the current user's
	// outstanding group invitations, mapped from groups awaiting their answer.
	GetMyGroupInvitations(w http.ResponseWriter, r *http.Request)
	// AcceptMyGroupInvitation handles the HTTP request accepting one of the current
	// user's pending group invitations.
	AcceptMyGroupInvitation(w http.ResponseWriter, r *http.Request)
	// RejectMyGroupInvitation handles the HTTP request rejecting one of the current
	// user's pending group invitations.
	RejectMyGroupInvitation(w http.ResponseWriter, r *http.Request)
	// GetGroupDetail handles the HTTP request returning a group's details, with
	// membership or admin access enforced and members plus owner enriched.
	GetGroupDetail(w http.ResponseWriter, r *http.Request)
	// GetGroupStats handles the HTTP request returning a group's statistics, with
	// membership or admin access enforced before computing seat usage and role
	// breakdown.
	GetGroupStats(w http.ResponseWriter, r *http.Request)
	// CreateGroup handles the HTTP request creating a new group, restricted to
	// admins or requesters with admin access to the specified parent group.
	CreateGroup(w http.ResponseWriter, r *http.Request)
	// UpdateGroup handles the HTTP request updating an existing group; admins may
	// update any group while non-admins need effective admin-level access to the
	// target.
	UpdateGroup(w http.ResponseWriter, r *http.Request)
	// DeleteGroup handles the HTTP request deleting a group; non-admins are
	// restricted to owning the target and hard deletion, and deletion is attributed
	// to the requester.
	DeleteGroup(w http.ResponseWriter, r *http.Request)
	// Group management methods
	AddGroupMember(w http.ResponseWriter, r *http.Request)
	// RemoveGroupMember handles the HTTP request removing a member from a group,
	// requiring admin status or admin-level access to the target group.
	RemoveGroupMember(w http.ResponseWriter, r *http.Request)
	// UpdateGroupMember handles the HTTP request updating a member's role in a
	// group, requiring admin status or admin-level access to the target group.
	UpdateGroupMember(w http.ResponseWriter, r *http.Request)
	// UpdateGroupOwner handles the HTTP request transferring group ownership,
	// requiring admin status or admin-level access, and returns the enriched new
	// owner.
	UpdateGroupOwner(w http.ResponseWriter, r *http.Request)
	// GetGroupsByUserID handles the HTTP request returning groups for a user;
	// reading another user's groups requires admin authority.
	GetGroupsByUserID(w http.ResponseWriter, r *http.Request)
	// GetGroupsConfig handles the HTTP request returning the group service
	// configuration capabilities.
	GetGroupsConfig(w http.ResponseWriter, r *http.Request)
	// GetGroupLineage handles the HTTP request returning a group's lineage, gated
	// by group access for non-admin requesters and issued as the requester.
	GetGroupLineage(w http.ResponseWriter, r *http.Request)
	// GetGroupDescendants handles the HTTP request returning a group's descendants,
	// gated by group access for non-admin requesters and issued as the requester.
	GetGroupDescendants(w http.ResponseWriter, r *http.Request)
	// ValidateGroupName handles the HTTP request validating a proposed group name;
	// non-admins must have access to the supplied parent group when one is given.
	ValidateGroupName(w http.ResponseWriter, r *http.Request)
	// Reminder methods
	CreateReminder(w http.ResponseWriter, r *http.Request)
	// GetReminderByID handles the HTTP request returning one reminder; admin
	// requesters may look up reminders without user-scope restriction.
	GetReminderByID(w http.ResponseWriter, r *http.Request)
	// ListReminders handles the HTTP request listing reminders for the
	// authenticated user, or across users when the requester is an admin.
	ListReminders(w http.ResponseWriter, r *http.Request)
	// UpdateReminderByID handles the HTTP request updating one reminder owned by
	// the currently authenticated user.
	UpdateReminderByID(w http.ResponseWriter, r *http.Request)
	// DeleteReminderByID handles the HTTP request deleting one reminder owned by
	// the currently authenticated user.
	DeleteReminderByID(w http.ResponseWriter, r *http.Request)
	// DisableReminderByID handles the HTTP request disabling one reminder owned by
	// the currently authenticated user.
	DisableReminderByID(w http.ResponseWriter, r *http.Request)
	// GetReminderStats handles the HTTP request returning aggregate reminder
	// statistics for admin or service views.
	GetReminderStats(w http.ResponseWriter, r *http.Request)
	// GetDueReminders handles the HTTP request returning reminders ready for
	// scheduler dispatch, scoped to the requester's authority.
	GetDueReminders(w http.ResponseWriter, r *http.Request)
	// Streak methods
	RecordStreak(w http.ResponseWriter, r *http.Request)
	// ListStreaks handles the HTTP request returning streak entries for the
	// authenticated user, with admin requesters able to filter across users.
	ListStreaks(w http.ResponseWriter, r *http.Request)
	// GetCurrentStreak handles the HTTP request returning the current streak count
	// for the authenticated user, defaulting to the daily period.
	GetCurrentStreak(w http.ResponseWriter, r *http.Request)
	// GetLongestStreak handles the HTTP request returning the personal best streak
	// for the authenticated user, defaulting to the daily period.
	GetLongestStreak(w http.ResponseWriter, r *http.Request)
	// GetNumberOfStreaks handles the HTTP request counting streak entries matching
	// the filters, scoped to the requester's authority.
	GetNumberOfStreaks(w http.ResponseWriter, r *http.Request)
	// Vision methods
	CreateVision(w http.ResponseWriter, r *http.Request)
	// GetVisions handles the HTTP request returning a page of vision summaries
	// enriched with associated public user data.
	GetVisions(w http.ResponseWriter, r *http.Request)
	// GetVisionByNanoID handles the HTTP request returning a privacy-safe vision
	// detail by nano ID with public user summaries.
	GetVisionByNanoID(w http.ResponseWriter, r *http.Request)
	// GetVisionConfig handles the HTTP request returning the client-safe vision
	// capabilities.
	GetVisionConfig(w http.ResponseWriter, r *http.Request)
	// UpdateVision handles the HTTP request for owner-or-admin edits restricted to
	// descriptive vision fields, excluding internal metadata, and returns an
	// enriched result.
	UpdateVision(w http.ResponseWriter, r *http.Request)
	// UpdateVisionStatus handles the HTTP request for an admin roadmap status
	// transition and returns the enriched updated vision.
	UpdateVisionStatus(w http.ResponseWriter, r *http.Request)
	// DeleteVision handles the HTTP request for owner-or-admin permanent deletion
	// of a vision after authorization.
	DeleteVision(w http.ResponseWriter, r *http.Request)
	// SetVisionVote serves the HTTP endpoint that records an authenticated vote on
	// a vision. The handler maps and validates the request, calls the
	// vision-enabled service, and writes an enriched vision response or error to w.
	SetVisionVote(w http.ResponseWriter, r *http.Request)
	// RemoveVisionVote serves the HTTP endpoint that removes an authenticated vote
	// on a vision. The handler maps and validates the request, calls the
	// vision-enabled service, and writes an enriched vision response or error to w.
	RemoveVisionVote(w http.ResponseWriter, r *http.Request)
	// AddVisionComment serves the HTTP endpoint that stores an authenticated
	// comment on a vision. The handler maps and validates the request, calls the
	// vision-enabled service, and writes an enriched vision response or error to w.
	AddVisionComment(w http.ResponseWriter, r *http.Request)
	// SetVisionCommentVote serves the HTTP endpoint that records an authenticated
	// vote on a vision comment. The handler maps and validates the request, calls
	// the vision-enabled service, and writes an enriched vision response or error
	// to w.
	SetVisionCommentVote(w http.ResponseWriter, r *http.Request)
	// RemoveVisionCommentVote serves the HTTP endpoint that removes an
	// authenticated vote on a vision comment. The handler maps and validates the
	// request, calls the vision-enabled service, and writes an enriched vision
	// response or error to w.
	RemoveVisionCommentVote(w http.ResponseWriter, r *http.Request)
}

const (
	// APIUserManagerV1Prefix base URI prefix for all usermanager routes
	APIUserManagerV1Prefix = "/api/v1/ums"
)

// AttachRoutesRequest holds everything needed to attach usermanager
// routes to router
type AttachRoutesRequest struct {
	// EnableCommsVoting registers optional private voting after its explicit
	// index migration. Routes always require AdminOnlyMiddleware and live manager
	// authorization; they never fall back to administrator API-token admission.
	EnableCommsVoting bool
	// EnableHandles registers the optional self-handle API after its explicit
	// storage migration. It requires ActiveOnlyMiddleware and a route evaluator.
	EnableHandles bool
	// Router main router being served by API
	Router *router.Router

	// Handler valid usermanager handler
	Handler UsermanagerHandler

	// AuthenticatedMiddleware is retained for source compatibility; this
	// attachment uses the session-or-API adapters instead.
	AuthenticatedMiddleware mux.MiddlewareFunc

	// ActiveOnlyMiddleware verifies an ACTIVE JWT session for optional handle
	// routes. Existing active routes still use the session-or-API adapter.
	ActiveOnlyMiddleware mux.MiddlewareFunc

	// AdminOnlyMiddleware middleware used to lock endpoints down to admin only
	AdminOnlyMiddleware mux.MiddlewareFunc

	// AdminApiTokenOrJWTMiddleware locks endpoints down to either admin API tokens or admin JWT users.
	// When nil, admin-service routes fall back to AdminOnlyMiddleware and AdminSession.
	AdminApiTokenOrJWTMiddleware mux.MiddlewareFunc

	// ActiveValidApiTokenOrJWTMiddleware is middleware that is used to lock
	// down endpoints to either tokens or JWT (active)
	ActiveValidApiTokenOrJWTMiddleware mux.MiddlewareFunc

	// ValidApiTokenOrJWTMiddleware is middleware that is used to lock
	// down endpoints to either tokens or JWT (authenticated)
	ValidApiTokenOrJWTMiddleware mux.MiddlewareFunc

	// RateLimitOrActiveMiddleware middleware used to open endpoints up (with rate limite) or active users only
	RateLimitOrActiveMiddleware mux.MiddlewareFunc

	// CustomMeEndpointValidApiTokenOrJWTMiddleware must implement ProfileOptional
	// for GET /me: permit anonymous profile discovery while verifying supplied
	// credentials. When nil, ValidApiTokenOrJWTMiddleware and SessionOrAPI apply.
	CustomMeEndpointValidApiTokenOrJWTMiddleware mux.MiddlewareFunc
}

// AttachRoutes registers user workflows with their existing credential modes.
// Optional adapters retain their documented stricter fallbacks. Every required
// adapter must be present; check Router.ValidateRoutePolicies before serving.
func AttachRoutes(request *AttachRoutesRequest) {
	if request.EnableHandles {
		attachHandleRoutes(request)
	}

	userManagerOpenRoutes := request.Router.NewRouteGroup(APIUserManagerV1Prefix, router.OptionalActive, request.RateLimitOrActiveMiddleware)
	userManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/comms", Operation: "usermanager.CreateComms", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateComms)
	userManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/comms/types", Operation: "usermanager.GetAvailableCommsTypes", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetAvailableCommsTypes)
	userManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/visions", Operation: "usermanager.GetVisions", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetVisions)
	userManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/visions/config", Operation: "usermanager.GetVisionConfig", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetVisionConfig)
	userManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/visions/{visionNanoID}", Operation: "usermanager.GetVisionByNanoID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetVisionByNanoID)

	usermanagerActiveOnlyRoutesPre := request.Router.NewRouteGroup(APIUserManagerV1Prefix, router.ActiveSessionOrAPI, request.ActiveValidApiTokenOrJWTMiddleware)
	usermanagerActiveOnlyRoutesPre.Handle(router.RouteDefinition{Path: "/groups/config", Operation: "usermanager.GetGroupsConfig", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupsConfig)

	// Special case route for /me endpoint to allow user to handle situations such
	// as avoiding 401s being returned to Google when it tries to index the page
	// without credentials
	userMeEndpointRouteMiddleware := request.CustomMeEndpointValidApiTokenOrJWTMiddleware
	userMeEndpointRouteAccess := router.ProfileOptional
	if userMeEndpointRouteMiddleware == nil {
		userMeEndpointRouteMiddleware = request.ValidApiTokenOrJWTMiddleware
		userMeEndpointRouteAccess = router.SessionOrAPI
	}
	userMeEndpointRoute := request.Router.NewRouteGroup(APIUserManagerV1Prefix, userMeEndpointRouteAccess, userMeEndpointRouteMiddleware)
	userMeEndpointRoute.Handle(router.RouteDefinition{Path: "/me", Operation: "usermanager.GetUserProfile", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserProfile)

	usermanagerAuthenticatedRoutes := request.Router.NewRouteGroup(APIUserManagerV1Prefix, router.SessionOrAPI, request.ValidApiTokenOrJWTMiddleware)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me", Operation: "usermanager.DeleteUserPermanently", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteUserPermanently)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/micro", Operation: "usermanager.GetUserMicroProfile", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserMicroProfile)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/enriched", Operation: "usermanager.GetEnrichedUserProfile", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetEnrichedUserProfile)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/memberships", Operation: "usermanager.GetUserGroupMembershipsRequest", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserGroupMembershipsRequest)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/groups", Operation: "usermanager.GetUserGroups", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserGroups)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/invitations", Operation: "usermanager.GetMyGroupInvitations", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetMyGroupInvitations)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/invitations/{groupID}/accept", Operation: "usermanager.AcceptMyGroupInvitation", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.AcceptMyGroupInvitation)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/invitations/{groupID}/reject", Operation: "usermanager.RejectMyGroupInvitation", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.RejectMyGroupInvitation)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/reminders", Operation: "usermanager.ListReminders", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ListReminders)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/reminders", Operation: "usermanager.CreateReminder", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateReminder)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/reminders/{reminderID}", Operation: "usermanager.GetReminderByID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetReminderByID)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/reminders/{reminderID}", Operation: "usermanager.UpdateReminderByID", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateReminderByID)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/reminders/{reminderID}", Operation: "usermanager.DeleteReminderByID", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteReminderByID)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/reminders/{reminderID}/disable", Operation: "usermanager.DisableReminderByID", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.DisableReminderByID)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/streaks", Operation: "usermanager.ListStreaks", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ListStreaks)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/streaks/record", Operation: "usermanager.RecordStreak", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.RecordStreak)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/streaks/current", Operation: "usermanager.GetCurrentStreak", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetCurrentStreak)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/streaks/longest", Operation: "usermanager.GetLongestStreak", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetLongestStreak)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/streaks/count", Operation: "usermanager.GetNumberOfStreaks", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetNumberOfStreaks)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/notifications/latest", Operation: "usermanager.GetLatestNotificationOverviews", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetLatestNotificationOverviews)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/notifications/config", Operation: "usermanager.GetNotifierConfig", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetNotifierConfig)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/notifications/addresses", Operation: "usermanager.ListNotificationAddresses", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ListNotificationAddresses)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/notifications/addresses", Operation: "usermanager.RegisterNotificationAddress", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.RegisterNotificationAddress)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/notifications/addresses/{addressID}", Operation: "usermanager.DeleteNotificationAddress", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteNotificationAddress)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/notifications/preferences", Operation: "usermanager.GetNotificationPreferences", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetNotificationPreferences)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/me/notifications/preferences", Operation: "usermanager.UpdateNotificationPreferences", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateNotificationPreferences)
	// Preserve the reachable member lookup. The service filters non-admins by
	// group membership; the former later admin declaration was fully shadowed.
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/users", Operation: "usermanager.GetUsers", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUsers)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/users/{userId}", Operation: "usermanager.GetUserByID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserByID)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/users/{userId}/groups", Operation: "usermanager.GetGroupsByUserID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupsByUserID)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/groups/validate-name", Operation: "usermanager.ValidateGroupName", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ValidateGroupName)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/groups/{groupID}", Operation: "usermanager.GetGroupDetail", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupDetail)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/groups/{groupID}/lineage", Operation: "usermanager.GetGroupLineage", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupLineage)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/groups/{groupID}/stats", Operation: "usermanager.GetGroupStats", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupStats)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/groups/{groupID}/descendants", Operation: "usermanager.GetGroupDescendants", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGroupDescendants)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/visions", Operation: "usermanager.CreateVision", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateVision)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/visions/{visionNanoID}", Operation: "usermanager.UpdateVision", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateVision)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/visions/{visionNanoID}", Operation: "usermanager.DeleteVision", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteVision)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/visions/{visionNanoID}/votes", Operation: "usermanager.SetVisionVote", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.SetVisionVote)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/visions/{visionNanoID}/votes", Operation: "usermanager.RemoveVisionVote", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.RemoveVisionVote)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/visions/{visionNanoID}/comments", Operation: "usermanager.AddVisionComment", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.AddVisionComment)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/visions/{visionNanoID}/comments/{commentID}/votes", Operation: "usermanager.SetVisionCommentVote", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.SetVisionCommentVote)
	usermanagerAuthenticatedRoutes.Handle(router.RouteDefinition{Path: "/visions/{visionNanoID}/comments/{commentID}/votes", Operation: "usermanager.RemoveVisionCommentVote", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.RemoveVisionCommentVote)

	usermanagerAdminRoutes := request.Router.NewRouteGroup(APIUserManagerV1Prefix, router.AdminSession, request.AdminOnlyMiddleware)
	attachCommsConversationRoutes(usermanagerAdminRoutes, request.Handler)
	if request.EnableCommsVoting {
		attachCommsVoteRoutes(usermanagerAdminRoutes, request.Handler)
	}
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/comms", Operation: "usermanager.GetComms", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetComms)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/comms/stats", Operation: "usermanager.GetCommsStats", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetCommsStats)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/comms/{id}", Operation: "usermanager.UpdateComms", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.UpdateComms)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/notifications/config", Operation: "usermanager.GetNotifierConfig", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetNotifierConfig)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/notifications/latest", Operation: "usermanager.GetLatestNotificationOverviews", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetLatestNotificationOverviews)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/notifications/{userId}/latest", Operation: "usermanager.GetLatestNotificationOverviews", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetLatestNotificationOverviews)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/notifications/addresses", Operation: "usermanager.ListNotificationAddresses", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ListNotificationAddresses)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/notifications/addresses", Operation: "usermanager.RegisterNotificationAddress", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.RegisterNotificationAddress)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/notifications/{userId}/addresses/{addressID}", Operation: "usermanager.DeleteNotificationAddress", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteNotificationAddress)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/notifications/{userId}/preferences", Operation: "usermanager.GetNotificationPreferences", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetNotificationPreferences)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/notifications/{userId}/preferences", Operation: "usermanager.UpdateNotificationPreferences", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateNotificationPreferences)
	usermanagerAdminRoutes.Handle(router.RouteDefinition{Path: "/visions/{visionNanoID}/status", Operation: "usermanager.UpdateVisionStatus", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateVisionStatus)

	usermanagerAdminServiceRoutesMiddleware := request.AdminApiTokenOrJWTMiddleware
	usermanagerAdminServiceRoutesAccess := router.AdminSessionOrAPI
	if usermanagerAdminServiceRoutesMiddleware == nil {
		usermanagerAdminServiceRoutesMiddleware = request.AdminOnlyMiddleware
		usermanagerAdminServiceRoutesAccess = router.AdminSession
	}
	usermanagerAdminServiceRoutes := request.Router.NewRouteGroup(APIUserManagerV1Prefix, usermanagerAdminServiceRoutesAccess, usermanagerAdminServiceRoutesMiddleware)
	usermanagerAdminServiceRoutes.Handle(router.RouteDefinition{Path: "/users/{userId}/notifications", Operation: "usermanager.NotifyUser", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.NotifyUser)
	usermanagerAdminServiceRoutes.Handle(router.RouteDefinition{Path: "/notifications", Operation: "usermanager.NotifyUsers", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.NotifyUsers)
	usermanagerAdminServiceRoutes.Handle(router.RouteDefinition{Path: "/reminders", Operation: "usermanager.ListReminders", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ListReminders)
	usermanagerAdminServiceRoutes.Handle(router.RouteDefinition{Path: "/reminders/stats", Operation: "usermanager.GetReminderStats", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetReminderStats)
	usermanagerAdminServiceRoutes.Handle(router.RouteDefinition{Path: "/reminders/due", Operation: "usermanager.GetDueReminders", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetDueReminders)
	usermanagerAdminServiceRoutes.Handle(router.RouteDefinition{Path: "/streaks", Operation: "usermanager.ListStreaks", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ListStreaks)
	usermanagerAdminServiceRoutes.Handle(router.RouteDefinition{Path: "/streaks/current", Operation: "usermanager.GetCurrentStreak", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetCurrentStreak)
	usermanagerAdminServiceRoutes.Handle(router.RouteDefinition{Path: "/streaks/longest", Operation: "usermanager.GetLongestStreak", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetLongestStreak)
	usermanagerAdminServiceRoutes.Handle(router.RouteDefinition{Path: "/streaks/count", Operation: "usermanager.GetNumberOfStreaks", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetNumberOfStreaks)

	usermanagerActiveOnlyRoutes := request.Router.NewRouteGroup(APIUserManagerV1Prefix, router.ActiveSessionOrAPI, request.ActiveValidApiTokenOrJWTMiddleware)
	usermanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/groups", Operation: "usermanager.CreateGroup", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateGroup)
	usermanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/groups/{groupID}", Operation: "usermanager.UpdateGroup", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateGroup)
	usermanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/groups/{groupID}", Operation: "usermanager.DeleteGroup", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteGroup)
	usermanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/groups/{groupID}/owner", Operation: "usermanager.UpdateGroupOwner", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.UpdateGroupOwner)
	usermanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/groups/{groupID}/members", Operation: "usermanager.AddGroupMember", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.AddGroupMember)
	usermanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/groups/{groupID}/members/{memberID}", Operation: "usermanager.RemoveGroupMember", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.RemoveGroupMember)
	usermanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/groups/{groupID}/members/{memberID}", Operation: "usermanager.UpdateGroupMember", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateGroupMember)
	usermanagerActiveOnlyRoutes.Handle(router.RouteDefinition{Path: "/me", Operation: "usermanager.UpdateUserProfile", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateUserProfile)

}
