package vision

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// visionService manages business logic around vision request
type visionService interface {
	CreateVision(ctx context.Context, r *CreateVisionRequest) (*VisionResponse, error)
	GetVisionByID(ctx context.Context, r *GetVisionByIDRequest) (*VisionResponse, error)
	GetVisions(ctx context.Context, r *GetVisionsRequest) (*GetVisionsResponse, error)
}

// visionValidator expected methods of a valid
type visionValidator interface {
	Validate(s interface{}) error
}

// Handler manages vision requests
type Handler struct {
	service   visionService
	validator visionValidator
	errorMaps []reply.ErrorManifest
}

// NewHandler returns a vision handler with optional error-map override layers.
func NewHandler(service visionService, validator visionValidator, errorMapLayers ...reply.ErrorManifest) *Handler {
	return &Handler{
		service:   service,
		validator: validator,
		errorMaps: errorMapLayers,
	}
}

// CreateVision handles vision creation.
func (h *Handler) CreateVision(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "internal/vision", "handle-create-vision")
	request, err := MapRequestToCreateVisionRequest(r, h.validator)
	if err != nil {
		logger.Warn("vision-create-request-map-failed", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.CreateVision(r.Context(), request)
	if err != nil {
		logger.Error("vision-create-service-failed", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	logger.Debug("vision-create-response-written", zap.String("vision-id", response.Vision.ID))
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Vision)
}

// GetVisions handles listing visions.
func (h *Handler) GetVisions(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "internal/vision", "handle-get-visions")
	request, err := MapRequestToGetVisionsRequest(r, h.validator)
	if err != nil {
		logger.Warn("vision-list-request-map-failed", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GetVisions(r.Context(), request)
	if err != nil {
		logger.Error("vision-list-service-failed", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	logger.Debug("vision-list-response-written", zap.Int("returned", len(response.Visions)), zap.Int64("total", response.Total))
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Visions)
}

// GetVisionByID handles getting a vision by ID.
func (h *Handler) GetVisionByID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "internal/vision", "handle-get-vision-by-id")
	request, err := MapRequestToGetVisionByIDRequest(r, h.validator)
	if err != nil {
		logger.Warn("vision-get-by-id-request-map-failed", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GetVisionByID(r.Context(), request)
	if err != nil {
		logger.Error("vision-get-by-id-service-failed", zap.String("vision-id", request.ID), zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	logger.Debug("vision-get-by-id-response-written", zap.String("vision-id", response.Vision.ID))
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Vision)
}

// getBaseResponseHandler returns a response handler with the vision error map
// as the base layer and caller-supplied maps as overrides.
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(
		errormanifest.NewComposer().
			Add(visionErrorMap).
			AddOverrides(h.errorMaps...).
			Build(),
	)
}
