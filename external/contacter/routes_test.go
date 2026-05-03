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

const (
	testCommsStatsEndpoint = "/api/v1/ums/comms/stats"
)

type routesMockContacterService struct {
	getCommsStatsFunc func(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error)
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
		serviceErr          error
		expectStatus        int
		expectServiceCalled bool
	}{
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

			adminOnly := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("X-Test-Admin") != "true" {
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte("forbidden"))
						return
					}
					next.ServeHTTP(w, r)
				})
			}

			contacter.AttachRoutes(&contacter.AttachRoutesRequest{
				Router:              r,
				Handler:             h,
				AdminOnlyMiddleware: mux.MiddlewareFunc(adminOnly),
			})

			req := httptest.NewRequest(http.MethodGet, testCommsStatsEndpoint, nil)
			if tt.headerValue != "" {
				req.Header.Set("X-Test-Admin", tt.headerValue)
			}
			rec := httptest.NewRecorder()

			r.GetRouter().ServeHTTP(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code)
			assert.Equal(t, tt.expectServiceCalled, serviceCalled)
		})
	}
}

func TestAttachRoutes_OnlyExpectedRouteRegistered(t *testing.T) {
	t.Parallel()

	svc := &routesMockContacterService{}
	h := contacter.NewHandler(svc, &routesMockValidator{})
	r := router.NewRouter(nil, nil)

	contacter.AttachRoutes(&contacter.AttachRoutesRequest{
		Router:  r,
		Handler: h,
		AdminOnlyMiddleware: func(next http.Handler) http.Handler {
			return next
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ums/comms/unknown", nil)
	rec := httptest.NewRecorder()

	r.GetRouter().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}
