package vision

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
)

type mockVisionRouteHandler struct{ called *string }

func (h *mockVisionRouteHandler) mark(value string, w http.ResponseWriter) {
	*h.called = value
	w.WriteHeader(http.StatusOK)
}

func (h *mockVisionRouteHandler) CreateVision(w http.ResponseWriter, _ *http.Request) {
	h.mark("create", w)
}
func (h *mockVisionRouteHandler) GetVisionByNanoID(w http.ResponseWriter, _ *http.Request) {
	h.mark("get-by-nano-id", w)
}
func (h *mockVisionRouteHandler) GetVisions(w http.ResponseWriter, _ *http.Request) {
	h.mark("list", w)
}
func (h *mockVisionRouteHandler) UpdateVision(w http.ResponseWriter, _ *http.Request) {
	h.mark("update", w)
}
func (h *mockVisionRouteHandler) UpdateVisionStatus(w http.ResponseWriter, _ *http.Request) {
	h.mark("status", w)
}
func (h *mockVisionRouteHandler) SetVisionVote(w http.ResponseWriter, _ *http.Request) {
	h.mark("vote", w)
}
func (h *mockVisionRouteHandler) RemoveVisionVote(w http.ResponseWriter, _ *http.Request) {
	h.mark("unvote", w)
}
func (h *mockVisionRouteHandler) AddVisionComment(w http.ResponseWriter, _ *http.Request) {
	h.mark("comment", w)
}
func (h *mockVisionRouteHandler) SetVisionCommentVote(w http.ResponseWriter, _ *http.Request) {
	h.mark("comment-vote", w)
}
func (h *mockVisionRouteHandler) RemoveVisionCommentVote(w http.ResponseWriter, _ *http.Request) {
	h.mark("comment-unvote", w)
}
func (h *mockVisionRouteHandler) DeleteVision(w http.ResponseWriter, _ *http.Request) {
	h.mark("delete", w)
}
func (h *mockVisionRouteHandler) GetVisionConfig(w http.ResponseWriter, _ *http.Request) {
	h.mark("config", w)
}

func TestAttachRoutes(t *testing.T) {
	tests := []struct {
		method      string
		path        string
		wantHandler string
		wantAuth    string
	}{
		{http.MethodPost, "/api/v1/visions", "create", "authenticated"},
		{http.MethodPatch, "/api/v1/visions/vision-1/status", "status", "admin"},
		{http.MethodGet, "/api/v1/visions", "list", "authenticated"},
		{http.MethodGet, "/api/v1/visions/vision-nano", "get-by-nano-id", "authenticated"},
		{http.MethodPut, "/api/v1/visions/vision-1/votes", "vote", "authenticated"},
		{http.MethodPost, "/api/v1/visions/vision-1/comments", "comment", "authenticated"},
		{http.MethodPut, "/api/v1/visions/vision-1/comments/comment-1/votes", "comment-vote", "authenticated"},
		{http.MethodDelete, "/api/v1/visions/vision-1/comments/comment-1/votes", "comment-unvote", "authenticated"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			called, auth := "", ""
			r := router.NewRouter(nil, nil)
			AttachRoutes(&AttachRoutesRequest{
				Router:  r,
				Handler: &mockVisionRouteHandler{called: &called},
				AdminOnlyMiddleware: func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						auth = "admin"
						next.ServeHTTP(w, r)
					})
				},
				AuthenticatedMiddleware: func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						auth = "authenticated"
						next.ServeHTTP(w, r)
					})
				},
			})

			rec := httptest.NewRecorder()
			r.GetRouter().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != http.StatusOK || called != tt.wantHandler || auth != tt.wantAuth {
				t.Fatalf("status=%d handler=%q auth=%q", rec.Code, called, auth)
			}
		})
	}
}
