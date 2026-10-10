package partnerprogram

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestConfiguredHoldLimitPreservesPublishedHistory exercises a lowered
// admission limit without repricing or invalidating a previously published rule.
func TestConfiguredHoldLimitPreservesPublishedHistory(t *testing.T) {
	for _, days := range []int{30, 90, MaxSupportedHoldDays} {
		t.Run((time.Duration(days) * 24 * time.Hour).String(), func(t *testing.T) {
			ctx := context.Background()
			repo := newFakeRepo()
			clock := fixedClock{time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
			ids := &seqIDs{}
			cfg := testConfig()
			cfg.MaxHoldDays = days
			svc, err := NewService(repo, clock, ids, cfg)
			require.NoError(t, err)
			partner, err := svc.Enroll(ctx, EnrollRequest{"customer", cfg.TermsVersion})
			require.NoError(t, err)
			draft := globalDraft(clock.Now())
			draft.HoldDays = days
			published, err := svc.PublishPolicy(ctx, PublishPolicyRequest{Draft: draft, ActorID: "operator", ExpectedRevision: 0})
			require.NoError(t, err)
			before, err := svc.ResolveTerms(ctx, partner, nil, clock.Now())
			require.NoError(t, err)

			cfg.MaxHoldDays = 14
			restarted, err := NewService(repo, clock, ids, cfg)
			require.NoError(t, err)
			after, err := restarted.ResolveTerms(ctx, partner, nil, clock.Now())
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Equal(t, time.Duration(days)*24*time.Hour, after.HoldDuration)
			require.Equal(t, []string{published.ID}, after.PolicyVersionIDs)
			_, err = restarted.PublishPolicy(ctx, PublishPolicyRequest{Draft: draft, ActorID: "operator", ExpectedRevision: 1})
			require.ErrorIs(t, err, ErrInvalid)
			require.Len(t, repo.policies, 1)
			draft.HoldDays = 14
			_, err = restarted.PublishPolicy(ctx, PublishPolicyRequest{Draft: draft, ActorID: "operator", ExpectedRevision: 1})
			require.NoError(t, err)
			require.Equal(t, days, repo.policies[0].HoldDays)
		})
	}
}
