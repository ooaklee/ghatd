package blueprint

import (
	"errors"
	"reflect"
	"testing"
)

func TestRegistryRegister(t *testing.T) {
	tests := []struct {
		name    string
		entries []Registration
		wantErr error
	}{
		{
			name:    "SUCCESS - registers entry",
			entries: []Registration{{Key: "Example", Name: "Example", Kind: "Demo"}},
		},
		{
			name:    "FAILURE - missing key",
			entries: []Registration{{Name: "Example", Kind: "Demo"}},
			wantErr: ErrBlueprintRegistrationKeyMissing,
		},
		{
			name:    "FAILURE - missing name",
			entries: []Registration{{Key: "example", Kind: "Demo"}},
			wantErr: ErrBlueprintNameIsRequired,
		},
		{
			name:    "FAILURE - missing kind",
			entries: []Registration{{Key: "example", Name: "Example"}},
			wantErr: ErrBlueprintKindIsRequired,
		},
		{
			name: "FAILURE - duplicate key",
			entries: []Registration{
				{Key: "example", Name: "Example", Kind: "Demo"},
				{Key: " example ", Name: "Other", Kind: "Demo"},
			},
			wantErr: ErrBlueprintRegistrationConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRegistry(tt.entries...)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewRegistry() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegistryGetAndList(t *testing.T) {
	registry, err := NewRegistry(
		Registration{Key: "service", Name: "Service", Kind: "Demo"},
		Registration{Key: "api", Name: "API", Kind: "Demo"},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	entry, exists := registry.Get(" SERVICE ")
	if !exists {
		t.Fatal("expected service registration")
	}
	if entry.Key != "service" || entry.Kind != "demo" {
		t.Fatalf("registration = %+v, want normalised key/kind", entry)
	}

	gotKeys := []string{}
	for _, entry := range registry.List() {
		gotKeys = append(gotKeys, entry.Key)
	}
	wantKeys := []string{"api", "service"}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("List() keys = %v, want %v", gotKeys, wantKeys)
	}
}

func TestRegistrationNewBlueprint(t *testing.T) {
	tests := []struct {
		name string
		reg  Registration
		want *Blueprint
	}{
		{
			name: "SUCCESS - default builder uses registration fields",
			reg:  Registration{Key: "demo", Name: "Demo Blueprint", Kind: "Example", Description: "A demo"},
			want: &Blueprint{Name: "Demo Blueprint", Kind: "example", Description: "A demo", Status: BlueprintStatusDraft},
		},
		{
			name: "SUCCESS - custom builder wins",
			reg: Registration{
				Key:  "custom",
				Name: "Custom",
				Kind: "Custom",
				Build: func() *Blueprint {
					return &Blueprint{Name: "Custom", Kind: "custom", Status: BlueprintStatusActive}
				},
			},
			want: &Blueprint{Name: "Custom", Kind: "custom", Status: BlueprintStatusActive},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.reg.NewBlueprint()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("NewBlueprint() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
