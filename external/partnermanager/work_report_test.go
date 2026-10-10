package partnermanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named cases independently cover scheduling boundaries,
// complete-or-error projection, private revision evidence and scoped authority.
type backlogRepositoryStub struct {
	WorkRepository
	snapshot WorkSnapshot
	err      error
	calls    int
}

func (r *backlogRepositoryStub) ReadWorkSnapshot(context.Context, string) (WorkSnapshot, error) {
	r.calls++
	return r.snapshot, r.err
}

func backlogSnapshot(program string) WorkSnapshot {
	return WorkSnapshot{ProgramID: program, Cursors: map[string]DiscoveryCursor{
		WorkSignup: {Revision: 1}, WorkRevenue: {Revision: 1}, WorkRevenueSource: {Revision: 1}, WorkMaturity: {Revision: 1},
	}}
}

func backlogItem(program, kind, source string, at time.Time) WorkItem {
	item := WorkItem{ID: workID(program, kind, source), ProgramID: program, Kind: kind, State: WorkPending,
		SourceID: source, SourceFingerprint: "private_source_digest", CreatedAt: at.Add(-time.Hour), NextAttemptAt: at.Add(-time.Hour)}
	if kind == WorkMaturity {
		item.InitialDueAt = item.NextAttemptAt
	}
	return item
}

func backlogDecision(t *testing.T, item WorkItem, at time.Time) *WorkDecision {
	t.Helper()
	d := &WorkDecision{ActorID: "private_worker_actor", Outcome: WorkAccepted, AcceptanceID: "private_owner_receipt", ReasonCode: "accepted", RecordedAt: at.Add(-time.Minute)}
	var err error
	d.Fingerprint, err = sourceDigest([]string{item.ID, item.SourceFingerprint, d.ActorID, d.Outcome, d.AcceptanceID, d.ReasonCode})
	require.NoError(t, err)
	d.ID = "decision_" + d.Fingerprint
	return d
}

func TestWorkBacklogScheduling(t *testing.T) {
	cases := []struct {
		name                          string
		next, lease                   time.Duration
		attempts                      int64
		decision                      bool
		ready, backoff, leased        int64
		delayed                       int64
		maturity                      bool
		attempted, awaitingCompletion int64
	}{
		{name: "never_attempted_ready", next: -time.Hour, ready: 1},
		{name: "future_maturity_is_delayed_not_a_retry", next: time.Hour, delayed: 1, maturity: true},
		{name: "maturity_due_at_as_of_is_ready", ready: 1, maturity: true},
		{name: "due_exactly_at_as_of", ready: 1},
		{name: "future_backoff", next: time.Second, backoff: 1, attempts: 1, attempted: 1},
		{name: "first_live_attempt_is_attempted_not_proven_retry", lease: time.Minute, leased: 1, attempts: 1, attempted: 1},
		{name: "live_lease_precedes_future_schedule", next: time.Minute, lease: time.Minute, leased: 1, attempts: 2, attempted: 1},
		{name: "expired_token_is_ready", lease: -time.Minute, ready: 1, attempts: 1, attempted: 1},
		{name: "expired_token_with_future_due_is_backoff", next: time.Minute, lease: -time.Minute, backoff: 1, attempts: 1, attempted: 1},
		{name: "committed_decision_still_waiting_for_queue_completion", lease: time.Minute, leased: 1, attempts: 1, attempted: 1, decision: true, awaitingCompletion: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(1700000000, 0).UTC()
			item := backlogItem("program", WorkRevenue, "private_payment", at)
			if tc.maturity {
				item = backlogItem("program", WorkMaturity, "private_maturity", at)
				item.InitialDueAt = at.Add(tc.next)
			}
			item.NextAttemptAt, item.Attempts = at.Add(tc.next), tc.attempts
			if tc.backoff > 0 && tc.attempts > 0 {
				item.LastErrorCode = "owning_unavailable"
			}
			if tc.lease != 0 {
				item.LeasedUntil, item.LeaseToken = at.Add(tc.lease), "private_lease_token"
			}
			if tc.decision {
				item.Decision = backlogDecision(t, item, at)
			}
			snapshot := backlogSnapshot("program")
			snapshot.Pending = []WorkItem{item}
			repo := &backlogRepositoryStub{snapshot: snapshot}
			queue, err := NewWorkQueue(repo, managerClock{at}, workTestConfig())
			require.NoError(t, err)
			out, err := queue.GetBacklog(context.Background())
			require.NoError(t, err)
			require.Equal(t, at, out.AsOf)
			require.EqualValues(t, 1, out.Counts.Pending)
			require.Equal(t, tc.ready, out.Counts.Ready)
			require.Equal(t, tc.backoff, out.Counts.Backoff)
			require.Equal(t, tc.delayed, out.Counts.Delayed)
			require.Equal(t, tc.leased, out.Counts.Leased)
			require.Equal(t, tc.attempted, out.Counts.AttemptedPending)
			require.Equal(t, tc.awaitingCompletion, out.Counts.DecisionAwaitingCompletion)
			require.True(t, item.CreatedAt.Equal(*out.Counts.OldestCreatedAt))
			if tc.ready == 1 {
				require.True(t, item.NextAttemptAt.Equal(*out.Counts.OldestReadyAt))
			} else {
				require.Nil(t, out.Counts.OldestReadyAt)
			}
			encoded, err := json.Marshal(out)
			require.NoError(t, err)
			for _, private := range []string{item.ID, item.SourceID, item.SourceFingerprint, "private_lease_token", "private_worker_actor", "private_owner_receipt"} {
				require.NotContains(t, string(encoded), private)
			}
			private, err := json.Marshal(snapshot)
			require.NoError(t, err)
			require.JSONEq(t, `{}`, string(private), "owning snapshot must not accidentally disclose cursor identities")
			require.Len(t, out.Kinds, 4)
			require.NoError(t, out.Validate())
		})
	}
}

