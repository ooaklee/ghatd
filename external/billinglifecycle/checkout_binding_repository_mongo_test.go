package billinglifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var _ CheckoutBindingRepository = (*RecordExecutionRepository)(nil)

// Actual acknowledged native checkout and encrypted storage; source admission
// here is explicit fixture setup, not owning discovery or platform runtime.
func nativeCheckoutBindingFixture(t *testing.T) (*inputNativeFixture, *RecordExecutionRepository, *executionTestClock, ScheduledJob) {
	t.Helper()
	f := nativeInputFixture(t)
	clock := &executionTestClock{at: f.clock.at}
	r, err := NewRecordExecutionRepository(f.reply, clock)
	require.NoError(t, err)
	i := f.intent
	source := ScheduledSource{Scope: i.Scope, Kind: billing.LifecycleCheckoutSources, PrincipalID: i.Request.UserID, Checkout: &i, SourceID: billing.LifecycleDiscoverySourceID(i.Scope, billing.LifecycleCheckoutSources, i.ID)}
	j := ScheduledJob{Source: source, Revision: 1, Lane: ColdLane, CreatedAt: clock.at, NextAttemptAt: clock.at}
	require.NoError(t, r.CommitScan(f.ctx, ScheduleCommit{Scope: i.Scope, Lane: ColdLane, Writes: []ScheduleWrite{{0, j}}}))
	j, err = r.Acquire(f.ctx, LeaseRequest{Source: source, ExpectedRevision: 1, Actor: "worker-original", Token: "controlled-checkout-token", Until: clock.at.Add(time.Minute)})
	require.NoError(t, err)
	return f, r, clock, j
}

