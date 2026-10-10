package billinglifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

type checkoutBinderTestRepository struct {
	repo  *schedulerTestRepository
	mode  string
	err   error
	calls int
	after func()
}

func (r *checkoutBinderTestRepository) BindCheckout(_ context.Context, h LeaseHandle, i CheckoutInput) (CheckoutBinding, error) {
	r.calls++
	r.repo.stage = "bind"
	j := r.repo.leased
	j.Revision++
	j.CheckoutPrepared = true
	switch r.mode {
	case "changed_owner":
		j.Source.PrincipalID = "other"
		j.OriginalStatus = nil
	case "lost_marker":
		j.CheckoutPrepared = false
	case "changed_intent":
		c := *j.Source.Checkout
		c.SessionID = "cs_other"
		j.Source.Checkout = &c
	case "changed_lane":
		j.Lane = RefreshLane
	case "changed_revision":
		j.Revision++
	case "changed_epoch":
		j.Fence++
	case "changed_attempts":
		j.Attempts++
	case "changed_token":
		j.LeaseToken = "other-token"
	case "changed_due":
		j.NextAttemptAt = j.NextAttemptAt.Add(time.Second)
	case "changed_creation":
		j.CreatedAt = j.CreatedAt.Add(time.Second)
	case "lost_evidence":
		i.Evidence = nil
	case "different_evidence":
		e := *i.Evidence
		e.CustomerID = "cus_other"
		i.Evidence = &e
	case "different_retained_input":
		i.Intent.SessionID = "cs_other"
	}
	r.repo.leased = j
	if r.after != nil {
		r.after()
	}
	return CheckoutBinding{j, i}, r.err
}

type checkoutBinderTestValidator struct {
	repo      *schedulerTestRepository
	denyStage string
	calls     int
	err       error
}

func (v *checkoutBinderTestValidator) ValidateCheckoutLifecycle(ctx context.Context, actor string, p billing.CheckoutIntent) error {
	v.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.repo.stage == v.denyStage {
		return v.err
	}
	if actor != "worker-original" {
		return partnermanager.ErrDenied
	}
	return p.ValidateAcknowledgedSubscription()
}

