package vision

import (
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// Vision is a small demo entity used to show the expected package shape.
type Vision struct {
	ID          string                 `json:"id" bson:"_id"`
	NanoID      string                 `json:"nano_id" bson:"_nano_id"`
	Name        string                 `json:"name" bson:"name"`
	Kind        string                 `json:"kind" bson:"kind"`
	Description string                 `json:"description,omitempty" bson:"description,omitempty"`
	Status      string                 `json:"status" bson:"status"`
	Metadata    map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`

	CreatedAt       string `json:"created_at" bson:"created_at"`
	CreatedByUserID string `json:"created_by_user_id,omitempty" bson:"created_by_user_id,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	UpdatedByUserID string `json:"updated_by_user_id,omitempty" bson:"updated_by_user_id,omitempty"`
}

// NewVision returns a vision model with normalised fields and defaults.
func NewVision(req *CreateVisionRequest) *Vision {
	if req == nil {
		return &Vision{Status: VisionStatusDraft}
	}

	status := normaliseVisionStatus(req.Status)
	if status == "" {
		status = VisionStatusDraft
	}

	return &Vision{
		Name:            normaliseVisionName(req.Name),
		Kind:            normaliseVisionKind(req.Kind),
		Description:     strings.TrimSpace(req.Description),
		Status:          status,
		Metadata:        req.Metadata,
		CreatedByUserID: strings.TrimSpace(req.CreatedByUserID),
	}
}

// GenerateID creates a UUID for the vision.
func (b *Vision) GenerateID() *Vision {
	b.ID = toolbox.GenerateUuidV4()
	return b
}

// GenerateNanoID creates a short public identifier for the vision.
func (b *Vision) GenerateNanoID() *Vision {
	b.NanoID = toolbox.GenerateNanoId()
	return b
}

// SetCreatedAtTimeToNow sets the created timestamp to the platform UTC format.
func (b *Vision) SetCreatedAtTimeToNow() *Vision {
	b.CreatedAt = toolbox.TimeNowUTC()
	return b
}

// SetUpdatedAtTimeToNow sets the updated timestamp to the platform UTC format.
func (b *Vision) SetUpdatedAtTimeToNow() *Vision {
	b.UpdatedAt = toolbox.TimeNowUTC()
	return b
}

// normaliseVisionName trims surrounding whitespace from a vision name.
func normaliseVisionName(value string) string {
	return strings.TrimSpace(value)
}

// normaliseVisionKind trims and standardises a vision kind to lowercase.
func normaliseVisionKind(value string) string {
	return toolbox.StringStandardisedToLower(strings.TrimSpace(value))
}

// normaliseVisionStatus trims and standardises a vision status to lowercase.
func normaliseVisionStatus(value string) string {
	return toolbox.StringStandardisedToLower(strings.TrimSpace(value))
}
