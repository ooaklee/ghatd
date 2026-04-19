package user

import (
	"context"
	"errors"
	"math"
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
	GetUsersByRoles(ctx context.Context, roles []string, page, perPage int, order string) ([]UniversalUser, error)
	GetUsersByStatus(ctx context.Context, status string, page, perPage int, order string) ([]UniversalUser, error)
	SearchUsersByExtension(ctx context.Context, key string, value interface{}, page, perPage int) ([]UniversalUser, error)
}

// Service holds and manages user business logic
type Service struct {
	UserRepository             UserRepository
	AuditService               AuditService
	Config                     *UserConfig
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

	return &Service{
		UserRepository:             userRepository,
		AuditService:               auditService,
		Config:                     config,
		IDGenerator:                idGenerator,
		TimeProvider:               timeProvider,
		StringUtils:                stringUtils,
		AutoAdminEmailAddressRegex: autoAdminEmailAddressRegex,
	}
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "create-user")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Check if user already exists
	existingUser, _ := s.UserRepository.GetUserByEmail(ctx, req.Email, false)
	if existingUser != nil {
		log.Error("user-with-email-already-exists", zap.String("email", req.Email))
		return nil, errors.New(ErrKeyEmailAlreadyExists)
	}

	// Create new user with dependencies
	user := NewUniversalUser(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

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

		if s.Config.DefaultRole != "" {
			user.Roles = append(user.Roles, s.Config.DefaultRole)
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
		user.Status = s.Config.DefaultStatus
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

	if req.GenerateNanoID && s.Config.MultipleIdentifiers {
		user.GenerateNewNanoID()
	}

	// Set initial timestamps and state
	user.SetInitialState()

	user.Standardise()

	// Validate user
	if err := user.Validate(); err != nil {
		log.Error("user-validation-failed", zap.Error(err))
		return nil, errors.New(ErrKeyValidationFailed)
	}

	// Create user in repository
	createdUser, err := s.UserRepository.CreateUser(ctx, user)
	if err != nil {
		log.Error("failed-to-create-user", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
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

	log.Info("user-created-successfully", zap.String("user-id", createdUser.ID))

	return &CreateUserResponse{User: createdUser}, nil
}

// GetUserByID retrieves a user by ID
func (s *Service) GetUserByID(ctx context.Context, req *GetUserByIDRequest) (*GetUserByIDResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "create-user")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req.ID == "" {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed to get user by ID", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	return &GetUserByIDResponse{User: user}, nil
}

// GetUserByNanoID retrieves a user by nano ID
func (s *Service) GetUserByNanoID(ctx context.Context, req *GetUserByNanoIDRequest) (*GetUserByNanoIDResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "create-user")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req.NanoID == "" {
		return nil, errors.New(ErrKeyInvalidNanoID)
	}

	user, err := s.UserRepository.GetUserByNanoID(ctx, req.NanoID)
	if err != nil {
		log.Error("failed to get user by nano ID", zap.Error(err), zap.String("nano_id", req.NanoID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	return &GetUserByNanoIDResponse{User: user}, nil
}

// GetUserByEmail retrieves a user by email
func (s *Service) GetUserByEmail(ctx context.Context, req *GetUserByEmailRequest) (*GetUserByEmailResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-user-by-email")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req.Email == "" {
		return nil, errors.New(ErrKeyInvalidEmail)
	}

	user, err := s.UserRepository.GetUserByEmail(ctx, normaliseUserEmail(req.Email), true)
	if err != nil {
		log.Error("failed to get user by email", zap.Error(err), zap.String("email", req.Email))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	return &GetUserByEmailResponse{User: user}, nil
}

// UpdateUser updates an existing user
func (s *Service) UpdateUser(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "update-user")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	targetUserId := req.ID
	if req.User != nil && req.User.ID != "" {
		targetUserId = req.User.ID
	}

	// Get existing user
	user, err := s.UserRepository.GetUserByID(ctx, targetUserId)
	if err != nil {
		log.Error("failed-to-get-user-for-update", zap.Error(err), zap.String("id", targetUserId))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	if req.User != nil {
		userWithProvidedData := req.User

		userWithProvidedData.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

		if userWithProvidedData.Email != "" && userWithProvidedData.Email != user.Email {
			// Check if new email already exists
			existingUser, _ := s.UserRepository.GetUserByEmail(ctx, userWithProvidedData.Email, false)
			if existingUser != nil && existingUser.ID != user.ID {
				return nil, errors.New(ErrKeyEmailAlreadyExists)
			}
			user.Email = normaliseUserEmail(userWithProvidedData.Email)
		}

		userWithProvidedData.SetFullName()

		user = userWithProvidedData

	}

	if req.User == nil {
		user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

		// Update fields
		hasChanges := false

		if req.Email != "" && req.Email != user.Email {
			// Check if new email already exists
			existingUser, _ := s.UserRepository.GetUserByEmail(ctx, req.Email, false)
			if existingUser != nil && existingUser.ID != user.ID {
				return nil, errors.New(ErrKeyEmailAlreadyExists)
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
				log.Error("failed-to-update-user-status", zap.Error(err))
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
		log.Error("user-validation-failed", zap.Error(err))
		return nil, errors.New(ErrKeyValidationFailed)
	}

	// Ensure version is set to 2 for migrated users
	user.EnsureVersion()

	user.Standardise()

	// Update in repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		log.Error("failed-to-update-user", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
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

	log.Info("user-updated-successfully", zap.String("user-id", updatedUser.ID))

	return &UpdateUserResponse{User: updatedUser}, nil
}

// DeleteUser deletes a user
func (s *Service) DeleteUser(ctx context.Context, req *DeleteUserRequest) error {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "delete-user")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Verify user exists
	_, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("user-not-found", zap.Error(err), zap.String("id", req.ID))
		return errors.New(ErrKeyUserNotFound)
	}

	// Delete user
	err = s.UserRepository.DeleteUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-delete-user", zap.Error(err), zap.String("id", req.ID))
		return errors.New(ErrKeyDatabaseError)
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

	log.Info("user-deleted-successfully", zap.String("user-id", req.ID))

	return nil
}

// GetUsers retrieves users with filters and pagination
func (s *Service) GetUsers(ctx context.Context, req *GetUsersRequest) (*GetUsersResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-users")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

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
		RolesFilter:     req.RolesFilter,
		OnlyAdmin:       req.OnlyAdmin,
		EmailVerified:   req.EmailVerified,
		PhoneVerified:   req.PhoneVerified,
	}

	total, err := s.UserRepository.GetTotalUsers(ctx, totalReq)
	if err != nil {
		log.Error("failed-to-get-total-users", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Calculate total pages
	totalPages := int(math.Ceil(float64(total) / float64(req.PerPage)))

	// Validate page is in range
	if req.Page > totalPages && totalPages > 0 {
		return nil, errors.New(ErrKeyPageOutOfRange)
	}

	// Get users
	users, err := s.UserRepository.GetUsers(ctx, req)
	if err != nil {
		log.Error("failed-to-get-users", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Reinject dependencies for all users
	for i := range users {
		users[i].SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
	}

	meta := &PaginationMetadata{
		Page:           req.Page,
		PerPage:        req.PerPage,
		TotalResources: total,
		TotalPages:     totalPages,
	}

	return &GetUsersResponse{
		Users: users,
		Meta:  meta,
	}, nil
}

// GetTotalUsers retrieves the total count of users matching filters
func (s *Service) GetTotalUsers(ctx context.Context, req *GetTotalUsersRequest) (*GetTotalUsersResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-total-users")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	total, err := s.UserRepository.GetTotalUsers(ctx, req)
	if err != nil {
		log.Error("failed-to-get-total-users", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	return &GetTotalUsersResponse{Total: total}, nil
}

// UpdateUserStatus updates a user's status
func (s *Service) UpdateUserStatus(ctx context.Context, req *UpdateUserStatusRequest) (*UpdateUserStatusResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "update-user-status")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-status-update", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Update status
	updatedUser, err := user.UpdateStatus(req.DesiredStatus)
	if err != nil {
		log.Error("failed-to-update-user-status", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedUser, err = s.UserRepository.UpdateUser(ctx, updatedUser)
	if err != nil {
		log.Error("failed-to-save-user-after-status-update", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
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

	log.Info("user-status-updated-successfully", zap.String("user-id", updatedUser.ID), zap.String("status", req.DesiredStatus))

	return &UpdateUserStatusResponse{User: updatedUser}, nil
}

// AddUserRole adds a role to a user
func (s *Service) AddUserRole(ctx context.Context, req *AddUserRoleRequest) (*AddUserRoleResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "add-user-role")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-adding-role", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Add role
	user.AddRole(req.Role)

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		log.Error("failed-to-save-user-after-adding-role", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
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

	log.Info("user-role-added-successfully", zap.String("user-id", updatedUser.ID), zap.String("role", req.Role))

	return &AddUserRoleResponse{User: updatedUser}, nil
}

// RemoveUserRole removes a role from a user
func (s *Service) RemoveUserRole(ctx context.Context, req *RemoveUserRoleRequest) (*RemoveUserRoleResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "remove-user-role")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-removing-role", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Remove role
	user.RemoveRole(req.Role)

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		log.Error("failed-to-save-user-after-removing-role", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
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

	log.Info("user-role-removed-successfully", zap.String("user-id", updatedUser.ID), zap.String("role", req.Role))

	return &RemoveUserRoleResponse{User: updatedUser}, nil
}

// VerifyUserEmail marks a user's email as verified
func (s *Service) VerifyUserEmail(ctx context.Context, req *VerifyUserEmailRequest) (*VerifyUserEmailResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "verify-user-email")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-email-verification", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Verify email
	user.VerifyEmail()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		log.Error("failed-to-save-user-after-email-verification", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
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

	log.Info("user-email-verified-successfully", zap.String("user-id", updatedUser.ID))

	return &VerifyUserEmailResponse{User: updatedUser}, nil
}

// UnverifyUserEmail marks a user's email as unverified
func (s *Service) UnverifyUserEmail(ctx context.Context, req *UnverifyUserEmailRequest) (*UnverifyUserEmailResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "unverify-user-email")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-email-unverification", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Unverify email
	user.UnverifyEmail()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		log.Error("failed-to-save-user-after-email-unverification", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
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

	log.Info("user-email-unverified-successfully", zap.String("user-id", updatedUser.ID))

	return &UnverifyUserEmailResponse{User: updatedUser}, nil
}

// VerifyUserPhone marks a user's phone as verified
func (s *Service) VerifyUserPhone(ctx context.Context, req *VerifyUserPhoneRequest) (*VerifyUserPhoneResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "veify-user-phone")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-phone-verification", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Verify phone
	user.VerifyPhone()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		log.Error("failed-to-save-user-after-phone-verification", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
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

	log.Info("user-phone-verified-successfully", zap.String("user-id", updatedUser.ID))

	return &VerifyUserPhoneResponse{User: updatedUser}, nil
}

// RecordUserLogin records a user login event
func (s *Service) RecordUserLogin(ctx context.Context, req *RecordUserLoginRequest) (*RecordUserLoginResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "record-user-login")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-login-recording", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Update last login timestamp
	user.SetLastLoginAtNow()
	user.SetUpdatedAtNow()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		log.Error("failed-to-save-user-after-login-recording", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	log.Info("user-login-recorded-successfully", zap.String("user-id", updatedUser.ID))

	return &RecordUserLoginResponse{User: updatedUser}, nil
}

// GetUserProfile retrieves a user's profile
func (s *Service) GetUserProfile(ctx context.Context, req *GetUserProfileRequest) (*GetUserProfileResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-user-profile")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	userResp, err := s.GetUserByID(ctx, &GetUserByIDRequest{ID: req.ID})
	if err != nil {
		log.Error("failed-to-get-user-for-profile-retrieval", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	profile := userResp.User.GetAsProfile()

	return &GetUserProfileResponse{Profile: profile}, nil
}

// GetUserMicroProfile retrieves a user's micro profile
func (s *Service) GetUserMicroProfile(ctx context.Context, req *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-user-micro-profile")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	userResp, err := s.GetUserByID(ctx, &GetUserByIDRequest{ID: req.ID})
	if err != nil {
		log.Error("failed-to-get-user-for-micro-profile-retrieval", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	microProfile := userResp.User.GetAsMicroProfile()

	return &GetUserMicroProfileResponse{MicroProfile: microProfile}, nil
}

// SetUserExtension sets an extension field value
func (s *Service) SetUserExtension(ctx context.Context, req *SetUserExtensionRequest) (*SetUserExtensionResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "set-user-extension")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-setting-extension", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Set extension
	if user.Extensions == nil {
		user.Extensions = make(map[string]interface{})
	}
	user.Extensions[req.Key] = req.Value
	user.SetUpdatedAtNow()

	// Save to repository
	updatedUser, err := s.UserRepository.UpdateUser(ctx, user)
	if err != nil {
		log.Error("failed-to-save-user-after-setting-extension", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	log.Info("user-extension-set-successfully", zap.String("user-id", updatedUser.ID), zap.String("key", req.Key))

	return &SetUserExtensionResponse{User: updatedUser}, nil
}

// GetUserExtension retrieves an extension field value
func (s *Service) GetUserExtension(ctx context.Context, req *GetUserExtensionRequest) (*GetUserExtensionResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-user-extension")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-getting-extension", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Get extension value
	value, exists := user.Extensions[req.Key]
	if !exists {
		return nil, errors.New(ErrKeyExtensionNotFound)
	}

	return &GetUserExtensionResponse{Key: req.Key, Value: value}, nil
}

// UpdateUserPersonalInfo updates a user's personal information
func (s *Service) UpdateUserPersonalInfo(ctx context.Context, req *UpdateUserPersonalInfoRequest) (*UpdateUserPersonalInfoResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "update-user-personal-info")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-updating-personal-info", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

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
		log.Error("failed-to-save-user-after-updating-personal-info", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
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

	log.Info("user-personal-info-updated-successfully", zap.String("user-id", updatedUser.ID))

	return &UpdateUserPersonalInfoResponse{User: updatedUser}, nil
}

// ValidateUser validates a user
func (s *Service) ValidateUser(ctx context.Context, req *ValidateUserRequest) (*ValidateUserResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "validate-user")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Get user
	user, err := s.UserRepository.GetUserByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-user-for-validation", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	// Reinject dependencies
	user.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Validate
	validationErr := user.Validate()

	if validationErr != nil {
		log.Error("user-validation-failed", zap.Error(validationErr))
		errorStr := validationErr.Error()
		return &ValidateUserResponse{
			Valid:  false,
			Errors: []string{errorStr},
		}, nil
	}

	return &ValidateUserResponse{Valid: true, Errors: []string{}}, nil
}

// SearchUsersByExtension searches for users by extension field value
func (s *Service) SearchUsersByExtension(ctx context.Context, req *SearchUsersByExtensionRequest) (*SearchUsersByExtensionResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "search-users-by-extension")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Validate pagination
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PerPage < 1 || req.PerPage > 100 {
		req.PerPage = 25
	}

	// Search
	totalMatchingUsers, err := s.UserRepository.GetTotalUsers(ctx, &GetTotalUsersRequest{
		ExtensionKey:   req.Key,
		ExtensionValue: req.Value,
	})
	if err != nil {
		log.Error("failed-to-get-total-users-for-extension-search", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}
	users, err := s.UserRepository.SearchUsersByExtension(ctx, req.Key, req.Value, req.Page, req.PerPage)
	if err != nil {
		log.Error("failed-to-search-users-by-extension", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Reinject dependencies for all users
	for i := range users {
		users[i].SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
	}

	// handle page pagination
	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{PerPage: req.PerPage, Page: req.Page}, users, int(totalMatchingUsers))
	if err != nil {
		return nil, err
	}

	return &SearchUsersByExtensionResponse{
		Users: paginatedResponse.Resources,
		Meta: &PaginationMetadata{
			Page:           paginatedResponse.Page,
			PerPage:        paginatedResponse.ResourcePerPage,
			TotalResources: int64(paginatedResponse.Total),
			TotalPages:     paginatedResponse.TotalPages,
		},
	}, nil
}

// BulkUpdateUsersStatus updates status for multiple users
func (s *Service) BulkUpdateUsersStatus(ctx context.Context, req *BulkUpdateUsersStatusRequest) (*BulkUpdateUsersStatusResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "bulk-update-users-status")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

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
			log.Warn("failed-to-update-user-status-in-bulk-operation", zap.String("user-id", userID), zap.Error(err))
		} else {
			successCount++
		}
	}

	log.Info("bulk-status-update-completed", zap.Int("success", successCount), zap.Int("failures", failureCount))

	return &BulkUpdateUsersStatusResponse{
		UpdatedCount: successCount,
		FailedIDs:    failedIDs,
	}, nil
}

// GetUsersByRoles retrieves users with specific roles
func (s *Service) GetUsersByRoles(ctx context.Context, req *GetUsersByRolesRequest) (*GetUsersByRolesResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-users-by-roles")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Validate pagination
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PerPage < 1 || req.PerPage > 100 {
		req.PerPage = 25
	}

	// Get users
	totalMatchingUsers, err := s.UserRepository.GetTotalUsers(ctx, &GetTotalUsersRequest{
		RolesFilter: req.Roles,
	})
	if err != nil {
		log.Error("failed-to-get-total-users-for-roles-search", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}
	users, err := s.UserRepository.GetUsersByRoles(ctx, req.Roles, req.Page, req.PerPage, req.Order)
	if err != nil {
		log.Error("failed-to-get-users-by-roles", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Reinject dependencies for all users
	for i := range users {
		users[i].SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
	}

	// handle page pagination
	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{PerPage: req.PerPage, Page: req.Page}, users, int(totalMatchingUsers))
	if err != nil {
		return nil, err
	}

	return &GetUsersByRolesResponse{
		Users: paginatedResponse.Resources,
		Meta: &PaginationMetadata{
			Page:           paginatedResponse.Page,
			PerPage:        paginatedResponse.ResourcePerPage,
			TotalResources: int64(paginatedResponse.Total),
			TotalPages:     paginatedResponse.TotalPages,
		},
	}, nil
}

// GetUsersByStatus retrieves users with a specific status
func (s *Service) GetUsersByStatus(ctx context.Context, req *GetUsersByStatusRequest) (*GetUsersByStatusResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-users-by-status")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Validate pagination
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PerPage < 1 || req.PerPage > 100 {
		req.PerPage = 25
	}

	// Get users
	totalMatchingUsers, err := s.UserRepository.GetTotalUsers(ctx, &GetTotalUsersRequest{
		StatusFilter: req.Status,
	})
	if err != nil {
		log.Error("failed-to-get-total-users-for-status-search", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}
	users, err := s.UserRepository.GetUsersByStatus(ctx, req.Status, req.Page, req.PerPage, req.Order)
	if err != nil {
		log.Error("failed-to-get-users-by-status", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Reinject dependencies for all users
	for i := range users {
		users[i].SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
	}

	// handle page pagination
	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{PerPage: req.PerPage, Page: req.Page}, users, int(totalMatchingUsers))
	if err != nil {
		return nil, err
	}

	return &GetUsersByStatusResponse{
		Users: paginatedResponse.Resources,
		Meta: &PaginationMetadata{
			Page:           paginatedResponse.Page,
			PerPage:        paginatedResponse.ResourcePerPage,
			TotalResources: int64(paginatedResponse.Total),
			TotalPages:     paginatedResponse.TotalPages,
		},
	}, nil
}

// Helper methods

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
