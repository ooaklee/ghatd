package group

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/PaesslerAG/jsonpath"
)

// IDGenerator generates unique identifiers
type IDGenerator interface {
	GenerateUUID() string
	GenerateNanoID() string
}

// TimeProvider provides current time (useful for testing)
type TimeProvider interface {
	Now() time.Time
	NowUTC() string
}

// StringUtils provides string manipulation utilities
type StringUtils interface {
	ToTitleCase(s string) string
	ToLowerCase(s string) string
	ToUpperCase(s string) string
	InSlice(item string, slice []string) bool
}

// GroupConfig holds configuration for group behavior
type GroupConfig struct {
	DefaultStatus       string
	StatusTransitions   map[string][]string
	Tree                map[string][]string
	RequiredFields      []string
	ValidTypes          []string
	ValidMemberTypes    []string
	AllowNestedGroups   bool
	MaxNestingDepth     int
	MultipleIdentifiers bool // Support both UUID and NanoID
	// Role configuration: maps group type to allowed roles
	TypeToRoleOverrides map[string][]string // e.g., "TEAM" -> ["ADMIN", "MEMBER", "READER"]
	DefaultRoles        []string            // Fallback roles when group type not in TypeToRoleOverrides
}

// UniversalGroup represents a flexible group/collection model
type UniversalGroup struct {
	// Core required fields
	ID            string `json:"id" bson:"_id" db:"id"`
	Name          string `json:"name" bson:"name" db:"name"`
	RawName       string `json:"raw_name" bson:"raw_name" db:"raw_name"`
	Type          string `json:"type" bson:"type" db:"type"`
	Status        string `json:"status" bson:"status" db:"status"`
	ParentGroupID string `json:"parent_group_id,omitempty" bson:"parent_group_id,omitempty" db:"parent_group_id"`
	OwnerID       string `json:"owner_id" bson:"owner_id" db:"owner_id"`

	// Lineage field for efficient hierarchical queries (root-first, e.g. ["rootID", "parentID"])
	Lineage []string `json:"lineage,omitempty" bson:"lineage,omitempty" db:"lineage"`

	// Version field for tracking model version
	Version int `json:"-" bson:"version" db:"version"`

	// Optional identifier (for systems that need multiple ID types)
	NanoID string `json:"nano_id,omitempty" bson:"_nano_id,omitempty" db:"nano_id"`

	// Display information
	DisplayInfo *DisplayInfo `json:"display_info,omitempty" bson:"display_info,omitempty" db:"display_info"`

	// Members collection - supports both users and nested groups
	Members []Member `json:"members,omitempty" bson:"members,omitempty" db:"members"`

	// Settings and permissions
	Settings *GroupSettings `json:"settings,omitempty" bson:"settings,omitempty" db:"settings"`

	// Integration points
	Integrations *Integrations `json:"integrations,omitempty" bson:"integrations,omitempty" db:"integrations"`

	// Metadata with flexible timestamps
	Metadata *GroupMetadata `json:"metadata" bson:"metadata" db:"metadata"`

	// Extension point for project-specific fields
	Extensions map[string]interface{} `json:"extensions,omitempty" bson:"extensions,omitempty" db:"extensions"`

	// Injected dependencies
	config       *GroupConfig `json:"-" bson:"-" db:"-"`
	idGenerator  IDGenerator  `json:"-" bson:"-" db:"-"`
	timeProvider TimeProvider `json:"-" bson:"-" db:"-"`
	stringUtils  StringUtils  `json:"-" bson:"-" db:"-"`
}

// DisplayInfo holds display-related information
type DisplayInfo struct {
	Description string   `json:"description,omitempty" bson:"description,omitempty" db:"description"`
	NameAliases []string `json:"name_aliases,omitempty" bson:"name_aliases,omitempty" db:"name_aliases"`
	Icon        string   `json:"icon,omitempty" bson:"icon,omitempty" db:"icon"`
	Avatar      string   `json:"avatar,omitempty" bson:"avatar,omitempty" db:"avatar"`
	Color       string   `json:"color,omitempty" bson:"color,omitempty" db:"color"`
	Email       string   `json:"email,omitempty" bson:"email,omitempty" db:"email"`
	Website     string   `json:"website,omitempty" bson:"website,omitempty" db:"website"`
}

