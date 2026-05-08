package post

import "github.com/ooaklee/reply/v2"

// PostErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var PostErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrInvalidPostPayload:                            {Title: "Bad Request", Detail: "Invalid post payload", StatusCode: 400, Code: "CNT0-01"},
	ErrRequiredPostTitleIsMissing:                    {Title: "Bad Request", Detail: "Required post title is missing", StatusCode: 400, Code: "CNT0-02"},
	ErrHeaderImageMissing:                            {Title: "Bad Request", Detail: "Header image is missing", StatusCode: 400, Code: "CNT0-03"},
	ErrRequiredPostTextIsMissing:                     {Title: "Bad Request", Detail: "Required post text is missing", StatusCode: 400, Code: "CNT0-04"},
	ErrUserIdMustBeProvided:                          {Title: "Forbidden", Detail: "Who are you? You do not have permission to carry out this action", StatusCode: 403, Code: "CNT0-05"},
	ErrInvalidPostPublishedAtProvided:                {Title: "Bad Request", Detail: "Invalid post published at provided", StatusCode: 400, Code: "CNT0-06"},
	ErrPostAlreadyExistsWithGivenUrlFriendlyId:       {Title: "Conflict", Detail: "Post already exists with given url friendly id", StatusCode: 409, Code: "CNT0-07"},
	ErrIdIsRequired:                                  {Title: "Bad Request", Detail: "Post Id is required", StatusCode: 400, Code: "CNT0-08"},
	ErrNanoIdIsRequired:                              {Title: "Bad Request", Detail: "Post Nano Id is required", StatusCode: 400, Code: "CNT0-09"},
	ErrResourceNotFound:                              {Title: "Not Found", Detail: "Post not found", StatusCode: 404, Code: "CNT0-10"},
	ErrPostBadRequest:                                {Title: "Bad Request", Detail: "Post fails validation", StatusCode: 400, Code: "CNT0-11"},
	ErrUrlFriendlyIdIsRequired:                       {Title: "Bad Request", Detail: "Post Url Friendly Id is required", StatusCode: 400, Code: "CNT0-12"},
	ErrInvalidPostQueryParam:                         {Title: "Bad Request", Detail: "Invalid post query param", StatusCode: 400, Code: "CNT0-13"},
	ErrChangelogPostMustHaveValidTagsSet:             {Title: "Bad Request", Detail: "Changelog post must have valid tags set/ provided", StatusCode: 400, Code: "CNT0-14"},
	ErrPostInvalidHeaderImageFailedTypeAssigment:     {Title: "Bad Request", Detail: "Post has invalid header image that cannot be assigned a supported type", StatusCode: 400, Code: "CNT0-15"},
	ErrPostInvalidHeaderImageFailedSvgUnmarshal:      {Title: "Bad Request", Detail: "Post has invalid svg header image", StatusCode: 400, Code: "CNT0-16"},
	ErrPostInvalidHeaderImageMissingRoleImgAttribute: {Title: "Bad Request", Detail: "Post has invalid header svg image missing the role img attribute", StatusCode: 400, Code: "CNT0-17"},
	ErrPostInvalidHeaderImageMissingTitleElement:     {Title: "Bad Request", Detail: "Post has invalid header svg image missing the title element", StatusCode: 400, Code: "CNT0-18"},
	ErrPostNotFoundForUpdate:                         {Title: "Not Found", Detail: "Post not found for update", StatusCode: 404, Code: "CNT0-19"},
	ErrPostUpdateAttemptOnDeletedPost:                {Title: "Bad Request", Detail: "Cannot update a deleted post", StatusCode: 400, Code: "CNT0-20"},
	ErrPostAlreadySoftDeleted:                        {Title: "Conflict", Detail: "Provided post is already soft-deleted", StatusCode: 409, Code: "CNT0-21"},
}
