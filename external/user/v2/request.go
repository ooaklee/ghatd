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
}

// GetUserByIDRequest holds data for retrieving a user by ID
type GetUserByIDRequest struct {
	ID string `json:"id"`
}

// GetUserByNanoIDRequest holds data for retrieving a user by nano ID
type GetUserByNanoIDRequest struct {
	NanoID string `json:"nano_id"`
}

// GetUserByEmailRequest holds data for retrieving a user by email
type GetUserByEmailRequest struct {
	Email string `json:"email"`
}

// DeleteUserRequest holds data for deleting a user
type DeleteUserRequest struct {
	ID string `json:"id"`
}

// GetUsersRequest holds filters and pagination for retrieving users
type GetUsersRequest struct {
	// Pagination
	Page    int `json:"page"`
	PerPage int `json:"per_page"`

	// Sorting
	Order string `json:"order"`

	// Metadata flag
	IncludeMeta bool `json:"meta,omitempty"`

	// Filters
	EmailFilter     string      `json:"email_filter,omitempty"`
	FirstNameFilter string      `json:"first_name_filter,omitempty"`
	LastNameFilter  string      `json:"last_name_filter,omitempty"`
	StatusFilter    string      `json:"status_filter,omitempty"`
	RoleFilter      string      `json:"role_filter,omitempty"`
	RolesFilter     []string    `json:"roles_filter,omitempty"`
	OnlyAdmin       bool        `json:"only_admin,omitempty"`
	EmailVerified   *bool       `json:"email_verified,omitempty"`
	PhoneVerified   *bool       `json:"phone_verified,omitempty"`
	ExtensionKey    string      `json:"extension_key,omitempty"`
	ExtensionValue  interface{} `json:"extension_value,omitempty"`
}

// GetTotalUsersRequest holds filters for counting total users
type GetTotalUsersRequest struct {
	EmailFilter     string      `json:"email_filter,omitempty"`
	FirstNameFilter string      `json:"first_name_filter,omitempty"`
	LastNameFilter  string      `json:"last_name_filter,omitempty"`
	StatusFilter    string      `json:"status_filter,omitempty"`
	RoleFilter      string      `json:"role_filter,omitempty"`
	RolesFilter     []string    `json:"roles_filter,omitempty"`
	OnlyAdmin       bool        `json:"only_admin,omitempty"`
	EmailVerified   *bool       `json:"email_verified,omitempty"`
	PhoneVerified   *bool       `json:"phone_verified,omitempty"`
	ExtensionKey    string      `json:"extension_key,omitempty"`
	ExtensionValue  interface{} `json:"extension_value,omitempty"`
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
	ID   string `json:"id"`
	Role string `json:"role"`
}

// VerifyUserEmailRequest holds data for verifying a user's email
type VerifyUserEmailRequest struct {
	ID string `json:"id"`
}

// UnverifyUserEmailRequest holds data for unverifying a user's email
type UnverifyUserEmailRequest struct {
	ID string `json:"id"`
}

// VerifyUserPhoneRequest holds data for verifying a user's phone
type VerifyUserPhoneRequest struct {
	ID string `json:"id"`
}

// SetUserExtensionRequest holds data for setting a user extension field
type SetUserExtensionRequest struct {
	ID    string      `json:"id"`
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// GetUserExtensionRequest holds data for retrieving a user extension field
type GetUserExtensionRequest struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// UpdateUserPersonalInfoRequest holds data for updating user personal information
type UpdateUserPersonalInfoRequest struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	FullName  string `json:"full_name,omitempty"`
	Avatar    string `json:"avatar,omitempty"`
	Phone     string `json:"phone,omitempty"`
}

// RecordUserLoginRequest holds data for recording a user login
type RecordUserLoginRequest struct {
	ID string `json:"id"`
}

// GetUserProfileRequest holds data for retrieving a user profile
type GetUserProfileRequest struct {
	ID string `json:"id"`
}

// GetUserMicroProfileRequest holds data for retrieving a user micro profile
type GetUserMicroProfileRequest struct {
	ID string `json:"id"`
}

// ValidateUserRequest holds data for validating a user
type ValidateUserRequest struct {
	ID string `json:"id"`
}

// SearchUsersByExtensionRequest holds data for searching users by extension field
type SearchUsersByExtensionRequest struct {
	Key     string      `json:"key"`
	Value   interface{} `json:"value"`
	Page    int         `json:"page"`
	PerPage int         `json:"per_page"`
}

// BulkUpdateUsersStatusRequest holds data for bulk updating user statuses
type BulkUpdateUsersStatusRequest struct {
	IDs           []string `json:"ids"`
	DesiredStatus string   `json:"desired_status"`
}

// GetUsersByRolesRequest holds data for retrieving users by roles
type GetUsersByRolesRequest struct {
	Roles   []string `json:"roles"`
	Page    int      `json:"page"`
	PerPage int      `json:"per_page"`
	Order   string   `json:"order,omitempty"`
}

// GetUsersByStatusRequest holds data for retrieving users by status
type GetUsersByStatusRequest struct {
	Status  string `json:"status"`
	Page    int    `json:"page"`
	PerPage int    `json:"per_page"`
	Order   string `json:"order,omitempty"`
}
