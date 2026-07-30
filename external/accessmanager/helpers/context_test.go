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
