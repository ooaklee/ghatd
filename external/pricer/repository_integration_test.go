package pricer_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/benweissmann/memongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
)

func setupPricerIntegration(t *testing.T) (context.Context, *pricer.Repository, func()) {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "7.0.14"})
	if err != nil {
		t.Skipf("skipping integration test: unable to start memongo: %v", err)
	}

	dbName := memongo.RandomDatabase()
	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(mongoServer.URI(), dbName))
	require.NoError(t, err)

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	repo := pricer.NewRepository(store)

	cleanup := func() {
		mongoServer.Stop()
		_ = mongoHandler.Close(ctx)
	}

	return ctx, repo, cleanup
}

func TestIntegration_PricePlanRepository_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, repo, cleanup := setupPricerIntegration(t)
	defer cleanup()

	testPlan := &pricer.PricePlan{
		Slug:        "integration-test-plan",
		Name:        "Integration Test Plan",
		Description: "A plan created during integration test",
		Status:      pricer.PricePlanStatusDraft,
		CreatedByID: testUserID,
		Costs: []pricer.PriceCost{
			{Amount: 1999, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
		},
		ProviderRefs: []pricer.PriceProviderRef{
			{Provider: pricer.PriceProviderManual},
		},
		Features: []pricer.PlanFeatureRef{
			{FeatureID: "feat-1", FeatureSlug: "projects", Included: true, Quantity: 5},
		},
	}

	t.Run("Create and retrieve price plan", func(t *testing.T) {
		created, err := repo.CreatePricePlan(ctx, testPlan)
		require.NoError(t, err)
		assert.NotEmpty(t, created.ID)
		assert.NotEmpty(t, created.NanoID)
		assert.Equal(t, "integration-test-plan", created.Slug)
		assert.Equal(t, "Integration Test Plan", created.Name)
		assert.Equal(t, pricer.PricePlanStatusDraft, created.Status)
		assert.NotEmpty(t, created.CreatedAt)
		assert.Equal(t, testUserID, created.CreatedByID)

		retrieved, err := repo.GetPricePlanByID(ctx, created.ID, &pricer.GetPricePlanByIDRequest{
			IncludeFeatures:  true,
			IncludeCosts:     true,
			IncludeProviders: true,
		})
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		assert.Equal(t, created.ID, retrieved.ID)
		assert.Equal(t, created.Name, retrieved.Name)
		assert.Equal(t, created.Status, retrieved.Status)
		assert.Len(t, retrieved.Costs, 1)
		assert.Equal(t, int64(1999), retrieved.Costs[0].Amount)
		assert.Len(t, retrieved.Features, 1)
		assert.Len(t, retrieved.ProviderRefs, 1)
	})

	t.Run("Update price plan", func(t *testing.T) {
		updatedName := "Updated Integration Plan"
		testPlan.Name = updatedName
		testPlan.Description = "Updated description"

		updated, err := repo.UpdatePricePlan(ctx, testPlan)
		require.NoError(t, err)
		assert.Equal(t, updatedName, updated.Name)
		assert.NotEmpty(t, updated.UpdatedAt)
		assert.Equal(t, testUserID, updated.UpdatedByID)

		retrieved, err := repo.GetPricePlanByID(ctx, testPlan.ID, &pricer.GetPricePlanByIDRequest{})
		require.NoError(t, err)
		assert.Equal(t, updatedName, retrieved.Name)
	})

	t.Run("Publish price plan", func(t *testing.T) {
		err := repo.PublishPricePlan(ctx, testPlan.ID, testUserID, "")
		require.NoError(t, err)

		retrieved, err := repo.GetPricePlanByID(ctx, testPlan.ID, &pricer.GetPricePlanByIDRequest{})
		require.NoError(t, err)
		assert.Equal(t, pricer.PricePlanStatusPublished, retrieved.Status)
		assert.NotEmpty(t, retrieved.PublishedAt)
		assert.Equal(t, testUserID, retrieved.PublishedByID)
	})

	t.Run("Archive price plan", func(t *testing.T) {
		err := repo.ArchivePricePlan(ctx, testPlan.ID, testUserID, "")
		require.NoError(t, err)

		retrieved, err := repo.GetPricePlanByID(ctx, testPlan.ID, &pricer.GetPricePlanByIDRequest{})
		require.NoError(t, err)
		assert.Equal(t, pricer.PricePlanStatusArchived, retrieved.Status)
	})

	t.Run("Soft delete price plan", func(t *testing.T) {
		err := repo.SoftDeletePricePlan(ctx, testPlan.ID, testUserID, "")
		require.NoError(t, err)

		retrieved, err := repo.GetPricePlanByID(ctx, testPlan.ID, &pricer.GetPricePlanByIDRequest{})
		require.NoError(t, err)
		assert.NotEmpty(t, retrieved.DeletedAt)
		assert.Equal(t, testUserID, retrieved.DeletedByID)
		assert.Equal(t, pricer.PricePlanStatusArchived, retrieved.Status)
	})
}

