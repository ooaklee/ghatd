package teleprovider

import (
	"context"
	"os"
	"testing"
)

// TestLiveRegistrationReadOnly performs an explicit opt-in lookup without sending or group mutations.
// Supply an authorised test number; failures deliberately omit identity and credentials.
// A standalone test is intentional: one explicit configured live lookup is the
// whole opt-in contract, so a table would duplicate real I/O without extra coverage.
func TestLiveRegistrationReadOnly(t *testing.T) {
	if os.Getenv("TELEPROVIDER_LIVE_READ_ONLY") != "true" {
		t.Skip("requires explicitly configured live read-only smoke")
	}
	s, err := NewTeleProvider(Config{Provider: "openwa", OpenWA: OpenWAConfig{Endpoint: os.Getenv("OPENWA_ENDPOINT"), APIKey: os.Getenv("OPENWA_API_KEY"), SessionID: os.Getenv("OPENWA_SESSION_ID")}})
	if err != nil {
		t.Fatal("live configuration invalid")
	}
	result, err := s.CheckNumber(context.Background(), os.Getenv("OPENWA_SMOKE_NUMBER"))
	if err != nil {
		t.Fatalf("live check failed: %v", err)
	}
	if result.Status != "registered" {
		t.Fatal("designated connected test account did not return registered")
	}
}
