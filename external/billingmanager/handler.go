package billingmanager

import (
	"context"
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// BillingManagerService manages business logic around billingmanager request
type BillingManagerService interface {
	ProcessBillingProviderWebhooks(ctx context.Context, req *ProcessBillingProviderWebhooksRequest) error
	GetUserSubscriptionStatus(ctx context.Context, r *GetUserSubscriptionStatusRequest) (*GetUserSubscriptionStatusResponse, error)
	GetUserBillingDetail(ctx context.Context, r *GetUserBillingDetailRequest) (*GetUserBillingDetailResponse, error)
	GetUserBillingEvents(ctx context.Context, r *GetUserBillingEventsRequest) (*GetUserBillingEventsResponse, error)
	GetPricingPlans(ctx context.Context, r *GetPricingPlansRequest) (*GetPricingPlansResponse, error)
	GetPricePlanBySlug(ctx context.Context, r *GetPricePlanBySlugRequest) (*GetPricePlanBySlugResponse, error)
	GetPricingFeatures(ctx context.Context, r *GetPriceFeaturesRequest) (*GetPriceFeaturesResponse, error)
}

// BillingManagerValidator expected methods of a valid
type BillingManagerValidator interface {
	Validate(s interface{}) error
}

// Handler manages billingmanager requests
type Handler struct {
	Service   BillingManagerService
	Validator BillingManagerValidator
	ErrorMaps []reply.ErrorManifest
}

// NewHandler returns billingmanager handler
func NewHandler(service BillingManagerService, validator BillingManagerValidator, errorMaps ...reply.ErrorManifest) *Handler {

	return &Handler{
		ErrorMaps: errorMaps,
		Service:   service,
		Validator: validator,
	}
}

// ProcessBillingProviderWebhooks handles request to process
// billing provider webhooks
func (h *Handler) ProcessBillingProviderWebhooks(w http.ResponseWriter, r *http.Request) {
	request, err := mapRequestToProcessBillingProviderWebhooksRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.ProcessBillingProviderWebhooks(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// GetUserBillingEvents handles request to get user billing events
func (h *Handler) GetUserBillingEvents(w http.ResponseWriter, r *http.Request) {
	request, err := mapRequestToGetUserBillingEventsRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserBillingEvents(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Events, reply.WithMeta(response.GetMetaData()))
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Events)
}

// GetUserSubscriptionStatus handles request to get user subscription status
func (h *Handler) GetUserSubscriptionStatus(w http.ResponseWriter, r *http.Request) {
	request, err := mapRequestToGetUserSubscriptionStatusRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserSubscriptionStatus(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.SubscriptionStatus)
}

// GetUserBillingDetail handles request to get user billing detail
func (h *Handler) GetUserBillingDetail(w http.ResponseWriter, r *http.Request) {
	request, err := mapRequestToGetUserBillingDetailRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserBillingDetail(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.BillingDetail)
}

// GetPricingPlans handles request to get pricing plans.
func (h *Handler) GetPricingPlans(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetPricingPlansRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetPricingPlans(r.Context(), request)
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

// GetPricePlanBySlug handles request to get a pricing plan by slug.
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

// GetPricingFeatures handles request to get pricing feature catalog items.
func (h *Handler) GetPricingFeatures(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetPriceFeaturesRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetPricingFeatures(r.Context(), request)
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

// getBaseResponseHandler returns response handler configured with billing manager error maps
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.ErrorMaps)
}
