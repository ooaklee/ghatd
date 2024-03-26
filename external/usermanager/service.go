package usermanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/user"
	"go.uber.org/zap"
)

// UserService expected methods of a valid user service
type UserService interface {
	GetMicroProfile(ctx context.Context, r *user.GetMicroProfileRequest) (*user.GetMicroProfileResponse, error)
	GetProfile(ctx context.Context, r *user.GetProfileRequest) (*user.GetProfileResponse, error)
	UpdateUser(ctx context.Context, r *user.UpdateUserRequest) (*user.UpdateUserResponse, error)
	DeleteUser(ctx context.Context, r *user.DeleteUserRequest) error
}

// ApiTokenService expected methods of a valid api token service
type ApiTokenService interface {
	DeleteApiTokensByOwnerId(ctx context.Context, ownerId string) error
	GetTotalApiTokens(ctx context.Context, r *apitoken.GetTotalApiTokensRequest) (int64, error)
}

// AuditService expected methods of a valid audit service
type AuditService interface {
	GetTotalAuditLogEvents(ctx context.Context, r *audit.GetTotalAuditLogEventsRequest) (int64, error)
}

// Service holds and manages usermanager business logic
type Service struct {
	UserService     UserService
	ApiTokenService ApiTokenService
	AuditService    AuditService
}

// NewServiceRequest holds all expected dependencies for an usermanager service
type NewServiceRequest struct {

	// UserService handles updating user information
	UserService UserService

	// ApiTokenService handles api token actions
	ApiTokenService ApiTokenService

	// AuditService handles affiramtion actions
	AuditService AuditService
}

// NewService creates usermanager service
func NewService(r *NewServiceRequest) *Service {
	return &Service{
		UserService:     r.UserService,
		ApiTokenService: r.ApiTokenService,
		AuditService:    r.AuditService,
	}
}

// UpdateUserProfile handles the business logic of updating the requesting user's profile
func (s *Service) UpdateUserProfile(ctx context.Context, r *UpdateUserProfileRequest) (*UpdateUserProfileResponse, error) {
	serviceResponse, err := s.UserService.UpdateUser(ctx, &user.UpdateUserRequest{
		ID:        r.UserID,
		FirstName: r.FirstName,
		LastName:  r.LastName,
	})
	if err != nil {
		return nil, err
	}

	return &UpdateUserProfileResponse{
		UserProfile: &user.UserProfile{
			ID:            serviceResponse.User.ID,
			FirstName:     serviceResponse.User.FirstName,
			LastName:      serviceResponse.User.LastName,
			Status:        serviceResponse.User.Status,
			Roles:         serviceResponse.User.Roles,
			Email:         serviceResponse.User.Email,
			EmailVerified: serviceResponse.User.Verified.EmailVerified,
			UpdatedAt:     serviceResponse.User.Meta.UpdatedAt,
		},
	}, nil
}

// GetUserMicroProfile handles the business logic of fetching the requesting user's micro profile
func (s *Service) GetUserMicroProfile(ctx context.Context, r *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error) {

	serviceResponse, err := s.UserService.GetMicroProfile(ctx, &user.GetMicroProfileRequest{
		ID: r.UserID,
	})
	if err != nil {
		return nil, err
	}

	return &GetUserMicroProfileResponse{
		UserMicroProfile: &user.UserMicroProfile{
			ID:     serviceResponse.MicroProfile.ID,
			Roles:  serviceResponse.MicroProfile.Roles,
			Status: serviceResponse.MicroProfile.Status,
		},
	}, nil
}

// GetUserProfile handles the business logic of fetching the requesting user's profile
func (s *Service) GetUserProfile(ctx context.Context, r *GetUserProfileRequest) (*GetUserProfileResponse, error) {

	serviceResponse, err := s.UserService.GetProfile(ctx, &user.GetProfileRequest{
		ID: r.UserID,
	})
	if err != nil {
		return nil, err
	}

	return &GetUserProfileResponse{
		UserProfile: &serviceResponse.Profile,
	}, nil
}

// DeleteUserPermanently handles the business logic of deleting user and all of their resource on the platform
// TODO: Add audit logs, add more resource types
func (s *Service) DeleteUserPermanently(ctx context.Context, r *DeleteUserPermanentlyRequest) error {

	var loggr = logger.AcquireFrom(ctx)
	var err error

	loggr.Warn("wiping-user-and-resources-from-platform-started", zap.String("user-id", r.UserId))

	loggr.Info("initiate-wiping-user-account", zap.String("user-id", r.UserId))
	err = s.UserService.DeleteUser(ctx, &user.DeleteUserRequest{ID: r.UserId})
	if err != nil {
		return err
	}
	loggr.Info("completed-wiping-user-account", zap.String("user-id", r.UserId))

	loggr.Info("initiate-wiping-user-owned-api-tokens", zap.String("user-id", r.UserId))
	err = s.ApiTokenService.DeleteApiTokensByOwnerId(ctx, r.UserId)
	if err != nil {
		return err
	}
	loggr.Info("completed-wiping-user-owned-api-tokens", zap.String("user-id", r.UserId))

	loggr.Info("wiping-user-and-resources-from-platform-completed", zap.String("user-id", r.UserId))

	return nil
}
