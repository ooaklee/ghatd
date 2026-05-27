package user

import (
	"context"
	"regexp"

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
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "create-user"))
	config, err := s.resolveRequestedConfig(req.Type)
	if err != nil {
		logger.Error("invalid-user-config-type", zap.String("config-type", req.Type), zap.Error(err))
		return nil, err
	}

	// Check if user already exists
	existingUser, _ := s.UserRepository.GetUserByEmail(ctx, req.Email, false)
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

	// Create user in repository
	createdUser, err := s.UserRepository.CreateUser(ctx, user)
	if err != nil {
		logger.Error("failed-to-create-user", zap.Error(err))
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

// GetUserByID retrieves a user by ID
func (s *Service) GetUserByID(ctx context.Context, req *GetUserByIDRequest) (*GetUserByIDResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "create-user"))

	if req.ID == "" {
		return nil, ErrInvalidUserID
	}

	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed to get user by ID", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	return &GetUserByIDResponse{User: user}, nil
}

// GetUserByNanoID retrieves a user by nano ID
func (s *Service) GetUserByNanoID(ctx context.Context, req *GetUserByNanoIDRequest) (*GetUserByNanoIDResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "create-user"))

	if req.NanoID == "" {
		return nil, ErrInvalidNanoID
	}

	user, err := s.UserRepository.GetUserByNanoID(ctx, req.NanoID)
	if err != nil {
		logger.Error("failed to get user by nano ID", zap.Error(err), zap.String("nano-id", req.NanoID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	return &GetUserByNanoIDResponse{User: user}, nil
}

// GetUserByEmail retrieves a user by email
func (s *Service) GetUserByEmail(ctx context.Context, req *GetUserByEmailRequest) (*GetUserByEmailResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "get-user-by-email"))

	if req.Email == "" {
		return nil, ErrInvalidEmail
	}

	user, err := s.UserRepository.GetUserByEmail(ctx, normaliseUserEmail(req.Email), true)
	if err != nil {
		logger.Error("failed to get user by email", append(emailLogFields("email", req.Email), zap.Error(err))...)
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	return &GetUserByEmailResponse{User: user}, nil
}

// UpdateUser updates an existing user
func (s *Service) UpdateUser(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "update-user"))

	targetUserId := req.ID
	if req.User != nil && req.User.ID != "" {
		targetUserId = req.User.ID
	}

	// Get existing user
	user, err := s.UserRepository.GetUserByID(ctx, targetUserId)
	if err != nil {
		logger.Error("failed-to-get-user-for-update", zap.Error(err), zap.String("id", targetUserId))
		return nil, ErrUserNotFound
	}

	if req.User != nil {
		userWithProvidedData := req.User
		requestedType := userWithProvidedData.Type
		if requestedType == "" {
			requestedType = user.Type
		}

		config, err := s.resolveRequestedConfig(requestedType)
		if err != nil {
			logger.Error("invalid-user-config-type", zap.String("config-type", requestedType), zap.Error(err))
			return nil, err
		}

		userWithProvidedData.SetDependencies(config, s.IDGenerator, s.TimeProvider, s.StringUtils)
		userWithProvidedData.Type = config.GetType(s.defaultConfig())

		if userWithProvidedData.Email != "" && userWithProvidedData.Email != user.Email {
			// Check if new email already exists
			existingUser, _ := s.UserRepository.GetUserByEmail(ctx, userWithProvidedData.Email, false)
			if existingUser != nil && existingUser.ID != user.ID {
				return nil, ErrEmailAlreadyExists
			}
			user.Email = normaliseUserEmail(userWithProvidedData.Email)
		}

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
				logger.Error("invalid-user-config-type", zap.String("config-type", req.Type), zap.Error(err))
				return nil, err
			}

			user.Type = config.GetType(s.defaultConfig())
			user.SetDependencies(config, s.IDGenerator, s.TimeProvider, s.StringUtils)
			hasChanges = true
		}

		if req.Email != "" && req.Email != user.Email {
			// Check if new email already exists
			existingUser, _ := s.UserRepository.GetUserByEmail(ctx, req.Email, false)
			if existingUser != nil && existingUser.ID != user.ID {
				return nil, ErrEmailAlreadyExists
			}
			user.Email = normaliseUserEmail(req.Email)
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
				logger.Error("failed-to-update-user-status", zap.Error(err))
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
		logger.Error("user-validation-failed", zap.Error(err))
		return nil, ErrValidationFailed
	}

	// Ensure version is set to 2 for migrated users
	user.EnsureVersion()

	user.Standardise()

	// Update in repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		logger.Error("failed-to-update-user", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
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

// UpdateUserStatus updates a user's status
func (s *Service) UpdateUserStatus(ctx context.Context, req *UpdateUserStatusRequest) (*UpdateUserStatusResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "update-user-status"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-status-update", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	// Update status
	updatedUser, err := user.UpdateStatus(req.DesiredStatus)
	if err != nil {
		logger.Error("failed-to-update-user-status", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedUser, err = s.UserRepository.UpdateUser(ctx, updatedUser)
	if err != nil {
		logger.Error("failed-to-save-user-after-status-update", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:     "user.status_updated",
			TargetId:   updatedUser.ID,
			TargetType: audit.TargetType("user"),
			Details:    map[string]interface{}{"user_id": updatedUser.ID, "new_status": req.DesiredStatus},
		})
	}

	logger.Info("user-status-updated-successfully", zap.String("user-id", updatedUser.ID), zap.String("status", req.DesiredStatus))

	return &UpdateUserStatusResponse{User: updatedUser}, nil
}

// AddUserRole adds a role to a user
func (s *Service) AddUserRole(ctx context.Context, req *AddUserRoleRequest) (*AddUserRoleResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "add-user-role"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-adding-role", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	// Add role
	user.AddRole(req.Role)

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		logger.Error("failed-to-save-user-after-adding-role", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:     "user.role_added",
			TargetId:   updatedUser.ID,
			TargetType: audit.TargetType("user"),
			Details:    map[string]interface{}{"user_id": updatedUser.ID, "role": req.Role},
		})
	}

	logger.Info("user-role-added-successfully", zap.String("user-id", updatedUser.ID), zap.String("role", req.Role))

	return &AddUserRoleResponse{User: updatedUser}, nil
}

// RemoveUserRole removes a role from a user
func (s *Service) RemoveUserRole(ctx context.Context, req *RemoveUserRoleRequest) (*RemoveUserRoleResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/user/v2").With(zap.String("operation", "remove-user-role"))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-user-for-removing-role", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrUserNotFound
	}

	// Reinject dependencies
	s.setUserDependencies(user)

	// Remove role
	user.RemoveRole(req.Role)

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		logger.Error("failed-to-save-user-after-removing-role", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:     "user.role_removed",
			TargetId:   updatedUser.ID,
			TargetType: audit.TargetType("user"),
			Details:    map[string]interface{}{"user_id": updatedUser.ID, "role": req.Role},
		})
	}

	logger.Info("user-role-removed-successfully", zap.String("user-id", updatedUser.ID), zap.String("role", req.Role))

	return &RemoveUserRoleResponse{User: updatedUser}, nil
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

func (s *Service) defaultConfig() *UserConfig {
	if s.Config == nil {
		s.Config = DefaultUserConfig()
	}

	s.Config = ensureUserConfigType(s.Config)
	return s.Config
}

func (s *Service) availableConfigs() []*UserConfig {
	if len(s.Configs) == 0 {
		s.Configs = registerUserConfigs(s.defaultConfig())
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
