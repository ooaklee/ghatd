package vision

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
)

type visionService interface {
	CreateVision(ctx context.Context, r *CreateVisionRequest) (*VisionResponse, error)
	GetVisionByNanoID(ctx context.Context, r *GetVisionByNanoIDRequest) (*VisionResponse, error)
	GetVisions(ctx context.Context, r *GetVisionsRequest) (*GetVisionsResponse, error)
	UpdateVision(ctx context.Context, r *UpdateVisionRequest) (*VisionResponse, error)
	UpdateVisionStatus(ctx context.Context, r *UpdateVisionStatusRequest) (*VisionResponse, error)
	SetVisionVote(ctx context.Context, r *SetVisionVoteRequest) (*VisionResponse, error)
	RemoveVisionVote(ctx context.Context, r *RemoveVisionVoteRequest) (*VisionResponse, error)
	AddVisionComment(ctx context.Context, r *AddVisionCommentRequest) (*VisionResponse, error)
	SetVisionCommentVote(ctx context.Context, r *SetVisionCommentVoteRequest) (*VisionResponse, error)
	RemoveVisionCommentVote(ctx context.Context, r *RemoveVisionCommentVoteRequest) (*VisionResponse, error)
	DeleteVision(ctx context.Context, r *DeleteVisionRequest) (*DeleteVisionResponse, error)
	GetVisionConfig(ctx context.Context) (*GetVisionConfigResponse, error)
}

type visionValidator interface {
	Validate(s interface{}) error
}

// Handler manages vision HTTP requests.
type Handler struct {
	service   visionService
	validator visionValidator
	errorMaps []reply.ErrorManifest
}

// NewHandler returns a vision handler with optional error-map overrides.
func NewHandler(service visionService, validator visionValidator, errorMapLayers ...reply.ErrorManifest) *Handler {
	return &Handler{service: service, validator: validator, errorMaps: errorMapLayers}
}

// CreateVision handles feedback or bug report creation.
func (h *Handler) CreateVision(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToCreateVisionRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.CreateVision(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Vision)
}

// GetVisions handles paginated vision listing.
func (h *Handler) GetVisions(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToGetVisionsRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.GetVisions(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Visions, reply.WithMeta(response.GetMetaData()))
}

// GetVisionByNanoID handles fetching a single vision by public NanoID.
func (h *Handler) GetVisionByNanoID(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToGetVisionByNanoIDRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.GetVisionByNanoID(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Vision)
}

// UpdateVision handles descriptive field updates on a vision.
func (h *Handler) UpdateVision(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToUpdateVisionRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.UpdateVision(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Vision)
}

// UpdateVisionStatus handles roadmap status transitions.
func (h *Handler) UpdateVisionStatus(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToUpdateVisionStatusRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.UpdateVisionStatus(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Vision)
}

// SetVisionVote handles setting or changing a user's vote on a vision.
func (h *Handler) SetVisionVote(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToSetVisionVoteRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.SetVisionVote(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Vision)
}

// RemoveVisionVote handles removing a user's vote from a vision.
func (h *Handler) RemoveVisionVote(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToRemoveVisionVoteRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.RemoveVisionVote(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Vision)
}

// AddVisionComment handles appending a comment to a vision.
func (h *Handler) AddVisionComment(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToAddVisionCommentRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.AddVisionComment(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Vision)
}

// SetVisionCommentVote handles setting or changing a vote on a vision comment.
func (h *Handler) SetVisionCommentVote(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToSetVisionCommentVoteRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.SetVisionCommentVote(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Vision)
}

// RemoveVisionCommentVote handles removing a vote from a vision comment.
func (h *Handler) RemoveVisionCommentVote(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToRemoveVisionCommentVoteRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.RemoveVisionCommentVote(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Vision)
}

// DeleteVision handles vision deletion by public NanoID.
func (h *Handler) DeleteVision(w http.ResponseWriter, r *http.Request) {
	req, err := MapRequestToDeleteVisionRequest(r, h.validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.service.DeleteVision(r.Context(), req)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetVisionConfig handles returning client-safe configuration.
func (h *Handler) GetVisionConfig(w http.ResponseWriter, r *http.Request) {
	response, err := h.service.GetVisionConfig(r.Context())
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Config)
}

// getBaseResponseHandler returns a reply.Replier configured with the vision
// error map and any handler-level overrides.
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(
		errormanifest.NewComposer().
			Add(VisionErrorMap).
			AddOverrides(h.errorMaps...).
			Build(),
	)
}
