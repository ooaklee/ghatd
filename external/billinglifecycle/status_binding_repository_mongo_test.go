package billinglifecycle

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var _ StatusBindingRepository = (*RecordExecutionRepository)(nil)

// These adapter cases use actual owning status originals and encrypted native
// transactions. The source admission is a fixture, not discovery/runtime proof.
func nativeStatusBindingFixture(t *testing.T) (*statusNativeFixture, *RecordExecutionRepository, *executionTestClock, ScheduledJob) {
	t.Helper()
	f := nativeStatusFixture(t)
	clock := &executionTestClock{at: f.base.clock.at}
	r, err := NewRecordExecutionRepository(f.base.reply, clock)
	require.NoError(t, err)
	p := f.p
	source := ScheduledSource{Scope: p.Scope, Kind: billing.LifecycleSubscriptionSources, PrincipalID: p.PrincipalID, SubscriptionID: p.SubscriptionID, SourceID: billing.LifecycleDiscoverySourceID(p.Scope, billing.LifecycleSubscriptionSources, p.SubscriptionID)}
	j := ScheduledJob{Source: source, Revision: 1, Lane: ColdLane, CreatedAt: clock.at, NextAttemptAt: clock.at}
	require.NoError(t, r.CommitScan(f.ctx, ScheduleCommit{Scope: p.Scope, Lane: ColdLane, Writes: []ScheduleWrite{{0, j}}}))
	j, err = r.Acquire(f.ctx, LeaseRequest{Source: source, ExpectedRevision: 1, Actor: "worker-original", Token: "controlled-binding-token", Until: clock.at.Add(time.Minute)})
	require.NoError(t, err)
	return f, r, clock, j
}

