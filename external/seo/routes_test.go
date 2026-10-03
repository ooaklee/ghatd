package seo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/seo"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// unusedSitemapPolicy refuses unexpected grant/usage calls. These routes impose
// an administrator identity boundary without implicitly adding grant requirements.
type unusedSitemapPolicy struct{ calls int }

func (s *unusedSitemapPolicy) Authorize(context.Context, accesspolicy.Subject, []string, []string) error {
	s.calls++
	return router.ErrRouteDenied
}

func (s *unusedSitemapPolicy) ConsumeAuthorized(context.Context, accesspolicy.Consumption) (accesspolicy.Usage, error) {
	s.calls++
	return accesspolicy.Usage{}, router.ErrRouteDenied
}

func TestSitemapVerifiedCredentialAdmission(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, credential, status string
		access                   router.AccessMode
		admin                    bool
		want                     int
	}{
		{"admin session", "session", "ACTIVE", router.AdminSession, true, http.StatusOK},
		{"admin API explicitly enabled", "api", "ACTIVE", router.AdminSessionOrAPI, true, http.StatusOK},
		{"session with either adapter", "session", "ACTIVE", router.AdminSessionOrAPI, true, http.StatusOK},
		{"API refused in default mode", "api", "ACTIVE", "", true, http.StatusUnauthorized},
		{"member API refused", "api", "ACTIVE", router.AdminSessionOrAPI, false, http.StatusForbidden},
		{"member session refused", "session", "ACTIVE", router.AdminSession, false, http.StatusForbidden},
		{"inactive admin refused", "api", "PROVISIONED", router.AdminSessionOrAPI, true, http.StatusForbidden},
		{"missing identity refused", "anonymous", "", router.AdminSessionOrAPI, false, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			store := &unusedSitemapPolicy{}
			guard, err := middleware.NewRoutePolicyGuard("fixture", store, nil)
			require.NoError(t, err)
			require.NoError(t, guard.Install(r))
			handler := &sitemapRouteSpy{}
			// Model an adapter's verified result, not token parsing or live storage.
			// The real guard must still enforce the declared identity/admin boundary.
			adapter := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					if tc.credential == "anonymous" {
						next.ServeHTTP(w, request)
						return
					}
					account := &user.UniversalUser{ID: "fixture-actor", Status: tc.status, Type: "person"}
					if tc.admin {
						account.Roles = []string{"ADMIN"}
					}
					result := &accessmanager.MiddlewareAuthedUserResponse{UserID: account.ID, User: account, Authenticated: true}
					if tc.credential == "api" {
						result.APIToken = &apitoken.CredentialDetails{UserID: account.ID, TokenID: "fixture-credential"}
					} else {
						result.Token = &auth.TokenAccessDetails{UserID: account.ID, UserType: account.Type, AccessUUID: "fixture-session"}
					}
					ctx, err := middleware.ContextWithAuthentication(request.Context(), result)
					require.NoError(t, err)
					next.ServeHTTP(w, request.WithContext(ctx))
				})
			}
			seo.AttachRoutes(&seo.AttachRoutesRequest{Router: r, Handler: handler, AdminOnlyMiddleware: adapter, AdminAccess: tc.access})
			require.NoError(t, r.ValidateRoutePolicies())
			recorder := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/seo/sitemap-items", nil))
			require.Equal(t, tc.want, recorder.Code)
			if tc.want == http.StatusOK {
				require.Equal(t, "GetSitemapItems", handler.called)
			} else {
				require.Empty(t, handler.called)
			}
			require.Zero(t, store.calls)
		})
	}
}

func TestSitemapAdminCredentialConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		access  router.AccessMode
		missing bool
		want    router.AccessMode
	}{
		{"default session", "", false, router.AdminSession},
		{"explicit session", router.AdminSession, false, router.AdminSession},
		{"explicit API or session", router.AdminSessionOrAPI, false, router.AdminSessionOrAPI},
		{"API mode still requires middleware", router.AdminSessionOrAPI, true, ""},
		{"public downgrade", router.Public, false, ""},
		{"proof downgrade", router.HandlerVerified, false, ""},
		{"member downgrade", router.ActiveSessionOrAPI, false, ""},
		{"unknown mode", "mistyped", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			var evaluated router.AccessMode
			require.NoError(t, r.SetRouteAuthorizer(func(ctx context.Context, req *http.Request, def router.RouteDefinition) error {
				evaluated = def.Access
				return router.ErrRouteDenied // Stop before domain work; tests admission metadata.
			}))
			var admin mux.MiddlewareFunc = func(next http.Handler) http.Handler { return next }
			if tc.missing {
				admin = nil
			}
			seo.AttachRoutes(&seo.AttachRoutesRequest{Router: r, Handler: &sitemapRouteSpy{}, AdminOnlyMiddleware: admin, AdminAccess: tc.access})
			recorder := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/seo/sitemap-items", nil))
			if tc.want == "" {
				require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
				require.Empty(t, evaluated)
			} else {
				require.NoError(t, r.ValidateRoutePolicies())
				require.Equal(t, http.StatusForbidden, recorder.Code)
				require.Equal(t, tc.want, evaluated)
			}
		})
	}
}

