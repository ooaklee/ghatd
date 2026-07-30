package usermanager

import (
	"context"
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/vision"
	"github.com/ooaklee/reply/v2"
)

type visionUsermanagerService interface {
	CreateVision(ctx context.Context, r *vision.CreateVisionRequest) (*GetVisionResponse, error)
	GetVisionByNanoID(ctx context.Context, r *vision.GetVisionByNanoIDRequest) (*GetVisionResponse, error)
	GetVisions(ctx context.Context, r *vision.GetVisionsRequest) (*GetVisionsResponse, error)
	SetVisionVote(ctx context.Context, r *vision.SetVisionVoteRequest) (*GetVisionResponse, error)
	RemoveVisionVote(ctx context.Context, r *vision.RemoveVisionVoteRequest) (*GetVisionResponse, error)
	AddVisionComment(ctx context.Context, r *vision.AddVisionCommentRequest) (*GetVisionResponse, error)
	SetVisionCommentVote(ctx context.Context, r *vision.SetVisionCommentVoteRequest) (*GetVisionResponse, error)
	RemoveVisionCommentVote(ctx context.Context, r *vision.RemoveVisionCommentVoteRequest) (*GetVisionResponse, error)
}

// CreateVision handles authenticated feedback or bug submission.
func (h *Handler) CreateVision(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToCreateVisionRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.CreateVision(r.Context(), req)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response)
}

// GetVisions handles an enriched vision list.
func (h *Handler) GetVisions(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToGetVisionsRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.GetVisions(r.Context(), req)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	setVisionReadCacheHeaders(w, r)
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response, reply.WithMeta(response.GetMetaData()))
}

// GetVisionByNanoID handles an enriched vision detail.
func (h *Handler) GetVisionByNanoID(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToGetVisionByNanoIDRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.GetVisionByNanoID(r.Context(), req)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	setVisionReadCacheHeaders(w, r)
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// SetVisionVote handles an authenticated vote.
func (h *Handler) SetVisionVote(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToSetVisionVoteRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.SetVisionVote(r.Context(), req)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// RemoveVisionVote handles authenticated vote removal.
func (h *Handler) RemoveVisionVote(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToRemoveVisionVoteRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.RemoveVisionVote(r.Context(), req)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// AddVisionComment handles an authenticated comment.
func (h *Handler) AddVisionComment(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToAddVisionCommentRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.AddVisionComment(r.Context(), req)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response)
}

// SetVisionCommentVote handles an authenticated comment vote.
func (h *Handler) SetVisionCommentVote(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToSetVisionCommentVoteRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.SetVisionCommentVote(r.Context(), req)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// RemoveVisionCommentVote handles authenticated comment vote removal.
func (h *Handler) RemoveVisionCommentVote(w http.ResponseWriter, r *http.Request) {
	req, err := vision.MapRequestToRemoveVisionCommentVoteRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(visionUsermanagerService)
	if !ok {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrVisionServiceNotEnabled)
		return
	}
	response, err := service.RemoveVisionCommentVote(r.Context(), req)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

func setVisionReadCacheHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Vary", "Cookie, Authorization")
	if accessmanagerhelpers.AcquireAuthenticatedFrom(r.Context()) {
		w.Header().Set("Cache-Control", "private, no-store")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60, s-maxage=300")
}
