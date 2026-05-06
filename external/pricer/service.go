package pricer

import (
	"context"
	"errors"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// PricerRepository is the expected repository surface for pricing operations.
type PricerRepository interface {
	PricePlanRepository
	PriceFeatureRepository
}

// PricerService describes pricing business operations.
type PricerService interface {
	CreatePricePlan(ctx context.Context, req *CreatePricePlanRequest) (*CreatePricePlanResponse, error)
	UpdatePricePlan(ctx context.Context, req *UpdatePricePlanRequest) (*UpdatePricePlanResponse, error)
	GetPricePlanByID(ctx context.Context, req *GetPricePlanByIDRequest) (*GetPricePlanByIDResponse, error)
	GetPricePlanBySlug(ctx context.Context, req *GetPricePlanBySlugRequest) (*GetPricePlanBySlugResponse, error)
	GetPricePlans(ctx context.Context, req *GetPricePlansRequest) (*GetPricePlansResponse, error)
	PublishPricePlan(ctx context.Context, req *PublishPricePlanRequest) (*PublishPricePlanResponse, error)
	ArchivePricePlan(ctx context.Context, req *ArchivePricePlanRequest) (*ArchivePricePlanResponse, error)
	DeletePricePlan(ctx context.Context, req *DeletePricePlanRequest) (*DeletePricePlanResponse, error)
	CreateFeature(ctx context.Context, req *CreateFeatureRequest) (*CreateFeatureResponse, error)
	UpdateFeature(ctx context.Context, req *UpdateFeatureRequest) (*UpdateFeatureResponse, error)
	GetFeatures(ctx context.Context, req *GetFeaturesRequest) (*GetFeaturesResponse, error)
	DeleteFeature(ctx context.Context, req *DeleteFeatureRequest) (*DeleteFeatureResponse, error)
}

// Service represents the pricer service.
type Service struct {
	PricerRepository PricerRepository
}

// NewService returns a new instance of the pricer service.
func NewService(pricerRepository PricerRepository) *Service {
	return &Service{
		PricerRepository: pricerRepository,
	}
}

// CreatePricePlan creates a new price plan.
func (s *Service) CreatePricePlan(ctx context.Context, req *CreatePricePlanRequest) (*CreatePricePlanResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("initiating-create-price-plan-request", zap.Any("request", req))

	if req == nil {
		return nil, errors.New(ErrKeyInvalidPricePlanPayload)
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New(ErrKeyPriceUserIDRequired)
	}

	pricePlan := &PricePlan{
		Slug:          req.Slug,
		Name:          strings.TrimSpace(req.Name),
		Description:   strings.TrimSpace(req.Description),
		Status:        req.Status,
		Features:      req.Features,
		Costs:         req.Costs,
		ProviderRefs:  req.ProviderRefs,
		Metadata:      req.Metadata,
		CreatedByID:   req.UserID,
		UpdatedByID:   req.UserID,
		PublishedAt:   strings.TrimSpace(req.PublishAtUtc),
		PublishedByID: "",
	}

	if pricePlan.Slug == "" {
		pricePlan.Slug = pricePlan.Name
	}
	pricePlan.NormaliseSlug()

	if pricePlan.Status == "" {
		pricePlan.Status = PricePlanStatusDraft
	}
	if req.PublishNow {
		pricePlan.Status = PricePlanStatusPublished
		pricePlan.PublishedAt = toolbox.TimeNowUTC()
	}
	if pricePlan.PublishedAt != "" || pricePlan.Status == PricePlanStatusPublished {
		pricePlan.PublishedByID = req.UserID
	}

	if pricePlan.Status == PricePlanStatusPublished {
		if err := validatePricePlanCanPublish(pricePlan); err != nil {
			return nil, err
		}
	}

	if err := pricePlan.Validate(); err != nil {
		log.Warn("attempt-made-to-create-invalid-price-plan", zap.Any("price_plan", pricePlan), zap.Error(err))
		return nil, err
	}

	createdPricePlan, err := s.PricerRepository.CreatePricePlan(ctx, pricePlan)
	if err != nil {
		log.Error("failed-to-create-price-plan", zap.Any("request", req), zap.Error(err))
		return &CreatePricePlanResponse{}, err
	}

	return &CreatePricePlanResponse{PricePlan: createdPricePlan}, nil
}

// UpdatePricePlan updates an existing price plan.
func (s *Service) UpdatePricePlan(ctx context.Context, req *UpdatePricePlanRequest) (*UpdatePricePlanResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("initiating-update-price-plan-request", zap.Any("request", req))

	if req == nil {
		return nil, errors.New(ErrKeyInvalidPricePlanPayload)
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New(ErrKeyPriceUserIDRequired)
	}

	pricePlanToUpdate := req.PricePlan
	if pricePlanToUpdate == nil {
		if strings.TrimSpace(req.ID) == "" {
			return nil, errors.New(ErrKeyPricePlanIDRequired)
		}

		var err error
		pricePlanToUpdate, err = s.PricerRepository.GetPricePlanByID(ctx, req.ID, &GetPricePlanByIDRequest{
			IncludeFeatures:  true,
			IncludeCosts:     true,
			IncludeProviders: true,
		})
		if err != nil {
			log.Warn("attempt-made-to-update-missing-price-plan", zap.String("price_plan_id", req.ID), zap.Error(err))
			return nil, err
		}

		if req.Slug != nil {
			pricePlanToUpdate.Slug = *req.Slug
		}
		if req.Name != nil {
			pricePlanToUpdate.Name = strings.TrimSpace(*req.Name)
		}
		if req.Description != nil {
			pricePlanToUpdate.Description = strings.TrimSpace(*req.Description)
		}
		if req.Status != nil {
			pricePlanToUpdate.Status = *req.Status
		}
		if req.Features != nil {
			pricePlanToUpdate.Features = req.Features
		}
		if req.Costs != nil {
			pricePlanToUpdate.Costs = req.Costs
		}
		if req.ProviderRefs != nil {
			pricePlanToUpdate.ProviderRefs = req.ProviderRefs
		}
		if req.Metadata != nil {
			pricePlanToUpdate.Metadata = req.Metadata
		}
	} else if strings.TrimSpace(pricePlanToUpdate.ID) == "" {
		return nil, errors.New(ErrKeyPricePlanIDRequired)
	} else {
		if _, err := s.PricerRepository.GetPricePlanByID(ctx, pricePlanToUpdate.ID, &GetPricePlanByIDRequest{}); err != nil {
			log.Warn("attempt-made-to-update-missing-price-plan", zap.String("price_plan_id", pricePlanToUpdate.ID), zap.Error(err))
			return nil, err
		}
	}

	if pricePlanToUpdate.Slug == "" {
		pricePlanToUpdate.Slug = pricePlanToUpdate.Name
	}
	pricePlanToUpdate.NormaliseSlug()
	pricePlanToUpdate.UpdatedByID = req.UserID

	if pricePlanToUpdate.Status == PricePlanStatusPublished {
		if err := validatePricePlanCanPublish(pricePlanToUpdate); err != nil {
			return nil, err
		}
	}

	if err := pricePlanToUpdate.Validate(); err != nil {
		log.Warn("attempt-made-to-update-invalid-price-plan", zap.Any("price_plan", pricePlanToUpdate), zap.Error(err))
		return nil, err
	}

	updatedPricePlan, err := s.PricerRepository.UpdatePricePlan(ctx, pricePlanToUpdate)
	if err != nil {
		log.Error("failed-to-update-price-plan", zap.Any("request", req), zap.Error(err))
		return &UpdatePricePlanResponse{}, err
	}

	return &UpdatePricePlanResponse{PricePlan: updatedPricePlan}, nil
}

// GetPricePlanByID returns a price plan by ID.
func (s *Service) GetPricePlanByID(ctx context.Context, req *GetPricePlanByIDRequest) (*GetPricePlanByIDResponse, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(ErrKeyPricePlanIDRequired)
	}

	pricePlan, err := s.PricerRepository.GetPricePlanByID(ctx, req.ID, req)
	if err != nil {
		return nil, err
	}

	return &GetPricePlanByIDResponse{
		GetPricePlanResponse: &GetPricePlanResponse{PricePlan: pricePlan},
	}, nil
}