func TestCheckoutBindingEncryptedAtomicStages(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"preparation_and_marker", nil}, {"evidence_and_revision", nil}, {"exact_evidence_replay", nil}, {"preparation_replay_preserves_evidence", nil},
		{"existing_outbox_attached_without_reset", nil}, {"stale_revision", recordstore.ErrConflict}, {"wrong_actor", recordstore.ErrConflict},
		{"wrong_token", recordstore.ErrConflict}, {"wrong_fence", recordstore.ErrConflict}, {"wrong_lane", recordstore.ErrConflict}, {"wrong_expiry", recordstore.ErrConflict},
		{"expired", recordstore.ErrConflict}, {"expires_after_input_write", recordstore.ErrConflict}, {"clock_backwards_after_input_write", recordstore.ErrConflict},
		{"different_intent", recordstore.ErrInvalid}, {"evidence_without_marker", recordstore.ErrConflict}, {"missing_bound_original", recordstore.ErrUnavailable},
		{"lost_preparation_reply", recordstore.ErrUncertain}, {"lost_evidence_reply", recordstore.ErrUncertain}, {"different_evidence", recordstore.ErrConflict},
		{"consumed_handle_cannot_replace", recordstore.ErrConflict}, {"replacement_preserves_marker", nil}, {"replacement_replays_retained_evidence", nil}, {"revision_exhaustion", recordstore.ErrInvalid}, {"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, j := nativeCheckoutBindingFixture(t)
			h := JobLease(j)
			input := CheckoutInput{Intent: f.intent}
			ctx := f.ctx
			var e paymentprovider.RevenueCheckoutEvidence
			switch tc.name {
			case "evidence_and_revision", "exact_evidence_replay", "preparation_replay_preserves_evidence", "missing_bound_original", "lost_evidence_reply", "different_evidence", "consumed_handle_cannot_replace", "replacement_preserves_marker", "replacement_replays_retained_evidence":
				first, err := r.BindCheckout(ctx, h, input)
				require.NoError(t, err)
				j = first.Job
				h = JobLease(j)
				if tc.name == "consumed_handle_cannot_replace" {
					h.Revision--
				}
			}
			switch tc.name {
			case "evidence_and_revision", "exact_evidence_replay", "preparation_replay_preserves_evidence", "evidence_without_marker", "lost_evidence_reply", "different_evidence", "consumed_handle_cannot_replace", "existing_outbox_attached_without_reset", "replacement_replays_retained_evidence":
				var err error
				e, err = f.manager.LookupCheckoutLifecycleEvidence(ctx, "worker-original", f.intent)
				require.NoError(t, err)
				input.Evidence = &e
			}
			switch tc.name {
			case "replacement_replays_retained_evidence", "exact_evidence_replay", "preparation_replay_preserves_evidence", "different_evidence":
				first, err := r.BindCheckout(ctx, h, input)
				require.NoError(t, err)
				j = first.Job
				h = JobLease(j)
				switch tc.name {
				case "preparation_replay_preserves_evidence":
					input.Evidence = nil
				case "different_evidence":
					other := e
					other.CustomerID = "cus_other"
					input.Evidence = &other
				}
			case "existing_outbox_attached_without_reset":
				_, err := f.outbox.RetainPreparation(ctx, "worker-original", f.intent)
				require.NoError(t, err)
				_, err = f.outbox.RetainEvidence(ctx, "worker-original", f.intent, e)
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
			case "clock_backwards_after_input_write":
				clock.sequence = []time.Time{clock.at, clock.at.Add(-time.Second)}
			case "different_intent":
				q := f.intent.Request
				q.IdempotencyKey = "other-checkout"
				q.Metadata = map[string]string{}
				other, err := f.native.PrepareCheckout(ctx, f.intent.Scope, q)
				require.NoError(t, err)
				require.NoError(t, f.native.AcknowledgeCheckout(ctx, other, "cs_other"))
				input.Intent, err = f.native.FindCheckoutIntent(ctx, f.intent.Scope, q.IdempotencyKey)
				require.NoError(t, err)
			case "missing_bound_original":
				id, _ := checkoutIdentity(f.intent)
				_, err := f.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": checkoutKind, "id": id})
				require.NoError(t, err)
			case "lost_preparation_reply", "lost_evidence_reply":
				f.reply.lose = true
			case "replacement_preserves_marker":
				clock.set(h.Until)
				next, err := r.Acquire(ctx, LeaseRequest{Source: j.Source, ExpectedRevision: j.Revision, Actor: "worker-replacement", Token: "replacement-checkout-token", Until: clock.at.Add(time.Minute)})
				require.NoError(t, err)
				j = next
				h = JobLease(j)
				require.True(t, j.CheckoutPrepared)
			case "revision_exhaustion":
				h.Revision = math.MaxInt64
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if tc.name == "replacement_replays_retained_evidence" {
				clock.set(h.Until)
				next, err := r.Acquire(ctx, LeaseRequest{Source: j.Source, ExpectedRevision: j.Revision, Actor: "worker-replacement", Token: "replacement-evidence-token", Until: clock.at.Add(time.Minute)})
				require.NoError(t, err)
				j = next
				h = JobLease(j)
			}
			out, err := r.BindCheckout(ctx, h, input)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, CheckoutBinding{}, out)
			} else {
				require.NoError(t, err)
				require.True(t, out.Job.CheckoutPrepared)
				require.Equal(t, j.Revision+1, out.Job.Revision)
				require.Equal(t, j.Fence, out.Job.Fence)
				require.Equal(t, j.Attempts, out.Job.Attempts)
				require.Equal(t, j.Source, out.Job.Source)
				require.Equal(t, j.LeaseActor, out.Job.LeaseActor)
				require.Equal(t, j.LeaseToken, out.Job.LeaseToken)
				require.Equal(t, j.LeasedUntil, out.Job.LeasedUntil)
				require.Equal(t, j.NextAttemptAt, out.Job.NextAttemptAt)
				require.Equal(t, j.CreatedAt, out.Job.CreatedAt)
				require.Equal(t, f.intent, out.Input.Intent)
				if input.Evidence != nil || tc.name == "preparation_replay_preserves_evidence" || tc.name == "existing_outbox_attached_without_reset" {
					require.Equal(t, e, *out.Input.Evidence)
				}
			}
			f.reply.lose = false
			actual, err := r.ReadJob(context.WithoutCancel(f.ctx), j.Source)
			require.NoError(t, err)
			if tc.want != nil && tc.want != recordstore.ErrUncertain {
				require.Equal(t, j, actual)
			} else {
				require.True(t, actual.CheckoutPrepared)
				require.Equal(t, j.Revision+1, actual.Revision)
			}
			id, _ := checkoutIdentity(f.intent)
			var retained CheckoutInput
			readErr := f.store.Read(context.WithoutCancel(f.ctx), func(tx recordstore.Tx) error {
				row, err := tx.Get(f.ctx, checkoutKind, id)
				if err != nil {
					return err
				}
				retained, err = decode(row, f.intent)
				return err
			})
			if !actual.CheckoutPrepared || tc.name == "missing_bound_original" {
				require.ErrorIs(t, readErr, recordstore.ErrNotFound)
			} else {
				require.NoError(t, readErr)
				require.Equal(t, f.intent, retained.Intent)
				if tc.name == "lost_evidence_reply" || tc.name == "different_evidence" {
					require.Equal(t, e, *retained.Evidence)
				}
			}
		})
	}
}

