package blueprint

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

type mockBlueprintHTTPService struct {
	createFunc  func(ctx context.Context, r *CreateBlueprintRequest) (*BlueprintResponse, error)
	getByIDFunc func(ctx context.Context, r *GetBlueprintByIDRequest) (*BlueprintResponse, error)
	getFunc     func(ctx context.Context, r *GetBlueprintsRequest) (*GetBlueprintsResponse, error)
}

func (m *mockBlueprintHTTPService) CreateBlueprint(ctx context.Context, r *CreateBlueprintRequest) (*BlueprintResponse, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, r)
	}
	return &BlueprintResponse{Blueprint: &Blueprint{ID: "bp-1", Name: r.Name, Kind: r.Kind}}, nil
}

func (m *mockBlueprintHTTPService) GetBlueprintByID(ctx context.Context, r *GetBlueprintByIDRequest) (*BlueprintResponse, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, r)
	}
	return &BlueprintResponse{Blueprint: &Blueprint{ID: r.ID, Name: "Starter API", Kind: "service"}}, nil
}

func (m *mockBlueprintHTTPService) GetBlueprints(ctx context.Context, r *GetBlueprintsRequest) (*GetBlueprintsResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, r)
	}
	return &GetBlueprintsResponse{Blueprints: []Blueprint{{ID: "bp-1", Name: "Starter API", Kind: "service"}}, Total: 1}, nil
}

func TestHandlerCreateBlueprint(t *testing.T) {
	var captured *CreateBlueprintRequest
	handler := NewHandler(&mockBlueprintHTTPService{
		createFunc: func(ctx context.Context, r *CreateBlueprintRequest) (*BlueprintResponse, error) {
			captured = r
			return &BlueprintResponse{Blueprint: &Blueprint{ID: "bp-1", Name: r.Name, Kind: r.Kind}}, nil
		},
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/blueprints", bytes.NewBufferString(`{"name":"Starter API","kind":"Service"}`))
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	handler.CreateBlueprint(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if captured == nil || captured.CreatedByUserID != "user-1" {
		t.Fatalf("captured request = %+v, want context user", captured)
	}
}

func TestHandlerGetBlueprints(t *testing.T) {
	var captured *GetBlueprintsRequest
	handler := NewHandler(&mockBlueprintHTTPService{
		getFunc: func(ctx context.Context, r *GetBlueprintsRequest) (*GetBlueprintsResponse, error) {
			captured = r
			return &GetBlueprintsResponse{Blueprints: []Blueprint{{ID: "bp-1"}}, Total: 1}, nil
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints?query=starter", nil)
	rec := httptest.NewRecorder()

	handler.GetBlueprints(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured == nil || captured.Query != "starter" {
		t.Fatalf("captured request = %+v, want decoded query", captured)
	}
}

func TestHandlerGetBlueprintByID(t *testing.T) {
	var captured *GetBlueprintByIDRequest
	handler := NewHandler(&mockBlueprintHTTPService{
		getByIDFunc: func(ctx context.Context, r *GetBlueprintByIDRequest) (*BlueprintResponse, error) {
			captured = r
			return &BlueprintResponse{Blueprint: &Blueprint{ID: r.ID}}, nil
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints/bp-1", nil)
	req = mux.SetURLVars(req, map[string]string{BlueprintURIVariableID: "bp-1"})
	req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	handler.GetBlueprintByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured == nil || captured.ID != "bp-1" || captured.UserID != "user-1" {
		t.Fatalf("captured request = %+v, want path ID and context user", captured)
	}
}