// GetPricePlanBySlug returns a price plan by slug.
func (s *Service) GetPricePlanBySlug(ctx context.Context, req *GetPricePlanBySlugRequest) (*GetPricePlanBySlugResponse, error) {
	if req == nil || strings.TrimSpace(req.Slug) == "" {
		return nil, errors.New(ErrKeyInvalidPriceSlug)
	}

	req.Slug = NormalisePriceSlug(req.Slug)
	pricePlan, err := s.PricerRepository.GetPricePlanBySlug(ctx, req.Slug, req)
	if err != nil {
		return nil, err
	}

	return &GetPricePlanBySlugResponse{
		GetPricePlanResponse: &GetPricePlanResponse{PricePlan: pricePlan},
	}, nil
}

// GetPricePlans returns a list of price plans.
func (s *Service) GetPricePlans(ctx context.Context, req *GetPricePlansRequest) (*GetPricePlansResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil {
		req = &GetPricePlansRequest{}
	}
	defaultPricePlanListRequest(req)

	total, err := s.PricerRepository.GetTotalPricePlans(ctx, req)
	if err != nil {
		log.Error("failed-to-get-price-plans-total", zap.Any("request", req), zap.Error(err))
		return &GetPricePlansResponse{}, err
	}
	req.TotalCount = int(total)

	pricePlans, err := s.PricerRepository.GetPricePlans(ctx, req)
	if err != nil {
		log.Error("failed-to-get-price-plans", zap.Any("request", req), zap.Error(err))
		return &GetPricePlansResponse{}, err
	}

	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: req.PerPage,
		Page:    req.Page,
	}, pricePlans, req.TotalCount)
	if err != nil {
		return nil, err
	}

	return &GetPricePlansResponse{
		Total:      paginatedResponse.Total,
		TotalPages: paginatedResponse.TotalPages,
		PricePlans: paginatedResponse.Resources,
		Page:       paginatedResponse.Page,
		PerPage:    paginatedResponse.ResourcePerPage,
	}, nil
}

