package vision

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockVisionHTTPService struct{}

func (*mockVisionHTTPService) CreateVision(_ context.Context, req *CreateVisionRequest) (*VisionResponse, error) {
	return &VisionResponse{Vision: &Vision{ID: "vision-1", Title: req.Title, Type: req.Type}}, nil
}
func (*mockVisionHTTPService) GetVisionByNanoID(context.Context, *GetVisionByNanoIDRequest) (*VisionResponse, error) {
	return &VisionResponse{Vision: &Vision{ID: "vision-1"}}, nil
}
func (*mockVisionHTTPService) GetVisions(context.Context, *GetVisionsRequest) (*GetVisionsResponse, error) {
	return &GetVisionsResponse{Visions: []Vision{}, Total: 0}, nil
}
func (*mockVisionHTTPService) UpdateVision(context.Context, *UpdateVisionRequest) (*VisionResponse, error) {
	return &VisionResponse{Vision: &Vision{ID: "vision-1"}}, nil
}
func (*mockVisionHTTPService) UpdateVisionStatus(context.Context, *UpdateVisionStatusRequest) (*VisionResponse, error) {
	return &VisionResponse{Vision: &Vision{ID: "vision-1"}}, nil
}
func (*mockVisionHTTPService) SetVisionVote(context.Context, *SetVisionVoteRequest) (*VisionResponse, error) {
	return &VisionResponse{Vision: &Vision{ID: "vision-1"}}, nil
}
func (*mockVisionHTTPService) RemoveVisionVote(context.Context, *RemoveVisionVoteRequest) (*VisionResponse, error) {
	return &VisionResponse{Vision: &Vision{ID: "vision-1"}}, nil
}
func (*mockVisionHTTPService) AddVisionComment(context.Context, *AddVisionCommentRequest) (*VisionResponse, error) {
	return &VisionResponse{Vision: &Vision{ID: "vision-1"}}, nil
}
func (*mockVisionHTTPService) SetVisionCommentVote(context.Context, *SetVisionCommentVoteRequest) (*VisionResponse, error) {
	return &VisionResponse{Vision: &Vision{ID: "vision-1"}}, nil
}
func (*mockVisionHTTPService) RemoveVisionCommentVote(context.Context, *RemoveVisionCommentVoteRequest) (*VisionResponse, error) {
	return &VisionResponse{Vision: &Vision{ID: "vision-1"}}, nil
}
func (*mockVisionHTTPService) DeleteVision(context.Context, *DeleteVisionRequest) (*DeleteVisionResponse, error) {
	return &DeleteVisionResponse{Deleted: true}, nil
}
func (*mockVisionHTTPService) GetVisionConfig(context.Context) (*GetVisionConfigResponse, error) {
	return &GetVisionConfigResponse{Config: DefaultVisionConfig().toCapabilities()}, nil
}

func TestHandlerCreateVision(t *testing.T) {
	for _, tc := range []struct {
		name, body    string
		authenticated bool
		status        int
	}{
		{"valid", `{"title":"Better search","type":"feedback"}`, true, http.StatusCreated},
		{"invalid payload", `{`, true, http.StatusBadRequest},
		{"missing authentication", `{"title":"Better search","type":"feedback"}`, false, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/visions", bytes.NewBufferString(tc.body))
			if tc.authenticated {
				req = req.WithContext(authenticatedActor(req.Context(), "user-1"))
			}
			rec := httptest.NewRecorder()
			NewHandler(&mockVisionHTTPService{}, nil).CreateVision(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}
