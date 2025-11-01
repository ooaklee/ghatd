package group

// DefaultGroupConfig returns a default configuration
func DefaultGroupConfig() *GroupConfig {
	return &GroupConfig{
		DefaultStatus: GroupStatusActive,
		StatusTransitions: map[string][]string{
			GroupStatusActive: {
				GroupStatusProvisioned,
				GroupStatusInactive,
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
