package group

// GroupFactory provides convenient methods for creating groups
type GroupFactory struct {
	Config       *GroupConfig
	IDGenerator  IDGenerator
	TimeProvider TimeProvider
	StringUtils  StringUtils
}

// NewGroupFactory creates a new group factory
func NewGroupFactory(
	config *GroupConfig,
	idGenerator IDGenerator,
	timeProvider TimeProvider,
	stringUtils StringUtils,
) *GroupFactory {
	if config == nil {
		config = DefaultGroupConfig()
	}
	if idGenerator == nil {
		idGenerator = NewDefaultIDGenerator()
	}
	if timeProvider == nil {
		timeProvider = NewDefaultTimeProvider()
	}
	if stringUtils == nil {
		stringUtils = NewDefaultStringUtils()
	}

	return &GroupFactory{
		Config:       config,
		IDGenerator:  idGenerator,
		TimeProvider: timeProvider,
		StringUtils:  stringUtils,
	}
}

// CreateGroup creates a new group with basic information
func (f *GroupFactory) CreateGroup(name, groupType string) *UniversalGroup {
	group := NewUniversalGroup(f.Config, f.IDGenerator, f.TimeProvider, f.StringUtils)
	group.Name = name
	group.Type = groupType
	group.SetInitialState()
	return group
}

// CreateTeam creates a new team group
func (f *GroupFactory) CreateTeam(name string) *UniversalGroup {
	return f.CreateGroup(name, GroupTypeTeam)
}

// CreateDepartment creates a new department group
func (f *GroupFactory) CreateDepartment(name string) *UniversalGroup {
	return f.CreateGroup(name, GroupTypeDepartment)
}

// CreateOrganization creates a new organization group
func (f *GroupFactory) CreateOrganization(name string) *UniversalGroup {
	return f.CreateGroup(name, GroupTypeOrganisation)
}

// CreateProject creates a new project group
func (f *GroupFactory) CreateProject(name string) *UniversalGroup {
	return f.CreateGroup(name, GroupTypeProject)
}

// CreateCommunity creates a new community group
func (f *GroupFactory) CreateCommunity(name string) *UniversalGroup {
	return f.CreateGroup(name, GroupTypeCommunity)
}

// CreateGroupWithMembers creates a group and adds initial members
func (f *GroupFactory) CreateGroupWithMembers(name, groupType string, memberIDs []string, memberType string) *UniversalGroup {
	group := f.CreateGroup(name, groupType)
	for _, memberID := range memberIDs {
		group.AddMember(memberID, memberType, MemberRoleMember)
	}
	return group
}

// CreateGroupWithOwner creates a group with an owner
func (f *GroupFactory) CreateGroupWithOwner(name, groupType, ownerID string) *UniversalGroup {
	group := f.CreateGroup(name, groupType)
	group.Leadership.OwnerID = ownerID
	group.AddMember(ownerID, MemberTypeUser, MemberRoleOwner)
	return group
}

// CreateHierarchicalGroup creates a parent-child group relationship
func (f *GroupFactory) CreateHierarchicalGroup(parentName, parentType, childName, childType string) (*UniversalGroup, *UniversalGroup) {
	parent := f.CreateGroup(parentName, parentType)
	child := f.CreateGroup(childName, childType)
	parent.AddMember(child.ID, MemberTypeGroup, MemberRoleMember)
	return parent, child
}

// ReinjectDependencies reinjects dependencies into an existing group
func (f *GroupFactory) ReinjectDependencies(group *UniversalGroup) *UniversalGroup {
	return group.SetDependencies(f.Config, f.IDGenerator, f.TimeProvider, f.StringUtils)
}

// ReinjectDependenciesMultiple reinjects dependencies into multiple groups
func (f *GroupFactory) ReinjectDependenciesMultiple(groups []*UniversalGroup) []*UniversalGroup {
	for _, group := range groups {
		group.SetDependencies(f.Config, f.IDGenerator, f.TimeProvider, f.StringUtils)
	}
	return groups
}
