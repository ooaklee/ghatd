package user

import (
	"context"
	"maps"
	"regexp"
	"strings"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// AuditService expected methods of a valid audit service
type AuditService interface {
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
}

// UserRepository expected methods of a valid user repository
type UserRepository interface {
	CreateUser(ctx context.Context, user *UniversalUser) (*UniversalUser, error)
	GetUserByID(ctx context.Context, id string) (*UniversalUser, error)
	GetUserByNanoID(ctx context.Context, nanoID string) (*UniversalUser, error)
	GetUserByEmail(ctx context.Context, email string, logError bool) (*UniversalUser, error)
	UpdateUser(ctx context.Context, user *UniversalUser) (*UniversalUser, error)
	DeleteUserByID(ctx context.Context, id string) error
	GetUsers(ctx context.Context, req *GetUsersRequest) ([]UniversalUser, error)
	GetTotalUsers(ctx context.Context, req *GetTotalUsersRequest) (int64, error)
	GetUserStatsCounts(ctx context.Context, req *GetUserStatsRequest) (*UserStats, error)
}

// Service holds and manages user business logic
type Service struct {
	signupAttribution *SignupAttributionConfig
	// handleGenerator is optional startup-only candidate generation, not entropy
	// for credentials. Nil uses the standard adjective/animal generator.
	handleGenerator            func() string
	UserRepository             UserRepository
	AuditService               AuditService
	Config                     *UserConfig
	Configs                    []*UserConfig
	IDGenerator                IDGenerator
	TimeProvider               TimeProvider
	StringUtils                StringUtils
	AutoAdminEmailAddressRegex string
}

// NewService creates a new user service
func NewService(
	userRepository UserRepository,
	auditService AuditService,
	config *UserConfig,
	idGenerator IDGenerator,
	timeProvider TimeProvider,
	stringUtils StringUtils,
	autoAdminEmailAddressRegex string,
) *Service {
	if config == nil {
		config = DefaultUserConfig()
	}
	config = ensureUserConfigType(config)

	service := &Service{
		UserRepository:             userRepository,
		AuditService:               auditService,
		Config:                     config,
		Configs:                    registerUserConfigs(config),
		IDGenerator:                idGenerator,
		TimeProvider:               timeProvider,
		StringUtils:                stringUtils,
		AutoAdminEmailAddressRegex: autoAdminEmailAddressRegex,
	}

	return service
}

// WithConfigs registers additional user configs supported by the service.
func (s *Service) WithConfigs(configs ...*UserConfig) *Service {
	s.Configs = registerUserConfigs(append([]*UserConfig{s.defaultConfig()}, configs...)...)
	return s
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
	if req == nil {
		return nil, ErrInvalidUserBody
	}
	if s == nil || s.UserRepository == nil || ctx == nil {
		return nil, ErrDatabaseError
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "create-user"))
	config, err := s.resolveRequestedConfig(req.Type)
	if err != nil {
		logger.Error("invalid-user-config-type", zap.String("config-type", req.Type), zap.Error(err))
		return nil, err
	}

	// Check if user already exists
	existingUser, lookupErr := s.UserRepository.GetUserByEmail(ctx, normaliseUserEmail(req.Email), false)
	if lookupErr != nil && lookupErr != ErrUserNotFound {
		return nil, lookupErr
	}
	if existingUser != nil {
		logger.Error("user-with-email-already-exists", emailLogFields("email", req.Email)...)
		return nil, ErrEmailAlreadyExists
	}

	// Create new user with dependencies
	user := NewUniversalUser(config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Set basic fields
	user.Email = normaliseUserEmail(req.Email)

	// Set personal info if provided
	if req.FirstName != "" || req.LastName != "" || req.FullName != "" || req.Avatar != "" || req.Phone != "" {
		user.PersonalInfo = &PersonalInfo{
			FirstName: req.FirstName,
			LastName:  req.LastName,
			FullName:  req.FullName,
			Avatar:    req.Avatar,
			Phone:     req.Phone,
		}

		user.SetFullName()
	}

	// Set roles
	if len(req.Roles) > 0 {
		user.Roles = req.Roles
	} else {
		user.Roles = []string{}

		if config.DefaultRole != "" {
			user.Roles = append(user.Roles, config.DefaultRole)
		}

		// Check if email matches auto-admin regex
		isAutoAdmin := s.shouldBeAutoAdmin(user.Email)
		if isAutoAdmin {
			user.Roles = append(user.Roles, UserRoleAdmin)
		}
	}

	// Set status
	if req.Status != "" {
		user.Status = req.Status
	} else {
		user.Status = config.DefaultStatus
	}

	// Set extensions
	if req.Extensions != nil {
		user.Extensions = req.Extensions
	}

	// Generate IDs
	if req.GenerateUUID {
		user.GenerateNewUUID()
	} else if user.ID == "" {
		user.ID = toolbox.GenerateUuidV4()
	}

	if req.GenerateNanoID && config.MultipleIdentifiers {
		user.GenerateNewNanoID()
	}

	// Set initial timestamps and state
	user.SetInitialState()

	user.Standardise()

	// Validate user
	if err := user.Validate(); err != nil {
		logger.Error("user-validation-failed", zap.Error(err))
		return nil, ErrValidationFailed
	}

	if err := s.captureSignup(user, req.AttributionEvidence); err != nil {
		return nil, err
	}

	// Create user in repository
	createdUser, err := createWithHandle(ctx, s, config, user, func() (*UniversalUser, error) { return s.UserRepository.CreateUser(ctx, user) })
	if err != nil {
		logger.Error("failed-to-create-user")
		return nil, err
	}
	if createdUser == nil {
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:     "user.created",
			TargetId:   createdUser.ID,
			TargetType: audit.TargetTypeUser,
			Details:    map[string]interface{}{"user_id": createdUser.ID, "email": createdUser.Email, "is_auto_admin": len(req.Roles) == 0 && s.shouldBeAutoAdmin(createdUser.Email)},
		})
	}

	logger.Info("user-created-successfully", zap.String("user-id", createdUser.ID))

	return &CreateUserResponse{User: createdUser}, nil
}

