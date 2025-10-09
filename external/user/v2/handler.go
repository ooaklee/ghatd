package user

import (
	"context"
	"net/http"

	"github.com/ooaklee/reply"
)

// UserService interface defines expected methods of a valid user service
type UserService interface {
	CreateUser(ctx context.Context, r *CreateUserRequest) (*CreateUserResponse, error)
	GetUserByID(ctx context.Context, r *GetUserByIDRequest) (*GetUserByIDResponse, error)
	GetUserByNanoID(ctx context.Context, r *GetUserByNanoIDRequest) (*GetUserByNanoIDResponse, error)
	GetUserByEmail(ctx context.Context, r *GetUserByEmailRequest) (*GetUserByEmailResponse, error)
	UpdateUser(ctx context.Context, r *UpdateUserRequest) (*UpdateUserResponse, error)
	DeleteUser(ctx context.Context, r *DeleteUserRequest) error
	GetUsers(ctx context.Context, r *GetUsersRequest) (*GetUsersResponse, error)
	GetTotalUsers(ctx context.Context, r *GetTotalUsersRequest) (*GetTotalUsersResponse, error)
	UpdateUserStatus(ctx context.Context, r *UpdateUserStatusRequest) (*UpdateUserStatusResponse, error)
	AddUserRole(ctx context.Context, r *AddUserRoleRequest) (*AddUserRoleResponse, error)
	RemoveUserRole(ctx context.Context, r *RemoveUserRoleRequest) (*RemoveUserRoleResponse, error)
	VerifyUserEmail(ctx context.Context, r *VerifyUserEmailRequest) (*VerifyUserEmailResponse, error)
	UnverifyUserEmail(ctx context.Context, r *UnverifyUserEmailRequest) (*UnverifyUserEmailResponse, error)
	VerifyUserPhone(ctx context.Context, r *VerifyUserPhoneRequest) (*VerifyUserPhoneResponse, error)
	RecordUserLogin(ctx context.Context, r *RecordUserLoginRequest) (*RecordUserLoginResponse, error)
	GetUserProfile(ctx context.Context, r *GetUserProfileRequest) (*GetUserProfileResponse, error)
	GetUserMicroProfile(ctx context.Context, r *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error)
	SetUserExtension(ctx context.Context, r *SetUserExtensionRequest) (*SetUserExtensionResponse, error)
	GetUserExtension(ctx context.Context, r *GetUserExtensionRequest) (*GetUserExtensionResponse, error)
	UpdateUserPersonalInfo(ctx context.Context, r *UpdateUserPersonalInfoRequest) (*UpdateUserPersonalInfoResponse, error)
	ValidateUser(ctx context.Context, r *ValidateUserRequest) (*ValidateUserResponse, error)
	SearchUsersByExtension(ctx context.Context, r *SearchUsersByExtensionRequest) (*SearchUsersByExtensionResponse, error)
	BulkUpdateUsersStatus(ctx context.Context, r *BulkUpdateUsersStatusRequest) (*BulkUpdateUsersStatusResponse, error)
	GetUsersByRoles(ctx context.Context, r *GetUsersByRolesRequest) (*GetUsersByRolesResponse, error)
	GetUsersByStatus(ctx context.Context, r *GetUsersByStatusRequest) (*GetUsersByStatusResponse, error)
}

// UserValidator interface defines expected methods of a valid validator
type UserValidator interface {
	Validate(s interface{}) error
}

// Handler manages user requests
type Handler struct {
	Service   UserService
	Validator UserValidator
}

// NewHandler returns a new user handler
func NewHandler(service UserService, validator UserValidator) *Handler {
	return &Handler{
		Service:   service,
		Validator: validator,
	}
}

// CreateUser handles user creation
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToCreateUserRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateUser(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.User)
}

// GetUserByID handles retrieval of a user by ID
func (h *Handler) GetUserByID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserByIDRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserByID(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// GetUserByNanoID handles retrieval of a user by nano ID
func (h *Handler) GetUserByNanoID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserByNanoIDRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserByNanoID(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// GetUserByEmail handles retrieval of a user by email
func (h *Handler) GetUserByEmail(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserByEmailRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserByEmail(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// UpdateUser handles user updates
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateUserRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateUser(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// DeleteUser handles user deletion
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToDeleteUserRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.DeleteUser(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusNoContent, nil)
}

// GetUsers handles retrieval of multiple users with filters and pagination
func (h *Handler) GetUsers(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUsersRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUsers(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	// Return with pagination metadata if requested
	if request.IncludeMeta {
		GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users, reply.WithMeta(response.Meta.GetMetaData()))
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users)
}

// UpdateUserStatus handles user status updates
func (h *Handler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateUserStatusRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateUserStatus(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// AddUserRole handles adding a role to a user
func (h *Handler) AddUserRole(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToAddUserRoleRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.AddUserRole(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// RemoveUserRole handles removing a role from a user
func (h *Handler) RemoveUserRole(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToRemoveUserRoleRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RemoveUserRole(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// VerifyUserEmail handles marking a user's email as verified
func (h *Handler) VerifyUserEmail(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToVerifyUserEmailRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.VerifyUserEmail(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// UnverifyUserEmail handles marking a user's email as unverified
func (h *Handler) UnverifyUserEmail(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUnverifyUserEmailRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UnverifyUserEmail(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// VerifyUserPhone handles marking a user's phone as verified
func (h *Handler) VerifyUserPhone(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToVerifyUserPhoneRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.VerifyUserPhone(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// RecordUserLogin handles recording a user login event
func (h *Handler) RecordUserLogin(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToRecordUserLoginRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RecordUserLogin(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// GetUserProfile handles retrieval of a user's full profile
func (h *Handler) GetUserProfile(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserProfileRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserProfile(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Profile)
}

// GetUserMicroProfile handles retrieval of a user's micro profile
func (h *Handler) GetUserMicroProfile(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserMicroProfileRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserMicroProfile(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.MicroProfile)
}

// SetUserExtension handles setting an extension field value
func (h *Handler) SetUserExtension(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToSetUserExtensionRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.SetUserExtension(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// GetUserExtension handles retrieving an extension field value
func (h *Handler) GetUserExtension(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserExtensionRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserExtension(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// UpdateUserPersonalInfo handles updating a user's personal information
func (h *Handler) UpdateUserPersonalInfo(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateUserPersonalInfoRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateUserPersonalInfo(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// ValidateUser handles validating a user
func (h *Handler) ValidateUser(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToValidateUserRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ValidateUser(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// SearchUsersByExtension handles searching for users by extension field value
func (h *Handler) SearchUsersByExtension(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToSearchUsersByExtensionRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.SearchUsersByExtension(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users, reply.WithMeta(response.Meta.GetMetaData()))
}

// BulkUpdateUsersStatus handles bulk updating user statuses
func (h *Handler) BulkUpdateUsersStatus(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToBulkUpdateUsersStatusRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.BulkUpdateUsersStatus(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetUsersByRoles handles retrieving users by roles
func (h *Handler) GetUsersByRoles(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUsersByRolesRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUsersByRoles(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users, reply.WithMeta(response.Meta.GetMetaData()))
}

// GetUsersByStatus handles retrieving users by status
func (h *Handler) GetUsersByStatus(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUsersByStatusRequest(r, h.Validator)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUsersByStatus(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users, reply.WithMeta(response.Meta.GetMetaData()))
}
