package accesspolicymanager

import (
	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/reply/v2"
	"net/http"
)

// ReviewCapabilities returns the selected user's current stored grant and ETag.
// The optional routes enforce explicit bearer administrator authentication.
func (h *Handler) ReviewCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	service, ok := h.service.(capabilityManagementService)
	if !ok {
		h.fail(w, accesspolicy.ErrConfiguration)
		return
	}
	grant, err := service.ReviewCapabilities(r.Context(), mux.Vars(r)["userID"])
	if err != nil {
		h.fail(w, err)
		return
	}
	revision := int64(0)
	if grant != nil {
		revision = grant.Revision
	}
	w.Header().Set("ETag", revisionTag(revision))
	_ = reply.NewReplier(h.manifests).NewHTTPDataResponse(w, http.StatusOK, grant)
}

// ApplyCapabilities accepts an explicit capability replacement and exact ETag;
// actor, system, token allowances and usage budgets cannot be selected by JSON.
func (h *Handler) ApplyCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	service, ok := h.service.(capabilityManagementService)
	if !ok {
		h.fail(w, accesspolicy.ErrConfiguration)
		return
	}
	expected, err := readRevision(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	patch, err := readCapabilities(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	grant, err := service.ApplyCapabilities(r.Context(), mux.Vars(r)["userID"], expected, patch)
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("ETag", revisionTag(grant.Revision))
	_ = reply.NewReplier(h.manifests).NewHTTPDataResponse(w, http.StatusOK, grant)
}

// PreviewCapabilities uses the same complete strict codec as apply and returns
// detached before/after fields with the current strong revision ETag.
func (h *Handler) PreviewCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	service, ok := h.service.(capabilityPreviewService)
	if !ok {
		h.fail(w, accesspolicy.ErrConfiguration)
		return
	}
	patch, err := readCapabilities(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	preview, err := service.PreviewCapabilities(r.Context(), mux.Vars(r)["userID"], patch)
	if err != nil {
		h.fail(w, err)
		return
	}
	revision := int64(0)
	if preview.Before != nil {
		revision = preview.Before.Revision
	}
	w.Header().Set("ETag", revisionTag(revision))
	_ = reply.NewReplier(h.manifests).NewHTTPDataResponse(w, http.StatusOK, preview)
}
