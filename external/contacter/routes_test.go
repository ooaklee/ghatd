package contacter_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/router"
)

func TestCommsAdminCredentialConfiguration(t *testing.T) {
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
			contacter.AttachRoutes(&contacter.AttachRoutesRequest{Router: r, Handler: contacter.NewHandler(&routesMockContacterService{}, &routesMockValidator{}), AdminOnlyMiddleware: admin, AdminAccess: tc.access})
			recorder := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/comms/stats", nil))
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

const (
	testCommsStatsEndpoint = "/api/v1/comms/stats"
	testCommsTypesEndpoint = "/api/v1/comms/types"
)

type routesMockContacterService struct {
	getCommsStatsFunc func(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error)
	typesCalled       bool
}

func (m *routesMockContacterService) CreateComms(ctx context.Context, req *contacter.CreateCommsRequest) (*contacter.CreateCommsResponse, error) {
	return nil, nil
}

func (m *routesMockContacterService) GetComms(ctx context.Context, req *contacter.GetCommsRequest) (*contacter.GetCommsResponse, error) {
	return nil, nil
}

func (m *routesMockContacterService) UpdateComms(ctx context.Context, req *contacter.UpdateCommsRequest) (*contacter.UpdateCommsResponse, error) {
	return nil, nil
}

func (m *routesMockContacterService) GetCommsStats(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error) {
	if m.getCommsStatsFunc != nil {
		return m.getCommsStatsFunc(ctx, req)
	}
	return &contacter.GetCommsStatsResponse{CommsStats: &contacter.CommsStats{}}, nil
}

func (m *routesMockContacterService) GetAvailableCommsTypes(context.Context) (*contacter.GetAvailableCommsTypesResponse, error) {
	m.typesCalled = true
	return &contacter.GetAvailableCommsTypesResponse{CommsTypes: contacter.DefaultCommsTypeMap()}, nil
}

type routesMockValidator struct {
	validateFunc func(s interface{}) error
}

func (m *routesMockValidator) Validate(s interface{}) error {
	if m.validateFunc != nil {
		return m.validateFunc(s)
	}
	return nil
}

func TestAttachRoutes_CommsStatsRouteAndAdminMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		headerValue         string
		method              string
		missingMiddleware   bool
		policyDenied        bool
		serviceErr          error
		expectStatus        int
		expectServiceCalled bool
	}{
		{
			name:              "Failure - missing middleware is not public",
			missingMiddleware: true,
			expectStatus:      http.StatusServiceUnavailable,
		},
		{
			name:         "Failure - policy denies after admin middleware",
			headerValue:  "true",
			policyDenied: true,
			expectStatus: http.StatusForbidden,
		},
		{
			name:                "Success - OPTIONS retains admin protection and handler",
			headerValue:         "true",
			method:              http.MethodOptions,
			expectStatus:        http.StatusOK,
			expectServiceCalled: true,
		},
		{
			name:         "Failure - OPTIONS cannot bypass admin middleware",
			method:       http.MethodOptions,
			expectStatus: http.StatusForbidden,
		},
		{
			name:                "Success - admin request reaches handler",
			headerValue:         "true",
			expectStatus:        http.StatusOK,
			expectServiceCalled: true,
		},
		{
			name:                "Failure - non-admin request blocked by middleware",
			headerValue:         "",
			expectStatus:        http.StatusForbidden,
			expectServiceCalled: false,
		},
		{
			name:                "Failure - admin request with service error returns 500",
			headerValue:         "true",
			serviceErr:          errors.New("boom"),
			expectStatus:        http.StatusInternalServerError,
			expectServiceCalled: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			serviceCalled := false
			svc := &routesMockContacterService{
				getCommsStatsFunc: func(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error) {
					serviceCalled = true
					if tt.serviceErr != nil {
						return nil, tt.serviceErr
					}
					return &contacter.GetCommsStatsResponse{CommsStats: &contacter.CommsStats{Total: 7}}, nil
				},
			}

			h := contacter.NewHandler(svc, &routesMockValidator{})
			r := router.NewRouter(nil, nil)
			middlewareCalled, policyCalled := false, false
			require.NoError(t, r.SetRouteAuthorizer(func(ctx context.Context, request *http.Request, definition router.RouteDefinition) error {
				policyCalled = true
				require.True(t, middlewareCalled)
				require.Equal(t, testCommsStatsEndpoint, definition.Path)
				require.Equal(t, "contacter.GetCommsStats", definition.Operation)
				require.Equal(t, router.AdminSession, definition.Access)
				if tt.policyDenied {
					return router.ErrRouteDenied
				}
				return nil
			}))

			var adminOnly mux.MiddlewareFunc = func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					middlewareCalled = true
					if r.Header.Get("X-Test-Admin") != "true" {
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte("forbidden"))
						return
					}
					next.ServeHTTP(w, r)
				})
			}
			if tt.missingMiddleware {
				adminOnly = nil
			}

			contacter.AttachRoutes(&contacter.AttachRoutesRequest{
				Router:              r,
				Handler:             h,
				AdminOnlyMiddleware: mux.MiddlewareFunc(adminOnly),
			})

			if tt.missingMiddleware {
				require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
			} else {
				require.NoError(t, r.ValidateRoutePolicies())
			}
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, testCommsStatsEndpoint, nil)
			if tt.headerValue != "" {
				req.Header.Set("X-Test-Admin", tt.headerValue)
			}
			rec := httptest.NewRecorder()

			r.GetRouter().ServeHTTP(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code)
			assert.Equal(t, tt.expectServiceCalled, serviceCalled)
			assert.Equal(t, !tt.missingMiddleware && tt.headerValue == "true", policyCalled)
			assert.False(t, svc.typesCalled)
			if tt.missingMiddleware {
				require.Contains(t, rec.Body.String(), "ROUTE_CONFIGURATION")
			}
		})
	}
}

