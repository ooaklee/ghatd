package emailprovider

import (
	"strings"
	"testing"
)

func TestNewSparkPostClient(t *testing.T) {
	tests := []struct {
		name       string
		req        *NewSparkPostClientRequest
		wantErr    string
		wantAPIKey string
	}{
		{
			name: "SUCCESS - defaults API version",
			req: &NewSparkPostClientRequest{
				BaseURL: "https://api.sparkpost.com",
				APIKey:  "test-key",
			},
			wantAPIKey: "test-key",
		},
		{
			name:    "FAILURE - nil request",
			req:     nil,
			wantErr: "emailprovider/sparkpost-client-nil-request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSparkPostClient(tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewSparkPostClient() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewSparkPostClient() error = %v", err)
			}
			if got == nil {
				t.Fatal("NewSparkPostClient() returned nil client")
			}
			if got.Config.ApiKey != tt.wantAPIKey {
				t.Fatalf("ApiKey = %q, want %q", got.Config.ApiKey, tt.wantAPIKey)
			}
		})
	}
}
