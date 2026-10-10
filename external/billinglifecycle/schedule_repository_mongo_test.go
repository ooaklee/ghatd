package billinglifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var _ ScheduleRepository = (*RecordScheduleRepository)(nil)

// Native encrypted storage only: source descriptors in these adapter cases are
// fixtures, not proof of owning discovery or current service authorization.
// The scheduling service must establish those before calling this private port.
func nativeScheduleFixture(t *testing.T) (*inputNativeFixture, *RecordScheduleRepository, []ScheduledJob) {
	t.Helper()
	f := nativeInputFixture(t)
	r, err := NewRecordScheduleRepository(f.reply)
	require.NoError(t, err)
	var jobs []ScheduledJob
	for _, sub := range []string{"sub_one", "sub_two", "sub_three"} {
		s := ScheduledSource{Scope: f.intent.Scope, Kind: billing.LifecycleSubscriptionSources, PrincipalID: "native-payer", SubscriptionID: sub, SourceID: billing.LifecycleDiscoverySourceID(f.intent.Scope, billing.LifecycleSubscriptionSources, sub)}
		jobs = append(jobs, ScheduledJob{Source: s, Revision: 1, Lane: ColdLane, CreatedAt: f.clock.at, NextAttemptAt: f.clock.at})
	}
	sort.Slice(jobs, func(a, b int) bool { return scheduledIdentity(jobs[a].Source) < scheduledIdentity(jobs[b].Source) })
	return f, r, jobs
}
func scheduleWrites(jobs []ScheduledJob, expected int64) []ScheduleWrite {
	var writes []ScheduleWrite
	for _, j := range jobs {
		writes = append(writes, ScheduleWrite{expected, j})
	}
	return writes
}