// Member represents a member (user or group) of the group
type Member struct {
	ID       string                 `json:"id" bson:"id" db:"id"`
	Type     string                 `json:"type" bson:"type" db:"type"` // USER or GROUP
	Role     string                 `json:"role,omitempty" bson:"role,omitempty" db:"role"`
	JoinedAt string                 `json:"joined_at,omitempty" bson:"joined_at,omitempty" db:"joined_at"`
	Metadata map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty" db:"metadata"`
}

// GroupSettings holds group configuration
type GroupSettings struct {
	Visibility         string   `json:"visibility,omitempty" bson:"visibility,omitempty" db:"visibility"` // PUBLIC, PRIVATE, INTERNAL
	AllowMemberInvites bool     `json:"allow_member_invites,omitempty" bson:"allow_member_invites,omitempty" db:"allow_member_invites"`
	RequireApproval    bool     `json:"require_approval,omitempty" bson:"require_approval,omitempty" db:"require_approval"`
	MaxMembers         int      `json:"max_members,omitempty" bson:"max_members,omitempty" db:"max_members"`
	AllowedMemberTypes []string `json:"allowed_member_types,omitempty" bson:"allowed_member_types,omitempty" db:"allowed_member_types"`
}

// Integrations holds external integration data
type Integrations struct {
	Slack  *SlackIntegration      `json:"slack,omitempty" bson:"slack,omitempty" db:"slack"`
	Custom map[string]interface{} `json:"custom,omitempty" bson:"custom,omitempty" db:"custom"`
}

// SlackIntegration holds Slack-specific data
type SlackIntegration struct {
	DisplayID            string   `json:"display_id,omitempty" bson:"display_id,omitempty" db:"display_id"`
	OnDutyDisplayID      string   `json:"on_duty_display_id,omitempty" bson:"on_duty_display_id,omitempty" db:"on_duty_display_id"`
	OnDutyDisplayName    string   `json:"on_duty_display_name,omitempty" bson:"on_duty_display_name,omitempty" db:"on_duty_display_name"`
	Emoji                string   `json:"emoji,omitempty" bson:"emoji,omitempty" db:"emoji"`
	AdditionalRecipients []string `json:"additional_recipients,omitempty" bson:"additional_recipients,omitempty" db:"additional_recipients"`
}

// GroupMetadata holds timestamp and audit information
type GroupMetadata struct {
	CreatedAt   string `json:"created_at" bson:"created_at" db:"created_at"`
	UpdatedAt   string `json:"updated_at,omitempty" bson:"updated_at,omitempty" db:"updated_at"`
	ArchivedAt  string `json:"archived_at,omitempty" bson:"archived_at,omitempty" db:"archived_at"`
	DeletedAt   string `json:"deleted_at,omitempty" bson:"deleted_at,omitempty" db:"deleted_at"`
	DeletedByID string `json:"deleted_by_id,omitempty" bson:"deleted_by_id,omitempty" db:"deleted_by_id"`

	// Extensible metadata
	CustomTimestamps map[string]string `json:"custom_timestamps,omitempty" bson:"custom_timestamps,omitempty" db:"custom_timestamps"`
}

// NewUniversalGroup creates a new group with injected dependencies
func NewUniversalGroup(
	config *GroupConfig,
	idGenerator IDGenerator,
	timeProvider TimeProvider,
	stringUtils StringUtils,
) *UniversalGroup {
	if config == nil {
		config = DefaultGroupConfig()
	}

	return &UniversalGroup{
		Members:      []Member{},
		Extensions:   make(map[string]interface{}),
		DisplayInfo:  &DisplayInfo{},
		Settings:     &GroupSettings{},
		Integrations: &Integrations{},
		Metadata: &GroupMetadata{
			CustomTimestamps: make(map[string]string),
		},
		config:       config,
		idGenerator:  idGenerator,
		timeProvider: timeProvider,
		stringUtils:  stringUtils,
	}
}

