package serverhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

func TestCORSRouteFamiliesAndOriginBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, path, origin, headers string
		empty, allow                bool
	}{
		{"partner headers", "/api/v1/partners/destination", "https://app.example.test", "content-type,if-match,idempotency-key,x-csrf-token", false, true},
		{"unknown partner origin", "/api/v1/partners/destination", "https://other.example.test", "content-type", false, false},
		{"suffix origin", "/api/v1/partners/destination", "https://app.example.test.attacker.test", "content-type", false, false},
		{"empty partners deny", "/api/v1/partners/destination", "https://app.example.test", "content-type", true, false},
		{"handle revision", "/api/v1/ums/me/handle", "https://app.example.test", "content-type,if-match", false, true},
		{"ordinary does not inherit revision", "/about", "https://app.example.test", "if-match", false, false},
		{"partner prefix boundary", "/api/v1/partnership", "https://app.example.test", "idempotency-key,x-csrf-token", false, false},
		{"handle suffix boundary", "/api/v1/ums/me/handle/unknown", "https://app.example.test", "if-match", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := CORSConfig{AllowedOrigins: []string{" https://app.example.test "}, PartnersOrigins: []string{"https://app.example.test"}, Handles: true}
			if tc.empty {
				cfg.PartnersOrigins = nil
			}
			mw, err := CORS(cfg)
			require.NoError(t, err)
			// Construction snapshots allowed origins instead of retaining host slices.
			cfg.AllowedOrigins[0] = "https://changed.example.test"
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
			r := httptest.NewRequest(http.MethodOptions, tc.path, nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Access-Control-Request-Method", http.MethodPatch)
			r.Header.Set("Access-Control-Request-Headers", tc.headers)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if tc.allow {
				require.Equal(t, tc.origin, w.Header().Get("Access-Control-Allow-Origin"))
				require.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
			} else {
				require.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}

func TestCORSInvalidCredentialedOrigins(t *testing.T) {
	for _, origin := range []string{"*", "https://*.example.test", "https://user:secret@example.test", "https://app.example.test/path", "https://app.example.test?token=secret", "https://app.example.test#fragment", "null", "//app.example.test", "https://"} {
		t.Run(origin, func(t *testing.T) {
			for _, cfg := range []CORSConfig{{PartnersOrigins: []string{origin}}, {AllowedOrigins: []string{origin}, Handles: true}} {
				mw, err := CORS(cfg)
				require.Error(t, err)
				require.Nil(t, mw)
				require.NotContains(t, err.Error(), origin)
			}
		})
	}
}

func TestHandlePreflightPreservesMutationAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, path, method, origin string
		status                     int
	}{
		{"trusted handle", "/api/v1/ums/me/handle", http.MethodPatch, "https://app.example.test", http.StatusOK},
		{"trusted validation", "/api/v1/ums/me/handle/validate", http.MethodPost, "https://app.example.test", http.StatusOK},
		{"untrusted handle", "/api/v1/ums/me/handle", http.MethodPatch, "https://untrusted.example.test", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := router.NewRouter(nil, nil)
			mw, err := CORS(CORSConfig{AllowedOrigins: []string{"https://app.example.test"}, Handles: true})
			require.NoError(t, err)
			routes.GetRouter().Use(mw)
			routes.GetRouter().HandleFunc(tc.path, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }).Methods(tc.method)
			require.NoError(t, AttachHandlePreflight(routes, "fixture.handle.preflight"))
			require.NoError(t, routes.ValidateRoutePolicies())
			r := httptest.NewRequest(http.MethodOptions, tc.path, nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Access-Control-Request-Method", tc.method)
			r.Header.Set("Access-Control-Request-Headers", "content-type,if-match")
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code)
			if tc.origin == "https://app.example.test" {
				require.Equal(t, tc.origin, w.Header().Get("Access-Control-Allow-Origin"))
			} else {
				require.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
			}
			mutation := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(mutation, httptest.NewRequest(tc.method, tc.path, nil))
			require.Equal(t, http.StatusUnauthorized, mutation.Code)
		})
	}
}

func TestCORSExposesRevisionReplayAndCSRFOnlyOnPartnerRoutes(t *testing.T) {
	for _, tc := range []struct {
		path    string
		partner bool
	}{{"/api/v1/partners", true}, {"/api/v1/partners/overview", true}, {"/api/v1/ums/me/handle", false}, {"/about", false}} {
		t.Run(tc.path, func(t *testing.T) {
			mw, err := CORS(CORSConfig{AllowedOrigins: []string{"https://app.example.test"}, PartnersOrigins: []string{"https://app.example.test"}, Handles: true})
			require.NoError(t, err)
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.Header.Set("Origin", "https://app.example.test")
			w := httptest.NewRecorder()
			mw(http.NotFoundHandler()).ServeHTTP(w, r)
			if tc.partner {
				for _, name := range []string{"Etag", "Idempotency-Replayed", "Retry-After", "X-Csrf-Token"} {
					require.Contains(t, w.Header().Get("Access-Control-Expose-Headers"), name)
				}
			} else {
				require.NotContains(t, w.Header().Get("Access-Control-Expose-Headers"), "X-Csrf-Token")
			}
		})
	}
}

func TestCORSPreservesExplicitPartnerMethodPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, requested  string
		methods          []string
		allowed, invalid bool
	}{
		{name: "default put", requested: http.MethodPut, allowed: true},
		{name: "narrowed patch", requested: http.MethodPatch, methods: []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions}, allowed: true},
		{name: "narrowed put", requested: http.MethodPut, methods: []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions}},
		{name: "empty denies", requested: http.MethodGet, methods: []string{}},
		{name: "unknown rejected", methods: []string{"SECRET-CANARY"}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mw, err := CORS(CORSConfig{PartnersOrigins: []string{"https://app.example.test"}, PartnersMethods: tc.methods})
			if tc.invalid {
				require.Error(t, err)
				require.Nil(t, mw)
				require.NotContains(t, err.Error(), "SECRET-CANARY")
				return
			}
			require.NoError(t, err)
			r := httptest.NewRequest(http.MethodOptions, "/api/v1/partners/overview", nil)
			r.Header.Set("Origin", "https://app.example.test")
			r.Header.Set("Access-Control-Request-Method", tc.requested)
			w := httptest.NewRecorder()
			mw(http.NotFoundHandler()).ServeHTTP(w, r)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			if tc.allowed {
				require.Equal(t, "https://app.example.test", w.Header().Get("Access-Control-Allow-Origin"))
			} else {
				require.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}
