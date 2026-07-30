package vision_test

import (
	"context"
	"errors"
	"testing"

	"github.com/benweissmann/memongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/vision"
)

func TestIntegration_VisionRepository_FullLifecycle(t *testing.T) {
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
	repo := vision.NewRepository(store)

	newVision := vision.NewVision(&vision.CreateVisionRequest{
		Name:            "Starter API",
		Kind:            "Service",
		Description:     "Reference package wiring",
		Status:          vision.VisionStatusActive,
		CreatedByUserID: "user-1",
	})
	newVision.GenerateID()
	newVision.GenerateNanoID()
	newVision.SetCreatedAtTimeToNow()

	created, err := repo.CreateVision(ctx, newVision)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.Equal(t, "service", created.Kind)

	retrieved, err := repo.GetVisionByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, "Starter API", retrieved.Name)

	byName, err := repo.GetVisionByNameAndKind(ctx, "Starter API", "Service")
	require.NoError(t, err)
	assert.Equal(t, created.ID, byName.ID)

	filtered, err := repo.GetVisions(ctx, &vision.GetVisionsRequest{
		Query:    "starter",
		Kind:     "Service",
		Status:   vision.VisionStatusActive,
		Page:     1,
		PageSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, created.ID, filtered[0].ID)

	total, err := repo.GetTotalVisions(ctx, &vision.GetVisionsRequest{Kind: "Service"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)

	created.Status = vision.VisionStatusArchived
	updated, err := repo.UpdateVision(ctx, created)
	require.NoError(t, err)
	assert.Equal(t, vision.VisionStatusArchived, updated.Status)

	retrieved, err = repo.GetVisionByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, vision.VisionStatusArchived, retrieved.Status)

	err = repo.DeleteVisionByID(ctx, created.ID)
	require.NoError(t, err)

	_, err = repo.GetVisionByID(ctx, created.ID)
	if !errors.Is(err, vision.ErrVisionResourceNotFound) {
		t.Fatalf("GetVisionByID() error = %v, want ErrVisionResourceNotFound", err)
	}
}
