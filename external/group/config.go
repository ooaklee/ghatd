package group

import (
	"fmt"
	"sort"
)

// DefaultGroupConfig returns a default configuration
func DefaultGroupConfig() *GroupConfig {
	return &GroupConfig{
		DefaultStatus: GroupStatusActive,
		StatusTransitions: map[string][]string{
			GroupStatusActive: {
				GroupStatusProvisioned,
				GroupStatusInactive,
				GroupStatusSuspended,
			},
			GroupStatusInactive: {
				GroupStatusActive,
				GroupStatusArchived,
			},
			GroupStatusArchived: {
				GroupStatusInactive,
				GroupStatusActive,
			},
			GroupStatusSuspended: {
				GroupStatusActive,
				GroupStatusInactive,
			},
		},
		Tree: map[string][]string{
			GroupTypeOrganisation: {
				GroupTypeDepartment,
				GroupTypeTeam,
				GroupTypeSquad,
				GroupTypeTribe,
				GroupTypeCompany,
				GroupTypeCustom,
			},
			GroupTypeDepartment: {
				GroupTypeTeam,
				GroupTypeTribe,
				GroupTypeCustom,
			},
			GroupTypeTribe: {
				GroupTypeSquad,
				GroupTypeTeam,
				GroupTypeCustom,
			},
			GroupTypeTeam: {
				GroupTypeSquad,
				GroupTypeCustom,
			},
			GroupTypeCommunity: {
				GroupTypeFamily,
				GroupTypeFriends,
				GroupTypeCustom,
			},
			GroupTypeProject: {
				GroupTypeTeam,
				GroupTypeSquad,
				GroupTypeCustom,
			},
		},
		RequiredFields: []string{"name", "type"},
		ValidTypes: []string{
			GroupTypeTeam,
			GroupTypeTribe,
			GroupTypeSquad,
			GroupTypeDepartment,
			GroupTypeOrganisation,
			GroupTypeCompany,
			GroupTypeProject,
			GroupTypeCommunity,
			GroupTypeFamily,
			GroupTypeFriends,
			GroupTypeCustom,
		},
		ValidMemberTypes: []string{
			MemberTypeUser,
			MemberTypeGroup,
		},
		AllowNestedGroups:   true,
		MaxNestingDepth:     5,
		MultipleIdentifiers: true,
	}
}

// NewCustomGroupConfig creates a custom group config builder
func NewCustomGroupConfig() *GroupConfig {
	return &GroupConfig{
		StatusTransitions: make(map[string][]string),
		RequiredFields:    []string{},
		ValidTypes:        []string{},
		ValidMemberTypes:  []string{},
	}
}

// WithDefaultStatus sets the default status
func (c *GroupConfig) WithDefaultStatus(status string) *GroupConfig {
	c.DefaultStatus = status
	return c
}

// WithStatusTransition adds a status transition
func (c *GroupConfig) WithStatusTransition(toStatus string, fromStatuses []string) *GroupConfig {
	c.StatusTransitions[toStatus] = fromStatuses
	return c
}

// WithRequiredFields sets required fields
func (c *GroupConfig) WithRequiredFields(fields ...string) *GroupConfig {
	c.RequiredFields = append(c.RequiredFields, fields...)
	return c
}

// WithValidTypes sets valid group types
func (c *GroupConfig) WithValidTypes(types ...string) *GroupConfig {
	c.ValidTypes = append(c.ValidTypes, types...)
	return c
}

// WithValidMemberTypes sets valid member types
func (c *GroupConfig) WithValidMemberTypes(types ...string) *GroupConfig {
	c.ValidMemberTypes = append(c.ValidMemberTypes, types...)
	return c
}

// WithNestedGroups enables/disables nested groups
func (c *GroupConfig) WithNestedGroups(allow bool) *GroupConfig {
	c.AllowNestedGroups = allow
	return c
}

// WithMaxNestingDepth sets maximum nesting depth
func (c *GroupConfig) WithMaxNestingDepth(depth int) *GroupConfig {
	c.MaxNestingDepth = depth
	return c
}

// WithMultipleIdentifiers enables/disables multiple identifier types
func (c *GroupConfig) WithMultipleIdentifiers(allow bool) *GroupConfig {
	c.MultipleIdentifiers = allow
	return c
}

// IsValidType checks whether a group type is allowed by this config.
// If no valid types are configured, any type is accepted.
func (c *GroupConfig) IsValidType(groupType string) bool {
	if c == nil || len(c.ValidTypes) == 0 {
		return true
	}

	for _, validType := range c.ValidTypes {
		if validType == groupType {
			return true
		}
	}

	return false
}

