package user

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// UserService interface defines expected methods of a valid user service
type UserService interface {
	// CreateUser creates a new user from the request via the user service,
	// resolving config, checking email uniqueness, assigning roles/status and
	// persisting; the handler responds 201 with the created user.
	CreateUser(ctx context.Context, r *CreateUserRequest) (*CreateUserResponse, error)
	// GetUserByID loads the account with the requested persistent ID and returns it
	// with model dependencies restored; the service maps missing accounts to
	// ErrUserNotFound.
	GetUserByID(ctx context.Context, r *GetUserByIDRequest) (*GetUserByIDResponse, error)
	// GetUserByNanoID loads the account with the requested public nano identifier
	// and returns it with model dependencies restored, sharing GetUserByID's
	// absence contract.
	GetUserByNanoID(ctx context.Context, r *GetUserByNanoIDRequest) (*GetUserByNanoIDResponse, error)
	// GetUserByEmail performs a normalised email lookup and returns the matching
	// account, preserving native failures including absence for the caller's error
	// mapping.
	GetUserByEmail(ctx context.Context, r *GetUserByEmailRequest) (*GetUserByEmailResponse, error)
	// UpdateUser applies a trusted broad update to the targeted account, either
	// patching non-empty scalar fields or replacing with the supplied snapshot, and
	// returns the acknowledged post-image; authorization belongs to callers.
	UpdateUser(ctx context.Context, r *UpdateUserRequest) (*UpdateUserResponse, error)
	// DeleteUser deletes the account with the requested ID after confirming it
	// exists, emitting an audit event when configured; the handler responds 204 on
	// success.
	DeleteUser(ctx context.Context, r *DeleteUserRequest) error
	// GetUsers retrieves users matching the request filters with normalised
	// pagination, returning users with dependencies restored plus pagination
	// metadata.
	GetUsers(ctx context.Context, r *GetUsersRequest) (*GetUsersResponse, error)
	// GetTotalUsers returns the total count of users matching the request's
	// filters.
	GetTotalUsers(ctx context.Context, r *GetTotalUsersRequest) (*GetTotalUsersResponse, error)
	// UpdateUserStatus applies a trusted status transition to the targeted account,
	// validating the configured model and writing only owned fields; external
	// callers use the manager for authorization.
	UpdateUserStatus(ctx context.Context, r *UpdateUserStatusRequest) (*UpdateUserStatusResponse, error)
	// AddUserRole adds the requested role to the targeted account as a trusted
	// domain command, returning the updated user and whether a change occurred.
	AddUserRole(ctx context.Context, r *AddUserRoleRequest) (*AddUserRoleResponse, error)
	// RemoveUserRole removes all occurrences of the requested role from the
	// targeted account, reporting Changed for no-ops without timestamp churn.
	RemoveUserRole(ctx context.Context, r *RemoveUserRoleRequest) (*RemoveUserRoleResponse, error)
	// VerifyUserEmail marks the targeted user's email as verified, persists the
	// account and returns the updated user, emitting an audit event when
	// configured.
	VerifyUserEmail(ctx context.Context, r *VerifyUserEmailRequest) (*VerifyUserEmailResponse, error)
	// UnverifyUserEmail marks the targeted user's email as unverified, persists the
	// account and returns the updated user, emitting an audit event when
	// configured.
	UnverifyUserEmail(ctx context.Context, r *UnverifyUserEmailRequest) (*UnverifyUserEmailResponse, error)
	// VerifyUserPhone marks the targeted user's phone as verified, persists the
	// account and returns the updated user, emitting an audit event when
	// configured.
	VerifyUserPhone(ctx context.Context, r *VerifyUserPhoneRequest) (*VerifyUserPhoneResponse, error)
	// RecordUserLogin loads the user identified by the request, stamps last-login
	// and updated timestamps, persists the change, and returns the updated user
	// within the response.
	RecordUserLogin(ctx context.Context, r *RecordUserLoginRequest) (*RecordUserLoginResponse, error)
	// GetUserProfile resolves the user referenced by the request ID and returns
	// that user's full profile derived from the loaded account.
	GetUserProfile(ctx context.Context, r *GetUserProfileRequest) (*GetUserProfileResponse, error)
	// GetUserMicroProfile resolves the user referenced by the request ID and
	// returns the reduced micro-profile projection of that account.
	GetUserMicroProfile(ctx context.Context, r *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error)
	// SetUserExtension stores the request's key/value pair in the identified user's
	// extension map, updates timestamps, persists the user, and returns the updated
	// account.
	SetUserExtension(ctx context.Context, r *SetUserExtensionRequest) (*SetUserExtensionResponse, error)
	// GetUserExtension returns the value stored under the request's extension key
	// for the identified user, along with the key itself.
	GetUserExtension(ctx context.Context, r *GetUserExtensionRequest) (*GetUserExtensionResponse, error)
	// UpdateUserPersonalInfo applies non-empty personal info fields from the
	// request to the identified user, persists changes, and returns the updated
	// account.
	UpdateUserPersonalInfo(ctx context.Context, r *UpdateUserPersonalInfoRequest) (*UpdateUserPersonalInfoResponse, error)
	// ValidateUser loads the identified user and reports whether the account passes
	// model validation, returning validation error messages when it does not.
	ValidateUser(ctx context.Context, r *ValidateUserRequest) (*ValidateUserResponse, error)
	// BulkUpdateUsersStatus applies the requested status to each listed user
	// individually, returning the count of successful updates and the IDs that
	// failed.
	BulkUpdateUsersStatus(ctx context.Context, r *BulkUpdateUsersStatusRequest) (*BulkUpdateUsersStatusResponse, error)
	// GetUserStats returns aggregated statistics about platform users by delegating
	// count queries to the user repository.
	GetUserStats(ctx context.Context, r *GetUserStatsRequest) (*GetUserStatsResponse, error)
	// GetUserConfigs returns the default user config type and the available config
	// presets with their capabilities; request arguments are ignored.
	GetUserConfigs(ctx context.Context, r *GetUserConfigsRequest) (*GetUserConfigsResponse, error)
}

