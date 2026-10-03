package blueprint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/stretchr/testify/require"
)

type mockBlueprintHTTPService struct {
	createFunc  func(context.Context, *CreateBlueprintRequest) (*BlueprintResponse, error)
	getByIDFunc func(context.Context, *GetBlueprintByIDRequest) (*BlueprintResponse, error)
	getFunc     func(context.Context, *GetBlueprintsRequest) (*GetBlueprintsResponse, error)
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
	return &BlueprintResponse{Blueprint: &Blueprint{ID: r.ID}}, nil
}
func (m *mockBlueprintHTTPService) GetBlueprints(ctx context.Context, r *GetBlueprintsRequest) (*GetBlueprintsResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, r)
	}
	return &GetBlueprintsResponse{Blueprints: []Blueprint{{ID: "bp-1"}}, Total: 1}, nil
}

// TestBlueprintHandlerMapping verifies successful transport binding and early
// rejection through actual mappers/validators. Middleware auth is a fixture here.
func TestBlueprintHandlerMapping(t *testing.T) {
	for _, op := range []string{"create", "get", "list"} {
		for _, tc := range []struct{ name string }{{"valid"}, {"invalid input"}, {"anonymous"}} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				calls := 0
				h := NewHandler(&mockBlueprintHTTPService{
					createFunc: func(_ context.Context, r *CreateBlueprintRequest) (*BlueprintResponse, error) {
						calls++
						require.Equal(t, "caller", r.ActorID)
						require.Equal(t, "Starter API", r.Name)
						require.Equal(t, "Service", r.Kind)
						return &BlueprintResponse{Blueprint: &Blueprint{ID: "bp-1"}}, nil
					},
					getByIDFunc: func(_ context.Context, r *GetBlueprintByIDRequest) (*BlueprintResponse, error) {
						calls++
						require.Equal(t, "bp-1", r.ID)
						require.Equal(t, "caller", r.ActorID)
						return &BlueprintResponse{Blueprint: &Blueprint{ID: r.ID}}, nil
					},
					getFunc: func(_ context.Context, r *GetBlueprintsRequest) (*GetBlueprintsResponse, error) {
						calls++
						require.Equal(t, "starter", r.Query)
						return &GetBlueprintsResponse{Blueprints: []Blueprint{{ID: "bp-1"}}, Total: 1}, nil
					},
				}, validator.NewValidator())
				body := `{"name":"Starter API","kind":"Service","ActorID":"spoofed","created_by_user_id":"spoofed"}`
				query := "/?query=starter&actor_id=spoofed&-=spoofed"
				if tc.name == "invalid input" {
					body = `{`
					query += "&page=bad"
				}
				r := httptest.NewRequest(http.MethodPost, query, strings.NewReader(body))
				if tc.name != "invalid input" {
					r = mux.SetURLVars(r, map[string]string{BlueprintURIVariableID: "bp-1"})
				}
				if tc.name != "anonymous" {
					r = r.WithContext(blueprintActorContext(r.Context(), "caller"))
				}
				w := httptest.NewRecorder()
				switch op {
				case "create":
					h.CreateBlueprint(w, r)
				case "get":
					h.GetBlueprintByID(w, r)
				case "list":
					h.GetBlueprints(w, r)
				}
				want := http.StatusOK
				if op == "create" {
					want = http.StatusCreated
				}
				if tc.name == "invalid input" {
					want = http.StatusBadRequest
				}
				if tc.name == "anonymous" && op != "list" {
					want = http.StatusUnauthorized
				}
				require.Equal(t, want, w.Code, w.Body.String())
				if want < 400 {
					require.Equal(t, 1, calls)
					require.Contains(t, w.Body.String(), "bp-1")
				} else {
					require.Zero(t, calls)
				}
			})
		}
	}
}
