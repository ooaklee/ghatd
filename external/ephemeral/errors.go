package ephemeral

import (
	"errors"
	"reflect"

	"github.com/go-redis/redis/v7"
)

var (
	// ErrInvalidSessionCleanup rejects unusable account namespaces or adapter receipts.
	ErrInvalidSessionCleanup = errors.New("ephemeral/invalid-session-cleanup")
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

// nilEphemeralDependency rejects missing and typed-nil storage adapters.
func nilEphemeralDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// IsAuthNotFound recognises expected session absence from current adapters and
// legacy Redis adapters. Every leaf must be an absence sentinel: a joined
// outage, cancellation, nil cause, cycle or oversized chain is not absence.
// Ordinary wrappers and the native dual-sentinel wrapper are supported; custom
// Is methods and error text cannot turn an unknown cause into expected absence.
func IsAuthNotFound(err error) bool {
	remaining := 64
	return authNotFoundCauses(err, &remaining)
}

// authNotFoundCauses bounds total traversal, including cycles and branching
// wrappers, before accepting only the two supported absence identities.
func authNotFoundCauses(err error, remaining *int) bool {
	if err == nil || *remaining == 0 {
		return false
	}
	*remaining -= 1
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return false
		}
	}
	if err == ErrAuthNotFound || err == redis.Nil {
		return true
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		causes := multi.Unwrap()
		if len(causes) == 0 || len(causes) > *remaining {
			return false
		}
		for _, cause := range causes {
			if !authNotFoundCauses(cause, remaining) {
				return false
			}
		}
		return true
	}
	return authNotFoundCauses(errors.Unwrap(err), remaining)
}
