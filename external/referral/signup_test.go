package referral

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSignupReconciliationAfterLaterChanges(t *testing.T) {
	cases := []struct {
		name                                        string
		retired, paused, corrected, changedEvidence bool
		want                                        error
	}{
		{name: "original accepted decision"}, {name: "retired link", retired: true}, {name: "acquisition paused", paused: true}, {name: "ownership corrected", corrected: true}, {name: "all later changes", retired: true, paused: true, corrected: true}, {name: "changed evidence does not replay", corrected: true, changedEvidence: true, want: ErrAlreadyReferred},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, p, l, e := fixture(t)
			ctx := context.Background()
			original, err := s.LockAttribution(ctx, p, l, e)
			require.NoError(t, err)
			s.clock = fakeClock{e.At.Add(time.Hour)}
			if tc.corrected {
				_, err = s.AssignAttribution(ctx, reviewedCorrection(t, s, CorrectionRequest{Partner: PartnerState{"new-partner", "new-owner", true}, ReferredCustomer: e.ReferredCustomer, ActorID: "operator", Reason: "audited prospective change", ExpectedRevision: original.Revision, ExpectedReferralID: original.ID, Terms: fixtureTerms()}))
				require.NoError(t, err)
			}
			if tc.retired {
				require.NoError(t, s.RetireLink(ctx, l.ID, "retire old link", "operator"))
				l, err = repo.GetLinkByCode(ctx, l.Code)
				require.NoError(t, err)
			}
			if tc.paused {
				p.CanAcquireReferrals = false
			}
			if tc.changedEvidence {
				e.Evidence.LinkID = "another-link"
			}
			// Current policy no longer participates in replay of already committed
			// immutable terms. It is required again only for a genuinely new decision.
			e.Terms = TermsSnapshot{}
			replayed, err := s.LockAttribution(ctx, p, l, e)
			require.ErrorIs(t, err, tc.want)
			if err == nil {
				require.Equal(t, original, replayed)
			}
			history, err := s.History(ctx, e.ReferredCustomer)
			require.NoError(t, err)
			want := 1
			if tc.corrected {
				want = 2
			}
			require.Len(t, history, want)
		})
	}
}
