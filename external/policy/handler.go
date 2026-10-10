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
	// GetPolicies returns the policy list held by the policyService store for the
	// given request. The service implementation reads all policies from the store
	// and returns them without modification.
	GetPolicies(ctx context.Context, r *GetPoliciesRequest) ([]WebAppPolicy, error)
	// GetPolicyByName returns the policy matching the request's name, with names
	// normalized to lowercase and spaces replaced by hyphens for consistent
	// matching. The policyService implementation returns an error when no matching
	// policy exists.
	GetPolicyByName(ctx context.Context, r *GetPolicyByNameRequest) (*WebAppPolicy, error)
}

// policyValidator expected methods of a valid
type policyValidator interface {
	// Validate checks whether the supplied value satisfies the policyValidator's
	// validation rules and returns an error describing any violation. It is the
	// expected validation port for policy request mapping.
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
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	policies, err := h.service.GetPolicies(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, policies)
}

// GetPolicyByName handles request for returning a policy with a specific name
// if found
func (h *Handler) GetPolicyByName(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/policy", "handle-get-policy-by-name")
	request, err := MapRequestToGetPolicyByNameRequest(r, h.validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	policy, err := h.service.GetPolicyByName(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, policy)
}

// getBaseResponseHandler returns response handler configured with auth error map
// nolint will be used later
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.responseManifests())
}

// responseManifests keeps success factories and error writers on the same
// domain base and last-wins caller override layers.
func (h *Handler) responseManifests() []reply.ErrorManifest {
	return errormanifest.NewComposer().Add(PolicyErrorMap).AddOverrides(h.errorMaps...).Build()
}

// NewHTTPErrorResponse preserves mapped wrappers and validation collections.
// It returns writer failures and never passes raw diagnostic causes to reply.
func (h *Handler) NewHTTPErrorResponse(w http.ResponseWriter, err error, attributes ...reply.ResponseAttributes) error {
	return errormanifest.WriteHTTPError(w, err, h.responseManifests(), attributes...)
}
