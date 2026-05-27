package cors

import (
	"net/http"

	"github.com/gorilla/handlers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// NewCorsMiddleware creates a middleware that handles Cross-Origin Resource Sharing.
func NewCorsMiddleware(allowedOrigins []string) func(handler http.Handler) http.Handler {

	return func(handler http.Handler) http.Handler {
		corsHandler := handlers.CORS(
			handlers.AllowCredentials(),
			handlers.AllowedHeaders([]string{common.CorrelationIdHttpHeader, "Content-Type", "Authorization", common.WebPlatformHttpRequestHeader, common.TimezoneHttpRequestHeader, common.SystemWideXApiToken, common.WebPartialHttpRequestHeader, common.CacheSkipHttpResponseHeader, common.HtmxHttpCurrentUrlHeader,
				common.HtmxHttpRequestHeader,
				common.HtmxHttpTargetHeader,
				common.HtmxHttpTriggerHeader}),
			handlers.AllowedMethods([]string{"HEAD", "OPTIONS", "GET", "PATCH", "POST", "PUT", "DELETE"}),
			handlers.AllowedOrigins(allowedOrigins),
		)(handler)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger.AcquireOperationFrom(r.Context(), "external/middleware/cors", "cors").
				Debug("cors-request", zap.String("origin", r.Header.Get("Origin")), zap.Int("allowed-origin-count", len(allowedOrigins)))
			corsHandler.ServeHTTP(w, r)
		})
	}

}