// PublishPricePlan publishes a price plan.
func (s *Service) PublishPricePlan(ctx context.Context, req *PublishPricePlanRequest) (*PublishPricePlanResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(ErrKeyPricePlanIDRequired)
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New(ErrKeyPriceUserIDRequired)
	}

	pricePlan, err := s.PricerRepository.GetPricePlanByID(ctx, req.ID, &GetPricePlanByIDRequest{
		IncludeFeatures:  true,
		IncludeCosts:     true,
		IncludeProviders: true,
	})
	if err != nil {
		log.Warn("attempt-made-to-publish-missing-price-plan", zap.String("price_plan_id", req.ID), zap.Error(err))
		return nil, err
	}

	pricePlan.Status = PricePlanStatusPublished
	if publishAt := strings.TrimSpace(req.PublishAtUtc); publishAt != "" {
		pricePlan.PublishedAt = publishAt
	} else {
		pricePlan.PublishedAt = toolbox.TimeNowUTC()
	}
	pricePlan.PublishedByID = req.UserID
	pricePlan.UpdatedByID = req.UserID

	if err := validatePricePlanCanPublish(pricePlan); err != nil {
		return nil, err
	}
	if err := pricePlan.Validate(); err != nil {
		return nil, err
	}

	err = s.PricerRepository.PublishPricePlan(ctx, req.ID, req.UserID, pricePlan.PublishedAt)
	if err != nil {
		log.Error("failed-to-publish-price-plan", zap.Any("request", req), zap.Error(err))
		return &PublishPricePlanResponse{}, err
	}

	publishedPricePlan, err := s.PricerRepository.GetPricePlanByID(ctx, req.ID, &GetPricePlanByIDRequest{
		IncludeFeatures:  true,
		IncludeCosts:     true,
		IncludeProviders: true,
	})
	if err != nil {
		return nil, err
	}

	return &PublishPricePlanResponse{PricePlan: publishedPricePlan}, nil
}

// ArchivePricePlan archives a price plan.
func (s *Service) ArchivePricePlan(ctx context.Context, req *ArchivePricePlanRequest) (*ArchivePricePlanResponse, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(ErrKeyPricePlanIDRequired)
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New(ErrKeyPriceUserIDRequired)
	}

	updatedAt := toolbox.TimeNowUTC()
	if err := s.PricerRepository.ArchivePricePlan(ctx, req.ID, req.UserID, updatedAt); err != nil {
		return &ArchivePricePlanResponse{}, err
	}

	pricePlan, err := s.PricerRepository.GetPricePlanByID(ctx, req.ID, &GetPricePlanByIDRequest{
		IncludeFeatures:  true,
		IncludeCosts:     true,
		IncludeProviders: true,
	})
	if err != nil {
		return nil, err
	}

	return &ArchivePricePlanResponse{PricePlan: pricePlan}, nil
}

