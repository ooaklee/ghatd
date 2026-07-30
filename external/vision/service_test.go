package vision

import (
	"context"
	"errors"
	"testing"
)

type mockVisionRepository struct {
	createFunc func(ctx context.Context, vision *Vision) (*Vision, error)
	getFunc    func(ctx context.Context, id string) (*Vision, error)
	listFunc   func(ctx context.Context, req *GetVisionsRequest) ([]Vision, error)
	countFunc  func(ctx context.Context, req *GetVisionsRequest) (int64, error)
	updateFunc func(ctx context.Context, vision *Vision) (*Vision, error)
	deleteFunc func(ctx context.Context, id string) error

	created *Vision
}

func (m *mockVisionRepository) CreateVision(ctx context.Context, vision *Vision) (*Vision, error) {
	m.created = vision
	if m.createFunc != nil {
		return m.createFunc(ctx, vision)
	}
	return vision, nil
}

func (m *mockVisionRepository) DeleteVisionByID(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockVisionRepository) GetVisionByID(ctx context.Context, id string) (*Vision, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return &Vision{ID: id, Name: "Example", Kind: "demo"}, nil
}

func (m *mockVisionRepository) GetVisionByNameAndKind(ctx context.Context, name, kind string) (*Vision, error) {
	return &Vision{ID: "bp-1", Name: name, Kind: kind}, nil
}

func (m *mockVisionRepository) GetVisions(ctx context.Context, req *GetVisionsRequest) ([]Vision, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, req)
	}
	return []Vision{{ID: "bp-1", Name: "Example", Kind: "demo"}}, nil
}

func (m *mockVisionRepository) GetTotalVisions(ctx context.Context, req *GetVisionsRequest) (int64, error) {
	if m.countFunc != nil {
		return m.countFunc(ctx, req)
	}
	return 1, nil
}

func (m *mockVisionRepository) UpdateVision(ctx context.Context, vision *Vision) (*Vision, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, vision)
	}
	return vision, nil
}

func TestServiceCreateVision(t *testing.T) {
	repoErr := errors.New("repo error")
	tests := []struct {
		name       string
		req        *CreateVisionRequest
		createFunc func(ctx context.Context, vision *Vision) (*Vision, error)
		wantErr    error
		wantKind   string
	}{
		{
			name:     "SUCCESS - creates normalised vision",
			req:      &CreateVisionRequest{Name: " Example ", Kind: " Demo ", CreatedByUserID: "user-1"},
			wantKind: "demo",
		},
		{
			name:    "FAILURE - nil request",
			req:     nil,
			wantErr: ErrVisionNameIsRequired,
		},
		{
			name:    "FAILURE - missing name",
			req:     &CreateVisionRequest{Kind: "demo"},
			wantErr: ErrVisionNameIsRequired,
		},
		{
			name:    "FAILURE - missing kind",
			req:     &CreateVisionRequest{Name: "Example"},
			wantErr: ErrVisionKindIsRequired,
		},
		{
			name: "FAILURE - repository error",
			req:  &CreateVisionRequest{Name: "Example", Kind: "demo"},
			createFunc: func(ctx context.Context, vision *Vision) (*Vision, error) {
				return nil, repoErr
			},
			wantErr: repoErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockVisionRepository{createFunc: tt.createFunc}
			svc := NewService(repo)

			resp, err := svc.CreateVision(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreateVision() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if resp == nil || resp.Vision == nil {
				t.Fatal("CreateVision() returned nil response")
			}
			if resp.Vision.ID == "" || resp.Vision.NanoID == "" || resp.Vision.CreatedAt == "" {
				t.Fatalf("CreateVision() did not set generated fields: %+v", resp.Vision)
			}
			if repo.created.Kind != tt.wantKind {
				t.Fatalf("created kind = %s, want %s", repo.created.Kind, tt.wantKind)
			}
		})
	}
}

func TestServiceGetVisions(t *testing.T) {
	svc := NewService(&mockVisionRepository{})

	resp, err := svc.GetVisions(context.Background(), &GetVisionsRequest{Kind: "demo"})
	if err != nil {
		t.Fatalf("GetVisions() error = %v", err)
	}
	if resp.Total != 1 || len(resp.Visions) != 1 {
		t.Fatalf("GetVisions() = %+v, want total and one vision", resp)
	}
}

func TestServiceGetVisionByID(t *testing.T) {
	repoErr := errors.New("repo error")
	tests := []struct {
		name    string
		req     *GetVisionByIDRequest
		getFunc func(ctx context.Context, id string) (*Vision, error)
		wantErr error
	}{
		{
			name: "SUCCESS - gets vision by ID",
			req:  &GetVisionByIDRequest{ID: "bp-1", UserID: "user-1"},
		},
		{
			name:    "FAILURE - missing ID",
			req:     &GetVisionByIDRequest{UserID: "user-1"},
			wantErr: ErrVisionIDIsRequired,
		},
		{
			name:    "FAILURE - missing user ID",
			req:     &GetVisionByIDRequest{ID: "bp-1"},
			wantErr: ErrVisionUserIDIsRequired,
		},
		{
			name: "FAILURE - repository error",
			req:  &GetVisionByIDRequest{ID: "bp-1", UserID: "user-1"},
			getFunc: func(ctx context.Context, id string) (*Vision, error) {
				return nil, repoErr
			},
			wantErr: repoErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(&mockVisionRepository{getFunc: tt.getFunc})

			resp, err := svc.GetVisionByID(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetVisionByID() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if resp == nil || resp.Vision == nil || resp.Vision.ID != tt.req.ID {
				t.Fatalf("GetVisionByID() response = %+v, want vision ID %s", resp, tt.req.ID)
			}
		})
	}
}

func TestServiceRegistry(t *testing.T) {
	svc := NewService(&mockVisionRepository{})

	if err := svc.RegisterVision(Registration{Key: "demo", Name: "Demo", Kind: "Example"}); err != nil {
		t.Fatalf("RegisterVision() error = %v", err)
	}

	entry, err := svc.GetVisionRegistration(" demo ")
	if err != nil {
		t.Fatalf("GetVisionRegistration() error = %v", err)
	}
	if entry.Key != "demo" || entry.Kind != "example" {
		t.Fatalf("registration = %+v, want normalised fields", entry)
	}
}