func TestScheduleRepositoryEncryptedAtomicCursorJobs(t *testing.T) {
	for _, scenario := range []string{"atomic_round_trip", "empty_read_is_not_persisted_cursor", "lost_commit_reply", "stale_cursor", "late_insert_rolls_back_all", "stale_job_rolls_back_cursor", "immutable_source", "immutable_creation_time", "lane_transition", "wrong_scope_refused", "duplicate_job_refused", "canceled", "bounded_ordered_page"} {
		t.Run(scenario, func(t *testing.T) {
			f, r, jobs := nativeScheduleFixture(t)
			q := ScheduleScan{f.intent.Scope, ColdLane, 200}
			before, err := r.ReadScan(f.ctx, q)
			require.NoError(t, err)
			require.Equal(t, ScheduleCursor{}, before.Cursor)
			require.Empty(t, before.Jobs)
			c := ScheduleCommit{Scope: q.Scope, Lane: q.Lane, Writes: scheduleWrites(jobs, 0)}
			if scenario == "empty_read_is_not_persisted_cursor" {
				n, err := f.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": scheduleCursorKind})
				require.NoError(t, err)
				require.Zero(t, n)
				return
			}
			if scenario == "wrong_scope_refused" {
				c.Writes[1].Job.Source.Scope.AccountID = "different"
				require.Error(t, r.CommitScan(f.ctx, c))
				return
			}
			if scenario == "duplicate_job_refused" {
				c.Writes = append(c.Writes, c.Writes[0])
				require.ErrorIs(t, r.CommitScan(f.ctx, c), recordstore.ErrInvalid)
				return
			}
			if scenario == "canceled" {
				ctx, cancel := context.WithCancel(f.ctx)
				cancel()
				require.ErrorIs(t, r.CommitScan(ctx, c), context.Canceled)
				out, err := r.ReadScan(ctx, q)
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, ScheduleSnapshot{}, out)
				return
			}
			if scenario == "late_insert_rolls_back_all" {
				first := c
				first.Writes = []ScheduleWrite{c.Writes[2]}
				require.NoError(t, r.CommitScan(f.ctx, first))
				c.Expected = ScheduleCursor{Revision: 1}
				require.ErrorIs(t, r.CommitScan(f.ctx, c), recordstore.ErrConflict)
				out, err := r.ReadScan(f.ctx, q)
				require.NoError(t, err)
				require.Equal(t, ScheduleCursor{Revision: 1}, out.Cursor)
				require.Len(t, out.Jobs, 1)
				require.Equal(t, jobs[2], out.Jobs[0])
				return
			}
			if scenario == "lost_commit_reply" {
				f.reply.lose = true
			}
			err = r.CommitScan(f.ctx, c)
			if scenario == "lost_commit_reply" {
				require.ErrorIs(t, err, recordstore.ErrUncertain)
				f.reply.lose = false
			} else {
				require.NoError(t, err)
			}
			// New adapter instance resumes persisted cursor and all jobs together.
			r, err = NewRecordScheduleRepository(f.store)
			require.NoError(t, err)
			out, err := r.ReadScan(f.ctx, q)
			require.NoError(t, err)
			require.Equal(t, ScheduleCursor{Revision: 1}, out.Cursor)
			require.Equal(t, jobs, out.Jobs)
			update := ScheduleCommit{Scope: q.Scope, Lane: q.Lane, Expected: out.Cursor, NextAfterID: scheduledIdentity(jobs[1].Source)}
			switch scenario {
			case "stale_cursor", "lost_commit_reply":
				require.ErrorIs(t, r.CommitScan(f.ctx, c), recordstore.ErrConflict)
			case "stale_job_rolls_back_cursor", "immutable_source", "immutable_creation_time", "lane_transition":
				job := jobs[0]
				job.Revision = 2
				if scenario == "immutable_source" {
					job.Source.PrincipalID = "another-payer"
				}
				if scenario == "immutable_creation_time" {
					job.CreatedAt = job.CreatedAt.Add(time.Nanosecond)
				}
				if scenario == "lane_transition" {
					job.Lane = RefreshLane
					job.NextAttemptAt = job.NextAttemptAt.Add(time.Minute)
				}
				update.Writes = []ScheduleWrite{{1, job}}
				if scenario == "stale_job_rolls_back_cursor" {
					first := update
					first.NextAfterID = ""
					require.NoError(t, r.CommitScan(f.ctx, first))
					update.Expected = ScheduleCursor{Revision: 2}
				}
				err = r.CommitScan(f.ctx, update)
				if scenario == "lane_transition" {
					require.NoError(t, err)
					refresh, err := r.ReadScan(f.ctx, ScheduleScan{q.Scope, RefreshLane, 200})
					require.NoError(t, err)
					require.Equal(t, []ScheduledJob{job}, refresh.Jobs)
				} else {
					require.ErrorIs(t, err, recordstore.ErrConflict)
					same, err := r.ReadScan(f.ctx, q)
					require.NoError(t, err)
					require.Equal(t, update.Expected, same.Cursor)
				}
			case "bounded_ordered_page":
				q.Limit = 1
				page, err := r.ReadScan(f.ctx, q)
				require.NoError(t, err)
				require.Equal(t, []ScheduledJob{jobs[0]}, page.Jobs)
				update.NextAfterID = scheduledIdentity(jobs[0].Source)
				require.NoError(t, r.CommitScan(f.ctx, update))
				page, err = r.ReadScan(f.ctx, q)
				require.NoError(t, err)
				require.Equal(t, []ScheduledJob{jobs[1]}, page.Jobs)
			default:
				require.NoError(t, r.CommitScan(f.ctx, update))
				remaining, err := r.ReadScan(f.ctx, q)
				require.NoError(t, err)
				require.Equal(t, []ScheduledJob{jobs[2]}, remaining.Jobs)
				reset := ScheduleCommit{Scope: q.Scope, Lane: q.Lane, Expected: remaining.Cursor}
				require.NoError(t, r.CommitScan(f.ctx, reset))
				remaining, err = r.ReadScan(f.ctx, q)
				require.NoError(t, err)
				require.Equal(t, jobs, remaining.Jobs)
			}
			var raw bson.Raw
			err = f.db.Collection("ghatd_owned_records").FindOne(f.ctx, bson.M{"kind": scheduleJobKind, "id": scheduledIdentity(jobs[0].Source)}).Decode(&raw)
			require.NoError(t, err)
			for _, secret := range []string{jobs[0].Source.PrincipalID, jobs[0].Source.SubscriptionID, jobs[0].Source.Scope.AccountID} {
				require.False(t, bytes.Contains(raw, []byte(secret)))
			}
		})
	}
}

func TestScheduleRepositoryEncryptedIntegrity(t *testing.T) {
	for _, field := range []string{"job_state", "job_partition", "job_expiry", "cursor_state", "cursor_partition"} {
		t.Run(field, func(t *testing.T) {
			f, r, jobs := nativeScheduleFixture(t)
			q := ScheduleScan{f.intent.Scope, ColdLane, 200}
			require.NoError(t, r.CommitScan(f.ctx, ScheduleCommit{Scope: q.Scope, Lane: q.Lane, Writes: scheduleWrites(jobs, 0)}))
			kind, id := scheduleJobKind, scheduledIdentity(jobs[0].Source)
			key, value := "state", any("wrong")
			if field == "cursor_state" || field == "cursor_partition" {
				kind, id = scheduleCursorKind, scheduleCursorID(q.Scope, q.Lane)
			}
			if field == "job_partition" || field == "cursor_partition" {
				key, value = "partition", "wrong"
			}
			if field == "job_expiry" {
				key, value = "expires_at", time.Now().Add(time.Hour)
			}
			_, err := f.db.Collection("ghatd_owned_records").UpdateOne(f.ctx, bson.M{"kind": kind, "id": id}, bson.M{"$set": bson.M{key: value}})
			require.NoError(t, err)
			out, err := r.ReadScan(f.ctx, q)
			// A changed job partition can hide the row from its old index. The
			// query is not exhaustive coverage proof; direct tamper checks below
			// must still reject the ciphertext against altered indexed metadata.
			if field == "job_partition" {
				require.NoError(t, err)
				require.Len(t, out.Jobs, 2)
				err = f.store.Read(f.ctx, func(tx recordstore.Tx) error { _, err := tx.Get(f.ctx, kind, id); return err })
				require.Error(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, ScheduleSnapshot{}, out)
			}
		})
	}
}