// UserValidator interface defines expected methods of a valid validator
type UserValidator interface {
	// Validate checks the supplied value and reports configuration or field
	// failures as an error; the model implementation verifies required fields,
	// status, and roles without logging.
	Validate(s interface{}) error
}

// Handler manages user requests
type Handler struct {
	// RoleManager owns role authorization and audit, not the trusted domain.
	RoleManager RoleManager
	// StatusManager owns administrative authority and audit, separate from Service.
	StatusManager StatusManager
	Service       UserService
	Validator     UserValidator
	ErrorMaps     []reply.ErrorManifest
}

// NewHandler returns a new user handler
func NewHandler(service UserService, validator UserValidator, errorMaps ...reply.ErrorManifest) *Handler {
	return &Handler{
		Service:   service,
		Validator: validator,
		ErrorMaps: errorMaps,
	}
}

// CreateUser handles user creation
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-create-user")
	request, err := MapRequestToCreateUserRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateUser(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.User)
}

// GetUserByID handles retrieval of a user by ID
func (h *Handler) GetUserByID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-get-user-by-id")
	request, err := MapRequestToGetUserByIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserByID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// GetUserByNanoID handles retrieval of a user by nano ID
func (h *Handler) GetUserByNanoID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-get-user-by-nano-id")
	request, err := MapRequestToGetUserByNanoIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserByNanoID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// GetUserByEmail handles retrieval of a user by email
