package apitoken

import "errors"

var (
	// ErrServiceUnavailable reports missing wiring or an invalid persistence result.
	// It is not an authentication denial or proof that a write never committed.
	ErrServiceUnavailable = errors.New("apitoken/service-unavailable")
	// ErrInventoryUnavailable denies admission when its lock/lifecycle is unsafe.
	ErrInventoryUnavailable = errors.New("apitoken/inventory-unavailable")
	// ErrInvalidTokenQuery rejects absent or contradictory list constraints.
	ErrInvalidTokenQuery = errors.New("apitoken/invalid-token-query")
	// ErrInvalidTokenTTL rejects negative or duration-overflowing lifetimes.
	ErrInvalidTokenTTL                    = errors.New("apitoken/invalid-token-ttl")
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
