package starter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	accessmiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/stretchr/testify/require"
)

func TestMiddlewareMeResponseModeForwarding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   accessmiddleware.MeEndpointResponseMode
		status int
		empty  bool
	}{
		{"legacy", accessmiddleware.MeEndpointLegacyAccepted, 202, false},
		{"empty", accessmiddleware.MeEndpointAcceptedEmpty, 202, true},
		{"unauthorized", accessmiddleware.MeEndpointUnauthorized, 401, false},
		{"invalid", "invalid", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := validMiddlewareRequest(t)
			r.MeEndpointResponseMode = tc.mode
			middleware, err := NewMiddleware(r)
			if tc.status == 0 {
				require.ErrorIs(t, err, accessmiddleware.ErrInvalidMeEndpointResponseMode)
				require.Nil(t, middleware)
				return
			}
			require.NoError(t, err)
			w := httptest.NewRecorder()
			middleware.AccessManager.CustomMeEndpointValidApiTokenOrJWT(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("anonymous dispatch") })).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me", nil))
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.empty, w.Body.Len() == 0)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Contains(t, w.Header().Values("X-Robots-Tag"), "noindex")
		})
	}
}
