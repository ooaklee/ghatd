package blueprint

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
)

type mockBlueprintRouteHandler struct {
	called *string
}

func (h *mockBlueprintRouteHandler) CreateBlueprint(w http.ResponseWriter, r *http.Request) {
	*h.called = "create"
	w.WriteHeader(http.StatusOK)
}

func (h *mockBlueprintRouteHandler) GetBlueprintByID(w http.ResponseWriter, r *http.Request) {
	*h.called = "get-by-id"
	w.WriteHeader(http.StatusOK)
}

func (h *mockBlueprintRouteHandler) GetBlueprints(w http.ResponseWriter, r *http.Request) {
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
			name:           "SUCCESS - POST blueprints uses admin middleware",
			method:         http.MethodPost,
			path:           "/api/v1/blueprints",
			wantHandler:    "create",
			wantMiddleware: "admin",
		},
		{
			name:           "SUCCESS - GET blueprints uses authenticated middleware",
			method:         http.MethodGet,
			path:           "/api/v1/blueprints",
			wantHandler:    "get",
			wantMiddleware: "authenticated",
		},
		{
			name:           "SUCCESS - GET blueprint by ID uses authenticated middleware",
			method:         http.MethodGet,
			path:           "/api/v1/blueprints/bp-1",
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
				Handler: &mockBlueprintRouteHandler{called: &calledHandler},
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
