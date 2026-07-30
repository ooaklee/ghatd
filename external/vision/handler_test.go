package vision

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
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
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/visions",
		bytes.NewBufferString(`{"title":"Better search","type":"feedback"}`),
	)
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	NewHandler(&mockVisionHTTPService{}, nil).CreateVision(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestHandlerRejectsInvalidPayload(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/visions", bytes.NewBufferString(`{`))

	NewHandler(&mockVisionHTTPService{}, nil).CreateVision(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
