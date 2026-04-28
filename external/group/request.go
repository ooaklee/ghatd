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

	// Owner filter
	OwnerID string ` query:"owner_id"`

	// Settings filters
	Visibility string `query:"visibility"`

	// Search
	NameSearch string `query:"name_search"`

	// PrefixName if true, prefixes child group names with the root group's name
	PrefixName bool `query:"prefix_name"`

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

// GetGroupsByUserIDRequest defines the request for getting groups referenced by a user.
type GetGroupsByUserIDRequest struct {
	UserID string `path:"userID"`

	// IncludeDescendants if true, includes descendants per root group
	// that the user can access.
	IncludeDescendants bool `query:"include_descendants"`

	// PrefixName if true, prefixes child group names with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetGroupByIDRequest defines the request for getting a single group
type GetGroupByIDRequest struct {
	ID string `path:"groupID"`

	// PrefixName if true, prefixes child group name with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetGroupByNanoIDRequest defines the request for getting a single group by nano ID
type GetGroupByNanoIDRequest struct {
	NanoID string `path:"groupNanoID"`

	// PrefixName if true, prefixes child group name with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// CreateGroupRequest defines the request for creating a new group
type CreateGroupRequest struct {
	Name          string                 `json:"name" validate:"required"`
	Type          string                 `json:"type" validate:"required"`
	ParentGroupID string                 `json:"parent_group_id,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Email         string                 `json:"email,omitempty"`
	Icon          string                 `json:"icon,omitempty"`
	Visibility    string                 `json:"visibility,omitempty"`
	Extensions    map[string]interface{} `json:"extensions,omitempty"`

	// Initial members
	InitialMembers []CreateMemberRequest `json:"initial_members,omitempty"`

	// Initial owner
	OwnerID string `json:"owner_id,omitempty"`
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
	GroupID             string `path:"groupID"`
	MemberID            string `path:"memberID"`
	ConfirmOwnerRemoval bool   `query:"confirm_owner_removal"`
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

// UpdateOwnerRequest defines the request for updating ownership.
type UpdateOwnerRequest struct {

	// GroupID is the ID of the group
	GroupID string `path:"groupID"`

	// OwnerID is the new owner's user ID (nil = no change, empty string = clear)
	OwnerID *string `json:"owner_id,omitempty"`
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

// GetGroupLineageRequest defines the request for getting a group's lineage
type GetGroupLineageRequest struct {
	ID string `path:"groupID"`

	// AsUserID if provided, checks whether given ID is a direct member of
	// each lineage group and includes this information in the response
	AsUserID string `query:"as_user_id"`

	// PrefixName if true, prefixes child group names with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetGroupDescendantsRequest defines the request for getting all descendants of a group.
type GetGroupDescendantsRequest struct {

	// ID is the ID of the group to fetch descendants for
	ID string `path:"groupID"`

	// MaxDepth limits how many levels of descendants to fetch (0 or negative for unlimited)
	MaxDepth int `query:"max_depth"`

	// IncludeSelf if true, includes the root group itself in the response
	IncludeSelf bool `query:"include_self"`

	// AsUserID allows permission checks using the specified user ID when retrieving
	// group descendants, ensuring that the response includes only the groups the
	// user can access.
	AsUserID string `query:"as_user_id"`

	// PrefixName if true, prefixes child group names with the root group's name
	PrefixName bool `query:"prefix_name"`
}

// GetGroupsStatsRequest defines the request for getting aggregate groups stats
type GetGroupsStatsRequest struct{}

// GetGroupsConfigRequest defines the request for getting the groups config
type GetGroupsConfigRequest struct{}

// ValidateGroupNameRequest defines the request for validating a proposed group name
type ValidateGroupNameRequest struct {
	Name          string `json:"name" query:"name" validate:"required"`
	Type          string `json:"type" query:"type" validate:"required"`
	ParentGroupID string `json:"parent_group_id,omitempty" query:"parent_group_id"`
}
