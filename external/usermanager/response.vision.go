package usermanager

import "github.com/ooaklee/ghatd/external/vision"

// VisionVoteSummary exposes aggregate sentiment without exposing voter IDs.
type VisionVoteSummary struct {
	Upvotes   int `json:"upvotes"`
	Downvotes int `json:"downvotes"`
	Score     int `json:"score"`
}

// VisionCommentView is the privacy-safe public comment representation.
type VisionCommentView struct {
	ID              string             `json:"id"`
	ParentCommentID string             `json:"parent_comment_id,omitempty"`
	UserNanoID      string             `json:"user_nano_id,omitempty"`
	Message         string             `json:"message"`
	Votes           VisionVoteSummary  `json:"votes"`
	ViewerVote      *vision.VisionVote `json:"viewer_vote,omitempty"`
	CreatedAt       string             `json:"created_at"`
}

// VisionView is the privacy-safe UMS representation of feedback and roadmap
// items. Raw user IDs, voter buckets, and internal metadata stay in vision.
type VisionView struct {
	NanoID              string              `json:"nano_id"`
	Title               string              `json:"title"`
	Type                vision.VisionType   `json:"type"`
	Description         string              `json:"description,omitempty"`
	Status              vision.VisionStatus `json:"status,omitempty"`
	Votes               VisionVoteSummary   `json:"votes"`
	ViewerVote          *vision.VisionVote  `json:"viewer_vote,omitempty"`
	Comments            []VisionCommentView `json:"comments,omitempty"`
	CommentCount        int                 `json:"comment_count"`
	CreatedAt           string              `json:"created_at"`
	CreatedByUserNanoID string              `json:"created_by_user_nano_id,omitempty"`
	UpdatedAt           string              `json:"updated_at,omitempty"`
	UpdatedByUserNanoID string              `json:"updated_by_user_nano_id,omitempty"`
	CanEdit             bool                `json:"can_edit"`
	CanDelete           bool                `json:"can_delete"`
}

// GetVisionResponse combines a safe vision projection with user summaries
// keyed by public NanoID.
type GetVisionResponse struct {
	Vision           *VisionView           `json:"vision"`
	Users            map[string]VisionUser `json:"users,omitempty"`
	ViewerUserNanoID string                `json:"viewer_user_nano_id,omitempty"`
}

// GetVisionsResponse combines safe vision summaries with public user
// summaries. Comments are omitted from the underlying list query.
type GetVisionsResponse struct {
	Visions []VisionView          `json:"visions"`
	Users   map[string]VisionUser `json:"users,omitempty"`
	Total   int64                 `json:"total"`
}

// GetMetaData returns pagination metadata suitable for reply.WithMeta.
func (r *GetVisionsResponse) GetMetaData() map[string]interface{} {
	return map[string]interface{}{"total_resources": r.Total}
}
