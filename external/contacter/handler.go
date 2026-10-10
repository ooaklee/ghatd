package contacter

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// ContacterService interface defines expected methods of a valid contacter service
type ContacterService interface {
	// CreateComms validates and persists a new comms, deriving identity, type, and
	// standardised contact fields from the request and returning the created
	// record.
	CreateComms(ctx context.Context, req *CreateCommsRequest) (*CreateCommsResponse, error)
	// GetComms returns a paginated, filtered list of comms entries from the
	// repository; the service applies default ordering, page and per-page values,
	// computes the total count and returns the paginated response.
	GetComms(ctx context.Context, req *GetCommsRequest) (*GetCommsResponse, error)
	// UpdateComms applies administrator-supplied field updates to an existing comms
	// identified by request ID, preserving prior data and recording reach-out
	// timing.
	UpdateComms(ctx context.Context, req *UpdateCommsRequest) (*UpdateCommsResponse, error)
	// GetCommsStats returns aggregated platform comms statistics, optionally scoped
	// by an email regex filter.
	GetCommsStats(ctx context.Context, req *GetCommsStatsRequest) (*GetCommsStatsResponse, error)
	// GetAvailableCommsTypes returns the communication types accepted by the
	// service for clients building a contact form dynamically.
	GetAvailableCommsTypes(ctx context.Context) (*GetAvailableCommsTypesResponse, error)
}

// ContacterValidator interface defines expected methods of a valid validator
type ContacterValidator interface {
	// Validate checks an arbitrary request payload for validity, returning an error
	// when validation fails.
	Validate(s interface{}) error
}

// Handler manages contacter requests
type Handler struct {
	Service   ContacterService
	Validator ContacterValidator
	ErrorMaps []reply.ErrorManifest
}

// NewHandler returns a new contacter handler
func NewHandler(service ContacterService, validator ContacterValidator, errorMaps ...reply.ErrorManifest) *Handler {
	return &Handler{
		Service:   service,
		Validator: validator,
		ErrorMaps: errorMaps,
	}
}

// GetAvailableCommsTypes handles retrieving the communication types accepted
// by the service. This endpoint intentionally contains no user-specific data.
func (h *Handler) GetAvailableCommsTypes(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contacter", "handle-get-available-comms-types")

	response, err := h.Service.GetAvailableCommsTypes(r.Context())
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.CommsTypes)
}

// GetCommsStats handles retrieving aggregated stats about platform comms
func (h *Handler) GetCommsStats(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contacter", "handle-get-comms-stats")
	request := &GetCommsStatsRequest{
		WithEmailRegex: r.URL.Query().Get("with_email_regex"),
	}

	if err := h.Validator.Validate(request); err != nil {
		// Validator diagnostics are not domain identities or public messages.
		logger.Warn("handler-returning-error-response", zap.Error(ErrInvalidCommsPayload))
		h.NewHTTPErrorResponse(w, ErrInvalidCommsPayload)
		return
	}

	response, err := h.Service.GetCommsStats(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}
