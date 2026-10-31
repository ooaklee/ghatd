package partners

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	partnerhttp "github.com/ooaklee/ghatd/external/partnermanager/http"
	"github.com/stretchr/testify/require"
)

func TestMountRoutesDisabledAndEnabled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.Handler
		path    string
		code    int
	}{{"disabled root", nil, partnerhttp.BasePath, 503}, {"disabled child", nil, partnerhttp.BasePath + "/profile", 503}, {"enabled", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), partnerhttp.BasePath + "/profile", 204}, {"unrelated fallback", nil, "/pricing", 200}} {
		t.Run(tc.name, func(t *testing.T) {
			r := mux.NewRouter()
			require.NoError(t, MountRoutes(r, tc.handler))
			r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
			out := httptest.NewRecorder()
			r.ServeHTTP(out, httptest.NewRequest("GET", tc.path, nil))
			require.Equal(t, tc.code, out.Code)
			if tc.code == 503 {
				require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
				require.Contains(t, out.Body.String(), "PARTNERS_DEPENDENCY_UNAVAILABLE")
				require.True(t, strings.Contains(out.Header().Get("Content-Type"), "application/json"))
			}
		})
	}
	require.Error(t, MountRoutes(nil, nil))
	_, err := BuildHTTP(nil, HTTPConfig{})
	require.Error(t, err)
}
func TestNeverCache(t *testing.T) {
	for _, tc := range []struct {
		path   string
		bypass bool
	}{{partnerhttp.BasePath, true}, {partnerhttp.BasePath + "/claims", true}, {"/ref/token", true}, {"/referral", false}, {"/pricing", false}} {
		t.Run(tc.path, func(t *testing.T) {
			cache := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Cached", "yes"); next.ServeHTTP(w, r) })
			}
			h := NeverCache(cache)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			out := httptest.NewRecorder()
			h.ServeHTTP(out, httptest.NewRequest("GET", tc.path, nil))
			require.Equal(t, !tc.bypass, out.Header().Get("X-Cached") == "yes")
		})
	}
}
