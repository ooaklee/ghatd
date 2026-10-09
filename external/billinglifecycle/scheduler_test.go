package billinglifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Isolated service boundaries, not transaction/fairness/native grant proof.
type schedulerTestRepository struct {
	snapshot                                                      ScheduleSnapshot
	leased                                                        ScheduledJob
	stage                                                         string
	scanErr, acquireErr, cursorErr, checkErr, readErr, releaseErr error
	acquireMode, checkMode, readMode, releaseMode                 string
	cursorCalls, acquireCalls                                     int
	commit                                                        ScheduleCommit
	afterAcquire                                                  func()
}

func (r *schedulerTestRepository) ReadScan(context.Context, ScheduleScan) (ScheduleSnapshot, error) {
	r.stage = "scan"
	return r.snapshot, r.scanErr
}
func (r *schedulerTestRepository) CommitScan(_ context.Context, c ScheduleCommit) error {
	r.stage = "cursor"
	r.cursorCalls++
	r.commit = c
	return r.cursorErr
}
func (r *schedulerTestRepository) Acquire(_ context.Context, q LeaseRequest) (ScheduledJob, error) {
	r.stage = "acquire"
	r.acquireCalls++
	if r.afterAcquire != nil {
		r.afterAcquire()
	}
	if r.acquireErr != nil {
		return ScheduledJob{}, r.acquireErr
	}
	j := r.snapshot.Jobs[0]
	j.Revision++
	j.Fence++
	j.Attempts++
	j.LeaseActor, j.LeaseToken, j.LeasedUntil = q.Actor, q.Token, q.Until
	switch r.acquireMode {
	case "changed_owner":
		j.Source.PrincipalID = "other"
		j.OriginalStatus = nil
	case "lost_original":
		j.OriginalStatus = nil
	case "changed_creation":
		j.CreatedAt = j.CreatedAt.Add(time.Second)
	case "bad_token":
		j.LeaseToken = "unexpected-token"
	}
	r.leased = j
	return j, nil
}
func (r *schedulerTestRepository) ReadJob(context.Context, ScheduledSource) (ScheduledJob, error) {
	r.stage = "read-job"
	j := r.snapshot.Jobs[0]
	if r.readMode == "changed_owner" {
		j.Source.PrincipalID = "other"
		j.OriginalStatus = nil
	}
	return j, r.readErr
}
func (r *schedulerTestRepository) CheckLease(context.Context, LeaseHandle) (ScheduledJob, error) {
	r.stage = "check"
	j := r.leased
	if r.checkMode == "changed_owner" {
		j.Source.PrincipalID = "other"
		j.OriginalStatus = nil
	}
	return j, r.checkErr
}
func (r *schedulerTestRepository) Release(_ context.Context, q LeaseDisposition) (ScheduledJob, error) {
	r.stage = "release"
	j := r.leased
	j.Revision++
	j.LeaseActor, j.LeaseToken, j.LeasedUntil = "", "", time.Time{}
	j.NextAttemptAt = q.NextAttemptAt
	if r.releaseMode == "lost_original" {
		j.OriginalStatus = nil
	}
	return j, r.releaseErr
}

type schedulerTestAuthority struct {
	repo      *schedulerTestRepository
	denyStage string
	selected  bool
	err       error
}

func (a *schedulerTestAuthority) check(ctx context.Context, actor, action string, selected bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor != "worker-original" || action != billingmanager.SubscriptionStatusRefresh {
		return partnermanager.ErrDenied
	}
	if a.repo.stage == a.denyStage && (!a.selected || selected) {
		if a.err != nil {
			return a.err
		}
		return partnermanager.ErrDenied
	}
	return nil
}
func (a *schedulerTestAuthority) AuthorizeSubscriptionStatus(ctx context.Context, actor, action string, target billingmanager.SubscriptionStatusTarget) error {
	return a.check(ctx, actor, action, target.PrincipalID != "")
}
func (a *schedulerTestAuthority) AuthorizeCheckoutLifecycle(ctx context.Context, actor, action string, target billingmanager.CheckoutLifecycleTarget) error {
	return a.check(ctx, actor, action, true)
}

func schedulerServiceFixture(t *testing.T) (*Scheduler, *schedulerTestRepository, *schedulerTestAuthority, billing.RevenueScope) {
	t.Helper()
	f := originalStatusFixture(t, "checkout")
	source := ScheduledSource{Scope: f.p.Scope, Kind: billing.LifecycleSubscriptionSources, SourceID: billing.LifecycleDiscoverySourceID(f.p.Scope, billing.LifecycleSubscriptionSources, f.p.SubscriptionID), PrincipalID: f.p.PrincipalID, SubscriptionID: f.p.SubscriptionID}
	j := ScheduledJob{Source: source, Revision: 1, Lane: ColdLane, CreatedAt: f.clock.at, NextAttemptAt: f.clock.at, OriginalStatus: &f.p}
	r := &schedulerTestRepository{snapshot: ScheduleSnapshot{Jobs: []ScheduledJob{j}}}
	a := &schedulerTestAuthority{repo: r, denyStage: "never"}
	s, err := NewScheduler(r, a, f.clock, schedulerTestConfig(f.p.Scope))
	require.NoError(t, err)
	return s, r, a, f.p.Scope
}

