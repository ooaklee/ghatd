package vision

import "errors"

var (
	ErrVisionDatabaseError          = errors.New(ErrMessageVisionDatabaseError)
	ErrVisionError                  = errors.New(ErrMessageVisionError)
	ErrVisionIDIsRequired           = errors.New(ErrMessageVisionIDIsRequired)
	ErrVisionInvalidPayload         = errors.New(ErrMessageVisionInvalidPayload)
	ErrVisionInvalidQueryParam      = errors.New(ErrMessageVisionInvalidQueryParam)
	ErrVisionKindIsRequired         = errors.New(ErrMessageVisionKindIsRequired)
	ErrVisionNameIsRequired         = errors.New(ErrMessageVisionNameIsRequired)
	ErrVisionRegistrationConflict   = errors.New(ErrMessageVisionRegistrationConflict)
	ErrVisionRegistrationKeyMissing = errors.New(ErrMessageVisionRegistrationKeyMissing)
	ErrVisionRegistrationNotFound   = errors.New(ErrMessageVisionRegistrationNotFound)
	ErrVisionResourceNotFound       = errors.New(ErrMessageVisionResourceNotFound)
	ErrVisionUserIDIsRequired       = errors.New(ErrMessageVisionUserIDIsRequired)
)
