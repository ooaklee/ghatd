package revenuestore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// Audit disposition: named native cases exercise the historical installation
// and modern discovery boundary in isolated encrypted replica-set databases.
func legacyDiscoveryReceipt(t *testing.T, r *Repository, ctx context.Context) billing.LegacyCheckoutMigrationReceipt {
	t.Helper()
	scope := checkoutResolverRequest().Scope
	owner := revenueService(t, r)
	source, err := owner.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: scope, EnvelopeID: "legacy-original", SourceFingerprint: "verified-original", QuarantineReason: "historical_payer_plan_pending"})
	require.NoError(t, err)
	created := time.Unix(1700000000, 0).UTC()
	linked := source.AcceptedAt.Add(time.Hour)
	a := billing.CheckoutAssociation{Scope: scope, SessionID: "cs_legacy", CustomerID: "cus_legacy", SubscriptionID: "sub_legacy", ProviderPriceID: "price_legacy", Currency: "GBP", PrincipalID: "legacy-payer", PlanID: "legacy-plan", CostID: "legacy-cost", CheckoutCreatedAt: created, LinkedAt: linked}
	providerScope := paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID, LiveMode: scope.LiveMode}
	receipt := billing.LegacyCheckoutMigrationReceipt{ID: billing.LegacyCheckoutMigrationID(scope, a.SessionID), Version: billing.LegacyCheckoutAssociationVersion, MigratedAt: linked, ActorID: "reviewed-operator", Reason: "reviewed_history", SourceID: source.ID, EnvelopeID: source.EnvelopeID, SourceObservationFingerprint: source.Fingerprint, SourceFingerprint: source.SourceFingerprint, RequestFingerprint: strings.Repeat("a", 64), Association: a,
		Checkout: paymentprovider.RevenueCheckoutEvidence{Scope: providerScope, SessionID: a.SessionID, ClientReferenceID: a.PrincipalID, CustomerID: a.CustomerID, SubscriptionID: a.SubscriptionID, PriceID: a.ProviderPriceID, Currency: a.Currency, Mode: "subscription", Status: "complete", CreatedAt: created, UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month"},
		Invoice:  paymentprovider.RevenueInvoiceEvidence{Scope: providerScope, InvoiceID: "in_legacy", PaymentID: "pi_legacy", CustomerID: a.CustomerID, SubscriptionID: a.SubscriptionID, Currency: a.Currency, CurrencyExponent: 2, PaidAt: created.Add(time.Minute), GrossPaidMinor: 1000, Lines: []paymentprovider.RevenueLineEvidence{{ID: "li_legacy", SubscriptionID: a.SubscriptionID, PriceID: a.ProviderPriceID, NetPaidMinor: 1000}}},
	}
	receipt.ProofFingerprint = receipt.ProofDigest()
	require.NoError(t, receipt.Validate())
	return receipt
}

func installLegacyDiscovery(t *testing.T, r *Repository, ctx context.Context, receipt billing.LegacyCheckoutMigrationReceipt) {
	t.Helper()
	require.NoError(t, r.WithCheckoutTransaction(ctx, receipt.Association.Scope, func(tx billing.CheckoutTx) error {
		_, err := tx.(billing.LegacyCheckoutMigrationTx).InstallLegacyCheckoutAssociation(ctx, receipt)
		return err
	}))
}

func TestMongoLegacyCheckoutLifecycleDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		prepareBefore, paid, anchored bool
		limit                         int
	}{
		{name: "installed_after_preparation_binding_only", prepareBefore: true, limit: 2},
		{name: "installed_before_preparation_binding_only", limit: 2},
		{name: "binding_only_advances_one_row_cursor", prepareBefore: true, limit: 1},
		{name: "accepted_fact_after_preparation", prepareBefore: true, paid: true, limit: 2},
		{name: "accepted_fact_before_preparation", paid: true, limit: 2},
		{name: "same_owner_modern_anchor_coexists", prepareBefore: true, anchored: true, limit: 2},
		{name: "same_owner_modern_anchor_before_preparation", anchored: true, limit: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			core, logs := observer.New(zap.DebugLevel)
			ctx = logger.TransitWith(ctx, zap.New(core))
			receipt := legacyDiscoveryReceipt(t, r, ctx)
			q := billing.LifecycleDiscoveryQuery{Scope: receipt.Association.Scope, Kind: billing.LifecycleSubscriptionSources, Limit: tc.limit}
			if tc.prepareBefore {
				preparedDiscoveryFixture(t, store, ctx, q.Scope)
				page, err := revenueService(t, r).DiscoverLifecycleSources(ctx, q)
				require.NoError(t, err)
				require.Empty(t, page.Items)
			}
			installLegacyDiscovery(t, r, ctx, receipt)
			if tc.anchored {
				checkout, err := billing.NewCheckoutService(r, fixtureClock{receipt.MigratedAt}, &checkoutEvidenceFixture{})
				require.NoError(t, err)
				request := historicalCheckoutRequest()
				a := receipt.Association
				request.UserID, request.UserReference, request.PlanID, request.CostID, request.PriceID = a.PrincipalID, a.PrincipalID, a.PlanID, a.CostID, a.ProviderPriceID
				intent, err := checkout.PrepareCheckout(ctx, a.Scope, request)
				require.NoError(t, err)
				intent.SessionID = "cs_modern_same_owner"
				require.NoError(t, checkout.AcknowledgeCheckout(ctx, intent, intent.SessionID))
				e := checkoutEvidence(intent)
				e.SessionID, e.CustomerID, e.SubscriptionID = intent.SessionID, a.CustomerID, a.SubscriptionID
				_, err = checkout.CaptureCheckoutLifecycleEvidence(ctx, intent, e)
				require.NoError(t, err)
			}
			if tc.paid {
				f := revenueFact()
				a := receipt.Association
				f.Scope, f.PrincipalID, f.ProviderCustomerID, f.SubscriptionID = a.Scope, a.PrincipalID, a.CustomerID, a.SubscriptionID
				f.PlanID, f.CostID, f.Currency = a.PlanID, a.CostID, a.Currency
				accept(t, revenueService(t, r), ctx, "legacy-accepted-payment", f)
			}
			if !tc.prepareBefore {
				preparedDiscoveryFixture(t, store, ctx, q.Scope)
			}
			page, err := revenueService(t, r).DiscoverLifecycleSources(ctx, q)
			require.NoError(t, err)
			if tc.paid || tc.anchored {
				require.Len(t, page.Items, 1)
				if tc.paid {
					require.NotEmpty(t, page.Items[0].Fact.ID)
				}
				if tc.anchored {
					require.NotEmpty(t, page.Items[0].Anchor.IntentID)
				}
				require.NotNil(t, page.Items[0].LegacyPaidOwner)
				require.Equal(t, receipt, *page.Items[0].LegacyPaidOwner)
				require.Empty(t, page.Items[0].PaidOwnerIntent.ID)
			} else {
				require.Empty(t, page.Items)
			}
			if tc.limit == 1 {
				require.False(t, page.ReachedEnd)
				require.NotEmpty(t, page.NextCursor)
				q.Cursor = page.NextCursor
				page, err = revenueService(t, r).DiscoverLifecycleSources(ctx, q)
				require.NoError(t, err)
				require.Empty(t, page.Items)
			}
			require.True(t, page.ReachedEnd)
			for _, kind := range []string{kindCheckoutIntent, kindCheckoutAck, kindCheckoutLifecycleAnchor, kindSubscriptionStatusHead} {
				n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, map[string]any{"kind": kind})
				require.NoError(t, err)
				if tc.anchored && kind != kindSubscriptionStatusHead {
					require.EqualValues(t, 1, n)
				} else {
					require.Zero(t, n)
				}
			}
			require.Zero(t, logs.FilterLevelExact(zap.ErrorLevel).Len(), "valid historical discovery and preparation must not produce transaction errors")
		})
	}
}

