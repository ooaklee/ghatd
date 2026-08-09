package migrations

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// TestPlansSeedCreatedByID identifies all documents inserted by the test plans
// seed migration so they can be precisely rolled back without affecting other
// seed data.
const TestPlansSeedCreatedByID = "test-plans-seed-migration"

// TestStripePlansSeedCreatedByID identifies the provider-specific checkout
// fixtures separately from the comparison-card seed. Keeping a distinct owner
// lets the forward reconciliation and rollback touch only these test plans.
const TestStripePlansSeedCreatedByID = "test-stripe-plans-seed-migration-v1"

// ErrTestPlansSeedConflict indicates that deterministic test identifiers are
// already present or that a partial seed could not be applied safely.
var ErrTestPlansSeedConflict = errors.New("test pricing seed conflict")

// Deterministic identifiers for the tiered E2E seed. Kept as exported vars so
// integration tests and golden fixtures can reference them by name.
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
	TestSeedPlanFreeID            = "10000000-0000-4000-8000-000000000001"
	TestSeedPlanProID             = "10000000-0000-4000-8000-000000000002"
	TestSeedPlanEnterpriseID      = "10000000-0000-4000-8000-000000000003"
	TestSeedPlanStripeOneTimeID   = "10000000-0000-4000-8000-000000000004"
	TestSeedPlanStripeRecurringID = "10000000-0000-4000-8000-000000000005"
	TestSeedPlanStripeTrialID     = "10000000-0000-4000-8000-000000000006"

	// Cost identifiers.
	TestSeedCostFreeMonthID            = "20000000-0000-4000-8000-000000000001"
	TestSeedCostProMonthID             = "20000000-0000-4000-8000-000000000002"
	TestSeedCostProYearID              = "20000000-0000-4000-8000-000000000003"
	TestSeedCostEnterpriseMonthID      = "20000000-0000-4000-8000-000000000004"
	TestSeedCostEnterpriseYearID       = "20000000-0000-4000-8000-000000000005"
	TestSeedCostStripeOneTimeID        = "20000000-0000-4000-8000-000000000006"
	TestSeedCostStripeRecurringMonthID = "20000000-0000-4000-8000-000000000007"
	TestSeedCostStripeRecurringYearID  = "20000000-0000-4000-8000-000000000008"
	TestSeedCostStripeRecurringWeekID  = "20000000-0000-4000-8000-000000000009"
	TestSeedCostStripeTrialMonthID     = "20000000-0000-4000-8000-000000000010"
	TestSeedCostStripeTrialYearID      = "20000000-0000-4000-8000-000000000011"
	TestSeedCostStripeTrialWeekID      = "20000000-0000-4000-8000-000000000012"

	TestSeedStripeOneTimeProductID   = "prod_test_stripe_one_time"
	TestSeedStripeRecurringProductID = "prod_test_stripe_recurring"
	TestSeedStripeTrialProductID     = "prod_test_stripe_recurring_trial"

	TestSeedStripeOneTimePriceID        = "price_test_stripe_one_time"
	TestSeedStripeRecurringMonthPriceID = "price_test_stripe_recurring_monthly"
	TestSeedStripeRecurringYearPriceID  = "price_test_stripe_recurring_yearly"
	TestSeedStripeRecurringWeekPriceID  = "price_test_stripe_recurring_weekly"
	TestSeedStripeTrialMonthPriceID     = "price_test_stripe_trial_monthly"
	TestSeedStripeTrialYearPriceID      = "price_test_stripe_trial_yearly"
	TestSeedStripeTrialWeekPriceID      = "price_test_stripe_trial_weekly"
)

type testStripeCostSeed struct {
	ID              string
	ProviderPriceID string
	Amount          int64
	BillingCadence  pricer.PriceBillingCadence
	TrialPeriodDays int
}

type testStripePlanSeed struct {
	ID                string
	NanoID            string
	Slug              string
	Name              string
	Description       string
	ProviderProductID string
	CTALabel          string
	DisplayOrder      int
	Costs             []testStripeCostSeed
}