// SetDependencies allows setting dependencies after creation (useful for existing records)
func (g *UniversalGroup) SetDependencies(
	config *GroupConfig,
	idGenerator IDGenerator,
	timeProvider TimeProvider,
	stringUtils StringUtils,
) *UniversalGroup {
	if config == nil {
		config = DefaultGroupConfig()
	}

	g.config = config
	g.idGenerator = idGenerator
	g.timeProvider = timeProvider
	g.stringUtils = stringUtils

	// Initialize nil fields if needed
	if g.Extensions == nil {
		g.Extensions = make(map[string]interface{})
	}
	if g.DisplayInfo == nil {
		g.DisplayInfo = &DisplayInfo{}
	}
	if g.Settings == nil {
		g.Settings = &GroupSettings{}
	}
	if g.Integrations == nil {
		g.Integrations = &Integrations{}
	}
	if g.Metadata == nil {
		g.Metadata = &GroupMetadata{
			CustomTimestamps: make(map[string]string),
		}
	}
	if g.Metadata.CustomTimestamps == nil {
		g.Metadata.CustomTimestamps = make(map[string]string)
	}

	return g
}

// Core ID Methods

// GenerateNewUUID creates a new UUID for the group
func (g *UniversalGroup) GenerateNewUUID() *UniversalGroup {
	if g.idGenerator != nil {
		g.ID = g.idGenerator.GenerateUUID()
	}
	return g
}

// GenerateNewNanoID creates a new nano ID for the group
func (g *UniversalGroup) GenerateNewNanoID() *UniversalGroup {
	if g.idGenerator != nil && g.config.MultipleIdentifiers {
		g.NanoID = g.idGenerator.GenerateNanoID()
	}
	return g
}

// SetInitialState sets up a new group with default values
func (g *UniversalGroup) SetInitialState() *UniversalGroup {
	g.Status = g.config.DefaultStatus
	g.Version = 1 // Mark as v1 model
	g.SetCreatedAtNow()
	return g
}

// SetVersion sets the model version
func (g *UniversalGroup) SetVersion(version int) *UniversalGroup {
	g.Version = version
	return g
}

// EnsureVersion ensures the group has version 1 set
func (g *UniversalGroup) EnsureVersion() *UniversalGroup {
	if g.Version != 1 {
		g.Version = 1
	}
	return g
}

// Timestamp Management

// SetCreatedAtNow sets created timestamp to current time
func (g *UniversalGroup) SetCreatedAtNow() *UniversalGroup {
	if g.timeProvider != nil {
		g.Metadata.CreatedAt = g.timeProvider.NowUTC()
	}
	return g
}

// SetUpdatedAtNow sets updated timestamp to current time
func (g *UniversalGroup) SetUpdatedAtNow() *UniversalGroup {
	if g.timeProvider != nil {
		g.Metadata.UpdatedAt = g.timeProvider.NowUTC()
	}
	return g
}

// SetArchivedAtNow sets archived timestamp to current time
func (g *UniversalGroup) SetArchivedAtNow() *UniversalGroup {
	if g.timeProvider != nil {
		g.Metadata.ArchivedAt = g.timeProvider.NowUTC()
	}
	return g
}

// SetDeletedAtNow sets deleted timestamp to current time
func (g *UniversalGroup) SetDeletedAtNow(deletedByID string) *UniversalGroup {
	if g.timeProvider != nil {
		g.Metadata.DeletedAt = g.timeProvider.NowUTC()
		g.Metadata.DeletedByID = deletedByID
	}
	return g
}