// DeletePricePlan soft-deletes a price plan.
func (s *Service) DeletePricePlan(ctx context.Context, req *DeletePricePlanRequest) (*DeletePricePlanResponse, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(ErrKeyPricePlanIDRequired)
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New(ErrKeyPriceUserIDRequired)
	}

	deletedAt := toolbox.TimeNowUTC()
	if err := s.PricerRepository.SoftDeletePricePlan(ctx, req.ID, req.UserID, deletedAt); err != nil {
		return &DeletePricePlanResponse{}, err
	}

	pricePlan, err := s.PricerRepository.GetPricePlanByID(ctx, req.ID, &GetPricePlanByIDRequest{
		IncludeFeatures:  true,
		IncludeCosts:     true,
		IncludeProviders: true,
	})
	if err != nil {
		return nil, err
	}

	return &DeletePricePlanResponse{PricePlan: pricePlan}, nil
}

// CreateFeature creates a feature catalog item.
func (s *Service) CreateFeature(ctx context.Context, req *CreateFeatureRequest) (*CreateFeatureResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil {
		return nil, errors.New(ErrKeyInvalidPriceFeaturePayload)
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New(ErrKeyPriceUserIDRequired)
	}

	feature := &PriceFeature{
		Slug:          req.Slug,
		Name:          strings.TrimSpace(req.Name),
		Description:   strings.TrimSpace(req.Description),
		Type:          req.Type,
		Unit:          req.Unit,
		SortOrder:     req.SortOrder,
		Metadata:      req.Metadata,
		CreatedByID:   req.UserID,
		UpdatedByID:   req.UserID,
		PublishedAt:   strings.TrimSpace(req.PublishAtUtc),
		PublishedByID: "",
	}

	if feature.Slug == "" {
		feature.Slug = feature.Name
	}
	feature.NormaliseSlug()
	if req.PublishNow {
		feature.PublishedAt = toolbox.TimeNowUTC()
	}
	if feature.PublishedAt != "" {
		feature.PublishedByID = req.UserID
	}

	if err := feature.Validate(); err != nil {
		log.Warn("attempt-made-to-create-invalid-price-feature", zap.Any("feature", feature), zap.Error(err))
		return nil, err
	}

	createdFeature, err := s.PricerRepository.CreateFeature(ctx, feature)
	if err != nil {
		log.Error("failed-to-create-price-feature", zap.Any("request", req), zap.Error(err))
		return &CreateFeatureResponse{}, err
	}

	return &CreateFeatureResponse{Feature: createdFeature}, nil
}

// UpdateFeature updates a feature catalog item.
func (s *Service) UpdateFeature(ctx context.Context, req *UpdateFeatureRequest) (*UpdateFeatureResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil {
		return nil, errors.New(ErrKeyInvalidPriceFeaturePayload)
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New(ErrKeyPriceUserIDRequired)
	}

	featureToUpdate := req.Feature
	if featureToUpdate == nil {
		if strings.TrimSpace(req.ID) == "" {
			return nil, errors.New(ErrKeyPriceFeatureIDRequired)
		}

		var err error
		featureToUpdate, err = s.PricerRepository.GetFeatureByID(ctx, req.ID)
		if err != nil {
			log.Warn("attempt-made-to-update-missing-price-feature", zap.String("feature_id", req.ID), zap.Error(err))
			return nil, err
		}

		if req.Slug != nil {
			featureToUpdate.Slug = *req.Slug
		}
		if req.Name != nil {
			featureToUpdate.Name = strings.TrimSpace(*req.Name)
		}
		if req.Description != nil {
			featureToUpdate.Description = strings.TrimSpace(*req.Description)
		}
		if req.Type != nil {
			featureToUpdate.Type = *req.Type
		}
		if req.Unit != nil {
			featureToUpdate.Unit = *req.Unit
		}
		if req.SortOrder != nil {
			featureToUpdate.SortOrder = *req.SortOrder
		}
		if req.Metadata != nil {
			featureToUpdate.Metadata = req.Metadata
		}
	} else if strings.TrimSpace(featureToUpdate.ID) == "" {
		return nil, errors.New(ErrKeyPriceFeatureIDRequired)
	} else {
		if _, err := s.PricerRepository.GetFeatureByID(ctx, featureToUpdate.ID); err != nil {
			log.Warn("attempt-made-to-update-missing-price-feature", zap.String("feature_id", featureToUpdate.ID), zap.Error(err))
			return nil, err
		}
	}

	if featureToUpdate.Slug == "" {
		featureToUpdate.Slug = featureToUpdate.Name
	}
	featureToUpdate.NormaliseSlug()
	featureToUpdate.UpdatedByID = req.UserID

	if err := featureToUpdate.Validate(); err != nil {
		log.Warn("attempt-made-to-update-invalid-price-feature", zap.Any("feature", featureToUpdate), zap.Error(err))
		return nil, err
	}

	updatedFeature, err := s.PricerRepository.UpdateFeature(ctx, featureToUpdate)
	if err != nil {
		log.Error("failed-to-update-price-feature", zap.Any("request", req), zap.Error(err))
		return &UpdateFeatureResponse{}, err
	}

	return &UpdateFeatureResponse{Feature: updatedFeature}, nil
}

