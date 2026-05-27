package response

import (
	"net/http"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

const (
	ErrKeyResourceNotFound = "DefaultResourceNotFound"
)

var defaultErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrResourceNotFound: {Title: "Resource not found.", StatusCode: 404},
}

// GetResourceNotFoundError returns default 404 response
func GetResourceNotFoundError(w http.ResponseWriter, r *http.Request) {
	replier := reply.NewReplier(append([]reply.ErrorManifest{}, defaultErrorMap))
	logger := logger.AcquireOperationFrom(r.Context(), "external/response", "resource-not-found")

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		logger.Debug("resource-not-found-json-response", zap.String("path", r.URL.Path))
		//nolint will set up default fallback later
		replier.NewHTTPErrorResponse(w, ErrResourceNotFound)
		return
	}

	logger.Debug("resource-not-found-text-response", zap.String("path", r.URL.Path))
	http.Error(w, "Not Found", http.StatusNotFound)
}

// GetDefault200Response returns default 200 response to be used
// in cases such as Healthchecks.
//
// TODO: Consider swapping out for https://github.com/etherlabsio/healthcheck at
// a later date
func GetDefault200Response(w http.ResponseWriter, r *http.Request) {
	replier := reply.NewReplier([]reply.ErrorManifest{})
	logger := logger.AcquireOperationFrom(r.Context(), "external/response", "default-200")

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		logger.Debug("default-200-json-response", zap.String("path", r.URL.Path))
		//nolint will set up default fallback later
		replier.NewHTTPBlankResponse(w, 200)
		return
	}

	logger.Debug("default-200-text-response", zap.String("path", r.URL.Path))
	w.WriteHeader(200)
	w.Write([]byte("OK"))
}
