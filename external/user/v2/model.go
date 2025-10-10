package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PaesslerAG/jsonpath"
	userX "github.com/ooaklee/ghatd/external/user/x"
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

// UserConfig holds configuration for user behavior
type UserConfig struct {
	DefaultStatus             string
	StatusTransitions         map[string][]string
	RequiredFields            []string
	ValidRoles                []string
	EmailVerificationRequired bool
	MultipleIdentifiers       bool // Support both UUID and NanoID
}

// DefaultUserConfig returns a sensible default configuration
func DefaultUserConfig() *UserConfig {
	return &UserConfig{
		DefaultStatus: "PROVISIONED",
		StatusTransitions: map[string][]string{
			"ACTIVE":      {"PROVISIONED"},
			"DEACTIVATED": {"PROVISIONED", "ACTIVE", "LOCKED_OUT", "RECOVERY", "SUSPENDED"},
			"SUSPENDED":   {"ACTIVE"},
			"LOCKED_OUT":  {"ACTIVE"},
			"RECOVERY":    {"ACTIVE"},
		},
		RequiredFields:            []string{"email"},
		ValidRoles:                []string{"ADMIN", "USER"},
		EmailVerificationRequired: true,
		MultipleIdentifiers:       true,
	}
}

// UniversalUser represents a flexible user model
type UniversalUser struct {
	// Core required fields
	ID     string `json:"id" bson:"_id" db:"id"`
	Email  string `json:"email" bson:"email" db:"email"`
	Status string `json:"status" bson:"status" db:"status"`

	// Version field for tracking model version (stored internally only)
	Version int `json:"-" bson:"version" db:"version"`

	// Optional identifier (for systems that need multiple ID types)
	NanoID string `json:"nano_id,omitempty" bson:"_nano_id,omitempty" db:"nano_id"`

	// Optional personal information
	PersonalInfo *PersonalInfo `json:"personal_info,omitempty" bson:"personal_info,omitempty" db:"personal_info"`

	// Flexible roles system
	Roles []string `json:"roles" bson:"roles" db:"roles"`

	// Verification status
	Verification *VerificationStatus `json:"verification,omitempty" bson:"verification,omitempty" db:"verification"`

	// Metadata with flexible timestamps
	Metadata *UserMetadata `json:"metadata" bson:"metadata" db:"metadata"`

	// Extension point for project-specific fields
	Extensions map[string]interface{} `json:"extensions,omitempty" bson:"extensions,omitempty" db:"extensions"`

	// Injected dependencies
	config       *UserConfig  `json:"-" bson:"-" db:"-"`
	idGenerator  IDGenerator  `json:"-" bson:"-" db:"-"`
	timeProvider TimeProvider `json:"-" bson:"-" db:"-"`
	stringUtils  StringUtils  `json:"-" bson:"-" db:"-"`
}

// PersonalInfo holds optional personal information
type PersonalInfo struct {
	FirstName string `json:"first_name,omitempty" bson:"first_name,omitempty" db:"first_name"`
	LastName  string `json:"last_name,omitempty" bson:"last_name,omitempty" db:"last_name"`
	FullName  string `json:"full_name,omitempty" bson:"full_name,omitempty" db:"full_name"`
	// Add other personal fields as needed
	Avatar string `json:"avatar,omitempty" bson:"avatar,omitempty" db:"avatar"`
	Phone  string `json:"phone,omitempty" bson:"phone,omitempty" db:"phone"`
}

// VerificationStatus holds verification information
type VerificationStatus struct {
	EmailVerified   bool   `json:"email_verified" bson:"email_verified" db:"email_verified"`
	EmailVerifiedAt string `json:"email_verified_at,omitempty" bson:"email_verified_at,omitempty" db:"email_verified_at"`
	PhoneVerified   bool   `json:"phone_verified,omitempty" bson:"phone_verified,omitempty" db:"phone_verified"`
	PhoneVerifiedAt string `json:"phone_verified_at,omitempty" bson:"phone_verified_at,omitempty" db:"phone_verified_at"`
}

