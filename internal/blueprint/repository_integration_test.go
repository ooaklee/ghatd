package blueprint_test

import (
	"context"
	"errors"
	"testing"

	"github.com/benweissmann/memongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/internal/blueprint"
)

func TestIntegration_BlueprintRepository_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "7.0.14"})
	if err != nil {
		t.Skipf("skipping integration test: unable to start memongo: %v", err)
	}
	t.Cleanup(func() {
		mongoServer.Stop()
	})

	dbName := memongo.RandomDatabase()
	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(mongoServer.URI(), dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = mongoHandler.Close(ctx)
	})

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	repo := blueprint.NewRepository(store)

	newBlueprint := blueprint.NewBlueprint(&blueprint.CreateBlueprintRequest{
		Name:            "Starter API",
		Kind:            "Service",
		Description:     "Reference package wiring",
		Status:          blueprint.BlueprintStatusActive,
		CreatedByUserID: "user-1",
	})
	newBlueprint.GenerateID()
	newBlueprint.GenerateNanoID()
	newBlueprint.SetCreatedAtTimeToNow()

	created, err := repo.CreateBlueprint(ctx, newBlueprint)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.Equal(t, "service", created.Kind)

	retrieved, err := repo.GetBlueprintByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, "Starter API", retrieved.Name)

	byName, err := repo.GetBlueprintByNameAndKind(ctx, "Starter API", "Service")
	require.NoError(t, err)
	assert.Equal(t, created.ID, byName.ID)

	filtered, err := repo.GetBlueprints(ctx, &blueprint.GetBlueprintsRequest{
		Query:    "starter",
		Kind:     "Service",
		Status:   blueprint.BlueprintStatusActive,
		Page:     1,
		PageSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, created.ID, filtered[0].ID)

	total, err := repo.GetTotalBlueprints(ctx, &blueprint.GetBlueprintsRequest{Kind: "Service"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)

	created.Status = blueprint.BlueprintStatusArchived
	updated, err := repo.UpdateBlueprint(ctx, created)
	require.NoError(t, err)
	assert.Equal(t, blueprint.BlueprintStatusArchived, updated.Status)

	retrieved, err = repo.GetBlueprintByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, blueprint.BlueprintStatusArchived, retrieved.Status)

	err = repo.DeleteBlueprintByID(ctx, created.ID)
	require.NoError(t, err)

	_, err = repo.GetBlueprintByID(ctx, created.ID)
	if !errors.Is(err, blueprint.ErrBlueprintResourceNotFound) {
		t.Fatalf("GetBlueprintByID() error = %v, want ErrBlueprintResourceNotFound", err)
	}
}
