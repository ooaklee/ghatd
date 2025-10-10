package x

import (
	"errors"
)

// UserMicroProfile holds user's micro metadata
type UserMicroProfile struct {
	ID     string   `json:"id"`
	Roles  []string `json:"roles"`
	Status string   `json:"status"`
}

// UserProfile holds user's profile metadata
type UserProfile struct {
	ID            string   `json:"id"`
	FirstName     string   `json:"first_name"`
	LastName      string   `json:"last_name"`
	Status        string   `json:"status"`
	Roles         []string `json:"roles"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified" `
	UpdatedAt     string   `json:"updated_at,omitempty"`
}

// UserModel represents a version-agnostic user interface that all user model versions must implement
// This allows for easier migration between versions and loose coupling between packages
type UserModel interface {
	// Core identification
	GetUserID() string
	GetUserEmail() string
	GetUserStatus() string
	GetUserRoles() []string

	// Profile information
	GetAsProfile() *UserProfile
	GetAsMicroProfile() *UserMicroProfile

	// Version tracking
	GetModelVersion() int

	// Role checking
	HasRole(role string) bool
	IsAdmin() bool

	// Validation
	Validate() error
}

// UserModelCollection represents a collection of users (version-agnostic)
type UserModelCollection interface {
	GetUsers() []UserModel
	Count() int
	GetModelVersion() int
}

// Versioned constants for model versions
const (
	UserModelVersionUnknown = 0
	UserModelVersionV1      = 1
	UserModelVersionV2      = 2
)

var (
	// ErrUnsupportedUserModelVersion returned when model version is not supported
	ErrUnsupportedUserModelVersion = errors.New("unsupported user model version")

	// ErrInvalidUserModel returned when user model is invalid or nil
	ErrInvalidUserModel = errors.New("invalid user model")
)

// GetUserModelVersion extracts the version number from any UserModel
func GetUserModelVersion(model UserModel) int {
	if model == nil {
		return UserModelVersionUnknown
	}
	return model.GetModelVersion()
}

// UserModelSlice wraps a slice of UserModel for easier collection handling
type UserModelSlice []UserModel

// GetUsers returns the underlying slice
func (s UserModelSlice) GetUsers() []UserModel {
	return s
}

// Count returns the number of users in the collection
func (s UserModelSlice) Count() int {
	return len(s)
}

// GetModelVersion returns the version of the first user in the collection
// Returns UserModelVersionUnknown if collection is empty
func (s UserModelSlice) GetModelVersion() int {
	if len(s) == 0 {
		return UserModelVersionUnknown
	}
	return s[0].GetModelVersion()
}

// FilterByRole returns users that have the specified role
func (s UserModelSlice) FilterByRole(role string) UserModelSlice {
	var filtered UserModelSlice
	for _, user := range s {
		if user.HasRole(role) {
			filtered = append(filtered, user)
		}
	}
	return filtered
}

// FilterByStatus returns users that have the specified status
func (s UserModelSlice) FilterByStatus(status string) UserModelSlice {
	var filtered UserModelSlice
	for _, user := range s {
		if user.GetUserStatus() == status {
			filtered = append(filtered, user)
		}
	}
	return filtered
}

// GetUsersByVersion separates a mixed collection of users by their model version
func GetUsersByVersion(models UserModelSlice) map[int]UserModelSlice {
	result := make(map[int]UserModelSlice)

	for _, model := range models {
		version := model.GetModelVersion()
		result[version] = append(result[version], model)
	}

	return result
}

// ValidateUserModels validates all users in a collection
// Returns map of user ID to validation error
func ValidateUserModels(models UserModelSlice) map[string]error {
	errors := make(map[string]error)

	for _, model := range models {
		if err := model.Validate(); err != nil {
			errors[model.GetUserID()] = err
		}
	}

	return errors
}
