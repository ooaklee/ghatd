package post

import "github.com/ooaklee/ghatd/external/toolbox"

// RestorePostByIdResponse represents the response after restoring a soft deleted post
type RestorePostByIdResponse struct {

	// Post is the restored post
	Post *Post
}

// DeletePostByIdResponse represents the response payload for deleting a post by its ID
type DeletePostByIdResponse struct {

	// HardDelete indicates whether a hard delete was performed
	HardDelete bool `json:"hard_delete"`
}

// CreatePostResponse holds everything needed to return
// the response to creating a post
type CreatePostResponse struct {

	// Post is the post that was created
	Post *Post `json:"post"`
}

// GetPostsResponse holds everything needed to return
// the response to get post
type GetPostsResponse struct {
	Posts []Post `json:"posts"`

	// Total number of posts found that matched provided
	// filters
	Total int

	// TotalPages total pages available, based on the provided
	// filters and resources per page
	TotalPages int

	// PerPage number of posts set to be returned per page
	PerPage int

	// Page specifies the page results were taken from. Default 1.
	Page int
}

// GetMetaData returns a map containing metadata about the GetPostsResponse,
// including the number of resources per page, total resources, total pages,
// and the current page.
func (g *GetPostsResponse) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap[string(toolbox.ResponseMetaKeyResourcePerPage)] = g.PerPage
	responseMap[string(toolbox.ResponseMetaKeyTotalResources)] = g.Total
	responseMap[string(toolbox.ResponseMetaKeyTotalPages)] = g.TotalPages
	responseMap[string(toolbox.ResponseMetaKeyPage)] = g.Page

	return responseMap
}

// GetChangelogItemsResponse holds the response for getting the changelog
// items response
type GetChangelogItemsResponse struct {
	*GetPostsResponse
}

func (g *GetChangelogItemsResponse) GetEmbeddedPostsResponse() *GetPostsResponse {
	return g.GetPostsResponse
}

// GetGlossaryItemsResponse holds the response for getting the glossary
// items response
type GetGlossaryItemsResponse struct {
	*GetPostsResponse
}

func (g *GetGlossaryItemsResponse) GetEmbeddedPostsResponse() *GetPostsResponse {
	return g.GetPostsResponse
}

// GetFaqItemsResponse holds the response for getting the faq
// items response
type GetFaqItemsResponse struct {
	*GetPostsResponse
}

func (g *GetFaqItemsResponse) GetEmbeddedPostsResponse() *GetPostsResponse {
	return g.GetPostsResponse
}

// GetArticlesResponse holds the response for getting the articles response
type GetArticlesResponse struct {
	*GetPostsResponse
}

func (g *GetArticlesResponse) GetEmbeddedPostsResponse() *GetPostsResponse {
	return g.GetPostsResponse
}

// GetLatestPostsByTypeResponse holds the response for getting the latest posts by type
type GetLatestPostsByTypeResponse struct {
	// Overviews is the list of latest overview posts
	Overviews []PostOverview `json:"overviews"`
}

// UpdatePostResponse holds everything needed to return
// the response to updating a post
type UpdatePostResponse struct {

	// Post is the post that was updated
	Post *Post `json:"post"`
}