func TestStatusBindingEncryptedAtomicStages(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"preparation_and_pointer", nil}, {"evidence_and_revision", nil}, {"exact_evidence_replay", nil}, {"preparation_replay_preserves_evidence", nil},
		{"existing_outbox_attached_without_reset", nil}, {"stale_revision", recordstore.ErrConflict}, {"wrong_actor", recordstore.ErrConflict},
		{"wrong_token", recordstore.ErrConflict}, {"wrong_fence", recordstore.ErrConflict}, {"wrong_lane", recordstore.ErrConflict},
		{"wrong_expiry", recordstore.ErrConflict}, {"expired", recordstore.ErrConflict}, {"expires_after_input_write", recordstore.ErrConflict},
		{"clock_moves_backwards_after_input_write", recordstore.ErrConflict}, {"different_owner", recordstore.ErrInvalid},
		{"evidence_without_pointer", recordstore.ErrConflict}, {"different_unresolved_original", recordstore.ErrConflict},
		{"missing_attached_original", recordstore.ErrUnavailable}, {"lost_preparation_reply", recordstore.ErrUncertain},
		{"lost_evidence_reply", recordstore.ErrUncertain}, {"different_evidence", recordstore.ErrConflict},
		{"stale_handle_cannot_replace_evidence", recordstore.ErrConflict}, {"replacement_preserves_original", nil},
		{"revision_exhaustion", recordstore.ErrInvalid}, {"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, j := nativeStatusBindingFixture(t)
			h := JobLease(j)
			input := StatusInput{Preparation: f.p}
			ctx := f.ctx
			var e billing.VerifiedSubscriptionStatusEvidence
			switch tc.name {
			case "exact_evidence_replay", "evidence_and_revision", "preparation_replay_preserves_evidence", "different_unresolved_original", "missing_attached_original", "lost_evidence_reply", "different_evidence", "stale_handle_cannot_replace_evidence", "replacement_preserves_original":
				first, err := r.BindStatus(ctx, h, input)
				require.NoError(t, err)
				j = first.Job
				h = JobLease(j)
				if tc.name == "stale_handle_cannot_replace_evidence" {
					h.Revision--
				}
			}
			switch tc.name {
			case "exact_evidence_replay", "evidence_and_revision", "preparation_replay_preserves_evidence", "evidence_without_pointer", "lost_evidence_reply", "different_evidence", "stale_handle_cannot_replace_evidence", "existing_outbox_attached_without_reset":
				e = f.evidence(t)
				input.Evidence = &e
			}
			switch tc.name {
			case "exact_evidence_replay", "preparation_replay_preserves_evidence", "different_evidence":
				first, err := r.BindStatus(ctx, h, input)
				require.NoError(t, err)
				j = first.Job
				h = JobLease(j)
				switch tc.name {
				case "preparation_replay_preserves_evidence":
					input.Evidence = nil
				case "different_evidence":
					other := e
					other.Status = "active"
					input.Evidence = &other
				}
			case "existing_outbox_attached_without_reset":
				f.prepared(t)
				_, err := f.outbox.RetainEvidence(ctx, "worker-original", f.p, e)
				require.NoError(t, err)
				input.Evidence = nil
			case "stale_revision":
				h.Revision++
			case "wrong_actor":
				h.Actor = "worker-other"
			case "wrong_token":
				h.Token = "other-token"
			case "wrong_fence":
				h.Fence++
			case "wrong_lane":
				h.Lane = RefreshLane
			case "wrong_expiry":
				h.Until = h.Until.Add(time.Second)
			case "expired":
				clock.set(h.Until)
			case "expires_after_input_write":
				clock.sequence = []time.Time{clock.at, h.Until}
			case "clock_moves_backwards_after_input_write":
				clock.sequence = []time.Time{clock.at, clock.at.Add(-time.Second)}
			case "different_owner":
				input.Preparation.PrincipalID = "different"
			case "different_unresolved_original":
				nativeRepo, err := revenuestore.NewRepository(f.base.reply)
				require.NoError(t, err)
				f.owner, err = billing.NewRevenueService(nativeRepo, inputNativeClock{f.base.clock.at.Add(time.Second)})
				require.NoError(t, err)
				f.manager, f.ctx = f.worker(t, "worker-original")
				ctx = f.ctx
				p, err := f.manager.PrepareSubscriptionStatusForCheckout(ctx, "worker-original", f.p.Scope, f.p.SubscriptionID)
				require.NoError(t, err)
				require.NotEqual(t, p.CaptureID, f.p.CaptureID)
				input.Preparation = p
			case "missing_attached_original":
				id, _ := statusIdentity(f.p)
				_, err := f.base.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": statusKind, "id": id})
				require.NoError(t, err)
			case "lost_preparation_reply", "lost_evidence_reply":
				f.base.reply.lose = true
			case "replacement_preserves_original":
				clock.set(h.Until)
				next, err := r.Acquire(ctx, LeaseRequest{Source: j.Source, ExpectedRevision: j.Revision, Actor: "worker-replacement", Token: "replacement-binding-token", Until: clock.at.Add(time.Minute)})
				require.NoError(t, err)
				j = next
				h = JobLease(j)
			case "revision_exhaustion":
				h.Revision = math.MaxInt64
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			result, err := r.BindStatus(ctx, h, input)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, StatusBinding{}, result)
			} else {
				require.NoError(t, err)
				require.Equal(t, j.Revision+1, result.Job.Revision)
				require.Equal(t, j.Fence, result.Job.Fence)
				require.Equal(t, j.Attempts, result.Job.Attempts)
				require.Equal(t, j.Source, result.Job.Source)
				require.Equal(t, j.LeaseActor, result.Job.LeaseActor)
				require.Equal(t, j.LeaseToken, result.Job.LeaseToken)
				require.Equal(t, j.LeasedUntil, result.Job.LeasedUntil)
				require.Equal(t, j.NextAttemptAt, result.Job.NextAttemptAt)
				require.Equal(t, j.CreatedAt, result.Job.CreatedAt)
				require.Equal(t, f.p, *result.Job.OriginalStatus)
				require.Equal(t, f.p, result.Input.Preparation)
				if input.Evidence != nil || tc.name == "preparation_replay_preserves_evidence" || tc.name == "existing_outbox_attached_without_reset" {
					require.Equal(t, e, *result.Input.Evidence)
				}
			}
			f.base.reply.lose = false
			actual, err := r.ReadJob(context.WithoutCancel(f.ctx), j.Source)
			require.NoError(t, err)
			if tc.want != nil && tc.want != recordstore.ErrUncertain {
				require.Equal(t, j, actual)
			} else {
				require.Equal(t, j.Revision+1, actual.Revision)
				require.Equal(t, f.p, *actual.OriginalStatus)
			}
			// Native durable read proves both records survived or both rolled back;
			// bypassing service validation here is intentional adapter inspection.
			id, _ := statusIdentity(f.p)
			var retained StatusInput
			readErr := f.base.store.Read(context.WithoutCancel(f.ctx), func(tx recordstore.Tx) error {
				row, err := tx.Get(f.ctx, statusKind, id)
				if err != nil {
					return err
				}
				retained, err = decodeStatus(row, f.p)
				return err
			})
			if actual.OriginalStatus == nil && tc.name != "existing_outbox_attached_without_reset" {
				require.ErrorIs(t, readErr, recordstore.ErrNotFound)
			} else if tc.name == "missing_attached_original" {
				require.ErrorIs(t, readErr, recordstore.ErrNotFound)
			} else {
				require.NoError(t, readErr)
				require.Equal(t, f.p, retained.Preparation)
				if tc.name == "lost_evidence_reply" || tc.name == "different_evidence" {
					require.Equal(t, e, *retained.Evidence)
				}
			}
		})
	}
}

