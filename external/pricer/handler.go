package pricer

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
)

// PriceService interface defines expected methods of a valid pricer service.
type PriceService interface {
	CreatePricePlan(ctx context.Context, r *CreatePricePlanRequest) (*CreatePricePlanResponse, error)
	UpdatePricePlan(ctx context.Context, r *UpdatePricePlanRequest) (*UpdatePricePlanResponse, error)
	GetPricePlanByID(ctx context.Context, r *GetPricePlanByIDRequest) (*GetPricePlanByIDResponse, error)
	GetPricePlanBySlug(ctx context.Context, r *GetPricePlanBySlugRequest) (*GetPricePlanBySlugResponse, error)
	GetPricePlans(ctx context.Context, r *GetPricePlansRequest) (*GetPricePlansResponse, error)
	ValidatePriceSlug(ctx context.Context, r *ValidatePriceSlugRequest) (*ValidatePriceSlugResponse, error)
	PublishPricePlan(ctx context.Context, r *PublishPricePlanRequest) (*PublishPricePlanResponse, error)
	ArchivePricePlan(ctx context.Context, r *ArchivePricePlanRequest) (*ArchivePricePlanResponse, error)
	DeletePricePlan(ctx context.Context, r *DeletePricePlanRequest) (*DeletePricePlanResponse, error)
	CreateFeature(ctx context.Context, r *CreateFeatureRequest) (*CreateFeatureResponse, error)
	UpdateFeature(ctx context.Context, r *UpdateFeatureRequest) (*UpdateFeatureResponse, error)
	GetFeatures(ctx context.Context, r *GetFeaturesRequest) (*GetFeaturesResponse, error)
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
	request, err := MapRequestToCreatePricePlanRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreatePricePlan(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.PricePlan)
}

// UpdatePricePlan handles price plan updates.
func (h *Handler) UpdatePricePlan(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdatePricePlanRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdatePricePlan(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// GetPricePlanByID handles getting a price plan by ID.
func (h *Handler) GetPricePlanByID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetPricePlanByIDRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetPricePlanByID(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// GetPricePlanBySlug handles getting a price plan by slug.
func (h *Handler) GetPricePlanBySlug(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetPricePlanBySlugRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetPricePlanBySlug(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// GetPricePlans handles getting price plans.
func (h *Handler) GetPricePlans(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetPricePlansRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetPricePlans(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
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
	request, err := MapRequestToValidatePriceSlugRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ValidatePriceSlug(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// PublishPricePlan handles price plan publishing.
func (h *Handler) PublishPricePlan(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToPublishPricePlanRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.PublishPricePlan(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// ArchivePricePlan handles price plan archiving.
func (h *Handler) ArchivePricePlan(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToArchivePricePlanRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ArchivePricePlan(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// DeletePricePlan handles price plan soft deletion.
func (h *Handler) DeletePricePlan(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToDeletePricePlanRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.DeletePricePlan(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.PricePlan)
}

// CreateFeature handles feature creation.
func (h *Handler) CreateFeature(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToCreateFeatureRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateFeature(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Feature)
}

// UpdateFeature handles feature updates.
func (h *Handler) UpdateFeature(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateFeatureRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateFeature(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Feature)
}

// GetFeatures handles getting feature catalog items.
func (h *Handler) GetFeatures(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetFeaturesRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetFeatures(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
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
	request, err := MapRequestToDeleteFeatureRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.DeleteFeature(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Feature)
}

func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(
		errormanifest.NewComposer().
			Add(h.ErrorMaps...).
			AddOverrides(PricerErrorMap).
			Build(),
	)
}
