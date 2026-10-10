package partnerprogram

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named policy selection/recurrence anchoring boundaries.
func TestReferralTermsSelectCurrentPolicyWithoutRestartingWindow(t *testing.T) {
	cases := []struct {
		name                                  string
		currentOnly, futureSignup, zeroSignup bool
		want                                  error
	}{
		{name: "current_policy_uses_original_signup_anchor"},
		{name: "policy_published_after_signup_is_selected_now", currentOnly: true},
		{name: "future_signup_cannot_anchor", futureSignup: true, want: ErrInvalid},
		{name: "missing_signup_cannot_anchor", zeroSignup: true, want: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, clock := newTestService(t)
			ctx := context.Background()
			p := Partner{ID: "partner", ProgramID: ProgramID, CustomerID: "customer"}
			draft := globalDraft(clock.Now())
			months := 3
			draft.RecurringMonths = &months
			if tc.currentOnly {
				draft.EffectiveFrom = clock.Now()
			}
			policy, err := s.PublishPolicy(ctx, PublishPolicyRequest{ActorID: "operator", Draft: draft})
			require.NoError(t, err)
			signup := clock.Now().AddDate(0, -1, 0)
			if tc.futureSignup {
				signup = clock.Now().Add(time.Nanosecond)
			}
			if tc.zeroSignup {
				signup = time.Time{}
			}
			out, err := s.ResolveReferralTerms(ctx, p, nil, clock.Now(), signup)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				return
			}
			require.Equal(t, signup.AddDate(0, months, 0), *out.RecurrenceEndsAt)
			require.Equal(t, []string{policy.ID}, out.PolicyVersionIDs)
			require.Equal(t, clock.Now(), out.ResolvedAt)
		})
	}
}
