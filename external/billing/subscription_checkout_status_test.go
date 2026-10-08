package billing

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

// Audit disposition: isolated named owning-service cases use a fresh immutable
// checkout snapshot and status transaction fixture. Mongo exercises the codec.
type checkoutStatusTestTx struct {
	lifecycleReadTx
	anchor         CheckoutLifecycleAnchor
	missingReceipt bool
}

func (t *checkoutStatusTestTx) GetCheckoutLifecycleAnchor(context.Context, RevenueScope, string) (CheckoutLifecycleAnchor, error) {
	return t.anchor, nil
}
func (t *checkoutStatusTestTx) GetCheckoutLifecycleReceipt(context.Context, string) (CheckoutLifecycleAnchor, error) {
	if t.missingReceipt {
		return CheckoutLifecycleAnchor{}, ErrRevenueNotFound
	}
	return t.anchor, nil
}
func (*checkoutStatusTestTx) InsertCheckoutLifecycleAnchor(context.Context, CheckoutLifecycleAnchor) error {
	panic("status cannot write checkout history")
}

type checkoutStatusTestRepo struct {
	*statusTestRepo
	checkout    checkoutStatusTestTx
	checkoutErr error
}

func (r *checkoutStatusTestRepo) ReadCheckout(ctx context.Context, fn func(CheckoutTx) error) error {
	if r.checkoutErr != nil {
		return r.checkoutErr
	}
	return fn(&r.checkout)
}
func (*checkoutStatusTestRepo) WithCheckoutTransaction(context.Context, RevenueScope, func(CheckoutTx) error) error {
	panic("status cannot write checkout history")
}
func checkoutStatusFixture(t *testing.T) (*RevenueService, *checkoutStatusTestRepo, *statusTestClock, CheckoutLifecycleAnchor, VerifiedRevenueRequest) {
	t.Helper()
	_, base, request := revenueFixture(t)
	f := request.Facts[0]
	f.ProviderCustomerID = "cus_payer"
	request.Facts[0] = f
	at := f.EffectiveAt.Add(time.Minute)
	clock := &statusTestClock{at.Add(time.Second)}
	q := paymentprovider.CheckoutSessionRequest{IdempotencyKey: "trial_checkout", PriceID: "price_original", PlanID: f.PlanID, CostID: f.CostID, UserID: f.PrincipalID, UserReference: f.PrincipalID, CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: f.Currency, ExpectedAmount: 1000, ExpectedBillingCadence: "month", TrialPeriodDays: 14, Metadata: map[string]string{}}
	id := checkoutIntentID(f.Scope, q.IdempotencyKey)
	q.Metadata["checkout_intent_id"] = id
	i := CheckoutIntent{ID: id, Scope: f.Scope, Request: q, CreatedAt: at, SessionID: "cs_trial", Fingerprint: checkoutRequestFingerprint(f.Scope, q)}
	e := paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: f.Scope.Provider, AccountID: f.Scope.AccountID, LiveMode: f.Scope.LiveMode}, SessionID: i.SessionID, IntentID: id, ClientReferenceID: q.UserID, CustomerID: f.ProviderCustomerID, SubscriptionID: f.SubscriptionID, PriceID: q.PriceID, Currency: q.ExpectedCurrency, Mode: q.Mode, Status: "complete", UnitAmountMinor: q.ExpectedAmount, IntervalCount: 1, BillingCadence: "month", CreatedAt: at}
	a := CheckoutLifecycleAnchor{IntentID: id, IntentFingerprint: i.Fingerprint, PrincipalID: q.UserID, PlanID: q.PlanID, CostID: q.CostID, Evidence: e, AnchoredAt: at}
	a.Fingerprint = lifecycleFingerprint(a)
	r := &checkoutStatusTestRepo{statusTestRepo: &statusTestRepo{revenueTestRepo: base, heads: map[string]SubscriptionStatus{}, captures: map[string]SubscriptionStatus{}}, checkout: checkoutStatusTestTx{lifecycleReadTx: lifecycleReadTx{intent: i, session: i.SessionID}, anchor: a}}
	s, err := NewRevenueService(r, clock)
	require.NoError(t, err)
	return s, r, clock, a, request
}
func TestCheckoutSubscriptionStatusSources(t *testing.T) {
	cases := []struct {
		name, state string
		paid        bool
		wrongPayer  bool
	}{{name: "trial_before_payment", state: "trialing"}, {name: "active_does_not_manufacture_payment", state: "active"}, {name: "cancel_before_payment", state: "canceled"}, {name: "checkout_head_then_paid_head", state: "active", paid: true}, {name: "paid_head_cannot_change_payer", state: "active", paid: true, wrongPayer: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, c, a, request := checkoutStatusFixture(t)
			ctx := context.Background()
			p, err := s.PrepareSubscriptionStatusForCheckout(ctx, "worker", lifecycleScope(a.Evidence), a.Evidence.SubscriptionID)
			require.NoError(t, err)
			require.Equal(t, SubscriptionStatusCheckoutSource, p.Source)
			require.Empty(t, p.FactID)
			first, err := s.CaptureVerifiedSubscriptionStatus(ctx, p, statusEvidence(p, tc.state))
			require.NoError(t, err)
			current, err := s.GetSubscriptionStatusForCheckout(ctx, p.Scope, p.SubscriptionID, time.Minute)
			require.NoError(t, err)
			require.Equal(t, first, current)
			require.Zero(t, r.sequence)
			require.Empty(t, r.facts)
			for _, v := range []any{p, first} {
				wire, err := json.Marshal(v)
				require.NoError(t, err)
				require.JSONEq(t, "{}", string(wire))
			}
			if tc.paid {
				if tc.wrongPayer {
					request.Facts[0].PrincipalID = "other_payer"
				}
				c.at = c.at.Add(time.Second)
				request.Facts[0].EffectiveAt = c.at
				o, err := s.AcceptVerified(ctx, request)
				require.NoError(t, err)
				paid, err := s.PrepareSubscriptionStatus(ctx, "paid_worker", o.FactIDs[0])
				if tc.wrongPayer {
					require.ErrorIs(t, err, ErrRevenueConflict)
					return
				}
				require.NoError(t, err)
				require.Empty(t, paid.Source)
				require.EqualValues(t, 1, paid.ExpectedRevision)
				second, err := s.CaptureVerifiedSubscriptionStatus(ctx, paid, statusEvidence(paid, "active"))
				require.NoError(t, err)
				current, err = s.GetSubscriptionStatusForCheckout(ctx, p.Scope, p.SubscriptionID, time.Minute)
				require.NoError(t, err)
				require.Equal(t, second, current)
				current, err = s.GetSubscriptionStatusForFact(ctx, o.FactIDs[0], time.Minute)
				require.NoError(t, err)
				require.Equal(t, second, current)
				old, err := s.CaptureVerifiedSubscriptionStatus(ctx, p, statusEvidence(p, tc.state))
				require.NoError(t, err)
				require.Equal(t, first, old)
			}
		})
	}
}

