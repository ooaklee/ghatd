package billinglifecycle

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

var _ SchedulerRepository = (*RecordExecutionRepository)(nil)
var _ ExecutionAuthority = (*partneraccess.WorkerAuthority)(nil)

func schedulerTestConfig(scope billing.RevenueScope) SchedulerConfig {
	return SchedulerConfig{ActorID: "worker-original", Scopes: []billing.RevenueScope{scope}, PageLimit: 3, ColdBudget: 1, RefreshBudget: 2, LeaseDuration: time.Minute, RetryBase: 2 * time.Second, RetryMax: 8 * time.Second}
}

// Actual native grants, bound worker identity checks, encrypted cursor/job
// transactions and execution fences; source descriptors are fixture inputs,
// not owning discovery proof. Active API-service identity remains controlled.
func nativeSchedulerFixture(t *testing.T) (*inputNativeFixture, *RecordExecutionRepository, *Scheduler, *executionTestClock, context.Context, []ScheduledJob) {
	t.Helper()
	f, queue, jobs := nativeScheduleFixture(t)
	require.NoError(t, queue.CommitScan(f.ctx, ScheduleCommit{Scope: f.intent.Scope, Lane: ColdLane, Writes: scheduleWrites(jobs, 0)}))
	clock := &executionTestClock{at: f.clock.at}
	r, err := NewRecordExecutionRepository(f.reply, clock)
	require.NoError(t, err)
	a, err := partneraccess.NewLifecycleWorkerAuthority("hostapp", "worker-original", inputNativeIdentity{}, f.policy, []billing.RevenueScope{f.intent.Scope})
	require.NoError(t, err)
	ctx, err := a.Bind(f.ctx)
	require.NoError(t, err)
	s, err := NewScheduler(r, a, clock, schedulerTestConfig(f.intent.Scope))
	require.NoError(t, err)
	return f, r, s, clock, ctx, jobs
}

type schedulerFailureRepository struct {
	SchedulerRepository
	sourceID string
	err      error
}

func (r schedulerFailureRepository) Acquire(ctx context.Context, q LeaseRequest) (ScheduledJob, error) {
	if q.Source.SourceID == r.sourceID {
		return ScheduledJob{}, r.err
	}
	return r.SchedulerRepository.Acquire(ctx, q)
}

