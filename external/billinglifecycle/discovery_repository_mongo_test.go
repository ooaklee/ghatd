package billinglifecycle

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var _ DiscoveryRepository = (*RecordScheduleRepository)(nil)

// These pages use actual acknowledged checkout originals. The repository's
// canonical page validation does not certify owning discovery preparation or
// current discovery grants; those belong to the composing service's tests.
func nativeDiscoveryFixture(t *testing.T) (*inputNativeFixture, *RecordScheduleRepository, DiscoveryCommit) {
	t.Helper()
	f := nativeInputFixture(t)
	r, err := NewRecordScheduleRepository(f.reply)
	require.NoError(t, err)
	q := billing.LifecycleDiscoveryQuery{Scope: f.intent.Scope, Kind: billing.LifecycleCheckoutSources, Limit: 200}
	i := billing.LifecycleDiscoveryCandidate{ID: billing.LifecycleDiscoverySourceID(q.Scope, q.Kind, f.intent.ID), Revision: 1, Scope: q.Scope, PrincipalID: f.intent.Request.UserID, Intent: f.intent}
	p := billing.LifecycleDiscoveryPage{Items: []billing.LifecycleDiscoveryCandidate{i}, ReachedEnd: true}
	require.NoError(t, p.Validate(q))
	return f, r, DiscoveryCommit{Query: q, Page: p, Now: f.clock.at}
}