// GetUserByID loads the current account by its persistent ID and restores model
// dependencies. Expected absence remains ErrUserNotFound; repository failures
// and cancellations are preserved rather than reported as missing accounts.
func (s *Service) GetUserByID(ctx context.Context, req *GetUserByIDRequest) (*GetUserByIDResponse, error) {
	if req == nil || req.ID == "" {
		return nil, ErrInvalidUserID
	}
	if s == nil || s.UserRepository == nil || ctx == nil {
		return nil, ErrDatabaseError
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	return &GetUserByIDResponse{User: user}, nil
}

// GetUserByNanoID loads the current account by its public identifier. It has the
// same absence, dependency and cancellation contract as GetUserByID.
func (s *Service) GetUserByNanoID(ctx context.Context, req *GetUserByNanoIDRequest) (*GetUserByNanoIDResponse, error) {
	if req == nil || req.NanoID == "" {
		return nil, ErrInvalidNanoID
	}
	if s == nil || s.UserRepository == nil || ctx == nil {
		return nil, ErrDatabaseError
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	user, err := s.UserRepository.GetUserByNanoID(ctx, req.NanoID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	return &GetUserByNanoIDResponse{User: user}, nil
}

// GetUserByEmail performs a strict normalized lookup. It preserves native
// failures, including absence, for the caller's shared error manifest.
func (s *Service) GetUserByEmail(ctx context.Context, req *GetUserByEmailRequest) (*GetUserByEmailResponse, error) {
	return s.lookupEmail(ctx, req, true)
}

// FindUserByEmail looks up a user for workflows where absence is an expected
// outcome, such as availability checks and optional account association. A
// missing user retains its native absence error without service diagnostics.
// Operational failures also retain their original error tree, never masquerading
// as absence. Nil/mismatched adapter receipts are operational failures.
func (s *Service) FindUserByEmail(ctx context.Context, req *GetUserByEmailRequest) (*GetUserByEmailResponse, error) {
	return s.lookupEmail(ctx, req, false)
}

// UpdateUser applies a trusted legacy broad update to a detached account model.
// A manager/route must authorize its target and editable fields before calling.
// Empty scalar fields retain their values; User supplies a replacement snapshot.
// Native failures retain their error tree for reply mapping. This is not a
// field-level CAS: stale replacement snapshots can overwrite unrelated changes.
func (s *Service) UpdateUser(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
	if req == nil {
		return nil, ErrInvalidUserBody
	}
	if s == nil || ctx == nil || nilUserDependency(s.UserRepository) {
		return nil, ErrUserUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "update-user"))
	input := *req
	input.Extensions = maps.Clone(req.Extensions)
	input.User = copyUserForUpdate(req.User)
	req = &input

	targetUserId := req.ID
	if req.User != nil && req.User.ID != "" {
		if targetUserId != "" && targetUserId != req.User.ID {
			return nil, ErrInvalidUserID
		}
		targetUserId = req.User.ID
	}
	if strings.TrimSpace(targetUserId) == "" || req.User != nil && req.User.ID == "" {
		return nil, ErrInvalidUserID
	}

	// Get existing user
	user, err := s.UserRepository.GetUserByID(ctx, targetUserId)
	if err != nil {
		return nil, err
	}
	if user == nil || user.ID != targetUserId {
		return nil, ErrUserUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if (req.User != nil && req.User.Email != user.Email) || (req.Email != "" && req.Email != user.Email) {
		return nil, ErrEmailChangeRequired
	}
	if nilUserDependency(s.StringUtils) || nilUserDependency(s.TimeProvider) {
		return nil, ErrUserUpdateUnavailable
	}
	user = copyUserForUpdate(user)

	if req.User != nil {
		userWithProvidedData := req.User
		requestedType := userWithProvidedData.Type
		if requestedType == "" {
			requestedType = user.Type
		}

		config, err := s.resolveRequestedConfig(requestedType)
		if err != nil {
			return nil, err
		}

		userWithProvidedData.SetDependencies(config, s.IDGenerator, s.TimeProvider, s.StringUtils)
		userWithProvidedData.Type = config.GetType(s.defaultConfig())

		userWithProvidedData.SetFullName()

		user = userWithProvidedData

	}

	if req.User == nil {
		s.setUserDependencies(user)

		// Update fields
		hasChanges := false

		if req.Type != "" && req.Type != user.Type {
			config, err := s.resolveRequestedConfig(req.Type)
			if err != nil {
				return nil, err
			}

			user.Type = config.GetType(s.defaultConfig())
			user.SetDependencies(config, s.IDGenerator, s.TimeProvider, s.StringUtils)
			hasChanges = true
		}

		if req.FirstName != "" && req.FirstName != user.PersonalInfo.FirstName {
			user.PersonalInfo.FirstName = req.FirstName
			hasChanges = true
		}

		if req.LastName != "" && req.LastName != user.PersonalInfo.LastName {
			user.PersonalInfo.LastName = req.LastName
			hasChanges = true
		}

		if hasChanges {
			user.SetFullName()
		}

		if req.FullName != "" && req.FullName != user.PersonalInfo.FullName {
			user.PersonalInfo.FullName = req.FullName
			hasChanges = true
		}

		if req.Avatar != "" && req.Avatar != user.PersonalInfo.Avatar {
			user.PersonalInfo.Avatar = req.Avatar
			hasChanges = true
		}

		if req.Phone != "" && req.Phone != user.PersonalInfo.Phone {
			user.PersonalInfo.Phone = req.Phone
			hasChanges = true
		}

		if req.Status != "" && req.Status != user.Status {
			_, err := user.UpdateStatus(req.Status)
			if err != nil {
				return nil, err
			}
			hasChanges = true
		}

		if req.Extensions != nil {
			for key, value := range req.Extensions {
				user.SetExtension(key, value)
			}
			hasChanges = true
		}

		if !hasChanges {
			return &UpdateUserResponse{User: user}, nil
		}
	}

	// Update timestamps
	user.SetUpdatedAtNow()

	// Validate user
	if err := user.Validate(); err != nil {
		return nil, err
	}

	// Ensure version is set to 2 for migrated users
	user.EnsureVersion()

	user.Standardise()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Snapshot expected receipt identity before exposing a pointer to an adapter.
	id, email, revision, accountType, status := user.ID, user.Email, user.EmailRevision, user.Type, user.Status

	// Update in repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		logger.Error("user-update-failed")
		return nil, err
	}
	if updatedUser == nil || updatedUser.ID != id || updatedUser.Email != email || updatedUser.EmailRevision != revision || updatedUser.Type != accountType || updatedUser.Status != status {
		return nil, ErrUserUpdateUnavailable
	}
	// The repository result, not the submitted snapshot, is authoritative. A
	// late cancellation must not invent a rollback of an acknowledged write.
	updatedUser = s.setUserDependencies(copyUserForUpdate(updatedUser))

	// Audit log
	if !nilUserDependency(s.AuditService) {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:     "user.updated",
			TargetId:   updatedUser.ID,
			TargetType: audit.TargetType("user"),
			Details:    map[string]interface{}{"user_id": updatedUser.ID},
		})
	}

	logger.Info("user-updated-successfully", zap.String("user-id", updatedUser.ID))

	return &UpdateUserResponse{User: updatedUser}, nil
}

