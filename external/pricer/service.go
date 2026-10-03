package pricer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/common"
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
	ValidatePriceSlug(ctx context.Context, req *ValidatePriceSlugRequest) (*ValidatePriceSlugResponse, error)
	PublishPricePlan(ctx context.Context, req *PublishPricePlanRequest) (*PublishPricePlanResponse, error)
	ArchivePricePlan(ctx context.Context, req *ArchivePricePlanRequest) (*ArchivePricePlanResponse, error)
	DeletePricePlan(ctx context.Context, req *DeletePricePlanRequest) (*DeletePricePlanResponse, error)
	CreateFeature(ctx context.Context, req *CreateFeatureRequest) (*CreateFeatureResponse, error)
	UpdateFeature(ctx context.Context, req *UpdateFeatureRequest) (*UpdateFeatureResponse, error)
	GetFeatures(ctx context.Context, req *GetFeaturesRequest) (*GetFeaturesResponse, error)
	DeleteFeature(ctx context.Context, req *DeleteFeatureRequest) (*DeleteFeatureResponse, error)
}

// Service validates catalogue operations and delegates persistence. HTTP route
// policy, or a trusted in-process caller, must authorize mutations beforehand.
type Service struct {
	// PricerRepository persists selected catalogue resources; it must not treat
	// caller attribution as proof of administrative authority.
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
	if err := s.validateMutation(ctx, req, ErrInvalidPricePlanPayload); err != nil {
		return nil, err
	}
	logger := logger.AcquirePackageFrom(ctx, "external/pricer")
	logger.Debug("initiating-create-price-plan-request")

	if req == nil {
		return nil, ErrInvalidPricePlanPayload
	}
	if !priceActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrPriceUserIDRequired
	}

	publishedAt, err := normaliseDateParam(req.PublishAtUtc)
	if err != nil {
		return nil, ErrInvalidPricePlanPayload
	}

	pricePlan := &PricePlan{
		Slug:          req.Slug,
		Name:          strings.TrimSpace(req.Name),
		Description:   strings.TrimSpace(req.Description),
		Status:        req.Status,
		Features:      req.Features,
		Costs:         append([]PriceCost(nil), req.Costs...),
		Discounts:     req.Discounts,
		PaymentTerms:  req.PaymentTerms,
		ProviderRefs:  req.ProviderRefs,
		Metadata:      req.Metadata,
		DisplayOrder:  req.DisplayOrder,
		CreatedByID:   req.ActorID,
		UpdatedByID:   req.ActorID,
		PublishedAt:   publishedAt,
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
	ensurePublishedPricePlanMetadata(pricePlan, req.ActorID)
	if pricePlan.PublishedAt != "" {
		pricePlan.PublishedByID = req.ActorID
	}
	assignMissingPriceCostIDs(pricePlan.Costs)

	if pricePlan.Status == PricePlanStatusPublished {
		if err := validatePricePlanCanPublish(pricePlan); err != nil {
			return nil, err
		}
	}

	if err := pricePlan.Validate(); err != nil {
		logger.Warn("attempt-made-to-create-invalid-price-plan")
		return nil, err
	}

	createdPricePlan, err := s.PricerRepository.CreatePricePlan(ctx, pricePlan)
	if err != nil {
		logger.Error("failed-to-create-price-plan")
		return &CreatePricePlanResponse{}, err
	}

	return &CreatePricePlanResponse{PricePlan: createdPricePlan}, nil
}