func TestAttachRoutes_PublicAndUnmatchedInputs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, path string
		missingMiddleware  bool
		status             int
		public             bool
	}{
		{"public types", http.MethodGet, testCommsTypesEndpoint, false, http.StatusOK, true},
		{"public OPTIONS", http.MethodOptions, testCommsTypesEndpoint, false, http.StatusOK, true},
		{"unknown path", http.MethodGet, "/api/v1/comms/unknown", false, http.StatusNotFound, false},
		{"types suffix", http.MethodGet, testCommsTypesEndpoint + "/unknown", false, http.StatusNotFound, false},
		{"unsupported public method", http.MethodPost, testCommsTypesEndpoint, false, 0, false},
		{"unsupported admin method", http.MethodPost, testCommsStatsEndpoint, false, 0, false},
		{"invalid registry closes public descriptors", http.MethodGet, testCommsTypesEndpoint, true, http.StatusServiceUnavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := &routesMockContacterService{}
			r := router.NewRouter(nil, nil)
			middlewareCalled, policyCalled := false, false
			require.NoError(t, r.SetRouteAuthorizer(func(ctx context.Context, req *http.Request, def router.RouteDefinition) error {
				policyCalled = true
				require.False(t, middlewareCalled)
				require.Equal(t, router.Public, def.Access)
				require.Equal(t, "contacter.GetAvailableCommsTypes", def.Operation)
				require.Equal(t, testCommsTypesEndpoint, def.Path)
				return nil
			}))
			var admin mux.MiddlewareFunc = func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					middlewareCalled = true
					w.WriteHeader(http.StatusForbidden)
				})
			}
			if tc.missingMiddleware {
				admin = nil
			}
			contacter.AttachRoutes(&contacter.AttachRoutesRequest{Router: r, Handler: contacter.NewHandler(svc, &routesMockValidator{}), AdminOnlyMiddleware: admin})
			rec := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if tc.status == 0 {
				require.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, rec.Code)
			} else {
				require.Equal(t, tc.status, rec.Code)
			}
			require.False(t, middlewareCalled)
			require.Equal(t, tc.public, policyCalled)
			require.Equal(t, tc.public, svc.typesCalled)
		})
	}
}

func TestAttachRoutes_Inventory(t *testing.T) {
	t.Parallel()
	r := router.NewRouter(nil, nil)
	contacter.AttachRoutes(&contacter.AttachRoutesRequest{Router: r, Handler: contacter.NewHandler(&routesMockContacterService{}, &routesMockValidator{}), AdminOnlyMiddleware: func(next http.Handler) http.Handler { return next }})
	require.NoError(t, r.ValidateRoutePolicies())
	registry := r.RouteInventory()
	require.Len(t, registry, 2)
	for i, expected := range []struct {
		path, operation string
		access          router.AccessMode
	}{
		{testCommsTypesEndpoint, "contacter.GetAvailableCommsTypes", router.Public},
		{testCommsStatsEndpoint, "contacter.GetCommsStats", router.AdminSession},
	} {
		t.Run(expected.operation, func(t *testing.T) {
			require.Equal(t, expected.path, registry[i].Path)
			require.Equal(t, expected.operation, registry[i].Operation)
			require.Equal(t, expected.access, registry[i].Access)
			require.Equal(t, []string{http.MethodGet, http.MethodOptions}, registry[i].Methods)
		})
	}
}
