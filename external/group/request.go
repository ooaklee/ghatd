package group

// GetGroupsRequest defines the request structure for getting groups
type GetGroupsRequest struct {
	// Type filters (flexible - any type string)
	Types []string `json:"types,omitempty" query:"types"`

	// Status filters
	Statuses []string `query:"statuses"`

	// Member filters
	MemberID       string              ` query:"member_id"`
	MemberType     string              ` query:"member_type"`
	MembersWithIDs map[string][]string ` query:"members_with_ids"`

	// Leadership filters
	OwnerID string ` query:"owner_id"`
	HeadID  string ` query:"head_id"`
	LeadID  string ` query:"lead_id"`

	// Settings filters
	Visibility string `query:"visibility"`

	// Search
	NameSearch string `query:"name_search"`

	// Pagination
	Page       int    ` query:"page"`
	PageSize   int    ` query:"page_size"`
	PerPage    int    ` query:"per_page"` // Alias for PageSize
	OrderBy    string ` query:"order_by"`
	Meta       bool   ` query:"meta"`
	TotalCount int    ` query:"total_count"`

	// Extension filters (for custom queries)
	ExtensionFilters map[string]interface{} `json:"extension_filters,omitempty" query:"extension_filters"`
}

// GetGroupByIDRequest defines the request for getting a single group
type GetGroupByIDRequest struct {
	ID string `path:"groupID"`
}

// GetGroupByNanoIDRequest defines the request for getting a single group by nano ID
type GetGroupByNanoIDRequest struct {
	NanoID string `path:"groupNanoID"`
}

// GetGroupByNameRequest defines the request for getting a group by name
type GetGroupByNameRequest struct {
	Name string `query:"name"`
	Type string `query:"type"` // Optional type filter
}

// CreateGroupRequest defines the request for creating a new group
type CreateGroupRequest struct {
	Name        string                 `json:"name" validate:"required"`
	Type        string                 `json:"type" validate:"required"`
	Description string                 `json:"description,omitempty"`
	Email       string                 `json:"email,omitempty"`
	Icon        string                 `json:"icon,omitempty"`
	Visibility  string                 `json:"visibility,omitempty"`
	Extensions  map[string]interface{} `json:"extensions,omitempty"`

	// Initial members
	InitialMembers []CreateMemberRequest `json:"initial_members,omitempty"`

	// Initial leadership
	OwnerID string `json:"owner_id,omitempty"`
	HeadID  string `json:"head_id,omitempty"`
	LeadID  string `json:"lead_id,omitempty"`
}

// CreateMemberRequest defines member data for creation
type CreateMemberRequest struct {
	ID   string `json:"id" validate:"required"`
	Type string `json:"type" validate:"required"`
	Role string `json:"role,omitempty"`
}

// UpdateGroupRequest defines the request for updating a group
type UpdateGroupRequest struct {
	ID          string                 `path:"groupID"`
	Name        *string                `json:"name,omitempty"`
	Description *string                `json:"description,omitempty"`
	Email       *string                `json:"email,omitempty"`
	Icon        *string                `json:"icon,omitempty"`
	Visibility  *string                `json:"visibility,omitempty"`
	Status      *string                `json:"status,omitempty"`
	Extensions  map[string]interface{} `json:"extensions,omitempty"`
	// Group if specified updates entire group object
	Group *UniversalGroup `json:"group,omitempty"`
}

// AddMemberRequest defines the request for adding a member
type AddMemberRequest struct {
	GroupID  string `path:"groupID"`
	MemberID string `json:"member_id" validate:"required"`
	Type     string `json:"type" validate:"required"`
	Role     string `json:"role,omitempty"`
}

// RemoveMemberRequest defines the request for removing a member
type RemoveMemberRequest struct {
	GroupID  string `path:"groupID"`
	MemberID string `path:"memberID"`
}

// UpdateMemberRoleRequest defines the request for updating a member's role
type UpdateMemberRoleRequest struct {
	GroupID  string `path:"groupID"`
	MemberID string `path:"memberID"`
	NewRole  string `json:"new_role" validate:"required"`
}

// GetGroupMembersRequest defines the request for getting group members
type GetGroupMembersRequest struct {
	GroupID    string `path:"groupID"`
	MemberType string `query:"member_type"`
	Role       string `query:"role"`
}

// UpdateLeadershipRequest defines the request for updating leadership
type UpdateLeadershipRequest struct {
	GroupID string  `path:"groupID"`
	OwnerID *string `json:"owner_id,omitempty"`
	HeadID  *string `json:"head_id,omitempty"`
	LeadID  *string `json:"lead_id,omitempty"`
}

// DeleteGroupRequest defines the request for deleting a group
type DeleteGroupRequest struct {
	ID          string `path:"groupID"`
	DeletedByID string `query:"deleted_by_id"`
	HardDelete  bool   `query:"hard_delete"`
}

// ArchiveGroupRequest defines the request for archiving a group
type ArchiveGroupRequest struct {
	ID string `path:"groupID"`
}

// RestoreGroupRequest defines the request for restoring a group
type RestoreGroupRequest struct {
	ID string `path:"groupID"`
}

// GetGroupStatsRequest defines the request for getting group statistics
type GetGroupStatsRequest struct {
	ID string `path:"groupID"`
}