// DeleteUser deletes a user
func (s *Service) DeleteUser(ctx context.Context, req *DeleteUserRequest) error {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "delete-user"))

	// Verify user exists
	_, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("user-not-found", zap.Error(err), zap.String("id", req.ID))
		return ErrUserNotFound
	}

	// Delete user
	err = s.UserRepository.DeleteUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-delete-user", zap.Error(err), zap.String("id", req.ID))
		return ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:     "user.deleted",
			TargetId:   req.ID,
			TargetType: audit.TargetType("user"),
			Details:    map[string]interface{}{"user_id": req.ID},
		})
	}

	logger.Info("user-deleted-successfully", zap.String("user-id", req.ID))

	return nil
}

// GetUsers retrieves users with filters and pagination
func (s *Service) GetUsers(ctx context.Context, req *GetUsersRequest) (*GetUsersResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "get-users"))

	// Validate pagination
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PerPage < 1 || req.PerPage > 100 {
		req.PerPage = 25
	}

	// Get total count
	totalReq := &GetTotalUsersRequest{
		EmailFilter:     req.EmailFilter,
		FirstNameFilter: req.FirstNameFilter,
		LastNameFilter:  req.LastNameFilter,
		StatusFilter:    req.StatusFilter,
		RoleFilter:      req.RoleFilter,
		IDsFilter:       req.IDsFilter,
		RolesFilter:     req.RolesFilter,
		OnlyAdmin:       req.OnlyAdmin,
		EmailVerified:   req.EmailVerified,
		PhoneVerified:   req.PhoneVerified,
		ExtensionKey:    req.ExtensionKey,
		ExtensionValue:  req.ExtensionValue,
	}

	totalMatchingUsers, err := s.UserRepository.GetTotalUsers(ctx, totalReq)
	if err != nil {
		logger.Error("failed-to-get-total-users", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Get users
	users, err := s.UserRepository.GetUsers(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-users", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Reinject dependencies for all users
	for i := range users {
		s.setUserDependencies(&users[i])
	}

	// handle page pagination
	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{PerPage: req.PerPage, Page: req.Page}, users, int(totalMatchingUsers))
	if err != nil {
		return nil, err
	}

	return &GetUsersResponse{
		Users: paginatedResponse.Resources,
		Meta: &PaginationMetadata{
			Page:           paginatedResponse.Page,
			PerPage:        paginatedResponse.ResourcePerPage,
			TotalResources: int64(paginatedResponse.Total),
			TotalPages:     paginatedResponse.TotalPages,
		},
	}, nil
}

// GetTotalUsers retrieves the total count of users matching filters
func (s *Service) GetTotalUsers(ctx context.Context, req *GetTotalUsersRequest) (*GetTotalUsersResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "get-total-users"))

	total, err := s.UserRepository.GetTotalUsers(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-total-users", zap.Error(err))
		return nil, ErrDatabaseError
	}

	return &GetTotalUsersResponse{Total: total}, nil
}

