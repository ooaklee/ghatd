package revenuestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Restore exact original native codecs into a fresh encrypted store, omitting
// ALL new projection/epoch/preparation records. This models pre-projection
// history; source fingerprints, receipts and economic sequence are unchanged.
func restoreLegacyLifecycleHistory(t *testing.T, ctx context.Context, from, to recordstore.Store, edits ...func(map[string][]recordstore.Record)) map[string][]recordstore.Record {
	t.Helper()
	originals := map[string][]recordstore.Record{}
	require.NoError(t, from.Read(ctx, func(tx recordstore.Tx) error {
		for _, kind := range []string{kindCheckoutIntent, kindCheckoutAck, kindCheckoutSession, kindCheckoutAssociation, kindCheckoutPrincipal, kindCheckoutLifecycleAnchor, kindCheckoutLifecycleReceipt, kindFact, kindObservation, kindHead} {
			part := checkoutPartition
			if kind == kindFact || kind == kindObservation || kind == kindHead {
				part = partition
			}
			rows, err := tx.Find(ctx, recordstore.Query{Kind: kind, Partition: part, Limit: 200})
			if err != nil {
				return err
			}
			require.Less(t, len(rows), 200)
			originals[kind] = rows
		}
		return nil
	}))
	for _, edit := range edits {
		edit(originals)
	}
	require.NoError(t, to.Transact(ctx, "test-legacy-history-restore", func(tx recordstore.Tx) error {
		for _, rows := range originals {
			for _, row := range rows {
				if err := tx.Insert(ctx, row); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	return originals
}
func finishLifecyclePreparation(t *testing.T, s *billing.RevenueService, ctx context.Context, scope billing.RevenueScope, limit int) billing.LifecyclePreparationState {
	t.Helper()
	for n := 0; n < 40; n++ {
		out, err := s.PrepareLifecycleDiscovery(ctx, scope, limit)
		require.NoError(t, err)
		if out.State.Phase == billing.LifecyclePreparationComplete {
			return out.State
		}
	}
	t.Fatal("fixture preparation did not finish its bounded sweep")
	return billing.LifecyclePreparationState{}
}
func TestMongoLifecyclePreparationLegacyUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		want         error
	}{
		{name: "fresh_empty_scope_requires_all_phases", change: "empty"},
		{name: "legacy_ack_anchor_and_payment_reconstruct"},
		{name: "legacy_binding_only_never_manufactures_status", change: "binding-only"},
		{name: "legacy_customerless_payment_remains_financial_only", change: "customerless"},
		{name: "other_account_history_not_selected", change: "account"},
		{name: "other_mode_history_not_selected", change: "mode"},
		{name: "other_provider_history_not_selected", change: "provider"},
		{name: "restart_uses_durable_original_progress", change: "resume"},
		{name: "missing_original_ack_blocks_without_marker", change: "missing-ack", want: billing.ErrRevenueUnavailable},
		{name: "missing_original_anchor_receipt_blocks_without_marker", change: "missing-receipt", want: billing.ErrRevenueUnavailable},
		{name: "corrupt_customerless_payment_blocks_without_marker", change: "bad-customerless", want: billing.ErrRevenueUnavailable},
		{name: "contradictory_payer_history_blocks_without_marker", change: "conflict", want: billing.ErrRevenueConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, oldStore, _, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			checkout := lifecycleService(t, old, p, time.Unix(1700000000, 0).UTC())
			var scope billing.RevenueScope
			if tc.change == "empty" {
				scope = checkoutResolverRequest().Scope
			} else {
				intent := lifecycleIntent(t, checkout, ctx)
				scope = intent.Scope
				e := checkoutEvidence(intent)
				if tc.change == "binding-only" {
					p.evidence = e
					_, err := checkout.ResolveRevenueAssociation(ctx, checkoutResolverRequest())
					require.NoError(t, err)
				} else if tc.change != "customerless" && tc.change != "bad-customerless" {
					_, err := checkout.CaptureCheckoutLifecycleEvidence(ctx, intent, e)
					require.NoError(t, err)
				}
				if tc.change != "binding-only" {
					f := revenueFact()
					f.Scope = scope
					f.PrincipalID = intent.Request.UserID
					f.ProviderCustomerID = e.CustomerID
					f.SubscriptionID = e.SubscriptionID
					if tc.change == "customerless" || tc.change == "bad-customerless" {
						f.ProviderCustomerID = ""
					}
					accept(t, revenueService(t, old), ctx, "legacy-payment", f)
				}
			}
			r, store, db, _ := revenueFixture(t)
			var edits []func(map[string][]recordstore.Record)
			if tc.change == "conflict" {
				// A separately accepted canonical payment proves a genuine
				// owner contradiction, not a damaged fingerprint/metadata.
				other, otherStore, _, _ := revenueFixture(t)
				var f billing.RevenueFact
				var native persistedFact
				require.NoError(t, originalsFromPayment(t, ctx, oldStore, &native))
				f = native.Fact
				f.PrincipalID = "other-canonical-payer"
				f.Sequence = 0
				f.AcceptedAt = time.Time{}
				f.Fingerprint = ""
				o := accept(t, revenueService(t, other), ctx, "legacy-payment", f)
				var row recordstore.Record
				require.NoError(t, otherStore.Read(ctx, func(tx recordstore.Tx) error { var e error; row, e = tx.Get(ctx, kindFact, o.FactIDs[0]); return e }))
				edits = append(edits, func(rows map[string][]recordstore.Record) { rows[kindFact] = []recordstore.Record{row} })
			}
			originals := restoreLegacyLifecycleHistory(t, ctx, oldStore, store, edits...)
			switch tc.change {
			case "account":
				scope.AccountID = "other-account"
			case "provider":
				scope.Provider = "other-provider"
			case "mode":
				scope.LiveMode = !scope.LiveMode
			case "missing-ack":
				_, err := db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": kindCheckoutAck})
				require.NoError(t, err)
			case "missing-receipt":
				_, err := db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": kindCheckoutLifecycleReceipt})
				require.NoError(t, err)
			case "bad-customerless":
				// Authenticated native tamper fixture changes payload while retaining the
				// original immutable metadata/fingerprint, never via provider input.
				require.NoError(t, store.Transact(ctx, "test-contradictory-original", func(tx recordstore.Tx) error {
					row := originals[kindFact][0]
					var f persistedFact
					require.NoError(t, row.Decode(&f))
					f.Fact.PaidMinor++
					row.Data, _ = json.Marshal(f)
					row.Revision = 2
					return tx.Replace(ctx, row, 1)
				}))

			}
			p.calls = 0
			service := revenueService(t, r)
			_, err := service.DiscoverLifecycleSources(ctx, billing.LifecycleDiscoveryQuery{Scope: scope, Kind: billing.LifecycleCheckoutSources, Limit: 1})
			require.ErrorIs(t, err, billing.ErrLifecycleDiscoveryUnprepared)
			first, err := service.PrepareLifecycleDiscovery(ctx, scope, 1)
			if tc.want == nil {
				require.NoError(t, err)
				require.Less(t, first.State.Phase, billing.LifecyclePreparationComplete)
			}
			var state billing.LifecyclePreparationState
			if tc.want != nil {
				for n := 0; err == nil && n < 20; n++ {
					_, err = service.PrepareLifecycleDiscovery(ctx, scope, 1)
				}
				require.ErrorIs(t, err, tc.want)
				n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLifecycleDiscoveryPreparation})
				require.NoError(t, e)
				require.Zero(t, n)
				return
			}
			if tc.change == "resume" {
				r, err = NewRepository(store)
				require.NoError(t, err)
				service = revenueService(t, r)
			}
			state = finishLifecyclePreparation(t, service, ctx, scope, 1)
			again, err := service.PrepareLifecycleDiscovery(ctx, scope, 1)
			require.NoError(t, err)
			require.Equal(t, state, again.State)
			for _, kind := range []string{billing.LifecycleCheckoutSources, billing.LifecycleSubscriptionSources} {
				page, e := service.DiscoverLifecycleSources(ctx, billing.LifecycleDiscoveryQuery{Scope: scope, Kind: kind, Limit: 200})
				require.NoError(t, e)
				if tc.change == "empty" || tc.change == "account" || tc.change == "mode" || tc.change == "provider" {
					require.Empty(t, page.Items)
				} else if kind == billing.LifecycleCheckoutSources {
					require.Len(t, page.Items, 1)
				} else if tc.change == "binding-only" || tc.change == "customerless" {
					require.Empty(t, page.Items)
				} else {
					require.Len(t, page.Items, 1)
				}
			}
			if tc.change == "customerless" {
				require.Equal(t, int64(1), state.CustomerlessPayments)
			}
			// Every restored original remains byte-identical; only additive projection,
			// progress, epoch and readiness records were written by preparation.
			require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
				for kind, rows := range originals {
					for _, before := range rows {
						after, e := tx.Get(ctx, kind, before.ID)
						if e != nil {
							return e
						}
						require.Equal(t, before, after)
					}
				}
				return nil
			}))
			for _, kind := range []string{kindSubscriptionStatusHead, kindSubscriptionStatusCapture} {
				n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
				require.NoError(t, e)
				require.Zero(t, n)
			}
			require.Zero(t, p.calls)
		})
	}
}

