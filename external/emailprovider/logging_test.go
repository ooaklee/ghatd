package emailprovider

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLoggingEmailProviderCapturesLocalEmail(t *testing.T) {
	provider := NewLoggingEmailProvider(&LoggingEmailProviderConfig{
		TimeProvider: func() time.Time {
			return time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
		},
	})

	result, err := provider.Send(context.Background(), &Email{
		To:       "dev@example.com",
		From:     "noreply@example.com",
		ReplyTo:  "support@example.com",
		Subject:  "Login",
		HTMLBody: `<a href="https://app.example.com/login?t=token">Sign in</a>`,
		TextBody: "Use code ABC12345",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.MessageID == "" || !strings.HasPrefix(result.MessageID, "local-") {
		t.Fatalf("MessageID = %q, want local ID", result.MessageID)
	}

	emails := provider.Inbox().List()
	if len(emails) != 1 {
		t.Fatalf("captured emails = %d, want 1", len(emails))
	}
	if emails[0].MessageID != result.MessageID {
		t.Fatalf("captured MessageID = %q, want %q", emails[0].MessageID, result.MessageID)
	}
	if emails[0].HTMLBody == "" || !strings.Contains(emails[0].HTMLBody, "login?t=token") {
		t.Fatalf("captured HTMLBody = %q, want login link", emails[0].HTMLBody)
	}
	if emails[0].TextBody != "Use code ABC12345" {
		t.Fatalf("captured TextBody = %q", emails[0].TextBody)
	}
}

func TestLocalEmailStoreKeepsNewestWithinLimit(t *testing.T) {
	store := NewLocalEmailStore(2)
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	store.Add(LocalEmail{MessageID: "one", CreatedAt: base})
	store.Add(LocalEmail{MessageID: "two", CreatedAt: base})
	store.Add(LocalEmail{MessageID: "three", CreatedAt: base})

	emails := store.List()
	if len(emails) != 2 {
		t.Fatalf("List() length = %d, want 2", len(emails))
	}
	if emails[0].MessageID != "three" || emails[1].MessageID != "two" {
		t.Fatalf("List() order = %v, want newest retained first", []string{emails[0].MessageID, emails[1].MessageID})
	}
	if _, ok := store.Get("one"); ok {
		t.Fatal("oldest email should be trimmed")
	}
}
