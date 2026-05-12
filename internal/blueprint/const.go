package blueprint

const (
	// BlueprintURIVariableId is the URI variable used to identify a blueprint.
	BlueprintURIVariableId = "blueprintId"

	// BlueprintURIVariableID is an exported initialism-friendly alias.
	BlueprintURIVariableID = BlueprintURIVariableId
)

const (
	// BlueprintCollection is the mongo collection name for blueprint records.
	BlueprintCollection string = "blueprints"

	// BlueprintStatusDraft is the default state for newly created examples.
	BlueprintStatusDraft string = "draft"

	// BlueprintStatusActive represents a usable blueprint example.
	BlueprintStatusActive string = "active"

	// BlueprintStatusArchived represents a blueprint example that should not be used for new work.
	BlueprintStatusArchived string = "archived"
)

const (
	// Error messages used as the concrete error values for blueprint failures.
	ErrMessageBlueprintDatabaseError          string = "blueprint-database-operation-failed"
	ErrMessageBlueprintError                  string = "blueprint-request-is-invalid"
	ErrMessageBlueprintIDIsRequired           string = "blueprint-id-is-required"
	ErrMessageBlueprintInvalidPayload         string = "blueprint-payload-is-invalid"
	ErrMessageBlueprintInvalidQueryParam      string = "blueprint-query-param-is-invalid"
	ErrMessageBlueprintKindIsRequired         string = "blueprint-kind-is-required"
	ErrMessageBlueprintNameIsRequired         string = "blueprint-name-is-required"
	ErrMessageBlueprintRegistrationConflict   string = "blueprint-registration-already-exists"
	ErrMessageBlueprintRegistrationKeyMissing string = "blueprint-registration-key-is-required"
	ErrMessageBlueprintRegistrationNotFound   string = "blueprint-registration-not-found"
	ErrMessageBlueprintResourceNotFound       string = "blueprint-resource-not-found"
	ErrMessageBlueprintUserIDIsRequired       string = "blueprint-user-id-is-required"
)