func TestSchedulerCurrentServiceBoundaries(t *testing.T) {
	outage := errors.New("scheduler fixture outage")
	for _, tc := range []struct {
		name   string
		want   error
		cursor bool
	}{
		{"confirmed_original_preserved", nil, true}, {"empty_scan_wrap", nil, true},
		{"initial_denied", partnermanager.ErrDenied, false}, {"scan_outage", outage, false},
		{"scan_outage_then_denied", partnermanager.ErrDenied, false}, {"scan_unknown_then_denied", recordstore.ErrUncertain, false},
		{"invalid_cursor", billing.ErrRevenueUnavailable, false}, {"invalid_job", billing.ErrRevenueUnavailable, false},
		{"selected_denied", partnermanager.ErrDenied, false},
		{"sole_conflict_inspected", nil, true}, {"joined_conflict_outage", outage, true},
		{"conflict_read_outage", outage, true}, {"conflict_changed_owner", billing.ErrRevenueUnavailable, true},
		{"conflict_read_unknown", recordstore.ErrUncertain, false},
		{"authority_conflict_is_not_acquisition_skip", recordstore.ErrConflict, false},
		{"acquire_outage", outage, true}, {"acquire_unknown", recordstore.ErrUncertain, false},
		{"acquire_unknown_then_denied", recordstore.ErrUncertain, false}, {"acquire_unknown_then_canceled", recordstore.ErrUncertain, false},
		{"acquired_owner_changed", billing.ErrRevenueUnavailable, false}, {"acquired_original_lost", billing.ErrRevenueUnavailable, false},
		{"acquired_creation_changed", billing.ErrRevenueUnavailable, false}, {"acquired_token_changed", billing.ErrRevenueUnavailable, false},
		{"cursor_outage", outage, true}, {"cursor_unknown_then_denied", recordstore.ErrUncertain, true},
		{"final_lease_lost", recordstore.ErrConflict, true}, {"final_lease_owner_changed", billing.ErrRevenueUnavailable, true},
		{"final_check_denied", partnermanager.ErrDenied, true}, {"unconfigured_scope", billing.ErrRevenueInvalid, false},
		{"retired_lane", billing.ErrRevenueInvalid, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r, a, scope := schedulerServiceFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			lane := ColdLane
			switch tc.name {
			case "empty_scan_wrap":
				r.snapshot.Jobs = nil
			case "initial_denied":
				a.denyStage = ""
			case "scan_outage":
				r.scanErr = outage
			case "scan_outage_then_denied":
				r.scanErr = outage
				a.denyStage = "scan"
			case "scan_unknown_then_denied":
				r.scanErr = recordstore.ErrUncertain
				a.denyStage = "scan"
			case "invalid_cursor":
				r.snapshot.Cursor.AfterID = "native.discovery.cursor"
			case "invalid_job":
				r.snapshot.Jobs[0].Revision = 0
			case "selected_denied":
				a.denyStage = "scan"
				a.selected = true
			case "sole_conflict_inspected":
				r.acquireErr = recordstore.ErrConflict
			case "joined_conflict_outage":
				r.acquireErr = errors.Join(recordstore.ErrConflict, outage)
			case "conflict_read_outage":
				r.acquireErr = recordstore.ErrConflict
				r.readErr = outage
			case "conflict_changed_owner":
				r.acquireErr = recordstore.ErrConflict
				r.readMode = "changed_owner"
			case "conflict_read_unknown":
				r.acquireErr = recordstore.ErrConflict
				r.readErr = recordstore.ErrUncertain
			case "authority_conflict_is_not_acquisition_skip":
				a.denyStage = "acquire"
				a.err = recordstore.ErrConflict
			case "acquire_outage":
				r.acquireErr = outage
			case "acquire_unknown":
				r.acquireErr = recordstore.ErrUncertain
			case "acquire_unknown_then_denied":
				r.acquireErr = recordstore.ErrUncertain
				a.denyStage = "acquire"
			case "acquire_unknown_then_canceled":
				r.acquireErr = recordstore.ErrUncertain
				r.afterAcquire = cancel
			case "acquired_owner_changed":
				r.acquireMode = "changed_owner"
			case "acquired_original_lost":
				r.acquireMode = "lost_original"
			case "acquired_creation_changed":
				r.acquireMode = "changed_creation"
			case "acquired_token_changed":
				r.acquireMode = "bad_token"
			case "cursor_outage":
				r.cursorErr = outage
			case "cursor_unknown_then_denied":
				r.cursorErr = recordstore.ErrUncertain
				a.denyStage = "cursor"
			case "final_lease_lost":
				r.checkErr = recordstore.ErrConflict
			case "final_lease_owner_changed":
				r.checkMode = "changed_owner"
			case "final_check_denied":
				a.denyStage = "check"
			case "unconfigured_scope":
				scope.AccountID = "other"
			case "retired_lane":
				lane = RetiredLane
			}
			out, err := s.Scan(ctx, scope, lane)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, ScheduleBatch{}, out)
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(1), out.Cursor.Revision)
				if tc.name == "confirmed_original_preserved" {
					require.Len(t, out.Jobs, 1)
					require.Equal(t, r.snapshot.Jobs[0].OriginalStatus, out.Jobs[0].OriginalStatus)
				}
				if tc.name == "sole_conflict_inspected" {
					require.Equal(t, 1, out.Conflicts)
					require.Empty(t, out.Jobs)
				}
				raw, err := json.Marshal(out)
				require.NoError(t, err)
				require.Equal(t, "{}", string(raw))
			}
			if tc.cursor {
				require.Equal(t, 1, r.cursorCalls)
				require.Empty(t, r.commit.Writes)
			} else {
				require.Zero(t, r.cursorCalls)
			}
			if tc.name == "acquire_unknown_then_denied" || tc.name == "cursor_unknown_then_denied" || tc.name == "scan_unknown_then_denied" {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			if tc.name == "acquire_unknown_then_canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}

func TestSchedulerRetryServiceBoundaries(t *testing.T) {
	for _, name := range []string{"original_preserved", "release_unknown", "release_unknown_then_denied", "release_original_lost", "check_owner_changed", "wrong_actor_handle"} {
		t.Run(name, func(t *testing.T) {
			s, r, a, scope := schedulerServiceFixture(t)
			batch, err := s.Scan(t.Context(), scope, ColdLane)
			require.NoError(t, err)
			h := JobLease(batch.Jobs[0])
			switch name {
			case "release_unknown":
				r.releaseErr = recordstore.ErrUncertain
			case "release_unknown_then_denied":
				r.releaseErr = recordstore.ErrUncertain
				a.denyStage = "release"
			case "release_original_lost":
				r.releaseMode = "lost_original"
			case "check_owner_changed":
				r.checkMode = "changed_owner"
			case "wrong_actor_handle":
				h.Actor = "other-worker"
			}
			out, err := s.Retry(t.Context(), h)
			if name == "original_preserved" {
				require.NoError(t, err)
				require.Equal(t, batch.Jobs[0].OriginalStatus, out.OriginalStatus)
				require.Equal(t, batch.Jobs[0].Attempts, out.Attempts)
				return
			}
			require.Error(t, err)
			require.Equal(t, ScheduledJob{}, out)
			if name == "release_unknown" || name == "release_unknown_then_denied" {
				require.ErrorIs(t, err, recordstore.ErrUncertain)
			}
			if name == "release_unknown_then_denied" {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
		})
	}
}

func TestSchedulerConfiguration(t *testing.T) {
	for _, name := range []string{"nil_repository", "nil_authority", "nil_clock", "empty_actor", "empty_scopes", "duplicate_scopes", "invalid_page", "invalid_cold_budget", "invalid_refresh_budget", "short_lease", "long_lease", "short_retry", "retry_max_below_base", "long_retry_max", "copied_configuration", "nil_context", "nil_service"} {
		t.Run(name, func(t *testing.T) {
			_, r, a, scope := schedulerServiceFixture(t)
			var repo SchedulerRepository = r
			var auth ExecutionAuthority = a
			var clock LifecycleClock = inputNativeClock{time.Unix(1700000000, 0)}
			cfg := schedulerTestConfig(scope)
			switch name {
			case "nil_repository":
				repo = nil
			case "nil_authority":
				auth = nil
			case "nil_clock":
				clock = nil
			case "empty_actor":
				cfg.ActorID = ""
			case "empty_scopes":
				cfg.Scopes = nil
			case "duplicate_scopes":
				cfg.Scopes = append(cfg.Scopes, scope)
			case "invalid_page":
				cfg.PageLimit = 201
			case "invalid_cold_budget":
				cfg.ColdBudget = cfg.PageLimit + 1
			case "invalid_refresh_budget":
				cfg.RefreshBudget = 0
			case "short_lease":
				cfg.LeaseDuration = time.Millisecond
			case "long_lease":
				cfg.LeaseDuration = time.Hour
			case "short_retry":
				cfg.RetryBase = time.Millisecond
			case "retry_max_below_base":
				cfg.RetryMax = time.Second
			case "long_retry_max":
				cfg.RetryMax = 48 * time.Hour
			}
			s, err := NewScheduler(repo, auth, clock, cfg)
			if name == "copied_configuration" {
				require.NoError(t, err)
				cfg.Scopes[0].AccountID = "changed"
				cfg.ActorID = "changed"
				require.Equal(t, scope, s.cfg.Scopes[0])
				require.Equal(t, "worker-original", s.cfg.ActorID)
				return
			}
			if name == "nil_context" || name == "nil_service" {
				require.NoError(t, err)
				ctx := t.Context()
				want := billing.ErrRevenueUnavailable
				if name == "nil_context" {
					ctx = nil
					want = billing.ErrRevenueInvalid
				} else {
					s = nil
				}
				out, err := s.Scan(ctx, scope, ColdLane)
				require.ErrorIs(t, err, want)
				require.Equal(t, ScheduleBatch{}, out)
				return
			}
			require.Error(t, err)
			require.Nil(t, s)
		})
	}
}
