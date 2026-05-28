package contentmanager

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/post"
)

type fakeContentManagerPostService struct {
	reqs  []*post.GetArticlesRequest
	resps []*post.GetArticlesResponse
	err   error
}

type noopContentManagerValidator struct{}

func (noopContentManagerValidator) Validate(s interface{}) error {
	return nil
}

func (f *fakeContentManagerPostService) GetArticles(ctx context.Context, req *post.GetArticlesRequest) (*post.GetArticlesResponse, error) {
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return nil, f.err
	}
	index := len(f.reqs) - 1
	if len(f.resps) > index {
		return f.resps[index], nil
	}
	return &post.GetArticlesResponse{GetPostsResponse: &post.GetPostsResponse{}}, nil
}

func (f *fakeContentManagerPostService) GetChangelogItems(ctx context.Context, req *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error) {
	return nil, nil
}

func (f *fakeContentManagerPostService) GetFaqItems(ctx context.Context, req *post.GetFaqItemsRequest) (*post.GetFaqItemsResponse, error) {
	return nil, nil
}

func (f *fakeContentManagerPostService) GetGlossaryItems(ctx context.Context, req *post.GetGlossaryItemsRequest) (*post.GetGlossaryItemsResponse, error) {
	return nil, nil
}

func (f *fakeContentManagerPostService) CreatePost(ctx context.Context, req *post.CreatePostRequest) (*post.CreatePostResponse, error) {
	return nil, nil
}

func (f *fakeContentManagerPostService) UpdatePost(ctx context.Context, req *post.UpdatePostRequest) (*post.UpdatePostResponse, error) {
	return nil, nil
}

func (f *fakeContentManagerPostService) DeletePostById(ctx context.Context, req *post.DeletePostByIdRequest) (*post.DeletePostByIdResponse, error) {
	return nil, nil
}

func (f *fakeContentManagerPostService) RestorePostById(ctx context.Context, req *post.RestorePostByIdRequest) (*post.RestorePostByIdResponse, error) {
	return nil, nil
}

func (f *fakeContentManagerPostService) GetPosts(ctx context.Context, req *post.GetPostsRequest) (*post.GetPostsResponse, error) {
	return nil, nil
}

func (f *fakeContentManagerPostService) GetPostByUrlFriendlyId(ctx context.Context, urlFriendlyId string) (*post.Post, error) {
	return nil, nil
}

func (f *fakeContentManagerPostService) GetLatestPostsByType(ctx context.Context, req *post.GetLatestPostsByTypeRequest) (*post.GetLatestPostsByTypeResponse, error) {
	return nil, nil
}

func (f *fakeContentManagerPostService) GetLatestNotificationOverviews(ctx context.Context, req *common.GetLatestNotificationOverviewsRequest) (*common.GetLatestNotificationOverviewsResponse, error) {
	return nil, nil
}

func TestGetArticleSitemapItemsBuildsPublicBlogURIs(t *testing.T) {
	postService := &fakeContentManagerPostService{
		resps: []*post.GetArticlesResponse{
			{
				GetPostsResponse: &post.GetPostsResponse{
					TotalPages: 1,
					Posts: []post.Post{
						{
							Id:            "post-1",
							UrlFriendlyId: "article-you-choose-to-stop-apologising-for-being-you-guided-reflection",
							Type:          post.PostTypeArticle,
							PublishedAt:   "2026-05-28T08:00:00",
							UpdatedAt:     "2026-05-28T09:00:00",
						},
						{
							Id:          "post-2",
							PublishedAt: "2026-05-28T10:00:00",
						},
					},
				},
			},
		},
	}

	response, err := getArticleSitemapItems(context.Background(), postService, &GetArticleSitemapItemsRequest{
		CreatedAtFrom: "2026-05-28T08:00:00",
	})
	if err != nil {
		t.Fatalf("getArticleSitemapItems() error = %v", err)
	}

	if response.ProcessedArticles != 2 || response.SeedableArticles != 1 || response.SkippedMissingURLFriendlyID != 1 {
		t.Fatalf("response = %+v, want processed=2 seedable=1 skipped=1", response)
	}
	if len(response.Items) != 1 {
		t.Fatalf("items = %#v, want one sitemap item", response.Items)
	}
	item := response.Items[0]
	if item.URI != "/blog/you-choose-to-stop-apologising-for-being-you-guided-reflection" {
		t.Fatalf("URI = %q, want public blog route", item.URI)
	}
	if item.LastMod != "2026-05-28T09:00:00" {
		t.Fatalf("LastMod = %q, want updated_at", item.LastMod)
	}
	if item.PublicSlug != "you-choose-to-stop-apologising-for-being-you-guided-reflection" {
		t.Fatalf("PublicSlug = %q, want article prefix removed", item.PublicSlug)
	}

	if len(postService.reqs) != 1 || postService.reqs[0].GetPostsRequest == nil {
		t.Fatalf("post service requests = %#v, want one embedded posts request", postService.reqs)
	}
	postReq := postService.reqs[0].GetPostsRequest
	if postReq.Order != "created_at_asc" || postReq.CreatedAtFrom != "2026-05-28T08:00:00" {
		t.Fatalf("post request = %+v, want created_at ascending from cursor", postReq)
	}
	if !postReq.IsPublished || !postReq.IsNotDeleted {
		t.Fatalf("post request = %+v, want published and not deleted filters", postReq)
	}
}

