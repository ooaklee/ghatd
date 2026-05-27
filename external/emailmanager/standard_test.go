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

func TestEmailManagerOutputsToLocalProviderWhenSendingDisabled(t *testing.T) {
	provider := emailprovider.NewLoggingEmailProvider(&emailprovider.LoggingEmailProviderConfig{
		TimeProvider: func() time.Time {
			return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		},
	})

	manager, err := NewStandardEmailManager(&NewStandardEmailManagerRequest{
		Provider:                      provider,
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
		Config: &Config{
			ShouldSendEmail:    false,
			EnableAuditLogging: false,
		},
	})
	if err != nil {
		t.Fatalf("NewStandardEmailManager() error = %v", err)
	}

	err = manager.SendLoginEmail(context.Background(), &SendLoginEmailRequest{
		Email: "user@example.com",
		Token: "login-token",
		Code:  "ABC12345",
	})
	if err != nil {
		t.Fatalf("SendLoginEmail() error = %v", err)
	}

	emails := provider.Inbox().List()
	if len(emails) != 1 {
		t.Fatalf("captured emails = %d, want 1", len(emails))
	}
	if !strings.Contains(emails[0].HTMLBody, "login-token") || !strings.Contains(emails[0].HTMLBody, "ABC12345") {
		t.Fatalf("captured email body does not include login token and code")
	}
}

func TestEmailManagerSendEmailOutputsToLocalProviderWhenSendingDisabled(t *testing.T) {
	provider := emailprovider.NewLoggingEmailProvider(&emailprovider.LoggingEmailProviderConfig{
		TimeProvider: func() time.Time {
			return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		},
	})
	manager := NewEmailManager(nil, provider, nil, &Config{
		ShouldSendEmail:    false,
		EnableAuditLogging: false,
	})

	err := manager.SendEmail(context.Background(), &SendEmailRequest{
		To:       "user@example.com",
		From:     "hello@example.com",
		Subject:  "Security code",
		HTMLBody: `<p>Use code ABC12345</p><a href="https://app.example.com/login?t=login-token">Sign in</a>`,
	})
	if err != nil {
		t.Fatalf("SendEmail() error = %v", err)
	}

	emails := provider.Inbox().List()
	if len(emails) != 1 {
		t.Fatalf("captured emails = %d, want 1", len(emails))
	}
	if !strings.Contains(emails[0].HTMLBody, "login-token") || !strings.Contains(emails[0].HTMLBody, "ABC12345") {
		t.Fatalf("captured email body does not include login token and code")
	}
}