func TestDiscoveryRepositoryEncryptedHandoff(t *testing.T) {
	for _, scenario := range []string{"atomic_admission", "lost_commit_reply", "repeat_preserves_active_job", "stale_checkpoint", "empty_raw_page", "end_restarts_sweep", "wrong_scope_cursor", "fair_cursor_refused", "malformed_page", "canceled", "kind_isolation", "unknown_schema", "tampered_state", "tampered_partition"} {
		t.Run(scenario, func(t *testing.T) {
			f, r, c := nativeDiscoveryFixture(t)
			before, err := r.ReadDiscovery(f.ctx, c.Query)
			require.NoError(t, err)
			require.Equal(t, DiscoveryCheckpoint{}, before)
			switch scenario {
			case "wrong_scope_cursor":
				wrong := c.Query
				wrong.Scope.AccountID = "other"
				c.Query.Cursor = wrong.CursorFor(c.Page.Items[0].ID)
				c.Expected = DiscoveryCheckpoint{1, c.Query.Cursor}
			case "fair_cursor_refused":
				c.Query.Cursor = c.Page.Items[0].ID
				c.Expected = DiscoveryCheckpoint{1, c.Query.Cursor}
			case "malformed_page":
				c.Page.Items[0].PrincipalID = "different-owner"
			case "canceled":
				ctx, cancel := context.WithCancel(f.ctx)
				cancel()
				require.ErrorIs(t, r.CommitDiscovery(ctx, c), context.Canceled)
				out, err := r.ReadDiscovery(ctx, c.Query)
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, DiscoveryCheckpoint{}, out)
				return
			case "empty_raw_page":
				c.Page.NextCursor = c.Query.CursorFor(c.Page.Items[0].ID)
				c.Page.Items = nil
				c.Page.ReachedEnd = false
			case "lost_commit_reply":
				f.reply.lose = true
			}
			err = r.CommitDiscovery(f.ctx, c)
			if scenario == "wrong_scope_cursor" || scenario == "fair_cursor_refused" || scenario == "malformed_page" {
				require.Error(t, err)
				c.Query.Cursor = ""
				out, err := r.ReadDiscovery(f.ctx, c.Query)
				require.NoError(t, err)
				require.Equal(t, before, out)
				jobs, err := r.ReadScan(f.ctx, ScheduleScan{c.Query.Scope, ColdLane, 200})
				require.NoError(t, err)
				require.Empty(t, jobs.Jobs)
				return
			}
			if scenario == "lost_commit_reply" {
				require.ErrorIs(t, err, recordstore.ErrUncertain)
				f.reply.lose = false
			} else {
				require.NoError(t, err)
			}
			// Reopened adapter reads actual committed state after lost replies.
			r, err = NewRecordScheduleRepository(f.store)
			require.NoError(t, err)
			out, err := r.ReadDiscovery(f.ctx, c.Query)
			require.NoError(t, err)
			require.Equal(t, DiscoveryCheckpoint{1, c.Page.NextCursor}, out)
			jobs, err := r.ReadScan(f.ctx, ScheduleScan{c.Query.Scope, ColdLane, 200})
			require.NoError(t, err)
			require.Len(t, jobs.Jobs, len(c.Page.Items))
			require.Equal(t, ScheduleCursor{}, jobs.Cursor)
			switch scenario {
			case "lost_commit_reply", "stale_checkpoint":
				require.ErrorIs(t, r.CommitDiscovery(f.ctx, c), recordstore.ErrConflict)
			case "repeat_preserves_active_job":
				j := jobs.Jobs[0]
				j.Revision++
				j.Lane, j.Attempts, j.Fence = RefreshLane, 7, 9
				j.LeaseActor, j.LeaseToken = "replacement-worker", "active-token"
				j.LeasedUntil = c.Now.Add(time.Hour)
				j.NextAttemptAt = c.Now.Add(2 * time.Hour)
				// Seed an execution-shaped record for admission preservation only.
				// CommitScan cannot create or modify execution leases.
				seedDiscoveryExecution(t, f, j, 1)
				c.Expected = out
				c.Now = c.Now.Add(3 * time.Hour)
				require.NoError(t, r.CommitDiscovery(f.ctx, c))
				retained, err := r.ReadScan(f.ctx, ScheduleScan{c.Query.Scope, RefreshLane, 200})
				require.NoError(t, err)
				require.Equal(t, []ScheduledJob{j}, retained.Jobs)
			case "empty_raw_page":
				c.Expected, c.Query.Cursor = out, out.Cursor
				c.Page = billing.LifecycleDiscoveryPage{ReachedEnd: true}
				require.NoError(t, r.CommitDiscovery(f.ctx, c))
				reset, err := r.ReadDiscovery(f.ctx, c.Query)
				require.NoError(t, err)
				require.Equal(t, DiscoveryCheckpoint{Revision: 2}, reset)
			case "end_restarts_sweep":
				c.Expected = out
				require.NoError(t, r.CommitDiscovery(f.ctx, c))
				reset, err := r.ReadDiscovery(f.ctx, c.Query)
				require.NoError(t, err)
				require.Equal(t, DiscoveryCheckpoint{Revision: 2}, reset)
			case "kind_isolation":
				q := c.Query
				q.Kind = billing.LifecycleSubscriptionSources
				other, err := r.ReadDiscovery(f.ctx, q)
				require.NoError(t, err)
				require.Equal(t, DiscoveryCheckpoint{}, other)
			case "unknown_schema":
				row, err := recordstore.NewRecord(discoveryCheckpointKind, discoveryCheckpointID(c.Query), discoveryCheckpointID(c.Query), 2, discoveryCheckpointPayload{2, c.Query.Scope, c.Query.Kind, ""})
				require.NoError(t, err)
				require.NoError(t, f.store.Transact(f.ctx, "test-corrupt-checkpoint", func(tx recordstore.Tx) error { return tx.Replace(f.ctx, row, 1) }))
				bad, err := r.ReadDiscovery(f.ctx, c.Query)
				require.ErrorIs(t, err, recordstore.ErrUnavailable)
				require.Equal(t, DiscoveryCheckpoint{}, bad)
			case "tampered_state", "tampered_partition":
				field := "state"
				if scenario == "tampered_partition" {
					field = "partition"
				}
				_, err := f.db.Collection("ghatd_owned_records").UpdateOne(f.ctx, bson.M{"kind": discoveryCheckpointKind, "id": discoveryCheckpointID(c.Query)}, bson.M{"$set": bson.M{field: "tampered"}})
				require.NoError(t, err)
				bad, err := r.ReadDiscovery(f.ctx, c.Query)
				require.Error(t, err)
				require.Equal(t, DiscoveryCheckpoint{}, bad)
			}
		})
	}
}