func TestMongoLegacyCheckoutLifecycleDiscoveryCorruption(t *testing.T) {
	for _, tc := range []struct {
		name, kind, change string
		want               error
	}{
		{"missing_receipt_has_no_global_history_scan", kindLegacyCheckoutMigration, "missing", billing.ErrRevenueUnavailable},
		{"corrupt_receipt_proof", kindLegacyCheckoutMigration, "proof", billing.ErrRevenueUnavailable},
		{"receipt_revision", kindLegacyCheckoutMigration, "revision", billing.ErrRevenueUnavailable},
		{"missing_reservation", kindCheckoutSession, "missing", billing.ErrRevenueUnavailable},
		{"modern_reservation_cannot_prove_historical_owner", kindCheckoutSession, "modern", billing.ErrRevenueConflict},
		{"wrong_reservation_receipt", kindCheckoutSession, "reservation", billing.ErrRevenueConflict},
		{"reservation_expiry", kindCheckoutSession, "expiry", billing.ErrRevenueUnavailable},
		{"missing_price_association", kindCheckoutAssociation, "missing", billing.ErrRevenueUnavailable},
		{"price_association_metadata", kindCheckoutAssociation, "revision", billing.ErrRevenueUnavailable},
		{"principal_plan_disagrees_with_receipt", kindCheckoutPrincipal, "principal", billing.ErrRevenueConflict},
		{"missing_original_source", kindObservation, "missing", billing.ErrRevenueUnavailable},
		{"source_fingerprint_disagrees_with_receipt", kindObservation, "source", billing.ErrRevenueConflict},
		{"source_expiry", kindObservation, "expiry", billing.ErrRevenueUnavailable},
		{"source_revision", kindObservation, "revision", billing.ErrRevenueUnavailable},
		{"projection_expiry", kindLifecycleSubscriptionSource, "expiry", billing.ErrRevenueUnavailable},
		{"projection_owner_disagrees", kindLifecycleSubscriptionSource, "projection", billing.ErrRevenueConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, ctx := revenueFixture(t)
			receipt := legacyDiscoveryReceipt(t, r, ctx)
			preparedDiscoveryFixture(t, store, ctx, receipt.Association.Scope)
			installLegacyDiscovery(t, r, ctx, receipt)
			filter := bson.M{"kind": tc.kind}
			switch tc.change {
			case "missing":
				_, err := db.Collection("ghatd_owned_records").DeleteOne(ctx, filter)
				require.NoError(t, err)
			default:
				// Reinsert only this isolated fixture row through the native codec.
				// AAD must match the chosen metadata, so successful decrypt proves
				// the domain proof/metadata rejection rather than cipher tampering.
				var next recordstore.Record
				require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
					rows, err := tx.Find(ctx, recordstore.Query{Kind: tc.kind, Limit: 2})
					if err != nil {
						return err
					}
					require.Len(t, rows, 1)
					row := rows[0]
					var payload any
					switch tc.change {
					case "revision", "expiry":
						require.NoError(t, row.Decode(&payload))
					case "proof":
						var p persistedLegacyCheckoutMigration
						require.NoError(t, row.Decode(&p))
						p.ProofFingerprint = strings.Repeat("b", 64)
						payload = p
					case "modern", "reservation":
						var p checkoutAcknowledgement
						require.NoError(t, row.Decode(&p))
						if tc.change == "modern" {
							p.IntentID = "modern-intent"
						} else {
							p.LegacyMigrationID = "wrong-receipt"
						}
						payload = p
					case "principal":
						var p billing.CheckoutAssociation
						require.NoError(t, row.Decode(&p))
						p.PlanID = "wrong-plan"
						payload = p
					case "source":
						var p persistedObservation
						require.NoError(t, row.Decode(&p))
						p.SourceFingerprint = "wrong-original"
						payload = p
					case "projection":
						var p lifecycleSubscriptionSource
						require.NoError(t, row.Decode(&p))
						p.PrincipalID = "other-payer"
						payload = p
					}
					next, err = recordstore.NewRecord(row.Kind, row.ID, row.Partition, row.Revision, payload)
					if err != nil {
						return err
					}
					next.Sequence, next.State, next.ExpiresAt = row.Sequence, row.State, row.ExpiresAt
					if tc.change == "revision" {
						next.Revision++
					}
					if tc.change == "expiry" {
						next, err = next.WithExpiration(time.Now().Add(time.Hour))
						if err != nil {
							return err
						}
					}
					return nil
				}))
				deleted, err := db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": next.Kind, "id": next.ID})
				require.NoError(t, err)
				require.EqualValues(t, 1, deleted.DeletedCount)
				require.NoError(t, store.Transact(ctx, "test-legacy-corruption", func(tx recordstore.Tx) error { return tx.Insert(ctx, next) }))
				require.NoError(t, store.Read(ctx, func(tx recordstore.Tx) error {
					stored, err := tx.Get(ctx, next.Kind, next.ID)
					if err != nil {
						return err
					}
					var decoded any
					require.NoError(t, stored.Decode(&decoded))
					require.NotNil(t, decoded)
					require.JSONEq(t, string(next.Data), string(stored.Data))
					require.Equal(t, next.Revision, stored.Revision)
					require.Equal(t, next.State, stored.State)
					require.Equal(t, next.Sequence, stored.Sequence)
					require.Equal(t, next.ExpiresAt, stored.ExpiresAt)
					return nil
				}))
			}
			observed := &discoveryReadStore{Store: store}
			adapter, err := NewRepository(observed)
			require.NoError(t, err)
			page, err := revenueService(t, adapter).DiscoverLifecycleSources(ctx, billing.LifecycleDiscoveryQuery{Scope: receipt.Association.Scope, Kind: billing.LifecycleSubscriptionSources, Limit: 1})
			require.ErrorIs(t, err, tc.want, "authenticated payloads must reach the intended owning proof rejection")
			require.Zero(t, page, "invalid binding-only rows must not advance or disclose a cursor")
			require.Len(t, observed.queries, 1, "discovery cannot scan global history for missing proof")
			require.Equal(t, kindLifecycleSubscriptionSource, observed.queries[0].Kind)
		})
	}
}
