package vision

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
)

type mockVisionRouteHandler struct {
	called *string
}

func (h *mockVisionRouteHandler) CreateVision(w http.ResponseWriter, r *http.Request) {
	*h.called = "create"
	w.WriteHeader(http.StatusOK)
}

func (h *mockVisionRouteHandler) GetVisionByID(w http.ResponseWriter, r *http.Request) {
	*h.called = "get-by-id"
	w.WriteHeader(http.StatusOK)
}

func (h *mockVisionRouteHandler) GetVisions(w http.ResponseWriter, r *http.Request) {
	*h.called = "get"
	w.WriteHeader(http.StatusOK)
}

func TestAttachRoutes(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		wantHandler    string
		wantMiddleware string
	}{
		{
			name:           "SUCCESS - POST visions uses admin middleware",
			method:         http.MethodPost,
			path:           "/api/v1/visions",
			wantHandler:    "create",
			wantMiddleware: "admin",
		},
		{
			name:           "SUCCESS - GET visions uses authenticated middleware",
			method:         http.MethodGet,
			path:           "/api/v1/visions",
			wantHandler:    "get",
			wantMiddleware: "authenticated",
		},
		{
			name:           "SUCCESS - GET vision by ID uses authenticated middleware",
			method:         http.MethodGet,
			path:           "/api/v1/visions/bp-1",
			wantHandler:    "get-by-id",
			wantMiddleware: "authenticated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calledHandler := ""
			calledMiddleware := ""

			r := router.NewRouter(nil, nil)
			AttachRoutes(&AttachRoutesRequest{
				Router:  r,
				Handler: &mockVisionRouteHandler{called: &calledHandler},
				AdminOnlyMiddleware: func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calledMiddleware = "admin"
						next.ServeHTTP(w, r)
					})
				},
				AuthenticatedMiddleware: func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calledMiddleware = "authenticated"
						next.ServeHTTP(w, r)
					})
				},
			})

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			r.GetRouter().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if calledHandler != tt.wantHandler {
				t.Fatalf("handler = %s, want %s", calledHandler, tt.wantHandler)
			}
			if calledMiddleware != tt.wantMiddleware {
				t.Fatalf("middleware = %s, want %s", calledMiddleware, tt.wantMiddleware)
			}
		})
	}
}
