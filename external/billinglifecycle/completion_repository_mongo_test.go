package billinglifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var _ CompletionRepository = (*RecordExecutionRepository)(nil)

// Fresh native originals/current grants/manager captures and encrypted storage
// per case. Source admission and API identity/provider responses are fixtures.
// No runtime, deployment or provider-network evidence is claimed here.
type completionNativeFixture struct {
	base            *inputNativeFixture
	status          *statusNativeFixture
	repo            *RecordExecutionRepository
	clock           *executionTestClock
	ctx             context.Context
	checkout        CheckoutObservation
	observation     StatusObservation
	checkoutService *CheckoutCompletion
	statusService   *StatusCompletionService
}

func nativeCompletionFixture(t *testing.T, kind string) *completionNativeFixture {
	t.Helper()
	f := &completionNativeFixture{}
	if kind == "checkout" {
		base, r, clock, j := nativeCheckoutBindingFixture(t)
		f.base, f.repo, f.clock = base, r, clock
		b, ctx := nativeCheckoutBinder(t, base, r, clock, "worker-original")
		f.ctx = ctx
		outbox, err := NewCheckoutOutbox(base.reply, b.validator)
		require.NoError(t, err)
		x, err := NewCheckoutExecution(b.scheduler, r, outbox)
		require.NoError(t, err)
		f.checkout, err = x.Observe(ctx, JobLease(j))
		require.NoError(t, err)
		f.checkoutService, err = NewCheckoutCompletion(x, r)
		require.NoError(t, err)
	} else {
		status, r, clock, j := nativeStatusBindingFixture(t)
		f.status, f.base, f.repo, f.clock = status, status.base, r, clock
		b, ctx := nativeStatusBinder(t, status, r, clock, "worker-original")
		f.ctx = ctx
		outbox, err := NewStatusOutbox(status.base.reply, b.validator)
		require.NoError(t, err)
		x, err := NewStatusExecution(b.scheduler, r, outbox)
		require.NoError(t, err)
		f.observation, err = x.Observe(ctx, JobLease(j))
		require.NoError(t, err)
		f.statusService, err = NewStatusCompletion(x, r, 30*time.Second)
		require.NoError(t, err)
	}
	return f
}
func (f *completionNativeFixture) job() ScheduledJob {
	if f.status != nil {
		return f.observation.Job
	}
	return f.checkout.Job
}
func (f *completionNativeFixture) key() CompletionKey {
	if f.status != nil {
		return CompletionKey{f.observation.Job.Source, f.observation.Input.Preparation.CaptureID}
	}
	return CompletionKey{f.checkout.Job.Source, f.checkout.Input.Intent.ID}
}
func (f *completionNativeFixture) complete(ctx context.Context, r *RecordExecutionRepository) (CompletedExecution, error) {
	if f.status != nil {
		return r.CompleteStatus(ctx, StatusCompletion{f.observation, f.clock.at.Add(30 * time.Second)})
	}
	return r.CompleteCheckout(ctx, f.checkout)
}