// UpdatePricePlan applies editable fields or a trusted matching replacement.
// Replacement history is taken from storage, update attribution from ActorID;
// mismatched targets and failed reads never authorize a persistence call.
func (s *Service) UpdatePricePlan(ctx context.Context, req *UpdatePricePlanRequest) (*UpdatePricePlanResponse, error) {
	if err := s.validateMutation(ctx, req, ErrInvalidPricePlanPayload); err != nil {
		return nil, err
	}
	logger := logger.AcquirePackageFrom(ctx, "external/pricer")
	logger.Debug("initiating-update-price-plan-request")

	if req == nil {
		return nil, ErrInvalidPricePlanPayload
	}
	if !priceActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrPriceUserIDRequired
	}

	pricePlanToUpdate := copyMutablePlan(req.PricePlan)
	if pricePlanToUpdate == nil {
		if strings.TrimSpace(req.ID) == "" {
			return nil, ErrPricePlanIDRequired
		}

		var err error
		pricePlanToUpdate, err = s.PricerRepository.GetPricePlanByID(ctx, req.ID, &GetPricePlanByIDRequest{
			IncludeFeatures:  true,
			IncludeCosts:     true,
			IncludeProviders: true,
		})
		if err != nil {
			logger.Warn("attempt-made-to-update-missing-price-plan")
			return nil, err
		}
		if pricePlanToUpdate == nil || pricePlanToUpdate.ID != req.ID {
			return nil, ErrPricerUnavailable
		}
		pricePlanToUpdate = copyMutablePlan(pricePlanToUpdate)

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
			pricePlanToUpdate.Costs = append([]PriceCost(nil), req.Costs...)
		}
		if req.Discounts != nil {
			pricePlanToUpdate.Discounts = req.Discounts
		}
		if req.PaymentTerms != nil {
			pricePlanToUpdate.PaymentTerms = req.PaymentTerms
		}
		if req.ProviderRefs != nil {
			pricePlanToUpdate.ProviderRefs = req.ProviderRefs
		}
		if req.Metadata != nil {
			pricePlanToUpdate.Metadata = req.Metadata
		}
		if req.DisplayOrder != nil {
			pricePlanToUpdate.DisplayOrder = req.DisplayOrder
		}
	} else if strings.TrimSpace(pricePlanToUpdate.ID) == "" {
		return nil, ErrPricePlanIDRequired
	} else if req.ID != "" && req.ID != pricePlanToUpdate.ID {
		return nil, ErrInvalidPricePlanPayload
	} else {
		existingPricePlan, err := s.PricerRepository.GetPricePlanByID(ctx, pricePlanToUpdate.ID, &GetPricePlanByIDRequest{})
		if err != nil {
			logger.Warn("attempt-made-to-update-missing-price-plan")
			return nil, err
		}
		if existingPricePlan == nil || existingPricePlan.ID != pricePlanToUpdate.ID {
			return nil, ErrPricerUnavailable
		}
		preservePricePlanAuditMetadata(pricePlanToUpdate, existingPricePlan)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if pricePlanToUpdate.Slug == "" {
		pricePlanToUpdate.Slug = pricePlanToUpdate.Name
	}
	pricePlanToUpdate.NormaliseSlug()
	pricePlanToUpdate.UpdatedByID = req.ActorID
	ensurePublishedPricePlanMetadata(pricePlanToUpdate, req.ActorID)
	assignMissingPriceCostIDs(pricePlanToUpdate.Costs)

	if pricePlanToUpdate.Status == PricePlanStatusPublished {
		if err := validatePricePlanCanPublish(pricePlanToUpdate); err != nil {
			return nil, err
		}
	}

	if err := pricePlanToUpdate.Validate(); err != nil {
		logger.Warn("attempt-made-to-update-invalid-price-plan")
		return nil, err
	}

	updatedPricePlan, err := s.PricerRepository.UpdatePricePlan(ctx, pricePlanToUpdate)
	if err != nil {
		logger.Error("failed-to-update-price-plan")
		return &UpdatePricePlanResponse{}, err
	}

	return &UpdatePricePlanResponse{PricePlan: updatedPricePlan}, nil
}

// ensurePublishedPricePlanMetadata prevents a published lifecycle state from
// existing without the publication metadata required by public projections.
func ensurePublishedPricePlanMetadata(pricePlan *PricePlan, actorID string) {
	if pricePlan == nil || pricePlan.Status != PricePlanStatusPublished {
		return
	}

	if strings.TrimSpace(pricePlan.PublishedAt) == "" {
		pricePlan.PublishedAt = toolbox.TimeNowUTC()
	}
	if strings.TrimSpace(pricePlan.PublishedByID) == "" {
		pricePlan.PublishedByID = actorID
	}
}