func TestWorkBacklogRejectsIncompleteEvidence(t *testing.T) {
	cases := []struct {
		name, mode string
		want       error
	}{
		{"complete_empty_scope", "", nil},
		{"wrong_program", "program", ErrUnavailable},
		{"missing_kind_cursor", "cursor_missing", ErrUnavailable},
		{"negative_cursor_revision", "cursor_revision", ErrUnavailable},
		{"position_without_persisted_revision", "cursor_position", ErrUnavailable},
		{"pending_without_persisted_cursor", "cursor_unwritten", ErrUnavailable},
		{"wrong_work_id", "id", ErrUnavailable},
		{"completed_row_in_pending_snapshot", "complete", ErrUnavailable},
		{"foreign_work_kind", "kind", ErrUnavailable},
		{"created_after_owning_clock", "future", ErrUnavailable},
		{"schedule_precedes_creation", "early", ErrUnavailable},
		{"lease_token_without_expiry", "token", ErrUnavailable},
		{"lease_expiry_without_token", "expiry", ErrUnavailable},
		{"never_attempted_error", "error", ErrUnavailable},
		{"decision_digest_mismatch", "digest", ErrUnavailable},
		{"decision_after_snapshot", "decision_future", ErrUnavailable},
		{"duplicate_work_item", "duplicate", ErrUnavailable},
		{"failed_repository_with_partial_output", "outage", ErrUnavailable},
		{"cancelled_read", "cancel", context.Canceled},
		{"nil_context", "nil_context", ErrInvalid},
		{"optional_repository_unwired", "unwired", ErrUnavailable},
		{"zero_owning_clock", "clock", ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(1700000000, 0).UTC()
			snapshot := backlogSnapshot("program")
			item := backlogItem("program", WorkSignup, "private_signup", at)
			if tc.mode != "" {
				snapshot.Pending = []WorkItem{item}
			}
			ctx := context.Background()
			repo := &backlogRepositoryStub{}
			switch tc.mode {
			case "program":
				snapshot.ProgramID = "another"
			case "cursor_missing":
				delete(snapshot.Cursors, WorkRevenueSource)
			case "cursor_revision":
				snapshot.Cursors[WorkSignup] = DiscoveryCursor{Revision: -1}
			case "cursor_position":
				snapshot.Cursors[WorkRevenue] = DiscoveryCursor{AfterSequence: 3}
			case "cursor_unwritten":
				snapshot.Cursors[WorkSignup] = DiscoveryCursor{}
			case "id":
				item.ID = "invented"
			case "complete":
				item.State = WorkComplete
			case "kind":
				item.Kind = "unknown"
			case "future":
				item.CreatedAt = at.Add(time.Second)
			case "early":
				item.NextAttemptAt = item.CreatedAt.Add(-time.Second)
			case "token":
				item.LeaseToken = "token"
			case "expiry":
				item.LeasedUntil = at.Add(time.Second)
			case "error":
				item.LastErrorCode = "owning_unavailable"
			case "digest", "decision_future":
				item.Attempts = 1
				item.Decision = backlogDecision(t, item, at)
				if tc.mode == "digest" {
					item.Decision.Fingerprint = "wrong"
					item.Decision.ID = "decision_wrong"
				} else {
					item.Decision.RecordedAt = at.Add(time.Second)
				}
			case "outage":
				repo.err = errors.Join(ErrNotFound, ErrUnavailable)
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil_context":
				ctx = nil
			case "clock":
				at = time.Time{}
			}
			if len(snapshot.Pending) > 0 {
				snapshot.Pending[0] = item
			}
			if tc.mode == "duplicate" {
				snapshot.Pending = append(snapshot.Pending, item)
			}
			repo.snapshot = snapshot
			var owning WorkRepository = repo
			if tc.mode == "unwired" {
				owning = &workRepositorySpy{}
			}
			queue, err := NewWorkQueue(owning, managerClock{at}, workTestConfig())
			require.NoError(t, err)
			out, err := queue.GetBacklog(ctx)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
			}
			if tc.mode == "cancel" || tc.mode == "nil_context" {
				require.Zero(t, repo.calls)
			}
		})
	}
}