func TestSchedulerEncryptedFairLanes(t *testing.T) {
	for _, name := range []string{"budget_reaches_due_suffix", "future_due_prefix", "active_lease_prefix", "full_active_page_wraps", "independent_refresh_budget", "known_failure_prefix_advances", "joined_conflict_outage_is_not_skip", "counter_exhaustion_reports_and_advances"} {
		t.Run(name, func(t *testing.T) {
			f, r, s, clock, ctx, jobs := nativeSchedulerFixture(t)
			switch name {
			case "future_due_prefix":
				j := jobs[0]
				j.Revision++
				j.NextAttemptAt = clock.at.Add(time.Hour)
				require.NoError(t, r.CommitScan(ctx, ScheduleCommit{Scope: j.Source.Scope, Lane: ColdLane, Expected: ScheduleCursor{Revision: 1}, Writes: []ScheduleWrite{{1, j}}}))
			case "active_lease_prefix", "full_active_page_wraps":
				count := 1
				if name == "full_active_page_wraps" {
					count = len(jobs)
				}
				for _, j := range jobs[:count] {
					_, err := r.Acquire(ctx, LeaseRequest{Source: j.Source, ExpectedRevision: 1, Actor: "worker-original", Token: "controlled-existing-lease", Until: clock.at.Add(time.Minute)})
					require.NoError(t, err)
				}
			case "independent_refresh_budget":
				writes := []ScheduleWrite{}
				for _, j := range jobs[1:] {
					j.Revision++
					j.Lane = RefreshLane
					writes = append(writes, ScheduleWrite{1, j})
				}
				require.NoError(t, r.CommitScan(ctx, ScheduleCommit{Scope: f.intent.Scope, Lane: ColdLane, Expected: ScheduleCursor{Revision: 1}, Writes: writes}))
			case "known_failure_prefix_advances", "joined_conflict_outage_is_not_skip":
				outage := errors.New("controlled per-job acquisition outage")
				if name == "joined_conflict_outage_is_not_skip" {
					outage = errors.Join(recordstore.ErrConflict, outage)
				}
				s.repo = schedulerFailureRepository{r, jobs[0].Source.SourceID, outage}
			case "counter_exhaustion_reports_and_advances":
				j := jobs[0]
				j.Revision++
				j.Attempts = math.MaxInt64
				seedDiscoveryExecution(t, f, j, 1)
			}
			first, err := s.Scan(ctx, f.intent.Scope, ColdLane)
			if name == "known_failure_prefix_advances" || name == "joined_conflict_outage_is_not_skip" || name == "counter_exhaustion_reports_and_advances" {
				require.Error(t, err)
				require.Equal(t, ScheduleBatch{}, first)
				if name == "counter_exhaustion_reports_and_advances" {
					require.ErrorIs(t, err, ErrExecutionCounterExhausted)
				}
				persisted, err := r.ReadScan(ctx, ScheduleScan{f.intent.Scope, ColdLane, 3})
				require.NoError(t, err)
				require.Equal(t, scheduledIdentity(jobs[0].Source), persisted.Cursor.AfterID)
				healthy, err := s.Scan(ctx, f.intent.Scope, ColdLane)
				require.NoError(t, err)
				require.Len(t, healthy.Jobs, 1)
				require.Equal(t, jobs[1].Source, healthy.Jobs[0].Source)
				return
			}
			require.NoError(t, err)
			if name == "full_active_page_wraps" {
				require.Empty(t, first.Jobs)
				require.Equal(t, 3, first.Examined)
				require.NotEmpty(t, first.Cursor.AfterID)
				end, err := s.Scan(ctx, f.intent.Scope, ColdLane)
				require.NoError(t, err)
				require.Empty(t, end.Cursor.AfterID)
				clock.set(clock.at.Add(time.Minute))
				again, err := s.Scan(ctx, f.intent.Scope, ColdLane)
				require.NoError(t, err)
				require.Len(t, again.Jobs, 1)
				require.Equal(t, jobs[0].Source, again.Jobs[0].Source)
				return
			}
			require.Len(t, first.Jobs, 1)
			token, err := base64.RawURLEncoding.DecodeString(first.Jobs[0].LeaseToken)
			require.NoError(t, err)
			require.Len(t, token, 32)
			if name == "future_due_prefix" || name == "active_lease_prefix" {
				require.Equal(t, jobs[1].Source, first.Jobs[0].Source)
				require.Equal(t, 2, first.Examined)
				return
			}
			if name == "independent_refresh_budget" {
				refresh, err := s.Scan(ctx, f.intent.Scope, RefreshLane)
				require.NoError(t, err)
				require.Len(t, refresh.Jobs, 2)
				require.Empty(t, refresh.Cursor.AfterID)
				return
			}
			require.Equal(t, jobs[0].Source, first.Jobs[0].Source)
			require.Equal(t, scheduledIdentity(jobs[0].Source), first.Cursor.AfterID)
			_, err = s.Retry(ctx, JobLease(first.Jobs[0]))
			require.NoError(t, err)
			second, err := s.Scan(ctx, f.intent.Scope, ColdLane)
			require.NoError(t, err)
			require.Len(t, second.Jobs, 1)
			require.Equal(t, jobs[1].Source, second.Jobs[0].Source)
			third, err := s.Scan(ctx, f.intent.Scope, ColdLane)
			require.NoError(t, err)
			require.Len(t, third.Jobs, 1)
			require.Equal(t, jobs[2].Source, third.Jobs[0].Source)
			require.Empty(t, third.Cursor.AfterID)
			require.NotEqual(t, first.Jobs[0].LeaseToken, second.Jobs[0].LeaseToken)
		})
	}
}