func TestScheduleRepositoryEncryptedConcurrentCursor(t *testing.T) {
	for _, different := range []bool{false, true} {
		name := "same_page_one_atomic_winner"
		if different {
			name = "different_pages_cannot_mix_jobs"
		}
		t.Run(name, func(t *testing.T) {
			f, r, jobs := nativeScheduleFixture(t)
			first := ScheduleCommit{Scope: f.intent.Scope, Lane: ColdLane, NextAfterID: scheduledIdentity(jobs[0].Source), Writes: scheduleWrites(jobs[:1], 0)}
			second := first
			if different {
				second.NextAfterID = scheduledIdentity(jobs[1].Source)
				second.Writes = scheduleWrites(jobs[1:2], 0)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, input := range []ScheduleCommit{first, second} {
				go func(input ScheduleCommit) { <-start; results <- r.CommitScan(f.ctx, input) }(input)
			}
			close(start)
			successes, conflicts := 0, 0
			for n := 0; n < 2; n++ {
				err := <-results
				if err == nil {
					successes++
				} else {
					require.ErrorIs(t, err, recordstore.ErrConflict)
					conflicts++
				}
			}
			require.Equal(t, 1, successes)
			require.Equal(t, 1, conflicts)
			var count int64
			err := f.store.Read(f.ctx, func(tx recordstore.Tx) error {
				rows, err := tx.Find(f.ctx, recordstore.Query{Kind: scheduleJobKind, Partition: schedulePartition(f.intent.Scope, ColdLane), Limit: 200})
				count = int64(len(rows))
				return err
			})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			out, err := r.ReadScan(f.ctx, ScheduleScan{f.intent.Scope, ColdLane, 200})
			require.NoError(t, err)
			require.EqualValues(t, 1, out.Cursor.Revision)
			require.Contains(t, []string{first.NextAfterID, second.NextAfterID}, out.Cursor.AfterID)
		})
	}
}

func TestScheduleRepositoryEncryptedPrivateInputs(t *testing.T) {
	for _, scenario := range []string{"checkout_original", "status_original", "nested_unknown_schema", "nested_evidence_forbidden"} {
		t.Run(scenario, func(t *testing.T) {
			f := nativeStatusFixture(t)
			r, err := NewRecordScheduleRepository(f.base.store)
			require.NoError(t, err)
			source := ScheduledSource{Scope: f.p.Scope, Kind: billing.LifecycleCheckoutSources, PrincipalID: f.p.PrincipalID, Checkout: &f.base.intent, SourceID: billing.LifecycleDiscoverySourceID(f.p.Scope, billing.LifecycleCheckoutSources, f.base.intent.ID)}
			job := ScheduledJob{Source: source, Revision: 1, Lane: ColdLane, CreatedAt: f.base.clock.at, NextAttemptAt: f.base.clock.at}
			if scenario == "status_original" {
				job.Source = ScheduledSource{Scope: f.p.Scope, Kind: billing.LifecycleSubscriptionSources, PrincipalID: f.p.PrincipalID, SubscriptionID: f.p.SubscriptionID, SourceID: billing.LifecycleDiscoverySourceID(f.p.Scope, billing.LifecycleSubscriptionSources, f.p.SubscriptionID)}
				job.OriginalStatus = &f.p
			}
			require.NoError(t, r.CommitScan(f.ctx, ScheduleCommit{Scope: job.Source.Scope, Lane: ColdLane, Writes: []ScheduleWrite{{0, job}}}))
			q := ScheduleScan{job.Source.Scope, ColdLane, 200}
			if scenario == "nested_unknown_schema" || scenario == "nested_evidence_forbidden" {
				err = f.base.store.Transact(f.ctx, "fixture-private-corruption", func(tx recordstore.Tx) error {
					row, err := tx.Get(f.ctx, scheduleJobKind, scheduledIdentity(job.Source))
					if err != nil {
						return err
					}
					var p scheduleJobPayload
					if err := row.Decode(&p); err != nil {
						return err
					}
					if scenario == "nested_unknown_schema" {
						p.Source.Checkout.Schema = 2
					} else {
						p.Source.Checkout.Evidence = &f.base.provider.evidence
					}
					row.Data, err = json.Marshal(p)
					if err != nil {
						return err
					}
					row.Revision++
					return tx.Replace(f.ctx, row, row.Revision-1)
				})
				require.NoError(t, err)
				out, err := r.ReadScan(f.ctx, q)
				require.ErrorIs(t, err, recordstore.ErrUnavailable)
				require.Equal(t, ScheduleSnapshot{}, out)
				return
			}
			out, err := r.ReadScan(f.ctx, q)
			require.NoError(t, err)
			require.Equal(t, []ScheduledJob{job}, out.Jobs)
			transport, err := json.Marshal(out.Jobs[0])
			require.NoError(t, err)
			require.JSONEq(t, "{}", string(transport))
		})
	}
}
