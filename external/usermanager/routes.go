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
	GetAvailableCommsTypes(w http.ResponseWriter, r *http.Request)
	// Group/Team management methods
	GetEnrichedUserProfile(w http.ResponseWriter, r *http.Request)
	GetUserGroupMembershipsRequest(w http.ResponseWriter, r *http.Request)
	GetUserGroups(w http.ResponseWriter, r *http.Request)
	GetLatestNotificationOverviews(w http.ResponseWriter, r *http.Request)
	GetNotifierConfig(w http.ResponseWriter, r *http.Request)
	RegisterNotificationAddress(w http.ResponseWriter, r *http.Request)
	ListNotificationAddresses(w http.ResponseWriter, r *http.Request)
	DeleteNotificationAddress(w http.ResponseWriter, r *http.Request)
	GetNotificationPreferences(w http.ResponseWriter, r *http.Request)
	UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request)
	NotifyUser(w http.ResponseWriter, r *http.Request)
	NotifyUsers(w http.ResponseWriter, r *http.Request)
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
	UpdateGroupMember(w http.ResponseWriter, r *http.Request)
	UpdateGroupOwner(w http.ResponseWriter, r *http.Request)
	GetGroupsByUserID(w http.ResponseWriter, r *http.Request)
	GetGroupsConfig(w http.ResponseWriter, r *http.Request)
	GetGroupLineage(w http.ResponseWriter, r *http.Request)
	GetGroupDescendants(w http.ResponseWriter, r *http.Request)
	ValidateGroupName(w http.ResponseWriter, r *http.Request)
	// Reminder methods
	CreateReminder(w http.ResponseWriter, r *http.Request)
	GetReminderByID(w http.ResponseWriter, r *http.Request)
	ListReminders(w http.ResponseWriter, r *http.Request)
	UpdateReminderByID(w http.ResponseWriter, r *http.Request)
	DeleteReminderByID(w http.ResponseWriter, r *http.Request)
	DisableReminderByID(w http.ResponseWriter, r *http.Request)
	GetReminderStats(w http.ResponseWriter, r *http.Request)
	GetDueReminders(w http.ResponseWriter, r *http.Request)
	// Streak methods
	RecordStreak(w http.ResponseWriter, r *http.Request)
	ListStreaks(w http.ResponseWriter, r *http.Request)
	GetCurrentStreak(w http.ResponseWriter, r *http.Request)
	GetLongestStreak(w http.ResponseWriter, r *http.Request)
	GetNumberOfStreaks(w http.ResponseWriter, r *http.Request)
	// Vision methods
	CreateVision(w http.ResponseWriter, r *http.Request)
	GetVisions(w http.ResponseWriter, r *http.Request)
	GetVisionByNanoID(w http.ResponseWriter, r *http.Request)
	GetVisionConfig(w http.ResponseWriter, r *http.Request)
	UpdateVision(w http.ResponseWriter, r *http.Request)
	UpdateVisionStatus(w http.ResponseWriter, r *http.Request)
	DeleteVision(w http.ResponseWriter, r *http.Request)
	SetVisionVote(w http.ResponseWriter, r *http.Request)
	RemoveVisionVote(w http.ResponseWriter, r *http.Request)
	AddVisionComment(w http.ResponseWriter, r *http.Request)
	SetVisionCommentVote(w http.ResponseWriter, r *http.Request)
	RemoveVisionCommentVote(w http.ResponseWriter, r *http.Request)
}

const (
	// APIUserManagerV1Prefix base URI prefix for all usermanager routes
	APIUserManagerV1Prefix = "/api/v1/ums"
)

// AttachRoutesRequest holds everything needed to attach usermanager
// routes to router
type AttachRoutesRequest struct {
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
