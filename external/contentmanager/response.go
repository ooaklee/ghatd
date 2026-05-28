package contentmanager

import (
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/post"
)

// CreatePostResponse represents the response payload for the CreatePost method
type CreatePostResponse struct {
	*post.CreatePostResponse
}

// UpdatePostByIdResponse represents the response payload for the UpdatePostById method
type UpdatePostByIdResponse struct {
	*post.UpdatePostResponse
}

// DeletePostByIdResponse represents the response payload for the DeletePostById method
type DeletePostByIdResponse struct {
	*post.DeletePostByIdResponse
}

// RestorePostByIdResponse represents the response payload for the RestorePostById method
type RestorePostByIdResponse struct {
	*post.RestorePostByIdResponse
}

// GetChangelogItemsResponse represents the response payload for changelog items
// that match respecive queries
type GetChangelogItemsResponse struct {
	*post.GetChangelogItemsResponse
}

// GetGlossaryItemsResponse represents the response payload for glossary items
// that match respecive queries
type GetGlossaryItemsResponse struct {
	*post.GetGlossaryItemsResponse
}

// GetFaqItemsResponse represents the response payload for faq items
// that match respecive queries
type GetFaqItemsResponse struct {
	*post.GetFaqItemsResponse
}

// GetArticlesResponse represents the response payload for articles
// that match respecive queries
type GetArticlesResponse struct {
	*post.GetArticlesResponse
}

// ArticleSitemapItem describes one public article URL ready for sitemap storage.
type ArticleSitemapItem struct {
	// PostID is the source article post identifier.
	PostID string `json:"post_id,omitempty"`

	// URLFriendlyID is the internal post URL-friendly identifier.
	URLFriendlyID string `json:"url_friendly_id,omitempty"`

	// PublicSlug is the public blog slug after removing the article prefix.
	PublicSlug string `json:"public_slug,omitempty"`

	// URI is the site-relative public sitemap URI.
	URI string `json:"uri"`

	// LastMod is the timestamp to use for the sitemap lastmod value.
	LastMod string `json:"last_mod,omitempty"`

	// PublishedAt is the source article publication timestamp.
	PublishedAt string `json:"published_at,omitempty"`

	// UpdatedAt is the source article update timestamp.
	UpdatedAt string `json:"updated_at,omitempty"`
}

// GetArticleSitemapItemsResponse describes sitemap entries built from articles.
type GetArticleSitemapItemsResponse struct {
	// Items is the collection of sitemap-ready article URL entries.
	Items []ArticleSitemapItem `json:"items"`

	// ProcessedArticles is the total number of article posts inspected.
	ProcessedArticles int `json:"processed_articles"`

	// SeedableArticles is the number of article posts with sitemap-ready URLs.
	SeedableArticles int `json:"seedable_articles"`

	// SkippedMissingURLFriendlyID is the number of article posts skipped for missing slugs.
	SkippedMissingURLFriendlyID int `json:"skipped_missing_url_friendly_id"`
}

// GetLatestPostsByTypeResponse represents the response payload for getting
// the latest posts by type
type GetLatestPostsByTypeResponse struct {
	*post.GetLatestPostsByTypeResponse
}

// GetLatestNotificationOverviewsResponse represents the response payload for getting
// the latest notification overviews for the user
type GetLatestNotificationOverviewsResponse struct {
	*common.GetLatestNotificationOverviewsResponse
}