// preservePricePlanAuditMetadata keeps server-owned lifecycle history when a
// caller submits a full replacement of the mutable plan fields.
func preservePricePlanAuditMetadata(pricePlan, existingPricePlan *PricePlan) {
	if pricePlan == nil || existingPricePlan == nil {
		return
	}

	pricePlan.NanoID = existingPricePlan.NanoID
	pricePlan.CreatedAt, pricePlan.CreatedByID = existingPricePlan.CreatedAt, existingPricePlan.CreatedByID
	pricePlan.PublishedAt, pricePlan.PublishedByID = existingPricePlan.PublishedAt, existingPricePlan.PublishedByID
	pricePlan.DeletedAt, pricePlan.DeletedByID = existingPricePlan.DeletedAt, existingPricePlan.DeletedByID
	pricePlan.UpdatedAt = existingPricePlan.UpdatedAt
}

func assignMissingPriceCostIDs(costs []PriceCost) {
	for i := range costs {
		if costs[i].ID == "" {
			costs[i].ID = toolbox.GenerateUuidV4()
		}
	}
}

// GetPricePlanByID returns a price plan by ID.
func (s *Service) GetPricePlanByID(ctx context.Context, req *GetPricePlanByIDRequest) (*GetPricePlanByIDResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/pricer", "get-price-plan-by-id")
	logger.Debug("handling-get-price-plan-by-id-request")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrPricePlanIDRequired
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
	logger := logger.AcquireOperationFrom(ctx, "external/pricer", "get-price-plan-by-slug")
	logger.Debug("handling-get-price-plan-by-slug-request")

	if req == nil || strings.TrimSpace(req.Slug) == "" {
		return nil, ErrInvalidPriceSlug
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
	logger := logger.AcquirePackageFrom(ctx, "external/pricer")

	if req == nil {
		req = &GetPricePlansRequest{}
	}
	if err := normalisePricePlanDateRanges(req); err != nil {
		return nil, err
	}
	defaultPricePlanListRequest(req)
	if err := validatePricePlanListRequest(req); err != nil {
		return nil, err
	}

	total, err := s.PricerRepository.GetTotalPricePlans(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-price-plans-total", zap.Any("request", safeLogValue(req)), zap.Error(err))
		return &GetPricePlansResponse{}, err
	}
	req.TotalCount = int(total)

	pricePlans, err := s.PricerRepository.GetPricePlans(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-price-plans", zap.Any("request", safeLogValue(req)), zap.Error(err))
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

// ValidatePriceSlug returns the normalized slug and availability for a pricing resource.
func (s *Service) ValidatePriceSlug(ctx context.Context, req *ValidatePriceSlugRequest) (*ValidatePriceSlugResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/pricer", "validate-price-slug")
	logger.Debug("handling-validate-price-slug-request")

	if req == nil {
		return nil, ErrInvalidPriceQueryParam
	}

	resourceType, lookupResourceType, err := normalisePriceSlugResourceType(req.ResourceType)
	if err != nil {
		return nil, ErrInvalidPriceQueryParam
	}

	rawName := strings.TrimSpace(req.Name)
	rawSlug := strings.TrimSpace(req.Slug)
	sourceValue := rawSlug
	if sourceValue == "" {
		sourceValue = rawName
	}
	slug := NormalisePriceSlug(sourceValue)
	if slug == "" {
		return nil, ErrInvalidPriceSlug
	}

	excludeID := strings.TrimSpace(req.ExcludeID)
	existingID := ""
	switch lookupResourceType {
	case PriceSlugResourcePlan:
		existing, err := s.PricerRepository.GetPricePlanBySlug(ctx, slug, &GetPricePlanBySlugRequest{})
		if err != nil && !errors.Is(err, ErrPricePlanNotFound) {
			return nil, err
		}
		if existing != nil {
			existingID = existing.ID
		}
	case PriceSlugResourceFeature:
		features, err := s.PricerRepository.GetFeatures(ctx, &GetFeaturesRequest{
			Slugs:   slug,
			PerPage: 1,
			Page:    1,
		})
		if err != nil {
			return nil, err
		}
		if len(features) > 0 {
			existingID = features[0].ID
		}
	}

	available := existingID == "" || (excludeID != "" && existingID == excludeID)
	if resourceType == PriceSlugResourcePlanFeatureRef {
		available = existingID != ""
	}
	adjusted := slug != sourceValue
	hint := fmt.Sprintf("%s will be the stored %s slug.", slug, resourceType)
	if resourceType == PriceSlugResourcePlanFeatureRef && available {
		hint = fmt.Sprintf("The slug %q matches an existing feature.", slug)
	} else if resourceType == PriceSlugResourcePlanFeatureRef {
		hint = fmt.Sprintf("No feature exists with the slug %q.", slug)
	} else if !available {
		hint = fmt.Sprintf("The slug %q is already used by another %s.", slug, resourceType)
	} else if adjusted {
		hint = fmt.Sprintf("%s will be the stored %s slug, adjusted to comply with slug rules.", slug, resourceType)
	}

	return &ValidatePriceSlugResponse{
		RawName:      rawName,
		RawSlug:      rawSlug,
		Slug:         slug,
		ResourceType: resourceType,
		Adjusted:     adjusted,
		Available:    available,
		ExistingID:   existingID,
		Hint:         hint,
	}, nil
}

func normalisePriceSlugResourceType(value string) (string, string, error) {
	resourceType := strings.TrimSpace(strings.ToLower(value))
	if resourceType == "" {
		resourceType = PriceSlugResourcePlan
	}

	switch resourceType {
	case PriceSlugResourcePlan, "price_plan":
		return PriceSlugResourcePlan, PriceSlugResourcePlan, nil
	case PriceSlugResourceFeature, "price_feature":
		return PriceSlugResourceFeature, PriceSlugResourceFeature, nil
	case PriceSlugResourcePlanFeatureRef, "plan-feature-ref", "feature_ref", "feature-reference":
		return PriceSlugResourcePlanFeatureRef, PriceSlugResourceFeature, nil
	default:
		return "", "", ErrInvalidPriceQueryParam
	}
}

// PublishPricePlan validates the selected plan before recording this actor as its
// publisher. It does not modify the repository's returned validation snapshot.
func (s *Service) PublishPricePlan(ctx context.Context, req *PublishPricePlanRequest) (*PublishPricePlanResponse, error) {
	if err := s.validateMutation(ctx, req, ErrPricePlanIDRequired); err != nil {
		return nil, err
	}
	logger := logger.AcquirePackageFrom(ctx, "external/pricer")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrPricePlanIDRequired
	}
	if !priceActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrPriceUserIDRequired
	}

	publishedAt, err := normaliseDateParam(req.PublishAtUtc)
	if err != nil {
		return nil, ErrInvalidPricePlanPayload
	}

	pricePlan, err := s.PricerRepository.GetPricePlanByID(ctx, req.ID, &GetPricePlanByIDRequest{
		IncludeFeatures:  true,
		IncludeCosts:     true,
		IncludeProviders: true,
	})
	if err != nil {
		logger.Warn("attempt-made-to-publish-missing-price-plan")
		return nil, err
	}
	if pricePlan == nil || pricePlan.ID != req.ID {
		return nil, ErrPricerUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pricePlan = copyMutablePlan(pricePlan)

	pricePlan.Status = PricePlanStatusPublished
	if publishedAt != "" {
		pricePlan.PublishedAt = publishedAt
	} else {
		pricePlan.PublishedAt = toolbox.TimeNowUTC()
	}
	pricePlan.PublishedByID = req.ActorID
	pricePlan.UpdatedByID = req.ActorID

	if err := validatePricePlanCanPublish(pricePlan); err != nil {
		return nil, err
	}
	if err := pricePlan.Validate(); err != nil {
		return nil, err
	}

	err = s.PricerRepository.PublishPricePlan(ctx, req.ID, req.ActorID, pricePlan.PublishedAt)
	if err != nil {
		logger.Error("failed-to-publish-price-plan")
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
	if publishedPricePlan == nil || publishedPricePlan.ID != req.ID {
		return nil, ErrPricerUnavailable
	}

	return &PublishPricePlanResponse{PricePlan: publishedPricePlan}, nil
}

// ArchivePricePlan archives a price plan.
func (s *Service) ArchivePricePlan(ctx context.Context, req *ArchivePricePlanRequest) (*ArchivePricePlanResponse, error) {
	if err := s.validateMutation(ctx, req, ErrPricePlanIDRequired); err != nil {
		return nil, err
	}
	logger := logger.AcquireOperationFrom(ctx, "external/pricer", "archive-price-plan")
	logger.Debug("handling-archive-price-plan-request")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrPricePlanIDRequired
	}
	if !priceActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrPriceUserIDRequired
	}

	updatedAt := toolbox.TimeNowUTC()
	if err := s.PricerRepository.ArchivePricePlan(ctx, req.ID, req.ActorID, updatedAt); err != nil {
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
	if err := s.validateMutation(ctx, req, ErrPricePlanIDRequired); err != nil {
		return nil, err
	}
	logger := logger.AcquireOperationFrom(ctx, "external/pricer", "delete-price-plan")
	logger.Debug("handling-delete-price-plan-request")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrPricePlanIDRequired
	}
	if !priceActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrPriceUserIDRequired
	}

	deletedAt := toolbox.TimeNowUTC()
	if err := s.PricerRepository.SoftDeletePricePlan(ctx, req.ID, req.ActorID, deletedAt); err != nil {
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
	if err := s.validateMutation(ctx, req, ErrInvalidPriceFeaturePayload); err != nil {
		return nil, err
	}
	logger := logger.AcquirePackageFrom(ctx, "external/pricer")

	if req == nil {
		return nil, ErrInvalidPriceFeaturePayload
	}
	if !priceActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrPriceUserIDRequired
	}

	publishedAt, err := normaliseDateParam(req.PublishAtUtc)
	if err != nil {
		return nil, ErrInvalidPriceFeaturePayload
	}

	feature := &PriceFeature{
		Slug:          req.Slug,
		Name:          strings.TrimSpace(req.Name),
		Description:   strings.TrimSpace(req.Description),
		Type:          req.Type,
		Unit:          req.Unit,
		SortOrder:     req.SortOrder,
		Metadata:      req.Metadata,
		CreatedByID:   req.ActorID,
		UpdatedByID:   req.ActorID,
		PublishedAt:   publishedAt,
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
		feature.PublishedByID = req.ActorID
	}

	if err := feature.Validate(); err != nil {
		logger.Warn("attempt-made-to-create-invalid-price-feature")
		return nil, err
	}

	createdFeature, err := s.PricerRepository.CreateFeature(ctx, feature)
	if err != nil {
		logger.Error("failed-to-create-price-feature")
		return &CreateFeatureResponse{}, err
	}

	return &CreateFeatureResponse{Feature: createdFeature}, nil
}

