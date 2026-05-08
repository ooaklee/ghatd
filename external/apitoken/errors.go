package apitoken

import "errors"

var (
	ErrErrorCreatingShortLivedAccessToken = errors.New(ErrKeyErrorCreatingShortLivedAccessToken)
	ErrInvalidAPIFormatDetected           = errors.New(ErrKeyInvalidAPIFormatDetected)
	ErrNoMatchingUserAPITokenFound        = errors.New(ErrKeyNoMatchingUserAPITokenFound)
	ErrPageOutOfRange                     = errors.New(ErrKeyPageOutOfRange)
	ErrRequiredUserIDMissing              = errors.New(ErrKeyRequiredUserIDMissing)
	ErrResourceNotFound                   = errors.New(ErrKeyResourceNotFound)
	ErrTokenStatusInvalid                 = errors.New(ErrKeyTokenStatusInvalid)
	ErrUnableToFindRequiredHeaders        = errors.New(ErrKeyUnableToFindRequiredHeaders)
	ErrUnableToValidateUserAPIToken       = errors.New(ErrKeyUnableToValidateUserAPIToken)
)
