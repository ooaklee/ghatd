package contentmanager

import (
	"encoding/json"
	"testing"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/post"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

func TestLatestFeedsMongoKeepRestrictedTitlesPrivate(t *testing.T) {
	for _, operation := range []string{"post overview", "notification overview"} {
		for _, kind := range []post.PostType{post.PostTypeArticle, post.PostTypeChangelog, post.PostTypeFaq, post.PostTypeGlossary} {
			for _, tc := range []struct {
				name          string
				administrator bool
			}{
				{name: "public caller"}, {name: "administrator still receives public latest feed", administrator: true},
			} {
				t.Run(operation+"/"+string(kind)+"/"+tc.name, func(t *testing.T) {
					ctx, core := changelogSitemapMongoFixture(t)
					posts := post.NewRepository(core)
					collection, err := posts.GetPostCollection(ctx)
					require.NoError(t, err)
					_, err = collection.InsertOne(ctx, post.Post{Id: "private", Type: kind, Title: "Members-only title must remain private", UrlFriendlyId: string(kind) + "-members-only", PublishedAt: "2020-01-01T00:00:00.000000000", Visibility: []string{"members"}})
					require.NoError(t, err)
					publicKind := post.PostTypeArticle
					if kind == publicKind {
						publicKind = post.PostTypeChangelog
					}
					_, err = collection.InsertOne(ctx, post.Post{Id: "public", Type: publicKind, Title: "Public update", UrlFriendlyId: string(publicKind) + "-public-update", PublishedAt: "2019-01-01T00:00:00.000000000"})
					require.NoError(t, err)
					users := &countingContentManagerUserService{}
					actor := ""
					if tc.administrator {
						actor = contentActorID
						users.user = &user.UniversalUser{ID: actor, Roles: []string{user.UserRoleAdmin}}
						ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, actor), true)
					}
					service := NewService(post.NewService(posts, nil), users)
					var encoded []byte
					if operation == "post overview" {
						response, readErr := service.GetLatestPostsByType(ctx, &GetLatestPostsByTypeRequest{ActorID: actor, GetLatestPostsByTypeRequest: &post.GetLatestPostsByTypeRequest{Types: "article,changelog,faq,glossary", Limit: 10}})
						require.NoError(t, readErr)
						require.Len(t, response.Overviews, 1)
						require.Equal(t, "public", response.Overviews[0].Id)
						encoded, err = json.Marshal(response)
					} else {
						response, readErr := service.GetLatestNotificationOverviews(ctx, &GetLatestNotificationOverviewsRequest{GetLatestNotificationOverviewsRequest: &common.GetLatestNotificationOverviewsRequest{UserID: actor, Kinds: "post_article,post_changelog,post_faq,post_glossary", Limit: 10}})
						require.NoError(t, readErr)
						require.Len(t, response.Overviews, 1)
						require.Equal(t, "public", response.Overviews[0].ID)
						encoded, err = json.Marshal(response)
					}
					require.NoError(t, err)
					require.NotContains(t, string(encoded), "Members-only")
					require.NotContains(t, string(encoded), "members-only")
					stored, err := posts.GetPostById(ctx, "private")
					require.NoError(t, err)
					require.Equal(t, []string{"members"}, stored.Visibility, "public reads cannot rewrite source visibility")
				})
			}
		}
	}
}