func TestCompletionRepositoryEncryptedAtomicTransition(t *testing.T) {
	for _, kind := range []string{"checkout", "status"} {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"confirmed", nil}, {"lost_reply_inspects_committed_identity", recordstore.ErrUncertain},
			{"stale_revision", recordstore.ErrConflict}, {"wrong_actor", recordstore.ErrConflict}, {"wrong_token", recordstore.ErrConflict}, {"wrong_epoch", recordstore.ErrConflict}, {"wrong_expiry", recordstore.ErrConflict},
			{"expired", recordstore.ErrConflict}, {"expires_after_completion_insert", recordstore.ErrConflict}, {"clock_backwards_after_insert", recordstore.ErrConflict},
			{"missing_bound_outbox", recordstore.ErrUnavailable}, {"different_retained_evidence", recordstore.ErrConflict}, {"missing_observed_evidence", recordstore.ErrInvalid},
			{"changed_job_creation", recordstore.ErrConflict}, {"changed_job_attempts", recordstore.ErrConflict}, {"revision_exhaustion", recordstore.ErrInvalid}, {"canceled", context.Canceled},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				f := nativeCompletionFixture(t, kind)
				original := f.job()
				ctx := f.ctx
				modify := func(fn func(*ScheduledJob)) {
					if f.status != nil {
						fn(&f.observation.Job)
					} else {
						fn(&f.checkout.Job)
					}
				}
				switch tc.name {
				case "lost_reply_inspects_committed_identity":
					f.base.reply.lose = true
				case "stale_revision":
					modify(func(j *ScheduledJob) { j.Revision-- })
				case "wrong_actor":
					modify(func(j *ScheduledJob) { j.LeaseActor = "worker-replacement" })
				case "wrong_token":
					modify(func(j *ScheduledJob) { j.LeaseToken = "other-token" })
				case "wrong_epoch":
					modify(func(j *ScheduledJob) { j.Fence++ })
				case "wrong_expiry":
					modify(func(j *ScheduledJob) { j.LeasedUntil = j.LeasedUntil.Add(time.Second) })
				case "expired":
					f.clock.set(original.LeasedUntil)
				case "expires_after_completion_insert":
					f.clock.sequence = []time.Time{f.clock.at, original.LeasedUntil}
				case "clock_backwards_after_insert":
					f.clock.sequence = []time.Time{f.clock.at, f.clock.at.Add(-time.Second)}
				case "missing_bound_outbox":
					var id, kind string
					if f.status != nil {
						id, _ = statusIdentity(f.observation.Input.Preparation)
						kind = statusKind
					} else {
						id, _ = checkoutIdentity(f.checkout.Input.Intent)
						kind = checkoutKind
					}
					_, err := f.base.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": kind, "id": id})
					require.NoError(t, err)
				case "different_retained_evidence":
					// Change encrypted retained evidence without changing the observed original.
					// The completion must join the ACTUAL outbox inside its transaction.
					if f.status != nil {
						changed := f.observation.Input
						v := *changed.Evidence
						v.Status = "active"
						changed.Evidence = &v
						row, err := encodeStatus(changed)
						require.NoError(t, err)
						replaceCompletionFixtureRecord(t, f, ctx, row)
					} else {
						changed := f.checkout.Input
						v := *changed.Evidence
						v.CreatedAt = v.CreatedAt.Add(time.Second)
						changed.Evidence = &v
						row, err := encode(changed)
						require.NoError(t, err)
						replaceCompletionFixtureRecord(t, f, ctx, row)
					}
				case "missing_observed_evidence":
					if f.status != nil {
						f.observation.Input.Evidence = nil
					} else {
						f.checkout.Input.Evidence = nil
					}
				case "changed_job_creation":
					modify(func(j *ScheduledJob) { j.CreatedAt = j.CreatedAt.Add(-time.Second) })
				case "changed_job_attempts":
					modify(func(j *ScheduledJob) { j.Attempts++ })
				case "revision_exhaustion":
					modify(func(j *ScheduledJob) { j.Revision = math.MaxInt64 })
				case "canceled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				beforeGets := f.base.provider.calls
				if f.status != nil {
					beforeGets = f.status.provider.calls
				}
				out, err := f.complete(ctx, f.repo)
				if tc.want != nil {
					require.ErrorIs(t, err, tc.want)
					require.Equal(t, CompletedExecution{}, out)
				} else {
					require.NoError(t, err)
				}
				if f.status != nil {
					require.Equal(t, beforeGets, f.status.provider.calls)
				} else {
					require.Equal(t, beforeGets, f.base.provider.calls)
				}
				f.base.reply.lose = false
				inspect := context.WithoutCancel(f.ctx)
				current, err := f.repo.ReadJob(inspect, original.Source)
				require.NoError(t, err)
				committed := tc.want == nil || tc.want == recordstore.ErrUncertain
				record, readErr := f.repo.FindLastCompletion(inspect, original.Source)
				if committed {
					require.NoError(t, readErr)
					require.Equal(t, original.Revision+1, current.Revision)
					require.Equal(t, original.Fence, current.Fence)
					require.NotEmpty(t, current.LastCompletionID)
					require.Empty(t, current.LeaseActor)
					require.Empty(t, current.LeaseToken)
					require.True(t, current.LeasedUntil.IsZero())
					require.True(t, sameExecutionJob(current, record.Job))
					require.NoError(t, validateScheduledJob(current))
					if kind == "checkout" {
						require.Equal(t, RetiredLane, current.Lane)
						require.True(t, current.CheckoutPrepared)
						require.Equal(t, original.Attempts, current.Attempts)
					} else {
						require.Equal(t, RefreshLane, current.Lane)
						require.Zero(t, current.Attempts)
						require.Nil(t, current.OriginalStatus)
						require.Equal(t, f.observation.Input.Preparation.RequestedAt, current.CadenceAnchor)
						require.NotNil(t, record.OriginalStatus)
					}
					// Completion history is immutable; consumed handles never blind-replay a write.
					again, e := f.complete(inspect, f.repo)
					require.ErrorIs(t, e, recordstore.ErrConflict)
					require.Equal(t, CompletedExecution{}, again)
					byKey, e := f.repo.FindCompletion(inspect, f.key())
					require.NoError(t, e)
					require.True(t, sameCompletion(record, byKey))
				} else {
					require.ErrorIs(t, readErr, recordstore.ErrNotFound)
					require.True(t, sameExecutionJob(original, current))
				}
				count, e := f.base.db.Collection("ghatd_owned_records").CountDocuments(inspect, bson.M{"kind": completionKind})
				require.NoError(t, e)
				if committed {
					require.EqualValues(t, 1, count)
				} else {
					require.Zero(t, count)
				}
				count, e = f.base.db.Collection("ghatd_owned_records").CountDocuments(inspect, bson.M{"kind": "billing_revenue_fact"})
				require.NoError(t, e)
				require.Zero(t, count)
			})
		}
	}
}