// SetCustomTimestamp sets a custom timestamp field
func (g *UniversalGroup) SetCustomTimestamp(key string) *UniversalGroup {
	if g.timeProvider != nil {
		g.Metadata.CustomTimestamps[key] = g.timeProvider.NowUTC()
	}
	return g
}

// Status Management

// UpdateStatus updates group status with validation
func (g *UniversalGroup) UpdateStatus(desiredStatus string) (*UniversalGroup, error) {
	if g.config == nil {
		return g, errors.New(ErrKeyGroupConfigNotSet)
	}

	// Check if transition is valid
	validSources, exists := g.config.StatusTransitions[desiredStatus]
	if !exists {
		return g, errors.New(ErrKeyInvalidGroupStatus)
	}

	// Check if current status allows transition
	if g.stringUtils != nil && !g.stringUtils.InSlice(g.Status, validSources) {
		return g, errors.New(ErrKeyInvalidStatusTransition)
	}

	// Update status
	g.Status = desiredStatus
	g.SetUpdatedAtNow()

	// Handle special status logic
	if desiredStatus == GroupStatusArchived {
		g.SetArchivedAtNow()
	}

	return g, nil
}

// IsValidStatus checks if a status is valid
func (g *UniversalGroup) IsValidStatus(status string) bool {
	if g.config == nil {
		return false
	}
	_, exists := g.config.StatusTransitions[status]
	return exists || status == g.config.DefaultStatus
}

// Member Management

// AddMember adds a member to the group
func (g *UniversalGroup) AddMember(memberID, memberType, role string) (*UniversalGroup, error) {
	memberID = strings.TrimSpace(memberID)
	if memberID == "" {
		return g, errors.New(ErrKeyInvalidMemberID)
	}

	// Validate member type
	if !g.isValidMemberType(memberType) {
		return g, errors.New(ErrKeyInvalidMemberType)
	}

	// Check if member already exists
	if g.HasMember(memberID) {
		return g, errors.New(ErrKeyMemberAlreadyExists)
	}

	// Check max members limit
	if g.Settings != nil && g.Settings.MaxMembers > 0 && len(g.Members) >= g.Settings.MaxMembers {
		return g, errors.New("MaxMembersReached")
	}

	member := Member{
		ID:   memberID,
		Type: memberType,
		Role: role,
	}

	if g.timeProvider != nil {
		member.JoinedAt = g.timeProvider.NowUTC()
	}

	g.Members = append(g.Members, member)
	g.SetUpdatedAtNow()

	return g, nil
}

// RemoveMember removes a member from the group
func (g *UniversalGroup) RemoveMember(memberID string) (*UniversalGroup, error) {
	for i, member := range g.Members {
		if member.ID == memberID {
			g.Members = append(g.Members[:i], g.Members[i+1:]...)
			g.SetUpdatedAtNow()
			return g, nil
		}
	}
	return g, errors.New(ErrKeyMemberNotFound)
}

// HasMember checks if a member exists in the group
func (g *UniversalGroup) HasMember(memberID string) bool {
	for _, member := range g.Members {
		if member.ID == memberID {
			return true
		}
	}
	return false
}

// GetMemberByID retrieves a member by ID
func (g *UniversalGroup) GetMemberByID(memberID string) (*Member, error) {
	for _, member := range g.Members {
		if member.ID == memberID {
			return &member, nil
		}
	}
	return nil, errors.New(ErrKeyMemberNotFound)
}

// GetMembersByType retrieves all members of a specific type
func (g *UniversalGroup) GetMembersByType(memberType string) []Member {
	var members []Member
	for _, member := range g.Members {
		if member.Type == memberType {
			members = append(members, member)
		}
	}
	return members
}

// GetMemberIDsByType retrieves all member IDs of a specific type
func (g *UniversalGroup) GetMemberIDsByType(memberType string) []string {
	var ids []string
	for _, member := range g.Members {
		if member.Type == memberType {
			ids = append(ids, member.ID)
		}
	}
	return ids
}

