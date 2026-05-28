package contentmanager

import (
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/post"
)

// CreatePostRequest represents the request payload for creating a post
// with given attributes
type CreatePostRequest struct {
	*post.CreatePostRequest
}

// UpdatePostByIdRequest represents the request payload for updating a post
// by its ID
type UpdatePostByIdRequest struct {
	*post.UpdatePostRequest
}

// DeletePostByIdRequest represents the request payload for deleting a post
// by its ID
type DeletePostByIdRequest struct {
	*post.DeletePostByIdRequest
}

// RestorePostByIdRequest represents the request payload for restoring a post
// by its ID
type RestorePostByIdRequest struct {
	*post.RestorePostByIdRequest
}

// GetChangelogItemsRequest represents the request payload for getting
// posts of type changelog
type GetChangelogItemsRequest struct {
	// UserId is the user making the request
	UserId string

	*post.GetChangelogItemsRequest
}

// GetChangelogItemByUrlFriendlyIdRequest reprethe request payload for getting
// changelog item by its url friendly id
type GetChangelogItemByUrlFriendlyIdRequest struct {
	// UserId is the user making the request
	UserId string

	// UrlFriendlyId is the url friendly id of the changelog item
	UrlFriendlyId string
}

// GetGlossaryItemsRequest represents the request payload for getting
// posts of type glossary
type GetGlossaryItemsRequest struct {
	// UserId is the user making the request
	UserId string

	*post.GetGlossaryItemsRequest
}

// GetFaqItemsRequest represents the request payload for getting
// posts of type faq
type GetFaqItemsRequest struct {
	// UserId is the user making the request
	UserId string

	*post.GetFaqItemsRequest
}

// GetArticlesRequest represents the request payload for getting
// posts of type article
type GetArticlesRequest struct {
	// UserId is the user making the request
	UserId string

	*post.GetArticlesRequest
}

// GetArticleItemByUrlFriendlyIdRequest represents the request payload for getting
// article item by its url friendly id
type GetArticleItemByUrlFriendlyIdRequest struct {
	// UserId is the user making the request
	UserId string

	// UrlFriendlyId is the url friendly id of the article item
	UrlFriendlyId string
}

// GetArticleSitemapItemsRequest represents the request payload for building
// sitemap entries from published article posts.
type GetArticleSitemapItemsRequest struct {
	// UserId is the user making the request.
	UserId string

	// CreatedAtFrom filters article posts created from the provided timestamp.
	CreatedAtFrom string `json:"created_at_from,omitempty" query:"created_at_from"`

	// Limit optionally caps the number of articles processed in this run.
	Limit int `json:"limit,omitempty" query:"limit"`

	// PerPage optionally controls the post-service page size. Defaults to 100.
	PerPage int `json:"per_page,omitempty" query:"per_page"`

	// URIPathPrefix is the public route prefix used for article sitemap entries.
	URIPathPrefix string `json:"uri_path_prefix,omitempty" query:"uri_path_prefix"`
}

// GetLatestPostsByTypeRequest represents the request payload for getting
// the latest posts by type
type GetLatestPostsByTypeRequest struct {
	// UserId is the user making the request
	UserId string

	*post.GetLatestPostsByTypeRequest
}

// GetLatestNotificationOverviewsRequest represents the request payload for getting
// the latest notification overviews for the user
type GetLatestNotificationOverviewsRequest struct {

	// GetLatestNotificationOverviewsRequest is embedded to allow for future expansion of the request without breaking changes
	*common.GetLatestNotificationOverviewsRequest
}
