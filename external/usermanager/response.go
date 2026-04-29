package usermanager

import (
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// GetGroupsByUserIDResponse represents the response for fetching groups by user ID
type GetGroupsByUserIDResponse struct {
	*group.GetGroupsByUserIDResponse
}

// GetUserGroupMembershipsResponse represents the response for fetching user group memberships
type GetUserGroupMembershipsResponse struct {
	// Memberships is a list of groups the user has access to (and directly or indirectly a member of)
	Memberships []UserGroupMembership `json:"memberships"`

	// UserID is the ID of the user these memberships belong to
	UserID string `json:"user_id"`
}

// GetUserMicroProfileResponse holds response data for GetUserMicroProfile request
type GetUserMicroProfileResponse struct {
	*userv2.GetUserMicroProfileResponse
}

// GetUserByIDResponse holds response data for GetUserByID request
type GetUserByIDResponse struct {

	// User is user that was requested by ID
	User *userv2.UniversalUser
}

// GetUsersResponse holds response data for GetUsers request
type GetUsersResponse struct {
	*userv2.GetUsersResponse
}

// GetUserProfileResponse holds response data for GetUserProfile request
type GetUserProfileResponse struct {
	*userv2.GetUserProfileResponse
}

// UpdateUserProfileResponse holds response data for UpdateUserProfile request
type UpdateUserProfileResponse struct {
	*userv2.UpdateUserResponse
}

// CreateCommsResponse holds the response from creating a comms
type CreateCommsResponse struct {
	Comms *contacter.Comms `json:"comms"`
}

// GetCommsResponse holds the response from getting a comms
type GetCommsResponse struct {
	Comms []contacter.Comms      `json:"comms"`
	Meta  map[string]interface{} `json:"-"`
}

// UpdateCommsResponse holds the response from updating a comms
type UpdateCommsResponse struct {
	Comms *contacter.Comms `json:"comms"`
}

// GetCommsStatsResponse holds the response from getting comms stats
type GetCommsStatsResponse struct {
	Stats *contacter.CommsStats `json:"stats"`
}

// GetEnrichedUserProfileResponse holds the response for an enriched user profile
type GetEnrichedUserProfileResponse struct {
	Profile *EnrichedUserProfile `json:"profile"`
}

// GetUserGroupsResponse holds the response for user groups
type GetUserGroupsResponse struct {
	Groups []GroupSummary         `json:"groups"`
	Total  int                    `json:"-"`
	Meta   map[string]interface{} `json:"-"`
}

// GetMetaData returns formatted metadata for pagination
func (r *GetUserGroupsResponse) GetMetaData() map[string]interface{} {
	if r.Meta != nil {
		return r.Meta
	}
	return map[string]interface{}{
		"total_resources": r.Total,
	}
}

// CreateGroupResponse holds the response for creating a new group
type CreateGroupResponse struct {
	Group *group.UniversalGroup `json:"group"`
}

// UpdateGroupResponse holds the response for updating an existing group
type UpdateGroupResponse struct {
	*group.UpdateGroupResponse
}

// DeleteGroupResponse holds the response for deleting a group
type DeleteGroupResponse struct {
	*group.DeleteGroupResponse
}

// GetGroupDetailResponse holds the enriched response for group detail
type GetGroupDetailResponse struct {
	Detail *GroupDetail `json:"detail"`
}

// GetGroupStatsResponse holds the response for group stats
type GetGroupStatsResponse struct {
	Stats GroupStats `json:"stats"`
}

// GetGroupsConfigResponse holds the response for the groups service config
type GetGroupsConfigResponse struct {
	*group.GetGroupsConfigResponse
}

// GetGroupLineageResponse holds the response for fetching a group's lineage
type GetGroupLineageResponse struct {
	*group.GetGroupLineageResponse
}

// GetGroupDescendantsResponse holds the response for fetching a group's descendants
type GetGroupDescendantsResponse struct {
	*group.GetGroupDescendantsResponse
}

// ValidateGroupNameResponse holds the response for validating a proposed group name
type ValidateGroupNameResponse struct {
	*group.ValidateGroupNameResponse
}

// EnrichedMember holds a group member with their user profile details resolved
type EnrichedMember struct {
	// ID is the user or group ID
	ID string `json:"id"`

	// FullName is the member's resolved full name
	FullName string `json:"full_name,omitempty"`

	// Initials are derived from the resolved name for avatar display
	Initials string `json:"initials,omitempty"`

	// Email is the member's resolved email address
	Email string `json:"email,omitempty"`

	// Type is the user's account type (for example USER, SERVICE, API)
	Type string `json:"type,omitempty"`

	// Roles are the user's platform roles
	Roles []string `json:"roles,omitempty"`

	// Role is the member's role within the group
	Role string `json:"role,omitempty"`

	// JoinedAt is when the member joined the group
	JoinedAt string `json:"joined_at,omitempty"`
}

// EnrichedOwner holds resolved owner details
type EnrichedOwner struct {
	Owner *EnrichedMember `json:"owner,omitempty"`
}

// GroupDetail holds the full enriched group view returned to the admin
type GroupDetail struct {
	// Group is the raw group object
	Group *group.UniversalGroup `json:"group"`

	// Members is the list of current members enriched with user profile data
	Members []EnrichedMember `json:"members"`

	// Owner holds the resolved owner position
	Owner *EnrichedOwner `json:"owner,omitempty"`
}

// AddGroupMemberResponse holds the response for adding a member to a group
type AddGroupMemberResponse struct {
	Success bool `json:"success"`
}

// RemoveGroupMemberResponse holds the response for removing a member from a group
type RemoveGroupMemberResponse struct {
	Success bool `json:"success"`
}

// UpdateGroupOwnerResponse holds the response for updating group ownership
type UpdateGroupOwnerResponse struct {
	Owner *EnrichedOwner `json:"owner"`
}
