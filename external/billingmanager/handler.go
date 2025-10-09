package billingmanager

import (
	"context"
	"net/http"

	"github.com/ooaklee/reply"
)

// BillingManagerService manages business logic around billingmanager request
type BillingManagerService interface {
	ProcessBillingProviderWebhooks(ctx context.Context, req *ProcessBillingProviderWebhooksRequest) error
	GetUserSubscriptionStatus(ctx context.Context, r *GetUserSubscriptionStatusRequest) (*GetUserSubscriptionStatusResponse, error)
	GetUserBillingDetail(ctx context.Context, r *GetUserBillingDetailRequest) (*GetUserBillingDetailResponse, error)
	GetUserBillingEvents(ctx context.Context, r *GetUserBillingEventsRequest) (*GetUserBillingEventsResponse, error)
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

// getBaseResponseHandler returns response handler configured with auth error map
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.ErrorMaps)
}
