package blueprint

import "errors"

var (
	ErrBlueprintDatabaseError          = errors.New(ErrMessageBlueprintDatabaseError)
	ErrBlueprintError                  = errors.New(ErrMessageBlueprintError)
	ErrBlueprintIDIsRequired           = errors.New(ErrMessageBlueprintIDIsRequired)
	ErrBlueprintInvalidPayload         = errors.New(ErrMessageBlueprintInvalidPayload)
	ErrBlueprintInvalidQueryParam      = errors.New(ErrMessageBlueprintInvalidQueryParam)
	ErrBlueprintKindIsRequired         = errors.New(ErrMessageBlueprintKindIsRequired)
	ErrBlueprintNameIsRequired         = errors.New(ErrMessageBlueprintNameIsRequired)
	ErrBlueprintRegistrationConflict   = errors.New(ErrMessageBlueprintRegistrationConflict)
	ErrBlueprintRegistrationKeyMissing = errors.New(ErrMessageBlueprintRegistrationKeyMissing)
	ErrBlueprintRegistrationNotFound   = errors.New(ErrMessageBlueprintRegistrationNotFound)
	ErrBlueprintResourceNotFound       = errors.New(ErrMessageBlueprintResourceNotFound)
	ErrBlueprintUserIDIsRequired       = errors.New(ErrMessageBlueprintUserIDIsRequired)
)
