package referral

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPaymentBindingPreservesHistoricalOwner(t *testing.T) {
	cases := []struct {
		name                  string
		paidAfterCorrection   bool
		replayAfterCorrection bool
		wantPartner           string
	}{
		{"late old payment uses original owner", false, false, "partner"},
		{"new payment after correction uses corrected owner", true, false, "other-partner"},
		{"lost response replay retains original owner", false, true, "partner"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, p, l, e := fixture(t)
			ctx := context.Background()
			original, err := svc.LockAttribution(ctx, p, l, e)
			require.NoError(t, err)
			paidAt := e.At.Add(time.Hour)
			correctionAt := paidAt.Add(time.Hour)
			if tc.replayAfterCorrection {
				_, err := svc.BindPayment(ctx, e.ReferredCustomer, "economic-payment", paidAt)
				require.NoError(t, err)
			}
			svc.clock = fakeClock{correctionAt}
			_, err = svc.AssignAttribution(ctx, reviewedCorrection(t, svc, CorrectionRequest{Partner: PartnerState{"other-partner", "other-owner", true}, ReferredCustomer: e.ReferredCustomer, ActorID: "operator", Reason: "prospective ownership correction", ExpectedRevision: 1, ExpectedReferralID: original.ID, Terms: fixtureTerms()}))
			require.NoError(t, err)
			if tc.paidAfterCorrection {
				paidAt = correctionAt.Add(time.Hour)
			}
			binding, err := svc.BindPayment(ctx, e.ReferredCustomer, "economic-payment", paidAt)
			require.NoError(t, err)
			require.Equal(t, tc.wantPartner, binding.PartnerID)
			require.Len(t, repo.bindings, 1)
			_, err = svc.BindPayment(ctx, "different-payer", "economic-payment", paidAt)
			require.ErrorIs(t, err, ErrStaleWrite)
		})
	}
}
