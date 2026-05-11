package notifier

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStandardSenders(t *testing.T) {
	tests := []struct {
		name               string
		request            func(t *testing.T) *StandardSendersRequest
		wantErr            string
		wantWebPushEnabled bool
		wantFCM            bool
		wantFCMEnabled     bool
		assert             func(t *testing.T, result *StandardSendersResult, fcm *FCMSender)
	}{
		{
			name: "GOOD - nil request returns disabled Web Push sender and no FCM sender",
			request: func(t *testing.T) *StandardSendersRequest {
				return nil
			},
		},
		{
			name: "GOOD - nil Web Push config returns disabled Web Push sender",
			request: func(t *testing.T) *StandardSendersRequest {
				return &StandardSendersRequest{}
			},
		},
		{
			name: "GOOD - explicit Web Push enable with keys creates enabled Web Push sender",
			request: func(t *testing.T) *StandardSendersRequest {
				return &StandardSendersRequest{
					WebPush: &WebPushSenderConfig{
						Enabled:         true,
						VAPIDPublicKey:  "public-key-value",
						VAPIDPrivateKey: "private-key-value",
					},
				}
			},
			wantWebPushEnabled: true,
		},
		{
			name: "GOOD - VAPID keys implicitly enable Web Push sender",
			request: func(t *testing.T) *StandardSendersRequest {
				return &StandardSendersRequest{
					WebPush: &WebPushSenderConfig{
						VAPIDPublicKey:  "public-key-value",
						VAPIDPrivateKey: "private-key-value",
					},
				}
			},
			wantWebPushEnabled: true,
		},
		{
			name: "GOOD - explicit Web Push enable without keys produces disabled sender",
			request: func(t *testing.T) *StandardSendersRequest {
				return &StandardSendersRequest{
					WebPush: &WebPushSenderConfig{
						Enabled: true,
					},
				}
			},
		},
		{
			name: "GOOD - disabled FCM config creates no FCM sender",
			request: func(t *testing.T) *StandardSendersRequest {
				return &StandardSendersRequest{
					FCM: &FCMSenderConfig{Enabled: false},
				}
			},
		},
		{
			name: "GOOD - FCM enabled with file path preserves caller-owned credentials file",
			request: func(t *testing.T) *StandardSendersRequest {
				tmpDir := t.TempDir()
				credentialsFile := filepath.Join(tmpDir, "fcm-credentials.json")
				if err := os.WriteFile(credentialsFile, []byte(`{"project_id":"test"}`), 0600); err != nil {
					t.Fatalf("failed to write credentials file: %v", err)
				}
				return &StandardSendersRequest{
					FCM: &FCMSenderConfig{
						Enabled:         true,
						CredentialsFile: credentialsFile,
					},
				}
			},
			wantFCM:        true,
			wantFCMEnabled: true,
			assert: func(t *testing.T, result *StandardSendersResult, fcm *FCMSender) {
				t.Helper()
				credentialsFile := fcm.config.CredentialsFile
				result.Cleanup()
				if _, err := os.Stat(credentialsFile); err != nil {
					t.Fatalf("expected caller-owned credentials file to remain, got %v", err)
				}
			},
		},
		{
			name: "GOOD - FCM enabled with base64 writes temp credentials and cleanup removes them",
			request: func(t *testing.T) *StandardSendersRequest {
				credentialsJSON := `{"project_id":"test-b64"}`
				return &StandardSendersRequest{
					FCM: &FCMSenderConfig{
						Enabled: true,
					},
					FCMCredentialsBase64: base64.StdEncoding.EncodeToString([]byte(credentialsJSON)),
				}
			},
			wantFCM:        true,
			wantFCMEnabled: true,
			assert: func(t *testing.T, result *StandardSendersResult, fcm *FCMSender) {
				t.Helper()
				credentialsFile := fcm.config.CredentialsFile
				if credentialsFile == "" {
					t.Fatal("expected temp credentials file path")
				}
				if _, err := os.Stat(credentialsFile); err != nil {
					t.Fatalf("expected temp credentials file to exist before cleanup: %v", err)
				}
				result.Cleanup()
				if _, err := os.Stat(credentialsFile); !os.IsNotExist(err) {
					t.Fatalf("expected temp credentials file to be removed, got %v", err)
				}
			},
		},
		{
			name: "BAD - invalid base64 credentials returns decode error",
			request: func(t *testing.T) *StandardSendersRequest {
				return &StandardSendersRequest{
					FCM: &FCMSenderConfig{
						Enabled: true,
					},
					FCMCredentialsBase64: "!!!not-valid-base64!!!",
				}
			},
			wantErr: "decode base64 credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewStandardSenders(tt.request(t))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("expected result")
			}
			if result.Cleanup == nil {
				t.Fatal("expected cleanup function")
			}

			webPush := findSender[*WebPushSender](result.Senders)
			if webPush == nil {
				t.Fatal("expected WebPushSender")
			}
			if got := webPush.Enabled(); got != tt.wantWebPushEnabled {
				t.Fatalf("expected WebPushSender.Enabled()=%v, got %v", tt.wantWebPushEnabled, got)
			}

			fcm := findSender[*FCMSender](result.Senders)
			if tt.wantFCM {
				if fcm == nil {
					t.Fatal("expected FCMSender")
				}
				if got := fcm.Enabled(); got != tt.wantFCMEnabled {
					t.Fatalf("expected FCMSender.Enabled()=%v, got %v", tt.wantFCMEnabled, got)
				}
			} else if fcm != nil {
				t.Fatal("expected no FCMSender")
			}

			if tt.assert != nil {
				tt.assert(t, result, fcm)
				return
			}
			result.Cleanup()
		})
	}
}

func findSender[T any](senders []ChannelSender) T {
	var zero T
	for _, sender := range senders {
		if typed, ok := sender.(T); ok {
			return typed
		}
	}
	return zero
}
