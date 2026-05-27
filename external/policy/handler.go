package policy

import (
	"context"
	"github.com/ooaklee/ghatd/external/logger"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// policyService manages business logic around policy request
type policyService interface {
	GetPolicies(ctx context.Context, r *GetPoliciesRequest) ([]WebAppPolicy, error)
	GetPolicyByName(ctx context.Context, r *GetPolicyByNameRequest) (*WebAppPolicy, error)
}

// policyValidator expected methods of a valid
type policyValidator interface {
	Validate(s interface{}) error
}

// Handler manages policy requests
type Handler struct {
	service   policyService
	validator policyValidator
	errorMaps []reply.ErrorManifest
}

// NewHandler returns policy handler
func NewHandler(service policyService, validator policyValidator, errorMaps ...reply.ErrorManifest) *Handler {
	return &Handler{
		service:   service,
		validator: validator,
		errorMaps: errorMaps,
	}
}

// GetPolicies handles request for returning all policies
func (h *Handler) GetPolicies(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/policy", "handle-get-policies")
	request, err := MapRequestToGetPoliciesRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	policies, err := h.service.GetPolicies(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, policies)
}

// GetPolicyByName handles request for returning a policy with a specific name
// if found
func (h *Handler) GetPolicyByName(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/policy", "handle-get-policy-by-name")
	request, err := MapRequestToGetPolicyByNameRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	policy, err := h.service.GetPolicyByName(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, policy)
}

// getBaseResponseHandler returns response handler configured with auth error map
// nolint will be used later
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(
		errormanifest.NewComposer().
			Add(PolicyErrorMap).
			AddOverrides(h.errorMaps...).
			Build(),
	)
}