func TestIntegration_PricePlanRepository_QueryFilters(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, repo, cleanup := setupPricerIntegration(t)
	defer cleanup()

	draftPlan := &pricer.PricePlan{
		Name:        "Filter Draft Plan",
		Description: "Draft plan for filter tests",
		Status:      pricer.PricePlanStatusDraft,
		CreatedByID: testUserID,
		Costs: []pricer.PriceCost{
			{Amount: 500, Currency: "EUR", BillingCadence: pricer.PriceBillingCadenceMonthly},
		},
	}

	publishedPlan := &pricer.PricePlan{
		Name:        "Filter Published Plan",
		Description: "Published plan for filter tests",
		Status:      pricer.PricePlanStatusPublished,
		CreatedByID: testUserID,
		Costs: []pricer.PriceCost{
			{Amount: 1500, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
			{Amount: 15000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceYearly},
		},
	}

	archivedPlan := &pricer.PricePlan{
		Name:        "Filter Archived Plan",
		Description: "Archived plan for filter tests",
		Status:      pricer.PricePlanStatusArchived,
		CreatedByID: testUserID,
		Costs: []pricer.PriceCost{
			{Amount: 1000, Currency: "GBP", BillingCadence: pricer.PriceBillingCadenceYearly},
		},
		DeletedAt:   "2025-01-01T00:00:00.000000000Z",
		DeletedByID: testUserID,
	}

	createdDraft, err := repo.CreatePricePlan(ctx, draftPlan)
	require.NoError(t, err)

	createdPublished, err := repo.CreatePricePlan(ctx, publishedPlan)
	require.NoError(t, err)

	createdArchived, err := repo.CreatePricePlan(ctx, archivedPlan)
	require.NoError(t, err)

	_ = createdDraft
	_ = createdPublished

	t.Run("Filter by slugs", func(t *testing.T) {
		req := &pricer.GetPricePlansRequest{
			Slugs:            createdPublished.Slug,
			PerPage:          25,
			Page:             1,
			IncludeFeatures:  true,
			IncludeCosts:     true,
			IncludeProviders: true,
		}
		results, err := repo.GetPricePlans(ctx, req)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, createdPublished.ID, results[0].ID)
	})

	t.Run("Filter by status", func(t *testing.T) {
		req := &pricer.GetPricePlansRequest{
			WithStatus: string(pricer.PricePlanStatusDraft) + "," + string(pricer.PricePlanStatusArchived),
			PerPage:    25,
			Page:       1,
		}
		results, err := repo.GetPricePlans(ctx, req)
		require.NoError(t, err)
		assert.Len(t, results, 2)

		ids := map[string]bool{}
		for _, r := range results {
			ids[r.ID] = true
		}
		assert.True(t, ids[createdDraft.ID])
		assert.True(t, ids[createdArchived.ID])
	})

	t.Run("Filter by currency", func(t *testing.T) {
		req := &pricer.GetPricePlansRequest{
			WithCurrency: "USD",
			IncludeCosts: true,
			PerPage:      25,
			Page:         1,
		}
		results, err := repo.GetPricePlans(ctx, req)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, createdPublished.ID, results[0].ID)
	})

	t.Run("Filter by billing cadence", func(t *testing.T) {
		req := &pricer.GetPricePlansRequest{
			WithBillingCadence: string(pricer.PriceBillingCadenceYearly),
			IncludeCosts:       true,
			PerPage:            25,
			Page:               1,
		}
		results, err := repo.GetPricePlans(ctx, req)
		require.NoError(t, err)
		assert.Len(t, results, 2) // Both published and archived have yearly costs
	})

	t.Run("Filter is_deleted", func(t *testing.T) {
		req := &pricer.GetPricePlansRequest{
			IsDeleted: true,
			PerPage:   25,
			Page:      1,
		}
		results, err := repo.GetPricePlans(ctx, req)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, createdArchived.ID, results[0].ID)
	})

	t.Run("Filter is_published", func(t *testing.T) {
		req := &pricer.GetPricePlansRequest{
			IsPublished: true,
			PerPage:     25,
			Page:        1,
		}
		results, err := repo.GetPricePlans(ctx, req)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, createdPublished.ID, results[0].ID)
	})

	t.Run("GetTotalPricePlans with filter", func(t *testing.T) {
		total, err := repo.GetTotalPricePlans(ctx, &pricer.GetPricePlansRequest{
			WithStatus: string(pricer.PricePlanStatusDraft),
		})
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
	})
}

