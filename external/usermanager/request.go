package usermanager

import (
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
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
	// UserId is the ID of the requester.
	UserId string

	// GetLatestNotificationOverviewsRequest carries the underlying notification query parameters.
	*common.GetLatestNotificationOverviewsRequest
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

// UpdateGroupOwnerRequest holds the data needed to update group ownership
type UpdateGroupOwnerRequest struct {

	// UserID is the ID of the making the request
	UserID string

	// UpdateOwnerRequest carries the underlying group-owner update payload.
	*group.UpdateOwnerRequest
}