type epochBarrierStore struct {
	recordstore.Store
	seen  atomic.Int32
	ready chan struct{}
}
type epochBarrierTx struct {
	recordstore.Tx
	store *epochBarrierStore
}

func (s *epochBarrierStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	return s.Store.Transact(ctx, key, func(tx recordstore.Tx) error { return fn(&epochBarrierTx{tx, s}) })
}
func (t *epochBarrierTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	row, err := t.Tx.Get(ctx, kind, id)
	if kind == kindLifecycleSourceEpoch {
		n := t.store.seen.Add(1)
		if n == 2 {
			close(t.store.ready)
		}
		if n <= 2 {
			select {
			case <-t.store.ready:
			case <-ctx.Done():
				return recordstore.Record{}, ctx.Err()
			}
		}
	}
	return row, err
}
func TestMongoLifecyclePreparationCompletionFence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		customerless bool
	}{{name: "concurrent_checkout_cannot_escape_completion"}, {name: "concurrent_customerless_payment_cannot_escape_completion", customerless: true}} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			scope := checkoutResolverRequest().Scope
			service := revenueService(t, r)
			for n := 0; n < 5; n++ {
				out, err := service.PrepareLifecycleDiscovery(ctx, scope, 1)
				require.NoError(t, err)
				require.Equal(t, n+1, out.State.Phase)
			}
			p := &lifecycleEvidenceFixture{}
			checkout := lifecycleService(t, r, p, time.Unix(1700000000, 0).UTC())
			intent, err := checkout.PrepareCheckout(ctx, scope, historicalCheckoutRequest())
			require.NoError(t, err)
			barrier := &epochBarrierStore{Store: store, ready: make(chan struct{})}
			raced, err := NewRepository(barrier)
			require.NoError(t, err)
			var wg sync.WaitGroup
			wg.Add(2)
			errs := make([]error, 2)
			go func() {
				defer wg.Done()
				_, errs[0] = revenueService(t, raced).PrepareLifecycleDiscovery(ctx, scope, 1)
			}()
			go func() {
				defer wg.Done()
				if tc.customerless {
					f := revenueFact()
					f.Scope = scope
					_, errs[1] = revenueService(t, raced).AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: scope, EnvelopeID: "racing-source", Facts: []billing.RevenueFact{f}})
				} else {
					errs[1] = lifecycleService(t, raced, p, intent.CreatedAt).AcknowledgeCheckout(ctx, intent, "cs_racing")
				}
			}()
			wg.Wait()
			require.GreaterOrEqual(t, barrier.seen.Load(), int32(2))
			for _, err := range errs {
				if err != nil {
					require.False(t, errors.Is(err, billing.ErrRevenueUncertain), fmt.Sprint(err))
				}
			}
			// Current native retry either reconstructs a lost source transaction or
			// confirms its committed original; no provider lookup or fabricated owner.
			if tc.customerless {
				f := revenueFact()
				f.Scope = scope
				accept(t, service, ctx, "racing-source", f)
			} else {
				require.NoError(t, checkout.AcknowledgeCheckout(ctx, intent, "cs_racing"))
			}
			state := finishLifecyclePreparation(t, service, ctx, scope, 1)
			require.False(t, state.PreparedAt.IsZero())
			require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
				epoch, e := readLifecycleSourceEpoch(ctx, tx, scope)
				require.GreaterOrEqual(t, epoch, int64(2))
				return e
			}))
			n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLifecycleDiscoveryPreparation})
			require.NoError(t, e)
			require.Equal(t, int64(1), n)
		})
	}
}

