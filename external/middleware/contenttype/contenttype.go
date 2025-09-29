package contenttype

import (
	"net/http"
	"strings"

	"github.com/ooaklee/ghatd/external/common"
)

// NewContentType creates a middleware that sets the content-type header to application/json
func NewContentType(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		const contentTypeHeaderName string = "Content-Type"
		const jsonContentType string = "application/json"

		if strings.Contains(r.Header.Get(contentTypeHeaderName), jsonContentType) ||
			(strings.HasPrefix(r.URL.Path, common.ApiV1UriPrefix) && !strings.Contains(r.Header.Get(common.HtmxHttpRequestHeader), "true")) {
			w.Header().Set(contentTypeHeaderName, jsonContentType)
		}

		h.ServeHTTP(w, r)
	})
}
