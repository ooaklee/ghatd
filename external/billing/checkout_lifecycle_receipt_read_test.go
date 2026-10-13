package billing

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

type receiptReadTx struct {
	completionReadTx
	CheckoutLifecycleTx
	receipt               CheckoutLifecycleAnchor
	receiptErr, anchorErr error
}

func (t *receiptReadTx) GetCheckoutLifecycleReceipt(context.Context, string) (CheckoutLifecycleAnchor, error) {
	return t.receipt, t.receiptErr
}

func (t *receiptReadTx) GetCheckoutLifecycleAnchor(context.Context, RevenueScope, string) (CheckoutLifecycleAnchor, error) {
	return t.receipt, t.anchorErr
}

type receiptReplayRepository struct {
	CheckoutRepository
	attempts  []CheckoutTx
	callbacks []error
	outerErr  error
	afterRead func()
}

func (r *receiptReplayRepository) ReadCheckout(_ context.Context, fn func(CheckoutTx) error) error {
	var last error
	for _, tx := range r.attempts {
		last = fn(tx)
		r.callbacks = append(r.callbacks, last)
	}
	if r.afterRead != nil {
		r.afterRead()
	}
	if r.outerErr != nil {
		return r.outerErr
	}
	return last
}

type receiptNotFoundAlias struct{}

func (receiptNotFoundAlias) Error() string     { return "custom absence alias" }
func (receiptNotFoundAlias) Is(err error) bool { return err == ErrRevenueNotFound }

// The owning read must finish before optional absence can be exposed. Repeated
// callbacks model snapshot retries; required provenance and commit failures
// remain authoritative over any optional result observed by an earlier callback.
func TestCheckoutLifecycleReceiptReadOutcome(t *testing.T) {
	outage := errors.New("native read failed")
	alias := receiptNotFoundAlias{}
	joined := errors.Join(ErrRevenueNotFound, outage)
	for _, tc := range []struct {
		name           string
		states         []string
		outerErr, want error
		cancel         bool
		callbackErrors []bool
	}{
		{name: "absence_inside_successful_read", states: []string{"absent"}, want: ErrRevenueNotFound, callbackErrors: []bool{false}},
		{name: "wrapped_absence_inside_successful_read", states: []string{"wrapped"}, want: ErrRevenueNotFound, callbackErrors: []bool{false}},
		{name: "existing_receipt", states: []string{"found"}, callbackErrors: []bool{false}},
		{name: "retry_absence_then_found", states: []string{"absent", "found"}, callbackErrors: []bool{false, false}},
		{name: "retry_found_then_absence", states: []string{"found", "absent"}, want: ErrRevenueNotFound, callbackErrors: []bool{false, false}},
		{name: "retry_absence_then_read_failure", states: []string{"absent", "outage"}, want: outage, callbackErrors: []bool{false, true}},
		{name: "retry_found_then_original_missing", states: []string{"found", "original"}, want: ErrRevenueUnavailable, callbackErrors: []bool{false, true}},
		{name: "joined_absence_and_outage", states: []string{"joined"}, want: joined, callbackErrors: []bool{true}},
		{name: "single_element_join_is_not_single_chain", states: []string{"single_join"}, want: ErrRevenueNotFound, callbackErrors: []bool{true}},
		{name: "custom_is_is_not_absence", states: []string{"alias"}, want: alias, callbackErrors: []bool{true}},
		{name: "original_missing", states: []string{"original"}, want: ErrRevenueUnavailable, callbackErrors: []bool{true}},
		{name: "forward_ack_missing", states: []string{"forward"}, want: ErrRevenueUnavailable, callbackErrors: []bool{true}},
		{name: "reverse_ack_missing", states: []string{"reverse"}, want: ErrRevenueUnavailable, callbackErrors: []bool{true}},
		{name: "joined_anchor_missing", states: []string{"anchor"}, want: ErrRevenueUnavailable, callbackErrors: []bool{true}},
		{name: "absence_then_commit_failure", states: []string{"absent"}, outerErr: outage, want: outage, callbackErrors: []bool{false}},
		{name: "absence_then_uncertain_commit", states: []string{"absent"}, outerErr: ErrRevenueUncertain, want: ErrRevenueUncertain, callbackErrors: []bool{false}},
		{name: "found_then_commit_failure", states: []string{"found"}, outerErr: outage, want: outage, callbackErrors: []bool{false}},
		{name: "absence_then_cancellation", states: []string{"absent"}, cancel: true, want: context.Canceled, callbackErrors: []bool{false}},
		{name: "commit_error_precedes_late_cancellation", states: []string{"absent"}, outerErr: ErrRevenueUncertain, cancel: true, want: ErrRevenueUncertain, callbackErrors: []bool{false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := completionIntent()
			e := paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: i.Scope.Provider, AccountID: i.Scope.AccountID}, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: i.Request.UserReference, CustomerID: "cus_original", SubscriptionID: "sub_original", PriceID: i.Request.PriceID, Currency: "GBP", Mode: i.Request.Mode, Status: "complete", UnitAmountMinor: i.Request.ExpectedAmount, IntervalCount: 1, BillingCadence: "month", CreatedAt: i.CreatedAt}
			receipt := CheckoutLifecycleAnchor{IntentID: i.ID, IntentFingerprint: i.Fingerprint, PrincipalID: i.Request.UserID, PlanID: i.Request.PlanID, CostID: i.Request.CostID, Evidence: e, AnchoredAt: i.CreatedAt}
			receipt.Fingerprint = lifecycleFingerprint(receipt)
			require.NoError(t, receipt.ValidateForCheckout(i))
			repo := &receiptReplayRepository{outerErr: tc.outerErr}
			for _, state := range tc.states {
				tx := &receiptReadTx{completionReadTx: completionReadTx{lifecycleReadTx: lifecycleReadTx{intent: i, session: i.SessionID}}, receipt: receipt}
				switch state {
				case "absent":
					tx.receiptErr = ErrRevenueNotFound
				case "wrapped":
					tx.receiptErr = fmt.Errorf("receipt read: %w", ErrRevenueNotFound)
				case "outage":
					tx.receiptErr = outage
				case "joined":
					tx.receiptErr = joined
				case "single_join":
					tx.receiptErr = errors.Join(ErrRevenueNotFound)
				case "alias":
					tx.receiptErr = alias
				case "original":
					tx.err = ErrRevenueNotFound
				case "forward":
					tx.session = ""
				case "reverse":
					tx.reverseErr = ErrRevenueNotFound
				case "anchor":
					tx.anchorErr = ErrRevenueNotFound
				}
				repo.attempts = append(repo.attempts, tx)
			}
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			if tc.cancel {
				repo.afterRead = cancel
			}
			provider := &lifecycleReadProvider{}
			service, err := NewCheckoutService(repo, revenueTestClock{at: i.CreatedAt}, provider)
			require.NoError(t, err)
			out, err := service.FindCheckoutLifecycleReceipt(ctx, i)
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.Equal(t, receipt, out)
			} else {
				require.Zero(t, out)
			}
			require.Len(t, repo.callbacks, len(tc.callbackErrors))
			for n, failed := range tc.callbackErrors {
				require.Equal(t, failed, repo.callbacks[n] != nil, "callback %d", n)
			}
			require.Zero(t, provider.calls)
		})
	}
}