// sitemapRouteSpy records transport dispatch without invoking storage or files.
type sitemapRouteSpy struct{ called string }

func (s *sitemapRouteSpy) CreateSitemapItem(http.ResponseWriter, *http.Request) {
	s.called = "CreateSitemapItem"
}
func (s *sitemapRouteSpy) DeleteEntriesWithUriRegex(http.ResponseWriter, *http.Request) {
	s.called = "DeleteEntriesWithUriRegex"
}
func (s *sitemapRouteSpy) DownloadSitemapByPath(http.ResponseWriter, *http.Request) {
	s.called = "DownloadSitemapByPath"
}
func (s *sitemapRouteSpy) GenerateSitemap(http.ResponseWriter, *http.Request) {
	s.called = "GenerateSitemap"
}
func (s *sitemapRouteSpy) GetSitemap(http.ResponseWriter, *http.Request) { s.called = "GetSitemap" }
func (s *sitemapRouteSpy) GetSitemapItems(http.ResponseWriter, *http.Request) {
	s.called = "GetSitemapItems"
}
func (s *sitemapRouteSpy) MassSitemapItemCreationByBatch(http.ResponseWriter, *http.Request) {
	s.called = "MassSitemapItemCreationByBatch"
}
func (s *sitemapRouteSpy) UpdateSitemapItemByUri(http.ResponseWriter, *http.Request) {
	s.called = "UpdateSitemapItemByUri"
}

func TestSitemapRouteAdmission(t *testing.T) {
	t.Parallel()
	routes := []struct {
		path, method, operation string
		public                  bool
	}{
		{"/sitemap.xml", http.MethodGet, "GetSitemap", true},
		{"/sitemap.xml", http.MethodOptions, "GetSitemap", true},
		{"/api/v1/seo/sitemap-items", http.MethodPost, "CreateSitemapItem", false},
		{"/api/v1/seo/sitemap-items", http.MethodGet, "GetSitemapItems", false},
		{"/api/v1/seo/sitemap-items", http.MethodPatch, "UpdateSitemapItemByUri", false},
		{"/api/v1/seo/sitemap-items", http.MethodDelete, "DeleteEntriesWithUriRegex", false},
		// Shared OPTIONS deliberately retains the first registration's handler.
		{"/api/v1/seo/sitemap-items", http.MethodOptions, "CreateSitemapItem", false},
		{"/api/v1/seo/sitemap-items/batch", http.MethodPost, "MassSitemapItemCreationByBatch", false},
		{"/api/v1/seo/sitemap-items/batch", http.MethodOptions, "MassSitemapItemCreationByBatch", false},
		{"/api/v1/seo/sitemap.xml/generate", http.MethodPost, "GenerateSitemap", false},
		{"/api/v1/seo/sitemap.xml/generate", http.MethodOptions, "GenerateSitemap", false},
		{"/api/v1/seo/sitemap.xml/download", http.MethodGet, "DownloadSitemapByPath", false},
		{"/api/v1/seo/sitemap.xml/download", http.MethodOptions, "DownloadSitemapByPath", false},
	}
	for _, route := range routes {
		t.Run(route.method+route.path, func(t *testing.T) {
			for _, mode := range []string{"allowed", "admin-denied", "missing-middleware", "policy-denied"} {
				t.Run(mode, func(t *testing.T) {
					t.Parallel()
					r := router.NewRouter(nil, nil)
					handler := &sitemapRouteSpy{}
					middlewareCalled, policyCalled := false, false
					require.NoError(t, r.SetRouteAuthorizer(func(ctx context.Context, request *http.Request, definition router.RouteDefinition) error {
						policyCalled = true
						require.Equal(t, !route.public, middlewareCalled, "authentication must precede policy")
						require.Equal(t, route.path, definition.Path)
						require.Equal(t, "seo."+route.operation, definition.Operation)
						require.Contains(t, definition.Methods, route.method)
						if route.public {
							require.Equal(t, router.Public, definition.Access)
						} else {
							require.Equal(t, router.AdminSession, definition.Access)
						}
						if mode == "policy-denied" {
							return router.ErrRouteDenied
						}
						return nil
					}))
					var admin mux.MiddlewareFunc
					if mode != "missing-middleware" {
						admin = func(next http.Handler) http.Handler {
							return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
								middlewareCalled = true
								if mode == "admin-denied" {
									w.WriteHeader(http.StatusForbidden)
									return
								}
								next.ServeHTTP(w, request)
							})
						}
					}
					seo.AttachRoutes(&seo.AttachRoutesRequest{Router: r, Handler: handler, AdminOnlyMiddleware: admin})
					if mode == "missing-middleware" {
						require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
					} else {
						require.NoError(t, r.ValidateRoutePolicies())
					}
					recorder := httptest.NewRecorder()
					r.GetRouter().ServeHTTP(recorder, httptest.NewRequest(route.method, route.path, nil))
					switch {
					case mode == "missing-middleware":
						// Registry errors close every descriptor, including public ones.
						require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
						require.Contains(t, recorder.Body.String(), "ROUTE_CONFIGURATION")
						require.False(t, policyCalled)
						require.Empty(t, handler.called)
					case mode == "admin-denied" && !route.public:
						require.Equal(t, http.StatusForbidden, recorder.Code)
						require.False(t, policyCalled)
						require.Empty(t, handler.called)
					case mode == "policy-denied":
						require.Equal(t, http.StatusForbidden, recorder.Code)
						require.True(t, policyCalled)
						require.Empty(t, handler.called)
					default:
						require.Equal(t, http.StatusOK, recorder.Code)
						require.True(t, policyCalled)
						require.Equal(t, route.operation, handler.called)
					}
				})
			}
		})
	}
}