func TestWorkBacklogRevisionAndCompleteCapacity(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		count        int
		want         error
		same         bool
	}{
		{name: "row_order_is_not_a_state_change", change: "order", count: 2, same: true},
		{name: "private_source_change_changes_revision", change: "source", count: 2},
		{name: "private_lease_change_changes_revision", change: "lease", count: 2},
		{name: "discovery_position_change_changes_revision", change: "cursor", count: 2},
		{name: "exact_combined_pending_budget", count: WorkReportCapacity, same: true},
		{name: "over_budget_is_error_without_partial_counts", count: WorkReportCapacity + 1, want: ErrReportCapacity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(1700000000, 0).UTC()
			s := backlogSnapshot("program")
			for n := 0; n < tc.count; n++ {
				s.Pending = append(s.Pending, backlogItem("program", []string{WorkSignup, WorkRevenue, WorkRevenueSource, WorkMaturity}[n%4], fmt.Sprintf("source_%05d", n), at))
			}
			if tc.change == "lease" {
				s.Pending[0].LeaseToken = "old_private_lease"
				s.Pending[0].LeasedUntil = at.Add(time.Minute)
				s.Pending[0].Attempts = 1
			}
			r := &backlogRepositoryStub{snapshot: s}
			q, err := NewWorkQueue(r, managerClock{at}, workTestConfig())
			require.NoError(t, err)
			before, err := q.GetBacklog(context.Background())
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, before)
				return
			}
			firstID := r.snapshot.Pending[0].ID
			switch tc.change {
			case "order":
				r.snapshot.Pending[0], r.snapshot.Pending[1] = r.snapshot.Pending[1], r.snapshot.Pending[0]
			case "source":
				r.snapshot.Pending[0].SourceFingerprint = "changed_private_source"
			case "lease":
				r.snapshot.Pending[0].LeaseToken = "new_private_lease"
			case "cursor":
				r.snapshot.Cursors[WorkRevenue] = DiscoveryCursor{Revision: 2, AfterSequence: 4}
			}
			after, err := q.GetBacklog(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.same, before.Revision == after.Revision)
			require.EqualValues(t, tc.count, after.Counts.Pending)
			if tc.change == "order" {
				require.Equal(t, firstID, r.snapshot.Pending[1].ID, "reporting must not reorder retained repository evidence")
			}
		})
	}
}

func TestWorkBacklogProjectionValidation(t *testing.T) {
	for _, tc := range []struct{ name, mode string }{
		{"negative_pending_count", "negative"},
		{"overlapping_schedule_states", "schedule"},
		{"decision_without_an_attempt", "decision"},
		{"missing_oldest_pending_time", "oldest"},
		{"future_oldest_ready_time", "future"},
		{"ready_time_precedes_creation", "early"},
		{"per_kind_counts_disagree_with_total", "sum"},
		{"duplicate_kind_hides_another_scope", "duplicate_kind"},
		{"unexpected_coverage_claim", "coverage"},
		{"invalid_persisted_revision", "revision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Unix(1700000000, 0).UTC()
			snapshot := backlogSnapshot("program")
			snapshot.Pending = []WorkItem{backlogItem("program", WorkSignup, "source", at)}
			r := &backlogRepositoryStub{snapshot: snapshot}
			q, err := NewWorkQueue(r, managerClock{at}, workTestConfig())
			require.NoError(t, err)
			out, err := q.GetBacklog(context.Background())
			require.NoError(t, err)
			switch tc.mode {
			case "negative":
				out.Counts.Pending = -1
			case "schedule":
				out.Counts.Leased = 1
			case "decision":
				out.Counts.DecisionAwaitingCompletion = 1
			case "oldest":
				out.Counts.OldestCreatedAt = nil
			case "future":
				value := at.Add(time.Second)
				out.Counts.OldestReadyAt = &value
			case "early":
				value := out.Counts.OldestCreatedAt.Add(-time.Second)
				out.Counts.OldestReadyAt = &value
			case "sum":
				out.Kinds[0].Counts = WorkBacklogCounts{}
			case "duplicate_kind":
				out.Kinds[2].Kind = WorkRevenue
			case "coverage":
				out.Coverage = "all_provider_events_processed"
			case "revision":
				out.Revision = strings.Repeat("z", 64)
			}
			require.ErrorIs(t, out.Validate(), ErrUnavailable)
		})
	}
}