// UserMetadata holds flexible timestamp information
type UserMetadata struct {
	CreatedAt        string `json:"created_at" bson:"created_at" db:"created_at"`
	UpdatedAt        string `json:"updated_at,omitempty" bson:"updated_at,omitempty" db:"updated_at"`
	LastLoginAt      string `json:"last_login_at,omitempty" bson:"last_login_at,omitempty" db:"last_login_at"`
	ActivatedAt      string `json:"activated_at,omitempty" bson:"activated_at,omitempty" db:"activated_at"`
	StatusChangedAt  string `json:"status_changed_at,omitempty" bson:"status_changed_at,omitempty" db:"status_changed_at"`
	LastFreshLoginAt string `json:"last_fresh_login_at,omitempty" bson:"last_fresh_login_at,omitempty" db:"last_fresh_login_at"`

	// Extensible metadata
	CustomTimestamps map[string]string `json:"custom_timestamps,omitempty" bson:"custom_timestamps,omitempty" db:"custom_timestamps"`
}

// NewUniversalUser creates a new user with injected dependencies
func NewUniversalUser(
	config *UserConfig,
	idGenerator IDGenerator,
	timeProvider TimeProvider,
	stringUtils StringUtils,
) *UniversalUser {
	if config == nil {
		config = DefaultUserConfig()
	}

	return &UniversalUser{
		Roles:        []string{},
		Extensions:   make(map[string]interface{}),
		PersonalInfo: &PersonalInfo{},
		Verification: &VerificationStatus{},
		Metadata: &UserMetadata{
			CustomTimestamps: make(map[string]string),
		},
		config:       config,
		idGenerator:  idGenerator,
		timeProvider: timeProvider,
		stringUtils:  stringUtils,
	}
}

// SetDependencies allows setting dependencies after creation (useful for existing records)
func (u *UniversalUser) SetDependencies(
	config *UserConfig,
	idGenerator IDGenerator,
	timeProvider TimeProvider,
	stringUtils StringUtils,
) *UniversalUser {
	if config == nil {
		config = DefaultUserConfig()
	}

	u.config = config
	u.idGenerator = idGenerator
	u.timeProvider = timeProvider
	u.stringUtils = stringUtils

	// Initialise nil fields if needed
	if u.Extensions == nil {
		u.Extensions = make(map[string]interface{})
	}
	if u.PersonalInfo == nil {
		u.PersonalInfo = &PersonalInfo{}
	}
	if u.Verification == nil {
		u.Verification = &VerificationStatus{}
	}
	if u.Metadata == nil {
		u.Metadata = &UserMetadata{
			CustomTimestamps: make(map[string]string),
		}
	}
	if u.Metadata.CustomTimestamps == nil {
		u.Metadata.CustomTimestamps = make(map[string]string)
	}

	return u
}

// Core Methods

// GenerateNewUUID creates a new UUID for the user
func (u *UniversalUser) GenerateNewUUID() *UniversalUser {
	if u.idGenerator != nil {
		u.ID = u.idGenerator.GenerateUUID()
	}
	return u
}

// GenerateNewNanoID creates a new nano ID for the user
func (u *UniversalUser) GenerateNewNanoID() *UniversalUser {
	if u.idGenerator != nil && u.config.MultipleIdentifiers {
		u.NanoID = u.idGenerator.GenerateNanoID()
	}
	return u
}

// SetInitialState sets up a new user with default values
func (u *UniversalUser) SetInitialState() *UniversalUser {
	u.Status = u.config.DefaultStatus
	u.Version = 2 // Mark as v2 model
	u.SetCreatedAtNow()
	return u
}

// SetVersion sets the model version (used for migration tracking)
func (u *UniversalUser) SetVersion(version int) *UniversalUser {
	u.Version = version
	return u
}

// EnsureVersion ensures the user has version 2 set
func (u *UniversalUser) EnsureVersion() *UniversalUser {
	if u.Version != 2 {
		u.Version = 2
	}
	return u
}

// Timestamp Management

// SetCreatedAtNow sets created timestamp to current time
func (u *UniversalUser) SetCreatedAtNow() *UniversalUser {
	if u.timeProvider != nil {
		u.Metadata.CreatedAt = u.timeProvider.NowUTC()
	}
	return u
}

// SetUpdatedAtNow sets updated timestamp to current time
func (u *UniversalUser) SetUpdatedAtNow() *UniversalUser {
	if u.timeProvider != nil {
		u.Metadata.UpdatedAt = u.timeProvider.NowUTC()
	}
	return u
}

