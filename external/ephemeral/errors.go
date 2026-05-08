package ephemeral

import "errors"

var (
	ErrHardenedRateLimitExceeded = errors.New(ErrKeyHardenedRateLimitExceeded)
	ErrRequestorLimitExceeded    = errors.New(ErrKeyRequestorLimitExceeded)
)
