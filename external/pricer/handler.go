package pricer

import (
	"context"
	"github.com/ooaklee/ghatd/external/logger"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// PriceService interface defines expected methods of a valid pricer service.
type PriceService interface {
	// CreatePricePlan persists and returns a newly created price plan from the
	// given request. The service implementation validates the payload and actor,
	// applies defaults and publishing rules, and delegates storage to the pricer
	// repository.
	CreatePricePlan(ctx context.Context, r *CreatePricePlanRequest) (*CreatePricePlanResponse, error)
	// UpdatePricePlan updates a price plan identified in the request and returns
	// the updated plan. The service applies editable fields or a trusted
	// replacement, validates before persisting, and attributes the update to the
	// request's ActorID matched against context.
	UpdatePricePlan(ctx context.Context, r *UpdatePricePlanRequest) (*UpdatePricePlanResponse, error)
	// GetPricePlanByID returns the price plan selected by the request ID in a
	// response; the request controls whether features, costs, and providers are
	// included in the retrieved plan.
	GetPricePlanByID(ctx context.Context, r *GetPricePlanByIDRequest) (*GetPricePlanByIDResponse, error)
	// GetPricePlanBySlug returns the price plan matching the request slug after
	// normalization; the request controls inclusion of features, costs, and
	// providers in the result.
	GetPricePlanBySlug(ctx context.Context, r *GetPricePlanBySlugRequest) (*GetPricePlanBySlugResponse, error)
	// GetPricePlans returns a paginated, filtered list of price plans with total
	// counts and pagination metadata derived from the request.
	GetPricePlans(ctx context.Context, r *GetPricePlansRequest) (*GetPricePlansResponse, error)
	// ValidatePriceSlug checks slug availability for a plan or feature without
	// persisting anything, returning the normalized slug, availability, existing
	// identifier, and a hint.
	ValidatePriceSlug(ctx context.Context, r *ValidatePriceSlugRequest) (*ValidatePriceSlugResponse, error)
	// PublishPricePlan marks the selected plan published after validation,
	// recording the request's ActorID as publisher, and returns the persisted
	// published plan.
	PublishPricePlan(ctx context.Context, r *PublishPricePlanRequest) (*PublishPricePlanResponse, error)
	// ArchivePricePlan archives the selected plan, attributing the change to the
	// request's ActorID, and returns the archived plan.
	ArchivePricePlan(ctx context.Context, r *ArchivePricePlanRequest) (*ArchivePricePlanResponse, error)
	// DeletePricePlan soft-deletes the selected plan, attributing deletion to the
	// request's ActorID, and returns the stored plan afterwards.
	DeletePricePlan(ctx context.Context, r *DeletePricePlanRequest) (*DeletePricePlanResponse, error)
	// CreateFeature creates a feature catalog item from the request, attributing
	// creation to the request's ActorID, and returns the persisted feature.
	CreateFeature(ctx context.Context, r *CreateFeatureRequest) (*CreateFeatureResponse, error)
	// UpdateFeature updates a selected feature, applying optional fields or a
	// trusted replacement that must match the target ID, and returns the updated
	// feature attributed to ActorID.
	UpdateFeature(ctx context.Context, r *UpdateFeatureRequest) (*UpdateFeatureResponse, error)
	// GetFeatures returns a paginated, filtered list of feature catalog items with
	// total counts and pagination metadata derived from the request.
	GetFeatures(ctx context.Context, r *GetFeaturesRequest) (*GetFeaturesResponse, error)
	// DeleteFeature soft-deletes the selected feature catalog item, attributing
	// deletion to the request's ActorID, and returns the stored feature afterwards.
	DeleteFeature(ctx context.Context, r *DeleteFeatureRequest) (*DeleteFeatureResponse, error)
}

// Handler manages pricer requests.
type Handler struct {
	Service   PriceService
	Validator PricerValidator
	ErrorMaps []reply.ErrorManifest
}

// NewHandler returns a new pricer handler.
func NewHandler(service PriceService, validator PricerValidator, errorMaps ...reply.ErrorManifest) *Handler {
	return &Handler{
		Service:   service,
		Validator: validator,
		ErrorMaps: errorMaps,
	}
}

// CreatePricePlan handles price plan creation.
func (h *Handler) CreatePricePlan(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-create-price-plan")
	request, err := MapRequestToCreatePricePlanRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreatePricePlan(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.PricePlan)
}

// UpdatePricePlan handles price plan updates.
func (h *Handler) UpdatePricePlan(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-update-price-plan")
	request, err := MapRequestToUpdatePricePlanRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdatePricePlan(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// GetPricePlanByID handles getting a price plan by ID.
func (h *Handler) GetPricePlanByID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-get-price-plan-by-id")
	request, err := MapRequestToGetPricePlanByIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetPricePlanByID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// GetPricePlanBySlug handles getting a price plan by slug.
func (h *Handler) GetPricePlanBySlug(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-get-price-plan-by-slug")
	request, err := MapRequestToGetPricePlanBySlugRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetPricePlanBySlug(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// GetPricePlans handles getting price plans.
func (h *Handler) GetPricePlans(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-get-price-plans")
	request, err := MapRequestToGetPricePlansRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetPricePlans(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlans, reply.WithMeta(response.GetMetaData()))
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlans)
}

// ValidatePriceSlug handles pricing slug validation without persisting anything.
func (h *Handler) ValidatePriceSlug(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-validate-price-slug")
	request, err := MapRequestToValidatePriceSlugRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ValidatePriceSlug(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// PublishPricePlan handles price plan publishing.
func (h *Handler) PublishPricePlan(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-publish-price-plan")
	request, err := MapRequestToPublishPricePlanRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.PublishPricePlan(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// ArchivePricePlan handles price plan archiving.
func (h *Handler) ArchivePricePlan(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-archive-price-plan")
	request, err := MapRequestToArchivePricePlanRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ArchivePricePlan(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// DeletePricePlan handles price plan soft deletion.
func (h *Handler) DeletePricePlan(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-delete-price-plan")
	request, err := MapRequestToDeletePricePlanRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.DeletePricePlan(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// CreateFeature handles feature creation.
func (h *Handler) CreateFeature(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-create-feature")
	request, err := MapRequestToCreateFeatureRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateFeature(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Feature)
}

// UpdateFeature handles feature updates.
func (h *Handler) UpdateFeature(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-update-feature")
	request, err := MapRequestToUpdateFeatureRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateFeature(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Feature)
}

// GetFeatures handles getting feature catalog items.
func (h *Handler) GetFeatures(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-get-features")
	request, err := MapRequestToGetFeaturesRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetFeatures(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Features, reply.WithMeta(response.GetMetaData()))
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Features)
}

// DeleteFeature handles feature soft deletion.
func (h *Handler) DeleteFeature(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/pricer", "handle-delete-feature")
	request, err := MapRequestToDeleteFeatureRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.DeleteFeature(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Feature)
}

// getBaseResponseHandler builds a fresh Replier from the handler's current
// response manifests.
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.responseManifests())
}

// responseManifests keeps success factories and error writers on the same
// domain base and last-wins caller override layers.
func (h *Handler) responseManifests() []reply.ErrorManifest {
	return errormanifest.NewComposer().Add(PricerErrorMap).AddOverrides(h.ErrorMaps...).Build()
}

// NewHTTPErrorResponse preserves mapped wrappers and validation collections.
// It returns writer failures and never passes raw diagnostic causes to reply.
func (h *Handler) NewHTTPErrorResponse(w http.ResponseWriter, err error, attributes ...reply.ResponseAttributes) error {
	return errormanifest.WriteHTTPError(w, err, h.responseManifests(), attributes...)
}
