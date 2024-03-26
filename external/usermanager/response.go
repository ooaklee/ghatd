package usermanager

import "github.com/ooaklee/ghatd/external/user"

// GetUserMicroProfileResponse holds response data for GetUserMicroProfile request
type GetUserMicroProfileResponse struct {

	// UserMicroProfile holds micro profile for requestor
	UserMicroProfile *user.UserMicroProfile
}

// GetUserProfileResponse holds response data for GetUserProfile request
type GetUserProfileResponse struct {

	// UserProfile holds the profile for the requestor
	UserProfile *user.UserProfile
}

// UpdateUserProfileResponse holds response data for UpdateUserProfile request
type UpdateUserProfileResponse struct {

	// UserProfile holds the updated profile for the requestor
	UserProfile *user.UserProfile
}
