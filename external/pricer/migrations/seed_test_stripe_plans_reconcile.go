package migrations

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ErrTestStripePlansSeedConflict indicates that the test catalogue is absent,
// partially applied, or was modified after seeding. Reconciliation fails
// closed instead of overwriting operator-managed pricing data.
var ErrTestStripePlansSeedConflict = errors.New("test Stripe pricing seed conflict")

type testStripeStoredPlan struct {
	ID            string                    `bson:"_id"`
	NanoID        string                    `bson:"_nano_id"`
	Slug          string                    `bson:"slug"`
	Name          string                    `bson:"name"`
	Description   string                    `bson:"description,omitempty"`
	Status        pricer.PricePlanStatus    `bson:"status"`
	Features      []pricer.PlanFeatureRef   `bson:"features,omitempty"`
	Costs         []pricer.PriceCost        `bson:"costs,omitempty"`
	Discounts     []pricer.PriceDiscount    `bson:"discounts,omitempty"`
	PaymentTerms  *pricer.PricePaymentTerms `bson:"payment_terms,omitempty"`
	ProviderRefs  []pricer.PriceProviderRef `bson:"provider_refs,omitempty"`
	Metadata      map[string]interface{}    `bson:"metadata,omitempty"`
	DisplayOrder  int                       `bson:"display_order,omitempty"`
	PublishedAt   string                    `bson:"published_at,omitempty"`
	PublishedByID string                    `bson:"published_by_id,omitempty"`
	CreatedByID   string                    `bson:"created_by_id,omitempty"`
	DeletedAt     string                    `bson:"deleted_at,omitempty"`
	DeletedByID   string                    `bson:"deleted_by_id,omitempty"`
}

// InitTestStripePlansSeedReconcileUp adds the provider-specific plans to an
// existing old three-plan test seed. It never creates the base test catalogue;
// callers must gate it to an explicit local/test environment.
func InitTestStripePlansSeedReconcileUp(db *mongo.Database) error { //Up
	if db == nil {
		return fmt.Errorf("%w: database is nil", ErrTestStripePlansSeedConflict)
	}

	ctx := context.Background()
	if err := requireOwnedBaseTestPlans(ctx, db); err != nil {
		return err
	}

	existing, err := findTestStripePlanDocuments(ctx, db)
	if err != nil {
		return err
	}
	if err := preflightTestStripeCostReferences(ctx, db); err != nil {
		return err
	}
	if len(existing) > 0 {
		if len(existing) != len(testStripePlanSeeds) {
			return fmt.Errorf("%w: provider fixtures are partially present", ErrTestStripePlansSeedConflict)
		}
		for _, document := range existing {
			seed, ok := testStripePlanSeedByID(document.ID)
			if !ok || !testStripeStoredPlanMatches(document, seed) {
				return fmt.Errorf("%w: provider fixture %q differs from the seed", ErrTestStripePlansSeedConflict, document.ID)
			}
		}
		return nil
	}

	collection := db.Collection(pricer.PricePlansCollection)
	insertedIDs := make([]string, 0, len(testStripePlanSeeds))
	for _, seed := range testStripePlanSeeds {
		if _, err := collection.InsertOne(ctx, testStripeFixturePlan(seed, toolbox.TimeNowUTC())); err != nil {
			if len(insertedIDs) > 0 {
				_, _ = collection.DeleteMany(ctx, bson.M{
					"_id":           bson.M{"$in": insertedIDs},
					"created_by_id": TestStripePlansSeedCreatedByID,
				})
			}
			return fmt.Errorf("%w: insert provider fixture: %v", ErrTestStripePlansSeedConflict, err)
		}
		insertedIDs = append(insertedIDs, seed.ID)
	}

	return nil
}