// UpdateFeature updates a selected catalogue item while retaining stored audit
// history. A full replacement is trusted in-process input and must agree with ID.
func (s *Service) UpdateFeature(ctx context.Context, req *UpdateFeatureRequest) (*UpdateFeatureResponse, error) {
	if err := s.validateMutation(ctx, req, ErrInvalidPriceFeaturePayload); err != nil {
		return nil, err
	}
	logger := logger.AcquirePackageFrom(ctx, "external/pricer")

	if req == nil {
		return nil, ErrInvalidPriceFeaturePayload
	}
	if !priceActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrPriceUserIDRequired
	}

	featureToUpdate := copyMutableFeature(req.Feature)
	if featureToUpdate == nil {
		if strings.TrimSpace(req.ID) == "" {
			return nil, ErrPriceFeatureIDRequired
		}

		var err error
		featureToUpdate, err = s.PricerRepository.GetFeatureByID(ctx, req.ID)
		if err != nil {
			logger.Warn("attempt-made-to-update-missing-price-feature")
			return nil, err
		}
		if featureToUpdate == nil || featureToUpdate.ID != req.ID {
			return nil, ErrPricerUnavailable
		}
		featureToUpdate = copyMutableFeature(featureToUpdate)

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
		return nil, ErrPriceFeatureIDRequired
	} else if req.ID != "" && req.ID != featureToUpdate.ID {
		return nil, ErrInvalidPriceFeaturePayload
	} else {
		existingFeature, err := s.PricerRepository.GetFeatureByID(ctx, featureToUpdate.ID)
		if err != nil {
			logger.Warn("attempt-made-to-update-missing-price-feature")
			return nil, err
		}
		if existingFeature == nil || existingFeature.ID != featureToUpdate.ID {
			return nil, ErrPricerUnavailable
		}
		preserveFeatureAuditMetadata(featureToUpdate, existingFeature)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if featureToUpdate.Slug == "" {
		featureToUpdate.Slug = featureToUpdate.Name
	}
	featureToUpdate.NormaliseSlug()
	featureToUpdate.UpdatedByID = req.ActorID

	if err := featureToUpdate.Validate(); err != nil {
		logger.Warn("attempt-made-to-update-invalid-price-feature")
		return nil, err
	}

	updatedFeature, err := s.PricerRepository.UpdateFeature(ctx, featureToUpdate)
	if err != nil {
		logger.Error("failed-to-update-price-feature")
		return &UpdateFeatureResponse{}, err
	}

	return &UpdateFeatureResponse{Feature: updatedFeature}, nil
}

