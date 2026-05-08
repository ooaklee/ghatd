package post

import "errors"

var (
	ErrChangelogPostMustHaveValidTagsSet             = errors.New(ErrKeyChangelogPostMustHaveValidTagsSet)
	ErrHeaderImageMissing                            = errors.New(ErrKeyHeaderImageMissing)
	ErrIdIsRequired                                  = errors.New(ErrKeyIdIsRequired)
	ErrInvalidPostPayload                            = errors.New(ErrKeyInvalidPostPayload)
	ErrInvalidPostPublishedAtProvided                = errors.New(ErrKeyInvalidPostPublishedAtProvided)
	ErrInvalidPostQueryParam                         = errors.New(ErrKeyInvalidPostQueryParam)
	ErrNanoIdIsRequired                              = errors.New(ErrKeyNanoIdIsRequired)
	ErrPostAlreadyExistsWithGivenUrlFriendlyId       = errors.New(ErrKeyPostAlreadyExistsWithGivenUrlFriendlyId)
	ErrPostAlreadySoftDeleted                        = errors.New(ErrKeyPostAlreadySoftDeleted)
	ErrPostBadRequest                                = errors.New(ErrKeyPostBadRequest)
	ErrPostInvalidHeaderImageFailedSvgUnmarshal      = errors.New(ErrKeyPostInvalidHeaderImageFailedSvgUnmarshal)
	ErrPostInvalidHeaderImageFailedTypeAssigment     = errors.New(ErrKeyPostInvalidHeaderImageFailedTypeAssigment)
	ErrPostInvalidHeaderImageMissingRoleImgAttribute = errors.New(ErrKeyPostInvalidHeaderImageMissingRoleImgAttribute)
	ErrPostInvalidHeaderImageMissingTitleElement     = errors.New(ErrKeyPostInvalidHeaderImageMissingTitleElement)
	ErrPostNotFoundForUpdate                         = errors.New(ErrKeyPostNotFoundForUpdate)
	ErrPostUpdateAttemptOnDeletedPost                = errors.New(ErrKeyPostUpdateAttemptOnDeletedPost)
	ErrRequiredPostTextIsMissing                     = errors.New(ErrKeyRequiredPostTextIsMissing)
	ErrRequiredPostTitleIsMissing                    = errors.New(ErrKeyRequiredPostTitleIsMissing)
	ErrRequiredPostTypeIsMissing                     = errors.New(ErrKeyRequiredPostTypeIsMissing)
	ErrResourceNotFound                              = errors.New(ErrKeyResourceNotFound)
	ErrUrlFriendlyIdIsRequired                       = errors.New(ErrKeyUrlFriendlyIdIsRequired)
	ErrUserIdMustBeProvided                          = errors.New(ErrKeyUserIdMustBeProvided)
)
