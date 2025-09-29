package contacter

import (
	"context"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// contacterRepository is the expected methods needed to
// interact with the database
type contacterRepository interface {
	GetTotalComms(ctx context.Context, req *GetTotalCommsRequest) (int64, error)
	GetComms(ctx context.Context, req *GetCommsRequest) ([]Comms, error)
	CreateComms(ctx context.Context, newComms *Comms) (*Comms, error)
}

// Service represents the contacter service
type Service struct {
	contacterRepository contacterRepository
}

// NewService returns a new instance of the contacter service
func NewService(contacterRepository contacterRepository) *Service {
	return &Service{
		contacterRepository: contacterRepository,
	}
}

// CreateComms creates a new comms
func (s *Service) CreateComms(ctx context.Context, req *CreateCommsRequest) (*CreateCommsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		newComms *Comms = &Comms{
			Message: req.Message,
			Meta:    req.Meta,
			UserId:  req.UserId,
		}
	)

	logger.Debug("initiating-create-comms-request", zap.Any("request", req))

	newComms = newComms.SetCommsType(string(req.Type)).SetStandardisedEmail(req.Email).SetStandardisedFullName(req.FullName)

	if req.UserId != "" {
		newComms.UserLoggedIn = true
	}

	createdComms, err := s.contacterRepository.CreateComms(ctx, newComms)
	if err != nil {
		logger.Error("failed-to-create-comms-error-creating-comms", zap.Any("request", req), zap.Error(err))
		return &CreateCommsResponse{}, err
	}

	logger.Debug("create-comms-request-successful", zap.Any("request", req), zap.Any("created-comms", createdComms))

	return &CreateCommsResponse{
		Comms: createdComms,
	}, nil
}

// GetComms returns a list of comms
func (s *Service) GetComms(ctx context.Context, req *GetCommsRequest) (*GetCommsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	// default
	if req.Order == "" {
		req.Order = "created_at_desc"
	}

	if req.PerPage == 0 {
		req.PerPage = 25
	}

	if req.Page == 0 {
		req.Page = 1
	}

	// get count of all comms
	getTotalCommsRequest := &GetTotalCommsRequest{
		FullName: req.FullName,
		Emails:   toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.FromEmails),
		CommsTypes: func(types []string) []CommsType {
			var commsTypes []CommsType
			for _, typ := range types {
				commsTypes = append(commsTypes, CommsType(typ))
			}
			return commsTypes
		}(
			toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithTypes),
		),
		MessageContains:       req.MessageContains,
		DisplayedAs:           toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.DisplayedAs),
		CustomSubjectContains: req.CustomSubjectContains,
		CreatedAtFrom:         req.CreatedAtFrom,
		CreatedAtTo:           req.CreatedAtTo,
		UserLoggedIn:          req.UserLoggedIn,
		UserNotLoggedIn:       req.UserNotLoggedIn,
	}
	totalComms, err := s.contacterRepository.GetTotalComms(ctx, getTotalCommsRequest)
	if err != nil {
		logger.Error("failed-to-get-comms-request-error-getting-total-comms", zap.Any("request", req), zap.Any("get-total-comms-request", getTotalCommsRequest), zap.Error(err))
		return &GetCommsResponse{}, err
	}

	req.TotalCount = int(totalComms)
	logger.Debug("handling-get-comms-request-total-comms-found", zap.Int64("total", totalComms), zap.Any("request", req))

	comms, err := s.contacterRepository.GetComms(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-comms-request-error-getting-comms", zap.Any("request", req), zap.Error(err))
		return &GetCommsResponse{}, err
	}

	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: req.PerPage,
		Page:    req.Page,
	}, comms, req.TotalCount)

	if err != nil {
		return nil, err
	}

	return &GetCommsResponse{
		Total:      paginatedResponse.Total,
		TotalPages: paginatedResponse.TotalPages,
		Comms:      paginatedResponse.Resources,
		Page:       paginatedResponse.Page,
		PerPage:    paginatedResponse.ResourcePerPage,
	}, nil

}
