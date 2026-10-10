package examples

import (
	"fmt"
	"log"
	"strings"
	"time"

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

// Example 5: Working with extensions
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

// Example 6: Testing with mock dependencies
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

// CustomIDGenerator is an example ID generator showing how a host injects its
// own identifier formats into the example configuration.
type CustomIDGenerator struct{}

// GenerateUUID returns the example's fixed placeholder UUID string,
// illustrating the injection point rather than a real generator.
func (g *CustomIDGenerator) GenerateUUID() string {
	return "custom-uuid-format"
}

// GenerateNanoID returns the example's fixed placeholder nano ID string,
// illustrating the injection point rather than a real generator.
func (g *CustomIDGenerator) GenerateNanoID() string {
	return "custom-nano-id"
}

// CustomTimeProvider is an example clock showing how a host can supply its own
// time source to the example configuration.
type CustomTimeProvider struct{}

// Now returns the current wall-clock time; the example marks the spot where
// hosts would apply custom time logic.
func (t *CustomTimeProvider) Now() time.Time {
	// Custom time logic
	return time.Now()
}

// NowUTC returns the current time formatted as a UTC millisecond-precision
// RFC3339 string; the example marks the spot for custom formatting.
func (t *CustomTimeProvider) NowUTC() string {
	// Custom format
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// CustomStringUtils is an example string-utility implementation showing how a
// host injects its own transformations into the example configuration.
type CustomStringUtils struct{}

// ToTitleCase upper-cases the first character and lower-cases the remainder; it
// illustrates a custom implementation of the model's string utility contract.
func (s *CustomStringUtils) ToTitleCase(str string) string {
	// Custom title case logic
	return strings.ToUpper(str[:1]) + strings.ToLower(str[1:])
}

// ToLowerCase lower-cases the input, illustrating the example's custom string
// utility contract.
func (s *CustomStringUtils) ToLowerCase(str string) string {
	return strings.ToLower(str)
}

// ToUpperCase upper-cases the input, illustrating the example's custom string
// utility contract.
func (s *CustomStringUtils) ToUpperCase(str string) string {
	return strings.ToUpper(str)
}

// InSlice reports whether item equals any element of slice, illustrating the
// example's custom string utility contract.
func (s *CustomStringUtils) InSlice(item string, slice []string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Mock implementations for testing (example 6)

// MockIDGenerator is an example generator that always returns the same
// identifiers, making example output deterministic.
type MockIDGenerator struct {
	fixedUUID string
}

// GenerateUUID returns the fixed UUID supplied to the example mock.
func (m *MockIDGenerator) GenerateUUID() string {
	return m.fixedUUID
}

// GenerateNanoID returns the example mock's constant nano ID.
func (m *MockIDGenerator) GenerateNanoID() string {
	return "mock-nano-id"
}

// MockTimeProvider is an example clock frozen at a fixed RFC3339 timestamp for
// deterministic example output.
type MockTimeProvider struct {
	fixedTime string
}

// Now returns the mock's fixed time parsed as RFC3339; parse failures yield the
// zero time.
func (m *MockTimeProvider) Now() time.Time {
	t, _ := time.Parse(time.RFC3339, m.fixedTime)
	return t
}

// NowUTC returns the mock's fixed timestamp string verbatim.
func (m *MockTimeProvider) NowUTC() string {
	return m.fixedTime
}

// MockStringUtils is an example pass-through string utility that performs no
// transformation.
type MockStringUtils struct{}

// ToTitleCase returns the input unchanged, demonstrating a no-op example
// implementation.
func (m *MockStringUtils) ToTitleCase(str string) string { return str }

// ToLowerCase returns the input unchanged, demonstrating a no-op example
// implementation.
func (m *MockStringUtils) ToLowerCase(str string) string { return str }

// ToUpperCase returns the input unchanged, demonstrating a no-op example
// implementation.
func (m *MockStringUtils) ToUpperCase(str string) string { return str }

// InSlice reports whether item equals any element of slice, mirroring the
// utility contract without transformation.
func (m *MockStringUtils) InSlice(item string, slice []string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
