package blueprint

import (
	"context"
	"errors"
	"testing"
)

type mockBlueprintRepository struct {
	createFunc func(ctx context.Context, blueprint *Blueprint) (*Blueprint, error)
	getFunc    func(ctx context.Context, id string) (*Blueprint, error)
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
			req:      &CreateBlueprintRequest{Name: " Example ", Kind: " Demo ", CreatedByUserID: "user-1"},
			wantKind: "demo",
		},
		{
			name:    "FAILURE - nil request",
			req:     nil,
			wantErr: ErrBlueprintNameIsRequired,
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
			req:  &CreateBlueprintRequest{Name: "Example", Kind: "demo"},
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
	svc := NewService(&mockBlueprintRepository{})

	resp, err := svc.GetBlueprints(context.Background(), &GetBlueprintsRequest{Kind: "demo"})
	if err != nil {
		t.Fatalf("GetBlueprints() error = %v", err)
	}
	if resp.Total != 1 || len(resp.Blueprints) != 1 {
		t.Fatalf("GetBlueprints() = %+v, want total and one blueprint", resp)
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
			req:  &GetBlueprintByIDRequest{ID: "bp-1", UserID: "user-1"},
		},
		{
			name:    "FAILURE - missing ID",
			req:     &GetBlueprintByIDRequest{UserID: "user-1"},
			wantErr: ErrBlueprintIDIsRequired,
		},
		{
			name:    "FAILURE - missing user ID",
			req:     &GetBlueprintByIDRequest{ID: "bp-1"},
			wantErr: ErrBlueprintUserIDIsRequired,
		},
		{
			name: "FAILURE - repository error",
			req:  &GetBlueprintByIDRequest{ID: "bp-1", UserID: "user-1"},
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
	svc := NewService(&mockBlueprintRepository{})

	if err := svc.RegisterBlueprint(Registration{Key: "demo", Name: "Demo", Kind: "Example"}); err != nil {
		t.Fatalf("RegisterBlueprint() error = %v", err)
	}

	entry, err := svc.GetBlueprintRegistration(" demo ")
	if err != nil {
		t.Fatalf("GetBlueprintRegistration() error = %v", err)
	}
	if entry.Key != "demo" || entry.Kind != "example" {
		t.Fatalf("registration = %+v, want normalised fields", entry)
	}
}
