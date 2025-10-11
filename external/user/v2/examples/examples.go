package examples

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/user"
	userV2 "github.com/ooaklee/ghatd/external/user/v2"
)

// Example 1: Basic usage with defaults
func ExampleBasicUsage() {
	// Create factory with default configuration
	factory := userV2.NewUserFactory(nil) // Uses DefaultUserConfig()

	// Create a new user
	user := factory.CreateUser("john.doe@example.com")

	// Add some roles
	user.AddRole("USER")
	user.AddRole("READER")

	// Update status
	if updatedUser, err := user.UpdateStatus("ACTIVE"); err != nil {
		log.Printf("Error updating status: %v", err)
	} else {
		fmt.Printf("User status updated to: %s\n", updatedUser.Status)
	}

	// Verify email
	user.VerifyEmail()

	// Check if user has role
	if user.HasRole("ADMIN") {
		fmt.Println("User is an admin")
	}
}

// Example 2: Web application configuration
func ExampleWebAppUsage() {
	// Use web app specific configuration
	config := userV2.WebAppUserConfig()
	factory := userV2.NewUserFactory(config)

	// Create user with personal info
	user := factory.CreateUserWithPersonalInfo(
		"jane.smith@example.com",
		"jane",
		"smith",
	)

	// Verify it meets requirements
	if err := user.Validate(); err != nil {
		log.Printf("Validation failed: %v", err)
		return
	}

	// Add extension data
	user.SetExtension("department", "Engineering")
	user.SetExtension("hire_date", "2025-01-15")

	// Set custom timestamp
	user.SetCustomTimestamp("onboarded_at")

	fmt.Printf("Created user: %s %s (%s)\n",
		user.PersonalInfo.FirstName,
		user.PersonalInfo.LastName,
		user.Email)
}

// Example 3: API service configuration
func ExampleAPIServiceUsage() {
	config := userV2.APIServiceUserConfig()
	factory := userV2.NewUserFactory(config)

	// Create service user
	user := factory.CreateUser("api-service@company.com")
	user.AddRole("SERVICE")

	// Service users start active (no email verification needed)
	if updatedUser, err := user.UpdateStatus("ACTIVE"); err != nil {
		log.Printf("Error: %v", err)
	} else {
		fmt.Printf("Service user created: %s\n", updatedUser.ID)
	}
}

// Example 4: Custom dependency injection
func ExampleCustomDependencies() {
	// Custom implementations
	customIDGen := &CustomIDGenerator{}
	customTime := &CustomTimeProvider{}
	customStrings := &CustomStringUtils{}

	config := userV2.DefaultUserConfig()
	factory := userV2.NewUserFactoryWithDependencies(
		config,
		customIDGen,
		customTime,
		customStrings,
	)

	user := factory.CreateUser("custom@example.com")
	fmt.Printf("User with custom dependencies: %s\n", user.ID)
}

// Example 5: Migration from existing User model
func ExampleMigration() {
	// Existing user from your current model
	existingUser := &user.User{
		ID:        "existing-uuid",
		NanoId:    "existing-nano",
		FirstName: "John",
		LastName:  "Doe",
		Email:     "john@example.com",
		Status:    "ACTIVE",
		Roles:     []string{"ADMIN"},
		Verified: user.UserVerifcationStatus{
			EmailVerified:   true,
			EmailVerifiedAt: "2025-01-01T00:00:00Z",
		},
		Meta: user.UserMeta{
			CreatedAt:   "2025-01-01T00:00:00Z",
			UpdatedAt:   "2025-01-02T00:00:00Z",
			ActivatedAt: "2025-01-01T12:00:00Z",
		},
	}

	// Convert to universal user
	factory := userV2.NewUserFactory(userV2.DefaultUserConfig())
	universalUser := userV2.MigrateFromLegacyUser(existingUser, factory)

	// Now you can use new features
	universalUser.SetExtension("migrated_from", "legacy_system")
	universalUser.SetCustomTimestamp("migration_completed_at")

	fmt.Printf("Migrated user: %s\n", universalUser.ID)

	// Convert back to legacy format if needed (for gradual migration)
	backToLegacy := userV2.MigrateToLegacyUser(universalUser)
	fmt.Printf("Back to legacy: %s\n", backToLegacy.ID)
}

