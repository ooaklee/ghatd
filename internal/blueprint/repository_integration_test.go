package blueprint_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/benweissmann/memongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/internal/blueprint"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// isolatedBlueprintMongo owns a unique database and bounded cleanup. Explicit
// test URIs are mandatory when supplied, including in short mode.
func isolatedBlueprintMongo(t *testing.T) (*blueprint.Repository, *mongo.Database, context.Context) {
	t.Helper()
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
	return repo, collection.Database(), ctx
}

// This stateful lifecycle deliberately follows the same record through each
// operation; splitting its steps into independent table cases would lose that contract.
func TestIntegration_BlueprintRepository_FullLifecycle(t *testing.T) {
	repo, _, ctx := isolatedBlueprintMongo(t)

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

func TestBlueprintMongoOutcomes(t *testing.T) {
	for _, scenario := range []string{"matched no-op", "missing update", "missing delete", "duplicate ID", "decode failure", "literal search", "cancel before write"} {
		t.Run(scenario, func(t *testing.T) {
			r, db, ctx := isolatedBlueprintMongo(t)
			core, logs := observer.New(zap.DebugLevel)
			ctx = logger.TransitWith(ctx, zap.New(core))
			original := &blueprint.Blueprint{ID: "private-record", Name: "a.b*", Kind: "demo", CreatedByUserID: "private-actor"}
			_, err := r.CreateBlueprint(ctx, original)
			require.NoError(t, err)
			switch scenario {
			case "matched no-op":
				got, err := r.UpdateBlueprint(ctx, original)
				require.NoError(t, err)
				require.Equal(t, original, got)
				require.NotSame(t, original, got)
			case "missing update":
				_, err = r.UpdateBlueprint(ctx, &blueprint.Blueprint{ID: "missing"})
				require.ErrorIs(t, err, blueprint.ErrBlueprintResourceNotFound)
			case "missing delete":
				err = r.DeleteBlueprintByID(ctx, "missing")
				require.ErrorIs(t, err, blueprint.ErrBlueprintResourceNotFound)
			case "duplicate ID":
				_, err = r.CreateBlueprint(ctx, original)
				require.Error(t, err)
				require.True(t, mongo.IsDuplicateKeyError(err))
				require.NotErrorIs(t, err, blueprint.ErrBlueprintResourceNotFound)
			case "decode failure":
				_, err = db.Collection(blueprint.BlueprintCollection).InsertOne(ctx, bson.M{"_id": "malformed", "name": bson.M{"private": "wrong type"}})
				require.NoError(t, err)
				_, err = r.GetBlueprintByID(ctx, "malformed")
				require.Error(t, err)
				require.NotErrorIs(t, err, blueprint.ErrBlueprintResourceNotFound)
			case "literal search":
				_, err = r.CreateBlueprint(ctx, &blueprint.Blueprint{ID: "other", Name: "axbzz", Kind: "demo"})
				require.NoError(t, err)
				found, err := r.GetBlueprints(ctx, &blueprint.GetBlueprintsRequest{Query: "a.b*"})
				require.NoError(t, err)
				require.Len(t, found, 1)
				require.Equal(t, original.ID, found[0].ID)
				total, err := r.GetTotalBlueprints(ctx, &blueprint.GetBlueprintsRequest{Query: "a.b*"})
				require.NoError(t, err)
				require.Equal(t, int64(1), total)
			case "cancel before write":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				_, err = r.CreateBlueprint(cancelled, &blueprint.Blueprint{ID: "cancelled"})
				require.ErrorIs(t, err, context.Canceled)
				count, err := db.Collection(blueprint.BlueprintCollection).CountDocuments(ctx, bson.M{"_id": "cancelled"})
				require.NoError(t, err)
				require.Zero(t, count)
			}
			for _, entry := range logs.All() {
				require.NotContains(t, fmt.Sprint(entry.ContextMap()), "private")
				require.NotContains(t, entry.Message, "private")
			}
		})
	}
}

func TestBlueprintMongoUnacknowledgedWrites(t *testing.T) {
	for _, op := range []string{"create", "update", "delete"} {
		t.Run(op, func(t *testing.T) {
			ack, db, ctx := isolatedBlueprintMongo(t)
			input := &blueprint.Blueprint{ID: "record", Name: "Original", Kind: "demo"}
			before := *input
			if op != "create" {
				_, err := ack.CreateBlueprint(ctx, input)
				require.NoError(t, err)
			}
			unackDB := db.Client().Database(db.Name(), options.Database().SetWriteConcern(writeconcern.Unacknowledged()))
			store, err := repository.NewMongoDbRepositoryFromDatabase(unackDB, repository.NewZapRepositoryLogger())
			require.NoError(t, err)
			r := blueprint.NewRepository(store)
			switch op {
			case "create":
				_, err = r.CreateBlueprint(ctx, input)
			case "update":
				_, err = r.UpdateBlueprint(ctx, input)
			case "delete":
				err = r.DeleteBlueprintByID(ctx, input.ID)
			}
			require.ErrorIs(t, err, blueprint.ErrBlueprintUnavailable)
			require.Equal(t, before, *input)
			// The server may already have applied a w:0 command. This result never
			// proves rollback, absence, or that replaying the command is safe.
		})
	}
}
