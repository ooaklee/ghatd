package user

// CreateUserRequest holds everything needed to create user on platform
type CreateUserRequest struct {
	// FirstName user's first name
	FirstName string `json:"first_name" validate:"min=2"`

	// LastName user's last / family/ sur name
	LastName string `json:"last_name" validate:"min=2"`

	// Email user's email address that will be used for receiving
	// correspondence & signing into platform
	Email string `json:"email" `
}

// GetUsersRequest holds all the data needed to action request
type GetUsersRequest struct {
	// Order defines how should response be sorted. Default: newest -> oldest (created_at_desc)
	// Valid options: created_at_asc, created_at_desc, last_login_at_asc, last_login_at_desc
	// activated_at_asc, activated_at_desc, status_changed_at_asc, status_changed_at_desc,
	// last_fresh_login_at_asc, last_fresh_login_at_desc,
	// email_verified_at_asc, email_verified_at_desc
	Order string

	// Total number of users to return per page, if available. Default 25.
	// Accepts anything between 1 and 100
	PerPage int `validate:"min=1,max=100"`

	// Page specifies the page results should be taken from. Default 1.
	Page int

	// TotalCount specifies the total count of all users
	TotalCount int

	// Meta whether response should contain meta information
	Meta bool

	// FirstName specifies the first name results should be like / match
	FirstName string

	// LastName specifies the last name results should be like / match
	LastName string

	// String specified the statuses users in response must be
	// Valid options: provisioned, active, deactivated, locked_out, recovery, suspended
	Status string

	// IsAdmin  whether response should contain only admin users information
	IsAdmin bool

	// Email specifies the email the user should match
	Email string
}

// GetUserByIDRequest holds everything needed for GetUserByID request
type GetUserByIDRequest struct {
	// ID the user's UUID
	ID string
}

// UpdateUserRequest holds everything needed to update user on platform
type UpdateUserRequest struct {
	// ID the user's UUID
	ID string

	// FirstName specifies the value to change the first name to
	FirstName string `json:"first_name"`

	// LastName specifies the value to change  the last name to
	LastName string `json:"last_name"`

	// User if specified updates entire user object
	User *User
}

// DeleteUserRequest holds everything needed for DeleteUser request
type DeleteUserRequest struct {
	// ID the user's UUID
	ID string
}

// GetUserByEmailRequest holds everything needed for GetUserByEmail request
type GetUserByEmailRequest struct {
	// Email the user's registered email address
	Email string `json:"email" `
}

// GetMicroProfileRequest holds everything needed for GetMicroProfile request
type GetMicroProfileRequest struct {
	// ID the user's UUID
	ID string
}

// GetProfileRequest holds everything needed for GetProfile request
type GetProfileRequest struct {
	// ID the user's UUID
	ID string
}
