package user

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/ooaklee/ghatd/external/user"
)

// Default implementations of the interfaces for immediate use

// DefaultTimeFormatRFC3339NanoUTC is the time format we use for all
// our date time. It will be used for parsing our UTC date time.
const DefaultTimeFormatRFC3339NanoUTC string = "2006-01-02T15:04:05.999999999"

// DefaultIDGenerator provides default ID generation
type DefaultIDGenerator struct{}

// GenerateUUID generates a new UUID v4
func (g *DefaultIDGenerator) GenerateUUID() string {
	return uuid.New().String()
}

// GenerateNanoID generates a simple nano ID (can be enhanced with proper nano ID library)
func (g *DefaultIDGenerator) GenerateNanoID() string {

	// nolint no need to check error as not returned
	id, err := gonanoid.New()
	if err == nil {
		return id
	}

	// Fallback to basic random string if nanoid generation fails
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const length = 21

	bytes := make([]byte, length)
	rand.Read(bytes)

	for i, b := range bytes {
		bytes[i] = charset[b%byte(len(charset))]
	}

	return string(bytes)
}

// DefaultTimeProvider provides default time operations
type DefaultTimeProvider struct{}

// Now returns current time
func (t *DefaultTimeProvider) Now() time.Time {
	return time.Now()
}

// NowUTC returns current UTC time as ISO string
func (t *DefaultTimeProvider) NowUTC() string {
	return time.Now().UTC().Format(DefaultTimeFormatRFC3339NanoUTC)
}

// DefaultStringUtils provides default string utilities
type DefaultStringUtils struct{}

// ToTitleCase converts string to title case
func (s *DefaultStringUtils) ToTitleCase(str string) string {
	return strings.Title(strings.ToLower(str))
}

// ToLowerCase converts string to lowercase
func (s *DefaultStringUtils) ToLowerCase(str string) string {
	return strings.ToLower(str)
}

// ToUpperCase converts string to uppercase
func (s *DefaultStringUtils) ToUpperCase(str string) string {
	return strings.ToUpper(str)
}