func TestSchedulerEncryptedCurrentAuthorityAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		want                      error
		acquired, cursorCommitted bool
	}{
		{"wrong_instance_context", partnermanager.ErrDenied, false, false},
		{"post_acquire_revoked", partnermanager.ErrDenied, true, false},
		{"post_acquire_canceled", context.Canceled, true, false},
		{"unknown_acquire", recordstore.ErrUncertain, true, false},
		{"unknown_acquire_then_revoked", recordstore.ErrUncertain, true, false},
		{"unknown_cursor", recordstore.ErrUncertain, true, true},
		{"unknown_cursor_then_revoked", recordstore.ErrUncertain, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, s, _, ctx, jobs := nativeSchedulerFixture(t)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if tc.name == "wrong_instance_context" {
				ctx = f.ctx
			}
			count := 0
			f.reply.after = func() {
				count++
				if count == 1 {
					switch tc.name {
					case "post_acquire_revoked":
						f.revoke(t)
					case "post_acquire_canceled":
						cancel()
					case "unknown_acquire":
						f.reply.lose = true
					case "unknown_acquire_then_revoked":
						f.reply.lose = true
						f.revoke(t)
					}
				}
				if count == 2 {
					switch tc.name {
					case "unknown_cursor":
						f.reply.lose = true
					case "unknown_cursor_then_revoked":
						f.reply.lose = true
						f.revoke(t)
					}
				}
			}
			out, err := s.Scan(ctx, f.intent.Scope, ColdLane)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, ScheduleBatch{}, out)
			if tc.name == "unknown_acquire_then_revoked" || tc.name == "unknown_cursor_then_revoked" {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			f.reply.after, f.reply.lose = nil, false
			inspect := context.WithoutCancel(f.ctx)
			persisted, err := r.ReadScan(inspect, ScheduleScan{f.intent.Scope, ColdLane, 3})
			require.NoError(t, err)
			if tc.cursorCommitted {
				require.Equal(t, scheduledIdentity(jobs[0].Source), persisted.Cursor.AfterID)
				require.Equal(t, int64(2), persisted.Cursor.Revision)
			} else {
				require.Equal(t, ScheduleCursor{Revision: 1}, persisted.Cursor)
			}
			j, err := r.ReadJob(inspect, jobs[0].Source)
			require.NoError(t, err)
			if tc.acquired {
				require.Equal(t, int64(1), j.Fence)
				require.NotEmpty(t, j.LeaseToken)
			} else {
				require.Zero(t, j.Fence)
				require.Empty(t, j.LeaseToken)
			}
			if tc.name == "post_acquire_revoked" {
				_, err := s.Retry(ctx, JobLease(j))
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			if tc.name == "unknown_acquire" || tc.name == "unknown_cursor" {
				next, err := s.Scan(ctx, f.intent.Scope, ColdLane)
				require.NoError(t, err)
				require.Len(t, next.Jobs, 1)
				require.Equal(t, jobs[1].Source, next.Jobs[0].Source)
			}
		})
	}
}

func TestSchedulerEncryptedRetryPolicy(t *testing.T) {
	for _, name := range []string{"bounded_backoff", "expired_handle", "lost_release_reply"} {
		t.Run(name, func(t *testing.T) {
			f, r, s, clock, ctx, _ := nativeSchedulerFixture(t)
			batch, err := s.Scan(ctx, f.intent.Scope, ColdLane)
			require.NoError(t, err)
			j := batch.Jobs[0]
			if name == "expired_handle" {
				clock.set(j.LeasedUntil)
				out, err := s.Retry(ctx, JobLease(j))
				require.ErrorIs(t, err, recordstore.ErrConflict)
				require.Equal(t, ScheduledJob{}, out)
				return
			}
			if name == "lost_release_reply" {
				f.reply.lose = true
				out, err := s.Retry(ctx, JobLease(j))
				require.ErrorIs(t, err, recordstore.ErrUncertain)
				require.Equal(t, ScheduledJob{}, out)
				f.reply.lose = false
				stored, err := r.ReadJob(ctx, j.Source)
				require.NoError(t, err)
				require.Empty(t, stored.LeaseToken)
				require.Equal(t, clock.at.Add(2*time.Second), stored.NextAttemptAt)
				_, err = s.Retry(ctx, JobLease(j))
				require.ErrorIs(t, err, recordstore.ErrConflict)
				return
			}
			for _, delay := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second} {
				before := clock.at
				out, err := s.Retry(ctx, JobLease(j))
				require.NoError(t, err)
				require.Equal(t, before.Add(delay), out.NextAttemptAt)
				require.Equal(t, j.Attempts, out.Attempts)
				clock.set(out.NextAttemptAt)
				j, err = r.Acquire(ctx, LeaseRequest{Source: out.Source, ExpectedRevision: out.Revision, Actor: "worker-original", Token: "controlled-next-backoff-token", Until: clock.at.Add(time.Minute)})
				require.NoError(t, err)
			}
		})
	}
}