func TestCheckoutSubscriptionStatusBoundaries(t *testing.T) {
	outage := errors.New("owning checkout outage")
	cases := []struct {
		name                                        string
		missingReceipt, joinedOutage, tamper, stale bool
		want                                        error
	}{{name: "missing_joined_receipt", missingReceipt: true, want: ErrRevenueUnavailable}, {name: "joined_absence_outage", joinedOutage: true, want: outage}, {name: "changed_owning_fingerprint", tamper: true, want: ErrRevenueConflict}, {name: "stale_is_not_inactive", stale: true, want: ErrSubscriptionStatusStale}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, c, a, _ := checkoutStatusFixture(t)
			ctx := context.Background()
			p, err := s.PrepareSubscriptionStatusForCheckout(ctx, "worker", lifecycleScope(a.Evidence), a.Evidence.SubscriptionID)
			require.NoError(t, err)
			_, err = s.CaptureVerifiedSubscriptionStatus(ctx, p, statusEvidence(p, "trialing"))
			require.NoError(t, err)
			if tc.missingReceipt {
				r.checkout.missingReceipt = true
			}
			if tc.joinedOutage {
				r.checkoutErr = errors.Join(ErrRevenueNotFound, outage)
			}
			if tc.tamper {
				p.CheckoutFingerprint = "changed"
				p.CaptureID = "subscription_capture_" + subscriptionDigest(preparationBody(p))
				require.ErrorIs(t, s.ValidateSubscriptionStatusPreparation(ctx, p), tc.want)
				return
			}
			if tc.stale {
				c.at = c.at.Add(time.Minute + time.Second)
			}
			v, err := s.GetSubscriptionStatusForCheckout(ctx, p.Scope, p.SubscriptionID, time.Minute)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, v)
		})
	}
}