// Example 6: Working with extensions
func ExampleExtensions() {
	factory := userV2.NewUserFactory(nil)
	user := factory.CreateUser("extensible@example.com")

	// Add various extension data
	user.SetExtension("profile_picture", "https://example.com/avatar.jpg")
	user.SetExtension("preferences", map[string]interface{}{
		"theme":         "dark",
		"language":      "en",
		"timezone":      "UTC",
		"notifications": true,
	})
	user.SetExtension("subscription", map[string]interface{}{
		"plan":       "premium",
		"expires_at": "2025-12-31T23:59:59Z",
	})

	// Retrieve extension data
	if preferences, exists := user.GetExtension("preferences"); exists {
		fmt.Printf("User preferences: %+v\n", preferences)
	}

	// Add custom timestamps
	user.SetCustomTimestamp("last_profile_update")
	user.SetCustomTimestamp("subscription_renewed_at")
}

// Example 7: Testing with mock dependencies
func ExampleTesting() {
	// Mock implementations for testing
	mockIDGen := &MockIDGenerator{fixedUUID: "test-uuid-123"}
	mockTime := &MockTimeProvider{fixedTime: "2025-01-01T00:00:00Z"}
	mockStrings := &MockStringUtils{}

	config := userV2.DefaultUserConfig()
	factory := userV2.NewUserFactoryWithDependencies(
		config,
		mockIDGen,
		mockTime,
		mockStrings,
	)

	user := factory.CreateUser("test@example.com")

	// Predictable values for testing
	fmt.Printf("Test user ID: %s\n", user.ID)               // Will be "test-uuid-123"
	fmt.Printf("Created at: %s\n", user.Metadata.CreatedAt) // Will be "2025-01-01T00:00:00Z"
}

// Custom implementations for example 4

type CustomIDGenerator struct{}

func (g *CustomIDGenerator) GenerateUUID() string {
	return "custom-uuid-format"
}

func (g *CustomIDGenerator) GenerateNanoID() string {
	return "custom-nano-id"
}

type CustomTimeProvider struct{}

func (t *CustomTimeProvider) Now() time.Time {
	// Custom time logic
	return time.Now()
}

func (t *CustomTimeProvider) NowUTC() string {
	// Custom format
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

type CustomStringUtils struct{}

func (s *CustomStringUtils) ToTitleCase(str string) string {
	// Custom title case logic
	return strings.ToUpper(str[:1]) + strings.ToLower(str[1:])
}

func (s *CustomStringUtils) ToLowerCase(str string) string {
	return strings.ToLower(str)
}

func (s *CustomStringUtils) ToUpperCase(str string) string {
	return strings.ToUpper(str)
}

func (s *CustomStringUtils) InSlice(item string, slice []string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Mock implementations for testing (example 7)

type MockIDGenerator struct {
	fixedUUID string
}

func (m *MockIDGenerator) GenerateUUID() string {
	return m.fixedUUID
}

func (m *MockIDGenerator) GenerateNanoID() string {
	return "mock-nano-id"
}

type MockTimeProvider struct {
	fixedTime string
}

func (m *MockTimeProvider) Now() time.Time {
	t, _ := time.Parse(time.RFC3339, m.fixedTime)
	return t
}

func (m *MockTimeProvider) NowUTC() string {
	return m.fixedTime
}

type MockStringUtils struct{}

func (m *MockStringUtils) ToTitleCase(str string) string { return str }
func (m *MockStringUtils) ToLowerCase(str string) string { return str }
func (m *MockStringUtils) ToUpperCase(str string) string { return str }
func (m *MockStringUtils) InSlice(item string, slice []string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
