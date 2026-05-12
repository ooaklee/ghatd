package usermanager

import (
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/reminder"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// GetUserGroups handles fetching groups for a user with filtering
type GetGroupsByUserIDRequest struct {

	// ID is the ID of the making the request
	ID string

	// GetGroupsByUserIDRequest carries the underlying group lookup parameters.
	*group.GetGroupsByUserIDRequest
}

// GetUserGroupMembershipsRequest represents the request for fetching user group memberships with filtering options
type GetUserGroupMembershipsRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	// GroupType if specified, filters groups to a specific type (e.g. "team", "department")
	GroupType string `query:"group_type"`

	// IncludeDescendants if true, includes descendant groups per root group that the user can access
	IncludeDescendants bool `query:"include_descendants"`

	// PrefixName if true, prefixes child group names with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetUserMicroProfileRequest holds all the data needed to action request
type GetUserMicroProfileRequest struct {

	// UserId the ID of the user requesting their micro profile
	UserId string
}

// GetUserProfileRequest holds all the data needed to action user
// profile retrieval request
type GetUserProfileRequest struct {

	// UserId the ID of the user requesting their profile
	UserId string

	// GetUserProfileRequest carries the underlying profile lookup parameters.
	*userv2.GetUserProfileRequest
}

// GetUserByIDRequest holds all the data needed to action user retrieval request
type GetUserByIDRequest struct {

	// UserId the ID of the user making the request
	UserId string

	// GetUserByIDRequest carries the underlying target-user lookup parameters.
	*userv2.GetUserByIDRequest
}

// GetUsersRequest holds all the data needed to action user list retrieval request
type GetUsersRequest struct {

	// UserId is the ID of the user making the request
	UserId string

	// GroupID if specified, filters users to those that are
	// members of the root group of the specified group id
	GroupID string `query:"group_id"`

	// GetUsersRequest carries the underlying user search and pagination parameters.
	*userv2.GetUsersRequest
}

// UpdateUserProfileRequest holds all the data needed to action user
// profile update request
type UpdateUserProfileRequest struct {

	// UserId the ID of the user requesting their profile
	UserId string

	// UpdateUserRequest carries the underlying user profile updates.
	*userv2.UpdateUserRequest
}

// DeleteUserPermanentlyRequest holds all the data needed to delete user and resources
type DeleteUserPermanentlyRequest struct {

	// UserId the Id of the user requesting the deletion
	UserId string

	// ID is the ID of the user to be deleted
	ID string `path:"userID"`

	// Reason optionally captures why account deletion was requested.
	Reason string `json:"reason,omitempty"`
}

// GetUserInsightsUsageRequest holds all the data needed to get basic user insights
type GetUserInsightsUsageRequest struct {
	// UserId the ID of the user requesting basic insights
	UserId string

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

	// UserId is the id of the user making the request
	UserId string

	// GetCommsRequest carries the underlying comms query parameters.
	*contacter.GetCommsRequest
}

// UpdateCommsRequest holds everything needed to make
// the request to update a comms
type UpdateCommsRequest struct {

	// UserId is the id of the user making the request
	UserId string

	// UpdateCommsRequest carries the underlying comms update payload.
	*contacter.UpdateCommsRequest
}

// GetCommsStatsRequest holds everything needed to make
// the request to get comms stats
type GetCommsStatsRequest struct {

	// UserId is the id of the user making the request
	UserId string

	// GetCommsStatsRequest carries the underlying comms stats query parameters.
	*contacter.GetCommsStatsRequest
}

// GetEnrichedUserProfileRequest holds the data needed to get an enriched user profile
type GetEnrichedUserProfileRequest struct {

	// UserId is the ID of the user requesting their enriched profile
	UserId string

	// IncludeAllGroups indicates whether to include all group memberships
	IncludeAllGroups bool `query:"include_all_groups"`

	// PrefixName if true, and IncludeAllGroups is true, prefixes child group names with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetUserGroupsRequest holds the data needed to get groups for a user
type GetUserGroupsRequest struct {

	// UserId is the ID of the user
	UserId string

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
	// UserId is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID for authorisation,
	// logging, and audit context. The target user being queried lives in the
	// embedded GetLatestNotificationOverviewsRequest.UserID field instead.
	UserId string

	// GetLatestNotificationOverviewsRequest carries the underlying notification query parameters.
	*common.GetLatestNotificationOverviewsRequest
}

// GetNotifierConfigRequest holds the data needed to fetch notifier config.
type GetNotifierConfigRequest struct {
	// UserId is the authenticated requester asking for notifier config.
	UserId string

	// GetNotifierConfigRequest carries the underlying notifier config request.
	*notifier.GetNotifierConfigRequest
}

// RegisterNotificationAddressRequest holds the data needed to register a notification address for the current user.
type RegisterNotificationAddressRequest struct {
	// UserId is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID. The target user whose
	// address is being registered lives in RegisterAddressRequest.UserID.
	UserId string

	// RegisterAddressRequest carries the underlying notification address payload.
	*notifier.RegisterAddressRequest
}

// ListNotificationAddressesRequest holds the data needed to list notification addresses for the current user.
type ListNotificationAddressesRequest struct {
	// UserId is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID. The target filter lives
	// in ListNotificationAddressesRequest.UserID on the embedded notifier request.
	UserId string

	// AdminView indicates whether the request came through the admin notifications route.
	AdminView bool

	// IncludeUsers indicates whether address summaries should be enriched with user profiles.
	IncludeUsers bool `query:"include_users"`

	// ListNotificationAddressesRequest carries the underlying notifier list filters and pagination.
	*notifier.ListNotificationAddressesRequest
}

// DeleteNotificationAddressRequest holds the data needed to delete a notification address for the current user.
type DeleteNotificationAddressRequest struct {
	// UserId is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID. The target user whose
	// address is being deleted lives in DeleteNotificationAddressRequest.UserID.
	UserId string

	// DeleteNotificationAddressRequest carries the target user and address identifiers.
	*notifier.DeleteNotificationAddressRequest
}

// GetNotificationPreferencesRequest holds the data needed to fetch notification preferences.
type GetNotificationPreferencesRequest struct {
	// UserId is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID. The target user whose
	// preferences are being fetched lives in GetNotificationPreferencesRequest.UserID.
	UserId string

	// IncludeUser indicates whether the response should include the target user's profile.
	IncludeUser bool

	// GetNotificationPreferencesRequest carries the target notification preference lookup.
	*notifier.GetNotificationPreferencesRequest
}

// UpdateNotificationPreferencesRequest holds the data needed to update notification preferences.
type UpdateNotificationPreferencesRequest struct {
	// UserId is the authenticated requester/actor ID.
	//
	// On admin routes this remains the admin user's ID. The target user whose
	// preferences are being updated lives in UpdateNotificationPreferencesRequest.UserID.
	UserId string

	// IncludeUser indicates whether the response should include the target user's profile.
	IncludeUser bool

	// UpdateNotificationPreferencesRequest carries the underlying preference update payload.
	*notifier.UpdateNotificationPreferencesRequest
}

// NotifyUserRequest holds the data needed for an admin/service notification send.
type NotifyUserRequest struct {
	// UserId is the authenticated requester/actor ID.
	//
	// The target notification recipient lives in NotifyUserRequest.UserID on the
	// embedded notifier request.
	UserId string

	// NotifyUserRequest carries the target recipient and notification payload.
	*notifier.NotifyUserRequest
}

// NotifyUsersRequest holds the data needed for an admin notification dispatch
// to zero or more users across zero or more channels.
type NotifyUsersRequest struct {
	// UserId is the authenticated requester/actor ID.
	UserId string

	// NotifyUsersRequest carries the target users, notification payload, and
	// optional channel filters.
	*notifier.NotifyUsersRequest
}

// GetMyGroupInvitationsRequest holds the data needed to fetch the current user's group invitations.
type GetMyGroupInvitationsRequest struct {
	// UserId is the ID of the requester.
	UserId string

	// PrefixName if true, prefixes child group names with the root group's name.
	PrefixName bool `query:"prefix_name"`
}

// AcceptMyGroupInvitationRequest holds the data needed to accept one of the current user's group invitations.
type AcceptMyGroupInvitationRequest struct {
	// UserId is the ID of the requester.
	UserId string
	// GroupID is the ID of the group invitation to accept.
	GroupID string `path:"groupID"`
}

// RejectMyGroupInvitationRequest holds the data needed to reject one of the current user's group invitations.
type RejectMyGroupInvitationRequest struct {
	// UserId is the ID of the requester.
	UserId string
	// GroupID is the ID of the group invitation to reject.
	GroupID string `path:"groupID"`
}

// UpdateGroupRequest holds the data needed to update a group
type UpdateGroupRequest struct {

	// UserId is the ID of the user making the request
	UserId string

	// UpdateGroupRequest carries the underlying group update payload.
	*group.UpdateGroupRequest
}

// ValidateGroupNameRequest holds the data needed to validate a group name
type ValidateGroupNameRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	// ValidateGroupNameRequest carries the underlying group-name validation parameters.
	*group.ValidateGroupNameRequest
}

// CreateGroupRequest holds the data needed for a user to create a new group
type CreateGroupRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	// CreateGroupRequest carries the underlying group creation payload.
	*group.CreateGroupRequest
}

