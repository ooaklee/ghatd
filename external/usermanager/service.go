package usermanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/contacter"
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

// ContacterService expected methods of a valid contacter service
type ContacterService interface {
	CreateComms(ctx context.Context, req *contacter.CreateCommsRequest) (*contacter.CreateCommsResponse, error)
	GetComms(ctx context.Context, req *contacter.GetCommsRequest) (*contacter.GetCommsResponse, error)
}

// Service holds and manages usermanager business logic
type Service struct {
	UserService      UserService
	ApiTokenService  ApiTokenService
	AuditService     AuditService
	ContacterService ContacterService
}

// NewServiceRequest holds all expected dependencies for an usermanager service
type NewServiceRequest struct {

	// UserService handles updating user information
	UserService UserService

	// ApiTokenService handles api token actions
	ApiTokenService ApiTokenService

	// AuditService handles audit actions
	AuditService AuditService

	// ContacterService handles comms actions
	ContacterService ContacterService
}

// NewService creates usermanager service
func NewService(r *NewServiceRequest) *Service {
	return &Service{
		UserService:      r.UserService,
		ApiTokenService:  r.ApiTokenService,
		AuditService:     r.AuditService,
		ContacterService: r.ContacterService,
	}
}

// UpdateUserProfile handles the business logic of updating the requesting user's profile
func (s *Service) UpdateUserProfile(ctx context.Context, r *UpdateUserProfileRequest) (*UpdateUserProfileResponse, error) {
	serviceResponse, err := s.UserService.UpdateUser(ctx, &user.UpdateUserRequest{
		Id:        r.UserId,
		FirstName: r.FirstName,
		LastName:  r.LastName,
	})
	if err != nil {
		return nil, err
	}

	return &UpdateUserProfileResponse{
		UpdateUserResponse: serviceResponse,
	}, nil
}

// GetUserMicroProfile handles the business logic of fetching the requesting user's micro profile
func (s *Service) GetUserMicroProfile(ctx context.Context, r *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error) {

	serviceResponse, err := s.UserService.GetMicroProfile(ctx, &user.GetMicroProfileRequest{
		Id: r.UserId,
	})
	if err != nil {
		return nil, err
	}

	return &GetUserMicroProfileResponse{
		GetMicroProfileResponse: serviceResponse,
	}, nil
}

// GetUserProfile handles the business logic of fetching the requesting user's profile
func (s *Service) GetUserProfile(ctx context.Context, r *GetUserProfileRequest) (*GetUserProfileResponse, error) {

	serviceResponse, err := s.UserService.GetProfile(ctx, &user.GetProfileRequest{
		Id: r.UserId,
	})
	if err != nil {
		return nil, err
	}

	return &GetUserProfileResponse{
		GetProfileResponse: serviceResponse,
	}, nil
}

// DeleteUserPermanently handles the business logic of deleting user and all of their resource on the platform
// TODO: Add audit logs, add more resource types
func (s *Service) DeleteUserPermanently(ctx context.Context, r *DeleteUserPermanentlyRequest) error {

	var loggr = logger.AcquireFrom(ctx)
	var err error

	loggr.Warn("wiping-user-and-resources-from-platform-started", zap.String("user-id", r.UserId))

	loggr.Info("initiate-wiping-user-account", zap.String("user-id", r.UserId))
	err = s.UserService.DeleteUser(ctx, &user.DeleteUserRequest{Id: r.UserId})
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

// CreateComms handles the logic of creating a comms
func (s *Service) CreateComms(ctx context.Context, req *CreateCommsRequest) (*CreateCommsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	logger.Info("initiating-create-comms-request", zap.Any("request", req))

	createdCommsResponse, err := s.ContacterService.CreateComms(ctx, req.CreateCommsRequest)
	if err != nil {
		logger.Error("failed-to-create-comms-error-creating-comms", zap.Any("request", req), zap.Error(err))
		return &CreateCommsResponse{}, err
	}

	return &CreateCommsResponse{
		Comms: createdCommsResponse.Comms,
	}, nil
}

// GetComms handles the logic of getting a comms
func (s *Service) GetComms(ctx context.Context, req *GetCommsRequest) (*GetCommsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		response = GetCommsResponse{
			Comms: []contacter.Comms{},
		}
	)

	logger.Info("initiating-get-comms-request", zap.Any("request", req))

	commsResponse, err := s.ContacterService.GetComms(ctx, req.GetCommsRequest)
	if err != nil {
		logger.Error("failed-to-get-comms-error-getting-comms", zap.Any("request", req), zap.Error(err))
		return &GetCommsResponse{}, err
	}

	response.Comms = commsResponse.Comms
	response.Meta = commsResponse.GetMetaData()

	return &response, nil
}