func TestStatusBindingEncryptedCompetingStages(t *testing.T) {
	for _, name := range []string{"competing_preparations", "competing_evidence"} {
		t.Run(name, func(t *testing.T) {
			f, r, _, j := nativeStatusBindingFixture(t)
			input := StatusInput{Preparation: f.p}
			if name == "competing_evidence" {
				first, err := r.BindStatus(f.ctx, JobLease(j), input)
				require.NoError(t, err)
				j = first.Job
				e := f.evidence(t)
				input.Evidence = &e
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for n := 0; n < 2; n++ {
				wg.Add(1)
				go func() { defer wg.Done(); <-start; _, err := r.BindStatus(f.ctx, JobLease(j), input); results <- err }()
			}
			close(start)
			wg.Wait()
			close(results)
			successes, conflicts := 0, 0
			for err := range results {
				if err == nil {
					successes++
				} else {
					require.ErrorIs(t, err, recordstore.ErrConflict)
					conflicts++
				}
			}
			require.Equal(t, 1, successes)
			require.Equal(t, 1, conflicts)
			actual, err := r.ReadJob(f.ctx, j.Source)
			require.NoError(t, err)
			require.Equal(t, j.Revision+1, actual.Revision)
			require.Equal(t, f.p, *actual.OriginalStatus)
		})
	}
}

type statusBindingFaultStore struct {
	recordstore.Store
	getErr, jobErr error
}
type statusBindingFaultTx struct {
	recordstore.Tx
	getErr, jobErr error
}

func (s statusBindingFaultStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	return s.Store.Transact(ctx, key, func(tx recordstore.Tx) error { return fn(statusBindingFaultTx{tx, s.getErr, s.jobErr}) })
}
func (tx statusBindingFaultTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	if kind == statusKind && tx.getErr != nil {
		return recordstore.Record{}, tx.getErr
	}
	return tx.Tx.Get(ctx, kind, id)
}
func (tx statusBindingFaultTx) Replace(ctx context.Context, row recordstore.Record, expected int64) error {
	if row.Kind == scheduleJobKind && tx.jobErr != nil {
		return tx.jobErr
	}
	return tx.Tx.Replace(ctx, row, expected)
}

func TestStatusBindingEncryptedLateRollback(t *testing.T) {
	for _, name := range []string{"preparation_job_failure", "evidence_job_failure", "joined_absence_outage", "joined_absence_unknown"} {
		t.Run(name, func(t *testing.T) {
			f, r, clock, j := nativeStatusBindingFixture(t)
			input := StatusInput{Preparation: f.p}
			if name == "evidence_job_failure" {
				first, err := r.BindStatus(f.ctx, JobLease(j), input)
				require.NoError(t, err)
				j = first.Job
				e := f.evidence(t)
				input.Evidence = &e
			}
			outage := errors.New("controlled failure at transaction boundary")
			store := statusBindingFaultStore{Store: f.base.reply}
			want := outage
			switch name {
			case "joined_absence_outage":
				store.getErr = errors.Join(recordstore.ErrNotFound, outage)
			case "joined_absence_unknown":
				store.getErr = errors.Join(recordstore.ErrNotFound, recordstore.ErrUncertain)
				want = recordstore.ErrUncertain
			default:
				store.jobErr = outage
			}
			broken, err := NewRecordExecutionRepository(store, clock)
			require.NoError(t, err)
			out, err := broken.BindStatus(f.ctx, JobLease(j), input)
			require.ErrorIs(t, err, want)
			require.Equal(t, StatusBinding{}, out)
			actual, err := r.ReadJob(f.ctx, j.Source)
			require.NoError(t, err)
			require.Equal(t, j, actual)
			id, _ := statusIdentity(f.p)
			err = f.base.store.Read(f.ctx, func(tx recordstore.Tx) error {
				row, err := tx.Get(f.ctx, statusKind, id)
				if err != nil {
					return err
				}
				old, err := decodeStatus(row, f.p)
				require.Nil(t, old.Evidence)
				return err
			})
			if name == "evidence_job_failure" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, recordstore.ErrNotFound)
			}
		})
	}
}