// DeleteGroupRequest holds the data needed for a user to delete a group
type DeleteGroupRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	// DeleteGroupRequest carries the underlying group deletion parameters.
	*group.DeleteGroupRequest
}

// GetGroupDetailRequest holds the data needed to fetch a specific group's detail
type GetGroupDetailRequest struct {

	// UserId is the ID of the requester
	UserId string

	// GroupID is the ID of the group to fetch
	GroupID string

	// PrefixName if true, prefixes child group name with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetGroupStatsRequest holds the data needed to fetch stats for a specific group
type GetGroupStatsRequest struct {

	// UserId is the ID of the requester
	UserId string

	// GroupID is the ID of the group to fetch
	GroupID string

	// PrefixName if true, prefixes child group name with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetGroupsConfigRequest holds the data needed to retrieve the group service config
type GetGroupsConfigRequest struct {

	// UserId is the ID of the requester
	UserId string
}

// GetGroupLineageRequest holds the data needed to fetch a group's lineage
type GetGroupLineageRequest struct {

	// UserId is the ID of the requester
	UserId string

	// GetGroupLineageRequest carries the underlying lineage query parameters.
	*group.GetGroupLineageRequest
}

// GetGroupDescendantsRequest holds the data needed to fetch a group's descendants
type GetGroupDescendantsRequest struct {

	// UserId is the ID of the requester
	UserId string

	// GetGroupDescendantsRequest carries the underlying descendants query parameters.
	*group.GetGroupDescendantsRequest
}

// AddGroupMemberRequest holds the data needed to add a user to a group
type AddGroupMemberRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	// AddMemberRequest carries the underlying member-addition payload.
	*group.AddMemberRequest
}

// RemoveGroupMemberRequest holds the data needed to remove a user from a group
type RemoveGroupMemberRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	// RemoveMemberRequest carries the underlying member-removal parameters.
	*group.RemoveMemberRequest
}