func originalsFromPayment(t *testing.T, ctx context.Context, store recordstore.Store, out *persistedFact) error {
	t.Helper()
	return store.Read(ctx, func(tx recordstore.Tx) error {
		rows, err := tx.Find(ctx, recordstore.Query{Kind: kindFact, Partition: partition, Limit: 1})
		if err != nil {
			return err
		}
		require.Len(t, rows, 1)
		return rows[0].Decode(out)
	})
}

// This store simulates precise native-write failure and a lost successful
// transaction reply. Failed writes roll back; uncertain replies retain commit.
type preparationFaultStore struct {
	recordstore.Store
	kind      string
	uncertain bool
}
type preparationFaultTx struct {
	recordstore.Tx
	kind string
}

func (t preparationFaultTx) Insert(ctx context.Context, row recordstore.Record) error {
	if row.Kind == t.kind {
		return recordstore.ErrUnavailable
	}
	return t.Tx.Insert(ctx, row)
}
func (t preparationFaultTx) Replace(ctx context.Context, row recordstore.Record, expected int64) error {
	if row.Kind == t.kind {
		return recordstore.ErrUnavailable
	}
	return t.Tx.Replace(ctx, row, expected)
}
func (s preparationFaultStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	err := s.Store.Transact(ctx, key, func(tx recordstore.Tx) error {
		if s.kind != "" {
			return fn(preparationFaultTx{tx, s.kind})
		}
		return fn(tx)
	})
	if err == nil && s.uncertain {
		return recordstore.ErrUncertain
	}
	return err
}
func TestMongoLifecyclePreparationAtomicRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, kind        string
		finish, uncertain bool
	}{
		{name: "projection_and_progress_rollback", kind: kindLifecyclePreparationProgress},
		{name: "lost_page_commit_reply_keeps_counts_once", uncertain: true},
		{name: "completion_epoch_failure_keeps_unprepared", kind: kindLifecycleSourceEpoch, finish: true},
		{name: "completion_marker_failure_rolls_back_epoch_and_progress", kind: kindLifecycleDiscoveryPreparation, finish: true},
		{name: "lost_completion_reply_recovers_same_marker", uncertain: true, finish: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			scope := checkoutResolverRequest().Scope
			if !tc.finish {
				old, oldStore, _, _ := revenueFixture(t)
				checkout := lifecycleService(t, old, &lifecycleEvidenceFixture{}, time.Unix(1700000000, 0).UTC())
				lifecycleIntent(t, checkout, ctx)
				restoreLegacyLifecycleHistory(t, ctx, oldStore, store)
			} else {
				for n := 0; n < 5; n++ {
					_, err := revenueService(t, r).PrepareLifecycleDiscovery(ctx, scope, 1)
					require.NoError(t, err)
				}
			}
			broken, err := NewRepository(preparationFaultStore{store, tc.kind, tc.uncertain})
			require.NoError(t, err)
			out, err := revenueService(t, broken).PrepareLifecycleDiscovery(ctx, scope, 1)
			require.Zero(t, out)
			if tc.uncertain {
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
			} else {
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				for _, kind := range []string{kindLifecycleDiscoveryPreparation, kindLifecycleSourceEpoch} {
					n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
					require.NoError(t, e)
					require.Zero(t, n)
				}
				if !tc.finish {
					n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLifecycleCheckoutSource})
					require.NoError(t, e)
					require.Zero(t, n)
				}
			}
			state := finishLifecyclePreparation(t, revenueService(t, r), ctx, scope, 1)
			if !tc.finish {
				require.Equal(t, int64(2), state.Scanned)
				require.Equal(t, int64(1), state.Selected)
			}
			repeated, err := revenueService(t, r).PrepareLifecycleDiscovery(ctx, scope, 1)
			require.NoError(t, err)
			require.Equal(t, state, repeated.State)
		})
	}
}
func TestMongoLifecyclePreparationLateSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		live bool
	}{{name: "checkout_inserted_behind_cursor_forces_new_sweep"}, {name: "separate_live_scope_uses_own_epoch", live: true}} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			checkout := lifecycleService(t, r, p, time.Unix(1700000000, 0).UTC())
			first := lifecycleIntent(t, checkout, ctx)
			scope := first.Scope
			var later billing.CheckoutIntent
			for n := 0; n < 100; n++ {
				q := historicalCheckoutRequest()
				q.IdempotencyKey = fmt.Sprintf("late-behind-%d", n)
				candidate, err := checkout.PrepareCheckout(ctx, scope, q)
				require.NoError(t, err)
				if candidate.ID < first.ID {
					later = candidate
					break
				}
			}
			require.NotEmpty(t, later.ID)
			service := revenueService(t, r)
			before, err := service.PrepareLifecycleDiscovery(ctx, scope, 1)
			require.NoError(t, err)
			require.Equal(t, first.ID, before.State.AfterID)
			if tc.live {
				scope.LiveMode = true
				q := historicalCheckoutRequest()
				later, err = checkout.PrepareCheckout(ctx, scope, q)
				require.NoError(t, err)
			}
			require.NoError(t, checkout.AcknowledgeCheckout(ctx, later, "cs_late"))
			after, err := service.PrepareLifecycleDiscovery(ctx, first.Scope, 1)
			require.NoError(t, err)
			if !tc.live {
				require.True(t, after.Restarted)
				require.Zero(t, after.State.Phase)
				require.Empty(t, after.State.AfterID)
				require.Zero(t, after.State.Scanned)
				require.Equal(t, int64(2), after.State.Sweeps)
			} else {
				require.False(t, after.Restarted)
			}
			n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLifecycleDiscoveryPreparation})
			require.NoError(t, e)
			require.Zero(t, n)
			state := finishLifecyclePreparation(t, service, ctx, first.Scope, 1)
			if !tc.live {
				require.Equal(t, int64(2), state.Selected)
			} else {
				require.Equal(t, int64(1), state.Selected)
			}
			require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
				epoch, e := readLifecycleSourceEpoch(ctx, tx, first.Scope)
				require.Equal(t, state.Epoch, epoch)
				return e
			}))
		})
	}
}
func TestMongoLifecycleSourceEpochWrites(t *testing.T) {
	for _, tc := range []struct {
		name, action string
		want         int64
	}{
		{name: "subscription_ack_and_replay", action: "ack", want: 1},
		{name: "first_anchor_and_original_replay", action: "anchor", want: 2},
		{name: "later_same_owner_anchor_is_new_source", action: "later-anchor", want: 4},
		{name: "paid_association_and_replay", action: "paid", want: 2},
		{name: "customer_payment_and_replay", action: "payment", want: 1},
		{name: "customerless_payment_and_replay", action: "customerless", want: 1},
		{name: "ack_epoch_failure_rolls_back_original_and_projection", action: "ack-fail", want: 0},
		{name: "payment_epoch_failure_rolls_back_financial_and_projection", action: "payment-fail", want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			checkout := lifecycleService(t, r, p, time.Unix(1700000000, 0).UTC())
			scope := checkoutResolverRequest().Scope
			if tc.action == "payment" || tc.action == "customerless" || tc.action == "payment-fail" {
				f := revenueFact()
				f.Scope = scope
				if tc.action != "customerless" {
					f.ProviderCustomerID = "customer"
				}
				if tc.action == "payment-fail" {
					broken, err := NewRepository(preparationFaultStore{Store: store, kind: kindLifecycleSourceEpoch})
					require.NoError(t, err)
					_, err = revenueService(t, broken).AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: scope, EnvelopeID: "epoch-payment", Facts: []billing.RevenueFact{f}})
					require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
					for _, kind := range []string{kindFact, kindHead, kindObservation, kindLifecycleSubscriptionSource} {
						n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
						require.NoError(t, e)
						require.Zero(t, n)
					}
				} else {
					accept(t, revenueService(t, r), ctx, "epoch-payment", f)
					accept(t, revenueService(t, r), ctx, "epoch-payment", f)
				}
			} else {
				intent, err := checkout.PrepareCheckout(ctx, scope, historicalCheckoutRequest())
				require.NoError(t, err)
				if tc.action == "ack-fail" {
					broken, e := NewRepository(preparationFaultStore{Store: store, kind: kindLifecycleSourceEpoch})
					require.NoError(t, e)
					err = lifecycleService(t, broken, p, intent.CreatedAt).AcknowledgeCheckout(ctx, intent, "cs_original")
					require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
					for _, kind := range []string{kindCheckoutAck, kindCheckoutSession, kindLifecycleCheckoutSource} {
						n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
						require.NoError(t, e)
						require.Zero(t, n)
					}
				} else {
					require.NoError(t, checkout.AcknowledgeCheckout(ctx, intent, "cs_original"))
					require.NoError(t, checkout.AcknowledgeCheckout(ctx, intent, "cs_original"))
					intent, err = checkout.FindCheckoutIntent(ctx, scope, intent.Request.IdempotencyKey)
					require.NoError(t, err)
					e := checkoutEvidence(intent)
					switch tc.action {
					case "anchor", "later-anchor":
						_, err = checkout.CaptureCheckoutLifecycleEvidence(ctx, intent, e)
						require.NoError(t, err)
						_, err = checkout.CaptureCheckoutLifecycleEvidence(ctx, intent, e)
						require.NoError(t, err)
						if tc.action == "later-anchor" {
							q := historicalCheckoutRequest()
							q.IdempotencyKey = "later-epoch"
							j, err := checkout.PrepareCheckout(ctx, scope, q)
							require.NoError(t, err)
							require.NoError(t, checkout.AcknowledgeCheckout(ctx, j, "cs_later"))
							j, err = checkout.FindCheckoutIntent(ctx, scope, q.IdempotencyKey)
							require.NoError(t, err)
							laterEvidence := checkoutEvidence(j)
							laterEvidence.SessionID = j.SessionID
							_, err = checkout.CaptureCheckoutLifecycleEvidence(ctx, j, laterEvidence)
							require.NoError(t, err)
						}
					case "paid":
						p.evidence = e
						_, err = checkout.ResolveRevenueAssociation(ctx, checkoutResolverRequest())
						require.NoError(t, err)
						_, err = checkout.ResolveRevenueAssociation(ctx, checkoutResolverRequest())
						require.NoError(t, err)
					}
				}
			}
			require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
				epoch, e := readLifecycleSourceEpoch(ctx, tx, scope)
				require.Equal(t, tc.want, epoch)
				return e
			}))
		})
	}
}