// Different guard names do not remove Mongo's overlapping-record transaction
// conflicts. Both legacy outbox and binding writers must conserve one response;
// runtime execution will use the binding port, never the standalone writer.
func TestStatusBindingEncryptedOutboxOverlap(t *testing.T) {
	for _, name := range []string{"same_evidence", "different_evidence"} {
		t.Run(name, func(t *testing.T) {
			f, r, _, j := nativeStatusBindingFixture(t)
			first, err := r.BindStatus(f.ctx, JobLease(j), StatusInput{Preparation: f.p})
			require.NoError(t, err)
			j = first.Job
			e := f.evidence(t)
			other := e
			if name == "different_evidence" {
				other.Status = "active"
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, err := r.BindStatus(f.ctx, JobLease(j), StatusInput{Preparation: f.p, Evidence: &e})
				results <- err
			}()
			go func() {
				defer wg.Done()
				<-start
				_, err := f.outbox.RetainEvidence(f.ctx, "worker-original", f.p, other)
				results <- err
			}()
			close(start)
			wg.Wait()
			close(results)
			successes := 0
			for err := range results {
				if err == nil {
					successes++
				} else {
					require.ErrorIs(t, err, recordstore.ErrConflict)
				}
			}
			if name == "same_evidence" {
				require.Equal(t, 2, successes)
			} else {
				require.Equal(t, 1, successes)
			}
			retained, err := f.outbox.Find(f.ctx, "worker-original", f.p)
			require.NoError(t, err)
			require.NotNil(t, retained.Evidence)
			actual, err := r.ReadJob(f.ctx, j.Source)
			require.NoError(t, err)
			require.Equal(t, f.p, *actual.OriginalStatus)
			if *retained.Evidence == e {
				require.Equal(t, j.Revision+1, actual.Revision)
			} else {
				require.Equal(t, j.Revision, actual.Revision)
			}
		})
	}
}