func TestDiscoveryRepositoryEncryptedConcurrentPage(t *testing.T) {
	for _, scenario := range []string{"same_page", "different_continuation"} {
		t.Run(scenario, func(t *testing.T) {
			f, r, c := nativeDiscoveryFixture(t)
			other := c
			if scenario == "different_continuation" {
				other.Page = billing.LifecycleDiscoveryPage{NextCursor: c.Query.CursorFor(c.Page.Items[0].ID)}
			}
			start, results := make(chan struct{}), make(chan error, 2)
			var wg sync.WaitGroup
			for _, commit := range []DiscoveryCommit{c, other} {
				wg.Add(1)
				go func(commit DiscoveryCommit) {
					defer wg.Done()
					<-start
					results <- r.CommitDiscovery(f.ctx, commit)
				}(commit)
			}
			close(start)
			wg.Wait()
			close(results)
			success, conflict := 0, 0
			for err := range results {
				if err == nil {
					success++
				} else {
					require.ErrorIs(t, err, recordstore.ErrConflict)
					conflict++
				}
			}
			require.Equal(t, 1, success)
			require.Equal(t, 1, conflict)
			checkpoint, err := r.ReadDiscovery(f.ctx, c.Query)
			require.NoError(t, err)
			require.Equal(t, int64(1), checkpoint.Revision)
			jobs, err := r.ReadScan(f.ctx, ScheduleScan{c.Query.Scope, ColdLane, 200})
			require.NoError(t, err)
			if checkpoint.Cursor == "" {
				require.Len(t, jobs.Jobs, 1)
			} else {
				require.Empty(t, jobs.Jobs)
			}
		})
	}
}

func TestDiscoveryRepositoryEncryptedPageRollback(t *testing.T) {
	for _, scenario := range []string{"late_owner_conflict", "late_corrupt_job"} {
		t.Run(scenario, func(t *testing.T) {
			f, r, c := nativeDiscoveryFixture(t)
			request := f.intent.Request
			request.IdempotencyKey = "another-original"
			i, err := f.native.PrepareCheckout(f.ctx, f.intent.Scope, request)
			require.NoError(t, err)
			require.NoError(t, f.native.AcknowledgeCheckout(f.ctx, i, "cs_another"))
			i, err = f.native.FindCheckoutIntent(f.ctx, i.Scope, request.IdempotencyKey)
			require.NoError(t, err)
			c.Page.Items = append(c.Page.Items, billing.LifecycleDiscoveryCandidate{ID: billing.LifecycleDiscoverySourceID(c.Query.Scope, c.Query.Kind, i.ID), Revision: 1, Scope: i.Scope, PrincipalID: i.Request.UserID, Intent: i})
			sort.Slice(c.Page.Items, func(a, b int) bool { return c.Page.Items[a].ID < c.Page.Items[b].ID })
			require.NoError(t, c.Page.Validate(c.Query))
			last := discoverySource(c.Query, c.Page.Items[1])
			// Seed a conflicting original using the private store to simulate a
			// previously admitted mismatched original. The first page job must
			// roll back when the later job fails; no cursor may be committed.
			job := ScheduledJob{Source: last, Revision: 1, Lane: ColdLane, CreatedAt: c.Now, NextAttemptAt: c.Now}
			row, err := jobRecord(job)
			require.NoError(t, err)
			var p scheduleJobPayload
			require.NoError(t, strictScheduleDecode(row.Data, &p))
			if scenario == "late_owner_conflict" {
				p.Source.Checkout.SessionID = "cs_different"
			} else {
				p.Schema = 2
			}
			row, err = recordstore.NewRecord(row.Kind, row.ID, row.Partition, row.Revision, p)
			require.NoError(t, err)
			row.State = ColdLane
			require.NoError(t, f.store.Transact(f.ctx, "test-seed-conflict", func(tx recordstore.Tx) error { return tx.Insert(f.ctx, row) }))
			require.Error(t, r.CommitDiscovery(f.ctx, c))
			checkpoint, err := r.ReadDiscovery(f.ctx, c.Query)
			require.NoError(t, err)
			require.Equal(t, DiscoveryCheckpoint{}, checkpoint)
			err = f.store.Read(f.ctx, func(tx recordstore.Tx) error {
				_, err := tx.Get(f.ctx, scheduleJobKind, scheduledIdentity(discoverySource(c.Query, c.Page.Items[0])))
				return err
			})
			require.ErrorIs(t, err, recordstore.ErrNotFound)
		})
	}
}