func TestCheckoutBinderCurrentServiceBoundaries(t *testing.T) {
	outage := errors.New("controlled binding outage")
	for _, tc := range []struct {
		name   string
		want   error
		writes int
	}{
		{"confirmed_preparation", nil, 1}, {"confirmed_evidence", nil, 1}, {"initial_denied", partnermanager.ErrDenied, 0},
		{"validator_denied", partnermanager.ErrDenied, 0}, {"outage", outage, 1}, {"outage_then_denied", partnermanager.ErrDenied, 1},
		{"unknown_then_denied", recordstore.ErrUncertain, 1}, {"unknown_then_validator_denied", recordstore.ErrUncertain, 1}, {"unknown_then_canceled", recordstore.ErrUncertain, 1},
		{"post_commit_validator_denied", partnermanager.ErrDenied, 1}, {"post_commit_lease_lost", recordstore.ErrConflict, 1},
		{"changed_owner", billing.ErrRevenueUnavailable, 1}, {"lost_marker", billing.ErrRevenueUnavailable, 1}, {"changed_intent", billing.ErrRevenueUnavailable, 1},
		{"changed_lane", billing.ErrRevenueUnavailable, 1}, {"changed_revision", billing.ErrRevenueUnavailable, 1}, {"changed_epoch", billing.ErrRevenueUnavailable, 1}, {"changed_attempts", billing.ErrRevenueUnavailable, 1},
		{"changed_token", billing.ErrRevenueUnavailable, 1}, {"changed_due", billing.ErrRevenueUnavailable, 1}, {"changed_creation", billing.ErrRevenueUnavailable, 1},
		{"lost_evidence", billing.ErrRevenueUnavailable, 1}, {"different_evidence", billing.ErrRevenueUnavailable, 1}, {"different_retained_input", billing.ErrRevenueUnavailable, 1},
		{"wrong_selected_source", billing.ErrRevenueInvalid, 0}, {"evidence_without_marker", billing.ErrRevenueConflict, 0},
		{"private_results", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, a, scope := checkoutBinderServiceFixture(t)
			j := repo.snapshot.Jobs[0]
			j.Revision = 2
			j.Fence = 1
			j.Attempts = 1
			j.LeaseActor = "worker-original"
			j.LeaseToken = "controlled-binding-token"
			j.LeasedUntil = j.CreatedAt.Add(time.Minute)
			repo.leased = j
			input := CheckoutInput{Intent: *j.Source.Checkout}
			if tc.name == "confirmed_preparation" {
				repo.leased.CheckoutPrepared = false
			}
			e := paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, SessionID: input.Intent.SessionID, IntentID: input.Intent.ID, ClientReferenceID: input.Intent.Request.UserID, CustomerID: "cus_original", SubscriptionID: "sub_original", PriceID: input.Intent.Request.PriceID, Currency: "GBP", Mode: input.Intent.Request.Mode, Status: "complete", UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month", CreatedAt: input.Intent.CreatedAt}

			if tc.name == "confirmed_evidence" || tc.name == "lost_evidence" || tc.name == "different_evidence" || tc.name == "evidence_without_marker" {
				input.Evidence = &e
			}
			r := &checkoutBinderTestRepository{repo: repo, mode: tc.name}
			v := &checkoutBinderTestValidator{repo: repo, denyStage: "never", err: partnermanager.ErrDenied}
			b, err := NewCheckoutBinder(s, r, v)
			require.NoError(t, err)
			ctx := context.Background()
			switch tc.name {
			case "initial_denied":
				a.denyStage = "check"
			case "validator_denied":
				v.denyStage = "check"
			case "outage":
				r.err = outage
			case "outage_then_denied":
				r.err = outage
				a.denyStage = "bind"
			case "unknown_then_denied":
				r.err = recordstore.ErrUncertain
				a.denyStage = "bind"
			case "unknown_then_validator_denied":
				r.err = recordstore.ErrUncertain
				v.denyStage = "bind"
			case "unknown_then_canceled":
				c, cancel := context.WithCancel(ctx)
				ctx = c
				r.after = cancel
				r.err = recordstore.ErrUncertain
			case "post_commit_validator_denied":
				v.denyStage = "bind"
			case "post_commit_lease_lost":
				r.after = func() { repo.checkErr = recordstore.ErrConflict }
			case "wrong_selected_source":
				input.Intent.SessionID = "cs_other"
			case "evidence_without_marker":
				repo.leased.CheckoutPrepared = false

			}
			out, err := b.Bind(ctx, JobLease(j), input)
			require.Equal(t, tc.writes, r.calls)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, CheckoutBinding{}, out)
				if tc.name == "unknown_then_denied" || tc.name == "unknown_then_validator_denied" {
					require.ErrorIs(t, err, partnermanager.ErrDenied)
				}
				if tc.name == "unknown_then_canceled" {
					require.ErrorIs(t, err, context.Canceled)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, j.Revision+1, out.Job.Revision)
				require.True(t, out.Job.CheckoutPrepared)
				require.Equal(t, input.Intent, out.Input.Intent)
			}
			if tc.name == "private_results" {
				data, err := json.Marshal(out)
				require.NoError(t, err)
				require.Equal(t, "{}", string(data))
			}
		})
	}
}

func TestCheckoutBinderDependencies(t *testing.T) {
	for _, name := range []string{"nil_scheduler", "nil_repository", "typed_nil_repository", "nil_validator", "typed_nil_validator", "nil_service", "nil_context"} {
		t.Run(name, func(t *testing.T) {
			s, repo, _, _ := checkoutBinderServiceFixture(t)
			r := &checkoutBinderTestRepository{repo: repo}
			v := &checkoutBinderTestValidator{repo: repo, denyStage: "never"}
			scheduler := s
			var repository CheckoutBindingRepository = r
			var validator CheckoutValidator = v
			switch name {
			case "nil_scheduler":
				scheduler = nil
			case "nil_repository":
				repository = nil
			case "typed_nil_repository":
				repository = (*checkoutBinderTestRepository)(nil)
			case "nil_validator":
				validator = nil
			case "typed_nil_validator":
				validator = (*checkoutBinderTestValidator)(nil)
			}
			b, err := NewCheckoutBinder(scheduler, repository, validator)
			switch name {
			case "nil_service":
				b = nil
				_, err = b.Bind(context.Background(), LeaseHandle{}, CheckoutInput{})
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			case "nil_context":
				require.NoError(t, err)
				var nilCtx context.Context
				_, err = b.Bind(nilCtx, LeaseHandle{}, CheckoutInput{})
				require.ErrorIs(t, err, billing.ErrRevenueInvalid)
			default:
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				require.Nil(t, b)
			}
		})
	}
}

