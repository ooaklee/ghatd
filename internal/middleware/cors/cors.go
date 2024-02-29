package cors

import (
	"net/http"

	"github.com/gorilla/handlers"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/common"
)

// NewCorsMiddleware creates a middleware that handles Cross-Origin Resource Sharing.
func NewCorsMiddleware(allowedOrigins []string) func(handler http.Handler) http.Handler {

	return func(handler http.Handler) http.Handler {
		return handlers.CORS(
			handlers.AllowCredentials(),
			handlers.AllowedHeaders([]string{"X-Correlation-Id", "Content-Type", "Authorization", "platform", common.SystemWideXApiToken}),
			handlers.AllowedMethods([]string{"HEAD", "OPTIONS", "GET", "PATCH", "POST", "PUT", "DELETE"}),
			handlers.AllowedOrigins(allowedOrigins),
		)(handler)
	}
}