func TestMongoLifecyclePreparationProjectionHealth(t *testing.T) {
	for _, tc := range []struct{ name, kind string }{
		{name: "orphan_checkout_projection_prevents_readiness", kind: kindLifecycleCheckoutSource},
		{name: "orphan_subscription_projection_prevents_readiness", kind: kindLifecycleSubscriptionSource},
		{name: "bad_tail_canonical_payment_rolls_back_valid_earlier_projection", kind: kindFact},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			scope := checkoutResolverRequest().Scope
			if tc.kind == kindFact {
				old, oldStore, _, _ := revenueFixture(t)
				f := revenueFact()
				f.Scope = scope
				f.ProviderCustomerID = "customer"
				accept(t, revenueService(t, old), ctx, "first-payment", f)
				f.PaymentID = "second-payment"
				f.InvoiceID = "second-invoice"
				accept(t, revenueService(t, old), ctx, "second-payment", f)
				restoreLegacyLifecycleHistory(t, ctx, oldStore, store, func(rows map[string][]recordstore.Record) {
					last := &rows[kindFact][1]
					var v persistedFact
					require.NoError(t, last.Decode(&v))
					v.Fact.PaidMinor++
					last.Data, _ = json.Marshal(v)
				})
			} else {
				require.NoError(t, store.Transact(ctx, "test-orphan-projection", func(tx recordstore.Tx) error {
					if tc.kind == kindLifecycleCheckoutSource {
						return insert(ctx, tx, tc.kind, billing.LifecycleDiscoverySourceID(scope, billing.LifecycleCheckoutSources, "checkout_missing"), lifecycleSourcePartition(scope), lifecycleCheckoutSource{scope, "checkout_missing", "missing-fingerprint", "cs_missing"})
					}
					return insert(ctx, tx, tc.kind, lifecycleSubscriptionKey(scope, "sub_missing"), lifecycleSourcePartition(scope), lifecycleSubscriptionSource{Scope: scope, SubscriptionID: "sub_missing", PrincipalID: "principal", CustomerID: "customer", FactID: "revenue_missing", FactFingerprint: "missing-fingerprint"})
				}))
			}
			service := revenueService(t, r)
			var err error
			for n := 0; n < 20 && err == nil; n++ {
				_, err = service.PrepareLifecycleDiscovery(ctx, scope, 200)
			}
			require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLifecycleDiscoveryPreparation})
			require.NoError(t, e)
			require.Zero(t, n)
			if tc.kind == kindFact {
				n, e := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLifecycleSubscriptionSource})
				require.NoError(t, e)
				require.Zero(t, n)
			}
		})
	}
}