// UpdateUserStatus is a trusted domain transition, not an authorization boundary.
// It validates the configured model and conditionally writes only owned fields.
// External callers must use the manager for live authority and actor-bound audit.
func (s *Service) UpdateUserStatus(ctx context.Context, req *UpdateUserStatusRequest) (*UpdateUserStatusResponse, error) {
	return s.updateAccountStatus(ctx, req)
}

// AddUserRole is a trusted configured domain command. External callers use the
// manager for live authority and actor-bound audit; native errors remain intact.
func (s *Service) AddUserRole(ctx context.Context, req *AddUserRoleRequest) (*AddUserRoleResponse, error) {
	if req == nil {
		return nil, ErrInvalidUserBody
	}
	v, changed, err := s.updateAccountRole(ctx, req.ID, req.Role, false)
	if err != nil {
		return nil, err
	}
	return &AddUserRoleResponse{User: v, Changed: changed}, nil
}

// RemoveUserRole conditionally removes all occurrences of the selected role.
// It permits retiring obsolete roles and confirms no-ops without timestamp churn.
func (s *Service) RemoveUserRole(ctx context.Context, req *RemoveUserRoleRequest) (*RemoveUserRoleResponse, error) {
	if req == nil {
		return nil, ErrInvalidUserBody
	}
	v, changed, err := s.updateAccountRole(ctx, req.ID, req.Role, true)
	if err != nil {
		return nil, err
	}
	return &RemoveUserRoleResponse{User: v, Changed: changed}, nil
}

