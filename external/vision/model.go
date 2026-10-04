package vision

import (
	"slices"
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/external/voter"
)

// Vision represents feedback or a bug report. A non-empty Status makes the
// item part of the roadmap.
type Vision struct {
	ID          string       `json:"id" bson:"_id"`
	NanoID      string       `json:"nano_id" bson:"_nano_id"`
	Title       string       `json:"title" bson:"title"`
	Type        VisionType   `json:"type" bson:"type"`
	Description string       `json:"description,omitempty" bson:"description,omitempty"`
	Status      VisionStatus `json:"status,omitempty" bson:"status,omitempty"`
	// VoteSummary is hydrated from shared storage, never persisted on the vision.
	VoteSummary voter.Summary `json:"votes" bson:"-"`
	// VoteViewerID binds this transient summary to its viewer; never returned publicly.
	VoteViewerID string                 `json:"-" bson:"-"`
	Comments     []VisionComment        `json:"comments,omitempty" bson:"comments,omitempty"`
	CommentCount int                    `json:"comment_count" bson:"comment_count"`
	Metadata     map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`

	CreatedAt       string `json:"created_at" bson:"created_at"`
	CreatedByUserID string `json:"created_by_user_id" bson:"created_by_user_id"`
	UpdatedAt       string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	UpdatedByUserID string `json:"updated_by_user_id,omitempty" bson:"updated_by_user_id,omitempty"`

	config *VisionConfig `json:"-" bson:"-"`
}

// VisionComment is a comment stored directly on a vision item. Message is
// persisted verbatim, including optional <@USER_NANO_ID> mention tokens.
type VisionComment struct {
	ID              string `json:"id" bson:"id"`
	ParentCommentID string `json:"parent_comment_id,omitempty" bson:"parent_comment_id,omitempty"`
	UserID          string `json:"user_id" bson:"user_id"`
	Message         string `json:"message" bson:"message"`
	// VoteSummary is hydrated from shared storage, never persisted on the vision.
	VoteSummary voter.Summary `json:"votes" bson:"-"`
	// VoteViewerID binds this transient summary to its viewer; never returned publicly.
	VoteViewerID string `json:"-" bson:"-"`
	CreatedAt    string `json:"created_at" bson:"created_at"`
}

// NewVision returns a normalised vision with an empty vote summary.
func NewVision(req *CreateVisionRequest, config *VisionConfig) *Vision {
	vision := &Vision{
		Comments:     []VisionComment{},
		CommentCount: 0,
		config:       config,
	}
	if req == nil {
		return vision
	}

	vision.Title = normaliseVisionTitle(req.Title)
	vision.Type = normaliseVisionType(req.Type)
	vision.Description = strings.TrimSpace(req.Description)
	vision.Metadata = req.Metadata
	vision.CreatedByUserID = strings.TrimSpace(req.ActorID)
	return vision
}

// SetConfig injects config after a vision is loaded from persistence.
func (v *Vision) SetConfig(config *VisionConfig) *Vision {
	v.config = config
	return v
}

// GenerateID creates a UUID for the vision.
func (v *Vision) GenerateID() *Vision {
	v.ID = toolbox.GenerateUuidV4()
	return v
}

// GenerateNanoID creates a short public identifier for the vision.
func (v *Vision) GenerateNanoID() *Vision {
	v.NanoID = toolbox.GenerateNanoId()
	return v
}

// SetCreatedAtTimeToNow sets the created timestamp to platform UTC format.
func (v *Vision) SetCreatedAtTimeToNow() *Vision {
	v.CreatedAt = toolbox.TimeNowUTC()
	return v
}

// SetUpdatedAtTimeToNow sets the updated timestamp to platform UTC format.
func (v *Vision) SetUpdatedAtTimeToNow() *Vision {
	v.UpdatedAt = toolbox.TimeNowUTC()
	return v
}

// IsRoadmapItem reports whether the item has been assigned a roadmap status.
func (v *Vision) IsRoadmapItem() bool {
	return v != nil && normaliseVisionStatus(v.Status) != ""
}

// UpdateStatus validates and applies a configured status transition.
func (v *Vision) UpdateStatus(desired VisionStatus) error {
	if v == nil || v.config == nil {
		return ErrVisionConfigNotSet
	}

	desired = normaliseVisionStatus(desired)
	allowedSources, exists := v.config.StatusTransitions[desired]
	if !exists {
		return ErrVisionInvalidStatus
	}

	current := normaliseVisionStatus(v.Status)
	if current == desired {
		return nil
	}
	if !slices.Contains(allowedSources, current) {
		return ErrVisionInvalidStatusTransition
	}

	v.Status = desired
	v.SetUpdatedAtTimeToNow()
	return nil
}

// NewVisionComment returns a comment with generated identity and timestamp.
func NewVisionComment(userID, message, parentCommentID string) *VisionComment {
	return &VisionComment{
		ID:              toolbox.GenerateUuidV4(),
		ParentCommentID: strings.TrimSpace(parentCommentID),
		UserID:          strings.TrimSpace(userID),
		Message:         strings.TrimSpace(message),
		CreatedAt:       toolbox.TimeNowUTC(),
	}
}

// normaliseVisionTitle trims whitespace from a vision title.
func normaliseVisionTitle(value string) string {
	return strings.TrimSpace(value)
}

// normaliseVisionType lowercases and trims a vision type.
func normaliseVisionType(value VisionType) VisionType {
	return VisionType(toolbox.StringStandardisedToLower(strings.TrimSpace(string(value))))
}

// normaliseVisionStatus uppercases and trims a vision status.
func normaliseVisionStatus(value VisionStatus) VisionStatus {
	return VisionStatus(toolbox.StringStandardisedToUpper(strings.TrimSpace(string(value))))
}

// isValidVisionVote reports whether the value is a recognised vote type.
func isValidVisionVote(value VisionVote) bool {
	return value == VisionVoteDownvote || value == VisionVoteUpvote
}
