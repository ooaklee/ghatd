package contentmanager

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func changelogSitemapMongoFixture(t *testing.T) (context.Context, *repository.MongoDbRepository) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI for MongoDB integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("ghatd_changelog_sitemap_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(cleanup))
		require.NoError(t, client.Disconnect(cleanup))
	})
	core, err := repository.NewMongoDbRepositoryFromDatabase(db, repository.NewZapRepositoryLogger())
	require.NoError(t, err)
	return ctx, core
}

func TestChangelogSitemapMongoSourceOffsetsAndStablePagination(t *testing.T) {
	for _, perPage := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("page-size-%d", perPage), func(t *testing.T) {
			ctx, core := changelogSitemapMongoFixture(t)
			posts := post.NewRepository(core)
			collection, err := posts.GetPostCollection(ctx)
			require.NoError(t, err)
			now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
			for index := 11; index >= 0; index-- { // Deliberately reverse insertion order.
				item := post.Post{
					Id: fmt.Sprintf("%02d", index), Type: post.PostTypeChangelog,
					UrlFriendlyId: fmt.Sprintf("changelog-release-%02d", index),
					CreatedAt:     "2026-10-28T13:00:00+01:00", PublishedAt: "2026-10-28T13:00:00+01:00",
				}
				switch index {
				case 0:
					item.PublishedAt = ""
				case 1:
					item.PublishedAt = "2026-10-28T13:00:00Z"
				case 2:
					item.Visibility = []string{"private-reader"}
				case 3:
					item.PublishedAt = "invalid"
				case 5:
					item.PublishedAt = "2026-10-28T12:00:00.000000000"
				case 6:
					item.PublishedAt = "2026-10-28T11:00:00-01:00"
				case 7:
					item.Type = post.PostTypeArticle
				case 8:
					item.DeletedAt = "2026-10-28T11:00:00.000000000"
				case 9:
					// Lexically later, but actually before the requested creation cutoff.
					item.CreatedAt = "2026-10-28T14:00:00+03:00"
				case 10:
					item.UrlFriendlyId = ""
				}
				_, err := collection.InsertOne(ctx, item)
				require.NoError(t, err)
			}
			source := post.NewService(posts, nil)
			request := &GetChangelogSitemapItemsRequest{PerPage: perPage, CreatedAtFrom: "2026-10-28T12:00:00Z"}
			for range 2 {
				result, err := getChangelogSitemapItems(ctx, source, request, now)
				require.NoError(t, err)
				require.Equal(t, 5, result.ProcessedArticles)
				require.Equal(t, 1, result.SkippedMissingURLFriendlyID)
				require.Equal(t, 4, result.SeedableArticles)
				var ids []string
				for _, item := range result.Items {
					ids = append(ids, item.PostID)
					require.Equal(t, "2026-10-28T12:00:00.000000000", item.LastMod)
				}
				require.Equal(t, []string{"04", "05", "06", "11"}, ids, "equal creation dates use ID ordering across every page")
			}
			request.Limit = 2
			result, err := getChangelogSitemapItems(ctx, source, request, now)
			require.NoError(t, err)
			require.Equal(t, 2, result.ProcessedArticles, "draft, future, restricted and invalid publication dates do not consume the limit")
			require.Equal(t, []string{"04", "05"}, []string{result.Items[0].PostID, result.Items[1].PostID})
			count, err := collection.CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.EqualValues(t, 12, count, "source generation performs no writes")

		})
	}
}
