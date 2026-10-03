package blueprint

import (
	"context"
	"errors"
	"testing"
)

type mockBlueprintRepository struct {
	createFunc func(ctx context.Context, blueprint *Blueprint) (*Blueprint, error)
	getFunc    func(ctx context.Context, id string) (*Blueprint, error)
	nameFunc   func(ctx context.Context, name, kind string) (*Blueprint, error)
	listFunc   func(ctx context.Context, req *GetBlueprintsRequest) ([]Blueprint, error)
	countFunc  func(ctx context.Context, req *GetBlueprintsRequest) (int64, error)
	updateFunc func(ctx context.Context, blueprint *Blueprint) (*Blueprint, error)
	deleteFunc func(ctx context.Context, id string) error

	created *Blueprint
}

func (m *mockBlueprintRepository) CreateBlueprint(ctx context.Context, blueprint *Blueprint) (*Blueprint, error) {
	m.created = blueprint
	if m.createFunc != nil {
		return m.createFunc(ctx, blueprint)
	}
	return blueprint, nil
}

func (m *mockBlueprintRepository) DeleteBlueprintByID(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockBlueprintRepository) GetBlueprintByID(ctx context.Context, id string) (*Blueprint, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return &Blueprint{ID: id, Name: "Example", Kind: "demo"}, nil
}

func (m *mockBlueprintRepository) GetBlueprintByNameAndKind(ctx context.Context, name, kind string) (*Blueprint, error) {
	if m.nameFunc != nil {
		return m.nameFunc(ctx, name, kind)
	}
	return &Blueprint{ID: "bp-1", Name: name, Kind: kind}, nil
}

func (m *mockBlueprintRepository) GetBlueprints(ctx context.Context, req *GetBlueprintsRequest) ([]Blueprint, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, req)
	}
	return []Blueprint{{ID: "bp-1", Name: "Example", Kind: "demo"}}, nil
}

func (m *mockBlueprintRepository) GetTotalBlueprints(ctx context.Context, req *GetBlueprintsRequest) (int64, error) {
	if m.countFunc != nil {
		return m.countFunc(ctx, req)
	}
	return 1, nil
}

func (m *mockBlueprintRepository) UpdateBlueprint(ctx context.Context, blueprint *Blueprint) (*Blueprint, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, blueprint)
	}
	return blueprint, nil
}

func TestServiceCreateBlueprint(t *testing.T) {
	repoErr := errors.New("repo error")
	tests := []struct {
		name       string
		req        *CreateBlueprintRequest
		createFunc func(ctx context.Context, blueprint *Blueprint) (*Blueprint, error)
		wantErr    error
		wantKind   string
	}{
		{
			name:     "SUCCESS - creates normalised blueprint",
			req:      &CreateBlueprintRequest{Name: " Example ", Kind: " Demo ", ActorID: "user-1"},
			wantKind: "demo",
		},
		{
			name:    "FAILURE - nil request",
			req:     nil,
			wantErr: ErrBlueprintInvalidPayload,
		},
		{
			name:    "FAILURE - missing name",
			req:     &CreateBlueprintRequest{Kind: "demo"},
			wantErr: ErrBlueprintNameIsRequired,
		},
		{
			name:    "FAILURE - missing kind",
			req:     &CreateBlueprintRequest{Name: "Example"},
			wantErr: ErrBlueprintKindIsRequired,
		},
		{
			name: "FAILURE - repository error",
			req:  &CreateBlueprintRequest{Name: "Example", Kind: "demo", ActorID: "user-1"},
			createFunc: func(ctx context.Context, blueprint *Blueprint) (*Blueprint, error) {
				return nil, repoErr
			},
			wantErr: repoErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockBlueprintRepository{createFunc: tt.createFunc}
			svc := NewService(repo)

			resp, err := svc.CreateBlueprint(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreateBlueprint() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if resp == nil || resp.Blueprint == nil {
				t.Fatal("CreateBlueprint() returned nil response")
			}
			if resp.Blueprint.ID == "" || resp.Blueprint.NanoID == "" || resp.Blueprint.CreatedAt == "" {
				t.Fatalf("CreateBlueprint() did not set generated fields: %+v", resp.Blueprint)
			}
			if repo.created.Kind != tt.wantKind {
				t.Fatalf("created kind = %s, want %s", repo.created.Kind, tt.wantKind)
			}
		})
	}
}

