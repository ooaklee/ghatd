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

	// AdminApiTokenOrJWTMiddleware locks endpoints down to either admin API tokens or admin JWT users.
	AdminApiTokenOrJWTMiddleware mux.MiddlewareFunc

	// ActiveValidApiTokenOrJWTMiddleware is middleware that is used to lock
	// down endpoints to either tokens or JWT (active)
	ActiveValidApiTokenOrJWTMiddleware mux.MiddlewareFunc

	// ValidApiTokenOrJWTMiddleware is middleware that is used to lock
	// down endpoints to either tokens or JWT (authenticated)
	ValidApiTokenOrJWTMiddleware mux.MiddlewareFunc

	// RateLimitOrActiveMiddleware middleware used to open endpoints up (with rate limite) or active users only
	RateLimitOrActiveMiddleware mux.MiddlewareFunc

	// CustomMeEndpointValidApiTokenOrJWTMiddleware is middleware exclusively
	// for the /me endpoint. Initially this exception was created  to stop the
	// return of soft-4XX (401) status, which was stopping Google from indexing
	// pages on their search engine
	CustomMeEndpointValidApiTokenOrJWTMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches usermanager handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	userManagerOpenRoutes := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	userManagerOpenRoutes.HandleFunc("/comms", request.Handler.CreateComms).Methods(http.MethodPost, http.MethodOptions)
	if request.RateLimitOrActiveMiddleware != nil {
		userManagerOpenRoutes.Use(request.RateLimitOrActiveMiddleware)
	}

	usermanagerActiveOnlyRoutesPre := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	usermanagerActiveOnlyRoutesPre.HandleFunc("/groups/config", request.Handler.GetGroupsConfig).Methods(http.MethodGet, http.MethodOptions)
	if request.ActiveValidApiTokenOrJWTMiddleware != nil {
		usermanagerActiveOnlyRoutesPre.Use(request.ActiveValidApiTokenOrJWTMiddleware)
	}

	// Special case route for /me endpoint to allow user to handle situations such
	// as avoiding 401s being returned to Google when it tries to index the page
	// without credentials
	userMeEndpointRoute := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	userMeEndpointRoute.HandleFunc("/me", request.Handler.GetUserProfile).Methods(http.MethodGet, http.MethodOptions)
	if request.CustomMeEndpointValidApiTokenOrJWTMiddleware != nil {
		userMeEndpointRoute.Use(request.CustomMeEndpointValidApiTokenOrJWTMiddleware)
	} else if request.ValidApiTokenOrJWTMiddleware != nil {
		userMeEndpointRoute.Use(request.ValidApiTokenOrJWTMiddleware)
	}

	usermanagerAuthenticatedRoutes := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	usermanagerAuthenticatedRoutes.HandleFunc("/me", request.Handler.DeleteUserPermanently).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/micro", request.Handler.GetUserMicroProfile).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/enriched", request.Handler.GetEnrichedUserProfile).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/memberships", request.Handler.GetUserGroupMembershipsRequest).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/groups", request.Handler.GetUserGroups).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/invitations", request.Handler.GetMyGroupInvitations).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/invitations/{groupID}/accept", request.Handler.AcceptMyGroupInvitation).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/invitations/{groupID}/reject", request.Handler.RejectMyGroupInvitation).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/reminders", request.Handler.ListReminders).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/reminders", request.Handler.CreateReminder).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/reminders/{reminderID}", request.Handler.GetReminderByID).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/reminders/{reminderID}", request.Handler.UpdateReminderByID).Methods(http.MethodPatch, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/reminders/{reminderID}", request.Handler.DeleteReminderByID).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/reminders/{reminderID}/disable", request.Handler.DisableReminderByID).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/streaks", request.Handler.ListStreaks).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/streaks/record", request.Handler.RecordStreak).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/streaks/current", request.Handler.GetCurrentStreak).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/streaks/longest", request.Handler.GetLongestStreak).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/streaks/count", request.Handler.GetNumberOfStreaks).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/notifications/latest", request.Handler.GetLatestNotificationOverviews).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/notifications/config", request.Handler.GetNotifierConfig).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/notifications/addresses", request.Handler.ListNotificationAddresses).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/notifications/addresses", request.Handler.RegisterNotificationAddress).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/notifications/addresses/{addressID}", request.Handler.DeleteNotificationAddress).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/notifications/preferences", request.Handler.GetNotificationPreferences).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/me/notifications/preferences", request.Handler.UpdateNotificationPreferences).Methods(http.MethodPatch, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/users", request.Handler.GetUsers).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/users/{userId}", request.Handler.GetUserByID).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/users/{userId}/groups", request.Handler.GetGroupsByUserID).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/validate-name", request.Handler.ValidateGroupName).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/{groupID}", request.Handler.GetGroupDetail).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/{groupID}/lineage", request.Handler.GetGroupLineage).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/{groupID}/stats", request.Handler.GetGroupStats).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAuthenticatedRoutes.HandleFunc("/groups/{groupID}/descendants", request.Handler.GetGroupDescendants).Methods(http.MethodGet, http.MethodOptions)
	if request.ValidApiTokenOrJWTMiddleware != nil {
		usermanagerAuthenticatedRoutes.Use(request.ValidApiTokenOrJWTMiddleware)
	}

	usermanagerAdminRoutes := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	usermanagerAdminRoutes.HandleFunc("/comms", request.Handler.GetComms).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/comms/stats", request.Handler.GetCommsStats).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/comms/{id}", request.Handler.UpdateComms).Methods(http.MethodPut, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/notifications/config", request.Handler.GetNotifierConfig).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/notifications/latest", request.Handler.GetLatestNotificationOverviews).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/notifications/{userId}/latest", request.Handler.GetLatestNotificationOverviews).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/notifications/addresses", request.Handler.ListNotificationAddresses).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/notifications/addresses", request.Handler.RegisterNotificationAddress).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/notifications/{userId}/addresses/{addressID}", request.Handler.DeleteNotificationAddress).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/notifications/{userId}/preferences", request.Handler.GetNotificationPreferences).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/notifications/{userId}/preferences", request.Handler.UpdateNotificationPreferences).Methods(http.MethodPatch, http.MethodOptions)
	usermanagerAdminRoutes.HandleFunc("/users", request.Handler.GetUsers).Methods(http.MethodGet, http.MethodOptions)
	if request.AdminOnlyMiddleware != nil {
		usermanagerAdminRoutes.Use(request.AdminOnlyMiddleware)
	}

	usermanagerAdminServiceRoutes := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	usermanagerAdminServiceRoutes.HandleFunc("/users/{userId}/notifications", request.Handler.NotifyUser).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAdminServiceRoutes.HandleFunc("/notifications", request.Handler.NotifyUsers).Methods(http.MethodPost, http.MethodOptions)
	usermanagerAdminServiceRoutes.HandleFunc("/reminders", request.Handler.ListReminders).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminServiceRoutes.HandleFunc("/reminders/stats", request.Handler.GetReminderStats).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminServiceRoutes.HandleFunc("/reminders/due", request.Handler.GetDueReminders).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminServiceRoutes.HandleFunc("/streaks", request.Handler.ListStreaks).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminServiceRoutes.HandleFunc("/streaks/current", request.Handler.GetCurrentStreak).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminServiceRoutes.HandleFunc("/streaks/longest", request.Handler.GetLongestStreak).Methods(http.MethodGet, http.MethodOptions)
	usermanagerAdminServiceRoutes.HandleFunc("/streaks/count", request.Handler.GetNumberOfStreaks).Methods(http.MethodGet, http.MethodOptions)
	if request.AdminApiTokenOrJWTMiddleware != nil {
		usermanagerAdminServiceRoutes.Use(request.AdminApiTokenOrJWTMiddleware)
	} else if request.AdminOnlyMiddleware != nil {
		usermanagerAdminServiceRoutes.Use(request.AdminOnlyMiddleware)
	}

	usermanagerActiveOnlyRoutes := httpRouter.PathPrefix(APIUserManagerV1Prefix).Subrouter()
	usermanagerActiveOnlyRoutes.HandleFunc("/groups", request.Handler.CreateGroup).Methods(http.MethodPost, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}", request.Handler.UpdateGroup).Methods(http.MethodPatch, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}", request.Handler.DeleteGroup).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}/owner", request.Handler.UpdateGroupOwner).Methods(http.MethodPut, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}/members", request.Handler.AddGroupMember).Methods(http.MethodPost, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}/members/{memberID}", request.Handler.RemoveGroupMember).Methods(http.MethodDelete, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/groups/{groupID}/members/{memberID}", request.Handler.UpdateGroupMember).Methods(http.MethodPatch, http.MethodOptions)
	usermanagerActiveOnlyRoutes.HandleFunc("/me", request.Handler.UpdateUserProfile).Methods(http.MethodPatch, http.MethodOptions)
	if request.ActiveValidApiTokenOrJWTMiddleware != nil {
		usermanagerActiveOnlyRoutes.Use(request.ActiveValidApiTokenOrJWTMiddleware)
	}
}