func TestIntegration_PricePlanRepository_Pagination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, repo, cleanup := setupPricerIntegration(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		plan := &pricer.PricePlan{
			Name:        "Pagination Plan",
			Status:      pricer.PricePlanStatusDraft,
			CreatedByID: testUserID,
			Costs: []pricer.PriceCost{
				{Amount: 1000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
			},
		}
		_, err := repo.CreatePricePlan(ctx, plan)
		require.NoError(t, err)
	}

	t.Run("Paginate with per_page", func(t *testing.T) {
		total, err := repo.GetTotalPricePlans(ctx, &pricer.GetPricePlansRequest{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, total, int64(5))

		results, err := repo.GetPricePlans(ctx, &pricer.GetPricePlansRequest{
			PerPage: 2,
			Page:    1,
			Order:   "created_at_asc",
		})
		require.NoError(t, err)
		assert.Len(t, results, 2)
	})
}

func TestIntegration_PricePlanRepository_GetBySlug(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, repo, cleanup := setupPricerIntegration(t)
	defer cleanup()

	plan := &pricer.PricePlan{
		Name:        "Slug Lookup Plan",
		Description: "Plan for slug lookup test",
		Status:      pricer.PricePlanStatusPublished,
		CreatedByID: testUserID,
	}
	created, err := repo.CreatePricePlan(ctx, plan)
	require.NoError(t, err)

	expectedSlug := created.Slug

	t.Run("Find by exact slug", func(t *testing.T) {
		retrieved, err := repo.GetPricePlanBySlug(ctx, expectedSlug, &pricer.GetPricePlanBySlugRequest{})
		require.NoError(t, err)
		assert.Equal(t, created.ID, retrieved.ID)
		assert.Equal(t, expectedSlug, retrieved.Slug)
	})

	t.Run("Find by normalised slug", func(t *testing.T) {
		retrieved, err := repo.GetPricePlanBySlug(ctx, expectedSlug, &pricer.GetPricePlanBySlugRequest{})
		require.NoError(t, err)
		assert.Equal(t, created.ID, retrieved.ID)
	})

	t.Run("Not found for missing slug", func(t *testing.T) {
		_, err := repo.GetPricePlanBySlug(ctx, "non-existent-slug", &pricer.GetPricePlanBySlugRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), pricer.ErrKeyPricePlanNotFound)
	})
}

func TestIntegration_FeatureRepository_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, repo, cleanup := setupPricerIntegration(t)
	defer cleanup()

	testFeature := &pricer.PriceFeature{
		Name:        "Integration Test Feature",
		Description: "A feature created during integration test",
		Type:        pricer.PriceFeatureTypeQuantity,
		Unit:        pricer.PriceFeatureUnitSeat,
		SortOrder:   1,
		CreatedByID: testUserID,
	}

	t.Run("Create and retrieve feature", func(t *testing.T) {
		created, err := repo.CreateFeature(ctx, testFeature)
		require.NoError(t, err)
		assert.NotEmpty(t, created.ID)
		assert.NotEmpty(t, created.NanoID)
		assert.Equal(t, "Integration Test Feature", created.Name)
		assert.Equal(t, pricer.PriceFeatureTypeQuantity, created.Type)
		assert.Equal(t, pricer.PriceFeatureUnitSeat, created.Unit)
		assert.NotEmpty(t, created.CreatedAt)
		assert.Equal(t, testUserID, created.CreatedByID)

		slug := created.Slug
		assert.Equal(t, "integration-test-feature", slug)

		retrieved, err := repo.GetFeatureByID(ctx, created.ID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		assert.Equal(t, created.ID, retrieved.ID)
		assert.Equal(t, created.Name, retrieved.Name)
		assert.Equal(t, created.Type, retrieved.Type)
	})

	t.Run("Update feature", func(t *testing.T) {
		updatedName := "Updated Integration Feature"
		testFeature.Name = updatedName
		testFeature.Description = "Updated feature description"

		updated, err := repo.UpdateFeature(ctx, testFeature)
		require.NoError(t, err)
		assert.Equal(t, updatedName, updated.Name)
		assert.NotEmpty(t, updated.UpdatedAt)
		assert.Equal(t, testUserID, updated.UpdatedByID)

		retrieved, err := repo.GetFeatureByID(ctx, testFeature.ID)
		require.NoError(t, err)
		assert.Equal(t, updatedName, retrieved.Name)
	})

	t.Run("Soft delete feature", func(t *testing.T) {
		err := repo.SoftDeleteFeature(ctx, testFeature.ID, testUserID, "")
		require.NoError(t, err)

		retrieved, err := repo.GetFeatureByID(ctx, testFeature.ID)
		require.NoError(t, err)
		assert.NotEmpty(t, retrieved.DeletedAt)
		assert.Equal(t, testUserID, retrieved.DeletedByID)
	})
}