// InSlice checks if string exists in slice
func (s *DefaultStringUtils) InSlice(item string, slice []string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// UserFactory provides convenient user creation
type UserFactory struct {
	config       *UserConfig
	idGenerator  IDGenerator
	timeProvider TimeProvider
	stringUtils  StringUtils
}

// NewUserFactory creates a new user factory with default implementations
func NewUserFactory(config *UserConfig) *UserFactory {
	if config == nil {
		config = DefaultUserConfig()
	}

	return &UserFactory{
		config:       config,
		idGenerator:  &DefaultIDGenerator{},
		timeProvider: &DefaultTimeProvider{},
		stringUtils:  &DefaultStringUtils{},
	}
}

// NewUserFactoryWithDependencies creates a factory with custom implementations
func NewUserFactoryWithDependencies(
	config *UserConfig,
	idGenerator IDGenerator,
	timeProvider TimeProvider,
	stringUtils StringUtils,
) *UserFactory {
	if config == nil {
		config = DefaultUserConfig()
	}

	return &UserFactory{
		config:       config,
		idGenerator:  idGenerator,
		timeProvider: timeProvider,
		stringUtils:  stringUtils,
	}
}

// CreateUser creates a new user with initial setup
func (f *UserFactory) CreateUser(email string) *UniversalUser {
	user := NewUniversalUser(f.config, f.idGenerator, f.timeProvider, f.stringUtils)

	user.Email = f.stringUtils.ToLowerCase(email)
	user.GenerateNewUUID()

	if f.config.MultipleIdentifiers {
		user.GenerateNewNanoID()
	}

	user.SetInitialState()

	return user
}

// CreateUserWithPersonalInfo creates a user with personal information
func (f *UserFactory) CreateUserWithPersonalInfo(email, firstName, lastName string) *UniversalUser {
	user := f.CreateUser(email)

	user.PersonalInfo.FirstName = f.stringUtils.ToTitleCase(firstName)
	user.PersonalInfo.LastName = f.stringUtils.ToTitleCase(lastName)
	user.PersonalInfo.FullName = fmt.Sprintf("%s %s",
		user.PersonalInfo.FirstName,
		user.PersonalInfo.LastName)

	return user
}

// LoadExistingUser loads an existing user and sets up dependencies
func (f *UserFactory) LoadExistingUser(user *UniversalUser) *UniversalUser {
	return user.SetDependencies(f.config, f.idGenerator, f.timeProvider, f.stringUtils)
}

// Migration helpers for existing projects

// MigrateFromLegacyUser converts your existing User to UniversalUser
func MigrateFromLegacyUser(legacyUser *user.User, factory *UserFactory) *UniversalUser {
	user := factory.CreateUser(legacyUser.Email)

	// Copy core fields
	user.ID = legacyUser.ID
	user.NanoID = legacyUser.NanoId
	user.Status = legacyUser.Status
	user.Roles = legacyUser.Roles

	// Copy personal info
	if legacyUser.FirstName != "" || legacyUser.LastName != "" {
		user.PersonalInfo.FirstName = legacyUser.FirstName
		user.PersonalInfo.LastName = legacyUser.LastName
		user.PersonalInfo.FullName = fmt.Sprintf("%s %s", legacyUser.FirstName, legacyUser.LastName)
	}

	// Copy verification status
	user.Verification.EmailVerified = legacyUser.Verified.EmailVerified
	user.Verification.EmailVerifiedAt = legacyUser.Verified.EmailVerifiedAt

	// Copy metadata
	user.Metadata.CreatedAt = legacyUser.Meta.CreatedAt
	user.Metadata.UpdatedAt = legacyUser.Meta.UpdatedAt
	user.Metadata.LastLoginAt = legacyUser.Meta.LastLoginAt
	user.Metadata.ActivatedAt = legacyUser.Meta.ActivatedAt
	user.Metadata.StatusChangedAt = legacyUser.Meta.StatusChangedAt
	user.Metadata.LastFreshLoginAt = legacyUser.Meta.LastFreshLoginAt

	return user
}

// MigrateToLegacyUser converts UniversalUser back to your existing User (for gradual migration)
func MigrateToLegacyUser(universalUser *UniversalUser) *user.User {
	legacyUser := &user.User{
		ID:     universalUser.ID,
		NanoId: universalUser.NanoID,
		Email:  universalUser.Email,
		Status: universalUser.Status,
		Roles:  universalUser.Roles,
	}

	// Copy personal info
	if universalUser.PersonalInfo != nil {
		legacyUser.FirstName = universalUser.PersonalInfo.FirstName
		legacyUser.LastName = universalUser.PersonalInfo.LastName
	}

	// Copy verification
	if universalUser.Verification != nil {
		legacyUser.Verified = user.UserVerifcationStatus{
			EmailVerified:   universalUser.Verification.EmailVerified,
			EmailVerifiedAt: universalUser.Verification.EmailVerifiedAt,
		}
	}

	// Copy metadata
	if universalUser.Metadata != nil {
		legacyUser.Meta = user.UserMeta{
			CreatedAt:        universalUser.Metadata.CreatedAt,
			UpdatedAt:        universalUser.Metadata.UpdatedAt,
			LastLoginAt:      universalUser.Metadata.LastLoginAt,
			ActivatedAt:      universalUser.Metadata.ActivatedAt,
			StatusChangedAt:  universalUser.Metadata.StatusChangedAt,
			LastFreshLoginAt: universalUser.Metadata.LastFreshLoginAt,
		}
	}

	return legacyUser
}

// Project-specific configurations

// WebAppUserConfig returns configuration suitable for web applications
func WebAppUserConfig() *UserConfig {
	return &UserConfig{
		DefaultStatus: "PROVISIONED",
		StatusTransitions: map[string][]string{
			"ACTIVE":      {"PROVISIONED"},
			"SUSPENDED":   {"ACTIVE"},
			"DEACTIVATED": {"ACTIVE", "SUSPENDED"},
			"REACTIVATE":  {"DEACTIVATED"},
			"UNSUSPEND":   {"SUSPENDED"},
		},
		RequiredFields:            []string{"email", "first_name", "last_name"},
		ValidRoles:                []string{"ADMIN", "USER", "MODERATOR"},
		EmailVerificationRequired: true,
		MultipleIdentifiers:       false,
	}
}

// APIServiceUserConfig returns configuration suitable for API services
func APIServiceUserConfig() *UserConfig {
	return &UserConfig{
		DefaultStatus: "ACTIVE",
		StatusTransitions: map[string][]string{
			"ACTIVE":    {"PROVISIONED"},
			"SUSPENDED": {"ACTIVE"},
			"DISABLED":  {"ACTIVE", "SUSPENDED"},
		},
		RequiredFields:            []string{"email"},
		ValidRoles:                []string{"SERVICE", "CLIENT", "ADMIN"},
		EmailVerificationRequired: false,
		MultipleIdentifiers:       true,
	}
}

// MicroserviceUserConfig returns minimal configuration for microservices
func MicroserviceUserConfig() *UserConfig {
	return &UserConfig{
		DefaultStatus: "ACTIVE",
		StatusTransitions: map[string][]string{
			"ACTIVE":   {},
			"INACTIVE": {"ACTIVE"},
		},
		RequiredFields:            []string{"email"},
		ValidRoles:                []string{}, // Allow any roles
		EmailVerificationRequired: false,
		MultipleIdentifiers:       true,
	}
}