// SetLastLoginAtNow sets last login timestamp to current time
func (u *UniversalUser) SetLastLoginAtNow() *UniversalUser {
	if u.timeProvider != nil {
		u.Metadata.LastLoginAt = u.timeProvider.NowUTC()
	}
	return u
}

// SetActivatedAtNow sets activated timestamp to current time
func (u *UniversalUser) SetActivatedAtNow() *UniversalUser {
	if u.timeProvider != nil {
		u.Metadata.ActivatedAt = u.timeProvider.NowUTC()
	}
	return u
}

// SetStatusChangedAtNow sets status changed timestamp to current time
func (u *UniversalUser) SetStatusChangedAtNow() *UniversalUser {
	if u.timeProvider != nil {
		u.Metadata.StatusChangedAt = u.timeProvider.NowUTC()
	}
	return u
}

// SetCustomTimestamp sets a custom timestamp field
func (u *UniversalUser) SetCustomTimestamp(key string) *UniversalUser {
	if u.timeProvider != nil {
		u.Metadata.CustomTimestamps[key] = u.timeProvider.NowUTC()
	}
	return u
}

// Status Management

// UpdateStatus updates user status with validation
func (u *UniversalUser) UpdateStatus(desiredStatus string) (*UniversalUser, error) {
	if u.config == nil {
		fmt.Println("user-configuration-not-set")
		return u, errors.New(ErrKeyUserConfigNotSet)
	}

	// Check if transition is valid
	validSources, exists := u.config.StatusTransitions[desiredStatus]
	if !exists {
		fmt.Printf("invalid-target-status: %s\n", desiredStatus)
		return u, errors.New(ErrKeyUserInvalidTargetStatus)
	}

	// Check if current status allows transition
	if u.stringUtils != nil && !u.stringUtils.InSlice(u.Status, validSources) {
		fmt.Printf("cannot-transition-from-%s-to-%s\n", u.Status, desiredStatus)
		return u, errors.New(ErrKeyUserInvalidStatusTransition)
	}

	// Update status
	u.Status = desiredStatus
	u.SetUpdatedAtNow()
	u.SetStatusChangedAtNow()

	// Handle special status logic
	if desiredStatus == "ACTIVE" {
		u.SetActivatedAtNow()
	}

	return u, nil
}

// IsValidStatus checks if a status is valid
func (u *UniversalUser) IsValidStatus(status string) bool {
	if u.config == nil {
		return false
	}
	_, exists := u.config.StatusTransitions[status]
	return exists || status == u.config.DefaultStatus
}

// Role Management