// GetFeatures returns feature catalog items.
func (s *Service) GetFeatures(ctx context.Context, req *GetFeaturesRequest) (*GetFeaturesResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if req == nil {
		req = &GetFeaturesRequest{}
	}
	defaultFeatureListRequest(req)

	total, err := s.PricerRepository.GetTotalFeatures(ctx, req)
	if err != nil {
		log.Error("failed-to-get-features-total", zap.Any("request", req), zap.Error(err))
		return &GetFeaturesResponse{}, err
	}
	req.TotalCount = int(total)

	features, err := s.PricerRepository.GetFeatures(ctx, req)
	if err != nil {
		log.Error("failed-to-get-features", zap.Any("request", req), zap.Error(err))
		return &GetFeaturesResponse{}, err
	}

	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: req.PerPage,
		Page:    req.Page,
	}, features, req.TotalCount)
	if err != nil {
		return nil, err
	}

	return &GetFeaturesResponse{
		Total:      paginatedResponse.Total,
		TotalPages: paginatedResponse.TotalPages,
		Features:   paginatedResponse.Resources,
		Page:       paginatedResponse.Page,
		PerPage:    paginatedResponse.ResourcePerPage,
	}, nil
}

// DeleteFeature soft-deletes a feature catalog item.
func (s *Service) DeleteFeature(ctx context.Context, req *DeleteFeatureRequest) (*DeleteFeatureResponse, error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(ErrKeyPriceFeatureIDRequired)
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, errors.New(ErrKeyPriceUserIDRequired)
	}

	deletedAt := toolbox.TimeNowUTC()
	if err := s.PricerRepository.SoftDeleteFeature(ctx, req.ID, req.UserID, deletedAt); err != nil {
		return &DeleteFeatureResponse{}, err
	}

	feature, err := s.PricerRepository.GetFeatureByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	return &DeleteFeatureResponse{Feature: feature}, nil
}

func validatePricePlanCanPublish(pricePlan *PricePlan) error {
	if pricePlan == nil {
		return errors.New(ErrKeyInvalidPricePlanPayload)
	}
	if len(pricePlan.Costs) == 0 {
		return errors.New(ErrKeyPricePlanPublishRequiresCost)
	}
	if err := ValidatePriceCosts(pricePlan.Costs); err != nil {
		return err
	}
	if !pricePlanHasProviderRef(pricePlan) {
		return errors.New(ErrKeyPricePlanPublishRequiresProvider)
	}

	return nil
}

func pricePlanHasProviderRef(pricePlan *PricePlan) bool {
	if len(pricePlan.ProviderRefs) > 0 && ValidatePriceProviderRefs(pricePlan.ProviderRefs) == nil {
		return true
	}

	for _, cost := range pricePlan.Costs {
		if len(cost.ProviderRefs) > 0 && ValidatePriceProviderRefs(cost.ProviderRefs) == nil {
			return true
		}
	}

	return false
}

func defaultPricePlanListRequest(req *GetPricePlansRequest) {
	if req.Order == "" {
		req.Order = "created_at_desc"
	}
	if req.PerPage == 0 {
		req.PerPage = 25
	}
	if req.Page == 0 {
		req.Page = 1
	}
}

func defaultFeatureListRequest(req *GetFeaturesRequest) {
	if req.Order == "" {
		req.Order = "created_at_desc"
	}
	if req.PerPage == 0 {
		req.PerPage = 25
	}
	if req.Page == 0 {
		req.Page = 1
	}
}