// VerifyUserEmail marks a user's email as verified
func (s *Service) VerifyUserEmail(ctx context.Context, req *VerifyUserEmailRequest) (*VerifyUserEmailResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "verify-user-email"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-email-verification", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	// Verify email
	user.VerifyEmail()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		logger.Error("failed-to-save-user-after-email-verification", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:     "user.email_verified",
			TargetId:   updatedUser.ID,
			TargetType: audit.TargetType("user"),
			Details:    map[string]interface{}{"user_id": updatedUser.ID},
		})
	}

	logger.Info("user-email-verified-successfully", zap.String("user-id", updatedUser.ID))

	return &VerifyUserEmailResponse{User: updatedUser}, nil
}

// UnverifyUserEmail marks a user's email as unverified
func (s *Service) UnverifyUserEmail(ctx context.Context, req *UnverifyUserEmailRequest) (*UnverifyUserEmailResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "unverify-user-email"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-email-unverification", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	// Unverify email
	user.UnverifyEmail()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		logger.Error("failed-to-save-user-after-email-unverification", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:     "user.email_unverified",
			TargetId:   updatedUser.ID,
			TargetType: audit.TargetType("user"),
			Details:    map[string]interface{}{"user_id": updatedUser.ID},
		})
	}

	logger.Info("user-email-unverified-successfully", zap.String("user-id", updatedUser.ID))

	return &UnverifyUserEmailResponse{User: updatedUser}, nil
}

// VerifyUserPhone marks a user's phone as verified
func (s *Service) VerifyUserPhone(ctx context.Context, req *VerifyUserPhoneRequest) (*VerifyUserPhoneResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "veify-user-phone"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-phone-verification", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	// Verify phone
	user.VerifyPhone()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		logger.Error("failed-to-save-user-after-phone-verification", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:     "user.phone_verified",
			TargetId:   updatedUser.ID,
			TargetType: audit.TargetType("user"),
			Details:    map[string]interface{}{"user_id": updatedUser.ID},
		})
	}

	logger.Info("user-phone-verified-successfully", zap.String("user-id", updatedUser.ID))

	return &VerifyUserPhoneResponse{User: updatedUser}, nil
}

// RecordUserLogin records a user login event
func (s *Service) RecordUserLogin(ctx context.Context, req *RecordUserLoginRequest) (*RecordUserLoginResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "record-user-login"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-login-recording", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	// Update last login timestamp
	user.SetLastLoginAtNow()
	user.SetUpdatedAtNow()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		logger.Error("failed-to-save-user-after-login-recording", zap.Error(err))
		return nil, ErrDatabaseError
	}

	logger.Info("user-login-recorded-successfully", zap.String("user-id", updatedUser.ID))

	return &RecordUserLoginResponse{User: updatedUser}, nil
}