func TestSchedulerEncryptedOwningDiscoveryHandoff(t *testing.T) {
	for _, name := range []string{"acknowledged_checkout", "native_subscription", "discovery_removed_after_admission", "discovery_only_cannot_execute"} {
		t.Run(name, func(t *testing.T) {
			var f *inputNativeFixture
			kind := billing.LifecycleCheckoutSources
			if name == "native_subscription" {
				f = nativeStatusFixture(t).base
				kind = billing.LifecycleSubscriptionSources
			} else {
				f = nativeInputFixture(t)
			}
			repo, err := revenuestore.NewRepository(f.reply)
			require.NoError(t, err)
			owner, err := billing.NewRevenueService(repo, f.clock)
			require.NoError(t, err)
			ready := false
			for n := 0; n < 30; n++ {
				p, err := owner.PrepareLifecycleDiscovery(f.ctx, f.intent.Scope, 200)
				require.NoError(t, err)
				if p.State.Phase == billing.LifecyclePreparationComplete {
					ready = true
					break
				}
			}
			require.True(t, ready)
			g := f.grant
			g.Permissions = []string{billingmanager.LifecycleDiscovery, billingmanager.SubscriptionStatusRefresh}
			if name == "discovery_only_cannot_execute" {
				g.Permissions = []string{billingmanager.LifecycleDiscovery}
			}
			f.grant, err = f.policy.ReplaceGrant(f.ctx, g, g.Revision)
			require.NoError(t, err)
			a, err := partneraccess.NewLifecycleWorkerAuthority("hostapp", "worker-original", inputNativeIdentity{}, f.policy, []billing.RevenueScope{f.intent.Scope})
			require.NoError(t, err)
			ctx, err := a.Bind(f.ctx)
			require.NoError(t, err)
			manager, err := (&billingmanager.Service{}).WithRevenueServices(inputNativeRegistry{}, owner, f.native)
			require.NoError(t, err)
			_, err = manager.WithLifecycleDiscoveryAuthority(a)
			require.NoError(t, err)
			queue, err := NewRecordExecutionRepository(f.reply, f.clock)
			require.NoError(t, err)
			discovery, err := NewDiscoveryCollector(queue, manager, a, f.clock, DiscoveryConfig{ActorID: "worker-original", Scopes: []billing.RevenueScope{f.intent.Scope}, PageLimit: 1})
			require.NoError(t, err)
			providerCalls := f.provider.calls
			admitted, err := discovery.Admit(ctx, f.intent.Scope, kind)
			require.NoError(t, err)
			require.Equal(t, 1, admitted.Sources)
			if name == "discovery_removed_after_admission" {
				g := f.grant
				g.Permissions = []string{billingmanager.SubscriptionStatusRefresh}
				f.grant, err = f.policy.ReplaceGrant(ctx, g, g.Revision)
				require.NoError(t, err)
			}
			s, err := NewScheduler(queue, a, f.clock, schedulerTestConfig(f.intent.Scope))
			require.NoError(t, err)
			batch, err := s.Scan(ctx, f.intent.Scope, ColdLane)
			if name == "discovery_only_cannot_execute" {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
				require.Equal(t, ScheduleBatch{}, batch)
			} else {
				require.NoError(t, err)
				require.Len(t, batch.Jobs, 1)
				require.Equal(t, kind, batch.Jobs[0].Source.Kind)
				current, err := s.Check(ctx, JobLease(batch.Jobs[0]))
				require.NoError(t, err)
				require.Equal(t, batch.Jobs[0], current)
			}
			require.Equal(t, providerCalls, f.provider.calls)
		})
	}
}
