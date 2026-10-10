package usermanager

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// UsermanagerService manages business logic around usermanager request
type UsermanagerService interface {
	// GetUserMicroProfile returns the requesting actor's micro profile by
	// delegating to the user service with the actor ID. The manager service wraps
	// the domain response for the handler.
	GetUserMicroProfile(ctx context.Context, r *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error)
	// GetUserProfile fetches a user's profile; when the requested ID differs from
	// the actor, the implementation loads the requesting user and rejects non-admin
	// access with an authorisation error.
	GetUserProfile(ctx context.Context, r *GetUserProfileRequest) (*GetUserProfileResponse, error)
	// GetUserByID fetches a user by the requested ID; when the target differs from
	// the actor, the implementation loads the requesting user and rejects non-admin
	// access with an authorisation error.
	GetUserByID(ctx context.Context, r *GetUserByIDRequest) (*GetUserByIDResponse, error)
	// GetUsers lists users for the actor's filters. Non-admin callers must supply
	// an accessible group ID; otherwise an empty list is returned, and accessible
	// root-group member IDs constrain the visible users.
	GetUsers(ctx context.Context, r *GetUsersRequest) (*GetUsersResponse, error)
	// UpdateUserProfile updates the authenticated caller's own profile names after
	// rechecking session or API credentials, live ACTIVE status, and email
	// revision, then delegating a guarded domain write and best-effort audit.
	UpdateUserProfile(ctx context.Context, r *UpdateUserProfileRequest) (*UpdateUserProfileResponse, error)
	// DeleteUserPermanently deletes the target user and their platform resources.
	// Self-deletion or admin authority is enforced for other-target requests; group
	// memberships, the account and owned API tokens are removed, with a best-effort
	// audit event.
	DeleteUserPermanently(ctx context.Context, r *DeleteUserPermanentlyRequest) error
	// CreateComms creates a comms by delegating to the contacter service and
	// returning the created comms. The handler responds with the comms creation
	// receipt on success.
	CreateComms(ctx context.Context, req *CreateCommsRequest) (*CreateCommsResponse, error)
	// GetComms returns comms matching the request filters by delegating to the
	// contacter service, including pagination metadata when the underlying response
	// provides it.
	GetComms(ctx context.Context, req *GetCommsRequest) (*GetCommsResponse, error)
	// UpdateComms updates a comms by delegating to the contacter service and
	// returns the updated comms in the response.
	UpdateComms(ctx context.Context, req *UpdateCommsRequest) (*UpdateCommsResponse, error)
	// GetCommsStats returns comms statistics by delegating to the contacter service
	// and wrapping the resulting stats for the handler.
	GetCommsStats(ctx context.Context, req *GetCommsStatsRequest) (*GetCommsStatsResponse, error)
	// GetAvailableCommsTypes returns the contact categories configured by the
	// underlying contacter service. It exposes configuration only and does not
	// return comms records.
	GetAvailableCommsTypes(ctx context.Context) (*GetAvailableCommsTypesResponse, error)
	// Group/Team management methods
	GetEnrichedUserProfile(ctx context.Context, r *GetEnrichedUserProfileRequest) (*GetEnrichedUserProfileResponse, error)
	// GetUserGroupMemberships returns group memberships for the trusted caller
	// only, projecting groups by the actor's ID with optional descendants, name
	// prefix and group type filters.
	GetUserGroupMemberships(ctx context.Context, r *GetUserGroupMembershipsRequest) (*GetUserGroupMembershipsResponse, error)
	// GetUserGroups returns the actor's group memberships as summaries with paging
	// and optional metadata, filtered by group types, status and name prefix via
	// the group service.
	GetUserGroups(ctx context.Context, r *GetUserGroupsRequest) (*GetUserGroupsResponse, error)
	// GetLatestNotificationOverviews returns notification overviews. Self-service
	// defaults to the actor's live email, ignoring recipient selectors; explicit
	// AdminView requires a live ACTIVE administrator before selecting another
	// account or invite email.
	GetLatestNotificationOverviews(ctx context.Context, r *GetLatestNotificationOverviewsRequest) (*GetLatestNotificationOverviewsResponse, error)
	// GetNotifierConfig returns the public, user-independent notifier configuration
	// describing available push channels and subscription keys, by delegating to
	// the notifier service.
	GetNotifierConfig(ctx context.Context, r *GetNotifierConfigRequest) (*GetNotifierConfigResponse, error)
	// RegisterNotificationAddress registers a push destination for the
	// authenticated user, forwarding a web-push subscription or FCM token to the
	// notifier service; the response omits endpoint URLs and tokens.
	RegisterNotificationAddress(ctx context.Context, r *RegisterNotificationAddressRequest) (*RegisterNotificationAddressResponse, error)
	// ListNotificationAddresses returns the current user's registered push
	// destinations with channel, device and status details but no endpoints or
	// tokens; AdminView switches to the administrative listing.
	ListNotificationAddresses(ctx context.Context, r *ListNotificationAddressesRequest) (*ListNotificationAddressesResponse, error)
	// DeleteNotificationAddress removes a single registered push destination
	// belonging to the current user by delegating to the notifier service's delete
	// operation.
	DeleteNotificationAddress(ctx context.Context, r *DeleteNotificationAddressRequest) error
	// GetNotificationPreferences returns the current user's notification
	// preferences, with per-channel toggles, defaulting to all channels enabled
	// when none were previously set.
	GetNotificationPreferences(ctx context.Context, r *GetNotificationPreferencesRequest) (*GetNotificationPreferencesResponse, error)
	// UpdateNotificationPreferences changes the current user's notification
	// settings, applying an optional global enable flag and per-channel toggles;
	// unknown channel names are rejected by the notifier service.
	UpdateNotificationPreferences(ctx context.Context, r *UpdateNotificationPreferencesRequest) (*UpdateNotificationPreferencesResponse, error)
	// NotifyUser dispatches a push notification to a target user's active
	// addresses, honouring that user's notification preferences. Part of
	// UsermanagerService; r selects the target user, title, message, channels and
	// payload data, and the response reports delivery results.
	NotifyUser(ctx context.Context, r *NotifyUserRequest) (*NotifyUserResponse, error)
	// NotifyUsers dispatches a push notification to multiple users across selected
	// channels, honouring per-user preferences; an empty user list targets all
	// users with active addresses. Part of UsermanagerService; r carries
	// recipients, title, message, channels and payload data.
	NotifyUsers(ctx context.Context, r *NotifyUsersRequest) (*NotifyUsersResponse, error)
	// GetMyGroupInvitations returns the requester's outstanding group invitations,
	// resolved via the requester's ActorID and email. Part of UsermanagerService; r
	// identifies the requester and optional name prefix, and the response lists
	// pending invitations with group and member details.
	GetMyGroupInvitations(ctx context.Context, r *GetMyGroupInvitationsRequest) (*GetMyGroupInvitationsResponse, error)
	// AcceptMyGroupInvitation accepts one of the requester's pending group
	// invitations. Part of UsermanagerService; r carries the requester's ActorID
	// and target GroupID, and the response wraps the underlying accept-invite
	// result after resolving the requester's email.
	AcceptMyGroupInvitation(ctx context.Context, r *AcceptMyGroupInvitationRequest) (*AcceptMyGroupInvitationResponse, error)
	// RejectMyGroupInvitation rejects one of the requester's pending group
	// invitations. Part of UsermanagerService; r carries the requester's ActorID
	// and target GroupID, and the response wraps the underlying reject-invite
	// result after resolving the requester's email.
	RejectMyGroupInvitation(ctx context.Context, r *RejectMyGroupInvitationRequest) (*RejectMyGroupInvitationResponse, error)
	// GetGroupDetail returns a single group's details with enriched members and
	// owner, applying membership or admin access checks for non-admin requesters.
	// Part of UsermanagerService; r identifies the requester and GroupID, and the
	// response contains the group, members and owner.
	GetGroupDetail(ctx context.Context, r *GetGroupDetailRequest) (*GetGroupDetailResponse, error)
	// GetGroupStats returns aggregate statistics for a group, including seat usage,
	// role breakdown and settings, after access checks. Part of UsermanagerService;
	// r identifies the requester, GroupID and optional prefix, and the response
	// carries the computed GroupStats.
	GetGroupStats(ctx context.Context, r *GetGroupStatsRequest) (*GetGroupStatsResponse, error)
	// CreateGroup creates a new group via the group service after verifying the
	// requester is an admin or has admin access to a specified parent group. Part
	// of UsermanagerService; r carries the requester's ActorID and group fields,
	// and the response returns the created group.
	CreateGroup(ctx context.Context, r *CreateGroupRequest) (*CreateGroupResponse, error)
	// UpdateGroup updates an existing group, restricted to admins or requesters
	// with effective admin-level access to the target group. Part of
	// UsermanagerService; r carries the requester's ActorID and the group update
	// fields, and the response wraps the underlying update result.
	UpdateGroup(ctx context.Context, r *UpdateGroupRequest) (*UpdateGroupResponse, error)
	// DeleteGroup deletes a group, attributing deletion to the requester; non-admin
	// requesters must own the target group and are forced to hard delete. Part of
	// UsermanagerService; r identifies the requester and target group, and the
	// response wraps the underlying delete result.
	DeleteGroup(ctx context.Context, r *DeleteGroupRequest) (*DeleteGroupResponse, error)
	// GetGroupsByUserID returns the groups a user belongs to, applying filters;
	// non-admin requesters may only query their own groups. Part of
	// UsermanagerService; r carries the requester's ActorID, the target UserID and
	// filter fields, and the response wraps the group listing.
	GetGroupsByUserID(ctx context.Context, r *GetGroupsByUserIDRequest) (*GetGroupsByUserIDResponse, error)
	// GetGroupsConfig retrieves the group service's configuration capabilities.
	// Part of UsermanagerService; r is accepted but unused, and the response wraps
	// the configuration returned by the backing group service.
	GetGroupsConfig(ctx context.Context, r *GetGroupsConfigRequest) (*GetGroupsConfigResponse, error)
	// GetGroupLineage returns a group's lineage, gated by group access for
	// non-admin requesters and issued as the requester. Part of UsermanagerService;
	// r identifies the requester and group, and the response wraps the lineage
	// returned by the backing group service.
	GetGroupLineage(ctx context.Context, r *GetGroupLineageRequest) (*GetGroupLineageResponse, error)
	// GetGroupDescendants returns a group's descendants, gated by group access for
	// non-admin requesters and issued as the requester. Part of UsermanagerService;
	// r identifies the requester and ancestor group, and the response wraps the
	// descendant listing.
	GetGroupDescendants(ctx context.Context, r *GetGroupDescendantsRequest) (*GetGroupDescendantsResponse, error)
	// ValidateGroupName validates a proposed group name through the group service;
	// non-admin requesters need access to the supplied parent group when one is
	// given. Part of UsermanagerService; r carries the requester, name and optional
	// parent group, and the response wraps the validation result.
	ValidateGroupName(ctx context.Context, r *ValidateGroupNameRequest) (*ValidateGroupNameResponse, error)
	// Group management methods
	AddGroupMember(ctx context.Context, r *AddGroupMemberRequest) (*AddGroupMemberResponse, error)
	// RemoveGroupMember removes a user from a group after verifying the requester
	// is an admin or has admin access to the group. Part of UsermanagerService; r
	// identifies the requester, group and member, and the response reports whether
	// removal succeeded.
	RemoveGroupMember(ctx context.Context, r *RemoveGroupMemberRequest) (*RemoveGroupMemberResponse, error)
	// UpdateGroupMember updates a member's role in a group after verifying the
	// requester is an admin or has admin access to the group. Part of
	// UsermanagerService; r identifies the requester, group, member and new role,
	// and the response reports success.
	UpdateGroupMember(ctx context.Context, r *UpdateGroupMemberRequest) (*UpdateGroupMemberResponse, error)
	// UpdateGroupOwner transfers group ownership after verifying the requester is
	// an admin or has admin access, returning the enriched new owner. Part of
	// UsermanagerService; r identifies the requester, group and new OwnerID, and
	// the response contains the enriched owner.
	UpdateGroupOwner(ctx context.Context, r *UpdateGroupOwnerRequest) (*UpdateGroupOwnerResponse, error)
	// Reminder methods
	CreateReminder(ctx context.Context, r *CreateReminderRequest) (*CreateReminderResponse, error)
	// GetReminderByID returns a single reminder owned by the authenticated
	// requester; admins may retrieve reminders regardless of owner. Part of
	// UsermanagerService; r carries the requester's ActorID and reminder ID, and
	// the response wraps the reminder service result.
	GetReminderByID(ctx context.Context, r *GetReminderByIDRequest) (*GetReminderByIDResponse, error)
	// ListReminders returns reminders for the authenticated requester, or across
	// users when the requester is an admin with a user filter. Part of
	// UsermanagerService; r carries the requester, filters and pagination, and the
	// response wraps the reminder listing.
	ListReminders(ctx context.Context, r *ListRemindersRequest) (*ListRemindersResponse, error)
	// UpdateReminderByID updates a reminder owned by the authenticated requester
	// after validating the requester. Part of UsermanagerService; r carries the
	// requester's ActorID and reminder update fields, and the response wraps the
	// updated reminder.
	UpdateReminderByID(ctx context.Context, r *UpdateReminderByIDRequest) (*UpdateReminderByIDResponse, error)
	// DeleteReminderByID deletes a reminder owned by the authenticated requester.
	// Part of UsermanagerService; r carries the requester's ActorID and reminder
	// ID, and the error result reports deletion failure.
	DeleteReminderByID(ctx context.Context, r *DeleteReminderByIDRequest) error
	// DisableReminderByID disables a reminder owned by the authenticated requester.
	// Part of UsermanagerService; r carries the requester's ActorID and reminder
	// ID, and the response wraps the disabled reminder.
	DisableReminderByID(ctx context.Context, r *DisableReminderByIDRequest) (*UpdateReminderByIDResponse, error)
	// GetReminderStats returns aggregate reminder statistics for admin or service
	// views, scoped by the requester's administrative role and user filters. Part
	// of UsermanagerService; r carries the requester and optional user filters, and
	// the response wraps the computed statistics.
	GetReminderStats(ctx context.Context, r *GetReminderStatsRequest) (*GetReminderStatsResponse, error)
	// GetDueReminders returns reminders ready for scheduler dispatch, scoped by the
	// requester's role, user filters, due-before cutoff and limit. Part of
	// UsermanagerService; r carries those parameters, and the response wraps the
	// due reminder listing.
	GetDueReminders(ctx context.Context, r *GetDueRemindersRequest) (*GetDueRemindersResponse, error)
	// Streak methods
	RecordStreak(ctx context.Context, r *RecordStreakRequest) (*RecordStreakResponse, error)
	// ListStreaks returns streak entries for the authenticated requester,
	// defaulting to a daily period when unspecified. Part of UsermanagerService; r
	// carries the requester, optional user filter and streak filters, and the
	// response wraps the streak service listing.
	ListStreaks(ctx context.Context, r *ListStreaksRequest) (*ListStreaksResponse, error)
	// GetCurrentStreak returns the current streak count for the authenticated
	// requester, defaulting to a daily period. Part of UsermanagerService; r
	// carries the requester and count filters, and the response wraps the current
	// count result.
	GetCurrentStreak(ctx context.Context, r *GetCurrentStreakRequest) (*GetCurrentStreakResponse, error)
	// GetLongestStreak returns the personal best streak for the authenticated
	// requester, defaulting to a daily period. Part of UsermanagerService; r
	// carries the requester and streak filters, and the response wraps the longest
	// streak result.
	GetLongestStreak(ctx context.Context, r *GetLongestStreakRequest) (*GetLongestStreakResponse, error)
	// GetNumberOfStreaks returns how many streak entries match the filters for the
	// authenticated requester, defaulting to a daily period. Part of
	// UsermanagerService; r carries the requester and filters, and the response
	// wraps the matching streak count.
	GetNumberOfStreaks(ctx context.Context, r *GetNumberOfStreaksRequest) (*GetNumberOfStreaksResponse, error)
}

