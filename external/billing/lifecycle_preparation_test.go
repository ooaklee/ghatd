package billing

import (
	"context"
	"errors"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type preparationTestTx struct {
	LifecyclePreparationTx
	state                        LifecyclePreparationState
	stateErr, sourceErr, saveErr error
	epoch                        int64
	rows                         []LifecyclePreparationRow
	saves, finishes, retains     int
	cancel                       context.CancelFunc
}

func (t *preparationTestTx) State(context.Context) (LifecyclePreparationState, error) {
	return t.state, t.stateErr
}
func (t *preparationTestTx) Epoch(context.Context) (int64, error) { return t.epoch, nil }
func (t *preparationTestTx) Sources(context.Context, int, string, int) ([]LifecyclePreparationRow, error) {
	return t.rows, t.sourceErr
}
func (t *preparationTestTx) Retain(context.Context, LifecycleDiscoveryCandidate) error {
	t.retains++
	return nil
}
func (t *preparationTestTx) Save(_ context.Context, v LifecyclePreparationState, _ int64) error {
	t.saves++
	t.state = v
	if t.cancel != nil {
		t.cancel()
	}
	return t.saveErr
}
func (t *preparationTestTx) Complete(_ context.Context, v LifecyclePreparationState, _ int64) error {
	t.finishes++
	t.state = v
	return t.saveErr
}

type preparationTestRepo struct {
	RevenueRepository
	tx         *preparationTestTx
	late       error
	noCallback bool
	calls      int
}

func (r *preparationTestRepo) WithLifecyclePreparation(ctx context.Context, _ RevenueScope, fn func(LifecyclePreparationTx) error) error {
	r.calls++
	if r.noCallback {
		return nil
	}
	if err := fn(r.tx); err != nil {
		return err
	}
	return r.late
}

type preparationTestClock struct{ at time.Time }

func (c preparationTestClock) Now() time.Time { return c.at }

func TestLifecyclePreparationServiceBoundaries(t *testing.T) {
	outage := errors.New("native preparation reply lost")
	for _, tc := range []struct {
		name, change string
		want         error
	}{
		{name: "fresh_empty_first_phase"},
		{name: "one_bounded_page_keeps_phase", change: "full"},
		{name: "final_phase_requires_epoch_completion", change: "finish"},
		{name: "already_prepared_is_read_only", change: "prepared"},
		{name: "changed_epoch_restarts_without_marker", change: "restart"},
		{name: "regressed_epoch_is_corruption", change: "epoch-regressed", want: ErrRevenueUnavailable},
		{name: "revision_overflow_withholds", change: "overflow", want: ErrRevenueUnavailable},
		{name: "malformed_progress_rejected", change: "bad-state", want: ErrRevenueUnavailable},
		{name: "joined_absence_outage_does_not_start", change: "joined", want: outage},
		{name: "late_failure_withholds_result", change: "late", want: outage},
		{name: "missing_optional_port", change: "legacy", want: ErrRevenueUnavailable},
		{name: "typed_nil_owner", change: "nil-owner", want: ErrRevenueUnavailable},
		{name: "nil_context", change: "nil-context", want: ErrRevenueInvalid},
		{name: "cancel_before", change: "cancel-before", want: context.Canceled},
		{name: "cancel_after_save_withholds_result", change: "cancel-after", want: context.Canceled},
		{name: "callback_not_executed", change: "no-callback", want: ErrRevenueUnavailable},
		{name: "oversized_page_rejected", change: "oversized", want: ErrRevenueUnavailable},
		{name: "unordered_rows_rejected", change: "unordered", want: ErrRevenueUnavailable},
		{name: "invalid_scope_before_io", change: "scope", want: ErrRevenueInvalid},
		{name: "invalid_limit_before_io", change: "limit", want: ErrRevenueInvalid},
		{name: "zero_completion_clock_rejected", change: "clock", want: ErrRevenueUnavailable},
		{name: "legacy_customerless_payment_counted", change: "customerless"},
		{name: "malformed_legacy_payment_rejected", change: "bad-customerless", want: ErrRevenueUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, fact := statusFixture(t)
			scope := fact.Scope
			tx := &preparationTestTx{stateErr: ErrRevenueNotFound}
			repo := &preparationTestRepo{tx: tx}
			service := &RevenueService{repo: repo, clock: preparationTestClock{time.Unix(1700000000, 0).UTC()}}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			limit := 2
			originals := []LifecyclePreparationRow{}
			for _, key := range []string{"first-page-row", "second-page-row"} {
				request := paymentprovider.CheckoutSessionRequest{IdempotencyKey: key, PriceID: "price", PlanID: fact.PlanID, CostID: fact.CostID, UserID: fact.PrincipalID, UserReference: fact.PrincipalID, CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: "payment", ExpectedCurrency: fact.Currency, ExpectedAmount: 1000, ExpectedBillingCadence: "one_time"}
				id := checkoutIntentID(scope, key)
				request.Metadata = map[string]string{"checkout_intent_id": id}
				i := CheckoutIntent{ID: id, Scope: scope, Request: request, CreatedAt: fact.AcceptedAt, SessionID: "cs_original", Fingerprint: checkoutRequestFingerprint(scope, request)}
				originals = append(originals, LifecyclePreparationRow{ID: id, OriginalIntent: &i})
			}
			sort.Slice(originals, func(i, j int) bool { return originals[i].ID < originals[j].ID })
			switch tc.change {
			case "full":
				tx.rows = originals
			case "finish", "clock", "prepared":
				tx.stateErr = nil
				tx.state = LifecyclePreparationState{Scope: scope, Revision: 3, Sweeps: 1, Phase: 5}
				if tc.change == "clock" {
					service.clock = preparationTestClock{}
				}
				if tc.change == "prepared" {
					tx.state.Phase = LifecyclePreparationComplete
					tx.state.PreparedAt = time.Unix(1, 0)
				}
			case "restart", "epoch-regressed", "overflow", "bad-state":
				tx.stateErr = nil
				tx.state = LifecyclePreparationState{Scope: scope, Revision: 2, Epoch: 1, Sweeps: 1, Phase: 1}
				tx.epoch = 2
				if tc.change == "epoch-regressed" {
					tx.epoch = 0
				}
				if tc.change == "overflow" {
					tx.state.Revision = math.MaxInt64
				}
				if tc.change == "bad-state" {
					tx.state.Phase = 7
				}
			case "joined":
				tx.stateErr = errors.Join(ErrRevenueNotFound, outage)
			case "late":
				repo.late = outage
			case "legacy":
				service.repo = &discoveryReadRepo{}
			case "nil-owner":
				var n *preparationTestRepo
				service.repo = n
			case "nil-context":
				ctx = nil
			case "cancel-before":
				cancel()
			case "cancel-after":
				tx.cancel = cancel
			case "no-callback":
				repo.noCallback = true
			case "oversized":
				tx.rows = make([]LifecyclePreparationRow, 3)
			case "unordered":
				tx.rows = []LifecyclePreparationRow{originals[1], originals[0]}
			case "scope":
				scope.AccountID = ""
			case "limit":
				limit = 201
			case "customerless", "bad-customerless":
				// Re-canonicalize after removing customer evidence: legacy fingerprint is
				// its own original shape, not a payment with a field merely erased.
				fact.ProviderCustomerID = ""
				seq, at := fact.Sequence, fact.AcceptedAt
				var err error
				fact, err = canonicalRevenueFact(fact)
				require.NoError(t, err)
				fact.Sequence, fact.AcceptedAt = seq, at
				if tc.change == "bad-customerless" {
					fact.PaidMinor++
				}
				tx.stateErr = nil
				tx.state = LifecyclePreparationState{Scope: scope, Revision: 1, Sweeps: 1, Phase: 1}
				tx.rows = []LifecyclePreparationRow{{ID: fact.ID, OriginalFact: &fact}}
			}
			result, err := service.PrepareLifecycleDiscovery(ctx, scope, limit)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, result)
				return
			}
			require.Equal(t, scope, result.State.Scope)
			switch tc.change {
			case "restart":
				require.True(t, result.Restarted)
				require.Zero(t, result.State.Phase)
				require.Equal(t, int64(2), result.State.Sweeps)
				require.Zero(t, tx.finishes)
			case "finish":
				require.Equal(t, LifecyclePreparationComplete, result.State.Phase)
				require.Equal(t, 1, tx.finishes)
				require.Equal(t, int64(1), result.State.Epoch)
			case "prepared":
				require.Zero(t, tx.saves)
				require.Zero(t, tx.finishes)
			case "full":
				require.Zero(t, result.State.Phase)
				require.Equal(t, originals[1].ID, result.State.AfterID)
			case "customerless":
				require.Equal(t, int64(1), result.State.CustomerlessPayments)
				require.Zero(t, tx.retains)
			default:
				require.Equal(t, 1, result.State.Phase)
			}
		})
	}
}
