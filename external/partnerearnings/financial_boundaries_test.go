package partnerearnings

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: new named negative financial-evidence cases establish
// payload integrity and dependency-vs-absence behavior before mutation.
type financialFaultRepo struct {
	Repository
	source, receipt bool
}

func (r *financialFaultRepo) WithTransaction(ctx context.Context, p, partner, currency string, fn func(Repository) error) error {
	return r.Repository.WithTransaction(ctx, p, partner, currency, func(tx Repository) error {
		return fn(&financialFaultRepo{Repository: tx, source: r.source, receipt: r.receipt})
	})
}
func (r *financialFaultRepo) EntryBySource(ctx context.Context, p, partner, kind, id string) (Entry, error) {
	if r.source {
		return Entry{}, errors.Join(ErrNotFound, ErrUnavailable)
	}
	return r.Repository.EntryBySource(ctx, p, partner, kind, id)
}
func (r *financialFaultRepo) GetReceipt(ctx context.Context, key ReceiptKey) (Receipt, error) {
	if r.receipt {
		return Receipt{}, errors.Join(ErrNotFound, ErrUnavailable)
	}
	return r.Repository.GetReceipt(ctx, key)
}
func TestFinancialReadOutageCannotCreateMoney(t *testing.T) {
	type testCase struct{ name, action string }
	cases := []testCase{{name: "joined_source_absence_does_not_admit_accrual", action: "accrual"}, {name: "joined_receipt_absence_does_not_create_claim", action: "claim"}, {name: "joined_receipt_absence_does_not_settle_payment", action: "payment"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, clock := newTestService(t)
			ctx := context.Background()
			request := AccrualRequest{PartnerID: "partner", PaymentID: "payment", PaymentMinor: 10000, RateBasisPoints: 10000, Currency: testCurrency, OccurredAt: clock.Now()}
			var claim Claim
			if tc.action != "accrual" {
				_, err := svc.Accrue(ctx, request)
				require.NoError(t, err)
			}
			if tc.action == "payment" {
				var err error
				claim, err = svc.RequestClaim(ctx, ClaimRequest{ActorID: "customer", PartnerID: "partner", AmountMinor: 1000, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "claim"})
				require.NoError(t, err)
				claim, err = svc.DecideClaim(ctx, ClaimDecision{ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "operator", ExpectedRevision: claim.Revision})
				require.NoError(t, err)
			}
			before := len(repo.entries)
			svc.repo = &financialFaultRepo{Repository: repo, source: tc.action == "accrual", receipt: tc.action != "accrual"}
			var err error
			switch tc.action {
			case "accrual":
				_, err = svc.Accrue(ctx, request)
			case "claim":
				_, err = svc.RequestClaim(ctx, ClaimRequest{ActorID: "customer", PartnerID: "partner", AmountMinor: 1000, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "claim"})
			case "payment":
				_, err = svc.RecordPayment(ctx, RecordPaymentRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, IdempotencyKey: "record", Method: "paypal", Reference: "verified-reference", PaidAt: clock.Now(), AmountMinor: claim.AmountMinor, Currency: testCurrency, State: PaymentStateFull})
			}
			require.ErrorIs(t, err, ErrUnavailable)
			require.Len(t, repo.entries, before)
		})
	}
}
func TestFinancialReceiptPayloadAndAssignment(t *testing.T) {
	type testCase struct {
		name, action string
		want         error
	}
	cases := []testCase{{name: "changed_destination_snapshot_under_same_key_conflicts", action: "destination", want: ErrConflict}, {name: "another_operator_cannot_record_assigned_transfer", action: "assignment", want: ErrConflict}, {name: "payment_revision_is_bound_to_receipt_payload", action: "payment_revision", want: ErrConflict}, {name: "amendment_revision_is_bound_to_receipt_payload", action: "amend_revision", want: ErrConflict}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, clock := newTestService(t)
			ctx := context.Background()
			_, err := svc.Accrue(ctx, AccrualRequest{PartnerID: "partner", PaymentID: "payment", PaymentMinor: 10000, RateBasisPoints: 10000, Currency: testCurrency, OccurredAt: clock.Now()})
			require.NoError(t, err)
			claimReq := ClaimRequest{ActorID: "customer", PartnerID: "partner", AmountMinor: 1000, Currency: testCurrency, DestinationID: "destination", IdempotencyKey: "claim", DestinationSnapshot: map[string]string{"a": "x;b=y"}}
			claim, err := svc.RequestClaim(ctx, claimReq)
			require.NoError(t, err)
			if tc.action == "destination" {
				before := len(repo.entries)
				claimReq.DestinationSnapshot = map[string]string{"a": "x", "b": "y"}
				_, err = svc.RequestClaim(ctx, claimReq)
				require.ErrorIs(t, err, tc.want)
				require.Len(t, repo.entries, before)
				return
			}
			claim, err = svc.DecideClaim(ctx, ClaimDecision{ClaimID: claim.ID, NewState: ClaimProcessing, ActorID: "operator", ExpectedRevision: claim.Revision})
			require.NoError(t, err)
			pay := RecordPaymentRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, IdempotencyKey: "record", Method: "paypal", Reference: "verified-reference", PaidAt: clock.Now(), AmountMinor: claim.AmountMinor, Currency: testCurrency, State: PaymentStateFull}
			before := len(repo.entries)
			if tc.action == "assignment" {
				pay.ActorID = "other-operator"
				_, err = svc.RecordPayment(ctx, pay)
				require.ErrorIs(t, err, tc.want)
				require.Len(t, repo.entries, before)
				return
			}
			paid, err := svc.RecordPayment(ctx, pay)
			require.NoError(t, err)
			if tc.action == "payment_revision" {
				before = len(repo.entries)
				pay.ExpectedRevision++
				_, err = svc.RecordPayment(ctx, pay)
				require.ErrorIs(t, err, tc.want)
				require.Len(t, repo.entries, before)
				return
			}
			amend := AmendPaymentRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: paid.Revision, IdempotencyKey: "amend", Method: "paypal", Reference: "corrected-reference", PaidAt: clock.Now(), Reason: "verified_receipt_correction"}
			_, err = svc.AmendPayment(ctx, amend)
			require.NoError(t, err)
			before = len(repo.entries)
			amend.ExpectedRevision++
			_, err = svc.AmendPayment(ctx, amend)
			require.ErrorIs(t, err, tc.want)
			require.Len(t, repo.entries, before)
		})
	}
}

