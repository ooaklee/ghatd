package partnerstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// This narrow port checks facade argument binding, not actual policy persistence
// or authenticated sessions. Program, identity, financial and encrypted record
// owners are real; direct financial setup establishes no provider/transfer proof.
type actionClaimAuthority struct {
	capability, target string
	checks             int
}

func (a *actionClaimAuthority) CheckPartners(_ context.Context, actor, capability, target string) error {
	a.checks++
	if actor != "fixture-operator" || capability != a.capability || target != a.target {
		return partnermanager.ErrDenied
	}
	return nil
}

func TestMongoSelectedActionClaimReadPreservesOwningObligation(t *testing.T) {
	for _, tc := range []struct {
		name, capability string
		paid, other      bool
		want             error
	}{
		{"processing_reads_selected_requested_claim", partnermanager.CapabilityProcessing, false, false, nil},
		{"recording_reads_selected_requested_claim", partnermanager.CapabilityRecordPayment, false, false, nil},
		{"amendment_reads_selected_paid_claim", partnermanager.CapabilityAmendPayment, true, false, nil},
		{"return_reads_selected_paid_claim", partnermanager.CapabilityReturnPayment, true, false, nil},
		{"grant_cannot_read_another_claim", partnermanager.CapabilityAmendPayment, true, true, partnermanager.ErrDenied},
		{"reporting_cannot_substitute_action", partnermanager.CapabilityReporting, false, false, partnermanager.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			p := enrollCorrectionPartner(t, f, "selected-claim-owner@example.test")
			_, err := f.earnings.Accrue(f.ctx, partnerearnings.AccrualRequest{PartnerID: p.ID, PaymentID: "fixture-credit", PaymentMinor: 10000, RateBasisPoints: 2000, Currency: "EUR", OccurredAt: f.clock.Now().Add(-8 * 24 * time.Hour)})
			require.NoError(t, err)
			_, err = f.earnings.Mature(f.ctx, p.ID)
			require.NoError(t, err)
			d, err := f.program.UpdatePayoutDestination(f.ctx, partnerprogram.DestinationRequest{CustomerID: p.CustomerID, Method: "paypal", PayPalEmail: "selected-claim-owner@example.test"})
			require.NoError(t, err)
			claim, err := f.earnings.RequestClaim(f.ctx, partnerearnings.ClaimRequest{ActorID: p.CustomerID, PartnerID: p.ID, AmountMinor: 100, Currency: "EUR", DestinationID: d.ID, DestinationSnapshot: map[string]string{"destination_id": d.ID, "method": d.Method, "email": d.Email, "version": fmt.Sprint(d.Version)}, IdempotencyKey: "fixture-request"})
			require.NoError(t, err)
			if tc.paid {
				claim, err = f.earnings.DecideClaim(f.ctx, partnerearnings.ClaimDecision{ActorID: "fixture-operator", ClaimID: claim.ID, NewState: partnerearnings.ClaimProcessing, Reason: "controlled fixture", ExpectedRevision: claim.Revision})
				require.NoError(t, err)
				claim, err = f.earnings.RecordPayment(f.ctx, partnerearnings.RecordPaymentRequest{ActorID: "fixture-operator", ClaimID: claim.ID, Method: "bank", Reference: "fixture-transfer", PaidAt: f.clock.Now(), AmountMinor: claim.AmountMinor, Currency: claim.Currency, State: partnerearnings.PaymentStateFull, ExpectedRevision: claim.Revision, IdempotencyKey: "fixture-payment"})
				require.NoError(t, err)
			}
			a := &actionClaimAuthority{capability: tc.capability, target: claim.ID}
			m, err := partnermanager.NewManager(partnermanager.Dependencies{Program: f.program, Referral: f.referrals, Earnings: f.earnings, Identity: f.identity, Authority: a, Groups: workerGroups{}, Revenue: f.revenue, Evidence: f.signer, Clock: f.clock})
			require.NoError(t, err)
			before, err := f.earnings.GetStatement(f.ctx, p.ID, partnerearnings.StatementQuery{Limit: 100})
			require.NoError(t, err)
			selected := claim.ID
			if tc.other {
				selected = "another-claim"
			}
			out, err := m.AdminClaimForAction(f.ctx, "fixture-operator", tc.capability, selected)
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.Equal(t, claim, out)
				require.Equal(t, 2, a.checks)
			} else {
				require.Equal(t, partnerearnings.Claim{}, out)
				if tc.other {
					require.Equal(t, 1, a.checks)
				} else {
					require.Zero(t, a.checks)
				}
			}
			after, err := f.earnings.GetStatement(f.ctx, p.ID, partnerearnings.StatementQuery{Limit: 100})
			require.NoError(t, err)
			require.Equal(t, before, after, "the selected action read cannot reserve, settle or append a journal entry")
			stored, err := f.earnings.GetClaim(f.ctx, claim.ID)
			require.NoError(t, err)
			require.Equal(t, claim, stored)
		})
	}
}
