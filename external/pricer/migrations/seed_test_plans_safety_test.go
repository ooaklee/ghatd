package migrations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/ooaklee/ghatd/external/pricer"
	pricermigrations "github.com/ooaklee/ghatd/external/pricer/migrations"
)

func TestE2E_TestPlansSeedPreflightRejectsGlobalProviderPriceCollision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test-plans seed safety test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	_, err := db.Collection(pricer.PricePlansCollection).InsertOne(ctx, bson.M{
		"_id":           "foreign-preflight-plan",
		"_nano_id":      "foreign-preflight-plan",
		"slug":          "foreign-preflight-plan",
		"name":          "Foreign preflight plan",
		"status":        pricer.PricePlanStatusDraft,
		"created_at":    "2026-01-01T00:00:00Z",
		"created_by_id": "foreign-owner",
		"costs": []bson.M{
			{
				"_id":             "foreign-preflight-cost",
				"amount":          100,
				"currency":        "USD",
				"billing_cadence": pricer.PriceBillingCadenceMonthly,
				"provider_refs": []bson.M{
					{
						"provider":          pricer.PriceProviderStripe,
						"provider_price_id": pricermigrations.TestSeedStripeRecurringMonthPriceID,
					},
				},
			},
		},
	})
	require.NoError(t, err)

	err = pricermigrations.InitTestPlansSeedUp(db)
	require.ErrorIs(t, err, pricermigrations.ErrTestPlansSeedConflict)
	assertTestPlansSeedAbsent(t, ctx, db)
	foreignCount, countErr := db.Collection(pricer.PricePlansCollection).CountDocuments(ctx, bson.M{"_id": "foreign-preflight-plan"})
	require.NoError(t, countErr)
	assert.Equal(t, int64(1), foreignCount)
}

func TestE2E_TestPlansSeedCompensatesWhenPlanInsertionFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test-plans seed safety test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	_, err := db.Collection(pricer.PricePlansCollection).InsertOne(ctx, bson.M{
		"_id":           "foreign-name-collision-plan",
		"_nano_id":      "foreign-name-collision-plan",
		"slug":          "foreign-name-collision-plan",
		"name":          "Stripe One-Time Test",
		"status":        pricer.PricePlanStatusDraft,
		"created_at":    "2026-01-01T00:00:00Z",
		"created_by_id": "foreign-owner",
	})
	require.NoError(t, err)
	_, err = db.Collection(pricer.PricePlansCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	require.NoError(t, err)

	err = pricermigrations.InitTestPlansSeedUp(db)
	require.Error(t, err)
	assertTestPlansSeedAbsent(t, ctx, db)
	foreignCount, countErr := db.Collection(pricer.PricePlansCollection).CountDocuments(ctx, bson.M{"_id": "foreign-name-collision-plan"})
	require.NoError(t, countErr)
	assert.Equal(t, int64(1), foreignCount, "compensation must not remove the foreign conflicting plan")
}

func assertTestPlansSeedAbsent(t *testing.T, ctx context.Context, db *mongo.Database) {
	t.Helper()
	planCount, err := db.Collection(pricer.PricePlansCollection).CountDocuments(ctx, bson.M{
		"created_by_id": bson.M{"$in": []string{
			pricermigrations.TestPlansSeedCreatedByID,
			pricermigrations.TestStripePlansSeedCreatedByID,
		}},
	})
	require.NoError(t, err)
	assert.Zero(t, planCount)
	featureCount, err := db.Collection(pricer.PriceFeaturesCollection).CountDocuments(ctx, bson.M{
		"created_by_id": pricermigrations.TestPlansSeedCreatedByID,
	})
	require.NoError(t, err)
	assert.Zero(t, featureCount)
}
