package response

import "errors"

var (
	ErrResourceNotFound = errors.New(ErrKeyResourceNotFound)
)