// UpdateGroupMemberRequest holds the data needed to update a user's role in a group
type UpdateGroupMemberRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	// UpdateMemberRoleRequest carries the underlying member-role update payload.
	*group.UpdateMemberRoleRequest
}

// CreateReminderRequest holds the data needed to create a reminder for the current user.
type CreateReminderRequest struct {
	// UserID is the authenticated requester creating the reminder.
	UserID string

	// CreateReminderRequest carries the underlying reminder creation payload.
	*reminder.CreateReminderRequest
}

// GetReminderByIDRequest holds the data needed to get a reminder.
type GetReminderByIDRequest struct {
	// UserID is the authenticated requester fetching the reminder.
	UserID string

	// Id is the reminder identifier to fetch.
	Id string
}

// ListRemindersRequest holds the data needed to list reminders.
type ListRemindersRequest struct {
	// UserID is the authenticated requester listing reminders.
	UserID string

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
	// UserID is the authenticated requester updating the reminder.
	UserID string

	// Id is the reminder identifier to update.
	Id string

	// UpdateReminderByIDRequest carries the underlying reminder update payload.
	*reminder.UpdateReminderByIDRequest
}

// DeleteReminderByIDRequest holds the data needed to delete a reminder.
type DeleteReminderByIDRequest struct {
	// UserID is the authenticated requester deleting the reminder.
	UserID string

	// Id is the reminder identifier to delete.
	Id string
}

// DisableReminderByIDRequest holds the data needed to disable a reminder.
type DisableReminderByIDRequest struct {
	// UserID is the authenticated requester disabling the reminder.
	UserID string

	// Id is the reminder identifier to disable.
	Id string
}

// GetDueRemindersRequest holds the data needed to get due reminders.
type GetDueRemindersRequest struct {
	// UserID is the authenticated requester fetching due reminders.
	UserID string

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
	// UserID is the authenticated requester fetching reminder stats.
	UserID string

	// FilterUserID optionally filters reminder stats by a single target user.
	FilterUserID string `query:"user_id"`

	// FilterUserIDs optionally filters reminder stats by multiple target users.
	FilterUserIDs []string `query:"user_ids"`
}

// UpdateGroupOwnerRequest holds the data needed to update group ownership
type UpdateGroupOwnerRequest struct {

	// UserID is the ID of the making the request
	UserID string

	// UpdateOwnerRequest carries the underlying group-owner update payload.
	*group.UpdateOwnerRequest
}
