package indexnow

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// Handler serves the public IndexNow key verification endpoint. It exposes
// exactly one route pair (GET and HEAD) at the configured key location path and
// answers with the API key as a UTF-8 plain-text body. It never permits
// submission, history, or any other operation: the verification endpoint is
// read-only and reflects the key only at the configured path.
type Handler struct {
	service *Service
}

// NewHandler returns a handler serving the service's key verification
// endpoint. The returned handler has no submission capability regardless of
// how it is mounted.
func NewHandler(s *Service) *Handler {
	return &Handler{service: s}
}

// RegisterRoutes registers the key verification route pair on router. Both
// GET and HEAD at the service's key location path answer with the key
// response; HEAD sends headers (including Content-Length) only. No other
// method or path is handled here, and no route accepts submission traffic.
// The key location is a validated literal path (no braces or wildcards), so
// the route matches exactly one path.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	if h == nil || h.service == nil || router == nil {
		return
	}
	router.HandleFunc(h.service.keyLocation, h.verifyKey).Methods(http.MethodGet, http.MethodHead)
}

// verifyKey writes the API key as UTF-8 plain text. Content is fixed; the
// request is never inspected and cannot alter the response, reflect other
// paths, or trigger any outbound call. The key is public verification
// material, not an action.
func (h *Handler) verifyKey(w http.ResponseWriter, r *http.Request) {
	// mux normally matches decoded URL.Path. Refuse encoded aliases too, so
	// the advertised literal ownership file is the only path returning the key.
	if r == nil || r.URL == nil || r.URL.Path != h.service.keyLocation || r.URL.RawPath != "" {
		http.NotFound(w, r)
		return
	}
	key := h.service.apiKey
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(key)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r != nil && r.Method == http.MethodHead {
		// HEAD responses carry headers only.
		return
	}
	if _, err := io.WriteString(w, key); err != nil {
		// Body write failures on a fixed response carry no secret; nothing to
		// echo back to the client.
		_ = err
	}
}
