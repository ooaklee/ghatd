package notifier

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"firebase.google.com/go/v4/messaging"
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
			name:      "wrapped 410 Gone",
			err:       fmt.Errorf("notify send failed: %w", fmt.Errorf("unexpected status code: 410")),
			permanent: true,
		},
		{
			name:      "joined 404 Not Found",
			err:       errors.Join(errors.New("temporary failure"), fmt.Errorf("unexpected status code: 404")),
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

func TestFCMBatchResponseError_NoFailures(t *testing.T) {
	t.Parallel()

	err := fcmBatchResponseError(&messaging.BatchResponse{
		SuccessCount: 1,
		FailureCount: 0,
		Responses: []*messaging.SendResponse{
			{Success: true, MessageID: "msg-1"},
		},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestFCMBatchResponseError_FailuresIncludeTokenIndex(t *testing.T) {
	t.Parallel()

	err := fcmBatchResponseError(&messaging.BatchResponse{
		SuccessCount: 1,
		FailureCount: 1,
		Responses: []*messaging.SendResponse{
			{Success: true, MessageID: "msg-1"},
			{Success: false, Error: errors.New("registration token is not registered")},
		},
	})
	if err == nil {
		t.Fatal("expected error for failed FCM response")
	}

	message := err.Error()
	if !strings.Contains(message, "fcm delivery failed for 1 token(s)") {
		t.Fatalf("expected failure count in error, got %q", message)
	}
	if !strings.Contains(message, "token[1]: registration token is not registered") {
		t.Fatalf("expected token index in error, got %q", message)
	}
}

func TestFCMBatchResponseError_FailureCountWithoutResponseErrors(t *testing.T) {
	t.Parallel()

	err := fcmBatchResponseError(&messaging.BatchResponse{
		SuccessCount: 0,
		FailureCount: 1,
		Responses: []*messaging.SendResponse{
			nil,
			{Success: false},
		},
	})
	if err == nil {
		t.Fatal("expected error for failed FCM response")
	}
	if err.Error() != "fcm delivery failed for 1 token(s)" {
		t.Fatalf("unexpected error: %q", err.Error())
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
