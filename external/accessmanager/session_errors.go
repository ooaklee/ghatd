package accessmanager

import (
	"errors"
	"reflect"

	"github.com/ooaklee/ghatd/external/auth"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// SessionErrorKind separates credential lifecycle from account policy and
// operational failure. It is independent of HTTP status codes and host response
// overrides. Only use it for failures from session verification/refresh, not
// unrelated resource lookups which happen to return the same error sentinel.
type SessionErrorKind uint8

const (
	// SessionErrorUnknown includes outages, cancellation, configuration failures
	// and ambiguous causes. Deny the request without refresh, cookie removal or
	// anonymous fallback. Unknown does not imply that a retry is safe.
	SessionErrorUnknown SessionErrorKind = iota
	// SessionErrorRefreshable means the access credential is absent/expired.
	// A separately verified refresh credential may establish a new session.
	SessionErrorRefreshable
	// SessionErrorInvalidCredential means reauthentication is needed. It is safe
	// to discard the presented cookies; an explicitly public route may fall back
	// to its anonymous rate-limited flow, never to authenticated authority.
	SessionErrorInvalidCredential
	// SessionErrorDenied means the current account cannot enter the route. A
	// refresh cannot grant missing roles/status; keep the existing cookies and
	// deny the request without a public downgrade.
	SessionErrorDenied
)

// sessionErrorKinds is an explicit allowlist. Custom adapters must wrap these
// stable sentinels for known credential failures; error text is never authority.
var sessionErrorKinds = map[error]SessionErrorKind{
	auth.ErrNoBearerHeaderFound:                      SessionErrorRefreshable,
	auth.ErrUnauthorizedParsedStringTokenExpired:     SessionErrorRefreshable,
	ErrUnauthorizedTokenNotFoundInStore:              SessionErrorRefreshable,
	auth.ErrUnauthorized:                             SessionErrorInvalidCredential,
	auth.ErrUnauthorizedMalformattedToken:            SessionErrorInvalidCredential,
	auth.ErrUnauthorizedNoAdminInfoFound:             SessionErrorInvalidCredential,
	auth.ErrUnauthorizedNoAuthorizationInfoFound:     SessionErrorInvalidCredential,
	auth.ErrUnauthorizedNoTokenUUID:                  SessionErrorInvalidCredential,
	auth.ErrUnauthorizedNoUserIDFound:                SessionErrorInvalidCredential,
	auth.ErrUnauthorizedParsedStringUnknown:          SessionErrorInvalidCredential,
	auth.ErrUnauthorizedRefreshTokenExpired:          SessionErrorInvalidCredential,
	auth.ErrUnauthorizedTokenUnexpectedSigningMethod: SessionErrorInvalidCredential,
	ErrUnauthorizedRefreshTokenCacheDeletionFailure:  SessionErrorInvalidCredential,
	ErrEmptyRefreshToken:                             SessionErrorInvalidCredential,
	ErrInvalidRefreshToken:                           SessionErrorInvalidCredential,
	ErrInvalidAuthToken:                              SessionErrorInvalidCredential,
	ErrOAuthReauthenticationRequired:                 SessionErrorInvalidCredential,
	user.ErrUserNotFound:                             SessionErrorInvalidCredential,
	ErrUnauthorizedNonActiveStatus:                   SessionErrorDenied,
	ErrUnauthorizedAdminAccessAttempted:              SessionErrorDenied,
}

// ClassifySessionError recognises one unambiguous known session cause through
// ordinary wrapping. Joined, cyclic, overly deep and unknown errors remain
// operational: a known denial must not hide an accompanying storage outage.
// Custom Is methods cannot replace the actual leaf cause for lifecycle decisions.
// Nil also returns Unknown and must not be used as evidence of failure.
func ClassifySessionError(err error) SessionErrorKind {
	return sessionErrorKinds[knownSessionCause(err)]
}

// knownSessionCause returns only an allowlisted leaf identity in a bounded
// unary chain. It deliberately does not call Is or use response-error mapping.
func knownSessionCause(err error) error {
	current := err
	for depth := 0; current != nil; depth++ {
		if depth >= 64 || nilSessionCause(current) {
			return nil
		}
		if _, multi := current.(interface{ Unwrap() []error }); multi {
			return nil
		}
		next := errors.Unwrap(current)
		if next == nil {
			for candidate := range sessionErrorKinds {
				if current == candidate {
					return candidate
				}
			}
			return nil
		}
		current = next
	}
	return nil
}

// nilSessionCause rejects typed-nil adapter errors before invoking their
// Unwrap or Is methods, which may otherwise dereference an absent receiver.
func nilSessionCause(err error) bool {
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