// InitTestStripePlansSeedReconcileDown removes only unchanged, owned provider
// fixtures. The base Free/Pro/Enterprise seed is left to InitTestPlansSeedDown.
func InitTestStripePlansSeedReconcileDown(db *mongo.Database) error { //Down
	if db == nil {
		return fmt.Errorf("%w: database is nil", ErrTestStripePlansSeedConflict)
	}

	ctx := context.Background()
	existing, err := findTestStripePlanDocuments(ctx, db)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return nil
	}

	ids := make([]string, 0, len(existing))
	for _, document := range existing {
		seed, ok := testStripePlanSeedByID(document.ID)
		if !ok || !testStripeStoredPlanMatches(document, seed) {
			return fmt.Errorf("%w: refusing to remove modified provider fixture %q", ErrTestStripePlansSeedConflict, document.ID)
		}
		ids = append(ids, document.ID)
	}

	_, err = db.Collection(pricer.PricePlansCollection).DeleteMany(ctx, bson.M{
		"_id":           bson.M{"$in": ids},
		"created_by_id": TestStripePlansSeedCreatedByID,
	})
	return err
}

func requireOwnedBaseTestPlans(ctx context.Context, db *mongo.Database) error {
	expected := map[string]string{
		TestSeedPlanFreeID:       "free",
		TestSeedPlanProID:        "pro",
		TestSeedPlanEnterpriseID: "enterprise",
	}
	collection := db.Collection(pricer.PricePlansCollection)
	for id, slug := range expected {
		var document struct {
			ID          string `bson:"_id"`
			Slug        string `bson:"slug"`
			CreatedByID string `bson:"created_by_id"`
		}
		err := collection.FindOne(ctx, bson.M{"_id": id}).Decode(&document)
		if err != nil || document.ID != id || document.Slug != slug || document.CreatedByID != TestPlansSeedCreatedByID {
			return fmt.Errorf("%w: owned base plan %q is unavailable", ErrTestStripePlansSeedConflict, id)
		}
	}
	return nil
}

func findTestStripePlanDocuments(ctx context.Context, db *mongo.Database) ([]testStripeStoredPlan, error) {
	ids := make([]string, 0, len(testStripePlanSeeds))
	slugs := make([]string, 0, len(testStripePlanSeeds))
	for _, seed := range testStripePlanSeeds {
		ids = append(ids, seed.ID)
		slugs = append(slugs, seed.Slug)
	}

	cursor, err := db.Collection(pricer.PricePlansCollection).Find(ctx, bson.M{
		"$or": []bson.M{
			{"_id": bson.M{"$in": ids}},
			{"slug": bson.M{"$in": slugs}},
		},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var documents []testStripeStoredPlan
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, err
	}
	return documents, nil
}

func preflightTestStripeCostReferences(ctx context.Context, db *mongo.Database) error {
	costIDs := make([]string, 0, 7)
	priceIDs := make([]string, 0, 7)
	planIDs := make([]string, 0, len(testStripePlanSeeds))
	for _, seed := range testStripePlanSeeds {
		planIDs = append(planIDs, seed.ID)
		for _, cost := range seed.Costs {
			costIDs = append(costIDs, cost.ID)
			priceIDs = append(priceIDs, cost.ProviderPriceID)
		}
	}

	count, err := db.Collection(pricer.PricePlansCollection).CountDocuments(ctx, bson.M{
		"_id": bson.M{"$nin": planIDs},
		"$or": []bson.M{
			{"costs._id": bson.M{"$in": costIDs}},
			{"costs.provider_refs.provider_price_id": bson.M{"$in": priceIDs}},
		},
	})
	if err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("%w: cost or provider Price reference already exists", ErrTestStripePlansSeedConflict)
	}
	return nil
}

func testStripePlanSeedByID(id string) (testStripePlanSeed, bool) {
	for _, seed := range testStripePlanSeeds {
		if seed.ID == id {
			return seed, true
		}
	}
	return testStripePlanSeed{}, false
}

