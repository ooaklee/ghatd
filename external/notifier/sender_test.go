package notifier

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsPermanentWebPushError_TruePositives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		permanent bool
	}{
		{
			name:      "410 Gone",
			err:       fmt.Errorf("unexpected status code: 410"),
			permanent: true,
		},
		{
			name:      "404 Not Found",
			err:       fmt.Errorf("unexpected status code: 404"),
			permanent: true,
		},
		{
			name:      "nil error",
			err:       nil,
			permanent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPermanentWebPushError(tt.err)
			if got != tt.permanent {
				t.Fatalf("expected %v, got %v", tt.permanent, got)
			}
		})
	}
}

func TestIsPermanentWebPushError_FalsePositives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "timeout after 410ms is not a 410 status code",
			err:  errors.New("dial tcp: timeout after 410ms"),
		},
		{
			name: "endpoint URL containing /404/ is not an HTTP status",
			err:  fmt.Errorf("connection to https://push.example/v1/404/subscription failed"),
		},
		{
			name: "500 Internal Server Error is not permanent",
			err:  fmt.Errorf("unexpected status code: 500"),
		},
		{
			name: "4100 is not 410 Gone",
			err:  fmt.Errorf("unexpected status code: 4100"),
		},
		{
			name: "4041 is not 404 Not Found",
			err:  fmt.Errorf("unexpected status code: 4041"),
		},
		{
			name: "generic network error with no HTTP status",
			err:  errors.New("i/o timeout"),
		},
		{
			name: "notify error wrapping a 410 substring (must match regex format)",
			err:  errors.New("notify: delivery failed: 410 total"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if isPermanentWebPushError(tt.err) {
				t.Fatalf("expected false for %q", tt.err)
			}
		})
	}
}
