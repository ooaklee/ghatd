// Package contacter implements contact management functionality for managing
// user contacts, addresses, and communication preferences.
package contacter

import (
	"context"
	"errors"

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
	UpdateComms(ctx context.Context, comms *Comms) (*Comms, error)
	GetCommsByIds(ctx context.Context, commsIds []string) ([]Comms, error)
	GetCommsStatsCounts(ctx context.Context, req *GetCommsStatsRequest) (*CommsStats, error)
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
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/contacter")

		newComms *Comms = &Comms{
			Message: req.Message,
			Meta:    req.Meta,
			UserId:  req.UserId,
		}
	)

	logger.Debug("initiating-create-comms-request", zap.Any("request", safeLogValue(req)))

	newComms = newComms.SetCommsType(string(req.Type)).SetStandardisedEmail(req.Email).SetStandardisedFullName(req.FullName)

	// If UserId is not provided, FullName and Email are required
	if req.UserId == "" {
		var err error = nil
		if req.FullName == "" {
			err = errors.Join(err, ErrFullNameRequired)
		}
		if req.Email == "" {
			err = errors.Join(err, ErrEmailRequired)
		}
		if err != nil {
			return nil, err
		}
	}

	if req.UserId != "" {
		newComms.UserLoggedIn = true
	}

	createdComms, err := s.contacterRepository.CreateComms(ctx, newComms)
	if err != nil {
		logger.Error("failed-to-create-comms-error-creating-comms", zap.Any("request", safeLogValue(req)), zap.Error(err))
		return &CreateCommsResponse{}, err
	}

	logger.Debug("create-comms-request-successful", zap.Any("request", safeLogValue(req)), zap.Any("created-comms", safeLogValue(createdComms)))

	return &CreateCommsResponse{
		Comms: createdComms,
	}, nil
}

// GetComms returns a list of comms
func (s *Service) GetComms(ctx context.Context, req *GetCommsRequest) (*GetCommsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/contacter")
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
		logger.Error("failed-to-get-comms-request-error-getting-total-comms", zap.Any("request", safeLogValue(req)), zap.Any("get-total-comms-request", safeLogValue(getTotalCommsRequest)), zap.Error(err))
		return &GetCommsResponse{}, err
	}

	req.TotalCount = int(totalComms)
	logger.Debug("handling-get-comms-request-total-comms-found", zap.Int64("total", totalComms), zap.Any("request", safeLogValue(req)))

	comms, err := s.contacterRepository.GetComms(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-comms-request-error-getting-comms", zap.Any("request", safeLogValue(req)), zap.Error(err))
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

// UpdateComms updates an existing comms with admin information
func (s *Service) UpdateComms(ctx context.Context, req *UpdateCommsRequest) (*UpdateCommsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/contacter")
	)

	logger.Debug("initiating-update-comms-request", zap.Any("request", safeLogValue(req)))

	if req.CommsId == "" {
		return nil, ErrCommsIdRequired
	}

	// Fetch existing comms to preserve existing data
	existingCommsSlice, err := s.contacterRepository.GetCommsByIds(ctx, []string{req.CommsId})
	if err != nil {
		logger.Error("failed-to-update-comms-error-fetching-existing-comms", zap.String("comms_id", req.CommsId), zap.Error(err))
		return &UpdateCommsResponse{}, err
	}

	if len(existingCommsSlice) == 0 {
		return nil, ErrCommsNotFound
	}

	comms := existingCommsSlice[0]

	// Update only the admin fields present on the request payload.
	if req.AdminNotes != nil {
		comms.AdminNotes = *req.AdminNotes
	}

	if req.AdminReply != nil {
		comms.AdminReply = *req.AdminReply
	}

	if req.LinkedCommsIds != nil {
		comms.LinkedCommsIds = *req.LinkedCommsIds
	}

	// Set reached out timestamp if the provided flag is true and it wasn't previously set.
	if req.ReachedOut != nil && *req.ReachedOut && comms.ReachedOutAt == "" {
		comms.ReachedOutAt = toolbox.TimeNowUTC()
	}

	updatedComms, err := s.contacterRepository.UpdateComms(ctx, &comms)
	if err != nil {
		logger.Error("failed-to-update-comms-error-updating-comms", zap.Any("request", safeLogValue(req)), zap.Error(err))
		return &UpdateCommsResponse{}, err
	}

	logger.Debug("update-comms-request-successful", zap.Any("request", safeLogValue(req)), zap.Any("updated-comms", safeLogValue(updatedComms)))

	return &UpdateCommsResponse{
		Comms: updatedComms,
	}, nil
}

// GetCommsStats retrieves aggregated stats about platform comms
func (s *Service) GetCommsStats(ctx context.Context, req *GetCommsStatsRequest) (*GetCommsStatsResponse, error) {

	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/contacter")

	logger.Debug("initiating-get-comms-stats-request", zap.Any("request", safeLogValue(req)))

	stats, err := s.contacterRepository.GetCommsStatsCounts(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-comms-stats-counts", zap.Error(err))
		return nil, errors.New("failed to retrieve comms statistics")
	}

	return &GetCommsStatsResponse{CommsStats: stats}, nil
}
