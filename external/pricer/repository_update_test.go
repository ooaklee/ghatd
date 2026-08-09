package pricer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestBuildPricePlanUpdateClearsEmptyMutableFields(t *testing.T) {
	t.Parallel()

	update := buildPricePlanUpdate(&PricePlan{
		ID:            "plan-1",
		Slug:          "starter",
		Name:          "Starter",
		Status:        PricePlanStatusArchived,
		PublishedAt:   "2026-08-01T00:00:00.000000000Z",
		PublishedByID: "publisher-1",
		CreatedAt:     "2026-07-01T00:00:00.000000000Z",
		CreatedByID:   "creator-1",
		UpdatedAt:     "2026-08-02T00:00:00.000000000Z",
		UpdatedByID:   "editor-1",
	})

	setFields, ok := update["$set"].(bson.M)
	require.True(t, ok)
	assert.Equal(t, PricePlanStatusArchived, setFields["status"])
	assert.Equal(t, "2026-08-01T00:00:00.000000000Z", setFields["published_at"])
	assert.Equal(t, "publisher-1", setFields["published_by_id"])
	assert.NotContains(t, setFields, "_id")
	assert.NotContains(t, setFields, "created_at")
	assert.NotContains(t, setFields, "created_by_id")

	unsetFields, ok := update["$unset"].(bson.M)
	require.True(t, ok)
	for _, field := range []string{
		"description",
		"features",
		"costs",
		"discounts",
		"payment_terms",
		"provider_refs",
		"metadata",
		"display_order",
	} {
		assert.Contains(t, unsetFields, field)
	}
	assert.NotContains(t, unsetFields, "published_at")
	assert.NotContains(t, unsetFields, "published_by_id")
	assert.NotContains(t, unsetFields, "deleted_at")
	assert.NotContains(t, unsetFields, "deleted_by_id")
}

func TestBuildPricePlanUpdateSetsMutableFieldsIncludingZeroDisplayOrder(t *testing.T) {
	t.Parallel()

	displayOrder := 0
	update := buildPricePlanUpdate(&PricePlan{
		Slug:         "starter",
		Name:         "Starter",
		Description:  "Starter plan",
		Status:       PricePlanStatusDraft,
		Features:     []PlanFeatureRef{{FeatureID: "feature-1", Included: true}},
		Costs:        []PriceCost{{ID: "cost-1", Amount: 100, Currency: "USD", BillingCadence: PriceBillingCadenceMonthly}},
		Discounts:    []PriceDiscount{{Type: PriceDiscountTypePercent, PercentBps: 100}},
		PaymentTerms: &PricePaymentTerms{},
		ProviderRefs: []PriceProviderRef{{Provider: PriceProviderManual}},
		Metadata:     map[string]interface{}{"audience": "test"},
		DisplayOrder: &displayOrder,
	})

	setFields, ok := update["$set"].(bson.M)
	require.True(t, ok)
	for _, field := range []string{
		"description",
		"features",
		"costs",
		"discounts",
		"payment_terms",
		"provider_refs",
		"metadata",
		"display_order",
	} {
		assert.Contains(t, setFields, field)
	}
	assert.Equal(t, &displayOrder, setFields["display_order"])
	assert.NotContains(t, update, "$unset")
}

func TestBuildPricePlanQueryFilterPublishedRequiresPublishedStatus(t *testing.T) {
	t.Parallel()

	filter := buildPricePlanQueryFilter(&GetPricePlansRequest{IsPublished: true})
	andFilters, ok := filter["$and"].([]bson.M)
	require.True(t, ok)
	require.Len(t, andFilters, 2)
	assert.Equal(t, bson.M{"status": PricePlanStatusPublished}, andFilters[0])

	publishedAtFilter, ok := andFilters[1]["published_at"].(bson.M)
	require.True(t, ok)
	assert.Equal(t, true, publishedAtFilter["$exists"])
	assert.Equal(t, "", publishedAtFilter["$ne"])
	assert.NotEmpty(t, publishedAtFilter["$lte"])
}

func TestBuildPricePlanQueryFilterNotPublishedIncludesArchivedStatus(t *testing.T) {
	t.Parallel()

	filter := buildPricePlanQueryFilter(&GetPricePlansRequest{IsNotPublished: true})
	andFilters, ok := filter["$and"].([]bson.M)
	require.True(t, ok)
	require.Len(t, andFilters, 1)
	orFilters, ok := andFilters[0]["$or"].([]bson.M)
	require.True(t, ok)
	require.NotEmpty(t, orFilters)
	assert.Equal(t, bson.M{"status": bson.M{"$ne": PricePlanStatusPublished}}, orFilters[0])
}
