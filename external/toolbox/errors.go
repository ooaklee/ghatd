package toolbox

import "errors"

var (
	ErrInvalidEmail       = errors.New(ErrKeyInvalidEmail)
	ErrMissingUriVariable = errors.New(ErrKeyMissingUriVariable)
	ErrPageOutOfRange     = errors.New(ErrKeyPageOutOfRange)
)
