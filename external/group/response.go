package group

// GetGroupsResponse defines the response structure for getting groups
type GetGroupsResponse struct {
	Total      int               `json:"total"`
	TotalPages int               `json:"total_pages"`
	Groups     []*UniversalGroup `json:"groups"`
	Page       int               `json:"page"`
	PerPage    int               `json:"per_page"`
}

// GetMetaData returns metadata in reply.WithMeta format
func (r *GetGroupsResponse) GetMetaData() map[string]interface{} {
	return map[string]interface{}{
		"page":            r.Page,
		"per_page":        r.PerPage,
		"total_resources": r.Total,
		"total_pages":     r.TotalPages,
	}
}

// GetGroupByIDResponse defines the response for getting a single group
type GetGroupByIDResponse struct {
	Group *UniversalGroup `json:"group"`
}

// GetGroupByNanoIDResponse defines the response for getting a single group by nano ID
type GetGroupByNanoIDResponse struct {
	Group *UniversalGroup `json:"group"`
}

// GetGroupByNameResponse defines the response for getting a group by name
type GetGroupByNameResponse struct {
	Group *UniversalGroup `json:"group"`
}

// CreateGroupResponse defines the response for creating a group
type CreateGroupResponse struct {
	Group *UniversalGroup `json:"group"`
}

// UpdateGroupResponse defines the response for updating a group
type UpdateGroupResponse struct {
	Group *UniversalGroup `json:"group"`
}

// DeleteGroupResponse defines the response for deleting a group
type DeleteGroupResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// GetGroupMembersResponse defines the response for getting group members
type GetGroupMembersResponse struct {
	Members []Member `json:"members"`
	Count   int      `json:"count"`
}

// AddMemberResponse defines the response for adding a member
type AddMemberResponse struct {
	Group *UniversalGroup `json:"group"`
}

// RemoveMemberResponse defines the response for removing a member
type RemoveMemberResponse struct {
	Group *UniversalGroup `json:"group"`
}

// UpdateMemberRoleResponse defines the response for updating a member role
type UpdateMemberRoleResponse struct {
	Group *UniversalGroup `json:"group"`
}

// UpdateLeadershipResponse defines the response for updating leadership
type UpdateLeadershipResponse struct {
	Group *UniversalGroup `json:"group"`
}

// ArchiveGroupResponse defines the response for archiving a group
type ArchiveGroupResponse struct {
	Group *UniversalGroup `json:"group"`
}

// RestoreGroupResponse defines the response for restoring a group
type RestoreGroupResponse struct {
	Group *UniversalGroup `json:"group"`
}

// GetGroupStatsResponse defines the response for group statistics
type GetGroupStatsResponse struct {
	GroupID          string `json:"group_id"`
	MemberCount      int    `json:"member_count"`
	UserMemberCount  int    `json:"user_member_count"`
	GroupMemberCount int    `json:"group_member_count"`
	SubgroupCount    int    `json:"subgroup_count,omitempty"`
}
