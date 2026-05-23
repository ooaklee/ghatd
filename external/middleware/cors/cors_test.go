package cors_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/middleware/cors"
)

func TestNewCorsMiddlewareAllowsClientContextHeaders(t *testing.T) {
	t.Parallel()

	handler := cors.NewCorsMiddleware([]string{"https://app.test"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/test", nil)
	req.Header.Set("Origin", "https://app.test")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", strings.Join([]string{
		common.WebPlatformHttpRequestHeader,
		common.TimezoneHttpRequestHeader,
	}, ", "))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	allowedHeaders := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
	assert.Contains(t, allowedHeaders, strings.ToLower(common.WebPlatformHttpRequestHeader))
	assert.Contains(t, allowedHeaders, strings.ToLower(common.TimezoneHttpRequestHeader))
}
