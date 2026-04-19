package contentmanager

import "github.com/ooaklee/ghatd/external/post"

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

// GetLatestPostsByTypeResponse represents the response payload for getting
// the latest posts by type
type GetLatestPostsByTypeResponse struct {
	*post.GetLatestPostsByTypeResponse
}
