package billing

import (
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

func TestCheckoutLifecycleExpiryProofBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*CheckoutIntent, *CheckoutLifecycleStatus, *time.Time)
		valid  bool
	}{
		{name: "exact_expired_original", valid: true},
		{name: "freshness_boundary", valid: true, change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, now *time.Time) {
			*now = s.ObservedAt.Add(5 * time.Minute)
		}},
		{name: "stale", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, now *time.Time) {
			*now = s.ObservedAt.Add(5*time.Minute + time.Nanosecond)
		}},
		{name: "future_observation", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, now *time.Time) {
			*now = s.ObservedAt.Add(-time.Nanosecond)
		}},
		{name: "unknown_subscription_absence", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) {
			s.Evidence.SubscriptionAbsent = false
		}},
		{name: "open_not_terminal", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) { s.Evidence.SessionStatus = "open" }},
		{name: "completed_not_terminal", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) {
			s.Evidence.SessionStatus = "complete"
		}},
		{name: "paid_not_expiry", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) { s.Evidence.PaymentStatus = "paid" }},
		{name: "unknown_total", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) { s.Evidence.AmountTotalKnown = false }},
		{name: "wrong_session", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) { s.Evidence.SessionID = "cs_other" }},
		{name: "wrong_intent", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) { s.Evidence.IntentID = "other" }},
		{name: "wrong_live_scope", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) { s.Evidence.Scope.LiveMode = true }},
		{name: "wrong_merchant", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) {
			s.Evidence.Scope.AccountID = "other"
		}},
		{name: "wrong_payer", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) {
			s.Evidence.ClientReferenceID = "other"
		}},
		{name: "wrong_price", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) { s.Evidence.PriceID = "other" }},
		{name: "wrong_currency", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) { s.Evidence.Currency = "USD" }},
		{name: "wrong_amount", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) { s.Evidence.UnitAmountMinor++ }},
		{name: "wrong_cadence", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) { s.Evidence.BillingCadence = "year" }},
		{name: "provider_future_creation", change: func(_ *CheckoutIntent, s *CheckoutLifecycleStatus, _ *time.Time) {
			s.Evidence.CreatedAt = s.ObservedAt.Add(time.Second)
		}},
		{name: "unacknowledged", change: func(i *CheckoutIntent, _ *CheckoutLifecycleStatus, _ *time.Time) { i.SessionID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := completionIntent()
			s := CheckoutLifecycleStatus{ObservedAt: i.CreatedAt.Add(time.Minute), Evidence: paymentprovider.CheckoutStatusEvidence{Scope: paymentprovider.RevenueScope{Provider: i.Scope.Provider, AccountID: i.Scope.AccountID}, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: i.Request.UserReference, PriceID: i.Request.PriceID, Currency: "GBP", Mode: i.Request.Mode, SessionStatus: "expired", PaymentStatus: "unpaid", UnitAmountMinor: i.Request.ExpectedAmount, IntervalCount: 1, BillingCadence: "month", AmountTotalMinor: i.Request.ExpectedAmount, AmountTotalKnown: true, SubscriptionAbsent: true, CreatedAt: i.CreatedAt}}
			now := s.ObservedAt
			if tc.change != nil {
				tc.change(&i, &s, &now)
			}
			proof, err := NewCheckoutLifecycleExpiry(i, s)
			if err == nil {
				err = proof.Status.ValidateFresh(i, now)
			}
			if tc.valid {
				require.NoError(t, err)
				require.NoError(t, proof.ValidateForCheckout(i))
				proof.Fingerprint = "altered"
				require.Error(t, proof.ValidateForCheckout(i))
			} else {
				require.Error(t, err)
			}
		})
	}
}