var testStripePlanSeeds = []testStripePlanSeed{
	{
		ID:                TestSeedPlanStripeOneTimeID,
		NanoID:            "tP4stripeOneTime004",
		Slug:              "stripe-one-time-test",
		Name:              "Stripe One-Time Test",
		Description:       "Test-only catalogue fixture for a one-time Stripe Checkout payment.",
		ProviderProductID: TestSeedStripeOneTimeProductID,
		CTALabel:          "Test one-time checkout",
		DisplayOrder:      4,
		Costs: []testStripeCostSeed{
			{
				ID:              TestSeedCostStripeOneTimeID,
				ProviderPriceID: TestSeedStripeOneTimePriceID,
				Amount:          4900,
				BillingCadence:  pricer.PriceBillingCadenceOneTime,
			},
		},
	},
	{
		ID:                TestSeedPlanStripeRecurringID,
		NanoID:            "tP5stripeRecurring05",
		Slug:              "stripe-recurring-test",
		Name:              "Stripe Recurring Test",
		Description:       "Test-only catalogue fixture for recurring Stripe Checkout without a trial.",
		ProviderProductID: TestSeedStripeRecurringProductID,
		CTALabel:          "Test recurring checkout",
		DisplayOrder:      5,
		Costs: []testStripeCostSeed{
			{
				ID:              TestSeedCostStripeRecurringMonthID,
				ProviderPriceID: TestSeedStripeRecurringMonthPriceID,
				Amount:          1800,
				BillingCadence:  pricer.PriceBillingCadenceMonthly,
			},
			{
				ID:              TestSeedCostStripeRecurringYearID,
				ProviderPriceID: TestSeedStripeRecurringYearPriceID,
				Amount:          18000,
				BillingCadence:  pricer.PriceBillingCadenceYearly,
			},
			{
				ID:              TestSeedCostStripeRecurringWeekID,
				ProviderPriceID: TestSeedStripeRecurringWeekPriceID,
				Amount:          500,
				BillingCadence:  pricer.PriceBillingCadenceWeekly,
			},
		},
	},
	{
		ID:                TestSeedPlanStripeTrialID,
		NanoID:            "tP6stripeTrial000006",
		Slug:              "stripe-recurring-trial-test",
		Name:              "Stripe Recurring Trial Test",
		Description:       "Test-only catalogue fixture for recurring Stripe Checkout with a catalogue-owned trial.",
		ProviderProductID: TestSeedStripeTrialProductID,
		CTALabel:          "Test trial checkout",
		DisplayOrder:      6,
		Costs: []testStripeCostSeed{
			{
				ID:              TestSeedCostStripeTrialMonthID,
				ProviderPriceID: TestSeedStripeTrialMonthPriceID,
				Amount:          1800,
				BillingCadence:  pricer.PriceBillingCadenceMonthly,
				TrialPeriodDays: 14,
			},
			{
				ID:              TestSeedCostStripeTrialYearID,
				ProviderPriceID: TestSeedStripeTrialYearPriceID,
				Amount:          18000,
				BillingCadence:  pricer.PriceBillingCadenceYearly,
				TrialPeriodDays: 30,
			},
			{
				ID:              TestSeedCostStripeTrialWeekID,
				ProviderPriceID: TestSeedStripeTrialWeekPriceID,
				Amount:          500,
				BillingCadence:  pricer.PriceBillingCadenceWeekly,
				TrialPeriodDays: 7,
			},
		},
	},
}

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

func testStripeFixturePlans(now string) []interface{} {
	plans := make([]interface{}, 0, len(testStripePlanSeeds))
	for _, seed := range testStripePlanSeeds {
		plans = append(plans, testStripeFixturePlan(seed, now))
	}
	return plans
}

func testStripeFixturePlan(seed testStripePlanSeed, now string) bson.M {
	costs := make([]bson.M, 0, len(seed.Costs))
	for _, costSeed := range seed.Costs {
		cost := bson.M{
			"_id":             costSeed.ID,
			"amount":          costSeed.Amount,
			"currency":        "USD",
			"billing_cadence": costSeed.BillingCadence,
			"provider_refs": []bson.M{
				{
					"provider":          pricer.PriceProviderStripe,
					"provider_price_id": costSeed.ProviderPriceID,
				},
			},
			"metadata": bson.M{
				"test_fixture": bson.M{
					"stripe":        true,
					"trial_variant": costSeed.TrialPeriodDays > 0,
				},
			},
		}
		if costSeed.TrialPeriodDays > 0 {
			cost["trial_period_days"] = costSeed.TrialPeriodDays
		}
		costs = append(costs, cost)
	}

	return bson.M{
		"_id":         seed.ID,
		"_nano_id":    seed.NanoID,
		"slug":        seed.Slug,
		"name":        seed.Name,
		"description": seed.Description,
		"status":      pricer.PricePlanStatusPublished,
		"features": []bson.M{
			{
				"feature_id":   TestSeedFeatureTranscriptionID,
				"feature_slug": "transcription-credits",
				"label":        "10,000 minutes per month",
				"included":     true,
				"quantity":     10000,
				"unit":         pricer.PriceFeatureUnitRequest,
			},
			{
				"feature_id":   TestSeedFeatureStorageID,
				"feature_slug": "storage-limit",
				"label":        "100 GB storage",
				"included":     true,
				"quantity":     100,
				"unit":         pricer.PriceFeatureUnitGB,
			},
			boolFeatureRef(TestSeedFeatureIntegrationsID, "integrations", "Integrations", true),
			boolFeatureRef(TestSeedFeatureApiAccessID, "developer-api-access", "API access", true),
		},
		"costs": costs,
		"provider_refs": []bson.M{
			{
				"provider":            pricer.PriceProviderStripe,
				"provider_product_id": seed.ProviderProductID,
			},
		},
		"metadata": bson.M{
			"ui": bson.M{
				"type":       "TEST",
				"is_popular": false,
				"cta_label":  seed.CTALabel,
			},
			"test_fixture": bson.M{
				"stripe":                true,
				"replace_provider_refs": true,
			},
		},
		"display_order":   seed.DisplayOrder,
		"published_at":    now,
		"published_by_id": TestStripePlansSeedCreatedByID,
		"created_at":      now,
		"created_by_id":   TestStripePlansSeedCreatedByID,
	}
}