func testStripeStoredPlanMatches(document testStripeStoredPlan, seed testStripePlanSeed) bool {
	if !testStripePublicationIsEffective(document.PublishedAt) || strings.TrimSpace(document.DeletedAt) != "" || strings.TrimSpace(document.DeletedByID) != "" {
		return false
	}
	expectedRaw := testStripeFixturePlan(seed, toolbox.TimeNowUTC())
	encoded, err := bson.Marshal(expectedRaw)
	if err != nil {
		return false
	}
	var expected testStripeStoredPlan
	if err := bson.Unmarshal(encoded, &expected); err != nil {
		return false
	}
	normalizeTestStripeStoredPlanMetadata(&document)
	normalizeTestStripeStoredPlanMetadata(&expected)
	// Publication must be present and effective, but its exact timestamp is an
	// audit detail that differs between fresh and reconciled fixtures.
	document.PublishedAt = ""
	expected.PublishedAt = ""
	return reflect.DeepEqual(document, expected)
}

func testStripePublicationIsEffective(publishedAt string) bool {
	publishedAt = strings.TrimSpace(publishedAt)
	if publishedAt == "" {
		return false
	}
	for _, layout := range []string{common.RFC3339NanoUTC, time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, publishedAt)
		if err == nil {
			return !parsed.After(time.Now().UTC())
		}
	}
	return false
}

func normalizeTestStripeStoredPlanMetadata(plan *testStripeStoredPlan) {
	plan.Metadata = normalizeTestStripeMetadataMap(plan.Metadata)
	for index := range plan.Features {
		plan.Features[index].Metadata = normalizeTestStripeMetadataMap(plan.Features[index].Metadata)
	}
	for index := range plan.Costs {
		plan.Costs[index].Metadata = normalizeTestStripeMetadataMap(plan.Costs[index].Metadata)
		for refIndex := range plan.Costs[index].ProviderRefs {
			plan.Costs[index].ProviderRefs[refIndex].Metadata = normalizeTestStripeMetadataMap(
				plan.Costs[index].ProviderRefs[refIndex].Metadata,
			)
		}
	}
	for index := range plan.Discounts {
		plan.Discounts[index].Metadata = normalizeTestStripeMetadataMap(plan.Discounts[index].Metadata)
		for refIndex := range plan.Discounts[index].ProviderRefs {
			plan.Discounts[index].ProviderRefs[refIndex].Metadata = normalizeTestStripeMetadataMap(
				plan.Discounts[index].ProviderRefs[refIndex].Metadata,
			)
		}
	}
	for index := range plan.ProviderRefs {
		plan.ProviderRefs[index].Metadata = normalizeTestStripeMetadataMap(plan.ProviderRefs[index].Metadata)
	}
	if plan.PaymentTerms != nil {
		plan.PaymentTerms.Metadata = normalizeTestStripeMetadataMap(plan.PaymentTerms.Metadata)
	}
}

func normalizeTestStripeMetadataMap(metadata map[string]interface{}) map[string]interface{} {
	if metadata == nil {
		return nil
	}
	normalized := make(map[string]interface{}, len(metadata))
	for key, value := range metadata {
		normalized[key] = normalizeTestStripeMetadataValue(value)
	}
	return normalized
}

func normalizeTestStripeMetadataValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case bson.D:
		normalized := make(map[string]interface{}, len(typed))
		for _, entry := range typed {
			normalized[entry.Key] = normalizeTestStripeMetadataValue(entry.Value)
		}
		return normalized
	case bson.M:
		return normalizeTestStripeMetadataMap(typed)
	case map[string]interface{}:
		return normalizeTestStripeMetadataMap(typed)
	case bson.A:
		normalized := make([]interface{}, len(typed))
		for index, entry := range typed {
			normalized[index] = normalizeTestStripeMetadataValue(entry)
		}
		return normalized
	case []interface{}:
		normalized := make([]interface{}, len(typed))
		for index, entry := range typed {
			normalized[index] = normalizeTestStripeMetadataValue(entry)
		}
		return normalized
	default:
		return value
	}
}
