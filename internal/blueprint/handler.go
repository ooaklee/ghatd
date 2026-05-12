package blueprint

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
)

// blueprintService manages business logic around blueprint request
type blueprintService interface {
	CreateBlueprint(ctx context.Context, r *CreateBlueprintRequest) (*BlueprintResponse, error)
	GetBlueprintByID(ctx context.Context, r *GetBlueprintByIDRequest) (*BlueprintResponse, error)
	GetBlueprints(ctx context.Context, r *GetBlueprintsRequest) (*GetBlueprintsResponse, error)
}

// blueprintValidator expected methods of a valid
type blueprintValidator interface {
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
	request, err := MapRequestToCreateBlueprintRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.CreateBlueprint(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Blueprint)
}

// GetBlueprints handles listing blueprints.
func (h *Handler) GetBlueprints(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetBlueprintsRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GetBlueprints(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Blueprints)
}

// GetBlueprintByID handles getting a blueprint by ID.
func (h *Handler) GetBlueprintByID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetBlueprintByIDRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GetBlueprintByID(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Blueprint)
}

// getBaseResponseHandler returns a response handler with the blueprint error map
// as the base layer and caller-supplied maps as overrides.
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(
		errormanifest.NewComposer().
			Add(blueprintErrorMap).
			AddOverrides(h.errorMaps...).
			Build(),
	)
}