type checkoutBindingFaultTx struct {
	recordstore.Tx
	getErr, jobErr, inputErr error
}
type checkoutBindingFaultStore struct {
	recordstore.Store
	getErr, jobErr, inputErr error
}

func (s checkoutBindingFaultStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	return s.Store.Transact(ctx, key, func(tx recordstore.Tx) error { return fn(checkoutBindingFaultTx{tx, s.getErr, s.jobErr, s.inputErr}) })
}
func (tx checkoutBindingFaultTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	if kind == checkoutKind && tx.getErr != nil {
		return recordstore.Record{}, tx.getErr
	}
	return tx.Tx.Get(ctx, kind, id)
}
func (tx checkoutBindingFaultTx) Replace(ctx context.Context, row recordstore.Record, expected int64) error {
	if row.Kind == checkoutKind && tx.inputErr != nil {
		return tx.inputErr
	}
	if row.Kind == scheduleJobKind && tx.jobErr != nil {
		return tx.jobErr
	}
	return tx.Tx.Replace(ctx, row, expected)
}

func TestCheckoutBindingEncryptedLateRollback(t *testing.T) {
	for _, name := range []string{"preparation_job_failure", "evidence_job_failure", "evidence_input_failure", "joined_absence_outage", "joined_absence_unknown"} {
		t.Run(name, func(t *testing.T) {
			f, r, clock, j := nativeCheckoutBindingFixture(t)
			input := CheckoutInput{Intent: f.intent}
			if name == "evidence_job_failure" || name == "evidence_input_failure" {
				first, err := r.BindCheckout(f.ctx, JobLease(j), input)
				require.NoError(t, err)
				j = first.Job
				e, err := f.manager.LookupCheckoutLifecycleEvidence(f.ctx, "worker-original", f.intent)
				require.NoError(t, err)
				input.Evidence = &e
			}
			outage := errors.New("controlled checkout transaction boundary failure")
			store := checkoutBindingFaultStore{Store: f.reply}
			want := outage
			switch name {
			case "joined_absence_outage":
				store.getErr = errors.Join(recordstore.ErrNotFound, outage)
			case "joined_absence_unknown":
				store.getErr = errors.Join(recordstore.ErrNotFound, recordstore.ErrUncertain)
				want = recordstore.ErrUncertain
			case "evidence_input_failure":
				store.inputErr = outage
			default:
				store.jobErr = outage
			}
			broken, err := NewRecordExecutionRepository(store, clock)
			require.NoError(t, err)
			out, err := broken.BindCheckout(f.ctx, JobLease(j), input)
			require.ErrorIs(t, err, want)
			require.Equal(t, CheckoutBinding{}, out)
			actual, err := r.ReadJob(f.ctx, j.Source)
			require.NoError(t, err)
			require.Equal(t, j, actual)
			id, _ := checkoutIdentity(f.intent)
			err = f.store.Read(f.ctx, func(tx recordstore.Tx) error {
				row, err := tx.Get(f.ctx, checkoutKind, id)
				if err != nil {
					return err
				}
				old, err := decode(row, f.intent)
				require.Nil(t, old.Evidence)
				return err
			})
			if name == "evidence_job_failure" || name == "evidence_input_failure" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, recordstore.ErrNotFound)
			}
		})
	}
}