func TestServiceGetBlueprints(t *testing.T) {
	failure := errors.New("private list failure")
	for _, tc := range []struct {
		name       string
		want       error
		countCalls int
	}{
		{"filtered", nil, 1}, {"nil filters", nil, 1}, {"mutating adapter", nil, 1},
		{"list failure", failure, 0}, {"count failure", failure, 1}, {"negative count", ErrBlueprintUnavailable, 1},
		{"cancel after list", context.Canceled, 0}, {"cancel after count", context.Canceled, 1},
		{"nil context", ErrBlueprintInvalidPayload, 0}, {"nil repository", ErrBlueprintUnavailable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			counts := 0
			req := &GetBlueprintsRequest{Kind: "demo"}
			if tc.name == "nil filters" {
				req = nil
			}
			repo := &mockBlueprintRepository{listFunc: func(_ context.Context, r *GetBlueprintsRequest) ([]Blueprint, error) {
				if tc.name == "list failure" {
					return nil, failure
				}
				if tc.name == "cancel after list" {
					cancel()
				}
				if tc.name == "mutating adapter" {
					r.Kind = "wrong"
				}
				return []Blueprint{{ID: "bp-1"}}, nil
			}, countFunc: func(_ context.Context, r *GetBlueprintsRequest) (int64, error) {
				counts++
				if tc.name != "nil filters" && r.Kind != "demo" {
					t.Fatalf("count filters mutated: %+v", r)
				}
				if tc.name == "count failure" {
					return 0, failure
				}
				if tc.name == "negative count" {
					return -1, nil
				}
				if tc.name == "cancel after count" {
					cancel()
				}
				return 1, nil
			}}
			svc := NewService(repo)
			if tc.name == "nil context" {
				ctx = nil
			}
			if tc.name == "nil repository" {
				svc.BlueprintRepository = nil
			}
			resp, err := svc.GetBlueprints(ctx, req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if counts != tc.countCalls {
				t.Fatalf("count calls = %d, want %d", counts, tc.countCalls)
			}
			if req != nil && req.Kind != "demo" {
				t.Fatal("caller request mutated")
			}
			if tc.want == nil && (resp.Total != 1 || len(resp.Blueprints) != 1) {
				t.Fatalf("response = %+v, want one record", resp)
			}
		})
	}
}

func TestServiceGetBlueprintByID(t *testing.T) {
	repoErr := errors.New("repo error")
	tests := []struct {
		name    string
		req     *GetBlueprintByIDRequest
		getFunc func(ctx context.Context, id string) (*Blueprint, error)
		wantErr error
	}{
		{
			name: "SUCCESS - gets blueprint by ID",
			req:  &GetBlueprintByIDRequest{ID: "bp-1", ActorID: "user-1"},
		},
		{
			name:    "FAILURE - missing ID",
			req:     &GetBlueprintByIDRequest{ActorID: "user-1"},
			wantErr: ErrBlueprintIDIsRequired,
		},
		{
			name:    "FAILURE - missing user ID",
			req:     &GetBlueprintByIDRequest{ID: "bp-1"},
			wantErr: ErrBlueprintUserIDIsRequired,
		},
		{
			name: "FAILURE - repository error",
			req:  &GetBlueprintByIDRequest{ID: "bp-1", ActorID: "user-1"},
			getFunc: func(ctx context.Context, id string) (*Blueprint, error) {
				return nil, repoErr
			},
			wantErr: repoErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(&mockBlueprintRepository{getFunc: tt.getFunc})

			resp, err := svc.GetBlueprintByID(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetBlueprintByID() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if resp == nil || resp.Blueprint == nil || resp.Blueprint.ID != tt.req.ID {
				t.Fatalf("GetBlueprintByID() response = %+v, want blueprint ID %s", resp, tt.req.ID)
			}
		})
	}
}

func TestServiceRegistry(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"default", nil}, {"custom", nil}, {"nil service", ErrBlueprintUnavailable}, {"nil registry", ErrBlueprintUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(nil)
			if tc.name == "custom" {
				registry := MustRegistry()
				svc = NewService(nil, registry)
				if svc.Registry != registry {
					t.Fatal("custom registry not retained")
				}
			}
			if tc.name == "nil service" {
				svc = nil
			}
			if tc.name == "nil registry" {
				svc.Registry = nil
			}
			err := svc.RegisterBlueprint(Registration{Key: "demo", Name: "Demo", Kind: "Example"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("register error = %v, want %v", err, tc.want)
			}
			entry, err := svc.GetBlueprintRegistration(" demo ")
			if !errors.Is(err, tc.want) {
				t.Fatalf("get error = %v, want %v", err, tc.want)
			}
			if tc.want == nil && (entry.Key != "demo" || entry.Kind != "example") {
				t.Fatalf("registration = %+v, want normalised fields", entry)
			}
		})
	}
}
