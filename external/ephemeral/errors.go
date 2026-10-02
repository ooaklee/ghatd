package ephemeral

import (
	"errors"

	"github.com/go-redis/redis/v7"
)

var (
	// ErrAuthNotFound means the requested session is absent or expired, not that
	// its backing store is unavailable. Custom session adapters should return it
	// for expected absence and preserve operational errors separately.
	ErrAuthNotFound = errors.New("ephemeral/auth-not-found")
	// ErrInvalidAuthLookup rejects missing context, session identity or client
	// configuration before attempting a session-store read.
	ErrInvalidAuthLookup         = errors.New("ephemeral/invalid-auth-lookup")
	ErrHardenedRateLimitExceeded = errors.New(ErrKeyHardenedRateLimitExceeded)
	ErrRequestorLimitExceeded    = errors.New(ErrKeyRequestorLimitExceeded)
)

// IsAuthNotFound recognises expected session absence from current adapters and
// legacy Redis adapters. It does not classify outages by their error strings.
func IsAuthNotFound(err error) bool {
	return errors.Is(err, ErrAuthNotFound) || errors.Is(err, redis.Nil)
}