// UsermanagerValidator expected methods of a valid
type UsermanagerValidator interface {
	// Validate checks that the supplied value satisfies the validator's expected
	// validation rules, returning an error describing any failure. Contract of
	// UsermanagerValidator, which defines the request-validation interface the
	// handler layer relies on.
	Validate(s interface{}) error
}

// Handler manages usermanager requests
type Handler struct {
	Service                  UsermanagerService
	Validator                UsermanagerValidator
	ErrorMaps                []reply.ErrorManifest
	CookiePrefixAuthToken    string
	CookiePrefixRefreshToken string
	Environment              string
	CookieDomain             string
}

// NewHandlerRequest holds things needed for creating a handler
type NewHandlerRequest struct {
	Service                  UsermanagerService
	Validator                UsermanagerValidator
	ErrorMaps                []reply.ErrorManifest
	Environment              string
	CookiePrefixAuthToken    string
	CookiePrefixRefreshToken string
	CookieDomain             string
}

// NewHandler returns usermanager handler
func NewHandler(r *NewHandlerRequest) *Handler {

	return &Handler{
		Service:                  r.Service,
		Validator:                r.Validator,
		ErrorMaps:                r.ErrorMaps,
		CookiePrefixAuthToken:    r.CookiePrefixAuthToken,
		CookiePrefixRefreshToken: r.CookiePrefixRefreshToken,
		Environment:              r.Environment,
		CookieDomain:             r.CookieDomain,
	}
}

