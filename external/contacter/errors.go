package contacter

import "errors"

var (
	ErrCommsIdRequired     = errors.New(ErrKeyCommsIdRequired)
	ErrCommsNotFound       = errors.New(ErrKeyCommsNotFound)
	ErrEmailRequired       = errors.New(ErrKeyEmailRequired)
	ErrFullNameRequired    = errors.New(ErrKeyFullNameRequired)
	ErrInvalidCommsPayload = errors.New(ErrKeyInvalidCommsPayload)
)