// InitTestPlansSeedUp inserts the comparison catalogue plus explicit Stripe
// checkout fixtures. It is intended for isolated E2E tests that register a
// fake checkout provider; the provider identifiers are non-live placeholders.
func InitTestPlansSeedUp(db *mongo.Database) error { //Up
	if db == nil {
		return fmt.Errorf("%w: database is nil", ErrTestPlansSeedConflict)
	}

	log.SetFlags(0)

	now := toolbox.TimeNowUTC()

	log.Default().Println(toolbox.OutputBasicLogString("info", "starting-task-to-test-plans-seed"))
	if err := preflightTestPlansSeed(context.Background(), db); err != nil {
		return err
	}

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
		return errors.Join(err, cleanupPartialTestPlansSeed(context.Background(), db))
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
				"type":       "PERSONAL",
				"is_popular": false,
				"cta_label":  "Get started",
			},
		},
		"display_order":   1,
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
				"type":       "TEAM",
				"is_popular": true,
				"cta_label":  "Start free trial",
			},
		},
		"display_order":   2,
		"priority":        2,
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
				"type":       "ENTERPRISE",
				"is_popular": false,
				"cta_label":  "Contact sales",
			},
		},
		"display_order":   3,
		"priority":        3,
		"published_at":    now,
		"published_by_id": TestPlansSeedCreatedByID,
		"created_at":      now,
		"created_by_id":   TestPlansSeedCreatedByID,
	}

	plans := []interface{}{freePlan, proPlan, enterprisePlan}
	plans = append(plans, testStripeFixturePlans(now)...)
	if _, err := db.Collection(pricer.PricePlansCollection).InsertMany(
		context.Background(),
		plans,
	); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-task-to-test-plans-seed-plans"))
		return errors.Join(err, cleanupPartialTestPlansSeed(context.Background(), db))
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-task-to-test-plans-seed"))
	return nil
}

