package contentmanager

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/post"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

func TestPublicPostPublicationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, date, deleted string
		visibility          []string
		nilItem, want       bool
	}{
		{name: "due UTC", date: "2026-10-30T12:00:00Z", want: true},
		{name: "due platform", date: "2026-10-30T12:00:00.000000000", want: true},
		{name: "offset instant", date: "2026-10-30T13:00:00+01:00", want: true},
		{name: "future nanosecond", date: "2026-10-30T12:00:00.000000001Z"},
		{name: "draft"}, {name: "malformed", date: "invalid"}, {name: "date only", date: "2026-10-30"},
		{name: "zero", date: "0001-01-01T00:00:00Z"},
		{name: "offset hour overflow", date: "2026-10-30T12:00:00+24:00"},
		{name: "offset minute overflow", date: "2026-10-30T12:00:00+01:60"},
		{name: "deleted", date: "2026-10-30T12:00:00Z", deleted: "deleted"},
		{name: "restricted", date: "2026-10-30T12:00:00Z", visibility: []string{"member"}},
		{name: "missing", nilItem: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := &post.Post{PublishedAt: tc.date, DeletedAt: tc.deleted, Visibility: tc.visibility}
			if tc.nilItem {
				item = nil
			}
			require.Equal(t, tc.want, IsPublicPostAt(item, cmsIndexNowTime()))
		})
	}
}

type publicationListPort struct {
	postService
	response *post.GetPostsResponse
}

func (p *publicationListPort) GetArticles(context.Context, *post.GetArticlesRequest) (*post.GetArticlesResponse, error) {
	return &post.GetArticlesResponse{GetPostsResponse: p.response}, nil
}
func (p *publicationListPort) GetChangelogItems(context.Context, *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error) {
	return &post.GetChangelogItemsResponse{GetPostsResponse: p.response}, nil
}
func (p *publicationListPort) GetGlossaryItems(context.Context, *post.GetGlossaryItemsRequest) (*post.GetGlossaryItemsResponse, error) {
	return &post.GetGlossaryItemsResponse{GetPostsResponse: p.response}, nil
}
func (p *publicationListPort) GetFaqItems(context.Context, *post.GetFaqItemsRequest) (*post.GetFaqItemsResponse, error) {
	return &post.GetFaqItemsResponse{GetPostsResponse: p.response}, nil
}

func TestPublicListsRecheckAdapterPublication(t *testing.T) {
	for _, kind := range []string{"article", "changelog", "glossary", "faq"} {
		for _, tc := range []struct {
			name               string
			admin, placeholder bool
			count              int
		}{
			{name: "public", count: 1}, {name: "verified administrator preview", admin: true, count: 5}, {name: "anonymous role claim", admin: true, placeholder: true, count: 1},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				source := &post.GetPostsResponse{Total: 5, TotalPages: 1, Posts: []post.Post{
					{Id: "public", PublishedAt: "2026-10-30T13:00:00+01:00"},
					{Id: "future", PublishedAt: "2026-10-30T12:00:00.000000001Z"},
					{Id: "malformed", PublishedAt: "invalid"},
					{Id: "private", PublishedAt: "2026-10-30T12:00:00Z", Visibility: []string{"members"}},
					{Id: "deleted", PublishedAt: "2026-10-30T12:00:00Z", DeletedAt: "deleted"},
				}}
				actor := ""
				ctx := context.Background()
				users := &countingContentManagerUserService{}
				if tc.admin {
					actor = contentActorID
					users.user = &user.UniversalUser{ID: actor, Roles: []string{user.UserRoleAdmin}}
					ctx = contentManagerRequestContext(actor, !tc.placeholder)
				}
				service := NewService(&publicationListPort{response: source}, users, WithPublicationClock(cmsIndexNowTime))
				var result *post.GetPostsResponse
				switch kind {
				case "article":
					r, e := service.GetArticles(ctx, &GetArticlesRequest{ActorID: actor, GetArticlesRequest: &post.GetArticlesRequest{}})
					require.NoError(t, e)
					result = r.GetPostsResponse
				case "changelog":
					r, e := service.GetChangelogItems(ctx, &GetChangelogItemsRequest{ActorID: actor, GetChangelogItemsRequest: &post.GetChangelogItemsRequest{}})
					require.NoError(t, e)
					result = r.GetPostsResponse
				case "glossary":
					r, e := service.GetGlossaryItems(ctx, &GetGlossaryItemsRequest{ActorID: actor, GetGlossaryItemsRequest: &post.GetGlossaryItemsRequest{}})
					require.NoError(t, e)
					result = r.GetPostsResponse
				default:
					r, e := service.GetFaqItems(ctx, &GetFaqItemsRequest{ActorID: actor, GetFaqItemsRequest: &post.GetFaqItemsRequest{}})
					require.NoError(t, e)
					result = r.GetPostsResponse
				}
				require.Len(t, result.Posts, tc.count)
				require.Len(t, source.Posts, 5)
				require.Empty(t, source.Posts[0].PublishedAs, "enrichment must not mutate adapter-owned records")
				require.Equal(t, 5, result.Total, "source totals cannot be recomputed from one filtered page")
			})
		}
	}
}

