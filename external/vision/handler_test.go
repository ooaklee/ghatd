package vision

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

type mockVisionHTTPService struct {
	createFunc  func(ctx context.Context, r *CreateVisionRequest) (*VisionResponse, error)
	getByIDFunc func(ctx context.Context, r *GetVisionByIDRequest) (*VisionResponse, error)
	getFunc     func(ctx context.Context, r *GetVisionsRequest) (*GetVisionsResponse, error)
}

func (m *mockVisionHTTPService) CreateVision(ctx context.Context, r *CreateVisionRequest) (*VisionResponse, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, r)
	}
	return &VisionResponse{Vision: &Vision{ID: "bp-1", Name: r.Name, Kind: r.Kind}}, nil
}

func (m *mockVisionHTTPService) GetVisionByID(ctx context.Context, r *GetVisionByIDRequest) (*VisionResponse, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, r)
	}
	return &VisionResponse{Vision: &Vision{ID: r.ID, Name: "Starter API", Kind: "service"}}, nil
}

func (m *mockVisionHTTPService) GetVisions(ctx context.Context, r *GetVisionsRequest) (*GetVisionsResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, r)
	}
	return &GetVisionsResponse{Visions: []Vision{{ID: "bp-1", Name: "Starter API", Kind: "service"}}, Total: 1}, nil
}

func TestHandlerCreateVision(t *testing.T) {
	var captured *CreateVisionRequest
	handler := NewHandler(&mockVisionHTTPService{
		createFunc: func(ctx context.Context, r *CreateVisionRequest) (*VisionResponse, error) {
			captured = r
			return &VisionResponse{Vision: &Vision{ID: "bp-1", Name: r.Name, Kind: r.Kind}}, nil
		},
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/visions", bytes.NewBufferString(`{"name":"Starter API","kind":"Service"}`))
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	handler.CreateVision(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if captured == nil || captured.CreatedByUserID != "user-1" {
		t.Fatalf("captured request = %+v, want context user", captured)
	}
}

func TestHandlerGetVisions(t *testing.T) {
	var captured *GetVisionsRequest
	handler := NewHandler(&mockVisionHTTPService{
		getFunc: func(ctx context.Context, r *GetVisionsRequest) (*GetVisionsResponse, error) {
			captured = r
			return &GetVisionsResponse{Visions: []Vision{{ID: "bp-1"}}, Total: 1}, nil
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/visions?query=starter", nil)
	rec := httptest.NewRecorder()

	handler.GetVisions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured == nil || captured.Query != "starter" {
		t.Fatalf("captured request = %+v, want decoded query", captured)
	}
}

func TestHandlerGetVisionByID(t *testing.T) {
	var captured *GetVisionByIDRequest
	handler := NewHandler(&mockVisionHTTPService{
		getByIDFunc: func(ctx context.Context, r *GetVisionByIDRequest) (*VisionResponse, error) {
			captured = r
			return &VisionResponse{Vision: &Vision{ID: r.ID}}, nil
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/visions/bp-1", nil)
	req = mux.SetURLVars(req, map[string]string{VisionURIVariableID: "bp-1"})
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	handler.GetVisionByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured == nil || captured.ID != "bp-1" || captured.UserID != "user-1" {
		t.Fatalf("captured request = %+v, want path ID and context user", captured)
	}
}