type completionFaultStore struct {
	recordstore.Store
	getErr, insertErr, jobErr error
}
type completionFaultTx struct {
	recordstore.Tx
	getErr, insertErr, jobErr error
}

func (s completionFaultStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	return s.Store.Transact(ctx, key, func(tx recordstore.Tx) error { return fn(completionFaultTx{tx, s.getErr, s.insertErr, s.jobErr}) })
}
func (t completionFaultTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	if kind == completionKind && t.getErr != nil {
		return recordstore.Record{}, t.getErr
	}
	return t.Tx.Get(ctx, kind, id)
}
func (t completionFaultTx) Insert(ctx context.Context, row recordstore.Record) error {
	if row.Kind == completionKind && t.insertErr != nil {
		return t.insertErr
	}
	return t.Tx.Insert(ctx, row)
}
func (t completionFaultTx) Replace(ctx context.Context, row recordstore.Record, revision int64) error {
	if row.Kind == scheduleJobKind && t.jobErr != nil {
		return t.jobErr
	}
	return t.Tx.Replace(ctx, row, revision)
}

func TestCompletionRepositoryEncryptedLateRollback(t *testing.T) {
	for _, kind := range []string{"checkout", "status"} {
		for _, name := range []string{"completion_insert_failure", "job_write_failure", "joined_completion_absence_outage", "joined_completion_absence_unknown"} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				f := nativeCompletionFixture(t, kind)
				outage := errors.New("controlled late completion failure")
				fault := completionFaultStore{Store: f.base.reply}
				want := outage
				switch name {
				case "completion_insert_failure":
					fault.insertErr = outage
				case "job_write_failure":
					fault.jobErr = outage
				case "joined_completion_absence_outage":
					fault.getErr = errors.Join(recordstore.ErrNotFound, outage)
				case "joined_completion_absence_unknown":
					fault.getErr = errors.Join(recordstore.ErrNotFound, recordstore.ErrUncertain)
					want = recordstore.ErrUncertain
				}
				broken, err := NewRecordExecutionRepository(fault, f.clock)
				require.NoError(t, err)
				out, err := f.complete(f.ctx, broken)
				require.ErrorIs(t, err, want)
				require.Equal(t, CompletedExecution{}, out)
				current, err := f.repo.ReadJob(f.ctx, f.job().Source)
				require.NoError(t, err)
				require.True(t, sameExecutionJob(current, f.job()))
				_, err = f.repo.FindLastCompletion(f.ctx, f.job().Source)
				require.ErrorIs(t, err, recordstore.ErrNotFound)
				n, err := f.base.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": completionKind})
				require.NoError(t, err)
				require.Zero(t, n)
			})
		}
	}
}

