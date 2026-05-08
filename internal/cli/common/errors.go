package common

import "errors"

var (
	ErrAppModuleNameInvalidError = errors.New(ErrKeyAppModuleNameInvalidError)
	ErrAppNameInvalidError       = errors.New(ErrKeyAppNameInvalidError)
	ErrDetailTypeInvalidError    = errors.New(ErrKeyDetailTypeInvalidError)
	ErrDetailUrlInvalidError     = errors.New(ErrKeyDetailUrlInvalidError)
)
