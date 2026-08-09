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

func TestE2E_PricingStarterSeedReconciliationRepairsOnlyLegacySeed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E starter pricing reconciliation test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	require.NoError(t, pricermigrations.InitPricingSeedUp(db))
	makeStarterSeedLegacy(t, ctx, db)

	require.NoError(t, pricermigrations.InitPricingSeedProviderNeutralReconcileUp(db))
	require.NoError(t, pricermigrations.InitPricingSeedProviderNeutralReconcileUp(db), "second Up must be idempotent")
	plan := readStarterPlan(t, ctx, db)
	assertStarterTrials(t, plan, 0)
	assert.True(t, starterReconciliationMarker(plan), "legacy repair must record ownership of the change")

	require.NoError(t, pricermigrations.InitPricingSeedProviderNeutralReconcileDown(db))
	require.NoError(t, pricermigrations.InitPricingSeedProviderNeutralReconcileDown(db), "second Down must be idempotent")
	plan = readStarterPlan(t, ctx, db)
	assertStarterTrials(t, plan, 14)
	assert.False(t, starterReconciliationMarker(plan), "rollback must remove only its own marker")
}

func TestE2E_PricingStarterSeedReconciliationLeavesFreshTargetUntouched(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E starter pricing reconciliation test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	require.NoError(t, pricermigrations.InitPricingSeedUp(db))

	require.NoError(t, pricermigrations.InitPricingSeedProviderNeutralReconcileUp(db))
	require.NoError(t, pricermigrations.InitPricingSeedProviderNeutralReconcileDown(db))
	plan := readStarterPlan(t, ctx, db)
	assertStarterTrials(t, plan, 0)
	assert.False(t, starterReconciliationMarker(plan), "fresh target data must not be marked as migrated")
}

func TestE2E_PricingStarterSeedReconciliationRefusesModifiedCosts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E starter pricing reconciliation test in short mode")
	}

	ctx, _, db, _ := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	require.NoError(t, pricermigrations.InitPricingSeedUp(db))
	makeStarterSeedLegacy(t, ctx, db)

	_, err := db.Collection(pricer.PricePlansCollection).UpdateOne(ctx, bson.M{"slug": "starter"}, bson.M{
		"$set": bson.M{"costs.0.amount": 99},
	})
	require.NoError(t, err)

	err = pricermigrations.InitPricingSeedProviderNeutralReconcileUp(db)
	require.Error(t, err)
	assert.True(t, errors.Is(err, pricermigrations.ErrPricingSeedConflict))
	plan := readStarterPlan(t, ctx, db)
	assert.Equal(t, int64(99), plan.Costs[0].Amount)
	assertStarterTrials(t, plan, 14)
}

func makeStarterSeedLegacy(t *testing.T, ctx context.Context, db *mongo.Database) {
	t.Helper()
	result, err := db.Collection(pricer.PricePlansCollection).UpdateOne(ctx, bson.M{"slug": "starter"}, bson.M{
		"$set": bson.M{
			"costs.0.trial_period_days": 14,
			"costs.1.trial_period_days": 14,
		},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.ModifiedCount)
}

func readStarterPlan(t *testing.T, ctx context.Context, db *mongo.Database) *pricer.PricePlan {
	t.Helper()
	var plan pricer.PricePlan
	require.NoError(t, db.Collection(pricer.PricePlansCollection).FindOne(ctx, bson.M{"slug": "starter"}).Decode(&plan))
	return &plan
}

func assertStarterTrials(t *testing.T, plan *pricer.PricePlan, trialDays int) {
	t.Helper()
	require.Len(t, plan.Costs, 2)
	for _, cost := range plan.Costs {
		assert.Equal(t, trialDays, cost.TrialPeriodDays)
	}
}

func starterReconciliationMarker(plan *pricer.PricePlan) bool {
	seedReconciliations, ok := plan.Metadata["seed_reconciliations"].(bson.D)
	if !ok {
		return false
	}
	for _, entry := range seedReconciliations {
		if entry.Key == "provider_neutral_costs_v1" {
			value, _ := entry.Value.(bool)
			return value
		}
	}
	return false
}