// HasRole checks if user has a specific role
func (u *UniversalUser) HasRole(role string) bool {
	if u.stringUtils != nil {
		return u.stringUtils.InSlice(role, u.Roles)
	}
	// Fallback implementation
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// AddRole adds a role to the user
func (u *UniversalUser) AddRole(role string) *UniversalUser {
	if !u.HasRole(role) && u.isValidRole(role) {
		u.Roles = append(u.Roles, role)
		u.SetUpdatedAtNow()
	}
	return u
}

// RemoveRole removes a role from the user
func (u *UniversalUser) RemoveRole(role string) *UniversalUser {
	for i, r := range u.Roles {
		if r == role {
			u.Roles = append(u.Roles[:i], u.Roles[i+1:]...)
			u.SetUpdatedAtNow()
			break
		}
	}
	return u
}

// isValidRole checks if role is in valid roles list
func (u *UniversalUser) isValidRole(role string) bool {
	if u.config == nil || len(u.config.ValidRoles) == 0 {
		return true // Allow any role if not configured
	}

	if u.stringUtils != nil {
		return u.stringUtils.InSlice(role, u.config.ValidRoles)
	}

	// Fallback
	for _, validRole := range u.config.ValidRoles {
		if validRole == role {
			return true
		}
	}
	return false
}

// Verification Management

// VerifyEmail marks email as verified
func (u *UniversalUser) VerifyEmail() *UniversalUser {
	u.Verification.EmailVerified = true
	if u.timeProvider != nil {
		u.Verification.EmailVerifiedAt = u.timeProvider.NowUTC()
	}
	u.SetUpdatedAtNow()
	return u
}

// UnverifyEmail marks email as unverified
func (u *UniversalUser) UnverifyEmail() *UniversalUser {
	u.Verification.EmailVerified = false
	u.Verification.EmailVerifiedAt = ""
	u.SetUpdatedAtNow()
	return u
}

// VerifyPhone marks phone as verified
func (u *UniversalUser) VerifyPhone() *UniversalUser {
	u.Verification.PhoneVerified = true
	if u.timeProvider != nil {
		u.Verification.PhoneVerifiedAt = u.timeProvider.NowUTC()
	}
	u.SetUpdatedAtNow()
	return u
}

// Extension Management

// SetExtension sets a custom extension field
func (u *UniversalUser) SetExtension(key string, value interface{}) *UniversalUser {
	u.Extensions[key] = value
	u.SetUpdatedAtNow()
	return u
}

// GetExtension retrieves a custom extension field
func (u *UniversalUser) GetExtension(key string) (interface{}, bool) {
	value, exists := u.Extensions[key]
	return value, exists
}

// Validation

// Validate checks if user meets configured requirements
func (u *UniversalUser) Validate() error {
	if u.config == nil {
		fmt.Println("user-configuration-not-set")
		return errors.New(ErrKeyUserConfigNotSet)
	}

	// Check required fields
	for _, field := range u.config.RequiredFields {
		switch field {
		case "email":
			if u.Email == "" {
				fmt.Printf("required-field-missing: %s\n", field)
				return errors.New(ErrKeyUserRequiredFieldMissingEmail)
			}
		case "first_name":
			if u.PersonalInfo == nil || u.PersonalInfo.FirstName == "" {
				fmt.Printf("required-field-missing: %s\n", field)
				return errors.New(ErrKeyUserRequiredFieldMissingFirstName)
			}
		case "last_name":
			if u.PersonalInfo == nil || u.PersonalInfo.LastName == "" {
				fmt.Printf("required-field-missing: %s\n", field)
				return errors.New(ErrKeyUserRequiredFieldMissingLastName)
			}
		}
	}

	// Validate status
	if !u.IsValidStatus(u.Status) {
		fmt.Printf("invalid-status: %s\n", u.Status)
		return errors.New(ErrKeyUserInvalidStatus)
	}

	// Validate roles
	for _, role := range u.Roles {
		if !u.isValidRole(role) {
			fmt.Printf("invalid-role: %s\n", role)
			return errors.New(ErrKeyUserInvalidRole)
		}
	}

	return nil
}

// Utility Methods

// GetAttributeByJSONPath retrieves nested field values using JSON path
func (u *UniversalUser) GetAttributeByJSONPath(jsonPath string) (interface{}, error) {
	// Convert struct to map for path traversal
	jsonData, err := json.Marshal(u)
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

// Profile Generation (backward compatibility)

// GetAsProfile returns a profile representation
func (u *UniversalUser) GetAsProfile() *userX.UserProfile {
	profile := &userX.UserProfile{
		ID:     u.ID,
		Status: u.Status,
		Roles:  u.Roles,
		Email:  u.Email,
	}

	if u.PersonalInfo != nil {
		profile.FirstName = u.PersonalInfo.FirstName
		profile.LastName = u.PersonalInfo.LastName
	}

	if u.Verification != nil {
		profile.EmailVerified = u.Verification.EmailVerified
	}

	if u.Metadata != nil {
		profile.UpdatedAt = u.Metadata.UpdatedAt
	}

	return profile
}

// GetAsMicroProfile returns a minimal profile representation
func (u *UniversalUser) GetAsMicroProfile() *userX.UserMicroProfile {
	return &userX.UserMicroProfile{
		ID:     u.ID,
		Roles:  u.Roles,
		Status: u.Status,
	}
}

// GetUserEmail returns the user's email
func (u *UniversalUser) GetUserEmail() string {
	return u.Email
}

// GetUserID implements UserModel interface (standardized method name)
func (u *UniversalUser) GetUserID() string {
	return u.ID
}

// GetUserRoles implements UserModel interface
func (u *UniversalUser) GetUserRoles() []string {
	return u.Roles
}

// GetModelVersion implements UserModel interface
// Returns 2 for v2 UniversalUser model
func (u *UniversalUser) GetModelVersion() int {
	return 2
}

// Legacy method aliases for backward compatibility
func (u *UniversalUser) GetUserId() string     { return u.ID }
func (u *UniversalUser) GetUserStatus() string { return u.Status }
func (u *UniversalUser) IsAdmin() bool         { return u.HasRole("ADMIN") }
