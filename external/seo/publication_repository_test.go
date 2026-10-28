package seo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Each fixture owns only its randomly named database and disconnects its client.
func publicationRepositoryMongo(t *testing.T) (context.Context, *Repository, *mongo.Collection) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI for MongoDB integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(5 * time.Second))
	require.NoError(t, err)
	db := client.Database("ghatd_seo_publication_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		require.NoError(t, db.Drop(cleanup))
		require.NoError(t, client.Disconnect(cleanup))
	})
	core, err := repository.NewMongoDbRepositoryFromDatabase(db, repository.NewZapRepositoryLogger())
	require.NoError(t, err)
	collection := db.Collection(SitemapItemsCollection)
	_, err = collection.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "uri", Value: 1}}, Options: options.Index().SetUnique(true)})
	require.NoError(t, err)
	return ctx, NewRepository(core), collection
}

func TestPublicationRepositoryMongoAtomicIntent(t *testing.T) {
	const currentSchedule = "2028-04-05T12:00:00.123456789Z"
	const staleSchedule = "2027-01-01T12:00:00Z"
	const newSchedule = "2029-02-03T04:05:06.987654321Z"
	for _, operation := range []string{"update", "upsert", "legacy update", "legacy upsert"} {
		for _, tc := range []struct {
			name                string
			exists, storedField bool
			intent              *string
			itemSchedule, want  string
			wantField           bool
		}{
			{name: "omission preserves current schedule despite stale caller", exists: true, storedField: true, itemSchedule: staleSchedule, want: currentSchedule, wantField: true},
			{name: "empty legacy caller preserves current schedule", exists: true, storedField: true, want: currentSchedule, wantField: true},
			{name: "explicit clear persists empty field", exists: true, storedField: true, intent: new(""), itemSchedule: staleSchedule, wantField: true},
			{name: "explicit set replaces current schedule", exists: true, storedField: true, intent: new(newSchedule), itemSchedule: staleSchedule, want: newSchedule, wantField: true},
			{name: "legacy absent field remains absent", exists: true},
			{name: "new omitted schedule ignores stale caller", itemSchedule: staleSchedule},
			{name: "new omitted schedule remains absent"},
			{name: "new explicit set", intent: new(newSchedule), want: newSchedule, wantField: true},
			{name: "new explicit clear", intent: new(""), wantField: true},
		} {
			legacy := strings.HasPrefix(operation, "legacy")
			upsert := strings.HasSuffix(operation, "upsert")
			if !upsert && !tc.exists {
				continue
			}
			// Legacy full-item methods cannot express clear or distinguish a
			// nonempty stale value from an explicit set; test their omission path.
			if legacy && (tc.intent != nil || tc.itemSchedule != "") {
				continue
			}
			t.Run(fmt.Sprintf("%s/%s", operation, tc.name), func(t *testing.T) {
				ctx, repo, collection := publicationRepositoryMongo(t)
				input := publicItem("/release")
				input.ID, input.CreatedAt = "caller-generated-id", "2026-10-28T12:00:00Z"
				input.PublishedAt, input.Priority = tc.itemSchedule, 0.9
				if tc.exists {
					document := bson.M{
						"_id": "stored-id", "uri": input.URI, "created_at": "2026-01-01T00:00:00Z",
						"priority": 0.2, "extra_metadata": bson.M{"reviewed": true},
					}
					if tc.storedField {
						document["published_at"] = currentSchedule
					}
					_, err := collection.InsertOne(ctx, document)
					require.NoError(t, err)
				}
				var result *SitemapItem
				var err error
				created := false
				switch operation {
				case "update":
					result, err = repo.UpdateSitemapItemWithPublication(ctx, &input, tc.intent)
				case "upsert":
					result, created, err = repo.UpsertSitemapItemWithPublication(ctx, &input, tc.intent)
				case "legacy update":
					result, err = repo.UpdateSitemapItemByURI(ctx, &input)
				case "legacy upsert":
					result, created, err = repo.UpsertSitemapItemByURI(ctx, &input)
				}
				require.NoError(t, err)
				if upsert {
					require.Equal(t, !tc.exists, created)
				}
				wantID, wantCreated := input.ID, input.CreatedAt
				if tc.exists {
					wantID, wantCreated = "stored-id", "2026-01-01T00:00:00Z"
				}
				require.Equal(t, wantID, result.ID, "upsert returns the stored identity, not the incoming generated ID")
				require.Equal(t, wantCreated, result.CreatedAt)
				require.Equal(t, tc.want, result.PublishedAt)
				require.Equal(t, 0.9, result.Priority)
				reloaded, err := repo.GetSitemapItemByURI(ctx, input.URI)
				require.NoError(t, err)
				require.Equal(t, result, reloaded)
				raw, err := collection.FindOne(ctx, bson.M{"uri": input.URI}).Raw()
				require.NoError(t, err)
				publication, err := raw.LookupErr("published_at")
				if tc.wantField {
					require.NoError(t, err)
					require.Equal(t, tc.want, publication.StringValue())
				} else {
					require.Error(t, err, "omission must not manufacture an empty field")
				}
				if tc.exists {
					require.True(t, raw.Lookup("extra_metadata").Document().Lookup("reviewed").Boolean(), "unknown metadata survives updates")
				}
				count, err := collection.CountDocuments(ctx, bson.M{})
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
			})
		}
	}
}