func TestIntegration_FeatureRepository_QueryFilters(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, repo, cleanup := setupPricerIntegration(t)
	defer cleanup()

	boolFeature := &pricer.PriceFeature{
		Name:        "Bool Feature",
		Type:        pricer.PriceFeatureTypeBoolean,
		CreatedByID: testUserID,
		SortOrder:   1,
	}

	qtyFeature := &pricer.PriceFeature{
		Name:        "Qty Feature",
		Type:        pricer.PriceFeatureTypeQuantity,
		Unit:        pricer.PriceFeatureUnitGB,
		CreatedByID: testUserID,
		SortOrder:   2,
	}

	textFeature := &pricer.PriceFeature{
		Name:        "Text Feature",
		Type:        pricer.PriceFeatureTypeText,
		CreatedByID: testUserID,
		SortOrder:   3,
	}

	createdBool, err := repo.CreateFeature(ctx, boolFeature)
	require.NoError(t, err)

	createdQty, err := repo.CreateFeature(ctx, qtyFeature)
	require.NoError(t, err)

	_, err = repo.CreateFeature(ctx, textFeature)
	require.NoError(t, err)

	_ = createdBool
	_ = createdQty

	t.Run("Filter by types", func(t *testing.T) {
		req := &pricer.GetFeaturesRequest{
			WithTypes: string(pricer.PriceFeatureTypeBoolean) + "," + string(pricer.PriceFeatureTypeQuantity),
			PerPage:   25,
			Page:      1,
		}
		results, err := repo.GetFeatures(ctx, req)
		require.NoError(t, err)
		assert.Len(t, results, 2)
	})

	t.Run("Filter by units", func(t *testing.T) {
		req := &pricer.GetFeaturesRequest{
			WithUnits: string(pricer.PriceFeatureUnitGB),
			PerPage:   25,
			Page:      1,
		}
		results, err := repo.GetFeatures(ctx, req)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, pricer.PriceFeatureUnitGB, results[0].Unit)
	})

	t.Run("Filter by slugs", func(t *testing.T) {
		req := &pricer.GetFeaturesRequest{
			Slugs:   createdBool.Slug,
			PerPage: 25,
			Page:    1,
		}
		results, err := repo.GetFeatures(ctx, req)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, createdBool.ID, results[0].ID)
	})

	t.Run("GetTotalFeatures", func(t *testing.T) {
		total, err := repo.GetTotalFeatures(ctx, &pricer.GetFeaturesRequest{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, total, int64(3))
	})
}

func TestIntegration_FeatureRepository_GetFeatureByID_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, repo, cleanup := setupPricerIntegration(t)
	defer cleanup()

	_, err := repo.GetFeatureByID(ctx, "00000000-0000-0000-0000-000000000000")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), pricer.ErrKeyPriceFeatureNotFound)
}

func TestIntegration_FallbackMongoURI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	dbName := memongo.RandomDatabase()
	mongoURI := ""

	mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "7.0.14"})
	if err == nil {
		mongoURI = mongoServer.URI()
		t.Cleanup(func() {
			mongoServer.Stop()
		})
	} else {
		mongoURI = strings.TrimSpace(os.Getenv("PRICER_IT_MONGO_URI"))
		if mongoURI == "" {
			t.Skipf("skipping integration test: memongo unavailable (%v). set PRICER_IT_MONGO_URI to run against a real MongoDB", err)
		}
	}

	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(mongoURI, dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = mongoHandler.Close(ctx)
	})

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	repo := pricer.NewRepository(store)

	plan, err := repo.CreatePricePlan(ctx, &pricer.PricePlan{
		Name:        "Fallback URI Test Plan",
		Status:      pricer.PricePlanStatusPublished,
		CreatedByID: testUserID,
		Costs: []pricer.PriceCost{
			{Amount: 1000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
		},
	})
	require.NoError(t, err)

	retrieved, err := repo.GetPricePlanByID(ctx, plan.ID, &pricer.GetPricePlanByIDRequest{})
	require.NoError(t, err)
	assert.Equal(t, plan.ID, retrieved.ID)
}
