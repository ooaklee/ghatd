package accessmanagerhelpers

import (
	"context"
	"testing"
)

func TestAcquireAuthenticatedFromFailsClosed(t *testing.T) {
	if AcquireAuthenticatedFrom(context.Background()) {
		t.Fatal("missing authentication state should be treated as unauthenticated")
	}
}

func TestTransitAuthenticatedWith(t *testing.T) {
	ctx := TransitAuthenticatedWith(context.Background(), true)
	if !AcquireAuthenticatedFrom(ctx) {
		t.Fatal("expected authenticated state to survive context transit")
	}
}

func TestAcquireAuthenticatedUserIDFromRejectsAnonymousPlaceholder(t *testing.T) {
	ctx := TransitWith(context.Background(), "anonymous-placeholder")
	ctx = TransitAuthenticatedWith(ctx, false)

	if got := AcquireAuthenticatedUserIDFrom(ctx); got != "" {
		t.Fatalf("AcquireAuthenticatedUserIDFrom() = %q, want empty ID", got)
	}
}

func TestAcquireAuthenticatedUserIDFromReturnsAuthenticatedID(t *testing.T) {
	ctx := TransitWith(context.Background(), "user-123")
	ctx = TransitAuthenticatedWith(ctx, true)

	if got := AcquireAuthenticatedUserIDFrom(ctx); got != "user-123" {
		t.Fatalf("AcquireAuthenticatedUserIDFrom() = %q, want user-123", got)
	}
}

func TestAcquireAuthenticatedUserIDFromFailsClosedWithoutAuthenticationState(t *testing.T) {
	ctx := TransitWith(context.Background(), "user-123")

	if got := AcquireAuthenticatedUserIDFrom(ctx); got != "" {
		t.Fatalf("AcquireAuthenticatedUserIDFrom() = %q, want empty ID", got)
	}
}
