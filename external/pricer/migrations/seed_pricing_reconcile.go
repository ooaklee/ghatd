package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/ooaklee/ghatd/external/pricer"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const providerNeutralStarterCostsReconciliation = "provider_neutral_costs_v1"

// ErrPricingSeedConflict indicates that a starter-seed reconciliation found
// operator-managed or structurally unexpected data and refused to overwrite it.
var ErrPricingSeedConflict = errors.New("pricing seed conflict")

type starterSeedProjection struct {
	ID          string             `bson:"_id"`
	Slug        string             `bson:"slug"`
	CreatedByID string             `bson:"created_by_id"`
	Costs       []pricer.PriceCost `bson:"costs"`
	Metadata    struct {
		SeedReconciliations struct {
			ProviderNeutralCosts bool `bson:"provider_neutral_costs_v1"`
		} `bson:"seed_reconciliations,omitempty"`
	} `bson:"metadata,omitempty"`
}

// InitPricingSeedProviderNeutralReconcileUp removes the obsolete trial fields
// from an already-applied, owned starter seed. A missing seed is a safe no-op;
// an altered seed fails closed. Fresh databases already contain the target
// shape and are not marked as migrated.
func InitPricingSeedProviderNeutralReconcileUp(db *mongo.Database) error { //Up
	plan, err := loadStarterSeedProjection(db)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return err
	}

	legacy, target, err := classifyStarterSeedProjection(plan)
	if err != nil {
		return err
	}
	if target {
		return nil
	}
	if !legacy || plan.Metadata.SeedReconciliations.ProviderNeutralCosts {
		return fmt.Errorf("%w: starter trial state is inconsistent", ErrPricingSeedConflict)
	}

	result, err := db.Collection(pricer.PricePlansCollection).UpdateOne(
		context.Background(),
		starterSeedReconciliationFilter(true, false),
		bson.M{
			"$unset": bson.M{"costs.$[cost].trial_period_days": ""},
			"$set": bson.M{
				"metadata.seed_reconciliations." + providerNeutralStarterCostsReconciliation: true,
			},
		},
		options.UpdateOne().SetArrayFilters([]any{
			bson.M{"cost._id": bson.M{"$in": []string{seedPlanStarterCostMonthID, seedPlanStarterCostYearID}}},
		}),
	)
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 || result.ModifiedCount != 1 {
		return fmt.Errorf("%w: starter seed changed during reconciliation", ErrPricingSeedConflict)
	}
	return nil
}

// InitPricingSeedProviderNeutralReconcileDown restores the legacy trial fields
// only when Up marked this exact owned seed as migrated. Fresh target seeds and
// absent seeds remain unchanged.
func InitPricingSeedProviderNeutralReconcileDown(db *mongo.Database) error { //Down
	plan, err := loadStarterSeedProjection(db)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return err
	}

	legacy, target, err := classifyStarterSeedProjection(plan)
	if err != nil {
		return err
	}
	migrated := plan.Metadata.SeedReconciliations.ProviderNeutralCosts
	if legacy && !migrated {
		return nil
	}
	if !target || !migrated {
		if target {
			return nil
		}
		return fmt.Errorf("%w: starter trial state is inconsistent", ErrPricingSeedConflict)
	}

	result, err := db.Collection(pricer.PricePlansCollection).UpdateOne(
		context.Background(),
		starterSeedReconciliationFilter(false, true),
		bson.M{
			"$set": bson.M{"costs.$[cost].trial_period_days": 14},
			"$unset": bson.M{
				"metadata.seed_reconciliations." + providerNeutralStarterCostsReconciliation: "",
			},
		},
		options.UpdateOne().SetArrayFilters([]any{
			bson.M{"cost._id": bson.M{"$in": []string{seedPlanStarterCostMonthID, seedPlanStarterCostYearID}}},
		}),
	)
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 || result.ModifiedCount != 1 {
		return fmt.Errorf("%w: starter seed changed during rollback", ErrPricingSeedConflict)
	}
	return nil
}

func loadStarterSeedProjection(db *mongo.Database) (*starterSeedProjection, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: database is nil", ErrPricingSeedConflict)
	}
	var plan starterSeedProjection
	err := db.Collection(pricer.PricePlansCollection).
		FindOne(context.Background(), bson.M{"_id": seedPlanStarterID}).
		Decode(&plan)
	return &plan, err
}

