package vision

// CreateVisionRequest holds fields needed to create feedback or a bug report.
type CreateVisionRequest struct {
	Title           string                 `json:"title" validate:"required"`
	Type            VisionType             `json:"type" validate:"required"`
	Description     string                 `json:"description,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	CreatedByUserID string                 `json:"-" query:"-"`
}

// GetVisionByNanoIDRequest identifies a vision by its public NanoID.
type GetVisionByNanoIDRequest struct {
	NanoID string `path:"visionNanoID" validate:"required"`
}

// GetVisionsRequest holds optional filters and pagination.
type GetVisionsRequest struct {
	Query       string       `query:"query"`
	Type        VisionType   `query:"type"`
	Status      VisionStatus `query:"status"`
	RoadmapOnly bool         `query:"roadmap_only"`
	Page        int64        `query:"page"`
	PageSize    int64        `query:"page_size" validate:"max=100"`
}

// UpdateVisionRequest holds mutable descriptive fields.
type UpdateVisionRequest struct {
	NanoID          string                 `json:"-" validate:"required"`
	Title           string                 `json:"title,omitempty"`
	Description     string                 `json:"description,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	UpdatedByUserID string                 `json:"-"`
}

// UpdateVisionStatusRequest requests a configured roadmap transition.
type UpdateVisionStatusRequest struct {
	NanoID          string       `json:"-" validate:"required"`
	Status          VisionStatus `json:"status" validate:"required"`
	UpdatedByUserID string       `json:"-"`
}

// SetVisionVoteRequest sets or changes the requestor's vote.
type SetVisionVoteRequest struct {
	NanoID string     `json:"-" validate:"required"`
	UserID string     `json:"-"`
	Vote   VisionVote `json:"vote"`
}

// RemoveVisionVoteRequest removes the requestor from both vote buckets.
type RemoveVisionVoteRequest struct {
	NanoID string `json:"-" validate:"required"`
	UserID string `json:"-"`
}

// AddVisionCommentRequest appends a comment to a vision.
type AddVisionCommentRequest struct {
	NanoID          string `json:"-" validate:"required"`
	ParentCommentID string `json:"parent_comment_id,omitempty"`
	UserID          string `json:"-"`
	Message         string `json:"message" validate:"required"`
}

// SetVisionCommentVoteRequest sets or changes the requestor's comment vote.
type SetVisionCommentVoteRequest struct {
	NanoID    string     `json:"-" validate:"required"`
	CommentID string     `json:"-" validate:"required"`
	UserID    string     `json:"-"`
	Vote      VisionVote `json:"vote"`
}

// RemoveVisionCommentVoteRequest removes the requestor from both comment vote buckets.
type RemoveVisionCommentVoteRequest struct {
	NanoID    string `json:"-" validate:"required"`
	CommentID string `json:"-" validate:"required"`
	UserID    string `json:"-"`
}

// DeleteVisionRequest identifies a vision by public NanoID for deletion.
type DeleteVisionRequest struct {
	NanoID string `json:"-" validate:"required"`
}