// GetFeatures returns feature catalog items.
func (s *Service) GetFeatures(ctx context.Context, req *GetFeaturesRequest) (*GetFeaturesResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/pricer")

	if req == nil {
		req = &GetFeaturesRequest{}
	}
	if err := normaliseFeatureDateRanges(req); err != nil {
		return nil, err
	}
	defaultFeatureListRequest(req)
	if err := validateFeatureListRequest(req); err != nil {
		return nil, err
	}

	total, err := s.PricerRepository.GetTotalFeatures(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-features-total", zap.Any("request", safeLogValue(req)), zap.Error(err))
		return &GetFeaturesResponse{}, err
	}
	req.TotalCount = int(total)

	features, err := s.PricerRepository.GetFeatures(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-features", zap.Any("request", safeLogValue(req)), zap.Error(err))
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
	if err := s.validateMutation(ctx, req, ErrPriceFeatureIDRequired); err != nil {
		return nil, err
	}
	logger := logger.AcquireOperationFrom(ctx, "external/pricer", "delete-feature")
	logger.Debug("handling-delete-feature-request")

	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrPriceFeatureIDRequired
	}
	if !priceActorMatchesContext(ctx, req.ActorID) {
		return nil, ErrPriceUserIDRequired
	}

	deletedAt := toolbox.TimeNowUTC()
	if err := s.PricerRepository.SoftDeleteFeature(ctx, req.ID, req.ActorID, deletedAt); err != nil {
		return &DeleteFeatureResponse{}, err
	}

	feature, err := s.PricerRepository.GetFeatureByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	return &DeleteFeatureResponse{Feature: feature}, nil
}

