package cors

import (
	"net/http"

	"github.com/gorilla/handlers"
	"github.com/ooaklee/ghatd/internal/common"
)

// NewCorsMiddleware creates a middleware that handles Cross-Origin Resource Sharing.
func NewCorsMiddleware(allowedOrigins []string) func(handler http.Handler) http.Handler {

	return func(handler http.Handler) http.Handler {
		return handlers.CORS(
			handlers.AllowCredentials(),
			handlers.AllowedHeaders([]string{common.CorrelationIdHttpHeader, "Content-Type", "Authorization", "platform", common.SystemWideXApiToken, common.WebPartialHttpRequestHeader, common.CacheSkipHttpResponseHeader}),
			handlers.AllowedMethods([]string{"HEAD", "OPTIONS", "GET", "PATCH", "POST", "PUT", "DELETE"}),
			handlers.AllowedOrigins(allowedOrigins),
		)(handler)
	}

}
