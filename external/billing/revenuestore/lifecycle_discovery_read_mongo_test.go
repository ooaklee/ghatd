package revenuestore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Use the owning bounded operation for normal read fixtures. Literal invalid
// marker cases below still exercise admission; upgrade/cross-write recovery is
// covered by the separate restored-history preparation suite.
func preparedDiscoveryFixture(t *testing.T, store recordstore.Store, ctx context.Context, scope billing.RevenueScope) {
	t.Helper()
	r, err := NewRepository(store)
	require.NoError(t, err)
	finishLifecyclePreparation(t, revenueService(t, r), ctx, scope, 200)
}

type discoveryReadStore struct {
	recordstore.Store
	queries   []recordstore.Query
	repeat    bool
	lateError error
}
type discoveryQueryTx struct {
	recordstore.Tx
	store *discoveryReadStore
}

func (t *discoveryQueryTx) Find(ctx context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	t.store.queries = append(t.store.queries, q)
	return t.Tx.Find(ctx, q)
}
func (s *discoveryReadStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	return s.Store.Read(ctx, func(tx recordstore.Tx) error {
		bound := &discoveryQueryTx{tx, s}
		if s.repeat {
			if err := fn(bound); err != nil {
				return err
			}
		}
		if err := fn(bound); err != nil {
			return err
		}
		return s.lateError
	})
}
func TestMongoLifecycleDiscoveryJoinedReads(t *testing.T) {
	outage := errors.New("snapshot reply unavailable")
	for _, tc := range []struct {
		name, change string
		want         error
	}{
		{name: "acknowledged_checkout_original_snapshot", change: "checkout"},
		{name: "trial_anchor_before_payment"},
		{name: "paid_binding_original_intent_is_joined", change: "paid-binding"},
		{name: "provider_checkout_creation_after_authorization_is_valid", change: "paid-binding-later-created"},
		{name: "bound_checkout_without_fact_or_anchor_stays_not_refreshable", change: "binding-only"},
		{name: "paid_binding_missing_ack_withholds_page", change: "paid-binding-missing-ack", want: billing.ErrRevenueUnavailable},
		{name: "paid_binding_without_original_intent_withholds_page", change: "paid-binding-missing-intent", want: billing.ErrRevenueUnavailable},
		{name: "paid_original_joins_trial", change: "paid"},
		{name: "new_projection_without_migration_is_unprepared", change: "unprepared", want: billing.ErrLifecycleDiscoveryUnprepared},
		{name: "missing_original_intent_withholds_page", change: kindCheckoutIntent, want: billing.ErrRevenueUnavailable},
		{name: "missing_acknowledgement_withholds_page", change: kindCheckoutAck, want: billing.ErrRevenueUnavailable},
		{name: "missing_reverse_session_reservation_withholds_page", change: kindCheckoutSession, want: billing.ErrRevenueUnavailable},
		{name: "missing_first_anchor_receipt_withholds_page", change: kindCheckoutLifecycleReceipt, want: billing.ErrRevenueUnavailable},
		{name: "missing_first_anchor_withholds_page", change: kindCheckoutLifecycleAnchor, want: billing.ErrRevenueUnavailable},
		{name: "missing_original_payment_withholds_page", change: kindFact, want: billing.ErrRevenueUnavailable},
		{name: "expired_projection_is_not_owned_source", change: "expired", want: billing.ErrRevenueUnavailable},
		{name: "late_store_failure_withholds_joined_page", change: "late-error", want: outage},
		{name: "repeat_snapshot_callback_does_not_skip_sources", change: "repeat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			checkout := lifecycleService(t, r, p, time.Unix(1700000000, 0).UTC())
			i := lifecycleIntent(t, checkout, ctx)
			e := checkoutEvidence(i)
			var err error
			if strings.HasPrefix(tc.change, "paid-binding") || tc.change == "binding-only" {
				if tc.change == "paid-binding-later-created" {
					e.CreatedAt = e.CreatedAt.Add(time.Second)
				}
				p.evidence = e
				_, err = checkout.ResolveRevenueAssociation(ctx, checkoutResolverRequest())
				require.NoError(t, err)
				p.calls = 0
			} else {
				_, err = checkout.CaptureCheckoutLifecycleEvidence(ctx, i, e)
				require.NoError(t, err)
			}
			if tc.change == "paid" || tc.change == kindFact || strings.HasPrefix(tc.change, "paid-binding") {
				f := revenueFact()
				f.Scope = i.Scope
				f.PrincipalID = i.Request.UserID
				f.ProviderCustomerID = e.CustomerID
				f.SubscriptionID = e.SubscriptionID
				accept(t, revenueService(t, r), ctx, "original-discovery-paid", f)
			}
			if tc.change != "unprepared" {
				preparedDiscoveryFixture(t, store, ctx, i.Scope)
			}
			switch tc.change {
			case kindCheckoutIntent, kindCheckoutAck, kindCheckoutSession, kindCheckoutLifecycleReceipt, kindCheckoutLifecycleAnchor, kindFact:
				_, err = db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": tc.change})
				require.NoError(t, err)
			case "paid-binding-missing-ack":
				_, err = db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": kindCheckoutAck})
				require.NoError(t, err)
			case "paid-binding-missing-intent":
				_, err = db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": kindCheckoutIntent})
				require.NoError(t, err)
			case "expired":
				_, err = db.Collection("ghatd_owned_records").UpdateOne(ctx, bson.M{"kind": kindLifecycleSubscriptionSource}, bson.M{"$set": bson.M{"expires_at": time.Now().Add(time.Hour)}})
				require.NoError(t, err)
			}
			observed := &discoveryReadStore{Store: store, repeat: tc.change == "repeat"}
			if tc.change == "late-error" {
				observed.lateError = outage
			}
			adapter, err := NewRepository(observed)
			require.NoError(t, err)
			owner := revenueService(t, adapter)
			kind := billing.LifecycleSubscriptionSources
			if tc.change == "checkout" {
				kind = billing.LifecycleCheckoutSources
			}
			q := billing.LifecycleDiscoveryQuery{Scope: i.Scope, Kind: kind, Limit: 2}
			page, err := owner.DiscoverLifecycleSources(ctx, q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, page)
				return
			}
			if tc.change == "binding-only" {
				require.Empty(t, page.Items)
				require.True(t, page.ReachedEnd)
				return
			}
			require.Len(t, page.Items, 1)
			require.True(t, page.ReachedEnd)
			source := page.Items[0]
			if kind == billing.LifecycleCheckoutSources {
				require.Equal(t, i.ID, source.Intent.ID)
				require.Equal(t, i.Fingerprint, source.Intent.Fingerprint)
				require.Equal(t, i.SessionID, source.Intent.SessionID)
			} else if strings.HasPrefix(tc.change, "paid-binding") {
				require.Equal(t, i.ID, source.PaidOwnerIntent.ID)
				require.Equal(t, i.Fingerprint, source.PaidOwnerIntent.Fingerprint)
				require.Equal(t, i.SessionID, source.PaidOwnerIntent.SessionID)
				require.Equal(t, e.CreatedAt, source.PaidOwner.CheckoutCreatedAt)
				require.Empty(t, source.Anchor.IntentID)
			} else {
				require.Equal(t, i.ID, source.Anchor.IntentID)
				require.Equal(t, i.Fingerprint, source.AnchorIntent.Fingerprint)
				require.Equal(t, e.CustomerID, source.CustomerID)
			}
			require.Zero(t, p.calls)
			wantQueries := 1
			if tc.change == "repeat" {
				wantQueries = 2
			}
			require.Len(t, observed.queries, wantQueries)
			for _, query := range observed.queries {
				require.Equal(t, lifecycleSourcePartition(i.Scope), query.Partition)
				require.Equal(t, 2, query.Limit)
				require.Empty(t, query.AfterID)
				require.NotEqual(t, kindFact, query.Kind)
			}
			n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": bson.M{"$in": []string{kindSubscriptionStatusHead, kindSubscriptionStatusCapture}}})
			require.NoError(t, err)
			require.Zero(t, n)
		})
	}
}