func TestCheckoutBindingEncryptedCompetingStages(t *testing.T) {
	for _, name := range []string{"competing_preparations", "competing_evidence"} {
		t.Run(name, func(t *testing.T) {
			f, r, _, j := nativeCheckoutBindingFixture(t)
			input := CheckoutInput{Intent: f.intent}
			if name == "competing_evidence" {
				first, err := r.BindCheckout(f.ctx, JobLease(j), input)
				require.NoError(t, err)
				j = first.Job
				e, err := f.manager.LookupCheckoutLifecycleEvidence(f.ctx, "worker-original", f.intent)
				require.NoError(t, err)
				input.Evidence = &e
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for n := 0; n < 2; n++ {
				wg.Add(1)
				go func() { defer wg.Done(); <-start; _, err := r.BindCheckout(f.ctx, JobLease(j), input); results <- err }()
			}
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
			require.Equal(t, 1, successes)
			actual, err := r.ReadJob(f.ctx, j.Source)
			require.NoError(t, err)
			require.True(t, actual.CheckoutPrepared)
			require.Equal(t, j.Revision+1, actual.Revision)
		})
	}
}

func TestCheckoutBindingEncryptedMarkerContract(t *testing.T) {
	for _, name := range []string{"legacy_absent_marker", "marker_on_subscription_refused", "bootstrap_cannot_set_marker", "release_acquire_conserves_marker"} {
		t.Run(name, func(t *testing.T) {
			f, r, clock, j := nativeCheckoutBindingFixture(t)
			switch name {
			case "legacy_absent_marker":
				row, err := jobRecord(j)
				require.NoError(t, err)
				var raw map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(row.Data, &raw))
				delete(raw, "CheckoutPrepared")
				row.Data, err = json.Marshal(raw)
				require.NoError(t, err)
				round, err := readJob(row)
				require.NoError(t, err)
				require.False(t, round.CheckoutPrepared)
				require.Equal(t, j, round)
				row.Revision++
				require.NoError(t, f.store.Transact(f.ctx, scheduleJobKind+":"+row.ID, func(tx recordstore.Tx) error { return tx.Replace(f.ctx, row, j.Revision) }))
				j.Revision++
				persisted, err := r.ReadJob(f.ctx, j.Source)
				require.NoError(t, err)
				require.Equal(t, j, persisted)
				require.False(t, persisted.CheckoutPrepared)
				bound, err := r.BindCheckout(f.ctx, JobLease(persisted), CheckoutInput{Intent: f.intent})
				require.NoError(t, err)
				require.True(t, bound.Job.CheckoutPrepared)
				require.Equal(t, persisted.Revision+1, bound.Job.Revision)
			case "marker_on_subscription_refused":
				j.Source = ScheduledSource{Scope: f.intent.Scope, Kind: billing.LifecycleSubscriptionSources, PrincipalID: j.Source.PrincipalID, SubscriptionID: "sub_original", SourceID: billing.LifecycleDiscoverySourceID(f.intent.Scope, billing.LifecycleSubscriptionSources, "sub_original")}
				j.CheckoutPrepared = true
				_, err := jobRecord(j)
				require.ErrorIs(t, err, recordstore.ErrInvalid)
			case "bootstrap_cannot_set_marker":
				j.Revision = 1
				j.Attempts = 0
				j.Fence = 0
				j.LeaseActor = ""
				j.LeaseToken = ""
				j.LeasedUntil = time.Time{}
				j.CheckoutPrepared = true
				err := r.CommitScan(f.ctx, ScheduleCommit{Scope: f.intent.Scope, Lane: ColdLane, Writes: []ScheduleWrite{{0, j}}})
				require.ErrorIs(t, err, recordstore.ErrInvalid)
			case "release_acquire_conserves_marker":
				first, err := r.BindCheckout(f.ctx, JobLease(j), CheckoutInput{Intent: f.intent})
				require.NoError(t, err)
				j = first.Job
				due := clock.at.Add(time.Second)
				released, err := r.Release(f.ctx, LeaseDisposition{Handle: JobLease(j), NextAttemptAt: due, Lane: RefreshLane})
				require.NoError(t, err)
				require.True(t, released.CheckoutPrepared)
				clock.set(due)
				next, err := r.Acquire(f.ctx, LeaseRequest{Source: released.Source, ExpectedRevision: released.Revision, Actor: "worker-replacement", Token: "replacement-token", Until: due.Add(time.Minute)})
				require.NoError(t, err)
				require.True(t, next.CheckoutPrepared)
				out, err := r.BindCheckout(f.ctx, JobLease(next), CheckoutInput{Intent: *next.Source.Checkout})
				require.NoError(t, err)
				require.True(t, out.Job.CheckoutPrepared)
				require.Equal(t, f.intent, out.Input.Intent)
			}
		})
	}
}