// GetUserProfile retrieves a user's profile
func (s *Service) GetUserProfile(ctx context.Context, req *GetUserProfileRequest) (*GetUserProfileResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "get-user-profile"))

	userResp, err := s.GetUserByID(ctx, &GetUserByIDRequest{ID: req.ID})
	if err != nil {
		logger.Error("failed-to-get-user-for-profile-retrieval", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	profile := userResp.User.GetAsProfile()

	return &GetUserProfileResponse{Profile: profile}, nil
}

// GetUserMicroProfile retrieves a user's micro profile
func (s *Service) GetUserMicroProfile(ctx context.Context, req *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "get-user-micro-profile"))

	userResp, err := s.GetUserByID(ctx, &GetUserByIDRequest{ID: req.ID})
	if err != nil {
		logger.Error("failed-to-get-user-for-micro-profile-retrieval", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	microProfile := userResp.User.GetAsMicroProfile()

	return &GetUserMicroProfileResponse{MicroProfile: microProfile}, nil
}

// SetUserExtension sets an extension field value
func (s *Service) SetUserExtension(ctx context.Context, req *SetUserExtensionRequest) (*SetUserExtensionResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "set-user-extension"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-setting-extension", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	// Set extension
	if user.Extensions == nil {
		user.Extensions = make(map[string]interface{})
	}
	user.Extensions[req.Key] = req.Value
	user.SetUpdatedAtNow()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		logger.Error("failed-to-save-user-after-setting-extension", zap.Error(err))
		return nil, ErrDatabaseError
	}

	logger.Info("user-extension-set-successfully", zap.String("user-id", updatedUser.ID), zap.String("key", req.Key))

	return &SetUserExtensionResponse{User: updatedUser}, nil
}

// GetUserExtension retrieves an extension field value
func (s *Service) GetUserExtension(ctx context.Context, req *GetUserExtensionRequest) (*GetUserExtensionResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "get-user-extension"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-getting-extension", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Get extension value
	value, exists := user.Extensions[req.Key]
	if !exists {
		return nil, ErrExtensionNotFound
	}

	return &GetUserExtensionResponse{Key: req.Key, Value: value}, nil
}

// UpdateUserPersonalInfo updates a user's personal information
func (s *Service) UpdateUserPersonalInfo(ctx context.Context, req *UpdateUserPersonalInfoRequest) (*UpdateUserPersonalInfoResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "update-user-personal-info"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-updating-personal-info", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	// Update personal info fields
	if user.PersonalInfo == nil {
		user.PersonalInfo = &PersonalInfo{}
	}

	hasChanges := false
	if req.FirstName != "" && req.FirstName != user.PersonalInfo.FirstName {
		user.PersonalInfo.FirstName = req.FirstName
		hasChanges = true
	}

	if req.LastName != "" && req.LastName != user.PersonalInfo.LastName {
		user.PersonalInfo.LastName = req.LastName
		hasChanges = true
	}

	if req.FullName != "" && req.FullName != user.PersonalInfo.FullName {
		user.PersonalInfo.FullName = req.FullName
		hasChanges = true
	}

	if req.Avatar != "" && req.Avatar != user.PersonalInfo.Avatar {
		user.PersonalInfo.Avatar = req.Avatar
		hasChanges = true
	}

	if req.Phone != "" && req.Phone != user.PersonalInfo.Phone {
		user.PersonalInfo.Phone = req.Phone
		hasChanges = true
	}

	if !hasChanges {
		return &UpdateUserPersonalInfoResponse{User: user}, nil
	}

	user.SetUpdatedAtNow()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		logger.Error("failed-to-save-user-after-updating-personal-info", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:     "user.personal_info_updated",
			TargetId:   updatedUser.ID,
			TargetType: audit.TargetType("user"),
			Details:    map[string]interface{}{"user_id": updatedUser.ID},
		})
	}

	logger.Info("user-personal-info-updated-successfully", zap.String("user-id", updatedUser.ID))

	return &UpdateUserPersonalInfoResponse{User: updatedUser}, nil
}

// ValidateUser validates a user
func (s *Service) ValidateUser(ctx context.Context, req *ValidateUserRequest) (*ValidateUserResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "validate-user"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-validation", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	// Validate
	validationErr := user.Validate()

	if validationErr != nil {
		logger.Error("user-validation-failed", zap.Error(validationErr))
		errorStr := validationErr.Error()
		return &ValidateUserResponse{
			Valid:  false,
			Errors: []string{errorStr},
		}, nil
	}

	return &ValidateUserResponse{Valid: true, Errors: []string{}}, nil
}

