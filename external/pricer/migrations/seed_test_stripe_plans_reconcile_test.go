package migrations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/ooaklee/ghatd/external/pricer"
	pricermigrations "github.com/ooaklee/ghatd/external/pricer/migrations"
)

func TestE2E_TestStripePlansReconciliationIsIdempotentAndReversible(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E pricing reconciliation test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	require.NoError(t, pricermigrations.InitTestPlansSeedUp(db))

	// A fresh seed already contains the target matrix, so the forward migration
	// must recognize it without rewriting the documents.
	require.NoError(t, pricermigrations.InitTestStripePlansSeedReconcileUp(db))
	assert.Equal(t, int64(3), countOwnedStripeFixturePlans(t, ctx, db))

	// Reconcile Down removes only the provider fixtures and preserves the old
	// three-plan comparison catalogue.
	require.NoError(t, pricermigrations.InitTestStripePlansSeedReconcileDown(db))
	assert.Equal(t, int64(0), countOwnedStripeFixturePlans(t, ctx, db))
	baseCount, err := db.Collection(pricer.PricePlansCollection).CountDocuments(ctx, bson.M{
		"created_by_id": pricermigrations.TestPlansSeedCreatedByID,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), baseCount)

	// This is the old-seed upgrade path. A second Up is a strict no-op.
	require.NoError(t, pricermigrations.InitTestStripePlansSeedReconcileUp(db))
	require.NoError(t, pricermigrations.InitTestStripePlansSeedReconcileUp(db))
	assert.Equal(t, int64(3), countOwnedStripeFixturePlans(t, ctx, db))
}

func TestE2E_TestStripePlansReconciliationRequiresOwnedBaseSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E pricing reconciliation test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))

	err := pricermigrations.InitTestStripePlansSeedReconcileUp(db)
	require.ErrorIs(t, err, pricermigrations.ErrTestStripePlansSeedConflict)
	assert.Equal(t, int64(0), countOwnedStripeFixturePlans(t, ctx, db))
}

func TestE2E_TestStripePlansReconciliationRejectsGlobalPriceCollisionWithoutWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E pricing reconciliation test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	require.NoError(t, pricermigrations.InitTestPlansSeedUp(db))
	require.NoError(t, pricermigrations.InitTestStripePlansSeedReconcileDown(db))

	_, err := db.Collection(pricer.PricePlansCollection).InsertOne(ctx, bson.M{
		"_id":           "foreign-plan-id",
		"_nano_id":      "foreign-plan-nano-id",
		"slug":          "foreign-plan",
		"name":          "Foreign plan",
		"status":        pricer.PricePlanStatusDraft,
		"created_at":    "2026-01-01T00:00:00Z",
		"created_by_id": "foreign-owner",
		"costs": []bson.M{
			{
				"_id":             "foreign-cost-id",
				"amount":          100,
				"currency":        "USD",
				"billing_cadence": pricer.PriceBillingCadenceMonthly,
				"provider_refs": []bson.M{
					{
						"provider":          pricer.PriceProviderStripe,
						"provider_price_id": pricermigrations.TestSeedStripeOneTimePriceID,
					},
				},
			},
		},
	})
	require.NoError(t, err)

	err = pricermigrations.InitTestStripePlansSeedReconcileUp(db)
	require.ErrorIs(t, err, pricermigrations.ErrTestStripePlansSeedConflict)
	assert.Equal(t, int64(0), countOwnedStripeFixturePlans(t, ctx, db))
}

func TestE2E_TestStripePlansReconciliationRejectsForeignCollisionBesideExactFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E pricing reconciliation test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	require.NoError(t, pricermigrations.InitTestPlansSeedUp(db))

	_, err := db.Collection(pricer.PricePlansCollection).InsertOne(ctx, bson.M{
		"_id":           "foreign-plan-beside-fixtures",
		"_nano_id":      "foreign-plan-beside-fixtures",
		"slug":          "foreign-plan-beside-fixtures",
		"name":          "Foreign plan beside fixtures",
		"status":        pricer.PricePlanStatusDraft,
		"created_at":    "2026-01-01T00:00:00Z",
		"created_by_id": "foreign-owner",
		"costs": []bson.M{
			{
				"_id":             "foreign-cost-beside-fixtures",
				"amount":          100,
				"currency":        "USD",
				"billing_cadence": pricer.PriceBillingCadenceMonthly,
				"provider_refs": []bson.M{
					{
						"provider":          pricer.PriceProviderStripe,
						"provider_price_id": pricermigrations.TestSeedStripeTrialMonthPriceID,
					},
				},
			},
		},
	})
	require.NoError(t, err)

	err = pricermigrations.InitTestStripePlansSeedReconcileUp(db)
	require.ErrorIs(t, err, pricermigrations.ErrTestStripePlansSeedConflict)
	assert.Equal(t, int64(3), countOwnedStripeFixturePlans(t, ctx, db), "exact owned fixtures must not be rewritten or removed")
}

func TestE2E_TestStripePlansReconciliationRejectsNonPublicFixtureState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E pricing reconciliation test in short mode")
	}

	tests := []struct {
		name   string
		update bson.M
	}{
		{name: "missing publication", update: bson.M{"$unset": bson.M{"published_at": ""}}},
		{name: "future publication", update: bson.M{"$set": bson.M{"published_at": "2099-01-01T00:00:00Z"}}},
		{name: "soft deleted", update: bson.M{"$set": bson.M{"deleted_at": "2026-08-09T00:00:00Z", "deleted_by_id": "operator"}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, db, _ := setupE2EMongo(t)
			require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
			require.NoError(t, pricermigrations.InitTestPlansSeedUp(db))

			_, err := db.Collection(pricer.PricePlansCollection).UpdateOne(context.Background(), bson.M{
				"_id": pricermigrations.TestSeedPlanStripeOneTimeID,
			}, test.update)
			require.NoError(t, err)

			err = pricermigrations.InitTestStripePlansSeedReconcileUp(db)
			require.ErrorIs(t, err, pricermigrations.ErrTestStripePlansSeedConflict)
		})
	}
}

func TestE2E_TestStripePlansReconciliationRefusesModifiedFixtureRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E pricing reconciliation test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	require.NoError(t, pricermigrations.InitTestPlansSeedUp(db))

	_, err := db.Collection(pricer.PricePlansCollection).UpdateOne(ctx, bson.M{
		"_id": pricermigrations.TestSeedPlanStripeOneTimeID,
	}, bson.M{
		"$set": bson.M{"name": "Operator-modified fixture"},
	})
	require.NoError(t, err)

	err = pricermigrations.InitTestStripePlansSeedReconcileDown(db)
	require.Error(t, err)
	require.True(t, errors.Is(err, pricermigrations.ErrTestStripePlansSeedConflict))
	assert.Equal(t, int64(3), countOwnedStripeFixturePlans(t, ctx, db))
}

func countOwnedStripeFixturePlans(t *testing.T, ctx context.Context, db *mongo.Database) int64 {
	t.Helper()
	count, err := db.Collection(pricer.PricePlansCollection).CountDocuments(ctx, bson.M{
		"created_by_id": pricermigrations.TestStripePlansSeedCreatedByID,
	})
	require.NoError(t, err)
	return count
}