func TestMongoLifecycleDiscoveryScopedCursorAndIndex(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		otherAccount, otherMode bool
	}{{name: "bounded_same_scope"}, {name: "merchant_account_isolation", otherAccount: true}, {name: "live_mode_isolation", otherMode: true}} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			checkout := lifecycleService(t, r, p, time.Unix(1700000000, 0).UTC())
			scope := checkoutResolverRequest().Scope
			preparedDiscoveryFixture(t, store, ctx, scope)
			expected := map[string]bool{}
			for n := 0; n < 4; n++ {
				selected := scope
				if n >= 2 && tc.otherAccount {
					selected.AccountID = "other-merchant"
				}
				if n >= 2 && tc.otherMode {
					selected.LiveMode = !scope.LiveMode
				}
				q := historicalCheckoutRequest()
				q.IdempotencyKey = fmt.Sprintf("indexed-%d", n)
				i, err := checkout.PrepareCheckout(ctx, selected, q)
				require.NoError(t, err)
				require.NoError(t, checkout.AcknowledgeCheckout(ctx, i, fmt.Sprintf("cs_indexed_%d", n)))
				if selected == scope {
					expected[i.ID] = true
				}
			}
			observed := &discoveryReadStore{Store: store}
			adapter, err := NewRepository(observed)
			require.NoError(t, err)
			owner := revenueService(t, adapter)
			q := billing.LifecycleDiscoveryQuery{Scope: scope, Kind: billing.LifecycleCheckoutSources, Limit: 1}
			seen := map[string]bool{}
			for pass := 0; pass < 6; pass++ {
				page, err := owner.DiscoverLifecycleSources(ctx, q)
				require.NoError(t, err)
				require.LessOrEqual(t, len(page.Items), 1)
				for _, c := range page.Items {
					require.False(t, seen[c.Intent.ID])
					require.Equal(t, scope, c.Scope)
					seen[c.Intent.ID] = true
				}
				if page.ReachedEnd {
					break
				}
				require.NotEmpty(t, page.NextCursor)
				q.Cursor = page.NextCursor
			}
			require.Equal(t, expected, seen)
			for _, query := range observed.queries {
				require.Equal(t, 1, query.Limit)
				require.Equal(t, lifecycleSourcePartition(scope), query.Partition)
			}
			var explain bson.M
			require.NoError(t, db.RunCommand(ctx, bson.D{{Key: "explain", Value: bson.D{{Key: "find", Value: "ghatd_owned_records"}, {Key: "filter", Value: bson.M{"kind": kindLifecycleCheckoutSource, "partition": lifecycleSourcePartition(scope)}}, {Key: "sort", Value: bson.D{{Key: "id", Value: 1}}}, {Key: "limit", Value: 1}}}, {Key: "verbosity", Value: "queryPlanner"}}).Decode(&explain))
			plan := fmt.Sprint(explain)
			require.Contains(t, plan, "owned_record_partition")
			require.Contains(t, plan, "IXSCAN")
			require.NotContains(t, strings.ToUpper(plan), "COLLSCAN")
		})
	}
}

