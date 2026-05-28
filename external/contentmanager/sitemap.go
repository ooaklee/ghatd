package contentmanager

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/post"
)

const (
	// defaultArticleSitemapPerPage is the fallback post page size for article sitemap generation.
	defaultArticleSitemapPerPage = 100

	// defaultArticleSitemapURIPathPrefix is the public route prefix for article sitemap entries.
	defaultArticleSitemapURIPathPrefix = "/blog/"
)

// GetArticleSitemapItems returns seedable sitemap entries for published article posts.
func (s *Service) GetArticleSitemapItems(ctx context.Context, req *GetArticleSitemapItemsRequest) (*GetArticleSitemapItemsResponse, error) {
	return getArticleSitemapItems(ctx, s.postService, req)
}

// getArticleSitemapItems builds sitemap-ready URLs from published article posts.
func getArticleSitemapItems(ctx context.Context, postService postService, req *GetArticleSitemapItemsRequest) (*GetArticleSitemapItemsResponse, error) {
	if req == nil || postService == nil {
		return nil, post.ErrPostBadRequest
	}
	if req.Limit < 0 {
		return nil, post.ErrPostBadRequest
	}

	response := &GetArticleSitemapItemsResponse{
		Items: make([]ArticleSitemapItem, 0),
	}

	for page := 1; ; page++ {
		perPage := defaultArticleSitemapPerPage
		if req.PerPage > 0 && req.PerPage < perPage {
			perPage = req.PerPage
		}
		if req.Limit > 0 {
			remaining := req.Limit - response.ProcessedArticles
			if remaining <= 0 {
				break
			}
			if remaining < perPage {
				perPage = remaining
			}
		}

		articlesResponse, err := postService.GetArticles(ctx, &post.GetArticlesRequest{
			GetPostsRequest: &post.GetPostsRequest{
				Order:         "created_at_asc",
				PerPage:       perPage,
				Page:          page,
				CreatedAtFrom: strings.TrimSpace(req.CreatedAtFrom),
				IsPublished:   true,
				IsNotDeleted:  true,
			},
		})
		if err != nil {
			return nil, err
		}
		if articlesResponse == nil || articlesResponse.GetPostsResponse == nil || len(articlesResponse.Posts) == 0 {
			break
		}

		response.ProcessedArticles += len(articlesResponse.Posts)
		for _, articlePost := range articlesResponse.Posts {
			publicHref := articlePost.ToOverview().Href
			publicSlug := strings.Trim(strings.TrimPrefix(publicHref, defaultArticleSitemapURIPathPrefix), "/")
			if publicHref == "" || publicSlug == "" {
				response.SkippedMissingURLFriendlyID++
				continue
			}

			lastMod := strings.TrimSpace(articlePost.UpdatedAt)
			if lastMod == "" {
				lastMod = strings.TrimSpace(articlePost.PublishedAt)
			}
			if lastMod == "" {
				lastMod = strings.TrimSpace(articlePost.CreatedAt)
			}

			response.Items = append(response.Items, ArticleSitemapItem{
				PostID:        articlePost.Id,
				URLFriendlyID: articlePost.UrlFriendlyId,
				PublicSlug:    publicSlug,
				URI:           normaliseArticleSitemapUrlWithPathPrefix(req.URIPathPrefix, publicHref),
				LastMod:       lastMod,
				PublishedAt:   articlePost.PublishedAt,
				UpdatedAt:     articlePost.UpdatedAt,
			})
		}

		if articlesResponse.TotalPages == 0 || page >= articlesResponse.TotalPages {
			break
		}
	}

	response.SeedableArticles = len(response.Items)
	return response, nil
}

// normaliseArticleSitemapUrlWithPathPrefix returns a site-relative article URI
// by applying a requested public path prefix to a post overview href.
func normaliseArticleSitemapUrlWithPathPrefix(pathPrefix, publicHref string) string {
	publicHref = strings.TrimSpace(publicHref)
	if publicHref == "" {
		return ""
	}

	pathPrefix = strings.TrimSpace(pathPrefix)
	if pathPrefix == "" {
		pathPrefix = defaultArticleSitemapURIPathPrefix
	}
	pathPrefix = "/" + strings.Trim(pathPrefix, "/") + "/"

	slug := strings.TrimPrefix(publicHref, defaultArticleSitemapURIPathPrefix)
	return pathPrefix + strings.Trim(slug, "/")
}