func classifyStarterSeedProjection(plan *starterSeedProjection) (legacy bool, target bool, err error) {
	if plan.ID != seedPlanStarterID || plan.Slug != "starter" || plan.CreatedByID != "seed-migration" || len(plan.Costs) != 2 {
		return false, false, fmt.Errorf("%w: starter seed ownership or shape differs", ErrPricingSeedConflict)
	}

	month, monthFound := starterSeedCostByID(plan.Costs, seedPlanStarterCostMonthID)
	year, yearFound := starterSeedCostByID(plan.Costs, seedPlanStarterCostYearID)
	if !monthFound || !yearFound ||
		!starterSeedCostMatches(month, pricer.PriceBillingCadenceMonthly, "starter_monthly_free") ||
		!starterSeedCostMatches(year, pricer.PriceBillingCadenceYearly, "starter_yearly_free") {
		return false, false, fmt.Errorf("%w: starter costs differ from the owned seed", ErrPricingSeedConflict)
	}

	legacy = month.TrialPeriodDays == 14 && year.TrialPeriodDays == 14
	target = month.TrialPeriodDays == 0 && year.TrialPeriodDays == 0
	if !legacy && !target {
		return false, false, fmt.Errorf("%w: starter trial state is mixed", ErrPricingSeedConflict)
	}
	return legacy, target, nil
}

func starterSeedCostByID(costs []pricer.PriceCost, id string) (pricer.PriceCost, bool) {
	for _, cost := range costs {
		if cost.ID == id {
			return cost, true
		}
	}
	return pricer.PriceCost{}, false
}

func starterSeedCostMatches(cost pricer.PriceCost, cadence pricer.PriceBillingCadence, priceID string) bool {
	return cost.Amount == 0 &&
		cost.Currency == "USD" &&
		cost.BillingCadence == cadence &&
		cost.SetupFeeAmount == 0 &&
		len(cost.ProviderRefs) == 1 &&
		cost.ProviderRefs[0].Provider == pricer.PriceProviderManual &&
		cost.ProviderRefs[0].ProviderPriceID == priceID &&
		cost.ProviderRefs[0].ProviderID == "" &&
		cost.ProviderRefs[0].ProviderProductID == ""
}

func starterSeedReconciliationFilter(legacy, migrated bool) bson.M {
	var trialDays interface{} = bson.M{"$in": []interface{}{0, nil}}
	if legacy {
		trialDays = 14
	}
	var migrationMarker interface{} = bson.M{"$ne": true}
	if migrated {
		migrationMarker = true
	}
	return bson.M{
		"_id":           seedPlanStarterID,
		"slug":          "starter",
		"created_by_id": "seed-migration",
		"costs": bson.M{
			"$size": 2,
			"$all": []bson.M{
				{"$elemMatch": starterSeedCostReconciliationFilter(seedPlanStarterCostMonthID, pricer.PriceBillingCadenceMonthly, "starter_monthly_free", trialDays)},
				{"$elemMatch": starterSeedCostReconciliationFilter(seedPlanStarterCostYearID, pricer.PriceBillingCadenceYearly, "starter_yearly_free", trialDays)},
			},
		},
		"metadata.seed_reconciliations." + providerNeutralStarterCostsReconciliation: migrationMarker,
	}
}

func starterSeedCostReconciliationFilter(
	id string,
	cadence pricer.PriceBillingCadence,
	priceID string,
	trialDays interface{},
) bson.M {
	return bson.M{
		"_id":               id,
		"amount":            0,
		"currency":          "USD",
		"billing_cadence":   cadence,
		"trial_period_days": trialDays,
		"setup_fee_amount":  bson.M{"$in": []interface{}{0, nil}},
		"provider_refs": bson.M{
			"$size": 1,
			"$elemMatch": bson.M{
				"provider":    pricer.PriceProviderManual,
				"provider_id": bson.M{"$in": []interface{}{"", nil}},
				"provider_product_id": bson.M{
					"$in": []interface{}{"", nil},
				},
				"provider_price_id": priceID,
			},
		},
	}
}
