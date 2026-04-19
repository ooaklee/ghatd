package post

import "github.com/ooaklee/reply"

// PostErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var PostErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrKeyInvalidPostPayload:                            {Title: "Bad Request", Detail: "Invalid post payload", StatusCode: 400, Code: "CNT0-01"},
	ErrKeyRequiredPostTitleIsMissing:                    {Title: "Bad Request", Detail: "Required post title is missing", StatusCode: 400, Code: "CNT0-02"},
	ErrKeyHeaderImageMissing:                            {Title: "Bad Request", Detail: "Header image is missing", StatusCode: 400, Code: "CNT0-03"},
	ErrKeyRequiredPostTextIsMissing:                     {Title: "Bad Request", Detail: "Required post text is missing", StatusCode: 400, Code: "CNT0-04"},
	ErrKeyUserIdMustBeProvided:                          {Title: "Forbidden", Detail: "Who are you? You do not have permission to carry out this action", StatusCode: 403, Code: "CNT0-05"},
	ErrKeyInvalidPostPublishedAtProvided:                {Title: "Bad Request", Detail: "Invalid post published at provided", StatusCode: 400, Code: "CNT0-06"},
	ErrKeyPostAlreadyExistsWithGivenUrlFriendlyId:       {Title: "Conflict", Detail: "Post already exists with given url friendly id", StatusCode: 409, Code: "CNT0-07"},
	ErrKeyIdIsRequired:                                  {Title: "Bad Request", Detail: "Post Id is required", StatusCode: 400, Code: "CNT0-08"},
	ErrKeyNanoIdIsRequired:                              {Title: "Bad Request", Detail: "Post Nano Id is required", StatusCode: 400, Code: "CNT0-09"},
	ErrKeyResourceNotFound:                              {Title: "Not Found", Detail: "Post not found", StatusCode: 404, Code: "CNT0-10"},
	ErrKeyPostBadRequest:                                {Title: "Bad Request", Detail: "Post fails validation", StatusCode: 400, Code: "CNT0-11"},
	ErrKeyUrlFriendlyIdIsRequired:                       {Title: "Bad Request", Detail: "Post Url Friendly Id is required", StatusCode: 400, Code: "CNT0-12"},
	ErrKeyInvalidPostQueryParam:                         {Title: "Bad Request", Detail: "Invalid post query param", StatusCode: 400, Code: "CNT0-13"},
	ErrKeyChangelogPostMustHaveValidTagsSet:             {Title: "Bad Request", Detail: "Changelog post must have valid tags set/ provided", StatusCode: 400, Code: "CNT0-14"},
	ErrKeyPostInvalidHeaderImageFailedTypeAssigment:     {Title: "Bad Request", Detail: "Post has invalid header image that cannot be assigned a supported type", StatusCode: 400, Code: "CNT0-15"},
	ErrKeyPostInvalidHeaderImageFailedSvgUnmarshal:      {Title: "Bad Request", Detail: "Post has invalid svg header image", StatusCode: 400, Code: "CNT0-16"},
	ErrKeyPostInvalidHeaderImageMissingRoleImgAttribute: {Title: "Bad Request", Detail: "Post has invalid header svg image missing the role img attribute", StatusCode: 400, Code: "CNT0-17"},
	ErrKeyPostInvalidHeaderImageMissingTitleElement:     {Title: "Bad Request", Detail: "Post has invalid header svg image missing the title element", StatusCode: 400, Code: "CNT0-18"},
	ErrKeyPostNotFoundForUpdate:                         {Title: "Not Found", Detail: "Post not found for update", StatusCode: 404, Code: "CNT0-19"},
	ErrKeyPostUpdateAttemptOnDeletedPost:                {Title: "Bad Request", Detail: "Cannot update a deleted post", StatusCode: 400, Code: "CNT0-20"},
	ErrKeyPostAlreadySoftDeleted:                        {Title: "Conflict", Detail: "Provided post is already soft-deleted", StatusCode: 409, Code: "CNT0-21"},
}