func preflightTestPlansSeed(ctx context.Context, db *mongo.Database) error {
	featureIDs := []string{
		TestSeedFeatureTranscriptionID,
		TestSeedFeatureStorageID,
		TestSeedFeatureTeamSeatsID,
		TestSeedFeatureIntegrationsID,
		TestSeedFeatureApiAccessID,
		TestSeedFeaturePrioritySupportID,
		TestSeedFeatureCustomBrandingID,
		TestSeedFeatureSsoID,
	}
	featureSlugs := []string{
		"transcription-credits",
		"storage-limit",
		"team-seats",
		"integrations",
		"developer-api-access",
		"priority-support-tier",
		"custom-branding",
		"sso",
	}
	featureCount, err := db.Collection(pricer.PriceFeaturesCollection).CountDocuments(ctx, bson.M{
		"$or": []bson.M{
			{"_id": bson.M{"$in": featureIDs}},
			{"slug": bson.M{"$in": featureSlugs}},
		},
	})
	if err != nil {
		return err
	}
	if featureCount != 0 {
		return fmt.Errorf("%w: feature identifier already exists", ErrTestPlansSeedConflict)
	}

	planIDs := []string{
		TestSeedPlanFreeID,
		TestSeedPlanProID,
		TestSeedPlanEnterpriseID,
		TestSeedPlanStripeOneTimeID,
		TestSeedPlanStripeRecurringID,
		TestSeedPlanStripeTrialID,
	}
	planSlugs := []string{
		"free",
		"pro",
		"enterprise",
		"stripe-one-time-test",
		"stripe-recurring-test",
		"stripe-recurring-trial-test",
	}
	costIDs := []string{
		TestSeedCostFreeMonthID,
		TestSeedCostProMonthID,
		TestSeedCostProYearID,
		TestSeedCostEnterpriseMonthID,
		TestSeedCostEnterpriseYearID,
		TestSeedCostStripeOneTimeID,
		TestSeedCostStripeRecurringMonthID,
		TestSeedCostStripeRecurringYearID,
		TestSeedCostStripeRecurringWeekID,
		TestSeedCostStripeTrialMonthID,
		TestSeedCostStripeTrialYearID,
		TestSeedCostStripeTrialWeekID,
	}
	priceIDs := []string{
		"free_monthly",
		"price_test_pro_monthly",
		"price_test_pro_yearly",
		"price_test_enterprise_monthly",
		"price_test_enterprise_yearly",
		TestSeedStripeOneTimePriceID,
		TestSeedStripeRecurringMonthPriceID,
		TestSeedStripeRecurringYearPriceID,
		TestSeedStripeRecurringWeekPriceID,
		TestSeedStripeTrialMonthPriceID,
		TestSeedStripeTrialYearPriceID,
		TestSeedStripeTrialWeekPriceID,
	}
	planCount, err := db.Collection(pricer.PricePlansCollection).CountDocuments(ctx, bson.M{
		"$or": []bson.M{
			{"_id": bson.M{"$in": planIDs}},
			{"slug": bson.M{"$in": planSlugs}},
			{"costs._id": bson.M{"$in": costIDs}},
			{"costs.provider_refs.provider_price_id": bson.M{"$in": priceIDs}},
		},
	})
	if err != nil {
		return err
	}
	if planCount != 0 {
		return fmt.Errorf("%w: plan, cost, or provider Price identifier already exists", ErrTestPlansSeedConflict)
	}
	return nil
}

func cleanupPartialTestPlansSeed(ctx context.Context, db *mongo.Database) error {
	planIDs := []string{
		TestSeedPlanFreeID,
		TestSeedPlanProID,
		TestSeedPlanEnterpriseID,
		TestSeedPlanStripeOneTimeID,
		TestSeedPlanStripeRecurringID,
		TestSeedPlanStripeTrialID,
	}
	_, planErr := db.Collection(pricer.PricePlansCollection).DeleteMany(ctx, bson.M{
		"_id":           bson.M{"$in": planIDs},
		"created_by_id": bson.M{"$in": []string{TestPlansSeedCreatedByID, TestStripePlansSeedCreatedByID}},
	})
	featureIDs := []string{
		TestSeedFeatureTranscriptionID,
		TestSeedFeatureStorageID,
		TestSeedFeatureTeamSeatsID,
		TestSeedFeatureIntegrationsID,
		TestSeedFeatureApiAccessID,
		TestSeedFeaturePrioritySupportID,
		TestSeedFeatureCustomBrandingID,
		TestSeedFeatureSsoID,
	}
	_, featureErr := db.Collection(pricer.PriceFeaturesCollection).DeleteMany(ctx, bson.M{
		"_id":           bson.M{"$in": featureIDs},
		"created_by_id": TestPlansSeedCreatedByID,
	})
	return errors.Join(planErr, featureErr)
}

// InitTestPlansSeedDown removes the documents created by InitTestPlansSeedUp.
func InitTestPlansSeedDown(db *mongo.Database) error { //Down
	if err := InitTestStripePlansSeedReconcileDown(db); err != nil {
		return err
	}
	log.SetFlags(0)

	log.Default().Println(toolbox.OutputBasicLogString("info", "rolling-back-task-to-test-plans-seed"))

	if _, err := db.Collection(pricer.PricePlansCollection).DeleteMany(
		context.Background(),
		bson.M{"created_by_id": TestPlansSeedCreatedByID},
	); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-test-plans-seed-plans"))
		return err
	}

	if _, err := db.Collection(pricer.PriceFeaturesCollection).DeleteMany(
		context.Background(),
		bson.M{"created_by_id": TestPlansSeedCreatedByID},
	); err != nil {
		log.Default().Println(toolbox.OutputBasicLogString("error", "failed-rolling-back-test-plans-seed-features"))
		return err
	}

	log.Default().Println(toolbox.OutputBasicLogString("info", "completed-rolling-back-task-to-test-plans-seed"))
	return nil
}
