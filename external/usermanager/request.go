package usermanager

// GetUserMicroProfileRequest holds all the data needed to action request
type GetUserMicroProfileRequest struct {

	// UserID the ID of the user requesting their micro profile
	UserID string
}

// GetUserProfileRequest holds all the data needed to action user
// profile retrival request
type GetUserProfileRequest struct {

	// UserID the ID of the user requesting their profile
	UserID string
}

// UpdateUserProfileRequest holds all the data needed to action user
// profile update request
type UpdateUserProfileRequest struct {

	// UserID the ID of the user requesting their profile
	UserID string

	// FirstName the new name to assign to the first name attribute
	FirstName string `json:"first_name"`

	// LastNmae the new name to assign to the last name attribute
	LastName string `json:"last_name"`
}

// DeleteUserPermanentlyRequest holds all the data needed to delete user and resources
type DeleteUserPermanentlyRequest struct {

	// UserId the Id of the user requesting the deletion
	UserId string
}

// GetUserInsightsUsageRequest holds all the data needed to get basic user insights
type GetUserInsightsUsageRequest struct {
	// UserId the ID of the user requesting basic insights
	UserId string

	// From the date from when the queries should be run between
	From string

	// To the date up to when the queries should be run between
	To string
}
