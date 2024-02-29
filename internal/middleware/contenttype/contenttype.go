package contenttype

import (
	"net/http"
	"strings"
)

// NewContentType creates a middleware that sets the content-type header to application/json
func NewContentType(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if strings.Contains(r.Header.Get("Content-Type"), "application/json") || strings.HasPrefix(r.URL.Path, "/v1") {
			w.Header().Set("Content-type", "application/json")
		}

		h.ServeHTTP(w, r)
	})
}
