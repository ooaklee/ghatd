package migrations

import (
	"context"
	"log"

	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// TestPlansSeedCreatedByID identifies all documents inserted by the test plans
// seed migration so they can be precisely rolled back without affecting other
// seed data.
const TestPlansSeedCreatedByID = "test-plans-seed-migration"

// Deterministic identifiers for the Fireflies-style E2E seed. Kept as exported
// vars so integration tests and golden fixtures can reference them by name.
var (
	// Feature catalog identifiers.
	TestSeedFeatureTranscriptionID   = "30000000-0000-4000-8000-000000000001"
	TestSeedFeatureStorageID         = "30000000-0000-4000-8000-000000000002"
	TestSeedFeatureTeamSeatsID       = "30000000-0000-4000-8000-000000000003"
	TestSeedFeatureIntegrationsID    = "30000000-0000-4000-8000-000000000004"
	TestSeedFeatureApiAccessID       = "30000000-0000-4000-8000-000000000005"
	TestSeedFeaturePrioritySupportID = "30000000-0000-4000-8000-000000000006"
	TestSeedFeatureCustomBrandingID  = "30000000-0000-4000-8000-000000000007"
	TestSeedFeatureSsoID             = "30000000-0000-4000-8000-000000000008"

	// Plan identifiers.
	TestSeedPlanFreeID       = "10000000-0000-4000-8000-000000000001"
	TestSeedPlanProID        = "10000000-0000-4000-8000-000000000002"
	TestSeedPlanEnterpriseID = "10000000-0000-4000-8000-000000000003"

	// Cost identifiers.
	TestSeedCostFreeMonthID       = "20000000-0000-4000-8000-000000000001"
	TestSeedCostProMonthID        = "20000000-0000-4000-8000-000000000002"
	TestSeedCostProYearID         = "20000000-0000-4000-8000-000000000003"
	TestSeedCostEnterpriseMonthID = "20000000-0000-4000-8000-000000000004"
	TestSeedCostEnterpriseYearID  = "20000000-0000-4000-8000-000000000005"
)

// boolFeatureRef builds a PlanFeatureRef document for boolean catalog features
// that are simply included or excluded from a plan.
func boolFeatureRef(featureID, slug, label string, included bool) bson.M {
	return bson.M{
		"feature_id":   featureID,
		"feature_slug": slug,
		"label":        label,
		"included":     included,
	}
}

// InitTestPlansSeedUp inserts a Fireflies-style three-tier pricing catalog
// (Free / Pro / Enterprise) used to drive E2E pricing card comparisons. Legacy
// UI fields (type, is_popular, conditional, more, is_new, link, images,
// priority, per_seat, disabled, discount_percentage) are persisted in the
// metadata maps so the existing model can serve them without schema expansion.
func InitTestPlansSeedUp(db *mongo.Database) error { //Up

	log.SetFlags(0)

	now := toolbox.TimeNowUTC()

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-test-plans-seed"))

	features := []interface{}{
		bson.M{
			"_id":           TestSeedFeatureTranscriptionID,
			"_nano_id":      "tF1transcription0001",
			"slug":          "transcription-credits",
			"name":          "Transcription credits",
			"description":   "Monthly meeting transcription minutes",
			"type":          pricer.PriceFeatureTypeQuantity,
			"unit":          pricer.PriceFeatureUnitRequest,
			"sort_order":    1,
			"created_at":    now,
			"created_by_id": TestPlansSeedCreatedByID,
			"metadata": bson.M{
				"category": "core",
				"ui":       bson.M{"icon": "microphone"},
			},
		},
		bson.M{
			"_id":           TestSeedFeatureStorageID,
			"_nano_id":      "tF2storage000000002",
			"slug":          "storage-limit",
			"name":          "Storage",
			"description":   "Recording and transcript storage",
			"type":          pricer.PriceFeatureTypeQuantity,
			"unit":          pricer.PriceFeatureUnitGB,
			"sort_order":    2,
			"created_at":    now,
			"created_by_id": TestPlansSeedCreatedByID,
			"metadata":      bson.M{"category": "infrastructure"},
		},
		bson.M{
			"_id":           TestSeedFeatureTeamSeatsID,
			"_nano_id":      "tF3seats00000000003",
			"slug":          "team-seats",
			"name":          "Team seats",
			"description":   "Number of users that can collaborate on the workspace",
			"type":          pricer.PriceFeatureTypeQuantity,
			"unit":          pricer.PriceFeatureUnitSeat,
			"sort_order":    3,
			"created_at":    now,
			"created_by_id": TestPlansSeedCreatedByID,
			"metadata":      bson.M{"category": "collaboration"},
		},
		bson.M{
			"_id":           TestSeedFeatureIntegrationsID,
			"_nano_id":      "tF4integrations0004",
			"slug":          "integrations",
			"name":          "Integrations",
			"description":   "Connect to Slack, Notion, Zapier and more",
			"type":          pricer.PriceFeatureTypeBoolean,
			"sort_order":    4,
			"created_at":    now,
			"created_by_id": TestPlansSeedCreatedByID,
			"metadata": bson.M{
				"category": "integration",
				"ui":       bson.M{"is_new": true},
			},
		},
		bson.M{
			"_id":           TestSeedFeatureApiAccessID,
			"_nano_id":      "tF5apiaccess0000005",
			"slug":          "developer-api-access",
			"name":          "API access",
			"description":   "Programmatic access to the platform API",
			"type":          pricer.PriceFeatureTypeBoolean,
			"sort_order":    5,
			"created_at":    now,
			"created_by_id": TestPlansSeedCreatedByID,
			"metadata":      bson.M{"category": "developer"},
		},
		bson.M{
			"_id":           TestSeedFeaturePrioritySupportID,
			"_nano_id":      "tF6support000000006",
			"slug":          "priority-support-tier",
			"name":          "Priority support",
			"description":   "Dedicated email and chat support with faster SLAs",
			"type":          pricer.PriceFeatureTypeBoolean,
			"sort_order":    6,
			"created_at":    now,
			"created_by_id": TestPlansSeedCreatedByID,
			"metadata":      bson.M{"category": "support"},
		},
		bson.M{
			"_id":           TestSeedFeatureCustomBrandingID,
			"_nano_id":      "tF7branding00000007",
			"slug":          "custom-branding",
			"name":          "Custom branding",
			"description":   "White-labelled meeting summaries and shared pages",
			"type":          pricer.PriceFeatureTypeBoolean,
			"sort_order":    7,
			"created_at":    now,
			"created_by_id": TestPlansSeedCreatedByID,
			"metadata":      bson.M{"category": "branding"},
		},
		bson.M{
			"_id":           TestSeedFeatureSsoID,
			"_nano_id":      "tF8sso0000000000008",
			"slug":          "sso",
			"name":          "SSO & SCIM",
			"description":   "Single sign-on and SCIM provisioning",
			"type":          pricer.PriceFeatureTypeBoolean,
			"sort_order":    8,
			"created_at":    now,
			"created_by_id": TestPlansSeedCreatedByID,
			"metadata":      bson.M{"category": "security"},
		},
	}

	if _, err := db.Collection(pricer.PriceFeaturesCollection).InsertMany(context.Background(), features); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-test-plans-seed-features"))
		return err
	}

	freePlan := bson.M{
		"_id":         TestSeedPlanFreeID,
		"_nano_id":    "tP1free00000000001",
		"slug":        "free",
		"name":        "Free",
		"description": "Get started with core meeting transcription for individuals.",
		"status":      pricer.PricePlanStatusPublished,
		"features": []bson.M{
			{
				"feature_id":   TestSeedFeatureTranscriptionID,
				"feature_slug": "transcription-credits",
				"label":        "800 minutes per month",
				"included":     true,
				"quantity":     800,
				"unit":         pricer.PriceFeatureUnitRequest,
			},
			{
				"feature_id":   TestSeedFeatureStorageID,
				"feature_slug": "storage-limit",
				"label":        "5 GB storage",
				"included":     true,
				"quantity":     5,
				"unit":         pricer.PriceFeatureUnitGB,
			},
			{
				"feature_id":   TestSeedFeatureTeamSeatsID,
				"feature_slug": "team-seats",
				"label":        "1 seat",
				"included":     true,
				"quantity":     1,
				"unit":         pricer.PriceFeatureUnitSeat,
			},
			boolFeatureRef(TestSeedFeatureIntegrationsID, "integrations", "Integrations", false),
			boolFeatureRef(TestSeedFeatureApiAccessID, "developer-api-access", "API access", false),
			boolFeatureRef(TestSeedFeaturePrioritySupportID, "priority-support-tier", "Priority support", false),
			boolFeatureRef(TestSeedFeatureCustomBrandingID, "custom-branding", "Custom branding", false),
			boolFeatureRef(TestSeedFeatureSsoID, "sso", "SSO & SCIM", false),
		},
		"costs": []bson.M{
			{
				"_id":             TestSeedCostFreeMonthID,
				"amount":          0,
				"currency":        "USD",
				"billing_cadence": pricer.PriceBillingCadenceMonthly,
				"provider_refs": []bson.M{
					{
						"provider":          pricer.PriceProviderManual,
						"provider_price_id": "free_monthly",
					},
				},
				"metadata": bson.M{
					"pricing_flags": bson.M{"per_seat": false, "disabled": false},
				},
			},
		},
		"provider_refs": []bson.M{
			{"provider": pricer.PriceProviderManual, "provider_product_id": "free"},
		},
		"metadata": bson.M{
			"ui": bson.M{
				"type":          "PERSONAL",
				"is_popular":    false,
				"priority":      1,
				"display_order": 1,
				"cta_label":     "Get started",
			},
		},
		"published_at":    now,
		"published_by_id": TestPlansSeedCreatedByID,
		"created_at":      now,
		"created_by_id":   TestPlansSeedCreatedByID,
	}

	proPlan := bson.M{
		"_id":         TestSeedPlanProID,
		"_nano_id":    "tP2pro000000000002",
		"slug":        "pro",
		"name":        "Pro",
		"description": "For growing teams that need unlimited transcription and integrations.",
		"status":      pricer.PricePlanStatusPublished,
		"features": []bson.M{
			{
				"feature_id":   TestSeedFeatureTranscriptionID,
				"feature_slug": "transcription-credits",
				"label":        "8,000 minutes per month",
				"included":     true,
				"quantity":     8000,
				"unit":         pricer.PriceFeatureUnitRequest,
			},
			{
				"feature_id":   TestSeedFeatureStorageID,
				"feature_slug": "storage-limit",
				"label":        "50 GB storage",
				"included":     true,
				"quantity":     50,
				"unit":         pricer.PriceFeatureUnitGB,
			},
			{
				"feature_id":   TestSeedFeatureTeamSeatsID,
				"feature_slug": "team-seats",
				"label":        "Up to 10 seats",
				"included":     true,
				"quantity":     10,
				"unit":         pricer.PriceFeatureUnitSeat,
			},
			boolFeatureRef(TestSeedFeatureIntegrationsID, "integrations", "Integrations", true),
			boolFeatureRef(TestSeedFeatureApiAccessID, "developer-api-access", "API access", true),
			boolFeatureRef(TestSeedFeaturePrioritySupportID, "priority-support-tier", "Priority support", true),
			boolFeatureRef(TestSeedFeatureCustomBrandingID, "custom-branding", "Custom branding", false),
			boolFeatureRef(TestSeedFeatureSsoID, "sso", "SSO & SCIM", false),
		},
		"costs": []bson.M{
			{
				"_id":               TestSeedCostProMonthID,
				"amount":            1800,
				"currency":          "USD",
				"billing_cadence":   pricer.PriceBillingCadenceMonthly,
				"trial_period_days": 14,
				"provider_refs": []bson.M{
					{
						"provider":          pricer.PriceProviderStripe,
						"provider_price_id": "price_test_pro_monthly",
					},
				},
				"metadata": bson.M{
					"pricing_flags": bson.M{"per_seat": true},
				},
			},
			{
				"_id":               TestSeedCostProYearID,
				"amount":            12000,
				"currency":          "USD",
				"billing_cadence":   pricer.PriceBillingCadenceYearly,
				"trial_period_days": 14,
				"provider_refs": []bson.M{
					{
						"provider":          pricer.PriceProviderStripe,
						"provider_price_id": "price_test_pro_yearly",
					},
				},
				"metadata": bson.M{
					"pricing_flags": bson.M{"per_seat": true, "discount_percentage": 44},
				},
			},
		},
		"provider_refs": []bson.M{
			{"provider": pricer.PriceProviderStripe, "provider_product_id": "prod_test_pro"},
		},
		"metadata": bson.M{
			"ui": bson.M{
				"type":          "TEAM",
				"is_popular":    true,
				"priority":      2,
				"display_order": 2,
				"cta_label":     "Start free trial",
			},
		},
		"published_at":    now,
		"published_by_id": TestPlansSeedCreatedByID,
		"created_at":      now,
		"created_by_id":   TestPlansSeedCreatedByID,
	}

	enterprisePlan := bson.M{
		"_id":         TestSeedPlanEnterpriseID,
		"_nano_id":    "tP3enterprise00003",
		"slug":        "enterprise",
		"name":        "Enterprise",
		"description": "Advanced security, branding and dedicated support for organisations.",
		"status":      pricer.PricePlanStatusPublished,
		"features": []bson.M{
			{
				"feature_id":   TestSeedFeatureTranscriptionID,
				"feature_slug": "transcription-credits",
				"label":        "Unlimited transcription",
				"included":     true,
				"quantity":     0,
				"unit":         pricer.PriceFeatureUnitRequest,
				"metadata":     bson.M{"ui": bson.M{"display": "Unlimited"}},
			},
			{
				"feature_id":   TestSeedFeatureStorageID,
				"feature_slug": "storage-limit",
				"label":        "Unlimited storage",
				"included":     true,
				"quantity":     0,
				"unit":         pricer.PriceFeatureUnitGB,
				"metadata":     bson.M{"ui": bson.M{"display": "Unlimited"}},
			},
			{
				"feature_id":   TestSeedFeatureTeamSeatsID,
				"feature_slug": "team-seats",
				"label":        "Unlimited seats",
				"included":     true,
				"quantity":     0,
				"unit":         pricer.PriceFeatureUnitSeat,
				"metadata":     bson.M{"ui": bson.M{"display": "Unlimited"}},
			},
			boolFeatureRef(TestSeedFeatureIntegrationsID, "integrations", "Integrations", true),
			boolFeatureRef(TestSeedFeatureApiAccessID, "developer-api-access", "API access", true),
			boolFeatureRef(TestSeedFeaturePrioritySupportID, "priority-support-tier", "Priority support", true),
			boolFeatureRef(TestSeedFeatureCustomBrandingID, "custom-branding", "Custom branding", true),
			boolFeatureRef(TestSeedFeatureSsoID, "sso", "SSO & SCIM", true),
		},
		"costs": []bson.M{
			{
				"_id":               TestSeedCostEnterpriseMonthID,
				"amount":            3900,
				"currency":          "USD",
				"billing_cadence":   pricer.PriceBillingCadenceMonthly,
				"trial_period_days": 30,
				"provider_refs": []bson.M{
					{
						"provider":          pricer.PriceProviderStripe,
						"provider_price_id": "price_test_enterprise_monthly",
					},
				},
				"metadata": bson.M{
					"pricing_flags": bson.M{"per_seat": true},
				},
			},
			{
				"_id":               TestSeedCostEnterpriseYearID,
				"amount":            39000,
				"currency":          "USD",
				"billing_cadence":   pricer.PriceBillingCadenceYearly,
				"trial_period_days": 30,
				"provider_refs": []bson.M{
					{
						"provider":          pricer.PriceProviderStripe,
						"provider_price_id": "price_test_enterprise_yearly",
					},
				},
				"metadata": bson.M{
					"pricing_flags": bson.M{"per_seat": true, "discount_percentage": 17},
				},
			},
		},
		"provider_refs": []bson.M{
			{"provider": pricer.PriceProviderStripe, "provider_product_id": "prod_test_enterprise"},
			{
				"provider":            pricer.PriceProviderKofi,
				"provider_product_id": "kofi_enterprise_sponsor",
				"metadata":            bson.M{"link": "https://ko-fi.com/example/enterprise"},
			},
		},
		"metadata": bson.M{
			"ui": bson.M{
				"type":          "ENTERPRISE",
				"is_popular":    false,
				"priority":      3,
				"display_order": 3,
				"cta_label":     "Contact sales",
			},
		},
		"published_at":    now,
		"published_by_id": TestPlansSeedCreatedByID,
		"created_at":      now,
		"created_by_id":   TestPlansSeedCreatedByID,
	}

	if _, err := db.Collection(pricer.PricePlansCollection).InsertMany(
		context.Background(),
		[]interface{}{freePlan, proPlan, enterprisePlan},
	); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-test-plans-seed-plans"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-test-plans-seed"))
	return nil
}

// InitTestPlansSeedDown removes the documents created by InitTestPlansSeedUp.
func InitTestPlansSeedDown(db *mongo.Database) error { //Down
	log.SetFlags(0)

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-test-plans-seed"))

	if _, err := db.Collection(pricer.PriceFeaturesCollection).DeleteMany(
		context.Background(),
		bson.M{"created_by_id": TestPlansSeedCreatedByID},
	); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-test-plans-seed-features"))
		return err
	}

	if _, err := db.Collection(pricer.PricePlansCollection).DeleteMany(
		context.Background(),
		bson.M{"created_by_id": TestPlansSeedCreatedByID},
	); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-test-plans-seed-plans"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-test-plans-seed"))
	return nil
}