func TestMongoLifecycleDiscoveryPreparationAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		want         error
	}{{name: "prepared_fresh_empty_snapshot"}, {name: "no_preparation_refuses_empty", change: "absent", want: billing.ErrLifecycleDiscoveryUnprepared}, {name: "unknown_schema_refuses_empty", change: "schema", want: billing.ErrRevenueUnavailable}, {name: "different_scope_refuses_empty", change: "scope", want: billing.ErrRevenueUnavailable}, {name: "expiry_refuses_preparation", change: "expiry", want: billing.ErrRevenueUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, ctx := revenueFixture(t)
			scope := checkoutResolverRequest().Scope
			if tc.change != "absent" {
				prep := lifecycleDiscoveryPreparation{Scope: scope, Schema: 1, PreparedAt: time.Unix(1700000000, 0).UTC()}
				if tc.change == "schema" {
					prep.Schema = 2
				}
				if tc.change == "scope" {
					prep.Scope.AccountID = "other-merchant"
				}
				row, err := recordstore.NewRecord(kindLifecycleDiscoveryPreparation, checkoutScopeKey(scope), lifecycleSourcePartition(scope), 1, prep)
				require.NoError(t, err)
				if tc.change == "expiry" {
					row, err = row.WithExpiration(time.Now().Add(time.Hour))
					require.NoError(t, err)
				}
				require.NoError(t, store.Transact(ctx, "test-schema-readiness", func(tx recordstore.Tx) error { return tx.Insert(ctx, row) }))
			}
			page, err := revenueService(t, r).DiscoverLifecycleSources(ctx, billing.LifecycleDiscoveryQuery{Scope: scope, Kind: billing.LifecycleSubscriptionSources, Limit: 1})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, page)
			} else {
				require.True(t, page.ReachedEnd)
				require.Empty(t, page.Items)
			}
		})
	}
}
