package billing

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

type associationReadSnapshot struct {
	association CheckoutAssociation
	err         error
}

type associationReadRepo struct {
	CheckoutRepository
	snapshots []associationReadSnapshot
	readError error
	callbacks []error
	cancel    context.CancelFunc
}

func (r *associationReadRepo) ReadCheckout(ctx context.Context, fn func(CheckoutTx) error) error {
	for _, snapshot := range r.snapshots {
		err := fn(associationReadTx{snapshot: snapshot})
		r.callbacks = append(r.callbacks, err)
		if err != nil {
			return err
		}
	}
	if r.cancel != nil {
		r.cancel()
	}
	return r.readError
}

type associationReadTx struct {
	CheckoutTx
	snapshot associationReadSnapshot
}

func (tx associationReadTx) GetCheckoutAssociation(context.Context, RevenueScope, string, string) (CheckoutAssociation, error) {
	return tx.snapshot.association, tx.snapshot.err
}

type associationAbsentProvider struct{ calls int }

func (p *associationAbsentProvider) LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error) {
	p.calls++
	return paymentprovider.RevenueCheckoutEvidence{}, paymentprovider.ErrRevenueUnassessable
}

type associationIsOnlyMissing struct{}

func (associationIsOnlyMissing) Error() string {
	return "fixture claims absence without a sentinel cause"
}
func (associationIsOnlyMissing) Is(target error) bool { return target == ErrRevenueNotFound }

func TestAssociationOptionalReadOutcomes(t *testing.T) {
	at := time.Unix(1700000000, 0).UTC()
	request := RevenueAssociationRequest{Scope: RevenueScope{Provider: "fixture", AccountID: "account"}, InvoiceID: "invoice", PaymentID: "payment", CustomerID: "customer", SubscriptionID: "subscription", ProviderPriceID: "price", Currency: "GBP", PaidAt: at}
	stored := CheckoutAssociation{Scope: request.Scope, SubscriptionID: request.SubscriptionID, CustomerID: request.CustomerID, ProviderPriceID: request.ProviderPriceID, Currency: request.Currency, CheckoutCreatedAt: at.Add(-time.Minute), PrincipalID: "principal", PlanID: "plan", CostID: "cost"}
	want := RevenueAssociation{PrincipalID: stored.PrincipalID, PlanID: stored.PlanID, CostID: stored.CostID}
	absent := associationReadSnapshot{err: ErrRevenueNotFound}
	present := associationReadSnapshot{association: stored}
	failed := errors.New("fixture database failure")
	joined := errors.Join(ErrRevenueNotFound, failed)
	joinedOnly := errors.Join(ErrRevenueNotFound)
	nested := fmt.Errorf("lookup: %w", joined)
	isOnly := associationIsOnlyMissing{}
	boundary := error(ErrRevenueNotFound)
	for range 31 {
		boundary = fmt.Errorf("lookup: %w", boundary)
	}
	tooDeep := fmt.Errorf("lookup: %w", boundary)
	cases := []struct {
		name            string
		snapshots       []associationReadSnapshot
		readError       error
		want            RevenueAssociation
		wantError       error
		callbacks       []error
		lookups         int
		cancelAfterRead bool
	}{
		{name: "absence_reaches_unchanged_provider_refusal", snapshots: []associationReadSnapshot{absent}, wantError: ErrRevenueUnassessable, callbacks: []error{nil}, lookups: 1},
		{name: "wrapped_absence", snapshots: []associationReadSnapshot{{err: fmt.Errorf("lookup: %w", ErrRevenueNotFound)}}, wantError: ErrRevenueUnassessable, callbacks: []error{nil}, lookups: 1},
		{name: "single_chain_depth_boundary", snapshots: []associationReadSnapshot{{err: boundary}}, wantError: ErrRevenueUnassessable, callbacks: []error{nil}, lookups: 1},
		{name: "existing_association", snapshots: []associationReadSnapshot{present}, want: want, callbacks: []error{nil}},
		{name: "retry_absent_then_present", snapshots: []associationReadSnapshot{absent, present}, want: want, callbacks: []error{nil, nil}},
		{name: "retry_present_then_absent", snapshots: []associationReadSnapshot{present, absent}, wantError: ErrRevenueUnassessable, callbacks: []error{nil, nil}, lookups: 1},
		{name: "retry_absent_present_absent", snapshots: []associationReadSnapshot{absent, present, absent}, wantError: ErrRevenueUnassessable, callbacks: []error{nil, nil, nil}, lookups: 1},
		{name: "retry_present_absent_present", snapshots: []associationReadSnapshot{present, absent, present}, want: want, callbacks: []error{nil, nil, nil}},
		{name: "joined_absence_and_failure", snapshots: []associationReadSnapshot{{err: joined}}, wantError: joined, callbacks: []error{joined}},
		{name: "joined_absence_only", snapshots: []associationReadSnapshot{{err: joinedOnly}}, wantError: joinedOnly, callbacks: []error{joinedOnly}},
		{name: "nested_joined_absence", snapshots: []associationReadSnapshot{{err: nested}}, wantError: nested, callbacks: []error{nested}},
		{name: "custom_is_does_not_establish_absence", snapshots: []associationReadSnapshot{{err: isOnly}}, wantError: isOnly, callbacks: []error{isOnly}},
		{name: "excessive_wrapper_depth_fails_closed", snapshots: []associationReadSnapshot{{err: tooDeep}}, wantError: tooDeep, callbacks: []error{tooDeep}},
		{name: "database_failure", snapshots: []associationReadSnapshot{{err: failed}}, wantError: failed, callbacks: []error{failed}},
		{name: "outer_read_failure_after_absence", snapshots: []associationReadSnapshot{absent}, readError: failed, wantError: failed, callbacks: []error{nil}},
		{name: "outer_read_failure_after_present", snapshots: []associationReadSnapshot{present}, readError: failed, wantError: failed, callbacks: []error{nil}},
		{name: "cancellation_after_absence", snapshots: []associationReadSnapshot{absent}, readError: context.Canceled, wantError: context.Canceled, callbacks: []error{nil}},
		{name: "deadline_after_absence", snapshots: []associationReadSnapshot{absent}, readError: context.DeadlineExceeded, wantError: context.DeadlineExceeded, callbacks: []error{nil}},
		{name: "successful_custom_read_cancels_after_absence", snapshots: []associationReadSnapshot{absent}, cancelAfterRead: true, wantError: context.Canceled, callbacks: []error{nil}},
		{name: "successful_custom_read_cancels_after_present", snapshots: []associationReadSnapshot{present}, cancelAfterRead: true, wantError: context.Canceled, callbacks: []error{nil}},
		{name: "real_outer_error_precedes_late_cancellation", snapshots: []associationReadSnapshot{absent}, readError: failed, cancelAfterRead: true, wantError: failed, callbacks: []error{nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &associationReadRepo{snapshots: tc.snapshots, readError: tc.readError}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			if tc.cancelAfterRead {
				repo.cancel = cancel
			}
			provider := &associationAbsentProvider{}
			service, err := NewCheckoutService(repo, revenueTestClock{at}, provider)
			require.NoError(t, err)
			got, err := service.ResolveRevenueAssociation(ctx, request)
			require.Equal(t, tc.want, got)
			if tc.wantError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantError)
			}
			require.Equal(t, tc.callbacks, repo.callbacks)
			require.Equal(t, tc.lookups, provider.calls, "provider lookup must wait for an acknowledged absent read")
		})
	}
}
