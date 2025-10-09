package user

import (
	userv1 "github.com/ooaklee/ghatd/external/user"
	"github.com/ooaklee/reply"
)

// CreateUserResponse holds the response for creating a user
type CreateUserResponse struct {
	User *UniversalUser `json:"user"`
}

// UpdateUserResponse holds the response for updating a user
type UpdateUserResponse struct {
	User *UniversalUser `json:"user"`
}

// GetUserByIDResponse holds the response for retrieving a user by ID
type GetUserByIDResponse struct {
	User *UniversalUser `json:"user"`
}

// GetUserByNanoIDResponse holds the response for retrieving a user by nano ID
type GetUserByNanoIDResponse struct {
	User *UniversalUser `json:"user"`
}

// GetUserByEmailResponse holds the response for retrieving a user by email
type GetUserByEmailResponse struct {
	User *UniversalUser `json:"user"`
}

// GetUsersResponse holds the response for retrieving users with pagination
type GetUsersResponse struct {
	Users []UniversalUser     `json:"users"`
	Meta  *PaginationMetadata `json:"meta"`
}

// PaginationMetadata holds pagination information
type PaginationMetadata struct {
	Page           int   `json:"page"`
	PerPage        int   `json:"per_page"`
	TotalResources int64 `json:"total_resources"`
	TotalPages     int   `json:"total_pages"`
}

// GetTotalUsersResponse holds the response for counting total users
type GetTotalUsersResponse struct {
	Total int64 `json:"total"`
}

// UpdateUserStatusResponse holds the response for updating user status
type UpdateUserStatusResponse struct {
	User *UniversalUser `json:"user"`
}

// AddUserRoleResponse holds the response for adding a role to a user
type AddUserRoleResponse struct {
	User *UniversalUser `json:"user"`
}

// RemoveUserRoleResponse holds the response for removing a role from a user
type RemoveUserRoleResponse struct {
	User *UniversalUser `json:"user"`
}

// VerifyUserEmailResponse holds the response for verifying a user's email
type VerifyUserEmailResponse struct {
	User *UniversalUser `json:"user"`
}

// UnverifyUserEmailResponse holds the response for unverifying a user's email
type UnverifyUserEmailResponse struct {
	User *UniversalUser `json:"user"`
}

// VerifyUserPhoneResponse holds the response for verifying a user's phone
type VerifyUserPhoneResponse struct {
	User *UniversalUser `json:"user"`
}

// SetUserExtensionResponse holds the response for setting a user extension field
type SetUserExtensionResponse struct {
	User *UniversalUser `json:"user"`
}

// GetUserExtensionResponse holds the response for retrieving a user extension field
type GetUserExtensionResponse struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
	Found bool        `json:"found"`
}

// UpdateUserPersonalInfoResponse holds the response for updating user personal information
type UpdateUserPersonalInfoResponse struct {
	User *UniversalUser `json:"user"`
}

// RecordUserLoginResponse holds the response for recording a user login
type RecordUserLoginResponse struct {
	User *UniversalUser `json:"user"`
}

// GetUserProfileResponse holds the response for retrieving a user profile
type GetUserProfileResponse struct {
	Profile *userv1.UserProfile `json:"profile"`
}

// GetUserMicroProfileResponse holds the response for retrieving a user micro profile
type GetUserMicroProfileResponse struct {
	MicroProfile *userv1.UserMicroProfile `json:"micro_profile"`
}

// ValidateUserResponse holds the response for validating a user
type ValidateUserResponse struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

// SearchUsersByExtensionResponse holds the response for searching users by extension field
type SearchUsersByExtensionResponse struct {
	Users []UniversalUser     `json:"users"`
	Meta  *PaginationMetadata `json:"meta"`
}

// BulkUpdateUsersStatusResponse holds the response for bulk updating user statuses
type BulkUpdateUsersStatusResponse struct {
	UpdatedCount int      `json:"updated_count"`
	FailedIDs    []string `json:"failed_ids,omitempty"`
}

// GetUsersByRolesResponse holds the response for retrieving users by roles
type GetUsersByRolesResponse struct {
	Users []UniversalUser     `json:"users"`
	Meta  *PaginationMetadata `json:"meta"`
}

// GetUsersByStatusResponse holds the response for retrieving users by status
type GetUsersByStatusResponse struct {
	Users []UniversalUser     `json:"users"`
	Meta  *PaginationMetadata `json:"meta"`
}

// DeleteUserResponse holds the response for deleting a user (if needed)
type DeleteUserResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// GetMetaData converts PaginationMetadata to map for reply.WithMeta
func (p *PaginationMetadata) GetMetaData() map[string]interface{} {
	return map[string]interface{}{
		"page":            p.Page,
		"per_page":        p.PerPage,
		"total_resources": p.TotalResources,
		"total_pages":     p.TotalPages,
	}
}

// GetBaseResponseHandler returns response handler configured with user error map
func GetBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(append([]reply.ErrorManifest{}, UserErrorMap))
}
