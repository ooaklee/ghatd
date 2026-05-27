package contenttype

import (
	"net/http"
	"strings"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// NewContentType creates a middleware that sets the content-type header to application/json
func NewContentType(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := logger.AcquireOperationFrom(r.Context(), "external/middleware/contenttype", "content-type")

		const contentTypeHeaderName string = "Content-Type"
		const jsonContentType string = "application/json"

		if strings.Contains(r.Header.Get(contentTypeHeaderName), jsonContentType) ||
			(strings.HasPrefix(r.URL.Path, common.ApiV1UriPrefix) && !strings.Contains(r.Header.Get(common.HtmxHttpRequestHeader), "true")) {
			w.Header().Set(contentTypeHeaderName, jsonContentType)
			logger.Debug("content-type-json-applied", zap.String("path", r.URL.Path))
		}

		h.ServeHTTP(w, r)
	})
}