// Isolated service fixtures use an actual owning acknowledgement, then controlled
// typed ports/authority. They do not establish Mongo or runtime behaviour.
func checkoutBinderServiceFixture(t *testing.T) (*Scheduler, *schedulerTestRepository, *schedulerTestAuthority, billing.RevenueScope) {
	t.Helper()
	records := &statusOriginalStore{records: map[string]recordstore.Record{}}
	repo, err := revenuestore.NewRepository(records)
	require.NoError(t, err)
	clock := &statusInputClock{time.Unix(1700000000, 123456789).UTC()}
	owner, err := billing.NewCheckoutService(repo, clock, &inputNativeProvider{})
	require.NoError(t, err)
	scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_fixture"}
	q := paymentprovider.CheckoutSessionRequest{IdempotencyKey: "original", PriceID: "price_frozen", PlanID: "plan", CostID: "cost", UserID: "payer", UserReference: "payer", CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: "GBP", ExpectedAmount: 1000, ExpectedBillingCadence: "month", TrialPeriodDays: 14, Metadata: map[string]string{"purpose": "trial"}}
	i, err := owner.PrepareCheckout(t.Context(), scope, q)
	require.NoError(t, err)
	require.NoError(t, owner.AcknowledgeCheckout(t.Context(), i, "cs_original"))
	i, err = owner.FindCheckoutIntent(t.Context(), scope, q.IdempotencyKey)
	require.NoError(t, err)
	source := ScheduledSource{Scope: scope, Kind: billing.LifecycleCheckoutSources, SourceID: billing.LifecycleDiscoverySourceID(scope, billing.LifecycleCheckoutSources, i.ID), PrincipalID: i.Request.UserID, Checkout: &i}
	j := ScheduledJob{Source: source, Revision: 1, Lane: ColdLane, CreatedAt: clock.at, NextAttemptAt: clock.at, CheckoutPrepared: true}
	r := &schedulerTestRepository{snapshot: ScheduleSnapshot{Jobs: []ScheduledJob{j}}}
	a := &schedulerTestAuthority{repo: r, denyStage: "never"}
	s, err := NewScheduler(r, a, clock, schedulerTestConfig(scope))
	require.NoError(t, err)
	return s, r, a, scope
}

type checkoutConservationRepository struct {
	SchedulerRepository
	drop string
}

func (r checkoutConservationRepository) Acquire(ctx context.Context, q LeaseRequest) (ScheduledJob, error) {
	j, err := r.SchedulerRepository.Acquire(ctx, q)
	if r.drop == "acquire" {
		j.CheckoutPrepared = false
	}
	return j, err
}
func (r checkoutConservationRepository) CheckLease(ctx context.Context, h LeaseHandle) (ScheduledJob, error) {
	j, err := r.SchedulerRepository.CheckLease(ctx, h)
	if r.drop == "check" {
		j.CheckoutPrepared = false
	}
	return j, err
}
func (r checkoutConservationRepository) Release(ctx context.Context, q LeaseDisposition) (ScheduledJob, error) {
	j, err := r.SchedulerRepository.Release(ctx, q)
	if r.drop == "release" {
		j.CheckoutPrepared = false
	}
	return j, err
}

func TestCheckoutBindingSchedulerConservation(t *testing.T) {
	for _, name := range []string{"acquire", "check", "release"} {
		t.Run(name, func(t *testing.T) {
			s, repo, _, scope := checkoutBinderServiceFixture(t)
			j := repo.snapshot.Jobs[0]
			j.Fence = 1
			j.Attempts = 1
			repo.snapshot.Jobs[0] = j
			s.repo = checkoutConservationRepository{repo, name}
			if name == "release" {
				j.Revision++
				j.LeaseActor = "worker-original"
				j.LeaseToken = "controlled-current"
				j.LeasedUntil = j.CreatedAt.Add(time.Minute)
				repo.leased = j
				out, err := s.Retry(t.Context(), JobLease(j))
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				require.Equal(t, ScheduledJob{}, out)
			} else {
				out, err := s.Scan(t.Context(), scope, ColdLane)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				require.Equal(t, ScheduleBatch{}, out)
			}
		})
	}
}