// GetMemberTypeByID retrieves the member type by ID
func (g *UniversalGroup) GetMemberTypeByID(memberID string) (string, error) {
	for _, member := range g.Members {
		if member.ID == memberID {

			if memberID == g.OwnerID {
				return MemberRoleOwner, nil
			}

			return member.Type, nil
		}
	}
	return "", errors.New(ErrKeyMemberNotFound)
}

// GetUserMemberIDs retrieves all user member IDs
func (g *UniversalGroup) GetUserMemberIDs() []string {
	return g.GetMemberIDsByType(MemberTypeUser)
}

// GetGroupMemberIDs retrieves all group member IDs (nested groups)
func (g *UniversalGroup) GetGroupMemberIDs() []string {
	return g.GetMemberIDsByType(MemberTypeGroup)
}

// UpdateMemberRole updates a member's role
func (g *UniversalGroup) UpdateMemberRole(memberID, newRole string) (*UniversalGroup, error) {
	for i, member := range g.Members {
		if member.ID == memberID {
			g.Members[i].Role = newRole
			g.SetUpdatedAtNow()
			return g, nil
		}
	}
	return g, errors.New(ErrKeyMemberNotFound)
}

// GetMemberCount returns the total number of members
func (g *UniversalGroup) GetMemberCount() int {
	return len(g.Members)
}

// isValidMemberType checks if member type is valid
func (g *UniversalGroup) isValidMemberType(memberType string) bool {
	if g.config == nil || len(g.config.ValidMemberTypes) == 0 {
		return true // Allow any if not configured
	}

	if g.stringUtils != nil {
		return g.stringUtils.InSlice(memberType, g.config.ValidMemberTypes)
	}

	for _, validType := range g.config.ValidMemberTypes {
		if validType == memberType {
			return true
		}
	}
	return false
}

// Extension Management

// SetExtension sets a custom extension field
func (g *UniversalGroup) SetExtension(key string, value interface{}) *UniversalGroup {
	g.Extensions[key] = value
	g.SetUpdatedAtNow()
	return g
}

// GetExtension retrieves a custom extension field
func (g *UniversalGroup) GetExtension(key string) (interface{}, bool) {
	value, exists := g.Extensions[key]
	return value, exists
}

// Validation

// Validate checks if group meets configured requirements
func (g *UniversalGroup) Validate() error {
	if g.config == nil {
		return errors.New(ErrKeyGroupConfigNotSet)
	}

	// Check required fields
	for _, field := range g.config.RequiredFields {
		switch field {
		case "name":
			if g.Name == "" {
				return errors.New(ErrKeyRequiredFieldMissingName)
			}
		case "type":
			if g.Type == "" {
				return errors.New(ErrKeyRequiredFieldMissingType)
			}
		}
	}

	// Validate type
	if !g.isValidType(g.Type) {
		return errors.New(ErrKeyInvalidGroupType)
	}

	// Validate status
	if !g.IsValidStatus(g.Status) {
		return errors.New(ErrKeyInvalidGroupStatus)
	}

	return nil
}

// isValidType checks if group type is valid
func (g *UniversalGroup) isValidType(groupType string) bool {
	if g.config == nil {
		return true
	}

	return g.config.IsValidType(groupType)
}

// Utility Methods

// GetAttributeByJSONPath retrieves nested field values using JSON path
func (g *UniversalGroup) GetAttributeByJSONPath(jsonPath string) (interface{}, error) {
	jsonData, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}

	var dataMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &dataMap); err != nil {
		return nil, err
	}

	result, err := jsonpath.Get(jsonPath, dataMap)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// BuildChildLineage returns the lineage slice for a direct child of this group.
// Ordering is root-first and ends at this group's ID.
func (g *UniversalGroup) BuildChildLineage() []string {
	childLineage := make([]string, 0, len(g.Lineage)+1)
	childLineage = append(childLineage, g.Lineage...)
	if g.ID != "" {
		childLineage = append(childLineage, g.ID)
	}

	return childLineage
}
