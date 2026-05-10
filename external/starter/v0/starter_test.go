package starter

import (
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name: "Success - local environment with debug logging",
			config: Config{
				Port:        4000,
				Environment: "local",
				LogLevel:    "debug",
			},
		},
		{
			name: "Success - development environment with typical port",
			config: Config{
				Port:        8080,
				Environment: "development",
				LogLevel:    "info",
			},
		},
		{
			name: "Success - production environment with TLS port",
			config: Config{
				Port:        443,
				Environment: "production",
				LogLevel:    "warn",
			},
		},
		{
			name: "Success - trims and normalises enum-like values",
			config: Config{
				Port:        3000,
				Environment: " STAGING ",
				LogLevel:    " ERROR ",
			},
		},
		{
			name: "Failure - port is zero",
			config: Config{
				Port:        0,
				Environment: "local",
				LogLevel:    "debug",
			},
			wantErr: "starter/config-invalid-port",
		},
		{
			name: "Failure - port exceeds max",
			config: Config{
				Port:        65536,
				Environment: "production",
				LogLevel:    "info",
			},
			wantErr: "starter/config-invalid-port",
		},
		{
			name: "Failure - empty environment",
			config: Config{
				Port:     4000,
				LogLevel: "debug",
			},
			wantErr: "starter/config-invalid-environment",
		},
		{
			name: "Failure - invalid environment",
			config: Config{
				Port:        4000,
				Environment: "prod",
				LogLevel:    "info",
			},
			wantErr: "starter/config-invalid-environment",
		},
		{
			name: "Failure - empty log level",
			config: Config{
				Port:        4000,
				Environment: "local",
			},
			wantErr: "starter/config-invalid-log-level",
		},
		{
			name: "Failure - invalid log level",
			config: Config{
				Port:        4000,
				Environment: "production",
				LogLevel:    "verbose",
			},
			wantErr: "starter/config-invalid-log-level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}
