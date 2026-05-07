package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	seedFeatureProjectsID        = "11111111-1111-1111-1111-111111111111"
	seedFeatureStorageID         = "22222222-2222-2222-2222-222222222222"
	seedFeatureTeamMembersID     = "33333333-3333-3333-3333-333333333333"
	seedFeatureApiAccessID       = "44444444-4444-4444-4444-444444444444"
	seedFeaturePrioritySupportID = "55555555-5555-5555-5555-555555555555"

	seedPlanStarterID          = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	seedPlanStarterCostMonthID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	seedPlanStarterCostYearID  = "cccccccc-cccc-cccc-cccc-cccccccccccc"
)

func InitPricingSeedUp(db *mongo.Database) error { //Up

	log.SetFlags(0)

	now := toolbox.TimeNowUTC()

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-pricing-seed"))

	features := []interface{}{
		bson.M{
			"_id":           seedFeatureProjectsID,
			"_nano_id":      "TF1kC2cQzXyvGxkLpfMKb",
			"slug":          "projects",
			"name":          "Projects",
			"description":   "Number of active projects you can create",
			"type":          pricer.PriceFeatureTypeQuantity,
			"unit":          pricer.PriceFeatureUnitProject,
			"sort_order":    1,
			"created_at":    now,
			"created_by_id": "seed-migration",
			"metadata":      bson.M{"category": "collaboration"},
		},
		bson.M{
			"_id":           seedFeatureStorageID,
			"_nano_id":      "HG3dE4eSaBwIynMqRrtPc",
			"slug":          "storage",
			"name":          "Storage",
			"description":   "File storage space for your projects",
			"type":          pricer.PriceFeatureTypeQuantity,
			"unit":          pricer.PriceFeatureUnitGB,
			"sort_order":    2,
			"created_at":    now,
			"created_by_id": "seed-migration",
			"metadata":      bson.M{"category": "infrastructure"},
		},
		bson.M{
			"_id":           seedFeatureTeamMembersID,
			"_nano_id":      "JI5fF6gUbDxKoPsTtVqA",
			"slug":          "team-members",
			"name":          "Team Members",
			"description":   "Number of team members you can invite",
			"type":          pricer.PriceFeatureTypeQuantity,
			"unit":          pricer.PriceFeatureUnitSeat,
			"sort_order":    3,
			"created_at":    now,
			"created_by_id": "seed-migration",
			"metadata":      bson.M{"category": "collaboration"},
		},
		bson.M{
			"_id":           seedFeatureApiAccessID,
			"_nano_id":      "LM7hH8iWcEzMqOtUuXrB",
			"slug":          "api-access",
			"name":          "API Access",
			"description":   "Access the platform API for custom integrations",
			"type":          pricer.PriceFeatureTypeBoolean,
			"sort_order":    4,
			"created_at":    now,
			"created_by_id": "seed-migration",
			"metadata":      bson.M{"category": "developer"},
		},
		bson.M{
			"_id":           seedFeaturePrioritySupportID,
			"_nano_id":      "NO9jJ0kYeDANrQwVyZsC",
			"slug":          "priority-support",
			"name":          "Priority Support",
			"description":   "Dedicated priority email and chat support",
			"type":          pricer.PriceFeatureTypeBoolean,
			"sort_order":    5,
			"created_at":    now,
			"created_by_id": "seed-migration",
			"metadata":      bson.M{"category": "support"},
		},
	}

	_, err := db.Collection(pricer.PriceFeaturesCollection).InsertMany(context.Background(), features)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-pricing-seed-features"))
		return err
	}

	starterPlan := bson.M{
		"_id":         seedPlanStarterID,
		"_nano_id":    "PQakL1mZfFEOtRxYuA0d",
		"slug":        "starter",
		"name":        "Starter",
		"description": "Perfect for individuals and small teams getting started",
		"status":      pricer.PricePlanStatusPublished,
		"features": []bson.M{
			{
				"feature_id":   seedFeatureProjectsID,
				"feature_slug": "projects",
				"label":        "Projects",
				"included":     true,
				"quantity":     3,
				"unit":         pricer.PriceFeatureUnitProject,
			},
			{
				"feature_id":   seedFeatureStorageID,
				"feature_slug": "storage",
				"label":        "Storage",
				"included":     true,
				"quantity":     5,
				"unit":         pricer.PriceFeatureUnitGB,
			},
			{
				"feature_id":   seedFeatureApiAccessID,
				"feature_slug": "api-access",
				"label":        "API Access",
				"included":     true,
			},
		},
		"costs": []bson.M{
			{
				"_id":               seedPlanStarterCostMonthID,
				"amount":            0,
				"currency":          "USD",
				"billing_cadence":   pricer.PriceBillingCadenceMonthly,
				"trial_period_days": 14,
				"provider_refs": []bson.M{
					{
						"provider":          pricer.PriceProviderManual,
						"provider_price_id": "starter_monthly_free",
					},
				},
			},
			{
				"_id":               seedPlanStarterCostYearID,
				"amount":            0,
				"currency":          "USD",
				"billing_cadence":   pricer.PriceBillingCadenceYearly,
				"trial_period_days": 14,
				"provider_refs": []bson.M{
					{
						"provider":          pricer.PriceProviderManual,
						"provider_price_id": "starter_yearly_free",
					},
				},
			},
		},
		"provider_refs": []bson.M{
			{
				"provider":            pricer.PriceProviderManual,
				"provider_product_id": "starter",
			},
		},
		"display_order":   1,
		"metadata":        bson.M{},
		"published_at":    now,
		"published_by_id": "seed-migration",
		"created_at":      now,
		"created_by_id":   "seed-migration",
	}

	_, err = db.Collection(pricer.PricePlansCollection).InsertOne(context.Background(), starterPlan)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-pricing-seed-plan"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-pricing-seed"))
	return nil

}

func InitPricingSeedDown(db *mongo.Database) error { //Down
	log.SetFlags(0)

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-pricing-seed"))

	_, err := db.Collection(pricer.PriceFeaturesCollection).DeleteMany(
		context.Background(),
		bson.M{"created_by_id": "seed-migration"},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-pricing-seed-features"))
		return err
	}

	_, err = db.Collection(pricer.PricePlansCollection).DeleteMany(
		context.Background(),
		bson.M{"created_by_id": "seed-migration"},
	)
	if err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-pricing-seed-plan"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-pricing-seed"))
	return nil
}
