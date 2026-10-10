package waitlist

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	grouter "github.com/ooaklee/ghatd/external/router"
)

// AttachAnnouncementRoutes installs private preview/send and public opaque-token
// unsubscribe routes. Both host guards and persistent storage are mandatory.
func AttachAnnouncementRoutes(router *grouter.Router, service *AnnouncementService, rateLimit, adminOnly mux.MiddlewareFunc) error {
	if router == nil || service == nil || service.Store == nil || rateLimit == nil || adminOnly == nil {
		return errors.New("waitlist announcement requires service and route guards")
	}
	h := &announcementHandler{service}
	admin := router.NewRouteGroup("/api/v1/waitlist/announcement", grouter.AdminSession, adminOnly)
	admin.Handle(grouter.RouteDefinition{Path: "", Operation: "waitlist.CurrentAnnouncement", Methods: []string{http.MethodGet, http.MethodOptions}}, h.current)
	admin.Handle(grouter.RouteDefinition{Path: "/preview", Operation: "waitlist.PreviewAnnouncement", Methods: []string{http.MethodPost, http.MethodOptions}}, h.preview)
	admin.Handle(grouter.RouteDefinition{Path: "/send", Operation: "waitlist.SendAnnouncement", Methods: []string{http.MethodPost, http.MethodOptions}}, h.send)
	public := router.NewRouteGroup("/api/v1/waitlist", grouter.PublicRateLimited, rateLimit)
	public.Handle(grouter.RouteDefinition{Path: "/unsubscribe", Operation: "waitlist.Unsubscribe", Methods: []string{http.MethodPost, http.MethodOptions}}, h.unsubscribe)
	return nil
}

// announcementHandler binds the shared announcement service to private HTTP
// endpoint methods.
type announcementHandler struct{ service *AnnouncementService }

// decodeAnnouncement strictly decodes a JSON request body of at most 10000
// bytes into value, rejecting non-JSON media types and trailing content; it
// writes the error response itself and reports success via its boolean.
func decodeAnnouncement(w http.ResponseWriter, r *http.Request, value any) bool {
	w.Header().Set("Cache-Control", "no-store")
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Please submit JSON."})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 10000))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Please check the request."})
		return false
	}
	return true
}

// current serves the frozen campaign preview with its delivery summary under a
// 10-second timeout, returning data: null when no campaign exists.
func (h *announcementHandler) current(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	preview, err := h.service.Current(ctx)
	if err != nil {
		announcementError(w, err)
		return
	}
	if preview == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": nil})
		return
	}
	summary, err := h.service.Summary(ctx)
	if err != nil {
		announcementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"preview": preview, "summary": summary}})
}

// preview validates and persists a proposed announcement from the JSON body via
// Prepare under a 15-second timeout.
func (h *announcementHandler) preview(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Data    map[string]string `json:"data"`
		Subject string            `json:"subject"`
		Message string            `json:"message"`
		URL     string            `json:"url"`
	}
	if !decodeAnnouncement(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	preview, err := h.service.Prepare(ctx, Announcement{Subject: input.Subject, Message: input.Message, URL: input.URL, Data: input.Data})
	if err != nil {
		announcementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": preview})
}

// send dispatches a batch for a non-empty preview ID of at most 100 characters
// under a 2-minute timeout, returning the dispatch summary.
func (h *announcementHandler) send(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PreviewID string `json:"previewId"`
	}
	if !decodeAnnouncement(w, r, &input) {
		return
	}
	if input.PreviewID == "" || len(input.PreviewID) > 100 {
		announcementError(w, ErrPreviewExpired)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	summary, err := h.service.Dispatch(ctx, input.PreviewID)
	if err != nil {
		announcementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": summary})
}

// unsubscribe accepts a 26-character base32-style token, hashes it before
// storage lookup, and suppresses the matching delivery identity, returning 204
// on success.
func (h *announcementHandler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decodeAnnouncement(w, r, &input) {
		return
	}
	if len(input.Token) != 26 || strings.IndexFunc(input.Token, func(r rune) bool { return (r < 'A' || r > 'Z') && (r < '2' || r > '7') }) >= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Please use the unsubscribe link from your email."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := h.service.Store.Unsubscribe(ctx, tokenHash(input.Token)); err != nil {
		announcementError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// announcementError maps known announcement errors to client responses: invalid
// input to 400, expired or frozen state to 409, disabled sending keeps 503 with
// its message, and unknown errors keep the generic unavailable text.
func announcementError(w http.ResponseWriter, err error) {
	status, message := http.StatusServiceUnavailable, "The announcement service is unavailable. Please try again."
	switch {
	case errors.Is(err, ErrInvalidAnnouncement):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, ErrPreviewExpired), errors.Is(err, ErrAnnouncementFrozen):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, ErrSendingDisabled):
		message = err.Error()
	}
	writeJSON(w, status, map[string]string{"error": message})
}
