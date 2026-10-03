package usermanager

import (
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/reminder"
	"github.com/ooaklee/ghatd/external/streaker"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// GetGroupsByUserIDRequest separates the caller from the user whose groups are
// requested. The service requires administrative authority for a different user.
type GetGroupsByUserIDRequest struct {

	// ActorID identifies the verified caller, not the embedded target UserID.
	ActorID string `json:"-"`

	// GetGroupsByUserIDRequest carries the underlying group lookup parameters.
	*group.GetGroupsByUserIDRequest
}

// GetUserGroupMembershipsRequest retrieves the caller's own memberships. Use
// GetGroupsByUserIDRequest for an authorized cross-user lookup instead.
type GetUserGroupMembershipsRequest struct {

	// ActorID identifies the verified caller whose memberships are requested.
	ActorID string `json:"-"`

	// GroupType if specified, filters groups to a specific type (e.g. "team", "department")
	GroupType string `query:"group_type"`

	// IncludeDescendants if true, includes descendant groups per root group that the user can access
	IncludeDescendants bool `query:"include_descendants"`

	// PrefixName if true, prefixes child group names with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetUserMicroProfileRequest holds all the data needed to action request
type GetUserMicroProfileRequest struct {

	// ActorID identifies the verified caller requesting their micro profile.
	ActorID string `json:"-"`
}

// GetUserProfileRequest holds all the data needed to action user
// profile retrieval request
type GetUserProfileRequest struct {

	// ActorID identifies the verified caller requesting their own profile.
	ActorID string `json:"-"`

	// GetUserProfileRequest carries the underlying profile lookup parameters.
	*userv2.GetUserProfileRequest
}

// GetUserByIDRequest holds all the data needed to action user retrieval request
type GetUserByIDRequest struct {

	// ActorID identifies the verified caller, independently of the target ID.
	ActorID string `json:"-"`

	// GetUserByIDRequest carries the underlying target-user lookup parameters.
	*userv2.GetUserByIDRequest
}

// GetUsersRequest holds all the data needed to action user list retrieval request
type GetUsersRequest struct {

	// ActorID is the ID of the user making the request
	ActorID string `json:"-"`

	// GroupID if specified, filters users to those that are
	// members of the root group of the specified group id
	GroupID string `query:"group_id"`

	// GetUsersRequest carries the underlying user search and pagination parameters.
	*userv2.GetUsersRequest
}

// UpdateUserProfileRequest holds all the data needed to action user
// profile update request
type UpdateUserProfileRequest struct {

	// ActorID identifies the verified caller whose profile is being updated.
	ActorID string `json:"-"`

	// UpdateUserRequest carries the underlying user profile updates.
	*userv2.UpdateUserRequest
}

// DeleteUserPermanentlyRequest holds all the data needed to delete user and resources
type DeleteUserPermanentlyRequest struct {

	// ActorID identifies the trusted actor; callers must not decode it from input.
	ActorID string `json:"-"`

	// ID selects the account to delete. The HTTP /me mapper always binds it to
	// ActorID. Trusted in-process admin workflows may select a different account;
	// the service checks that actor's administrative authority.
	ID string `path:"userID"`

	// Reason optionally captures why account deletion was requested.
	Reason string `json:"reason,omitempty"`
}

// GetUserInsightsUsageRequest holds all the data needed to get basic user insights
type GetUserInsightsUsageRequest struct {
	// ActorID identifies the verified caller requesting their basic insights.
	ActorID string `json:"-"`

	// From the date from when the queries should be run between
	From string `query:"from"`

	// To the date up to when the queries should be run between
	To string `query:"to"`
}

// CreateCommsRequest holds everything needed to make
// the request to create a comms
type CreateCommsRequest struct {
	// CreateCommsRequest carries the underlying comms creation payload.
	*contacter.CreateCommsRequest
}

// GetCommsRequest holds everything needed to make
// the request to get a comms
type GetCommsRequest struct {

	// ActorID is the id of the user making the request
	ActorID string `json:"-"`

	// GetCommsRequest carries the underlying comms query parameters.
	*contacter.GetCommsRequest
}

// UpdateCommsRequest holds everything needed to make
// the request to update a comms
type UpdateCommsRequest struct {

	// ActorID is the verified actor supplied by the HTTP mapper. Admin authority
	// is enforced by route middleware, not by the contact persistence service.
	ActorID string `json:"-"`

	// UpdateCommsRequest carries the underlying comms update payload.
	*contacter.UpdateCommsRequest
}

// GetCommsStatsRequest holds everything needed to make
// the request to get comms stats
type GetCommsStatsRequest struct {

	// ActorID is the id of the user making the request
	ActorID string `json:"-"`

	// GetCommsStatsRequest carries the underlying comms stats query parameters.
	*contacter.GetCommsStatsRequest
}

// GetEnrichedUserProfileRequest holds the data needed to get an enriched user profile
type GetEnrichedUserProfileRequest struct {

	// ActorID is the ID of the user requesting their enriched profile
	ActorID string `json:"-"`

	// IncludeAllGroups indicates whether to include all group memberships
	IncludeAllGroups bool `query:"include_all_groups"`

	// PrefixName if true, and IncludeAllGroups is true, prefixes child group names with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetUserGroupsRequest holds the data needed to get groups for a user
type GetUserGroupsRequest struct {

	// ActorID identifies the verified caller whose own groups are requested.
	ActorID string `json:"-"`

	// GroupType filters by group type (optional: TEAM, DEPARTMENT, etc.)
	GroupType string `query:"types"`

	// Status filters by group status (optional: ACTIVE, INACTIVE, etc.)
	Status string `query:"status"`

	// Page specifies the page results should be taken from. Default 1.
	Page int `query:"page"`

	// PerPage specifies the number of groups to return per page. Default 25. Max 100.
	PerPage int `query:"per_page" validate:"max=100"`

	// Meta indicates whether response should contain meta information
	Meta bool `query:"meta"`

	// PrefixName if true, prefixes child group names with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetLatestNotificationOverviewsRequest holds the data needed to fetch latest notification overviews.
type GetLatestNotificationOverviewsRequest struct {
	// ActorID is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID for authorisation,
	// logging, and audit context. The target user being queried lives in the
	// embedded GetLatestNotificationOverviewsRequest.UserID field instead.
	ActorID string `json:"-"`

	// GetLatestNotificationOverviewsRequest carries the underlying notification query parameters.
	*common.GetLatestNotificationOverviewsRequest
}

// GetNotifierConfigRequest holds the data needed to fetch notifier config.
type GetNotifierConfigRequest struct {
	// ActorID is the authenticated requester asking for notifier config.
	ActorID string `json:"-"`

	// GetNotifierConfigRequest carries the underlying notifier config request.
	*notifier.GetNotifierConfigRequest
}

// RegisterNotificationAddressRequest holds the data needed to register a notification address for the current user.
type RegisterNotificationAddressRequest struct {
	// ActorID is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID. The target user whose
	// address is being registered lives in RegisterAddressRequest.UserID.
	ActorID string `json:"-"`

	// RegisterAddressRequest carries the underlying notification address payload.
	*notifier.RegisterAddressRequest
}

// ListNotificationAddressesRequest holds the data needed to list notification addresses for the current user.
type ListNotificationAddressesRequest struct {
	// ActorID is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID. The target filter lives
	// in ListNotificationAddressesRequest.UserID on the embedded notifier request.
	ActorID string `json:"-"`

	// AdminView indicates whether the request came through the admin notifications route.
	AdminView bool

	// IncludeUsers indicates whether address summaries should be enriched with user profiles.
	IncludeUsers bool `query:"include_users"`

	// ListNotificationAddressesRequest carries the underlying notifier list filters and pagination.
	*notifier.ListNotificationAddressesRequest
}

// DeleteNotificationAddressRequest holds the data needed to delete a notification address for the current user.
type DeleteNotificationAddressRequest struct {
	// ActorID is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID. The target user whose
	// address is being deleted lives in DeleteNotificationAddressRequest.UserID.
	ActorID string `json:"-"`

	// DeleteNotificationAddressRequest carries the target user and address identifiers.
	*notifier.DeleteNotificationAddressRequest
}

// GetNotificationPreferencesRequest holds the data needed to fetch notification preferences.
type GetNotificationPreferencesRequest struct {
	// ActorID is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID. The target user whose
	// preferences are being fetched lives in GetNotificationPreferencesRequest.UserID.
	ActorID string `json:"-"`

	// IncludeUser indicates whether the response should include the target user's profile.
	IncludeUser bool

	// GetNotificationPreferencesRequest carries the target notification preference lookup.
	*notifier.GetNotificationPreferencesRequest
}

// UpdateNotificationPreferencesRequest holds the data needed to update notification preferences.
type UpdateNotificationPreferencesRequest struct {
	// ActorID is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID. The target user whose
	// preferences are being updated lives in UpdateNotificationPreferencesRequest.UserID.
	ActorID string `json:"-"`

	// IncludeUser indicates whether the response should include the target user's profile.
	IncludeUser bool

	// UpdateNotificationPreferencesRequest carries the underlying preference update payload.
	*notifier.UpdateNotificationPreferencesRequest
}

// NotifyUserRequest holds the data needed for an admin/service notification send.
type NotifyUserRequest struct {
	// ActorID is the authenticated requester/actor ID.
	//
	// The target notification recipient lives in NotifyUserRequest.UserID on the
	// embedded notifier request.
	ActorID string `json:"-"`

	// NotifyUserRequest carries the target recipient and notification payload.
	*notifier.NotifyUserRequest
}

// NotifyUsersRequest holds the data needed for an admin notification dispatch
// to zero or more users across zero or more channels.
type NotifyUsersRequest struct {
	// ActorID is the authenticated requester/actor ID.
	ActorID string `json:"-"`

	// NotifyUsersRequest carries the target users, notification payload, and
	// optional channel filters.
	*notifier.NotifyUsersRequest
}

// GetMyGroupInvitationsRequest holds the data needed to fetch the current user's group invitations.
type GetMyGroupInvitationsRequest struct {
	// ActorID is the ID of the requester.
	ActorID string `json:"-"`

	// PrefixName if true, prefixes child group names with the root group's name.
	PrefixName bool `query:"prefix_name"`
}

// AcceptMyGroupInvitationRequest holds the data needed to accept one of the current user's group invitations.
type AcceptMyGroupInvitationRequest struct {
	// ActorID is the ID of the requester.
	ActorID string `json:"-"`
	// GroupID is the ID of the group invitation to accept.
	GroupID string `path:"groupID"`
}

// RejectMyGroupInvitationRequest holds the data needed to reject one of the current user's group invitations.
type RejectMyGroupInvitationRequest struct {
	// ActorID is the ID of the requester.
	ActorID string `json:"-"`
	// GroupID is the ID of the group invitation to reject.
	GroupID string `path:"groupID"`
}

// UpdateGroupRequest holds the data needed to update a group
type UpdateGroupRequest struct {

	// ActorID is the trusted actor used for group authorization, not a body field.
	ActorID string `json:"-"`

	// UpdateGroupRequest carries the underlying update. HTTP accepts editable
	// fields only and binds ID from the URL; Group is reserved for trusted code.
	*group.UpdateGroupRequest
}

// ValidateGroupNameRequest holds the data needed to validate a group name
type ValidateGroupNameRequest struct {

	// ActorID is the ID of the user making the request
	ActorID string `json:"-"`

	// ValidateGroupNameRequest carries the underlying group-name validation parameters.
	*group.ValidateGroupNameRequest
}

// CreateGroupRequest holds the data needed for a user to create a new group
type CreateGroupRequest struct {

	// ActorID is the trusted actor used for authorization and default ownership.
	// The HTTP mapper binds it independently of the group creation payload.
	ActorID string `json:"-"`

	// CreateGroupRequest carries the underlying group creation payload.
	*group.CreateGroupRequest
}

// DeleteGroupRequest holds the data needed for a user to delete a group
type DeleteGroupRequest struct {

	// ActorID is the ID of the user making the request
	ActorID string `json:"-"`

	// DeleteGroupRequest carries the underlying group deletion parameters.
	*group.DeleteGroupRequest
}

// GetGroupDetailRequest holds the data needed to fetch a specific group's detail
type GetGroupDetailRequest struct {

	// ActorID is the ID of the requester
	ActorID string `json:"-"`

	// GroupID is the ID of the group to fetch
	GroupID string

	// PrefixName if true, prefixes child group name with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetGroupStatsRequest holds the data needed to fetch stats for a specific group
type GetGroupStatsRequest struct {

	// ActorID is the ID of the requester
	ActorID string `json:"-"`

	// GroupID is the ID of the group to fetch
	GroupID string

	// PrefixName if true, prefixes child group name with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetGroupsConfigRequest holds the data needed to retrieve the group service config
type GetGroupsConfigRequest struct {

	// ActorID is the ID of the requester
	ActorID string `json:"-"`
}

// GetGroupLineageRequest holds the data needed to fetch a group's lineage
type GetGroupLineageRequest struct {

	// ActorID is the ID of the requester
	ActorID string `json:"-"`

	// GetGroupLineageRequest carries the underlying lineage query parameters.
	*group.GetGroupLineageRequest
}

// GetGroupDescendantsRequest holds the data needed to fetch a group's descendants
type GetGroupDescendantsRequest struct {

	// ActorID is the ID of the requester
	ActorID string `json:"-"`

	// GetGroupDescendantsRequest carries the underlying descendants query parameters.
	*group.GetGroupDescendantsRequest
}

// AddGroupMemberRequest holds the data needed to add a user to a group
type AddGroupMemberRequest struct {

	// ActorID is the trusted actor, distinct from the member selected in the body.
	ActorID string `json:"-"`

	// AddMemberRequest carries the underlying member-addition payload.
	*group.AddMemberRequest
}

// RemoveGroupMemberRequest holds the data needed to remove a user from a group
type RemoveGroupMemberRequest struct {

	// ActorID is the ID of the user making the request
	ActorID string `json:"-"`

	// RemoveMemberRequest carries the underlying member-removal parameters.
	*group.RemoveMemberRequest
}

// UpdateGroupMemberRequest holds the data needed to update a user's role in a group
type UpdateGroupMemberRequest struct {

	// ActorID is the trusted actor, distinct from the URL-selected member.
	ActorID string `json:"-"`

	// UpdateMemberRoleRequest carries the underlying member-role update payload.
	*group.UpdateMemberRoleRequest
}

// CreateReminderRequest holds the data needed to create a reminder for the current user.
type CreateReminderRequest struct {
	// ActorID is the authenticated requester creating the reminder.
	ActorID string `json:"-"`

	// CreateReminderRequest carries the underlying reminder creation payload.
	*reminder.CreateReminderRequest
}

// GetReminderByIDRequest holds the data needed to get a reminder.
type GetReminderByIDRequest struct {
	// ActorID is the authenticated requester fetching the reminder.
	ActorID string `json:"-"`

	// Id is the reminder identifier to fetch.
	Id string
}

// ListRemindersRequest holds the data needed to list reminders.
type ListRemindersRequest struct {
	// ActorID is the authenticated requester listing reminders.
	ActorID string `json:"-"`

	// FilterUserID optionally filters reminders by a single target user.
	FilterUserID string `query:"user_id"`

	// Status optionally filters reminders by reminder status.
	Status string `query:"status"`

	// TargetType optionally filters reminders by the type of target resource.
	TargetType string `query:"target_type"`

	// TargetId optionally filters reminders by the target resource identifier.
	TargetId string `query:"target_id"`

	// Page specifies the page of reminder results to return.
	Page int `query:"page"`

	// PerPage specifies the number of reminder results to return per page.
	PerPage int `query:"per_page"`
}

// UpdateReminderByIDRequest holds the data needed to update a reminder.
type UpdateReminderByIDRequest struct {
	// ActorID is the authenticated requester updating the reminder.
	ActorID string `json:"-"`

	// Id is the reminder identifier to update.
	Id string

	// UpdateReminderByIDRequest carries the underlying reminder update payload.
	*reminder.UpdateReminderByIDRequest
}

// DeleteReminderByIDRequest holds the data needed to delete a reminder.
type DeleteReminderByIDRequest struct {
	// ActorID is the authenticated requester deleting the reminder.
	ActorID string `json:"-"`

	// Id is the reminder identifier to delete.
	Id string
}

// DisableReminderByIDRequest holds the data needed to disable a reminder.
type DisableReminderByIDRequest struct {
	// ActorID is the authenticated requester disabling the reminder.
	ActorID string `json:"-"`

	// Id is the reminder identifier to disable.
	Id string
}

// GetDueRemindersRequest holds the data needed to get due reminders.
type GetDueRemindersRequest struct {
	// ActorID is the authenticated requester fetching due reminders.
	ActorID string `json:"-"`

	// FilterUserID optionally filters due reminders by a single target user.
	FilterUserID string `query:"user_id"`

	// FilterUserIDs optionally filters due reminders by multiple target users.
	FilterUserIDs []string `query:"user_ids"`

	// DueBefore optionally filters reminders due before the provided timestamp.
	DueBefore string `query:"due_before"`

	// Limit optionally caps the number of due reminders returned.
	Limit int `query:"limit"`
}

// GetReminderStatsRequest holds the data needed to get reminder stats.
type GetReminderStatsRequest struct {
	// ActorID is the authenticated requester fetching reminder stats.
	ActorID string `json:"-"`

	// FilterUserID optionally filters reminder stats by a single target user.
	FilterUserID string `query:"user_id"`

	// FilterUserIDs optionally filters reminder stats by multiple target users.
	FilterUserIDs []string `query:"user_ids"`
}

// RecordStreakRequest holds the data needed to record a streak for the current user.
type RecordStreakRequest struct {
	// ActorID is the authenticated requester recording the streak.
	ActorID string `json:"-"`

	// RecordStreakRequest carries the underlying streak creation payload.
	*streaker.RecordStreakRequest
}

// ListStreaksRequest holds the data needed to list streak history.
type ListStreaksRequest struct {
	// ActorID is the authenticated requester listing streaks.
	ActorID string `json:"-"`

	// FilterUserID optionally filters streaks by a target user on admin/service routes.
	FilterUserID string `query:"user_id"`

	// ListStreaksRequest carries the underlying streak history filters.
	*streaker.ListStreaksRequest
}

// GetCurrentStreakRequest holds filters used to get the current streak count.
type GetCurrentStreakRequest struct {
	// ActorID is the authenticated requester getting streak stats.
	ActorID string `json:"-"`

	// FilterUserID optionally filters streaks by a target user on admin/service routes.
	FilterUserID string `query:"user_id"`

	// GetCurrentCountRequest carries the underlying streak stats filters.
	*streaker.GetCurrentCountRequest
}

// GetLongestStreakRequest holds filters used to get the personal best streak.
type GetLongestStreakRequest struct {
	// ActorID is the authenticated requester getting streak stats.
	ActorID string `json:"-"`

	// FilterUserID optionally filters streaks by a target user on admin/service routes.
	FilterUserID string `query:"user_id"`

	// GetLongestStreakRequest carries the underlying streak stats filters.
	*streaker.GetLongestStreakRequest
}

// GetNumberOfStreaksRequest holds filters used to count streak entries.
type GetNumberOfStreaksRequest struct {
	// ActorID is the authenticated requester getting streak stats.
	ActorID string `json:"-"`

	// FilterUserID optionally filters streaks by a target user on admin/service routes.
	FilterUserID string `query:"user_id"`

	// GetNumberOfStreaksRequest carries the underlying streak stats filters.
	*streaker.GetNumberOfStreaksRequest
}

// UpdateGroupOwnerRequest holds the data needed to update group ownership
type UpdateGroupOwnerRequest struct {

	// ActorID is the trusted actor, distinct from the proposed new owner.
	ActorID string `json:"-"`

	// UpdateOwnerRequest carries the underlying group-owner update payload.
	*group.UpdateOwnerRequest
}
