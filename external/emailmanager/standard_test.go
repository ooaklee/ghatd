package emailmanager

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/emailprovider"
)

func TestNewStandardEmailManager(t *testing.T) {
	tests := []struct {
		name    string
		req     *NewStandardEmailManagerRequest
		wantErr string
	}{
		{
			name: "SUCCESS - creates standard email manager",
			req: &NewStandardEmailManagerRequest{
				Provider:                      emailprovider.NewLoggingEmailProvider(nil),
				FrontendBaseURL:               "https://app.example.com",
				EmailVerificationFullEndpoint: "https://api.example.com/v0/auth/verify",
				DashboardVerificationURIPath:  "https://api.example.com/v0/auth/verify",
				Environment:                   "local",
				BusinessEntityName:            "Example",
				BusinessEntityWebsite:         "https://example.com",
				WelcomeEmailSubject:           "Welcome",
				LoginEmailSubject:             "Login",
				FromEmailAddress:              "hello@example.com",
				NoReplyEmailAddress:           "noreply@example.com",
				TimeProvider: func() time.Time {
					return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
				},
			},
		},
		{
			name:    "FAILURE - nil request",
			req:     nil,
			wantErr: "emailmanager/standard-nil-request",
		},
		{
			name:    "FAILURE - missing provider",
			req:     &NewStandardEmailManagerRequest{},
			wantErr: "emailmanager/standard-missing-provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewStandardEmailManager(tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewStandardEmailManager() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewStandardEmailManager() error = %v", err)
			}
			if got == nil {
				t.Fatal("NewStandardEmailManager() returned nil manager")
			}
			if err := got.SendCustomEmail(context.Background(), &SendCustomEmailRequest{
				EmailTo:      "user@example.com",
				EmailSubject: "Hello",
				EmailBody:    "Welcome",
				EmailPreview: "Hello",
			}); err != nil {
				t.Fatalf("SendCustomEmail() error = %v", err)
			}
		})
	}
}
