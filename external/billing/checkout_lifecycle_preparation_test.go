package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

type completionReadTx struct {
	lifecycleReadTx
	reverseErr   error
	reverseCalls int
}

func (t *completionReadTx) ValidateCheckoutAcknowledgement(context.Context, CheckoutIntent) error {
	t.reverseCalls++
	return t.reverseErr
}

type completionReadRepo struct {
	tx            CheckoutTx
	reads, writes int
	afterRead     func()
}

func (r *completionReadRepo) ReadCheckout(_ context.Context, fn func(CheckoutTx) error) error {
	r.reads++
	err := fn(r.tx)
	if r.afterRead != nil {
		r.afterRead()
	}
	return err
}
func (r *completionReadRepo) WithCheckoutTransaction(context.Context, RevenueScope, func(CheckoutTx) error) error {
	r.writes++
	panic("preparation must not write")
}

func completionIntent() CheckoutIntent {
	at := time.Unix(1700000000, 0).UTC()
	scope := RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
	q := paymentprovider.CheckoutSessionRequest{PriceID: "price_frozen", PlanID: "plan", CostID: "cost", UserID: "payer", UserReference: "payer", CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: "GBP", ExpectedAmount: 1000, ExpectedBillingCadence: "month", IdempotencyKey: "key", Metadata: map[string]string{}}
	id := checkoutIntentID(scope, q.IdempotencyKey)
	q.Metadata["checkout_intent_id"] = id
	return CheckoutIntent{ID: id, Scope: scope, Request: q, CreatedAt: at, SessionID: "cs_saved", Fingerprint: checkoutRequestFingerprint(scope, q)}
}

func TestCheckoutLifecyclePreparation(t *testing.T) {
	outage := errors.New("read unavailable")
	for _, tc := range []struct {
		name, state string
		want        error
	}{
		{"acknowledged_original_detached", "", nil}, {"legacy_snapshot_missing_reverse_capability", "legacy", ErrRevenueUnavailable},
		{"original_absence_is_not_authorization_absence", "absent", ErrRevenueUnavailable}, {"joined_absence_outage_preserved", "joined", outage},
		{"reverse_absence_is_unavailable", "reverse-absent", ErrRevenueUnavailable}, {"reverse_owner_conflict", "reverse-conflict", ErrRevenueConflict},
		{"original_session_changed", "session", ErrRevenueConflict}, {"original_creation_changed", "created", ErrRevenueConflict},
		{"original_fingerprint_changed", "fingerprint", ErrRevenueUnavailable}, {"input_currency_changed", "input", ErrRevenueInvalid},
		{"input_not_acknowledged", "unacked", ErrRevenueInvalid}, {"input_payment_mode", "payment", ErrRevenueInvalid},
		{"cancel_before_read", "cancel", context.Canceled}, {"cancel_after_snapshot", "late-cancel", context.Canceled},
		{"nil_context", "nil", ErrRevenueInvalid}, {"repository_outage", "outage", outage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := completionIntent()
			original := i
			original.Request = cloneCheckoutRequest(i.Request)
			tx := &completionReadTx{lifecycleReadTx: lifecycleReadTx{intent: original, session: i.SessionID}}
			repo := &completionReadRepo{tx: tx}
			provider := &lifecycleReadProvider{}
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			switch tc.state {
			case "legacy":
				repo.tx = tx.lifecycleReadTx
			case "absent":
				tx.err = ErrRevenueNotFound
			case "joined":
				tx.err = errors.Join(ErrRevenueNotFound, outage)
			case "reverse-absent":
				tx.reverseErr = ErrRevenueNotFound
			case "reverse-conflict":
				tx.reverseErr = ErrRevenueConflict
			case "session":
				tx.session = "cs_changed"
			case "created":
				i.CreatedAt = i.CreatedAt.Add(time.Second)
			case "fingerprint":
				tx.intent.Fingerprint = "changed"
			case "input":
				i.Request.ExpectedCurrency = "EUR"
			case "unacked":
				i.SessionID = ""
			case "payment":
				i.Request.Mode = paymentprovider.CheckoutModePayment
				i.Fingerprint = checkoutRequestFingerprint(i.Scope, i.Request)
			case "cancel":
				cancel()
			case "late-cancel":
				repo.afterRead = cancel
			case "nil":
				ctx = nil
			case "outage":
				tx.err = outage
			}
			s, err := NewCheckoutService(repo, revenueTestClock{at: i.CreatedAt.Add(time.Minute)}, provider)
			require.NoError(t, err)
			out, err := s.PrepareCheckoutLifecycle(ctx, i)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, out)
			} else {
				require.Equal(t, i, out)
				out.Request.Metadata["mutated"] = "value"
				require.NotContains(t, tx.intent.Request.Metadata, "mutated")
				require.Equal(t, 1, tx.reverseCalls)
				require.Equal(t, 1, repo.reads)
			}
			require.Zero(t, repo.writes)
			require.Zero(t, provider.calls)
		})
	}
}

type completionCancelProvider struct {
	lifecycleReadProvider
	afterLookup func()
}

func (p *completionCancelProvider) LookupRevenueCheckoutSessionEvidence(ctx context.Context, scope paymentprovider.RevenueScope, session string) (paymentprovider.RevenueCheckoutEvidence, error) {
	out, err := p.lifecycleReadProvider.LookupRevenueCheckoutSessionEvidence(ctx, scope, session)
	if p.afterLookup != nil {
		p.afterLookup()
	}
	return out, err
}
func TestCheckoutLifecycleLookupLateCancellation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		afterSnapshot bool
		calls         int
	}{{"snapshot_cancellation_prevents_provider", true, 0}, {"provider_cancellation_withholds_evidence", false, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			i := completionIntent()
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			repo := &completionReadRepo{tx: &completionReadTx{lifecycleReadTx: lifecycleReadTx{intent: i, session: i.SessionID}}}
			p := &completionCancelProvider{lifecycleReadProvider: lifecycleReadProvider{evidence: paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: i.Scope.Provider, AccountID: i.Scope.AccountID}, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: i.Request.UserID, CustomerID: "cus_original", SubscriptionID: "sub_original", PriceID: i.Request.PriceID, Currency: "GBP", Mode: i.Request.Mode, Status: "complete", UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month", CreatedAt: i.CreatedAt}}}
			if tc.afterSnapshot {
				repo.afterRead = cancel
			} else {
				p.afterLookup = cancel
			}
			s, err := NewCheckoutService(repo, revenueTestClock{at: i.CreatedAt.Add(time.Minute)}, p)
			require.NoError(t, err)
			out, err := s.LookupCheckoutLifecycleEvidence(ctx, i)
			require.ErrorIs(t, err, context.Canceled)
			require.Zero(t, out)
			require.Equal(t, tc.calls, p.calls)
			require.Zero(t, repo.writes)
		})
	}
}
