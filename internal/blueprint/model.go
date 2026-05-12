package blueprint

import (
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// Blueprint is a small demo entity used to show the expected package shape.
type Blueprint struct {
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

// NewBlueprint returns a blueprint model with normalised fields and defaults.
func NewBlueprint(req *CreateBlueprintRequest) *Blueprint {
	if req == nil {
		return &Blueprint{Status: BlueprintStatusDraft}
	}

	status := normaliseBlueprintStatus(req.Status)
	if status == "" {
		status = BlueprintStatusDraft
	}

	return &Blueprint{
		Name:            normaliseBlueprintName(req.Name),
		Kind:            normaliseBlueprintKind(req.Kind),
		Description:     strings.TrimSpace(req.Description),
		Status:          status,
		Metadata:        req.Metadata,
		CreatedByUserID: strings.TrimSpace(req.CreatedByUserID),
	}
}

// GenerateID creates a UUID for the blueprint.
func (b *Blueprint) GenerateID() *Blueprint {
	b.ID = toolbox.GenerateUuidV4()
	return b
}

// GenerateNanoID creates a short public identifier for the blueprint.
func (b *Blueprint) GenerateNanoID() *Blueprint {
	b.NanoID = toolbox.GenerateNanoId()
	return b
}

// SetCreatedAtTimeToNow sets the created timestamp to the platform UTC format.
func (b *Blueprint) SetCreatedAtTimeToNow() *Blueprint {
	b.CreatedAt = toolbox.TimeNowUTC()
	return b
}

// SetUpdatedAtTimeToNow sets the updated timestamp to the platform UTC format.
func (b *Blueprint) SetUpdatedAtTimeToNow() *Blueprint {
	b.UpdatedAt = toolbox.TimeNowUTC()
	return b
}

// normaliseBlueprintName trims surrounding whitespace from a blueprint name.
func normaliseBlueprintName(value string) string {
	return strings.TrimSpace(value)
}

// normaliseBlueprintKind trims and standardises a blueprint kind to lowercase.
func normaliseBlueprintKind(value string) string {
	return toolbox.StringStandardisedToLower(strings.TrimSpace(value))
}

// normaliseBlueprintStatus trims and standardises a blueprint status to lowercase.
func normaliseBlueprintStatus(value string) string {
	return toolbox.StringStandardisedToLower(strings.TrimSpace(value))
}
