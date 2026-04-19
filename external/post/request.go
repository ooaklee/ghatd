package post

// RestorePostByIdRequest represents the request to restore a soft deleted post by its ID
type RestorePostByIdRequest struct {
	// Id is the ID of the post to restore
	Id string `validate:"required,uuid"`

	// UserId is the ID of the user performing the restoration
	UserId string `validate:"required,uuid"`
}

// DeletePostByIdRequest represents the request payload for deleting a post by its ID
type DeletePostByIdRequest struct {

	// Id is the ID of the post to delete
	Id string `validate:"required,uuid"`

	// UserId is the ID of the user performing the deletion
	UserId string `validate:"required,uuid"`

	// HardDelete indicates whether to perform a hard delete
	// if true, or a soft delete if false
	HardDelete bool `query:"hard_delete"`
}

// GetTotalPostsRequest holds everything needed to make
// the request to get the total count of post from repository
type GetTotalPostsRequest struct {

	// Title is the title to filter by
	Title string

	// PublishedWithAlias is in to filter by the posts that have a publisher alias aligned
	PublishedWithAlias bool

	// PublishedWithoutAlias is in to filter by the posts that do not have a publisher alias aligned
	PublishedWithoutAlias bool

	// CreatedByIds is the list of user id that created a post to filter by
	CreatedByIds []string

	// UrlFriendlyIds is the list of url friendly post ids to filter by
	UrlFriendlyIds []string

	// PostTypes is the list of post types to filter by
	PostTypes []PostType

	// Tags is the list of tags to filter by
	Tags []string

	// WithoutTags is the list of tags to filter out
	WithoutTags []string

	// PostTextFormats is the list of text formats to filter by
	PostTextFormats []TextFormat

	// TextContains is the text to filter by
	TextContains string

	// PostHeaderImageTypes is the list of header image type to filter by
	PostHeaderImageTypes []HeaderImageType

	// PublishedAs is the list of published as aliases to filter by
	PublishedAs []string

	// CreatedAtFrom is to filter by the date to which the post was created from
	CreatedAtFrom string

	// CreatedAtTo is to filter by the date to which the post was created up to
	CreatedAtTo string

	// PublishedAtFrom is to filter by the date which the post was published from
	PublishedAtFrom string

	// PublishedAtTo is to filter by the date which the post was published up to
	PublishedAtTo string

	// DeletedAtFrom is to filter by the date which the post was soft deleted from
	DeletedAtFrom string

	// DeletedAtTo is to filter by the date which the post was soft deleted up to
	DeletedAtTo string

	// IsDeleted is to filter by posts that are soft deleted
	IsDeleted bool

	// IsNotDeleted is to filter by posts that are not soft deleted
	IsNotDeleted bool

	// IsPublished is to filter by posts that have a published date
	IsPublished bool

	// IsNotPublished is to filter by posts that do not have a published date
	IsNotPublished bool
}

// GetPostsRequest holds everything needed to make
// the request to get post
type GetPostsRequest struct {

	// Order defines how should response be sorted. Default: newest -> oldest (created_at_desc)
	// Valid options: created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, deleted_at_asc, deleted_at_desc
	// puvlished_at_asc, puvlished_at_desc,
	Order string `query:"order"`

	// Total number of post to return per page, if available. Default 25.
	// Accepts anything between 1 and 100
	PerPage int `query:"per_page"`

	// Page specifies the page results should be taken from. Default 1.
	Page int `query:"page"`

	// TotalCount specifies the total count of all post
	TotalCount int

	// TotalPages specifies the total pages of results
	TotalPages int

	// Meta whether response should contain meta information
	Meta bool `query:"meta"`

	// Title filters for post with the provided title
	Title string `query:"title"`

	// PublishedWithAlias filters for post with a published alias
	PublishedWithAlias bool `query:"published_with_alias"`

	// PublishedWithoutAlias filters for post without a published alias
	PublishedWithoutAlias bool `query:"published_without_alias"`

	// CreatedByIds filters for post from the provided ids
	// comma-separated list of ids
	CreatedByIds string `query:"created_by_ids"`

	// UrlFriendlyIds filters for posts from the provided url friendly ids
	// comma-separated list of ids
	UrlFriendlyIds string `query:"url_friendly_ids"`

	// WithTypes filters for post with the provided types
	// comma-separated list of types
	WithTypes string `query:"with_types"`

	// WithTags filters for post with the provided tags
	// comma-separated list of tags
	WithTags string `query:"with_tags"`

	// WithoutTags filters for post without the provided tags
	// comma-separated list of tags
	WithoutTags string `query:"without_tags"`

	// WithTextFormats filters for post with the provided text format
	// comma-separated list of text formats
	WithTextFormats string `query:"with_text_formats"`

	// TextContains filters for post with the provided text
	TextContains string `query:"text_contains"`

	// WithHeaderImageType filters for post with the provided header image
	// comma-separated list of text formats
	WithHeaderImageType string `query:"with_header_image_type"`

	// PublishedAs filters for post with the provided provided as publisher
	// comma-separated list of publish as aliases
	PublishedAs string `query:"published_as"`

	// CreatedAtFrom filters for post created at from the provided date
	CreatedAtFrom string `query:"created_at_from"`

	// CreatedAtTo filters for post created at up to the provided date
	CreatedAtTo string `query:"created_at_to"`

	// PublishedAtFrom filters for post published at from the provided date
	PublishedAtFrom string `query:"published_at_from"`

	// PublishedAtTo filters for post published at up to the provided date
	PublishedAtTo string `query:"published_at_to"`

	// DeletedAtFrom filters for post deleted at from the provided date
	DeletedAtFrom string `query:"deleted_at_from"`

	// DeletedAtTo filters for post deleted at up to the provided date
	DeletedAtTo string `query:"deleted_at_to"`

	// IsDeleted filters for post that are deleted
	IsDeleted bool `query:"is_deleted"`

	// IsNotDeleted filters for post that are not deleted
	IsNotDeleted bool `query:"is_not_deleted"`

	// IsPublished filters for post that are published
	IsPublished bool `query:"is_published"`

	// IsNotPublished filters for post that are not published
	IsNotPublished bool `query:"is_not_published"`

	// EnforceUniqueType when true, ensures only one post per type is returned
	// When combined with Limit, it limits the total across all types
	EnforceUniqueType bool `query:"enforce_unique_type"`
}

