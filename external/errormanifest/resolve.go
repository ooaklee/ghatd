package errormanifest

import (
	"errors"
	"reflect"

	"github.com/ooaklee/reply/v2"
)

// unmappedError is deliberately opaque: reply must not inspect a joined cause,
// log private diagnostics or attempt a map lookup using an uncomparable error.
// It has no manifest entry, so the standard generic server response is used.
var unmappedError = errors.New("errormanifest/unmapped-error")

// CanonicalError resolves one unambiguous domain error to its manifest key for
// authentication responses, whose policy is stricter than reply's general
// structural resolver. It never matches error strings or copies diagnostic text
// into a public response.
// Unknown, multi-cause, over-deep or ambiguous chains resolve to an opaque
// unmapped error and therefore the replier's generic fallback. Nil stays nil.
// Manifests use the composer's last-wins response overrides for the same
// canonical key.
// Use this only at response time: retain the original error for internal cause
// inspection, retry decisions and appropriately redacted diagnostics.
func CanonicalError(err error, manifests []reply.ErrorManifest) error {
	if err == nil {
		return nil
	}
	// An errors.Join containing a public denial plus a storage failure must not
	// accidentally turn a server failure into the one recognized client error.
	current := err
	for depth := 0; current != nil; depth++ {
		if depth >= 64 || nilErrorValue(current) {
			return unmappedError
		}
		if _, multi := current.(interface{ Unwrap() []error }); multi {
			return unmappedError
		}
		current = errors.Unwrap(current)
	}
	var matched error
	for _, manifest := range manifests {
		for key := range manifest {
			if nilErrorValue(key) || !errors.Is(err, key) {
				continue
			}
			if matched != nil && matched != key {
				return unmappedError
			}
			matched = key
		}
	}
	if matched != nil {
		return matched
	}
	return unmappedError
}

// nilErrorValue rejects nil-capable dynamic values before invoking their
// methods. A typed nil is an invalid failure, not permission to call adapter
// Is/Unwrap methods that may dereference an absent receiver.
func nilErrorValue(err error) bool {
	if err == nil {
		return true
	}
	switch value := reflect.ValueOf(err); value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
