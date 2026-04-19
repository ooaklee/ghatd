package contentmanager

import "github.com/ooaklee/ghatd/external/post"

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

// GetLatestPostsByTypeRequest represents the request payload for getting
// the latest posts by type
type GetLatestPostsByTypeRequest struct {
	// UserId is the user making the request
	UserId string

	*post.GetLatestPostsByTypeRequest
}