// BulkUpdateUsersStatus updates status for multiple users
func (s *Service) BulkUpdateUsersStatus(ctx context.Context, req *BulkUpdateUsersStatusRequest) (*BulkUpdateUsersStatusResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "bulk-update-users-status"))

	var successCount, failureCount int
	var failedIDs []string

	for _, userID := range req.IDs {
		updateReq := &UpdateUserStatusRequest{
			ID:            userID,
			DesiredStatus: req.DesiredStatus,
		}

		_, err := s.UpdateUserStatus(ctx, updateReq)
		if err != nil {
			failureCount++
			failedIDs = append(failedIDs, userID)
			logger.Warn("failed-to-update-user-status-in-bulk-operation", zap.String("user-id", userID), zap.Error(err))
		} else {
			successCount++
		}
	}

	logger.Info("bulk-status-update-completed", zap.Int("success", successCount), zap.Int("failures", failureCount))

	return &BulkUpdateUsersStatusResponse{
		UpdatedCount: successCount,
		FailedIDs:    failedIDs,
	}, nil
}

// GetUserStats retrieves aggregated stats about platform users
func (s *Service) GetUserStats(ctx context.Context, req *GetUserStatsRequest) (*GetUserStatsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "get-user-stats"))

	stats, err := s.UserRepository.GetUserStatsCounts(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-user-stats-counts", zap.Error(err))
		return nil, ErrDatabaseError
	}

	return &GetUserStatsResponse{UserStats: stats}, nil
}

// GetUserConfigs returns supported user config presets and capabilities.
func (s *Service) GetUserConfigs(_ context.Context, _ *GetUserConfigsRequest) (*GetUserConfigsResponse, error) {
	defaultConfig := s.defaultConfig()
	configs := s.availableConfigs()
	availableConfigs := make([]AvailableUserConfig, 0, len(configs))
	for _, config := range configs {
		availableConfigs = append(availableConfigs, AvailableUserConfig{
			Type:   config.GetType(defaultConfig),
			Config: config.ToCapabilities(defaultConfig),
		})
	}

	return &GetUserConfigsResponse{
		DefaultConfigType: defaultConfig.GetType(DefaultUserConfig()),
		Configs:           availableConfigs,
	}, nil
}

// Helper methods

// defaultConfig reads configuration without mutating shared service state during requests.
func (s *Service) defaultConfig() *UserConfig {
	if s.Config == nil {
		return DefaultUserConfig()
	}
	if s.Config.Type == "" {
		config := *s.Config
		config.Type = UserConfigTypeCustom
		return &config
	}
	return s.Config
}

// availableConfigs returns the configured registry without a concurrent lazy write.
func (s *Service) availableConfigs() []*UserConfig {
	if len(s.Configs) == 0 {
		return []*UserConfig{s.defaultConfig()}
	}
	return s.Configs
}

func (s *Service) resolveRequestedConfig(configType string) (*UserConfig, error) {
	if configType == "" {
		return s.defaultConfig(), nil
	}

	for _, config := range s.availableConfigs() {
		if config.GetType(s.defaultConfig()) == configType {
			return config, nil
		}
	}

	return nil, ErrInvalidUserConfigType
}

func (s *Service) resolveStoredConfig(configType string) *UserConfig {
	config, err := s.resolveRequestedConfig(configType)
	if err != nil {
		return s.defaultConfig()
	}

	return config
}

func (s *Service) setUserDependencies(user *UniversalUser) *UniversalUser {
	config := s.resolveStoredConfig(user.Type)
	return user.SetDependencies(config, s.IDGenerator, s.TimeProvider, s.StringUtils)
}

// shouldBeAutoAdmin checks if email matches auto-admin regex
func (s *Service) shouldBeAutoAdmin(email string) bool {
	if s.AutoAdminEmailAddressRegex == "" {
		return false
	}

	matched, err := regexp.MatchString(s.AutoAdminEmailAddressRegex, email)
	if err != nil {
		return false
	}

	return matched
}
