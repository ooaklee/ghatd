package referral

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type prefixedIDs struct{ next int }

func (ids *prefixedIDs) NewID() string {
	ids.next++
	return fmt.Sprintf("p%032d", ids.next)
}

// This lifecycle exercises multiple partners, and replay
// against one store, because uniqueness is a relation between successive links.
func TestLinkCodesConsumeWholeIdentifier(t *testing.T) {
	repo := newFakeRepo()
	svc, err := NewService(repo, fakeClock{t: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)}, &prefixedIDs{}, 7*24*time.Hour)
	require.NoError(t, err)
	seen := make(map[string]bool)
	for index := 0; index < 20; index++ {
		partner := PartnerState{PartnerID: fmt.Sprintf("partner-%d", index), CustomerID: fmt.Sprintf("customer-%d", index), CanAcquireReferrals: true}
		link, err := svc.IssueLink(context.Background(), partner)
		require.NoError(t, err)
		require.NotEmpty(t, link.Code)
		require.False(t, seen[link.Code], "fixed generator prefixes must not collapse entropy")
		seen[link.Code] = true
		replayed, err := svc.IssueLink(context.Background(), partner)
		require.NoError(t, err)
		require.Equal(t, link.Code, replayed.Code)
	}
}
