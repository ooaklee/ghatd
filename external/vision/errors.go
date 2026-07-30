package vision

import "errors"

var (
	ErrVisionCommentNotFound          = errors.New(ErrMessageVisionCommentNotFound)
	ErrVisionCommentMessageIsRequired = errors.New(ErrMessageVisionCommentMessageIsRequired)
	ErrVisionConfigInvalid            = errors.New(ErrMessageVisionConfigInvalid)
	ErrVisionConfigNotSet             = errors.New(ErrMessageVisionConfigNotSet)
	ErrVisionDatabaseError            = errors.New(ErrMessageVisionDatabaseError)
	ErrVisionDownvotingDisabled       = errors.New(ErrMessageVisionDownvotingDisabled)
	ErrVisionError                    = errors.New(ErrMessageVisionError)
	ErrVisionNanoIDIsRequired         = errors.New(ErrMessageVisionNanoIDIsRequired)
	ErrVisionInvalidPayload           = errors.New(ErrMessageVisionInvalidPayload)
	ErrVisionInvalidQueryParam        = errors.New(ErrMessageVisionInvalidQueryParam)
	ErrVisionInvalidStatus            = errors.New(ErrMessageVisionInvalidStatus)
	ErrVisionInvalidStatusTransition  = errors.New(ErrMessageVisionInvalidStatusTransition)
	ErrVisionInvalidType              = errors.New(ErrMessageVisionInvalidType)
	ErrVisionInvalidVote              = errors.New(ErrMessageVisionInvalidVote)
	ErrVisionResourceNotFound         = errors.New(ErrMessageVisionResourceNotFound)
	ErrVisionTitleIsRequired          = errors.New(ErrMessageVisionTitleIsRequired)
	ErrVisionUserIDIsRequired         = errors.New(ErrMessageVisionUserIDIsRequired)
)
