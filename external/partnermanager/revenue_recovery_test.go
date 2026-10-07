package partnermanager

import (
	"context"
	"errors"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Audit disposition: new named cases for recovery of existing financial
// acceptance and adjustment provenance through current scoped owning ports.
func TestRevenueCommittedAcceptanceRecovery(t *testing.T) {
	type testCase struct {
		name      string
		accepted  bool
		paused    bool
		revoked   bool
		readError error
		want      error
	}
	cases := []testCase{
		{name: "accepted_accrual_replays_after_global_and_partner_pause", accepted: true, paused: true},
		{name: "fresh_accrual_remains_denied_after_pause", paused: true, want: ErrDenied},
		{name: "accepted_accrual_still_requires_current_worker_authority", accepted: true, paused: true, revoked: true, want: ErrDenied},
		{name: "acceptance_read_outage_is_retryable", paused: true, readError: partnerearnings.ErrUnavailable, want: partnerearnings.ErrUnavailable},
		{name: "joined_absence_and_outage_is_not_new_admission", readError: errors.Join(partnerearnings.ErrNotFound, partnerearnings.ErrUnavailable), want: partnerearnings.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, p, r, e, a, _, feed := managerFixture(t)
			at := m.deps.Clock.Now().Add(-time.Hour)
			fact := billing.RevenueFact{Scope: billing.RevenueScope{Provider: "fixture", AccountID: "account"}, Kind: billing.RevenuePayment, PaymentID: "payment", InvoiceID: "invoice", AllocationID: "line", PrincipalID: "payer", SubscriptionID: "subscription", PlanID: "plan", CostID: "cost", Currency: "EUR", CurrencyExponent: 2, PaidMinor: 10000, EffectiveAt: at}
			fact.ID = fact.PaymentFactID()
			feed.facts[fact.ID] = fact
			if tc.paused {
				m.deps.Controls.Accrual = false
				p.partner.CanAccrue = false
			}
			if tc.accepted {
				e.acceptedEntry = &partnerearnings.Entry{ID: "previously-committed"}
				p.lookupErr = partnerLookupUnavailable()
			}
			e.acceptedError = tc.readError
			if tc.revoked {
				a.deny = true
			}
			entries, err := m.ProcessRevenueFact(context.Background(), "worker", fact.ID)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, entries)
				require.Nil(t, e.accrual)
				return
			}
			require.NoError(t, err)
			require.Len(t, entries, 1)
			require.NotNil(t, e.accrual)
			require.Equal(t, 2000, e.accrual.RateBasisPoints)
			require.Equal(t, 7*24*time.Hour, e.accrual.HoldDuration)
			require.Equal(t, "payer", r.boundPrincipal)
			require.Equal(t, at, e.accrual.OccurredAt)
			require.Equal(t, fact.PlanID, e.accrual.PlanID, "billing freezes the original plan even after current policy/admission changes")
		})
	}
}
func partnerLookupUnavailable() error { return ErrUnavailable }

func TestRevenueAdjustmentOriginalProvenance(t *testing.T) {
	type testCase struct {
		name, kind string
		alter      func(*billing.RevenueFact)
		want       error
	}
	cases := []testCase{
		{name: "dispute_hold_recovery_while_accrual_paused", kind: billing.RevenueDisputeHold},
		{name: "dispute_won_recovery_while_accrual_paused", kind: billing.RevenueDisputeWon},
		{name: "dispute_lost_recovery_while_accrual_paused", kind: billing.RevenueDisputeLost},
		{name: "refund_payer_cannot_change", kind: billing.RevenueRefund, alter: func(f *billing.RevenueFact) { f.PrincipalID = "other" }, want: billing.ErrRevenueUnassessable},
		{name: "refund_original_net_cannot_change", kind: billing.RevenueRefund, alter: func(f *billing.RevenueFact) { f.PaidMinor++ }, want: billing.ErrRevenueUnassessable},
		{name: "refund_plan_cannot_change", kind: billing.RevenueRefund, alter: func(f *billing.RevenueFact) { f.PlanID = "current-plan" }, want: billing.ErrRevenueUnassessable},
		{name: "refund_provider_price_cannot_change", kind: billing.RevenueRefund, alter: func(f *billing.RevenueFact) { f.ProviderPriceID = "new-price" }, want: billing.ErrRevenueUnassessable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, p, _, e, _, _, feed := managerFixture(t)
			m.deps.Controls.Accrual = false
			p.partner.CanAccrue = false
			at := m.deps.Clock.Now().Add(-time.Hour)
			original := billing.RevenueFact{Scope: billing.RevenueScope{Provider: "fixture", AccountID: "account"}, Kind: billing.RevenuePayment, PaymentID: "payment", InvoiceID: "invoice", AllocationID: "line", PrincipalID: "payer", SubscriptionID: "subscription", PlanID: "plan", CostID: "cost", ProviderPriceID: "original-price", Currency: "EUR", CurrencyExponent: 2, PaidMinor: 10000, EffectiveAt: at}
			original.ID = original.PaymentFactID()
			feed.facts[original.ID] = original
			adjustment := original
			adjustment.ID = "verified-adjustment"
			adjustment.Kind = tc.kind
			adjustment.AdjustmentID = "stable-dispute"
			adjustment.EffectiveAt = at.Add(time.Hour)
			adjustment.CumulativeRefundedMinor = 1000
			if tc.alter != nil {
				tc.alter(&adjustment)
			}
			feed.facts[adjustment.ID] = adjustment
			_, err := m.ProcessRevenueFact(context.Background(), "current-worker", adjustment.ID)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, e.refund)
				require.Nil(t, e.dispute)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, e.dispute)
			require.Equal(t, "current-worker", e.dispute.ActorID)
			require.Equal(t, original.ID, e.dispute.PaymentID)
			require.Equal(t, "stable-dispute", e.dispute.DisputeID)
			require.Equal(t, adjustment.ID, e.dispute.OperationID)
		})
	}
}