func TestGetArticleSitemapItemsSupportsCustomPathPrefixAndLimit(t *testing.T) {
	postService := &fakeContentManagerPostService{
		resps: []*post.GetArticlesResponse{
			{
				GetPostsResponse: &post.GetPostsResponse{
					TotalPages: 1,
					Posts: []post.Post{
						{
							Id:            "post-1",
							UrlFriendlyId: "article-first-post",
							Type:          post.PostTypeArticle,
							PublishedAt:   "2026-05-28T08:00:00",
						},
					},
				},
			},
		},
	}

	response, err := getArticleSitemapItems(context.Background(), postService, &GetArticleSitemapItemsRequest{
		Limit:         1,
		URIPathPrefix: "/journal",
	})
	if err != nil {
		t.Fatalf("getArticleSitemapItems() error = %v", err)
	}

	if response.Items[0].URI != "/journal/first-post" {
		t.Fatalf("URI = %q, want custom route prefix", response.Items[0].URI)
	}
	if got := postService.reqs[0].PerPage; got != 1 {
		t.Fatalf("PerPage = %d, want capped by limit", got)
	}
}

func TestGetArticleSitemapItemsRejectsInvalidRequest(t *testing.T) {
	postService := &fakeContentManagerPostService{}

	_, err := getArticleSitemapItems(context.Background(), postService, &GetArticleSitemapItemsRequest{Limit: -1})
	if !errors.Is(err, post.ErrPostBadRequest) {
		t.Fatalf("getArticleSitemapItems() error = %v, want ErrPostBadRequest", err)
	}
}

func TestMapRequestToGetArticleSitemapItemsRequestReadsQuery(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/seo/posts/articles/sitemap-items?created_at_from=2026-05-28T08:00:00&limit=10&uri_path_prefix=/journal", nil)

	parsedRequest, err := mapRequestToGetArticleSitemapItemsRequest(request, noopContentManagerValidator{})
	if err != nil {
		t.Fatalf("mapRequestToGetArticleSitemapItemsRequest() error = %v", err)
	}

	if parsedRequest.CreatedAtFrom != "2026-05-28T08:00:00" || parsedRequest.Limit != 10 || parsedRequest.URIPathPrefix != "/journal" {
		t.Fatalf("parsed request = %+v, want query values", parsedRequest)
	}
}

func TestMapRequestToGetArticleSitemapItemsRequestReadsBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/seo/posts/articles/sitemap-items", strings.NewReader(`{"limit":5,"uri_path_prefix":"/blog"}`))

	parsedRequest, err := mapRequestToGetArticleSitemapItemsRequest(request, noopContentManagerValidator{})
	if err != nil {
		t.Fatalf("mapRequestToGetArticleSitemapItemsRequest() error = %v", err)
	}

	if !reflect.DeepEqual(parsedRequest, &GetArticleSitemapItemsRequest{Limit: 5, URIPathPrefix: "/blog"}) {
		t.Fatalf("parsed request = %+v, want body values", parsedRequest)
	}
}
