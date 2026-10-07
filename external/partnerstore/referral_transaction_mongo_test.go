package partnerstore

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Named owning-transaction cases cover rollback, bound guard scope and retry.
func TestMongoReferralTransactionBoundary(t *testing.T) {
	cases := []struct {
		name string
		mode string
		want error
	}{
		{name: "callback_failure_rolls_back_revision_and_head", mode: "failure", want: injectedFailure},
		{name: "bound_callback_cannot_write_another_customer", mode: "cross_customer", want: referral.ErrInvalid},
		{name: "nested_owning_transaction_is_denied", mode: "nested", want: referral.ErrInvalid},
		{name: "owning_revision_and_head_commit_together"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			r, err := NewReferralRepository(store)
			require.NoError(t, err)
			row := referral.Referral{ID: "revision", Revision: 1, ProgramID: referral.ProgramID, PartnerID: "partner", ReferredCustomer: "customer", LockedAt: clock.Now(), PostedAt: clock.Now(), TermsSnapshot: frozenTerms()}
			err = r.WithAttributionTransaction(ctx, referral.ProgramID, "customer", func(tx referral.Repository) error {
				if tc.mode == "cross_customer" {
					row.ReferredCustomer = "other"
				}
				if tc.mode == "nested" {
					return tx.WithAttributionTransaction(ctx, referral.ProgramID, "customer", func(referral.Repository) error { t.Fatal("nested callback must not run"); return nil })
				}
				if err := tx.InsertReferral(ctx, row, 0); err != nil {
					return err
				}
				if tc.mode == "failure" {
					return injectedFailure
				}
				return nil
			})
			require.ErrorIs(t, err, tc.want)
			for _, customer := range []string{"customer", "other"} {
				got, err := r.GetReferralByCustomer(ctx, referral.ProgramID, customer)
				if tc.want == nil && customer == "customer" {
					require.NoError(t, err)
					require.Equal(t, row, got)
				} else {
					require.ErrorIs(t, err, referral.ErrNotFound)
				}
				history, err := r.ListReferralHistory(ctx, referral.ProgramID, customer)
				require.NoError(t, err)
				if tc.want == nil && customer == "customer" {
					require.Equal(t, []referral.Referral{row}, history)
				} else {
					require.Empty(t, history)
				}
			}
		})
	}
}

type attributionSignalStore struct {
	recordstore.Store
	attempted, entered chan struct{}
	release            <-chan struct{}
}

func (s attributionSignalStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	if key == referralPartition("customer") && s.attempted != nil {
		select {
		case s.attempted <- struct{}{}:
		default:
		}
	}
	return s.Store.Transact(ctx, key, func(tx recordstore.Tx) error {
		if key == referralPartition("customer") {
			if s.entered != nil {
				select {
				case s.entered <- struct{}{}:
				default:
				}
			}
			if s.release != nil {
				select {
				case <-s.release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		return fn(tx)
	})
}

// One gated overlap proves transaction linearization: a binding attempted
// while a correction owns the customer guard must select the committed head,
// rather than carry an earlier outside-transaction history read into its write.
func TestMongoReferralCorrectionAndBindingShareGuard(t *testing.T) {
	_, _, store, _, clock, ctx := mongoEarnings(t)
	r, err := NewReferralRepository(store)
	require.NoError(t, err)
	s, err := referral.NewService(r, clock, randomIDs{}, 30*24*time.Hour)
	require.NoError(t, err)
	first, err := s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "original", CustomerID: "original-owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "audited initial assignment", Terms: frozenTerms()}))
	require.NoError(t, err)
	clock.now = clock.now.Add(time.Hour) // Freeze this clock before concurrent calls.
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	attempted := make(chan struct{}, 1)
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	cr, err := NewReferralRepository(attributionSignalStore{Store: store, entered: entered, release: release})
	require.NoError(t, err)
	cs, err := referral.NewService(cr, clock, randomIDs{}, 30*24*time.Hour)
	require.NoError(t, err)
	br, err := NewReferralRepository(attributionSignalStore{Store: store, attempted: attempted})
	require.NoError(t, err)
	bs, err := referral.NewService(br, clock, randomIDs{}, 30*24*time.Hour)
	require.NoError(t, err)
	correctionRequest := reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "new", CustomerID: "new-owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "prospective correction", ExpectedRevision: first.Revision, ExpectedReferralID: first.ID, Terms: frozenTerms()})
	correction := make(chan error, 1)
	go func() {
		_, err := cs.AssignAttribution(ctx, correctionRequest)
		correction <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	type result struct {
		binding referral.PaymentAttribution
		err     error
	}
	bound := make(chan result, 1)
	go func() {
		b, err := bs.BindPayment(ctx, "customer", "payment-at-cutover", clock.Now())
		bound <- result{b, err}
	}()
	select {
	case <-attempted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(release)
	released = true
	select {
	case err := <-correction:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case got := <-bound:
		require.NoError(t, got.err)
		require.Equal(t, "new", got.binding.PartnerID)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestMongoReferralBindingTransactionRecovery(t *testing.T) {
	cases := []struct {
		name      string
		failAt    int
		uncertain bool
		want      error
	}{
		{name: "binding_write_failure_keeps_no_receipt", failAt: 1, want: injectedFailure},
		{name: "committed_binding_lost_reply_replays_original", uncertain: true, want: referral.ErrUncertain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			r, err := NewReferralRepository(store)
			require.NoError(t, err)
			s, err := referral.NewService(r, clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			_, err = s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "audited assignment", Terms: frozenTerms()}))
			require.NoError(t, err)
			broken, err := NewReferralRepository(injectedStore{Store: store, failAt: tc.failAt, uncertain: tc.uncertain})
			require.NoError(t, err)
			bs, err := referral.NewService(broken, clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			partial, err := bs.BindPayment(ctx, "customer", "payment", clock.Now())
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, partial, "unconfirmed result is not success")
			old, err := r.GetPaymentAttribution(ctx, referral.ProgramID, "payment")
			if tc.uncertain {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, referral.ErrNotFound)
			}
			got, err := s.BindPayment(ctx, "customer", "payment", clock.Now())
			require.NoError(t, err)
			if tc.uncertain {
				require.Equal(t, old, got)
			}
			_, err = s.BindPayment(ctx, "different-customer", "payment", clock.Now())
			require.ErrorIs(t, err, referral.ErrStaleWrite)
		})
	}
}

func TestMongoReferralTransactionContext(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "nil_context"
		if cancelled {
			name = "cancelled_context"
		}
		t.Run(name, func(t *testing.T) {
			_, _, store, _, _, _ := mongoEarnings(t)
			r, err := NewReferralRepository(store)
			require.NoError(t, err)
			var ctx context.Context
			want := referral.ErrInvalid
			if cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
				want = context.Canceled
			}
			err = r.WithAttributionTransaction(ctx, referral.ProgramID, "customer", func(referral.Repository) error { t.Fatal("invalid context must not reach callback"); return nil })
			require.ErrorIs(t, err, want)
		})
	}
}