func TestSingleItemPublicationChecksKeepAdministratorPreview(t *testing.T) {
	for _, kind := range []string{"article", "changelog"} {
		for _, tc := range []struct {
			name, date                           string
			restricted, admin, placeholder, want bool
		}{
			{name: "due", date: "2026-10-30T12:00:00Z", want: true},
			{name: "future denied", date: "2026-10-30T12:00:01Z"},
			{name: "malformed denied", date: "invalid"},
			{name: "restricted denied", date: "2026-10-30T12:00:00Z", restricted: true},
			{name: "verified admin future preview", date: "2026-10-30T12:00:01Z", admin: true, want: true},
			{name: "placeholder cannot preview", date: "2026-10-30T12:00:01Z", admin: true, placeholder: true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				item := &post.Post{Id: contentTargetID, UrlFriendlyId: kind + "-example", PublishedAt: tc.date}
				if tc.restricted {
					item.Visibility = []string{"member"}
				}
				port := &contentPublicPort{item: item}
				users := &countingContentManagerUserService{}
				actor := ""
				ctx := context.Background()
				if tc.admin {
					actor = contentActorID
					users.user = &user.UniversalUser{ID: actor, Roles: []string{user.UserRoleAdmin}}
					ctx = contentManagerRequestContext(actor, !tc.placeholder)
				}
				service := NewService(port, users, WithPublicationClock(cmsIndexNowTime))
				var got *post.Post
				var err error
				if kind == "article" {
					got, err = service.GetArticleItemByUrlFriendlyId(ctx, &GetArticleItemByUrlFriendlyIdRequest{ActorID: actor, UrlFriendlyId: item.UrlFriendlyId})
				} else {
					got, err = service.GetChangelogItemByUrlFriendlyId(ctx, &GetChangelogItemByUrlFriendlyIdRequest{ActorID: actor, UrlFriendlyId: item.UrlFriendlyId})
				}
				if tc.want {
					require.NoError(t, err)
					require.Equal(t, item.Id, got.Id)
				} else {
					require.ErrorIs(t, err, ErrUnauthorisedCMUser)
					require.Nil(t, got)
				}
			})
		}
	}
}

func TestReconciliationReplayAndBoundedPages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		replay bool
		count  int
	}{
		{name: "multiple pages", count: 101}, {name: "explicit replay may duplicate", count: 1, replay: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &cmsIndexNowPostPort{pages: map[int]*post.GetPostsResponse{}}
			for i := 0; i < tc.count; i++ {
				page := i/100 + 1
				if p.pages[page] == nil {
					p.pages[page] = &post.GetPostsResponse{TotalPages: (tc.count + 99) / 100}
				}
				item := cmsIndexNowPost("article-story")
				item.PublishedAt = "2026-10-30T12:00:00Z"
				p.pages[page].Posts = append(p.pages[page].Posts, *item)
			}
			submitter := &cmsIndexNowSubmitter{t: t, status: 200}
			s := NewService(p, nil, WithIndexNow(submitter, cmsIndexNowConfig(p)))
			runs := 1
			if tc.replay {
				runs = 2
			}
			for range runs {
				response, err := s.ReconcileIndexNowPublications(t.Context(), &ReconcileIndexNowPublicationsRequest{After: cmsIndexNowTime().Add(-time.Hour), Through: cmsIndexNowTime()})
				require.NoError(t, err)
				require.True(t, response.Complete)
				require.Equal(t, tc.count, response.Inspected)
			}
			require.Len(t, submitter.calls, runs)
			for _, call := range submitter.calls {
				require.Len(t, call, 1, "duplicate catalogue URLs are deduplicated within a batch")
			}
		})
	}
}