func TestSitemapRouteInventory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path, operation, method string
		access                  router.AccessMode
		shadowed                bool
	}{
		{"/sitemap.xml", "GetSitemap", http.MethodGet, router.Public, false},
		{"/api/v1/seo/sitemap-items", "CreateSitemapItem", http.MethodPost, router.AdminSession, false},
		{"/api/v1/seo/sitemap-items", "GetSitemapItems", http.MethodGet, router.AdminSession, true},
		{"/api/v1/seo/sitemap-items", "UpdateSitemapItemByUri", http.MethodPatch, router.AdminSession, true},
		{"/api/v1/seo/sitemap-items", "DeleteEntriesWithUriRegex", http.MethodDelete, router.AdminSession, true},
		{"/api/v1/seo/sitemap-items/batch", "MassSitemapItemCreationByBatch", http.MethodPost, router.AdminSession, false},
		{"/api/v1/seo/sitemap.xml/generate", "GenerateSitemap", http.MethodPost, router.AdminSession, false},
		{"/api/v1/seo/sitemap.xml/download", "DownloadSitemapByPath", http.MethodGet, router.AdminSession, false},
	}
	r := router.NewRouter(nil, nil)
	seo.AttachRoutes(&seo.AttachRoutesRequest{Router: r, Handler: &sitemapRouteSpy{}, AdminOnlyMiddleware: func(next http.Handler) http.Handler { return next }})
	require.NoError(t, r.ValidateRoutePolicies())
	registry := r.RouteInventory()
	require.Len(t, registry, len(tests))
	for i, expected := range tests {
		t.Run(expected.operation, func(t *testing.T) {
			actual := registry[i]
			require.Equal(t, expected.path, actual.Path)
			require.Equal(t, "seo."+expected.operation, actual.Operation)
			require.Equal(t, expected.access, actual.Access)
			require.Equal(t, []string{expected.method, http.MethodOptions}, actual.Methods)
			if expected.shadowed {
				require.Equal(t, []string{http.MethodOptions}, actual.ShadowedMethods)
			} else {
				require.Empty(t, actual.ShadowedMethods)
			}
		})
	}
}

func TestSitemapRouteUnknownInputs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, path string
		status             int
	}{
		{"unknown admin path", http.MethodGet, "/api/v1/seo/unknown", http.StatusNotFound},
		{"unknown public suffix", http.MethodGet, "/sitemap.xml/private", http.StatusNotFound},
		{"unsupported admin method", http.MethodPut, "/api/v1/seo/sitemap-items", http.StatusNotFound},
		{"unsupported public method", http.MethodPost, "/sitemap.xml", http.StatusMethodNotAllowed},
		{"implicit HEAD not admitted", http.MethodHead, "/sitemap.xml", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			handler := &sitemapRouteSpy{}
			seo.AttachRoutes(&seo.AttachRoutesRequest{Router: r, Handler: handler, AdminOnlyMiddleware: func(next http.Handler) http.Handler { return next }})
			recorder := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
			require.Equal(t, tc.status, recorder.Code, "preserve the legacy Mux attachment's status")
			require.Empty(t, handler.called)
		})
	}
}
