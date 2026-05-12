package config

import (
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/internal/cli/common"
)

func TestIsSupportedDetailType(t *testing.T) {
	tests := []struct {
		name       string
		detailType string
		want       bool
	}{
		{name: "Success - api", detailType: DetailTypeAPI, want: true},
		{name: "Success - web", detailType: DetailTypeWeb, want: true},
		{name: "Success - web vite", detailType: DetailTypeWebVite, want: true},
		{name: "Failure - unsupported", detailType: "worker", want: false},
		{name: "Failure - empty", detailType: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSupportedDetailType(tt.detailType)
			if got != tt.want {
				t.Fatalf("IsSupportedDetailType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateDetailConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *DetailConfig
		wantErr error
	}{
		{name: "Success - nil config", config: nil},
		{name: "Success - empty type remains allowed for partial config", config: &DetailConfig{}},
		{name: "Success - web vite", config: &DetailConfig{Type: DetailTypeWebVite}},
		{name: "Failure - unsupported type", config: &DetailConfig{Type: "worker"}, wantErr: common.ErrDetailTypeInvalidError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDetailConfig(tt.config)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateDetailConfig() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
