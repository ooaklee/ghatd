package usermanager

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
)

type mockUsermanagerVisionRouteHandler struct {
	UsermanagerHandler
	called *string
}

func (h *mockUsermanagerVisionRouteHandler) mark(value string, w http.ResponseWriter) {
	*h.called = value
	w.WriteHeader(http.StatusOK)
}

func (h *mockUsermanagerVisionRouteHandler) CreateVision(w http.ResponseWriter, _ *http.Request) {
	h.mark("create", w)
}

func (h *mockUsermanagerVisionRouteHandler) GetVisions(w http.ResponseWriter, _ *http.Request) {
	h.mark("list", w)
}

func (h *mockUsermanagerVisionRouteHandler) GetVisionByNanoID(w http.ResponseWriter, _ *http.Request) {
	h.mark("detail", w)
}

func (h *mockUsermanagerVisionRouteHandler) GetVisionConfig(w http.ResponseWriter, _ *http.Request) {
	h.mark("config", w)
}

func (h *mockUsermanagerVisionRouteHandler) UpdateVision(w http.ResponseWriter, _ *http.Request) {
	h.mark("edit", w)
}

func (h *mockUsermanagerVisionRouteHandler) UpdateVisionStatus(w http.ResponseWriter, _ *http.Request) {
	h.mark("status", w)
}

func (h *mockUsermanagerVisionRouteHandler) DeleteVision(w http.ResponseWriter, _ *http.Request) {
	h.mark("delete", w)
}

func TestVisionReadRoutesUseOptionalAuthAndWritesRemainStrict(t *testing.T) {
	tests := []struct {
		method     string
		path       string
		wantCall   string
		wantAccess string
	}{
		{method: http.MethodGet, path: "/api/v1/ums/visions", wantCall: "list", wantAccess: "optional"},
		{method: http.MethodGet, path: "/api/v1/ums/visions/config", wantCall: "config", wantAccess: "optional"},
		{method: http.MethodGet, path: "/api/v1/ums/visions/public-nano", wantCall: "detail", wantAccess: "optional"},
		{method: http.MethodPost, path: "/api/v1/ums/visions", wantCall: "create", wantAccess: "strict"},
		{method: http.MethodPatch, path: "/api/v1/ums/visions/public-nano", wantCall: "edit", wantAccess: "strict"},
		{method: http.MethodPatch, path: "/api/v1/ums/visions/public-nano/status", wantCall: "status", wantAccess: "admin"},
		{method: http.MethodDelete, path: "/api/v1/ums/visions/public-nano", wantCall: "delete", wantAccess: "strict"},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			called, access := "", ""
			r := router.NewRouter(nil, nil)
			AttachRoutes(&AttachRoutesRequest{
				Router:  r,
				Handler: &mockUsermanagerVisionRouteHandler{called: &called},
				RateLimitOrActiveMiddleware: func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						access = "optional"
						next.ServeHTTP(w, r)
					})
				},
				ValidApiTokenOrJWTMiddleware: func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						access = "strict"
						next.ServeHTTP(w, r)
					})
				},
				AdminOnlyMiddleware: func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						access = "admin"
						next.ServeHTTP(w, r)
					})
				},
			})

			response := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, nil)
			r.GetRouter().ServeHTTP(response, request)

			if response.Code != http.StatusOK || called != test.wantCall || access != test.wantAccess {
				t.Fatalf(
					"status=%d handler=%q access=%q, want handler=%q access=%q",
					response.Code,
					called,
					access,
					test.wantCall,
					test.wantAccess,
				)
			}
		})
	}
}
