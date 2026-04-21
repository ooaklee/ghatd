package user

// CreateUserRequest holds data for creating a new user
type CreateUserRequest struct {
	Email          string                 `json:"email"`
	FirstName      string                 `json:"first_name,omitempty"`
	LastName       string                 `json:"last_name,omitempty"`
	FullName       string                 `json:"full_name,omitempty"`
	Avatar         string                 `json:"avatar,omitempty"`
	Phone          string                 `json:"phone,omitempty"`
	Roles          []string               `json:"roles,omitempty"`
	Status         string                 `json:"status,omitempty"`
	Extensions     map[string]interface{} `json:"extensions,omitempty"`
	GenerateUUID   bool                   `json:"generate_uuid,omitempty"`
	GenerateNanoID bool                   `json:"generate_nano_id,omitempty"`
}

// UpdateUserRequest holds data for updating an existing user
type UpdateUserRequest struct {
	ID         string                 `json:"id"`
	Email      string                 `json:"email,omitempty"`
	FirstName  string                 `json:"first_name,omitempty"`
	LastName   string                 `json:"last_name,omitempty"`
	FullName   string                 `json:"full_name,omitempty"`
	Avatar     string                 `json:"avatar,omitempty"`
	Phone      string                 `json:"phone,omitempty"`
	Status     string                 `json:"status,omitempty"`
	Extensions map[string]interface{} `json:"extensions,omitempty"`

	User *UniversalUser `json:"-"`
}

// GetUserByIDRequest holds data for retrieving a user by ID
type GetUserByIDRequest struct {
	ID string
}

// GetUserByNanoIDRequest holds data for retrieving a user by nano ID
type GetUserByNanoIDRequest struct {
	NanoID string
}

// GetUserByEmailRequest holds data for retrieving a user by email
type GetUserByEmailRequest struct {
	Email string
}

// DeleteUserRequest holds data for deleting a user
type DeleteUserRequest struct {
	ID string
}

// GetUsersRequest holds filters and pagination for retrieving users
type GetUsersRequest struct {
	// Pagination
	Page    int `query:"page"`
	PerPage int `query:"per_page"`

	// Sorting
	Order string `query:"order"`

	// Metadata flag
	IncludeMeta bool `query:"meta"`

	// Filters
	EmailFilter     string      `query:"with_email"`
	FirstNameFilter string      `query:"with_first_name"`
	LastNameFilter  string      `query:"with_last_name"`
	StatusFilter    string      `query:"with_status"`
	RoleFilter      string      `query:"with_role"`
	RolesFilter     []string    `query:"with_roles"`
	OnlyAdmin       bool        `query:"only_admin"`
	EmailVerified   *bool       `query:"email_verified"`
	PhoneVerified   *bool       `query:"phone_verified"`
	ExtensionKey    string      `query:"with_extension_key"`
	ExtensionValue  interface{} `query:"with_extension_value"`
}

// GetTotalUsersRequest holds filters for counting total users
type GetTotalUsersRequest struct {
	EmailFilter     string      `query:"with_email"`
	FirstNameFilter string      `query:"with_first_name"`
	LastNameFilter  string      `query:"with_last_name"`
	StatusFilter    string      `query:"with_status"`
	RoleFilter      string      `query:"with_role"`
	RolesFilter     []string    `query:"with_roles"`
	OnlyAdmin       bool        `query:"only_admin"`
	EmailVerified   *bool       `query:"email_verified"`
	PhoneVerified   *bool       `query:"phone_verified"`
	ExtensionKey    string      `query:"with_extension_key"`
	ExtensionValue  interface{} `query:"with_extension_value"`
}

// UpdateUserStatusRequest holds data for updating user status
type UpdateUserStatusRequest struct {
	ID            string `json:"id"`
	DesiredStatus string `json:"desired_status"`
}

// AddUserRoleRequest holds data for adding a role to a user
type AddUserRoleRequest struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

// RemoveUserRoleRequest holds data for removing a role from a user
type RemoveUserRoleRequest struct {
	ID   string
	Role string `json:"role"`
}

// VerifyUserEmailRequest holds data for verifying a user's email
type VerifyUserEmailRequest struct {
	ID string
}

// UnverifyUserEmailRequest holds data for unverifying a user's email
type UnverifyUserEmailRequest struct {
	ID string
}

// VerifyUserPhoneRequest holds data for verifying a user's phone
type VerifyUserPhoneRequest struct {
	ID string
}

// SetUserExtensionRequest holds data for setting a user extension field
type SetUserExtensionRequest struct {
	ID    string
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// GetUserExtensionRequest holds data for retrieving a user extension field
type GetUserExtensionRequest struct {
	ID  string
	Key string
}

// UpdateUserPersonalInfoRequest holds data for updating user personal information
type UpdateUserPersonalInfoRequest struct {
	ID        string
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	FullName  string `json:"full_name,omitempty"`
	Avatar    string `json:"avatar,omitempty"`
	Phone     string `json:"phone,omitempty"`
}

// RecordUserLoginRequest holds data for recording a user login
type RecordUserLoginRequest struct {
	ID string
}

// GetUserProfileRequest holds data for retrieving a user profile
type GetUserProfileRequest struct {
	ID string
}

// GetUserMicroProfileRequest holds data for retrieving a user micro profile
type GetUserMicroProfileRequest struct {
	ID string
}

// ValidateUserRequest holds data for validating a user
type ValidateUserRequest struct {
	ID string `json:"id"`
}

// BulkUpdateUsersStatusRequest holds data for bulk updating user statuses
type BulkUpdateUsersStatusRequest struct {
	IDs           []string `json:"ids"`
	DesiredStatus string   `json:"desired_status"`
}
