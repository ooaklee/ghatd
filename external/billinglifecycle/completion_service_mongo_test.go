package billinglifecycle

import (
	"context"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Faults surround the actual encrypted completion transaction, after native
// receipt confirmation. They do not replace the completion repository logic.
type completionReplyBoundary struct {
	CompletionRepository
	store *inputLostReplyStore
	lose  bool
	after func()
}

func (r completionReplyBoundary) CompleteCheckout(ctx context.Context, o CheckoutObservation) (CompletedExecution, error) {
	r.store.lose = r.lose
	out, err := r.CompletionRepository.CompleteCheckout(ctx, o)
	r.store.lose = false
	if r.after != nil {
		r.after()
	}
	return out, err
}
func (r completionReplyBoundary) CompleteStatus(ctx context.Context, o StatusCompletion) (CompletedExecution, error) {
	r.store.lose = r.lose
	out, err := r.CompletionRepository.CompleteStatus(ctx, o)
	r.store.lose = false
	if r.after != nil {
		r.after()
	}
	return out, err
}

func TestCompletionServiceCurrentAuthorityAndRecovery(t *testing.T) {
	for _, kind := range []string{"checkout", "status"} {
		for _, tc := range []struct {
			name       string
			lose, deny bool
			want       error
		}{
			{"acknowledged", false, false, nil},
			{"lost_reply_inspect_without_lookup", true, false, recordstore.ErrUncertain},
			{"known_commit_then_denied", false, true, partnermanager.ErrDenied},
			{"unknown_commit_then_denied_preserves_uncertainty", true, true, recordstore.ErrUncertain},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				f := nativeCompletionFixture(t, kind)
				boundary := completionReplyBoundary{CompletionRepository: f.repo, store: f.base.reply, lose: tc.lose}
				if tc.deny {
					boundary.after = func() { f.base.revoke(t) }
				}
				var complete func() (CompletedExecution, error)
				var inspect func() (CompletedExecution, error)
				if kind == "checkout" {
					f.checkoutService.repo = boundary
					complete = func() (CompletedExecution, error) { return f.checkoutService.Complete(f.ctx, f.checkout) }
					inspect = func() (CompletedExecution, error) { return f.checkoutService.InspectLast(f.ctx, f.job().Source) }
				} else {
					f.statusService.repo = boundary
					complete = func() (CompletedExecution, error) { return f.statusService.Complete(f.ctx, f.observation) }
					inspect = func() (CompletedExecution, error) { return f.statusService.InspectLast(f.ctx, f.job().Source) }
				}
				before := f.base.provider.calls
				if f.status != nil {
					before = f.status.provider.calls
				}
				out, err := complete()
				if tc.want != nil {
					require.ErrorIs(t, err, tc.want)
					require.Equal(t, CompletedExecution{}, out)
				} else {
					require.NoError(t, err)
				}
				if tc.deny && !tc.lose {
					require.NotErrorIs(t, err, recordstore.ErrUncertain)
				}
				actual, e := f.repo.FindLastCompletion(context.WithoutCancel(f.ctx), f.job().Source)
				require.NoError(t, e)
				require.Empty(t, actual.Job.LeaseActor)
				recovered, e := inspect()
				if tc.deny {
					require.ErrorIs(t, e, partnermanager.ErrDenied)
					require.Equal(t, CompletedExecution{}, recovered)
				} else {
					require.NoError(t, e)
					require.True(t, sameCompletion(actual, recovered))
				}
				if f.status != nil {
					require.Equal(t, before, f.status.provider.calls)
				} else {
					require.Equal(t, before, f.base.provider.calls)
				}
			})
		}
	}
}

func TestStatusCompletionRecurringCycles(t *testing.T) {
	for _, retry := range []bool{false, true} {
		name := "next_cycle"
		if retry {
			name = "retry_then_next_cycle"
		}
		t.Run(name, func(t *testing.T) {
			f := nativeCompletionFixture(t, "status")
			first, err := f.statusService.Complete(f.ctx, f.observation)
			require.NoError(t, err)
			anchor := first.Job.CadenceAnchor
			// Miss the next slot. A new cycle must retain the first cadence anchor,
			// while preparing a genuinely new native capture under the current owner.
			f.clock.set(anchor.Add(65 * time.Second))
			nativeRepo, err := revenuestore.NewRepository(f.base.reply)
			require.NoError(t, err)
			f.status.owner, err = billing.NewRevenueService(nativeRepo, inputNativeClock{f.clock.at})
			require.NoError(t, err)
			b, ctx := nativeStatusBinder(t, f.status, f.repo, f.clock, "worker-original")
			box, err := NewStatusOutbox(f.base.reply, b.validator)
			require.NoError(t, err)
			x, err := NewStatusExecution(b.scheduler, f.repo, box)
			require.NoError(t, err)
			service, err := NewStatusCompletion(x, f.repo, 30*time.Second)
			require.NoError(t, err)
			current, err := f.repo.Acquire(ctx, LeaseRequest{Source: first.Job.Source, ExpectedRevision: first.Job.Revision, Actor: "worker-original", Token: "next-cycle", Until: f.clock.at.Add(time.Minute)})
			require.NoError(t, err)
			require.EqualValues(t, 1, current.Attempts)
			require.Equal(t, first.Job.Fence+1, current.Fence)
			require.Equal(t, anchor, current.CadenceAnchor)
			require.Equal(t, first.Job.LastCompletionID, current.LastCompletionID)
			if retry {
				released, e := f.repo.Release(ctx, LeaseDisposition{Handle: JobLease(current), NextAttemptAt: f.clock.at.Add(time.Second), Lane: RefreshLane})
				require.NoError(t, e)
				f.clock.set(f.clock.at.Add(time.Second))
				current, e = f.repo.Acquire(ctx, LeaseRequest{Source: released.Source, ExpectedRevision: released.Revision, Actor: "worker-original", Token: "retried-cycle", Until: f.clock.at.Add(time.Minute)})
				require.NoError(t, e)
				require.EqualValues(t, 2, current.Attempts)
			}
			observation, err := x.Observe(ctx, JobLease(current))
			require.NoError(t, err)
			require.NotEqual(t, first.Receipt.NativeCaptureID, observation.Input.Preparation.CaptureID)
			// Native now has a newer head; last confirmed host history still points
			// at the old receipt. Inspection must replay it without another GET.
			gets := f.status.provider.calls
			historical, err := service.InspectLast(ctx, first.Job.Source)
			require.NoError(t, err)
			require.True(t, sameCompletion(first, historical))
			require.Equal(t, gets, f.status.provider.calls)
			second, err := service.Complete(ctx, observation)
			require.NoError(t, err)
			require.Zero(t, second.Job.Attempts)
			require.Equal(t, anchor, second.Job.CadenceAnchor)
			require.True(t, second.Job.NextAttemptAt.Equal(anchor.Add(90*time.Second)))
			require.Equal(t, observation.Job.Fence, second.Job.Fence)
			require.NotEqual(t, first.Job.LastCompletionID, second.Job.LastCompletionID)
			old, err := f.repo.FindCompletion(ctx, CompletionKey{first.Job.Source, first.Receipt.NativeCaptureID})
			require.NoError(t, err)
			require.True(t, sameCompletion(first, old))
			latest, err := service.InspectLast(ctx, first.Job.Source)
			require.NoError(t, err)
			require.True(t, sameCompletion(second, latest))
			require.Equal(t, gets, f.status.provider.calls)
		})
	}
}