// validatePricePlanCanPublish validates that a price plan is ready for publishing.
// It checks that the plan is not nil, has at least one cost, all costs are valid,
// and has at least one valid provider reference at the plan or cost level.
func validatePricePlanCanPublish(pricePlan *PricePlan) error {
	return ValidatePricePlanForPublish(pricePlan)
}

// defaultPricePlanListRequest applies default values to a price plan list request.
// Sets default ordering to "created_at_desc", per-page limit to 25, and page number to 1
// if not already specified.
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

var validPriceSortOrders = map[string]struct{}{
	"created_at_asc":     {},
	"created_at_desc":    {},
	"updated_at_asc":     {},
	"updated_at_desc":    {},
	"deleted_at_asc":     {},
	"deleted_at_desc":    {},
	"published_at_asc":   {},
	"published_at_desc":  {},
	"name_asc":           {},
	"name_desc":          {},
	"slug_asc":           {},
	"slug_desc":          {},
	"display_order_asc":  {},
	"display_order_desc": {},
}

func isValidPriceSortOrder(order string) bool {
	_, ok := validPriceSortOrders[order]
	return ok
}

func validatePricePlanListRequest(req *GetPricePlansRequest) error {
	if !isValidPriceSortOrder(req.Order) {
		return ErrInvalidPriceQueryParam
	}
	return nil
}