func TestFinancialAggregateOverflowRollsBack(t *testing.T) {
	type testCase struct {
		name    string
		hold    time.Duration
		pending bool
	}
	cases := []testCase{{name: "matured_aggregate_cannot_wrap"}, {name: "pending_aggregate_cannot_wrap", hold: 24 * time.Hour, pending: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, clock := newTestService(t)
			ctx := context.Background()
			first := AccrualRequest{PartnerID: "partner", PaymentID: "first", PaymentMinor: math.MaxInt64, RateBasisPoints: 10000, HoldDuration: tc.hold, Currency: testCurrency, OccurredAt: clock.Now()}
			_, err := svc.Accrue(ctx, first)
			require.NoError(t, err)
			before := append([]Entry(nil), repo.entries...)
			second := first
			second.PaymentID = "second"
			second.PaymentMinor = 1
			_, err = svc.Accrue(ctx, second)
			require.ErrorIs(t, err, ErrInvalid)
			require.Equal(t, before, repo.entries)
			balance, err := svc.Balances(ctx, "partner")
			require.NoError(t, err)
			if tc.pending {
				require.EqualValues(t, math.MaxInt64, balance.PendingMinor)
			} else {
				require.EqualValues(t, math.MaxInt64, balance.MatchedMinor)
			}
		})
	}
}
func TestFinancialHoldBounds(t *testing.T) {
	type testCase struct {
		name string
		hold time.Duration
		want error
	}
	cases := []testCase{{name: "approved_zero_hold"}, {name: "maximum_28_days", hold: 28 * 24 * time.Hour}, {name: "beyond_28_days", hold: 28*24*time.Hour + time.Nanosecond, want: ErrInvalid}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, clock := newTestService(t)
			_, err := svc.Accrue(context.Background(), AccrualRequest{PartnerID: "partner", PaymentID: "payment", PaymentMinor: 10000, RateBasisPoints: 2000, HoldDuration: tc.hold, Currency: testCurrency, OccurredAt: clock.Now()})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, repo.entries)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
