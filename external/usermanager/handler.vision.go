package usermanager

import (
	"context"
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/vision"
	"github.com/ooaklee/reply/v2"
)

// visionUsermanagerService is the usermanager-side view of vision domain
// operations, returning manager-enriched vision responses; it narrows the
// vision service to what the handler layer invokes.
type visionUsermanagerService interface {
	// CreateVision stores an authenticated feedback or bug submission and returns a
	// manager-enriched vision response. Contract of visionUsermanagerService, the
	// handler-facing view of vision operations; r carries the submission, and the
	// response contains the enriched created vision.
	CreateVision(ctx context.Context, r *vision.CreateVisionRequest) (*GetVisionResponse, error)
	// GetVisionByNanoID retrieves a vision by its NanoID and returns it with
	// privacy-safe public user summaries. Contract of visionUsermanagerService; r
	// identifies the vision to fetch, and the response contains the projected
	// vision, public users and viewer NanoID.
	GetVisionByNanoID(ctx context.Context, r *vision.GetVisionByNanoIDRequest) (*GetVisionResponse, error)
	// GetVisions returns a page of vision summaries enriched with associated user
	// views for the usermanager handler. The implementation delegates to the vision
	// service and projects each vision with its users and total count.
	GetVisions(ctx context.Context, r *vision.GetVisionsRequest) (*GetVisionsResponse, error)
	// GetVisionConfig returns client-safe vision capabilities and configuration.
	// The implementation forwards the call to the configured vision service and
	// returns its response unchanged.
	GetVisionConfig(ctx context.Context) (*vision.GetVisionConfigResponse, error)
	// UpdateVision applies owner-or-admin edits restricted to descriptive vision
	// fields, clearing metadata, and returns the enriched updated vision. The
	// implementation rechecks the requester identity against the trusted context
	// and the current vision before delegating.
	UpdateVision(ctx context.Context, r *vision.UpdateVisionRequest) (*GetVisionResponse, error)
	// UpdateVisionStatus performs an admin roadmap status transition for a vision
	// and returns the enriched result. The implementation delegates the transition
	// to the vision service, then enriches the updated vision.
	UpdateVisionStatus(ctx context.Context, r *vision.UpdateVisionStatusRequest) (*GetVisionResponse, error)
	// DeleteVision permanently removes a vision after owner-or-admin authorization.
	// The implementation verifies the authenticated requester matches the actor and
	// can manage the current vision before delegating deletion.
	DeleteVision(ctx context.Context, r *vision.DeleteVisionRequest) (*vision.DeleteVisionResponse, error)
	// SetVisionVote records a vote on a vision and returns the enriched updated
	// vision. The implementation delegates the vote to the vision service, then
	// enriches the resulting vision.
	SetVisionVote(ctx context.Context, r *vision.SetVisionVoteRequest) (*GetVisionResponse, error)
	// RemoveVisionVote removes the caller's vote on a vision and returns the
	// enriched updated vision. The implementation delegates removal to the vision
	// service, then enriches the resulting vision.
	RemoveVisionVote(ctx context.Context, r *vision.RemoveVisionVoteRequest) (*GetVisionResponse, error)
	// AddVisionComment stores a comment on a vision and returns the enriched
	// updated vision. The implementation delegates comment storage to the vision
	// service, then enriches the resulting vision.
	AddVisionComment(ctx context.Context, r *vision.AddVisionCommentRequest) (*GetVisionResponse, error)
	// SetVisionCommentVote records a vote on a vision comment and returns the
	// enriched updated vision. The implementation delegates the comment vote to the
	// vision service, then enriches the resulting vision.
	SetVisionCommentVote(ctx context.Context, r *vision.SetVisionCommentVoteRequest) (*GetVisionResponse, error)
	// RemoveVisionCommentVote removes a vote on a vision comment and returns the
	// enriched updated vision. The implementation delegates the vote removal to the
	// vision service, then enriches the resulting vision.
	RemoveVisionCommentVote(ctx context.Context, r *vision.RemoveVisionCommentVoteRequest) (*GetVisionResponse, error)
}

// UpdateVision handles owner-or-admin edits to descriptive vision fields.
func (h *Handler) UpdateVision(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToUpdateVisionRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	// The usermanager edit surface intentionally excludes internal metadata.
	req.Metadata = nil
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.UpdateVision(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetVisionConfig handles client-safe vision configuration.
func (h *Handler) GetVisionConfig(w http.ResponseWriter, r *http.Request) {
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.GetVisionConfig(r.Context())
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	setVisionReadCacheHeaders(w, r)
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Config)
}

// UpdateVisionStatus handles an admin roadmap transition and returns an enriched vision.
func (h *Handler) UpdateVisionStatus(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToUpdateVisionStatusRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.UpdateVisionStatus(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// DeleteVision handles owner-or-admin deletion of a vision.
func (h *Handler) DeleteVision(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToDeleteVisionRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.DeleteVision(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// CreateVision handles authenticated feedback or bug submission.
func (h *Handler) CreateVision(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToCreateVisionRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.CreateVision(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response)
}

// GetVisions handles an enriched vision list.
func (h *Handler) GetVisions(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToGetVisionsRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.GetVisions(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	setVisionReadCacheHeaders(w, r)
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response, reply.WithMeta(response.GetMetaData()))
}

// GetVisionByNanoID handles an enriched vision detail.
func (h *Handler) GetVisionByNanoID(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToGetVisionByNanoIDRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.GetVisionByNanoID(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	setVisionReadCacheHeaders(w, r)
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// SetVisionVote handles an authenticated vote.
func (h *Handler) SetVisionVote(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToSetVisionVoteRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.SetVisionVote(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// RemoveVisionVote handles authenticated vote removal.
func (h *Handler) RemoveVisionVote(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToRemoveVisionVoteRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.RemoveVisionVote(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// AddVisionComment handles an authenticated comment.
func (h *Handler) AddVisionComment(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToAddVisionCommentRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.AddVisionComment(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response)
}

// SetVisionCommentVote handles an authenticated comment vote.
func (h *Handler) SetVisionCommentVote(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToSetVisionCommentVoteRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.SetVisionCommentVote(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// RemoveVisionCommentVote handles authenticated comment vote removal.
func (h *Handler) RemoveVisionCommentVote(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToRemoveVisionCommentVoteRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.RemoveVisionCommentVote(r.Context(), req)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// setVisionReadCacheHeaders sets Cache-Control headers appropriate for the
// requestor's authentication state.
func setVisionReadCacheHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Vary", "Cookie, Authorization")
	if accessmanagerhelpers.AcquireAuthenticatedFrom(r.Context()) {
		w.Header().Set("Cache-Control", "private, no-store")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60, s-maxage=300")
}
