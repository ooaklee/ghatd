package post

// Error keys
const (

	// ErrKeyInvalidPostPayload is the error key for when the Post payload is invalid
	ErrKeyInvalidPostPayload = "InvalidPostPayload"

	// ErrKeyHeaderImageMissing is the error key for when a change/ creation request is made
	// but no header image in provided when needed for content type
	ErrKeyHeaderImageMissing = "HeaderImageMissing"

	// ErrKeyRequiredPostTitleIsMissing is the error key for when a post is missing a title
	ErrKeyRequiredPostTitleIsMissing = "RequiredPostTitleIsMissing"

	// ErrKeyRequiredPostTextIsMissing is the error key for when a post is missing its text
	// body
	ErrKeyRequiredPostTextIsMissing = "RequiredPostTextIsMissing"

	// ErrKeyUserIdMustBeProvided is the error key returned when a user id cannot be obtained
	// with the request
	ErrKeyUserIdMustBeProvided = "UserIdMustBeProvided"

	// ErrKeyInvalidPostPublishedAtProvided is the error key returned when the provided published
	// at string fails to parse
	ErrKeyInvalidPostPublishedAtProvided = "InvalidPostPublishedAtProvided"

	// ErrKeyPostAlreadyExistsWithGivenUrlFriendlyId is the error key returned when attempting to
	// save a post that's using a key already used by another post
	ErrKeyPostAlreadyExistsWithGivenUrlFriendlyId = "PostAlreadyExistsWithGivenUrlFriendlyId"

	// ErrKeyIdIsRequired is the error key returned when an operation that requires an Id on a post
	// cannot detect/find one
	ErrKeyIdIsRequired = "IdIsRequired"

	// ErrKeyNanoIdIsRequired is the error key returned when an operation that requires a nano Id on a post
	// cannot detect/find one
	ErrKeyNanoIdIsRequired = "NanoIdIsRequired"

	// ErrKeyUrlFriendlyIdIsRequired is the error key returned when an operation that requires a url friendly id
	// on a post cannot detect/find one
	ErrKeyUrlFriendlyIdIsRequired = "UrlFriendlyIdIsRequired"

	// ErrKeyResourceNotFound is the error key returned when a post resource with given id cannot be found
	ErrKeyResourceNotFound = "ResourceNotFound"

	// ErrKeyPostBadRequest is the error key for when the post payload is invalid
	ErrKeyPostBadRequest = "PostBadRequest"

	// ErrKeyInvalidPostQueryParam is the error key for when an invalid query param is provided
	// for a post related request
	ErrKeyInvalidPostQueryParam = "InvalidPostQueryParam"

	// ErrKeyChangelogPostMustHaveValidTagsSet is the error key for when missing or invalid tags have been
	// passed for creating a changelog item post
	ErrKeyChangelogPostMustHaveValidTagsSet = "ChangelogPostMustHaveValidTagsSet"

	// ErrKeyPostInvalidHeaderImageFailedTypeAssigment is the error key for when a post has an invalid header image
	// type that cannot be assigned to the post
	ErrKeyPostInvalidHeaderImageFailedTypeAssigment = "PostInvalidHeaderImageFailedTypeAssigment"

	// ErrKeyPostInvalidHeaderImageFailedSvgUnmarshal is the error key for when a post has an invalid svg header image
	ErrKeyPostInvalidHeaderImageFailedSvgUnmarshal = "PostInvalidHeaderImageFailedSvgUnmarshal"

	//  ErrKeyPostInvalidHeaderImageMissingRoleImgAttribute  is the error key for when a post has an invalid header image missing the role img attribute
	ErrKeyPostInvalidHeaderImageMissingRoleImgAttribute = "PostInvalidHeaderImageMissingRoleImgAttribute"

	// ErrKeyPostInvalidHeaderImageMissingTitleElement is the error key for when a post has an invalid header image missing the title element
	ErrKeyPostInvalidHeaderImageMissingTitleElement = "PostInvalidHeaderImageMissingTitleElement"

	// ErrKeyPostNotFoundForUpdate is the error key for when a post cannot be found for update
	ErrKeyPostNotFoundForUpdate = "PostNotFoundForUpdate"

	// ErrKeyPostUpdateAttemptOnDeletedPost is the error key for when attempting to update a soft-deleted post
	ErrKeyPostUpdateAttemptOnDeletedPost = "PostUpdateAttemptOnDeletedPost"

	// ErrKeyRequiredPostTypeIsMissing is the error key for when no valid post types are provided
	ErrKeyRequiredPostTypeIsMissing = "RequiredPostTypeIsMissing"

	// ErrKeyPostAlreadySoftDeleted is the error key for when attempting to delete a post that has already been soft deleted
	ErrKeyPostAlreadySoftDeleted = "PostAlreadySoftDeleted"
)

var (
	// DefaultValidPostTags is the default set of valid post tags that can be used for changelog posts
	DefaultValidPostTags = []string{"announcement", "bug-fix", "product-news", "exciting-news"}
)
