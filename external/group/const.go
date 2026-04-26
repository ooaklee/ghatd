package group

// DefaultRoles is the default set of roles that can be assigned to members when a group type does not have specific roles defined in the configuration.
var DefaultRoles = []string{"MEMBER", "ADMIN"}

const (
	// Group Type Keys
	GroupTypeTeam         = "TEAM"
	GroupTypeTribe        = "TRIBE"
	GroupTypeSquad        = "SQUAD"
	GroupTypeDepartment   = "DEPARTMENT"
	GroupTypeOrganisation = "ORGANISATION"
	GroupTypeCompany      = "COMPANY"
	GroupTypeProject      = "PROJECT"
	GroupTypeCommunity    = "COMMUNITY"
	GroupTypeFamily       = "FAMILY"
	GroupTypeFriends      = "FRIENDS"
	GroupTypeCustom       = "CUSTOM"

	// Member Type Keys
	MemberTypeUser  = "USER"
	MemberTypeGroup = "GROUP"

	// Group Status Keys
	GroupStatusActive      = "ACTIVE"
	GroupStatusInactive    = "INACTIVE"
	GroupStatusArchived    = "ARCHIVED"
	GroupStatusSuspended   = "SUSPENDED"
	GroupStatusProvisioned = "PROVISIONED"

	// Membership Role Keys
	MemberRoleOwner       = "OWNER"
	MemberRoleAdmin       = "ADMIN"
	MemberRoleMember      = "MEMBER"
	MemberRoleModerator   = "MODERATOR"
	MemberRoleGuest       = "GUEST"
	MemberRoleHead        = "HEAD"
	MemberRoleLead        = "LEAD"
	MemberRoleCoordinator = "COORDINATOR"

	// Visibility Keys
	VisibilityPublic   = "PUBLIC"
	VisibilityPrivate  = "PRIVATE"
	VisibilityInternal = "INTERNAL"
)

const (
	// Error Keys
	ErrKeyGroupConfigNotSet            = "GroupConfigNotSet"
	ErrKeyInvalidGroupType             = "InvalidGroupType"
	ErrKeyInvalidGroupStatus           = "InvalidGroupStatus"
	ErrKeyInvalidStatusTransition      = "InvalidStatusTransition"
	ErrKeyRequiredFieldMissingName     = "RequiredFieldMissingName"
	ErrKeyRequiredFieldMissingType     = "RequiredFieldMissingType"
	ErrKeyValidationFailed             = "GroupValidationFailed"
	ErrKeyResourceNotFound             = "GroupResourceNotFound"
	ErrKeyResourceConflict             = "GroupResourceConflict"
	ErrKeyDatabaseError                = "GroupDatabaseError"
	ErrKeyInvalidQueryParam            = "GroupInvalidQueryParam"
	ErrKeyNoChangesDetected            = "GroupNoChangesDetected"
	ErrKeyInvalidMemberType            = "InvalidMemberType"
	ErrKeyInvalidMemberRole            = "InvalidMemberRole"
	ErrKeyMemberNotFound               = "MemberNotFound"
	ErrKeyMemberAlreadyExists          = "MemberAlreadyExists"
	ErrKeyInsufficientPermissions      = "InsufficientPermissions"
	ErrKeyCircularReferenceDetected    = "CircularReferenceDetected"
	ErrKeyMaxDepthExceeded             = "MaxDepthExceeded"
	ErrKeyInvalidNanoID                = "GroupInvalidNanoID"
	ErrKeyNameAlreadyExists            = "GroupNameAlreadyExists"
	ErrKeyUnableToFindGroupWithName    = "UnableToFindGroupWithGivenName"
	ErrKeyInvalidGroupID               = "GroupInvalidID"
	ErrKeyInvalidGroupBody             = "GroupInvalidBody"
	ErrKeyInvalidMemberID              = "GroupInvalidMemberID"
	ErrKeyInvalidGroupHierarchyTree    = "InvalidGroupHierarchyTree"
	ErrKeyInvalidParentChildRelation   = "InvalidParentChildRelation"
	ErrKeyGroupDependedOnByOtherGroups = "GroupDependedOnByOtherGroups"
)

const (
	// Group Order Constants
	GetGroupOrderCreatedAtDesc   = "created_at_desc"
	GetGroupOrderCreatedAtAsc    = "created_at_asc"
	GetGroupOrderUpdatedAtDesc   = "updated_at_desc"
	GetGroupOrderUpdatedAtAsc    = "updated_at_asc"
	GetGroupOrderNameAsc         = "name_asc"
	GetGroupOrderNameDesc        = "name_desc"
	GetGroupOrderMemberCountDesc = "member_count_desc"
	GetGroupOrderMemberCountAsc  = "member_count_asc"
)