func TestMongoLifecyclePreparationScopeFilteringIntegrity(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		want         error
	}{
		{name: "tampered_payment_scope_cannot_hide_before_canonical_validation", change: "fact-scope", want: billing.ErrRevenueUnavailable},
		{name: "tampered_checkout_mode_cannot_hide_before_frozen_validation", change: "intent-mode", want: billing.ErrRevenueUnavailable},
		{name: "valid_foreign_history_still_allows_selected_empty_scope", change: "foreign"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, oldStore, _, ctx := revenueFixture(t)
			scope := checkoutResolverRequest().Scope
			if tc.change == "intent-mode" {
				checkout := lifecycleService(t, old, &lifecycleEvidenceFixture{}, time.Unix(1700000000, 0).UTC())
				lifecycleIntent(t, checkout, ctx)
			} else {
				f := revenueFact()
				f.Scope = scope
				f.ProviderCustomerID = "customer"
				if tc.change == "foreign" {
					f.Scope.AccountID = "foreign-account"
				}
				accept(t, revenueService(t, old), ctx, "legacy-payment", f)
			}
			r, store, _, _ := revenueFixture(t)
			restoreLegacyLifecycleHistory(t, ctx, oldStore, store, func(rows map[string][]recordstore.Record) {
				if tc.change == "fact-scope" {
					row := &rows[kindFact][0]
					var v persistedFact
					require.NoError(t, row.Decode(&v))
					v.Fact.Scope.AccountID = "foreign-account"
					row.Data, _ = json.Marshal(v)
				}
				if tc.change == "intent-mode" {
					row := &rows[kindCheckoutIntent][0]
					var v persistedCheckout
					require.NoError(t, row.Decode(&v))
					v.Request.Mode = "payment"
					row.Data, _ = json.Marshal(v)
				}
			})
			service := revenueService(t, r)
			var err error
			var out billing.LifecyclePreparationResult
			for n := 0; n < 20 && err == nil && out.State.Phase != billing.LifecyclePreparationComplete; n++ {
				out, err = service.PrepareLifecycleDiscovery(ctx, scope, 200)
			}
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, out)
			} else {
				require.Equal(t, billing.LifecyclePreparationComplete, out.State.Phase)
				require.Zero(t, out.State.Selected)
			}
		})
	}
}