func TestSubscriptionStatusLegacyCanonicalGolden(t *testing.T) {
	cases := []struct {
		name     string
		checkout bool
	}{{"legacy_payment_bytes_unchanged", false}, {"checkout_format_is_distinct", true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := SubscriptionStatusPreparation{FactID: "fact_legacy", FactFingerprint: "fp_legacy", ActorID: "actor_legacy", Scope: RevenueScope{Provider: "stripe", AccountID: "acct_primary"}, PrincipalID: "payer", ProviderCustomerID: "cus_payer", SubscriptionID: "sub_original", RequestedAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
			// Frozen from the pre-extension canonical representation, not computed
			// from the implementation under test. Existing receipt IDs must survive.
			const capture = "subscription_capture_d2e995760a9f0f810bbf42a5ad19f39cdda68776051a4943ceea0c4d3ae221d3"
			const fingerprint = "2fb71b9dbfe546a7370a88a59fe3930235910a6959d467307dc806e5d4bf014b"
			if tc.checkout {
				p.Source = SubscriptionStatusCheckoutSource
				p.CheckoutIntentID = "checkout_original"
				p.CheckoutFingerprint = "anchor_frozen"
				p.FactID = ""
				p.FactFingerprint = ""
			}
			p.CaptureID = "subscription_capture_" + subscriptionDigest(preparationBody(p))
			require.NoError(t, p.Validate())
			if tc.checkout {
				require.NotEqual(t, capture, p.CaptureID)
				require.NotEqual(t, fingerprint, statusFingerprint(p, "active", false))
			} else {
				require.Equal(t, capture, p.CaptureID)
				require.Equal(t, fingerprint, statusFingerprint(p, "active", false))
			}
		})
	}
}

func TestSubscriptionStatusSourceUnion(t *testing.T) {
	cases := []struct {
		name   string
		change func(*SubscriptionStatusPreparation)
	}{
		{name: "unknown_source", change: func(p *SubscriptionStatusPreparation) { p.Source = "unreviewed" }},
		{name: "payment_alias_is_not_legacy", change: func(p *SubscriptionStatusPreparation) { p.Source = "payment" }},
		{name: "checkout_cannot_carry_fact", change: func(p *SubscriptionStatusPreparation) {
			p.FactID = "invented_paid_fact"
			p.FactFingerprint = "invented_paid_fp"
		}},
		{name: "empty_source_cannot_carry_checkout", change: func(p *SubscriptionStatusPreparation) { p.Source = "" }},
		{name: "missing_checkout_identity", change: func(p *SubscriptionStatusPreparation) { p.CheckoutIntentID = "" }},
		{name: "missing_checkout_fingerprint", change: func(p *SubscriptionStatusPreparation) { p.CheckoutFingerprint = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, a, _ := checkoutStatusFixture(t)
			p, err := s.PrepareSubscriptionStatusForCheckout(context.Background(), "worker", lifecycleScope(a.Evidence), a.Evidence.SubscriptionID)
			require.NoError(t, err)
			tc.change(&p)
			p.CaptureID = "subscription_capture_" + subscriptionDigest(preparationBody(p))
			require.ErrorIs(t, p.Validate(), ErrRevenueInvalid)
		})
	}
}