func (h *Handler) GetUserByEmail(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-get-user-by-email")
	request, err := MapRequestToGetUserByEmailRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserByEmail(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// UpdateUser handles user updates
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-update-user")
	request, err := MapRequestToUpdateUserRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if nilUserDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrUserUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	response, err := h.Service.UpdateUser(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if response == nil || response.User == nil || response.User.ID != request.ID {
		h.NewHTTPErrorResponse(w, ErrUserUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User, reply.WithContext(r.Context()))
}

// DeleteUser handles user deletion
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-delete-user")
	request, err := MapRequestToDeleteUserRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.DeleteUser(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusNoContent, nil)
}

// GetUsers handles retrieval of multiple users with filters and pagination
func (h *Handler) GetUsers(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-get-users")
	request, err := MapRequestToGetUsersRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUsers(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	// Return with pagination metadata if requested
	if request.IncludeMeta {
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users, reply.WithMeta(response.Meta.GetMetaData()))
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users)
}

// UpdateUserStatus handles user status updates
func (h *Handler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-update-user-status")
	request, err := MapRequestToUpdateUserStatusRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if nilUserDependency(h.StatusManager) {
		h.NewHTTPErrorResponse(w, ErrStatusUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	response, err := h.StatusManager.UpdateUserStatus(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if response == nil || response.User == nil || response.User.ID != request.ID {
		h.NewHTTPErrorResponse(w, ErrStatusUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User, reply.WithContext(r.Context()))
}

// AddUserRole handles adding a role to a user
func (h *Handler) AddUserRole(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-add-user-role")
	request, err := MapRequestToAddUserRoleRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if nilUserDependency(h.RoleManager) {
		h.NewHTTPErrorResponse(w, ErrRoleUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	response, err := h.RoleManager.AddUserRole(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if response == nil || response.User == nil || response.User.ID != request.ID {
		h.NewHTTPErrorResponse(w, ErrRoleUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User, reply.WithContext(r.Context()))
}

// RemoveUserRole handles removing a role from a user
func (h *Handler) RemoveUserRole(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-remove-user-role")
	request, err := MapRequestToRemoveUserRoleRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if nilUserDependency(h.RoleManager) {
		h.NewHTTPErrorResponse(w, ErrRoleUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	response, err := h.RoleManager.RemoveUserRole(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if response == nil || response.User == nil || response.User.ID != request.ID {
		h.NewHTTPErrorResponse(w, ErrRoleUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User, reply.WithContext(r.Context()))
}

// VerifyUserEmail handles marking a user's email as verified
func (h *Handler) VerifyUserEmail(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-verify-user-email")
	request, err := MapRequestToVerifyUserEmailRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.VerifyUserEmail(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// UnverifyUserEmail handles marking a user's email as unverified
func (h *Handler) UnverifyUserEmail(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-unverify-user-email")
	request, err := MapRequestToUnverifyUserEmailRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UnverifyUserEmail(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// VerifyUserPhone handles marking a user's phone as verified
func (h *Handler) VerifyUserPhone(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-verify-user-phone")
	request, err := MapRequestToVerifyUserPhoneRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.VerifyUserPhone(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// RecordUserLogin handles recording a user login event
func (h *Handler) RecordUserLogin(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-record-user-login")
	request, err := MapRequestToRecordUserLoginRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RecordUserLogin(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// GetUserProfile handles retrieval of a user's full profile
func (h *Handler) GetUserProfile(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-get-user-profile")
	request, err := MapRequestToGetUserProfileRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserProfile(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Profile)
}

// GetUserMicroProfile handles retrieval of a user's micro profile
func (h *Handler) GetUserMicroProfile(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-get-user-micro-profile")
	request, err := MapRequestToGetUserMicroProfileRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserMicroProfile(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.MicroProfile)
}

// SetUserExtension handles setting an extension field value
func (h *Handler) SetUserExtension(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-set-user-extension")
	request, err := MapRequestToSetUserExtensionRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.SetUserExtension(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// GetUserExtension handles retrieving an extension field value
func (h *Handler) GetUserExtension(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-get-user-extension")
	request, err := MapRequestToGetUserExtensionRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserExtension(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// UpdateUserPersonalInfo handles updating a user's personal information
func (h *Handler) UpdateUserPersonalInfo(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-update-user-personal-info")
	request, err := MapRequestToUpdateUserPersonalInfoRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateUserPersonalInfo(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// ValidateUser handles validating a user
func (h *Handler) ValidateUser(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-validate-user")
	request, err := MapRequestToValidateUserRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ValidateUser(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// BulkUpdateUsersStatus handles bulk updating user statuses
func (h *Handler) BulkUpdateUsersStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-bulk-update-users-status")
	request, err := MapRequestToBulkUpdateUsersStatusRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if nilUserDependency(h.StatusManager) {
		h.NewHTTPErrorResponse(w, ErrStatusUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	response, err := h.StatusManager.BulkUpdateUsersStatus(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}

	if response == nil {
		h.NewHTTPErrorResponse(w, ErrStatusUpdateUnavailable, reply.WithContext(r.Context()))
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response, reply.WithContext(r.Context()))
}

// GetUserStats handles retrieving aggregated stats about platform users
func (h *Handler) GetUserStats(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-get-user-stats")
	request, err := MapRequestToGetUserStatsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserStats(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetUserConfigs handles retrieving supported user config presets
func (h *Handler) GetUserConfigs(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/user/v2", "handle-get-user-configs")
	request, err := MapRequestToGetUserConfigsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserConfigs(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}