func TestCompletionRepositoryPrivateCodecAndHistoryIntegrity(t *testing.T) {
	for _, kind := range []string{"checkout", "status"} {
		for _, name := range []string{"ciphertext_only", "missing_referenced_completion", "corrupt_metadata", "bad_codec", "old_shape_defaults_empty_reference", "bootstrap_cannot_fabricate_completion"} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				f := nativeCompletionFixture(t, kind)
				if name == "bootstrap_cannot_fabricate_completion" {
					j := f.job()
					j.Revision = 1
					j.Fence = 0
					j.Attempts = 0
					j.LeaseActor = ""
					j.LeaseToken = ""
					j.LeasedUntil = time.Time{}
					j.OriginalStatus = nil
					j.CheckoutPrepared = false
					j.LastCompletionID = digest([]string{"fake"})
					err := f.repo.CommitScan(f.ctx, ScheduleCommit{Scope: j.Source.Scope, Lane: ColdLane, Writes: []ScheduleWrite{{0, j}}})
					require.ErrorIs(t, err, recordstore.ErrInvalid)
					return
				}
				if name == "old_shape_defaults_empty_reference" {
					row, err := jobRecord(f.job())
					require.NoError(t, err)
					var raw map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(row.Data, &raw))
					delete(raw, "LastCompletionID")
					delete(raw, "CadenceAnchor")
					row.Data, err = json.Marshal(raw)
					require.NoError(t, err)
					row.Revision++
					require.NoError(t, f.base.store.Transact(f.ctx, "legacy-local-row", func(tx recordstore.Tx) error { return tx.Replace(f.ctx, row, row.Revision-1) }))
					j, err := f.repo.ReadJob(f.ctx, f.job().Source)
					require.NoError(t, err)
					require.Empty(t, j.LastCompletionID)
					require.True(t, j.CadenceAnchor.IsZero())
					return
				}
				out, err := f.complete(f.ctx, f.repo)
				require.NoError(t, err)
				id := out.Job.LastCompletionID
				switch name {
				case "ciphertext_only":
					var raw bson.Raw
					require.NoError(t, f.base.db.Collection("ghatd_owned_records").FindOne(f.ctx, bson.M{"kind": completionKind, "id": id}).Decode(&raw))
					require.Equal(t, bson.TypeBinary, raw.Lookup("payload").Type)
					require.NotContains(t, string(raw), "worker-original")
					require.NotContains(t, string(raw), "cus_original")
				case "missing_referenced_completion":
					_, err = f.base.db.Collection("ghatd_owned_records").DeleteOne(f.ctx, bson.M{"kind": completionKind, "id": id})
					require.NoError(t, err)
					_, err = f.repo.FindLastCompletion(f.ctx, f.job().Source)
					require.ErrorIs(t, err, recordstore.ErrUnavailable)
				case "corrupt_metadata":
					_, err = f.base.db.Collection("ghatd_owned_records").UpdateOne(f.ctx, bson.M{"kind": completionKind, "id": id}, bson.M{"$set": bson.M{"state": "other"}})
					require.NoError(t, err)
					_, err = f.repo.FindLastCompletion(f.ctx, f.job().Source)
					require.Error(t, err)
				case "bad_codec":
					row, err := encodeCompletion(out)
					require.NoError(t, err)
					var p completionPayload
					require.NoError(t, json.Unmarshal(row.Data, &p))
					p.Schema = 2
					row.Data, err = json.Marshal(p)
					require.NoError(t, err)
					replaceCompletionFixtureRecord(t, f, f.ctx, row)
					_, err = f.repo.FindLastCompletion(f.ctx, f.job().Source)
					require.ErrorIs(t, err, recordstore.ErrUnavailable)
				}
			})
		}
	}
}

// Controlled corruption setup removes the old encrypted row then reinserts
// through the native encrypting adapter. It is not a supported runtime write;
// preserving metadata isolates payload validation from revision validation.
func replaceCompletionFixtureRecord(t *testing.T, f *completionNativeFixture, ctx context.Context, row recordstore.Record) {
	t.Helper()
	_, err := f.base.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": row.Kind, "id": row.ID})
	require.NoError(t, err)
	require.NoError(t, f.base.store.Transact(ctx, "controlled-fixture-reinsert", func(tx recordstore.Tx) error { return tx.Insert(ctx, row) }))
}

func TestCompletionRepositoryCompetingConfirmations(t *testing.T) {
	for _, kind := range []string{"checkout", "status"} {
		t.Run(kind, func(t *testing.T) {
			f := nativeCompletionFixture(t, kind)
			type result struct {
				out CompletedExecution
				err error
			}
			results := make(chan result, 2)
			start := make(chan struct{})
			for i := 0; i < 2; i++ {
				go func() { <-start; out, err := f.complete(f.ctx, f.repo); results <- result{out, err} }()
			}
			close(start)
			successes := 0
			for i := 0; i < 2; i++ {
				r := <-results
				if r.err == nil {
					successes++
					require.NotEmpty(t, r.out.Job.LastCompletionID)
				} else {
					require.ErrorIs(t, r.err, recordstore.ErrConflict)
					require.Equal(t, CompletedExecution{}, r.out)
				}
			}
			require.Equal(t, 1, successes)
			current, err := f.repo.ReadJob(f.ctx, f.job().Source)
			require.NoError(t, err)
			require.Equal(t, f.job().Revision+1, current.Revision)
			count, err := f.base.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": completionKind})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestStatusCompletionDeadlineInsideTransaction(t *testing.T) {
	for _, name := range []string{"due_already_elapsed", "due_elapses_after_insert"} {
		t.Run(name, func(t *testing.T) {
			f := nativeCompletionFixture(t, "status")
			due := f.clock.at
			if name == "due_elapses_after_insert" {
				due = due.Add(30 * time.Second)
				f.clock.sequence = []time.Time{f.clock.at, due}
			}
			out, err := f.repo.CompleteStatus(f.ctx, StatusCompletion{f.observation, due})
			require.ErrorIs(t, err, recordstore.ErrConflict)
			require.Equal(t, CompletedExecution{}, out)
			current, e := f.repo.ReadJob(f.ctx, f.job().Source)
			require.NoError(t, e)
			require.True(t, sameExecutionJob(f.job(), current))
			_, e = f.repo.FindLastCompletion(f.ctx, f.job().Source)
			require.ErrorIs(t, e, recordstore.ErrNotFound)
		})
	}
}
