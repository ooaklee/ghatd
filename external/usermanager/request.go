package usermanager

import (
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// GetUserGroupMembershipsRequest represents the request for fetching user group memberships with filtering options
type GetUserGroupMembershipsRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	// GroupType if specified, filters groups to a specific type (e.g. "team", "department")
	GroupType string `query:"group_type"`

	// IncludeDescendants if true, includes descendant groups per root group that the user can access
	IncludeDescendants bool `query:"include_descendants"`
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

	*userv2.GetUserProfileRequest
}

// GetUserByIDRequest holds all the data needed to action user retrieval request
type GetUserByIDRequest struct {

	// UserId the ID of the user making the request
	UserId string

	*userv2.GetUserByIDRequest
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

	// ID is the ID of the user to be deleted
	ID string `path:"userID"`
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

// GetCommsStatsRequest holds everything needed to make
// the request to get comms stats
type GetCommsStatsRequest struct {

	// UserId is the id of the user making the request
	UserId string

	*contacter.GetCommsStatsRequest
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

// CreateGroupRequest holds the data needed for a user to create a new group
type CreateGroupRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	*group.CreateGroupRequest
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

// AddGroupMemberRequest holds the data needed to add a user to a group
type AddGroupMemberRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	*group.AddMemberRequest
}

// RemoveGroupMemberRequest holds the data needed to remove a user from a group
type RemoveGroupMemberRequest struct {

	// UserID is the ID of the user making the request
	UserID string

	*group.RemoveMemberRequest
}

// UpdateGroupOwnerRequest holds the data needed to update group ownership
type UpdateGroupOwnerRequest struct {

	// UserID is the ID of the making the request
	UserID string

	*group.UpdateOwnerRequest
}
