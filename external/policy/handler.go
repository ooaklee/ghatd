package policy

import (
	"context"
	"net/http"

	"github.com/ooaklee/reply"
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
}

// NewHandler returns policy handler
func NewHandler(service policyService, validator policyValidator) *Handler {
	return &Handler{
		service:   service,
		validator: validator,
	}
}

// GetPolicies handles request for returning all policies
func (h *Handler) GetPolicies(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetPoliciesRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	policies, err := h.service.GetPolicies(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, policies)
}

// GetPolicyByName handles request for returning a policy with a specific name
// if found
func (h *Handler) GetPolicyByName(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetPolicyByNameRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	policy, err := h.service.GetPolicyByName(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, policy)
}

// getBaseResponseHandler returns response handler configured with auth error map
// nolint will be used later
func getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(append([]reply.ErrorManifest{}, PolicyErrorMap))
}