func TestCheckoutBindingEncryptedIntegrity(t *testing.T) {
	for _, name := range []string{"corrupt_outbox_metadata", "corrupt_job_metadata", "corrupt_outbox_codec", "corrupt_job_codec", "subscription_kind_refused"} {
		t.Run(name, func(t *testing.T) {
			f, r, _, j := nativeCheckoutBindingFixture(t)
			input := CheckoutInput{Intent: f.intent}
			h := JobLease(j)
			if name == "subscription_kind_refused" {
				h.Source = ScheduledSource{Scope: f.intent.Scope, Kind: billing.LifecycleSubscriptionSources, PrincipalID: j.Source.PrincipalID, SubscriptionID: "sub_original", SourceID: billing.LifecycleDiscoverySourceID(f.intent.Scope, billing.LifecycleSubscriptionSources, "sub_original")}
				out, err := r.BindCheckout(f.ctx, h, input)
				require.ErrorIs(t, err, recordstore.ErrInvalid)
				require.Equal(t, CheckoutBinding{}, out)
				actual, err := r.ReadJob(f.ctx, j.Source)
				require.NoError(t, err)
				require.Equal(t, j, actual)
				return
			}
			if name == "corrupt_outbox_metadata" || name == "corrupt_outbox_codec" {
				first, err := r.BindCheckout(f.ctx, h, input)
				require.NoError(t, err)
				j = first.Job
				h = JobLease(j)
			}
			kind, id := checkoutKind, ""
			id, _ = checkoutIdentity(f.intent)
			if name == "corrupt_job_metadata" || name == "corrupt_job_codec" {
				kind, id = scheduleJobKind, scheduledIdentity(j.Source)
			}
			collection := f.db.Collection("ghatd_owned_records")
			var err error
			want := error(encryption.ErrInvalidPayload)
			if name == "corrupt_outbox_codec" || name == "corrupt_job_codec" {
				want = recordstore.ErrUnavailable
				err = f.store.Transact(f.ctx, kind+":"+id, func(tx recordstore.Tx) error {
					row, err := tx.Get(f.ctx, kind, id)
					if err != nil {
						return err
					}
					expected := row.Revision
					row.Revision++
					var data map[string]json.RawMessage
					if err := json.Unmarshal(row.Data, &data); err != nil {
						return err
					}
					data["Schema"] = json.RawMessage("2")
					row.Data, err = json.Marshal(data)
					if err != nil {
						return err
					}
					return tx.Replace(f.ctx, row, expected)
				})
			} else {
				_, err = collection.UpdateOne(f.ctx, bson.M{"kind": kind, "id": id}, bson.M{"$set": bson.M{"state": "corrupted"}})
			}
			require.NoError(t, err)

			var before bson.Raw
			require.NoError(t, collection.FindOne(f.ctx, bson.M{"kind": scheduleJobKind, "id": scheduledIdentity(j.Source)}).Decode(&before))
			out, err := r.BindCheckout(f.ctx, h, input)
			require.ErrorIs(t, err, want)
			require.Equal(t, CheckoutBinding{}, out)
			var after bson.Raw
			require.NoError(t, collection.FindOne(f.ctx, bson.M{"kind": scheduleJobKind, "id": scheduledIdentity(j.Source)}).Decode(&after))
			require.Equal(t, before, after)
			if name == "corrupt_job_metadata" || name == "corrupt_job_codec" {
				n, err := collection.CountDocuments(f.ctx, bson.M{"kind": checkoutKind})
				require.NoError(t, err)
				require.Zero(t, n)
			} else {
				actual, err := r.ReadJob(f.ctx, j.Source)
				require.NoError(t, err)
				require.Equal(t, j, actual)
			}
		})
	}
}