func validateFeatureListRequest(req *GetFeaturesRequest) error {
	if !isValidPriceSortOrder(req.Order) {
		return ErrInvalidPriceQueryParam
	}
	return nil
}

// normalisePricePlanDateRanges parses and validates all date range fields in a price plan
// list request, converting them to RFC3339 nano UTC format. Returns an error if any date
// cannot be parsed.
func normalisePricePlanDateRanges(req *GetPricePlansRequest) error {
	var err error
	req.CreatedAtFrom, err = normaliseDateParam(req.CreatedAtFrom)
	if err != nil {
		return ErrInvalidPriceDate
	}
	req.CreatedAtTo, err = normaliseDateParam(req.CreatedAtTo)
	if err != nil {
		return ErrInvalidPriceDate
	}
	req.PublishedAtFrom, err = normaliseDateParam(req.PublishedAtFrom)
	if err != nil {
		return ErrInvalidPriceDate
	}
	req.PublishedAtTo, err = normaliseDateParam(req.PublishedAtTo)
	if err != nil {
		return ErrInvalidPriceDate
	}
	req.DeletedAtFrom, err = normaliseDateParam(req.DeletedAtFrom)
	if err != nil {
		return ErrInvalidPriceDate
	}
	req.DeletedAtTo, err = normaliseDateParam(req.DeletedAtTo)
	if err != nil {
		return ErrInvalidPriceDate
	}

	return nil
}

// defaultFeatureListRequest applies default values to a feature list request.
// Sets default ordering to "created_at_desc", per-page limit to 25, and page number to 1
// if not already specified.
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

// normaliseFeatureDateRanges parses and validates all date range fields in a feature
// list request, converting them to RFC3339 nano UTC format. Returns an error if any date
// cannot be parsed.
func normaliseFeatureDateRanges(req *GetFeaturesRequest) error {
	var err error
	req.CreatedAtFrom, err = normaliseDateParam(req.CreatedAtFrom)
	if err != nil {
		return ErrInvalidPriceDate
	}
	req.CreatedAtTo, err = normaliseDateParam(req.CreatedAtTo)
	if err != nil {
		return ErrInvalidPriceDate
	}
	req.PublishedAtFrom, err = normaliseDateParam(req.PublishedAtFrom)
	if err != nil {
		return ErrInvalidPriceDate
	}
	req.PublishedAtTo, err = normaliseDateParam(req.PublishedAtTo)
	if err != nil {
		return ErrInvalidPriceDate
	}
	req.DeletedAtFrom, err = normaliseDateParam(req.DeletedAtFrom)
	if err != nil {
		return ErrInvalidPriceDate
	}
	req.DeletedAtTo, err = normaliseDateParam(req.DeletedAtTo)
	if err != nil {
		return ErrInvalidPriceDate
	}

	return nil
}

// normaliseDateParam parses a date string and returns it in RFC3339 nano UTC format.
// Accepts Unix timestamps (as integers), RFC3339 formats, and common date formats like
// "2006-01-02". Returns an error if the date cannot be parsed.
func normaliseDateParam(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	if unixTimestamp, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(unixTimestamp, 0).UTC().Format(common.RFC3339NanoUTC), nil
	}

	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		common.RFC3339NanoUTC,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		parsed, err := time.Parse(format, value)
		if err == nil {
			return parsed.UTC().Format(common.RFC3339NanoUTC), nil
		}
	}

	return "", ErrInvalidPriceDate
}