func TestMongoLifecyclePreparationStateAdmission(t *testing.T) {
	for _, tc := range []struct{ name, change string }{
		{name: "marker_without_progress_is_not_fresh_preparation", change: "progress-missing"},
		{name: "completed_progress_without_marker_is_unavailable", change: "marker-missing"},
		{name: "prepared_time_disagreement_is_unavailable", change: "time"},
		{name: "marker_before_completion_is_unavailable", change: "early"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			scope := checkoutResolverRequest().Scope
			service := revenueService(t, r)
			if tc.change == "early" {
				_, err := service.PrepareLifecycleDiscovery(ctx, scope, 1)
				require.NoError(t, err)
				require.NoError(t, store.Transact(ctx, "test-premature-marker", func(tx recordstore.Tx) error {
					return insert(ctx, tx, kindLifecycleDiscoveryPreparation, checkoutScopeKey(scope), lifecycleSourcePartition(scope), lifecycleDiscoveryPreparation{scope, 1, time.Unix(1, 0).UTC()})
				}))
			} else {
				finishLifecyclePreparation(t, service, ctx, scope, 1)
				switch tc.change {
				case "progress-missing", "marker-missing":
					kind := kindLifecyclePreparationProgress
					if tc.change == "marker-missing" {
						kind = kindLifecycleDiscoveryPreparation
					}
					_, err := db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": kind})
					require.NoError(t, err)
				case "time":
					require.NoError(t, store.Transact(ctx, "test-progress-time", func(tx recordstore.Tx) error {
						p, row, err := get[lifecyclePreparationProgress](ctx, tx, kindLifecyclePreparationProgress, checkoutScopeKey(scope), lifecycleSourcePartition(scope))
						if err != nil {
							return err
						}
						p.PreparedAt = p.PreparedAt.Add(time.Second)
						row.Revision++
						row.Data, _ = json.Marshal(p)
						return tx.Replace(ctx, row, row.Revision-1)
					}))
				}
			}
			before := map[string][]recordstore.Record{}
			require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
				for _, kind := range []string{kindLifecyclePreparationProgress, kindLifecycleDiscoveryPreparation, kindLifecycleSourceEpoch} {
					rows, err := tx.Find(ctx, recordstore.Query{Kind: kind, Partition: lifecycleSourcePartition(scope), Limit: 10})
					if err != nil {
						return err
					}
					before[kind] = rows
				}
				return nil
			}))
			out, err := service.PrepareLifecycleDiscovery(ctx, scope, 1)
			require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			require.Zero(t, out)
			require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
				for kind, rows := range before {
					after, err := tx.Find(ctx, recordstore.Query{Kind: kind, Partition: lifecycleSourcePartition(scope), Limit: 10})
					if err != nil {
						return err
					}
					require.Equal(t, rows, after)
				}
				return nil
			}))
		})
	}
}
