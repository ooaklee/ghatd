package usermanager

import (
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// GetUserMicroProfileResponse holds response data for GetUserMicroProfile request
type GetUserMicroProfileResponse struct {
	*userv2.GetUserMicroProfileResponse
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

// GetUserTeamMembershipsResponse holds the response for team memberships
type GetUserTeamMembershipsResponse struct {
	Memberships map[string]UserGroupMembership `json:"memberships"`
	UserID      string                         `json:"user_id"`
}

// UpdateUserTeamMembershipResponse holds the response for updating team membership
type UpdateUserTeamMembershipResponse struct {
	Success             bool                `json:"success"`
	Membership          UserGroupMembership `json:"membership"`
	PreviousMemberships []GroupSummary      `json:"previous_memberships,omitempty"`
}

// RemoveUserFromGroupResponse holds the response for removing a user from a group
type RemoveUserFromGroupResponse struct {
	Success bool   `json:"success"`
	GroupID string `json:"group_id"`
	Message string `json:"message,omitempty"`
}

// FindUserInfoResponse holds the response for finding user information
type FindUserInfoResponse struct {
	User                  *userv2.UserProfile   `json:"user"`
	TeamMemberships       []UserGroupMembership `json:"team_memberships,omitempty"`
	GroupMemberships      []UserGroupMembership `json:"group_memberships,omitempty"`
	MembershipDataFetched bool                  `json:"membership_data_fetched"`
}

// BulkUpdateUserGroupMembershipsResponse holds the response for bulk membership updates
type BulkUpdateUserGroupMembershipsResponse struct {
	Results      []GroupMembershipUpdateResult `json:"results"`
	SuccessCount int                           `json:"success_count"`
	FailureCount int                           `json:"failure_count"`
	TotalActions int                           `json:"total_actions"`
}

// GetGroupsByTypeResponse holds the response for getting groups by type
type GetGroupsByTypeResponse struct {
	Groups []GroupSummary         `json:"groups"`
	Total  int                    `json:"-"`
	Meta   map[string]interface{} `json:"-"`
}

// GetMetaData returns formatted metadata for pagination
func (r *GetGroupsByTypeResponse) GetMetaData() map[string]interface{} {
	if r.Meta != nil {
		return r.Meta
	}
	return map[string]interface{}{
		"total_resources": r.Total,
	}
}

// CreateGroupResponse holds the response for creating a new group
type CreateGroupResponse struct {
	Group *GroupSummary `json:"group"`
}

// GetGroupDetailResponse holds the response for group detail
type GetGroupDetailResponse struct {
	Group *group.UniversalGroup `json:"group"`
}

// GetGroupStatsResponse holds the response for group stats
type GetGroupStatsResponse struct {
	Stats GroupStats `json:"stats"`
}