type backlogServiceStub struct {
	output    WorkBacklog
	err       error
	calls     int
	afterRead func()
}

func (s *backlogServiceStub) GetBacklog(context.Context) (WorkBacklog, error) {
	s.calls++
	if s.afterRead != nil {
		s.afterRead()
	}
	return s.output, s.err
}

type operationsAuthorityStub struct {
	denied, revoked               bool
	calls                         int
	actors, targets, capabilities []string
}

func (a *operationsAuthorityStub) CheckPartners(_ context.Context, actor, capability, target string) error {
	a.calls++
	a.actors = append(a.actors, actor)
	a.targets = append(a.targets, target)
	a.capabilities = append(a.capabilities, capability)
	if a.denied || a.revoked {
		return ErrDenied
	}
	return nil
}

func TestAdminWorkerBacklogAuthorityAndOptionalWiring(t *testing.T) {
	for _, tc := range []struct {
		name, mode    string
		want          error
		reads, checks int
	}{
		{"paused_admission_preserves_authorized_ops", "", nil, 1, 2},
		{"program_operations_denied_before_read", "denied", ErrDenied, 0, 1},
		{"revoked_during_read_discards_result", "revoked", ErrDenied, 1, 2},
		{"wrong_program_discards_result", "program", ErrUnavailable, 1, 1},
		{"malformed_result_discards_result", "shape", ErrUnavailable, 1, 1},
		{"future_classification_clock_discards_result", "future", ErrUnavailable, 1, 1},
		{"zero_classification_clock_discards_result", "zero", ErrUnavailable, 1, 1},
		{"service_failure_discards_partial_result", "outage", ErrUnavailable, 1, 1},
		{"optional_service_unwired", "unwired", ErrUnavailable, 0, 1},
		{"typed_nil_service_unwired", "typed_nil", ErrUnavailable, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, _, _, _ := managerFixture(t)
			m.deps.Controls = Controls{}
			a := &operationsAuthorityStub{}
			m.deps.Authority = a
			cfg := workTestConfig()
			cfg.ProgramID = partnerprogram.ProgramID
			r := &backlogRepositoryStub{snapshot: backlogSnapshot(cfg.ProgramID)}
			q, err := NewWorkQueue(r, m.deps.Clock, cfg)
			require.NoError(t, err)
			out, err := q.GetBacklog(context.Background())
			require.NoError(t, err)
			s := &backlogServiceStub{output: out}
			m.deps.WorkReporting = s
			switch tc.mode {
			case "denied":
				a.denied = true
			case "revoked":
				s.afterRead = func() { a.revoked = true }
			case "program":
				s.output.ProgramID = "another_program"
			case "shape":
				s.output.Revision = strings.Repeat("Z", 64)
			case "future":
				s.output.AsOf = s.output.AsOf.Add(time.Second)
			case "zero":
				s.output.AsOf = time.Time{}
			case "outage":
				s.err = ErrUnavailable
			case "unwired":
				m.deps.WorkReporting = nil
			case "typed_nil":
				var empty *backlogServiceStub
				m.deps.WorkReporting = empty
			}
			constructed, err := NewManager(m.deps)
			require.NoError(t, err, "optional reporting must not break unrelated construction")
			result, err := constructed.AdminWorkerBacklog(context.Background(), "verified_operator")
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, result)
			}
			require.Equal(t, tc.reads, s.calls)
			require.Equal(t, tc.checks, a.calls)
			for i := range a.targets {
				require.Equal(t, partnerprogram.ProgramID, a.targets[i])
				require.Equal(t, CapabilityOperations, a.capabilities[i])
				require.Equal(t, "verified_operator", a.actors[i])
			}
		})
	}
}
