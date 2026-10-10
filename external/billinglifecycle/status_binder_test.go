package billinglifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

type statusBinderTestRepository struct {
	repo  *schedulerTestRepository
	mode  string
	err   error
	calls int
	after func()
}

func (r *statusBinderTestRepository) BindStatus(_ context.Context, h LeaseHandle, i StatusInput) (StatusBinding, error) {
	r.calls++
	r.repo.stage = "bind"
	j := r.repo.leased
	j.Revision++
	p := i.Preparation
	j.OriginalStatus = &p
	switch r.mode {
	case "changed_owner":
		j.Source.PrincipalID = "other"
		j.OriginalStatus = nil
	case "lost_original":
		j.OriginalStatus = nil
	case "changed_original":
		p.ActorID = "other-author"
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
		e.Status = "active"
		i.Evidence = &e
	case "different_retained_input":
		i.Preparation.ActorID = "other-author"
	}
	r.repo.leased = j
	if r.after != nil {
		r.after()
	}
	return StatusBinding{j, i}, r.err
}

type statusBinderTestValidator struct {
	repo      *schedulerTestRepository
	denyStage string
	calls     int
	err       error
}

func (v *statusBinderTestValidator) ValidateSubscriptionStatusPreparation(ctx context.Context, actor string, p billing.SubscriptionStatusPreparation) error {
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
	return p.Validate()
}

func TestStatusBinderCurrentServiceBoundaries(t *testing.T) {
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
		{"changed_owner", billing.ErrRevenueUnavailable, 1}, {"lost_original", billing.ErrRevenueUnavailable, 1}, {"changed_original", billing.ErrRevenueUnavailable, 1},
		{"changed_revision", billing.ErrRevenueUnavailable, 1}, {"changed_epoch", billing.ErrRevenueUnavailable, 1}, {"changed_attempts", billing.ErrRevenueUnavailable, 1},
		{"changed_token", billing.ErrRevenueUnavailable, 1}, {"changed_due", billing.ErrRevenueUnavailable, 1}, {"changed_creation", billing.ErrRevenueUnavailable, 1},
		{"lost_evidence", billing.ErrRevenueUnavailable, 1}, {"different_evidence", billing.ErrRevenueUnavailable, 1}, {"different_retained_input", billing.ErrRevenueUnavailable, 1},
		{"wrong_selected_source", billing.ErrRevenueInvalid, 0}, {"evidence_without_pointer", billing.ErrRevenueConflict, 0}, {"different_unresolved_original", billing.ErrRevenueConflict, 0},
		{"private_results", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, a, scope := schedulerServiceFixture(t)
			j := repo.snapshot.Jobs[0]
			j.Revision = 2
			j.Fence = 1
			j.Attempts = 1
			j.LeaseActor = "worker-original"
			j.LeaseToken = "controlled-binding-token"
			j.LeasedUntil = j.CreatedAt.Add(time.Minute)
			repo.leased = j
			input := StatusInput{Preparation: *j.OriginalStatus}
			if tc.name == "confirmed_preparation" {
				repo.leased.OriginalStatus = nil
			}
			e := billing.VerifiedSubscriptionStatusEvidence{Scope: scope, SubscriptionID: input.Preparation.SubscriptionID, ProviderCustomerID: input.Preparation.ProviderCustomerID, Status: "trialing"}
			if tc.name == "confirmed_evidence" || tc.name == "lost_evidence" || tc.name == "different_evidence" || tc.name == "evidence_without_pointer" {
				input.Evidence = &e
			}
			r := &statusBinderTestRepository{repo: repo, mode: tc.name}
			v := &statusBinderTestValidator{repo: repo, denyStage: "never", err: partnermanager.ErrDenied}
			b, err := NewStatusBinder(s, r, v)
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
				input.Preparation.PrincipalID = "other"
			case "evidence_without_pointer":
				repo.leased.OriginalStatus = nil
			case "different_unresolved_original":
				f := originalStatusFixture(t, "checkout")
				input.Preparation, err = f.owner.PrepareSubscriptionStatusForCheckout(ctx, "later-author", input.Preparation.Scope, input.Preparation.SubscriptionID)
				require.NoError(t, err)
			}
			out, err := b.Bind(ctx, JobLease(j), input)
			require.Equal(t, tc.writes, r.calls)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, StatusBinding{}, out)
				if tc.name == "unknown_then_denied" || tc.name == "unknown_then_validator_denied" {
					require.ErrorIs(t, err, partnermanager.ErrDenied)
				}
				if tc.name == "unknown_then_canceled" {
					require.ErrorIs(t, err, context.Canceled)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, j.Revision+1, out.Job.Revision)
				require.Equal(t, input.Preparation, *out.Job.OriginalStatus)
			}
			if tc.name == "private_results" {
				data, err := json.Marshal(out)
				require.NoError(t, err)
				require.Equal(t, "{}", string(data))
			}
		})
	}
}

func TestStatusBinderDependencies(t *testing.T) {
	for _, name := range []string{"nil_scheduler", "nil_repository", "typed_nil_repository", "nil_validator", "typed_nil_validator", "nil_service", "nil_context"} {
		t.Run(name, func(t *testing.T) {
			s, repo, _, _ := schedulerServiceFixture(t)
			r := &statusBinderTestRepository{repo: repo}
			v := &statusBinderTestValidator{repo: repo, denyStage: "never"}
			scheduler := s
			var repository StatusBindingRepository = r
			var validator StatusValidator = v
			switch name {
			case "nil_scheduler":
				scheduler = nil
			case "nil_repository":
				repository = nil
			case "typed_nil_repository":
				repository = (*statusBinderTestRepository)(nil)
			case "nil_validator":
				validator = nil
			case "typed_nil_validator":
				validator = (*statusBinderTestValidator)(nil)
			}
			b, err := NewStatusBinder(scheduler, repository, validator)
			switch name {
			case "nil_service":
				b = nil
				_, err = b.Bind(context.Background(), LeaseHandle{}, StatusInput{})
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			case "nil_context":
				require.NoError(t, err)
				var nilCtx context.Context
				_, err = b.Bind(nilCtx, LeaseHandle{}, StatusInput{})
				require.ErrorIs(t, err, billing.ErrRevenueInvalid)
			default:
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				require.Nil(t, b)
			}
		})
	}
}
