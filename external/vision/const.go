package vision

const (
	// VisionURIVariableID is an exported initialism-friendly alias.
	VisionURIVariableID = "visionID"
)

const (
	// VisionCollection is the mongo collection name for vision records.
	VisionCollection string = "visions"

	// VisionStatusDraft is the default state for newly created examples.
	VisionStatusDraft string = "draft"

	// VisionStatusActive represents a usable vision example.
	VisionStatusActive string = "active"

	// VisionStatusArchived represents a vision example that should not be used for new work.
	VisionStatusArchived string = "archived"
)

const (
	// Error messages used as the concrete error values for vision failures.
	ErrMessageVisionDatabaseError          string = "vision-database-operation-failed"
	ErrMessageVisionError                  string = "vision-request-is-invalid"
	ErrMessageVisionIDIsRequired           string = "vision-id-is-required"
	ErrMessageVisionInvalidPayload         string = "vision-payload-is-invalid"
	ErrMessageVisionInvalidQueryParam      string = "vision-query-param-is-invalid"
	ErrMessageVisionKindIsRequired         string = "vision-kind-is-required"
	ErrMessageVisionNameIsRequired         string = "vision-name-is-required"
	ErrMessageVisionRegistrationConflict   string = "vision-registration-already-exists"
	ErrMessageVisionRegistrationKeyMissing string = "vision-registration-key-is-required"
	ErrMessageVisionRegistrationNotFound   string = "vision-registration-not-found"
	ErrMessageVisionResourceNotFound       string = "vision-resource-not-found"
	ErrMessageVisionUserIDIsRequired       string = "vision-user-id-is-required"
)
