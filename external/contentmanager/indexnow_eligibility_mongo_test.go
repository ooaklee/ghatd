package contentmanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/seo"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestIndexNowNativeInventorySources(t *testing.T) {
	for _, source := range []string{"CMS", "SEO"} {
		for _, tc := range []struct {
			name      string
			invalidID any
		}{
			{name: "complete stable keyset pages"},
			{name: "object ID fails rather than disappearing", invalidID: bson.NewObjectID()},
			{name: "numeric ID fails rather than disappearing", invalidID: int64(1)},
			{name: "blank ID fails rather than disappearing", invalidID: ""},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				ctx, core := changelogSitemapMongoFixture(t)
				posts := post.NewRepository(core)
				entries := seo.NewRepository(core)
				collection, err := posts.GetPostCollection(ctx)
				require.NoError(t, err)
				if source == "SEO" {
					collection, err = entries.GetSitemapItemsCollection(ctx)
					require.NoError(t, err)
				}
				for i := 104; i >= 0; i-- { // Insertion/timestamp order cannot substitute for the ID cursor.
					_, err := collection.InsertOne(ctx, bson.M{"_id": fmt.Sprintf("item-%03d", i), "created_at": "same-value", "published_at": ""})
					require.NoError(t, err)
				}
				if tc.invalidID != nil {
					_, err := collection.InsertOne(ctx, bson.M{"_id": tc.invalidID})
					require.NoError(t, err)
				}
				after := ""
				var ids []string
				var scanErr error
				for page := 0; page < 3; page++ {
					if source == "CMS" {
						response, err := post.NewService(posts, nil).ListIndexNowPosts(ctx, &post.ListIndexNowPostsRequest{AfterID: after, Limit: 100})
						scanErr = err
						if err != nil {
							break
						}
						for _, item := range response.Items {
							ids = append(ids, item.Id)
						}
						after = response.NextID
					} else {
						response, err := seo.NewService(&seo.NewServiceRequest{SitemapRepository: entries}).ListIndexNowSitemapItems(ctx, &seo.ListIndexNowSitemapItemsRequest{AfterID: after, Limit: 100})
						scanErr = err
						if err != nil {
							break
						}
						for _, item := range response.Items {
							ids = append(ids, item.ID)
						}
						after = response.NextID
					}
					if after == "" {
						break
					}
				}
				if tc.invalidID != nil {
					require.Error(t, scanErr)
					return
				}
				require.NoError(t, scanErr)
				require.Empty(t, after)
				require.Len(t, ids, 105)
				for i, id := range ids {
					require.Equal(t, fmt.Sprintf("item-%03d", i), id)
				}
			})
		}
	}
}

func TestIndexNowNativeIndependentSchedulesRemainReadOnly(t *testing.T) {
	now := cmsIndexNowTime()
	for _, tc := range []struct {
		name, cmsDate, seoDate, seoUpdated string
		want                               bool
	}{
		{name: "SEO becomes due after CMS checkpoint", cmsDate: "2026-10-30T10:00:00Z", seoDate: "2026-10-30T12:00:00Z", want: true},
		{name: "CMS becomes due after SEO checkpoint", cmsDate: "2026-10-30T12:00:00Z", seoDate: "2026-10-30T10:00:00Z", want: true},
		{name: "rescheduled SEO blocks earlier CMS", cmsDate: "2026-10-30T12:00:00Z", seoDate: "2026-10-31T12:00:00Z"},
		{name: "cleared SEO gate has update event", cmsDate: "2026-10-30T10:00:00Z", seoUpdated: "2026-10-30T12:00:00Z", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, core := changelogSitemapMongoFixture(t)
			posts := post.NewRepository(core)
			entries := seo.NewRepository(core)
			postCollection, err := posts.GetPostCollection(ctx)
			require.NoError(t, err)
			seoCollection, err := entries.GetSitemapItemsCollection(ctx)
			require.NoError(t, err)
			item := cmsIndexNowPost("article-due")
			item.PublishedAt = tc.cmsDate
			_, err = postCollection.InsertOne(ctx, item)
			require.NoError(t, err)
			entry := seo.SitemapItem{ID: "entry-1", URI: "/blog/due", PublishedAt: tc.seoDate, UpdatedAt: tc.seoUpdated, LastMod: "2026-10-01T00:00:00Z", Priority: 0.8, ChangeFrequency: seo.ChangeFrequencyDaily}
			_, err = seoCollection.InsertOne(ctx, entry)
			require.NoError(t, err)
			notifier := &cmsIndexNowSubmitter{t: t, status: 202}
			config := cmsIndexNowConfig(&cmsIndexNowPostPort{})
			config.LookupPost = posts.GetPostById
			config.PublicationSource, config.SitemapSource = posts, entries
			config.EligibleURL = func(_ context.Context, _ string, at time.Time) (bool, error) {
				require.Equal(t, now, at)
				return true, nil
			}
			service := NewService(post.NewService(posts, nil), nil, WithIndexNow(notifier, config))
			result, err := service.ReconcileIndexNowEligibility(ctx, &ReconcileIndexNowPublicationsRequest{After: now.Add(-time.Hour), Through: now})
			require.NoError(t, err)
			require.True(t, result.Complete)
			require.Equal(t, tc.want, len(notifier.calls) == 1)
			var storedPost post.Post
			require.NoError(t, postCollection.FindOne(ctx, bson.M{"_id": item.Id}).Decode(&storedPost))
			require.Equal(t, *item, storedPost)
			var storedSEO seo.SitemapItem
			require.NoError(t, seoCollection.FindOne(ctx, bson.M{"_id": entry.ID}).Decode(&storedSEO))
			require.Equal(t, entry, storedSEO, "reconciliation cannot ingest or rewrite independent schedules and metadata")
		})
	}
}
