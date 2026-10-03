package blueprint_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/benweissmann/memongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/internal/blueprint"
)

// This stateful lifecycle deliberately follows the same record through each
// operation; splitting its steps into independent table cases would lose that contract.
func TestIntegration_BlueprintRepository_FullLifecycle(t *testing.T) {
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if testing.Short() && uri == "" {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	if uri == "" {
		mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "7.0.14"})
		if err != nil {
			t.Skipf("unable to start optional memongo: %v", err)
		}
		t.Cleanup(mongoServer.Stop)
		uri = mongoServer.URI()
	}

	dbName := memongo.RandomDatabase()
	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, mongoHandler.Close(cleanup))
	})

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	repo := blueprint.NewRepository(store)
	collection, err := repo.GetBlueprintCollection(ctx)
	require.NoError(t, err, "an explicit Mongo URI must be reachable")
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, collection.Database().Drop(cleanup))
	})

	newBlueprint := blueprint.NewBlueprint(&blueprint.CreateBlueprintRequest{
		Name:        "Starter API",
		Kind:        "Service",
		Description: "Reference package wiring",
		Status:      blueprint.BlueprintStatusActive,
		ActorID:     "user-1",
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
