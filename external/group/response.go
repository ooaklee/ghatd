package group

import "github.com/ooaklee/ghatd/external/toolbox"

// GetGroupsResponse defines the response structure for getting groups
type GetGroupsResponse struct {
	Total      int               `json:"total"`
	TotalPages int               `json:"total_pages"`
	Groups     []*UniversalGroup `json:"groups"`
	Page       int               `json:"page"`
	PerPage    int               `json:"per_page"`
}

// GetGroupsByUserIDResponse defines the response for getting groups referenced by a user.
type GetGroupsByUserIDResponse struct {
	Groups      []*UniversalGroup                   `json:"groups"`
	Descendants map[string][][]GroupDescendantsNode `json:"descendants"`
}

// UserGroupAccessSummary describes a user's effective access for a given group.
type UserGroupAccessSummary struct {
	// MaxRole is the highest role observed for the user in that group.
	MaxRole string `json:"max_role"`

	// IsAccessible is true when the user should be considered to have access to the group.
	IsAccessible bool `json:"is_accessible"`

	// IsAdmin is true when the user has effective admin privileges in that group.
	IsAdmin bool `json:"is_admin"`
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

// GroupLineageNode is a compact representation of a group in a lineage chain.
type GroupLineageNode struct {
	ID            string `json:"id"`
	ParentGroupID string `json:"parent_group_id,omitempty"`
	Name          string `json:"name"`
	RawName       string `json:"raw_name,omitempty"`
	Type          string `json:"type"`
	IsMember      bool   `json:"is_member,omitempty"`
	IsOwner       bool   `json:"is_owner,omitempty"`
	IsAdmin       bool   `json:"is_admin,omitempty"`
}

// GetGroupLineageResponse defines the response for getting a group's lineage.
type GetGroupLineageResponse struct {
	Lineage []GroupLineageNode `json:"lineage"`
}

// GroupDescendantsNode is a compact representation of a group in descendants results.
// It intentionally matches GroupLineageNode.
type GroupDescendantsNode = GroupLineageNode

// GetGroupDescendantsResponse defines the response for getting a group's descendants.
// Descendants are grouped by level depth where index 0 = direct children.
type GetGroupDescendantsResponse struct {
	Descendants [][]GroupDescendantsNode `json:"descendants"`
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

// UpdateOwnerResponse defines the response for updating ownership.
type UpdateOwnerResponse struct {
	Group *UniversalGroup `json:"group"`
}

// RepairInvalidMembersResponse defines the response for invalid member repair operations.
type RepairInvalidMembersResponse struct {
	BeforeAffectedGroupIDs []string `json:"before_affected_group_ids"`
	AfterAffectedGroupIDs  []string `json:"after_affected_group_ids"`
	RepairedGroupIDs       []string `json:"repaired_group_ids"`
	BeforeCount            int      `json:"before_count"`
	AfterCount             int      `json:"after_count"`
	RepairedCount          int      `json:"repaired_count"`
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

// GroupsByStatusStats holds per-status group counts.
type GroupsByStatusStats map[string]int64

// GroupsByTypeStats holds per-type group counts.
type GroupsByTypeStats map[string]int64

// GroupsByVisibilityStats holds per-visibility group counts.
type GroupsByVisibilityStats map[string]int64

// GroupIntegrationStats holds integration-related group counts.
type GroupIntegrationStats struct {
	WithSlack          int64 `json:"with_slack"`
	WithCustom         int64 `json:"with_custom"`
	WithAnyIntegration int64 `json:"with_any_integration"`
}

// GroupLeadershipStats holds ownership-related group counts.
type GroupLeadershipStats struct {
	WithOwner int64 `json:"with_owner"`
	WithAny   int64 `json:"with_any"`
}

// AllGroupsStats holds aggregated platform group statistics.
type AllGroupsStats struct {
	Total        int64                   `json:"total"`
	TotalMembers int64                   `json:"total_members"`
	ByStatus     GroupsByStatusStats     `json:"by_status"`
	ByType       GroupsByTypeStats       `json:"by_type"`
	ByVisibility GroupsByVisibilityStats `json:"by_visibility"`
	Integrations GroupIntegrationStats   `json:"integrations"`
	Leadership   GroupLeadershipStats    `json:"leadership"`
}

// GetGroupsStatsResponse defines the response for aggregate groups stats.
type GetGroupsStatsResponse struct {
	*AllGroupsStats
}

// NormaliseGroupsStatsKeysToSnakeCase normalises dynamic map keys in stats payloads.
func (r *GetGroupsStatsResponse) NormaliseGroupsStatsKeysToSnakeCase() {
	if r == nil || r.AllGroupsStats == nil {
		return
	}

	r.ByStatus = normaliseStatsMapKeysToSnakeCase(r.ByStatus)
	r.ByType = normaliseStatsMapKeysToSnakeCase(r.ByType)
	r.ByVisibility = normaliseStatsMapKeysToSnakeCase(r.ByVisibility)
}

// normaliseStatsMapKeysToSnakeCase converts dynamic stats map keys into
// lower snake_case while preserving their associated counts.
func normaliseStatsMapKeysToSnakeCase(input map[string]int64) map[string]int64 {
	if input == nil {
		return nil
	}

	output := make(map[string]int64, len(input))
	for key, value := range input {
		normalisedKey := toolbox.StringConvertToSnakeCase(toolbox.StringStandardisedToLower(key))
		output[normalisedKey] = value
	}

	return output
}

// Statuses describes status configuration for groups.
type Statuses struct {
	Default     string              `json:"default"`
	Valid       []string            `json:"valid"`
	Transitions map[string][]string `json:"transitions"`
}

// Types describes valid group and member types.
type Types struct {
	Valid            []string `json:"valid"`
	ValidMemberTypes []string `json:"valid_member_types"`
}

// GroupNesting describes hierarchical group nesting configuration.
type GroupNesting struct {
	Allow           bool                `json:"allow"`
	MaxNestingDepth int                 `json:"max_nesting_depth"`
	Tree            map[string][]string `json:"tree,omitempty"`
}

// Roles describes role configuration for groups.
type Roles struct {
	Default       []string            `json:"default"`
	TypeOverrides map[string][]string `json:"type_overrides"`
}

// GroupConfigCapabilities describes the capabilities exposed by the group config.
type GroupConfigCapabilities struct {
	RequiredFields      []string     `json:"required_fields,omitempty"`
	MultipleIdentifiers bool         `json:"multiple_identifiers"`
	Statuses            Statuses     `json:"statuses"`
	Types               Types        `json:"types"`
	GroupNesting        GroupNesting `json:"group_nesting"`
	Roles               Roles        `json:"roles"`
}

// GetGroupsConfigResponse defines the response for the groups config endpoint.
type GetGroupsConfigResponse struct {
	Config *GroupConfigCapabilities `json:"config"`
}

// ValidateGroupNameResponse describes the outcome of a name validation check.
// It is intended for use by front-end forms so they can show the user exactly
// what name will be stored before they submit the create request.
type ValidateGroupNameResponse struct {
	// RawName is the user's input after leading/trailing whitespace is removed.
	RawName string `json:"raw_name"`

	// Name is the kebab-case, de-duplicated name that will be persisted if the
	// user proceeds with creation.
	Name string `json:"name"`

	// Adjusted is true whenever Name differs from the kebab-case conversion of
	// RawName (i.e. a numeric suffix was appended to avoid a collision).
	Adjusted bool `json:"adjusted"`

	// Available is true when no existing group of the same type already uses
	// the base kebab-case name.  For non-root types this is always true because
	// a unique suffix is generated automatically.
	Available bool `json:"available"`

	// IsRootType is true when the requested group type is a root of one of the
	// independent hierarchy trees (e.g. ORGANISATION, COMMUNITY).  Root-type
	// groups require globally unique names as they are used for @-mentions.
	IsRootType bool `json:"is_root_type"`

	// Hint is a human-readable message suitable for display below a name input
	// field, explaining any adjustments or naming rules that apply.
	Hint string `json:"hint,omitempty"`
}
