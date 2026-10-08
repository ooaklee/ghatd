package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

// Audit disposition: fresh narrow-port fixtures verify owning service boundaries
// without a datastore. Unexpected unused transaction methods must not be called.
type lifecycleReadTx struct {
	CheckoutTx
	intent  CheckoutIntent
	session string
	err     error
}

func (t lifecycleReadTx) GetCheckoutIntent(context.Context, string) (CheckoutIntent, error) {
	return t.intent, t.err
}
func (t lifecycleReadTx) GetCheckoutAcknowledgement(context.Context, string) (string, error) {
	return t.session, t.err
}

type lifecycleReadRepo struct {
	tx     lifecycleReadTx
	writes int
}

func (r *lifecycleReadRepo) ReadCheckout(ctx context.Context, fn func(CheckoutTx) error) error {
	return fn(r.tx)
}
func (r *lifecycleReadRepo) WithCheckoutTransaction(ctx context.Context, _ RevenueScope, fn func(CheckoutTx) error) error {
	r.writes++
	return fn(r.tx)
}

type lifecycleReadProvider struct {
	evidence paymentprovider.RevenueCheckoutEvidence
	calls    int
	scope    paymentprovider.RevenueScope
	session  string
}

func (p *lifecycleReadProvider) LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error) {
	panic("subscription lookup must not discover a pre-payment session")
}
func (p *lifecycleReadProvider) LookupRevenueCheckoutSessionEvidence(_ context.Context, scope paymentprovider.RevenueScope, session string) (paymentprovider.RevenueCheckoutEvidence, error) {
	p.calls++
	p.scope = scope
	p.session = session
	return p.evidence, nil
}

type lifecycleLegacyProvider struct{}

func (*lifecycleLegacyProvider) LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error) {
	panic("missing capability must not fall back")
}

func TestCheckoutLifecycleLookupBoundaries(t *testing.T) {
	outage := errors.New("repository unavailable")
	cases := []struct {
		name                                                      string
		missingAck, canceled, nilContext, legacy, changedOriginal bool
		repoErr                                                   error
		want                                                      error
		calls                                                     int
	}{
		{name: "exact_retained_session_readonly", calls: 1},
		{name: "unacknowledged_intent_refuses_lookup", missingAck: true, want: ErrRevenueInvalid},
		{name: "cancellation_before_lookup", canceled: true, want: context.Canceled},
		{name: "nil_context", nilContext: true, want: ErrRevenueInvalid},
		{name: "legacy_provider_remains_compatible", legacy: true, want: ErrRevenueUnavailable},
		{name: "repository_outage_is_not_absence", repoErr: outage, want: outage},
		{name: "changed_original_acknowledgement", changedOriginal: true, want: ErrRevenueConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(1700000000, 0).UTC()
			scope := RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
			q := paymentprovider.CheckoutSessionRequest{PriceID: "price_frozen", PlanID: "plan", CostID: "cost", UserID: "payer", UserReference: "payer", CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: "GBP", ExpectedAmount: 1000, ExpectedBillingCadence: "month", IdempotencyKey: "key", Metadata: map[string]string{}}
			id := checkoutIntentID(scope, q.IdempotencyKey)
			q.Metadata["checkout_intent_id"] = id
			i := CheckoutIntent{ID: id, Scope: scope, Request: q, CreatedAt: at, SessionID: "cs_saved", Fingerprint: checkoutRequestFingerprint(scope, q)}
			repo := &lifecycleReadRepo{tx: lifecycleReadTx{intent: i, session: i.SessionID, err: tc.repoErr}}
			if tc.missingAck {
				i.SessionID = ""
			}
			if tc.changedOriginal {
				repo.tx.session = "cs_changed"
			}
			p := &lifecycleReadProvider{evidence: paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, SessionID: "cs_saved", IntentID: id, ClientReferenceID: q.UserID, CustomerID: "cus_payer", SubscriptionID: "sub_trial", PriceID: q.PriceID, Currency: "GBP", Mode: q.Mode, Status: "complete", UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month", CreatedAt: at}}
			var provider CheckoutEvidenceProvider = p
			if tc.legacy {
				provider = &lifecycleLegacyProvider{}
			}
			s, err := NewCheckoutService(repo, revenueTestClock{at}, provider)
			require.NoError(t, err)
			ctx := context.Background()
			if tc.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if tc.nilContext {
				ctx = nil
			}
			e, err := s.LookupCheckoutLifecycleEvidence(ctx, i)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, e.SubscriptionID)
			} else {
				require.NoError(t, err)
				require.Equal(t, p.evidence, e)
				require.Equal(t, i.SessionID, p.session)
				require.Equal(t, p.evidence.Scope, p.scope)
			}
			require.Equal(t, tc.calls, p.calls)
			require.Zero(t, repo.writes)
		})
	}
}