func TestDiscoveryRepositoryEncryptedSubscriptionOriginal(t *testing.T) {
	for _, scenario := range []string{"repeat_retains_status_original", "first_paid_fact_preserves_checkout_origin", "retired_job_is_not_reset"} {
		t.Run(scenario, func(t *testing.T) {
			f := nativeStatusFixture(t)
			r, err := NewRecordScheduleRepository(f.base.reply)
			require.NoError(t, err)
			// Actual owning preparation/read, without claiming current manager
			// discovery authority or a host migration orchestrator is installed.
			ready := false
			for n := 0; n < 30; n++ {
				p, err := f.owner.PrepareLifecycleDiscovery(f.ctx, f.p.Scope, 200)
				require.NoError(t, err)
				if p.State.Phase == billing.LifecyclePreparationComplete {
					ready = true
					break
				}
			}
			require.True(t, ready)
			q := billing.LifecycleDiscoveryQuery{Scope: f.p.Scope, Kind: billing.LifecycleSubscriptionSources, Limit: 200}
			page, err := f.owner.DiscoverLifecycleSources(f.ctx, q)
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			require.Empty(t, page.Items[0].Fact.ID)
			c := DiscoveryCommit{Query: q, Page: page, Now: f.base.clock.at}
			require.NoError(t, r.CommitDiscovery(f.ctx, c))
			jobs, err := r.ReadScan(f.ctx, ScheduleScan{q.Scope, ColdLane, 200})
			require.NoError(t, err)
			require.Len(t, jobs.Jobs, 1)
			j := jobs.Jobs[0]
			j.Revision, j.Attempts, j.Fence = 2, 3, 5
			j.OriginalStatus = &f.p
			j.Lane = RefreshLane
			if scenario == "retired_job_is_not_reset" {
				j.Lane = RetiredLane
			}
			seedDiscoveryExecution(t, f.base, j, 1)
			if scenario == "first_paid_fact_preserves_checkout_origin" {
				paid, err := f.owner.AcceptVerified(f.ctx, billing.VerifiedRevenueRequest{Scope: f.p.Scope, EnvelopeID: "evt_discovery_first", Facts: []billing.RevenueFact{{Scope: f.p.Scope, Kind: billing.RevenuePayment, PaymentID: "pi_first", InvoiceID: "in_first", AllocationID: "il_first", PrincipalID: f.p.PrincipalID, ProviderCustomerID: f.p.ProviderCustomerID, SubscriptionID: f.p.SubscriptionID, PlanID: "plan", CostID: "cost", Currency: "GBP", CurrencyExponent: 2, PaidMinor: 1000, EffectiveAt: f.base.clock.at}}})
				require.NoError(t, err)
				page, err = f.owner.DiscoverLifecycleSources(f.ctx, q)
				require.NoError(t, err)
				require.Equal(t, paid.FactIDs[0], page.Items[0].Fact.ID)
				c.Page = page
			}
			c.Expected, err = r.ReadDiscovery(f.ctx, q)
			require.NoError(t, err)
			c.Now = c.Now.Add(time.Hour)
			require.NoError(t, r.CommitDiscovery(f.ctx, c))
			retained, err := r.ReadScan(f.ctx, ScheduleScan{q.Scope, j.Lane, 200})
			require.NoError(t, err)
			require.Equal(t, []ScheduledJob{j}, retained.Jobs)
			require.Empty(t, retained.Jobs[0].Source.FactID)
			require.Equal(t, &f.p, retained.Jobs[0].OriginalStatus)
		})
	}
}

func seedDiscoveryExecution(t *testing.T, f *inputNativeFixture, job ScheduledJob, expected int64) {
	t.Helper()
	row, err := jobRecord(job)
	require.NoError(t, err)
	require.NoError(t, f.store.Transact(f.ctx, "test-seed-discovery-execution", func(tx recordstore.Tx) error {
		return tx.Replace(f.ctx, row, expected)
	}))
}
