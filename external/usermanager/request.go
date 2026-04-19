package usermanager

import (
	"github.com/ooaklee/ghatd/external/contacter"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

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
}

// UpdateUserProfileRequest holds all the data needed to action user
// profile update request
type UpdateUserProfileRequest struct {

	// UserId the ID of the user requesting their profile
	UserId string

	*userv2.UpdateUserRequest
}

// DeleteUserPermanentlyRequest holds all the data needed to delete user and resources
type DeleteUserPermanentlyRequest struct {

	// UserId the Id of the user requesting the deletion
	UserId string
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
	*contacter.CreateCommsRequest
}

// GetCommsRequest holds everything needed to make
// the request to get a comms
type GetCommsRequest struct {

	// UserId is the id of the user making the request
	UserId string

	*contacter.GetCommsRequest
}

// UpdateCommsRequest holds everything needed to make
// the request to update a comms
type UpdateCommsRequest struct {

	// UserId is the id of the user making the request
	UserId string

	*contacter.UpdateCommsRequest
}

// GetEnrichedUserProfileRequest holds the data needed to get an enriched user profile
type GetEnrichedUserProfileRequest struct {

	// UserId is the ID of the user requesting their enriched profile
	UserId string

	// IncludeTeams indicates whether to include team memberships
	IncludeTeams bool `query:"include_teams"`

	// IncludeDepartments indicates whether to include department memberships
	IncludeDepartments bool `query:"include_departments"`

	// IncludeAllGroups indicates whether to include all group memberships
	IncludeAllGroups bool `query:"include_all_groups"`
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
}

// GetUserTeamMembershipsRequest holds the data needed to get team memberships for a user
type GetUserTeamMembershipsRequest struct {

	// UserId is the ID of the user (can be current user or admin checking another user)
	UserId string

	// TargetUserId is the ID of the user to check memberships for (admin feature)
	TargetUserId string `query:"target_user_id"`

	// IncludeInactive indicates whether to include inactive teams
	IncludeInactive bool `query:"include_inactive"`
}

// UpdateUserTeamMembershipRequest holds the data needed to update a user's team membership
type UpdateUserTeamMembershipRequest struct {

	// UserId is the ID of the user making the request
	UserId string

	// GroupID is the ID of the group to join/update
	GroupID string

	// Role is the role to assign (optional, uses default if not specified)
	Role string `json:"role,omitempty"`

	// RemoveFromOtherTeams indicates whether to leave other teams of the same type
	RemoveFromOtherTeams bool `json:"remove_from_other_teams,omitempty"`

	// DisableNotification specifies whether to disable notifications for this action
	DisableNotification bool `json:"disable_notification,omitempty"`
}

// RemoveUserFromGroupRequest holds the data needed to remove a user from a group
type RemoveUserFromGroupRequest struct {

	// UserId is the ID of the user making the request
	UserId string

	// GroupID is the ID of the group to leave
	GroupID string

	// DisableNotification specifies whether to disable notifications for this action
	DisableNotification bool `json:"disable_notification,omitempty" query:"disable_notification"`
}

// FindUserInfoRequest holds the data needed to find user information with flexible lookup
type FindUserInfoRequest struct {

	// UserId is the ID of the user to find (direct lookup)
	UserId string `query:"user_id"`

	// Email is the email address to search by
	Email string `query:"email"`

	// IncludeTeamMemberships indicates whether to include team membership data
	IncludeTeamMemberships bool `query:"include_team_memberships"`

	// IncludeGroupMemberships indicates whether to include all group membership data
	IncludeGroupMemberships bool `query:"include_group_memberships"`
}

// BulkUpdateUserGroupMembershipsRequest holds the data for bulk membership updates
type BulkUpdateUserGroupMembershipsRequest struct {

	// UserId is the ID of the user making the request (must be admin)
	UserId string

	// TargetUserId is the ID of the user whose memberships are being updated
	TargetUserId string

	// Actions are the membership actions to perform
	Actions []GroupMembershipAction `json:"actions"`

	// DisableNotifications specifies whether to disable notifications for all actions
	DisableNotifications bool `json:"disable_notifications,omitempty"`
}

// GetGroupsByTypeRequest holds the data needed to get groups filtered by type
type GetGroupsByTypeRequest struct {

	// UserId is the ID of the user making the request
	UserId string

	// GroupType is the type to filter by (TEAM, DEPARTMENT, ORGANIZATION, etc.)
	GroupType string `query:"type"`

	// Name filters groups by name (optional, partial match)
	Name string `query:"name"`

	// Status filters by status (optional: ACTIVE, INACTIVE, etc.)
	Status string `query:"status"`

	// OnlyUserMemberships returns only groups the user belongs to
	OnlyUserMemberships bool `query:"only_user_memberships"`

	// Page specifies the page results should be taken from. Default 1.
	Page int `query:"page"`

	// PerPage specifies the number of groups to return per page. Default 25. Max 100.
	PerPage int `query:"per_page" validate:"max=100"`

	// Meta indicates whether response should contain meta information
	Meta bool `query:"meta"`

	// Order defines how results should be sorted (e.g., "name_asc", "created_at_desc")
	Order string `query:"order"`
}

// CreateGroupRequest holds the data needed for an admin to create a new group
type CreateGroupRequest struct {

	// AdminUserId is the ID of the admin making the request
	AdminUserId string

	// Name is the name of the group
	Name string `json:"name" validate:"required"`

	// Type is the type of group (TEAM, DEPARTMENT, etc.)
	Type string `json:"type" validate:"required"`

	// Description is the group description
	Description string `json:"description,omitempty"`

	// Email is the group contact email
	Email string `json:"email,omitempty"`

	// Icon is the group icon/emoji
	Icon string `json:"icon,omitempty"`

	// Visibility controls group visibility (PUBLIC, PRIVATE, etc.)
	Visibility string `json:"visibility,omitempty"`

	// Extensions are custom key-value pairs
	Extensions map[string]interface{} `json:"extensions,omitempty"`

	// InitialMemberIDs are user IDs to add as initial members
	InitialMemberIDs []string `json:"initial_member_ids,omitempty"`

	// InitialMemberRole is the default role for initial members
	InitialMemberRole string `json:"initial_member_role,omitempty"`

	// OwnerID is the ID of the group owner
	OwnerID string `json:"owner_id,omitempty"`

	// HeadID is the ID of the group head
	HeadID string `json:"head_id,omitempty"`

	// LeadID is the ID of the group lead
	LeadID string `json:"lead_id,omitempty"`
}

// GetGroupDetailRequest holds the data needed to fetch a specific group's detail
type GetGroupDetailRequest struct {

	// UserId is the ID of the requester
	UserId string

	// GroupID is the ID of the group to fetch
	GroupID string
}

// GetGroupStatsRequest holds the data needed to fetch stats for a specific group
type GetGroupStatsRequest struct {

	// UserId is the ID of the requester
	UserId string

	// GroupID is the ID of the group to fetch
	GroupID string
}
