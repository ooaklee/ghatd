package blueprint

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// blueprintService manages business logic around blueprint request
type blueprintService interface {
	// CreateBlueprint creates a blueprint through the blueprintService
	// business-logic port; the service implementation validates the request,
	// generates identity and creation attribution for the actor, persists via the
	// repository, and returns the created record.
	CreateBlueprint(ctx context.Context, r *CreateBlueprintRequest) (*BlueprintResponse, error)
	// GetBlueprintByID reads one blueprint through the blueprintService
	// business-logic port; the service implementation requires a non-empty ID,
	// verifies the actor against context without inferring ownership, and returns
	// the selected record.
	GetBlueprintByID(ctx context.Context, r *GetBlueprintByIDRequest) (*BlueprintResponse, error)
	// GetBlueprints lists blueprints through the blueprintService business-logic
	// port; the service implementation performs independent list and count reads,
	// returns rows plus a total, and treats a nil request as an unfiltered query.
	GetBlueprints(ctx context.Context, r *GetBlueprintsRequest) (*GetBlueprintsResponse, error)
}

// blueprintValidator expected methods of a valid
type blueprintValidator interface {
	// Validate reports whether the given value passes blueprint validation through
	// the blueprintValidator contract; it inspects s and returns an error
	// describing any validation failure.
	Validate(s interface{}) error
}

// Handler manages blueprint requests
type Handler struct {
	service   blueprintService
	validator blueprintValidator
	errorMaps []reply.ErrorManifest
}

// NewHandler returns a blueprint handler with optional error-map override layers.
func NewHandler(service blueprintService, validator blueprintValidator, errorMapLayers ...reply.ErrorManifest) *Handler {
	return &Handler{
		service:   service,
		validator: validator,
		errorMaps: errorMapLayers,
	}
}

// CreateBlueprint handles blueprint creation.
func (h *Handler) CreateBlueprint(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "internal/blueprint", "handle-create-blueprint")
	request, err := MapRequestToCreateBlueprintRequest(r, h.validator)
	if err != nil {
		logger.Warn("blueprint-create-request-map-failed", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.CreateBlueprint(r.Context(), request)
	if err != nil {
		logger.Error("blueprint-create-service-failed", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	logger.Debug("blueprint-create-response-written")
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Blueprint)
}

// GetBlueprints handles listing blueprints.
func (h *Handler) GetBlueprints(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "internal/blueprint", "handle-get-blueprints")
	request, err := MapRequestToGetBlueprintsRequest(r, h.validator)
	if err != nil {
		logger.Warn("blueprint-list-request-map-failed", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GetBlueprints(r.Context(), request)
	if err != nil {
		logger.Error("blueprint-list-service-failed", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	logger.Debug("blueprint-list-response-written", zap.Int("returned", len(response.Blueprints)), zap.Int64("total", response.Total))
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Blueprints)
}

// GetBlueprintByID handles getting a blueprint by ID.
func (h *Handler) GetBlueprintByID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "internal/blueprint", "handle-get-blueprint-by-id")
	request, err := MapRequestToGetBlueprintByIDRequest(r, h.validator)
	if err != nil {
		logger.Warn("blueprint-get-by-id-request-map-failed", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GetBlueprintByID(r.Context(), request)
	if err != nil {
		logger.Error("blueprint-get-by-id-service-failed", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	logger.Debug("blueprint-get-by-id-response-written")
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Blueprint)
}

// getBaseResponseHandler returns a response handler with the blueprint error map
// as the base layer and caller-supplied maps as overrides.
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.responseManifests())
}

// responseManifests keeps success factories and error writers on the same
// domain base and last-wins caller override layers.
func (h *Handler) responseManifests() []reply.ErrorManifest {
	return errormanifest.NewComposer().Add(blueprintErrorMap).AddOverrides(h.errorMaps...).Build()
}

// NewHTTPErrorResponse preserves mapped wrappers and validation collections.
// It returns writer failures and never passes raw diagnostic causes to reply.
func (h *Handler) NewHTTPErrorResponse(w http.ResponseWriter, err error, attributes ...reply.ResponseAttributes) error {
	return errormanifest.WriteHTTPError(w, err, h.responseManifests(), attributes...)
}