// GetMetaData returns a map of metadata about the GetPostsRequest, including the
// number of resources per page, the total number of resources, the total
// number of pages, and the current page.
func (g *GetPostsRequest) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap["resources_per_page"] = g.PerPage
	responseMap["total_resources"] = g.TotalCount
	responseMap["total_pages"] = g.TotalPages
	responseMap["page"] = g.Page

	return responseMap
}

// CreatePostRequest holds everything needed to make
// the request to create a post
//
//		{
//			"title": "The state of Blogs in 2025",
//			"type": "article",
//			"text": "# Hello\nI love what you've done with this",
//			"text_format": "markdown",
//	        "publish_as": "tc"
//		  }
type CreatePostRequest struct {

	// UserId is the ID of the user making requests to
	// create the post on the platform
	UserId string

	// HeaderImage is the URL or the SVG of the header
	// image for the post
	HeaderImage string `json:"header_image,omitempty"`

	// Title is the title of the post
	Title string `json:"title" validate:"required"`

	// Type is the type of the post
	Type PostType `json:"type" validate:"required"`

	// Tags is the tags to apply to the post
	Tags []string `json:"tags,omitempty"`

	// Text is the body of the post
	Text string `json:"text" validate:"required"`

	// TextFormat is the format the body of the post is written in
	TextFormat string `json:"text_format,omitempty"`

	// PublishAs is the alias that should be used to
	// publish the post
	PublishAs string `json:"publish_as,omitempty"`

	// PublishNow is whether the post should be published immediately
	PublishNow bool `json:"publish_now,omitempty"`

	// PublishAtUtc is the target publish at date for the post
	// Should be in the format YYYY-MM-DDThh:mm:ss
	PublishAtUtc string `json:"publish_at_utc,omitempty"`
}

// GetChangelogItemsRequest holds everything needed to get
// changelogs items from repository
type GetChangelogItemsRequest struct {
	*GetPostsRequest
}

// GetGlossaryItemsRequest holds everything needed to get
// glossary items from repository
type GetGlossaryItemsRequest struct {
	*GetPostsRequest
}

// GetFaqItemsRequest holds everything needed to get
// faq items from repository
type GetFaqItemsRequest struct {
	*GetPostsRequest
}

// GetArticlesRequest holds everything needed to get
// articles from repository
type GetArticlesRequest struct {
	*GetPostsRequest
}

// GetLatestPostsByTypeRequest holds everything needed to get
// the latest posts by type
type GetLatestPostsByTypeRequest struct {

	// Types is a comma-separated list of post types to retrieve
	// If empty, defaults to "article,changelog"
	Types string `query:"types"`

	// Limit is the maximum total number of posts to return across all types
	// With EnforceUniqueType, this limits the combined result
	// Default is 5 if not specified
	Limit int `query:"limit"`
}

// UpdatePostRequest holds everything needed to make
// the request to update a post
//
//	{
//		"post_id": "[SOME_UUID]",
//		"title": "Updated Title",
//		"text": "Updated content",
//		"tags": ["updated-tag"]
//	}
type UpdatePostRequest struct {

	// UserId is the ID of the user making the request to
	// update the post on the platform
	UserId string

	// PostId is the ID of the post to update. This is required
	// if Post is not provided
	PostId string

	// Post is the complete post object to update. If provided,
	// this will be used instead of individual fields
	Post *Post `json:"post,omitempty"`

	// Title is the updated title of the post
	Title *string `json:"title,omitempty"`

	// Type is the updated type of the post
	Type *PostType `json:"type,omitempty"`

	// HeaderImage is the updated URL or SVG of the header image
	HeaderImage *string `json:"header_image,omitempty"`

	// Tags are the updated tags to apply to the post
	Tags []string `json:"tags,omitempty"`

	// Text is the updated body of the post
	Text *string `json:"text,omitempty"`

	// TextFormat is the updated format the body of the post is written in
	TextFormat *string `json:"text_format,omitempty"`

	// PublishAs is the updated alias that should be used to publish the post
	PublishAs *string `json:"publish_as,omitempty"`

	// PublishNow is whether the post should be published immediately
	PublishNow *bool `json:"publish_now,omitempty"`

	// PublishAtUtc is the updated target publish at date for the post
	// Should be in the format YYYY-MM-DDThh:mm:ss
	PublishAtUtc *string `json:"publish_at_utc,omitempty"`
}