// EffectiveTree returns the hierarchy rules that should be used by callers.
// When no explicit tree is provided, every valid type can have every valid type as a child.
func (c *GroupConfig) EffectiveTree() map[string][]string {
	tree := make(map[string][]string)
	if c == nil {
		return tree
	}

	if len(c.Tree) == 0 {
		for _, parentType := range c.ValidTypes {
			tree[parentType] = append([]string{}, c.ValidTypes...)
		}
		return tree
	}

	for parentType, childTypes := range c.Tree {
		tree[parentType] = uniqueStrings(childTypes)
	}

	return tree
}

// ValidateHierarchy validates tree references against valid types and checks duplicate child entries.
func (c *GroupConfig) ValidateHierarchy() error {
	if c == nil {
		return nil
	}

	validTypes := make(map[string]struct{}, len(c.ValidTypes))
	for _, groupType := range c.ValidTypes {
		validTypes[groupType] = struct{}{}
	}

	effectiveTree := c.EffectiveTree()
	for parentType, childTypes := range effectiveTree {
		if len(validTypes) > 0 {
			if _, exists := validTypes[parentType]; !exists {
				return fmt.Errorf("group hierarchy contains invalid parent type %q", parentType)
			}
		}

		seenChildren := make(map[string]struct{}, len(childTypes))
		for _, childType := range childTypes {
			if len(validTypes) > 0 {
				if _, exists := validTypes[childType]; !exists {
					return fmt.Errorf("group hierarchy contains invalid child type %q for parent %q", childType, parentType)
				}
			}

			if _, exists := seenChildren[childType]; exists {
				return fmt.Errorf("group hierarchy contains duplicate child type %q for parent %q", childType, parentType)
			}
			seenChildren[childType] = struct{}{}
		}
	}

	return nil
}

// ReverseTreeIndex builds child -> allowed parent types from the effective hierarchy.
func (c *GroupConfig) ReverseTreeIndex() map[string][]string {
	reverse := make(map[string][]string)
	if c == nil {
		return reverse
	}

	for _, groupType := range c.ValidTypes {
		reverse[groupType] = []string{}
	}

	effectiveTree := c.EffectiveTree()
	parentTypes := make([]string, 0, len(effectiveTree))
	for parentType := range effectiveTree {
		parentTypes = append(parentTypes, parentType)
	}
	sort.Strings(parentTypes)

	for _, parentType := range parentTypes {
		for _, childType := range effectiveTree[parentType] {
			reverse[childType] = append(reverse[childType], parentType)
		}
	}

	for childType, parentList := range reverse {
		reverse[childType] = uniqueSortedStrings(parentList)
	}

	return reverse
}

// GetAllowedChildTypes returns the configured child types for a given parent type.
func (c *GroupConfig) GetAllowedChildTypes(parentType string) []string {
	effectiveTree := c.EffectiveTree()
	childTypes := effectiveTree[parentType]
	return append([]string{}, childTypes...)
}

// CanHaveChildType reports whether parentType is allowed to contain childType.
func (c *GroupConfig) CanHaveChildType(parentType, childType string) bool {
	if c == nil {
		return false
	}

	if !c.IsValidType(parentType) || !c.IsValidType(childType) {
		return false
	}

	for _, allowedChildType := range c.GetAllowedChildTypes(parentType) {
		if allowedChildType == childType {
			return true
		}
	}

	return false
}

// GetIndependentHierarchyTrees returns the root types of each independent hierarchy tree.
// Root types are those that appear as parents in the tree but never appear as children,
// meaning they have no parent types and represent the top of their respective hierarchies.
func (c *GroupConfig) GetIndependentHierarchyTrees() []string {
	if c == nil || len(c.Tree) == 0 {
		return []string{}
	}

	// Track all types that appear as children
	childTypes := make(map[string]struct{})
	for _, children := range c.Tree {
		for _, childType := range children {
			childTypes[childType] = struct{}{}
		}
	}

	// Find parent types that are never children (the roots)
	var roots []string
	for parentType := range c.Tree {
		if _, isChild := childTypes[parentType]; !isChild {
			roots = append(roots, parentType)
		}
	}

	// Sort for consistent ordering
	sort.Strings(roots)
	return roots
}

// uniqueStrings returns input values with duplicates removed while preserving
// the order of first appearance.
func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}

	return unique
}

// uniqueSortedStrings removes duplicate values and returns the result sorted
// lexicographically in ascending order.
func uniqueSortedStrings(values []string) []string {
	unique := uniqueStrings(values)
	sort.Strings(unique)
	return unique
}
