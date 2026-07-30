package vision

// VisionType identifies the board/category a vision item belongs to.
type VisionType string

const (
	// VisionTypeBugs identifies bug reports.
	VisionTypeBugs VisionType = "bugs"

	// VisionTypeFeedback identifies product feedback and feature requests.
	VisionTypeFeedback VisionType = "feedback"
)

// VisionStatus identifies a roadmap stage. An empty status means the item has
// not yet been selected for the roadmap.
type VisionStatus string

const (
	VisionStatusUnderReview VisionStatus = "UNDER_REVIEW"
	VisionStatusPlanning    VisionStatus = "PLANNING"
	VisionStatusPlanned     VisionStatus = "PLANNED"
	VisionStatusRejected    VisionStatus = "REJECTED"
	VisionStatusInProgress  VisionStatus = "IN_PROGRESS"
	VisionStatusComplete    VisionStatus = "COMPLETE"
)

// VisionVote is the numeric vote bucket stored on a vision.
type VisionVote int

const (
	VisionVoteDownvote VisionVote = 0
	VisionVoteUpvote   VisionVote = 1
)

const (
	// VisionURIVariableNanoID is the URI variable used to identify a vision.
	VisionURIVariableNanoID = "visionNanoID"

	// VisionURIVariableCommentID identifies a comment nested below a vision.
	VisionURIVariableCommentID = "commentID"

	// VisionCollection is the Mongo collection name for vision records.
	VisionCollection = "visions"
)

const (
	ErrMessageVisionCommentNotFound          = "vision-comment-not-found"
	ErrMessageVisionCommentMessageIsRequired = "vision-comment-message-is-required"
	ErrMessageVisionConfigInvalid            = "vision-config-is-invalid"
	ErrMessageVisionConfigNotSet             = "vision-config-is-not-set"
	ErrMessageVisionDatabaseError            = "vision-database-operation-failed"
	ErrMessageVisionDownvotingDisabled       = "vision-downvoting-is-disabled"
	ErrMessageVisionError                    = "vision-request-is-invalid"
	ErrMessageVisionNanoIDIsRequired         = "vision-nano-id-is-required"
	ErrMessageVisionInvalidPayload           = "vision-payload-is-invalid"
	ErrMessageVisionInvalidQueryParam        = "vision-query-param-is-invalid"
	ErrMessageVisionInvalidStatus            = "vision-status-is-invalid"
	ErrMessageVisionInvalidStatusTransition  = "vision-status-transition-is-invalid"
	ErrMessageVisionInvalidType              = "vision-type-is-invalid"
	ErrMessageVisionInvalidVote              = "vision-vote-is-invalid"
	ErrMessageVisionResourceNotFound         = "vision-resource-not-found"
	ErrMessageVisionTitleIsRequired          = "vision-title-is-required"
	ErrMessageVisionUserIDIsRequired         = "vision-user-id-is-required"
)