// GetAvailableCommsTypes handles public discovery of configured contact
// categories. It returns configuration only and does not expose comms records.
func (h *Handler) GetAvailableCommsTypes(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-available-comms-types")

	response, err := h.Service.GetAvailableCommsTypes(r.Context())
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.CommsTypes)
}

// GetGroupLineage handles the request to get a group's lineage
func (h *Handler) GetGroupLineage(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-group-lineage")
	request, err := MapRequestToGetGroupLineageRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupLineage(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Lineage)
}

// GetGroupsByUserID handles the request to get groups by user ID
// GetGroupDescendants handles the request to get group descendants
func (h *Handler) GetGroupDescendants(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-group-descendants")
	request, err := MapRequestToGetGroupDescendantsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupDescendants(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Descendants)
}

// GetGroupsByUserID handles the request to get groups by user ID.
func (h *Handler) GetGroupsByUserID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-groups-by-user-id")
	request, err := MapRequestToGetGroupsByUserIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsByUserID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetGroupsConfig handles the request to get the group service config
func (h *Handler) GetGroupsConfig(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-groups-config")
	request, err := MapRequestToGetGroupsConfigRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsConfig(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Config)
}

// DeleteUserPermanently deletes the authenticated caller's account through the
// manager service and clears authentication cookies on both success and failure.
func (h *Handler) DeleteUserPermanently(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-delete-user-permanently")
	request, err := MapRequestToDeleteUserPermanentlyRequest(r, h.Validator)
	if err != nil {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.DeleteUserPermanently(r.Context(), request)
	if err != nil {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.RemoveAuthCookies(w)
	h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
	h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// UpdateUserProfile returns response for request to update updatedable attributes
// of the user's profile
func (h *Handler) UpdateUserProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-update-user-profile")
	request, err := MapRequestToUpdateUserProfileRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if nilProfilePort(h.Service) {
		h.NewHTTPErrorResponse(w, userv2.ErrProfileUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	response, err := h.Service.UpdateUserProfile(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if response == nil || response.UpdateUserResponse == nil || response.User == nil {
		h.NewHTTPErrorResponse(w, userv2.ErrProfileUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User, reply.WithContext(r.Context()))
}

// GetUserMicroProfile returns response for request to get user's
// micro profile
func (h *Handler) GetUserMicroProfile(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-user-micro-profile")
	request, err := MapRequestToGetUserMicroProfileRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserMicroProfile(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.MicroProfile)
}

// GetUserByID returns response for request to get user by ID
func (h *Handler) GetUserByID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-user-by-id")
	request, err := MapRequestToGetUserByIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserByID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)

}

// GetUsers returns response for request to get users
func (h *Handler) GetUsers(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-users")
	request, err := MapRequestToGetUsersRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUsers(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if request.IncludeMeta {
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users, reply.WithMeta(response.Meta.GetMetaData()))
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users)
}

// GetUserProfile returns response for request to get user's
// profile
func (h *Handler) GetUserProfile(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-user-profile")
	request, err := MapRequestToGetUserProfileRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserProfile(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Profile)
}

// CreateComms handles the request to create a comms
func (h *Handler) CreateComms(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-create-comms")

	request, err := MapRequestToCreateCommsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	newCommsResponse, err := h.Service.CreateComms(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if newCommsResponse == nil || newCommsResponse.Comms == nil {
		h.NewHTTPErrorResponse(w, contacter.ErrCommsConversationUnavailable)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, newCommsResponse.Comms.CreationReceipt())
}

// GetComms handles the request to get a comms
func (h *Handler) GetComms(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-comms")

	request, err := mapGetCommsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	getCommsResponse, err := h.Service.GetComms(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, getCommsResponse.Comms, reply.WithMeta(getCommsResponse.Meta))
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, getCommsResponse.Comms)
}

// GetCommsStats handles the request to get comms stats
func (h *Handler) GetCommsStats(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-comms-stats")

	request, err := mapGetCommsStatsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	getCommsStatsResponse, err := h.Service.GetCommsStats(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, getCommsStatsResponse.Stats)
}

// UpdateComms handles the request to update a comms
func (h *Handler) UpdateComms(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-update-comms")

	request, err := MapRequestToUpdateCommsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	updateCommsResponse, err := h.Service.UpdateComms(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, updateCommsResponse.Comms)
}

// GetEnrichedUserProfile handles the request to get an enriched user profile with group memberships
func (h *Handler) GetEnrichedUserProfile(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-enriched-user-profile")
	request, err := MapRequestToGetEnrichedUserProfileRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetEnrichedUserProfile(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Profile)
}

// GetUserGroups handles the request to get a user's group memberships
func (h *Handler) GetUserGroups(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-user-groups")
	request, err := MapRequestToGetUserGroupsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserGroups(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups, reply.WithMeta(response.Meta))
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups)
}

// GetUserGroupMembershipsRequest handles the request to get a user's team memberships.
func (h *Handler) GetUserGroupMembershipsRequest(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-user-group-memberships-request")
	request, err := MapRequestToGetUserGroupMembershipsRequestRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserGroupMemberships(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetLatestNotificationOverviews handles the request to get latest notification overviews.
func (h *Handler) GetLatestNotificationOverviews(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-latest-notification-overviews")
	request, err := MapRequestToGetLatestNotificationOverviewsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetLatestNotificationOverviews(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if response == nil || response.GetLatestNotificationOverviewsResponse == nil {
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, []common.NotificationOverview{})
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Overviews)
}

// GetNotifierConfig handles the request to get notifier config.
func (h *Handler) GetNotifierConfig(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-notifier-config")
	request, err := MapRequestToGetNotifierConfigRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetNotifierConfig(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Config)
}

// RegisterNotificationAddress handles notification address registration.
func (h *Handler) RegisterNotificationAddress(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-register-notification-address")
	request, err := MapRequestToRegisterNotificationAddressRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RegisterNotificationAddress(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Address)
}

// ListNotificationAddresses handles notification address listing.
func (h *Handler) ListNotificationAddresses(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-list-notification-addresses")
	request, err := MapRequestToListNotificationAddressesRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ListNotificationAddresses(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	addresses := response.Addresses
	if addresses == nil && response.ListNotificationAddressesResponse != nil {
		addresses = make([]NotificationAddressWithUser, 0, len(response.ListNotificationAddressesResponse.Addresses))
		for _, address := range response.ListNotificationAddressesResponse.Addresses {
			addresses = append(addresses, NotificationAddressWithUser{NotificationAddressSummary: address})
		}
	}

	if request.ListNotificationAddressesRequest != nil && request.ListNotificationAddressesRequest.Meta {
		meta := response.Meta
		if meta == nil && response.ListNotificationAddressesResponse != nil {
			meta = response.ListNotificationAddressesResponse.GetMetaData()
		}
		if meta != nil {
			h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, addresses, reply.WithMeta(meta))
			return
		}
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, addresses)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, addresses)
}

// DeleteNotificationAddress handles notification address deletion.
func (h *Handler) DeleteNotificationAddress(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-delete-notification-address")
	request, err := MapRequestToDeleteNotificationAddressRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if err := h.Service.DeleteNotificationAddress(r.Context(), request); err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// GetNotificationPreferences handles notification preference lookup.
func (h *Handler) GetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-notification-preferences")
	request, err := MapRequestToGetNotificationPreferencesRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetNotificationPreferences(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	preferences := response.Preferences
	if preferences == nil && response.GetNotificationPreferencesResponse != nil {
		preferences = &NotificationPreferencesWithUser{NotificationPreferences: response.GetNotificationPreferencesResponse.Preferences}
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, preferences)
}

// UpdateNotificationPreferences handles notification preference updates.
func (h *Handler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-update-notification-preferences")
	request, err := MapRequestToUpdateNotificationPreferencesRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateNotificationPreferences(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	preferences := response.Preferences
	if preferences == nil && response.UpdateNotificationPreferencesResponse != nil {
		preferences = &NotificationPreferencesWithUser{NotificationPreferences: response.UpdateNotificationPreferencesResponse.Preferences}
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, preferences)
}

// NotifyUser handles admin/service notification sends.
func (h *Handler) NotifyUser(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-notify-user")
	request, err := MapRequestToNotifyUserRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.NotifyUser(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Results)
}

// NotifyUsers handles admin notification dispatches to multiple users.
func (h *Handler) NotifyUsers(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-notify-users")
	request, err := MapRequestToNotifyUsersRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.NotifyUsers(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Results)
}

// GetMyGroupInvitations handles the request to get the current user's outstanding group invitations.
func (h *Handler) GetMyGroupInvitations(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-my-group-invitations")
	request, err := MapRequestToGetMyGroupInvitationsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetMyGroupInvitations(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Invitations)
}

// AcceptMyGroupInvitation handles the request to accept one of the current user's group invitations.
func (h *Handler) AcceptMyGroupInvitation(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-accept-my-group-invitation")
	request, err := MapRequestToAcceptMyGroupInvitationRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.AcceptMyGroupInvitation(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.AcceptInviteResponse)
}

// RejectMyGroupInvitation handles the request to reject one of the current user's group invitations.
func (h *Handler) RejectMyGroupInvitation(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-reject-my-group-invitation")
	request, err := MapRequestToRejectMyGroupInvitationRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RejectMyGroupInvitation(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.RejectInviteResponse)
}

// GetGroupDetail handles the request to fetch a group's details for the requester
func (h *Handler) GetGroupDetail(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-group-detail")
	request, err := MapRequestToGetGroupDetailRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupDetail(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Detail)
}

// GetGroupStats handles the request to fetch a group's stats for the requester
func (h *Handler) GetGroupStats(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-group-stats")
	request, err := MapRequestToGetGroupStatsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupStats(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Stats)
}

// ValidateGroupName handles the request to validate a proposed group name
func (h *Handler) ValidateGroupName(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-validate-group-name")
	request, err := MapRequestToValidateGroupNameRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ValidateGroupName(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.ValidateGroupNameResponse)
}

// CreateGroup handles the request to create a new group
func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-create-group")
	request, err := MapRequestToCreateGroupRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateGroup(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Group)
}

// UpdateGroup handles the request to update an existing group
func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-update-group")
	request, err := MapRequestToUpdateGroupRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateGroup(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// DeleteGroup handles the request to delete a group
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-delete-group")
	request, err := MapRequestToDeleteGroupRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.DeleteGroup(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// AddGroupMember handles the request to add a member to a group
func (h *Handler) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-add-group-member")
	request, err := MapRequestToAddGroupMemberRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.AddGroupMember(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// RemoveGroupMember handles the request to remove a member from a group
func (h *Handler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-remove-group-member")
	request, err := MapRequestToRemoveGroupMemberRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RemoveGroupMember(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// UpdateGroupMember handles the request to update a member role in a group
func (h *Handler) UpdateGroupMember(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-update-group-member")
	request, err := MapRequestToUpdateGroupMemberRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateGroupMember(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// UpdateGroupOwner handles the request to update group ownership
func (h *Handler) UpdateGroupOwner(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-update-group-owner")
	request, err := MapRequestToUpdateGroupOwnerRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateGroupOwner(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// CreateReminder handles the request to create a reminder.
func (h *Handler) CreateReminder(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-create-reminder")
	request, err := MapRequestToCreateReminderRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateReminder(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Reminder)
}

// GetReminderByID handles the request to get a reminder by ID.
func (h *Handler) GetReminderByID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-reminder-by-id")
	request, err := MapRequestToGetReminderByIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetReminderByID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Reminder)
}

// ListReminders handles the request to list reminders.
func (h *Handler) ListReminders(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-list-reminders")
	request, err := MapRequestToListRemindersRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ListReminders(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Reminders)
}

// UpdateReminderByID handles the request to update a reminder by ID.
func (h *Handler) UpdateReminderByID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-update-reminder-by-id")
	request, err := MapRequestToUpdateReminderByIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateReminderByID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Reminder)
}

// DeleteReminderByID handles the request to delete a reminder by ID.
func (h *Handler) DeleteReminderByID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-delete-reminder-by-id")
	request, err := MapRequestToDeleteReminderByIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if err := h.Service.DeleteReminderByID(r.Context(), request); err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// DisableReminderByID handles the request to disable a reminder by ID.
func (h *Handler) DisableReminderByID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-disable-reminder-by-id")
	request, err := MapRequestToDisableReminderByIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.DisableReminderByID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Reminder)
}

// GetReminderStats handles the request to get reminder statistics.
func (h *Handler) GetReminderStats(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-reminder-stats")
	request, err := MapRequestToGetReminderStatsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetReminderStats(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Stats)
}

// GetDueReminders handles the request to get due reminders.
func (h *Handler) GetDueReminders(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-due-reminders")
	request, err := MapRequestToGetDueRemindersRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetDueReminders(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Reminders)
}

// RecordStreak handles the request to record a streak.
func (h *Handler) RecordStreak(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-record-streak")
	request, err := MapRequestToRecordStreakRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RecordStreak(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Streak)
}

// ListStreaks handles the request to list streaks.
func (h *Handler) ListStreaks(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-list-streaks")
	request, err := MapRequestToListStreaksRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ListStreaks(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Streaks)
}

// GetCurrentStreak handles the request to get the current streak count.
func (h *Handler) GetCurrentStreak(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-current-streak")
	request, err := MapRequestToGetCurrentStreakRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetCurrentStreak(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.GetCurrentCountResponse)
}

// GetLongestStreak handles the request to get the longest streak count.
func (h *Handler) GetLongestStreak(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-longest-streak")
	request, err := MapRequestToGetLongestStreakRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetLongestStreak(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.GetLongestStreakResponse)
}

// GetNumberOfStreaks handles the request to count streak entries.
func (h *Handler) GetNumberOfStreaks(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/usermanager", "handle-get-number-of-streaks")
	request, err := MapRequestToGetNumberOfStreaksRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetNumberOfStreaks(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.GetNumberOfStreaksResponse)
}

// GetBaseResponseHandler composes manager and dependency maps, then host overrides.
func (h *Handler) GetBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.responseManifests())
}

// responseManifests keeps success factories and error writers on the same
// domain base and last-wins caller override layers.
func (h *Handler) responseManifests() []reply.ErrorManifest {
	return errormanifest.NewComposer().Add(UsermanagerErrorMap).Add(DependencyErrorMaps()...).AddOverrides(h.ErrorMaps...).Build()
}

// NewHTTPErrorResponse preserves mapped wrappers and validation collections.
// It returns writer failures and never passes raw diagnostic causes to reply.
func (h *Handler) NewHTTPErrorResponse(w http.ResponseWriter, err error, attributes ...reply.ResponseAttributes) error {
	return errormanifest.WriteHTTPError(w, err, h.responseManifests(), attributes...)
}

// RemoveAuthCookies is handling removing the cookies from the client
// cookie store regardless of what happens on the platform
func (h *Handler) RemoveAuthCookies(w http.ResponseWriter) {

	toolbox.RemoveAuthCookies(w, h.Environment, h.CookieDomain, h.CookiePrefixAuthToken, h.CookiePrefixRefreshToken)
}

// RemoveCookiesWithName is handling removing the cookies from the client
// cookie store regardless of what happens on the platform
func (h *Handler) RemoveCookiesWithName(w http.ResponseWriter, cookieName string) {

	toolbox.RemoveCookiesWithName(w, h.Environment, cookieName, h.CookieDomain)
}
