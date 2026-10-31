package indexnow

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for validation and wiring failures. They never carry keys,
// URLs, or transport diagnostics.
var (
	// ErrInvalidAPIKey is returned when the API key is not 8-128 characters of
	// ASCII letters, digits, or hyphens.
	ErrInvalidAPIKey = errors.New("indexnow: invalid api key")

	// ErrMissingSiteURL is returned when the configured site URL is empty.
	ErrMissingSiteURL = errors.New("indexnow: missing site url")

	// ErrInvalidSiteURL is returned when the configured site URL is not an
	// absolute http(s) URL with a host.
	ErrInvalidSiteURL = errors.New("indexnow: invalid site url")

	// ErrInvalidURL is returned when a submitted URL is not absolute http(s).
	ErrInvalidURL = errors.New("indexnow: invalid url")

	// ErrURLHostMismatch is returned when a submitted URL belongs to a different
	// host than the configured site.
	ErrURLHostMismatch = errors.New("indexnow: url host does not match site")

	// ErrTooManyURLs is returned when a batch exceeds MaxURLsPerRequest.
	ErrTooManyURLs = errors.New("indexnow: too many urls in request")

	// ErrNoURLs is returned when a request contains no URLs.
	ErrNoURLs = errors.New("indexnow: no urls in request")

	// ErrNilRequest is returned when a nil request is supplied.
	ErrNilRequest = errors.New("indexnow: nil request")

	// ErrInvalidKeyLocation is returned when the configured key location is not
	// an absolute path within the site.
	ErrInvalidKeyLocation = errors.New("indexnow: invalid key location")

	// ErrInvalidEndpoint is returned when the configured endpoint is not an
	// absolute http(s) URL.
	ErrInvalidEndpoint = errors.New("indexnow: invalid endpoint")

	// ErrNilService is returned when a method is called on a nil service.
	ErrNilService = errors.New("indexnow: nil service")

	// ErrNilContext rejects an absent context before dependencies are invoked.
	ErrNilContext = errors.New("indexnow: nil context")
	// ErrInvalidConfiguration identifies invalid timeout or transport wiring.
	ErrInvalidConfiguration = errors.New("indexnow: invalid configuration")
	// ErrAuditUnavailable means attempt persistence could not be confirmed.
	ErrAuditUnavailable = errors.New("indexnow: audit unavailable")

	// ErrAttemptUnavailable reports missing audit storage wiring or an unusable
	// dependency result. It never confirms rollback or authorizes a blind retry.
	ErrAttemptUnavailable = errors.New("indexnow: attempt storage unavailable")
	// ErrAttemptInvalidPayload rejects invalid audit input before persistence.
	ErrAttemptInvalidPayload = errors.New("indexnow: invalid attempt record")
	// ErrAttemptNotFound reports a confirmed absent attempt ID.
	ErrAttemptNotFound = errors.New("indexnow: attempt not found")
	// ErrAttemptAlreadyFinished rejects a conflicting final audit completion.
	ErrAttemptAlreadyFinished = errors.New("indexnow: attempt already finished")
	// ErrAttemptDuplicateID rejects reuse of an existing attempt identity.
	ErrAttemptDuplicateID = errors.New("indexnow: duplicate attempt id")
)

// Outcome classifies the result of one submission attempt.
type Outcome string

const (
	// OutcomeInFlight is persisted before dispatch. A retained record with this
	// outcome has an unknown delivery result and must not be replayed blindly.
	OutcomeInFlight Outcome = "in_flight"
	// OutcomeAcknowledged means the search engine accepted the URL(s) as
	// processed (HTTP 200). It does not guarantee the URL is indexed.
	OutcomeAcknowledged Outcome = "acknowledged"

	// OutcomePending means the search engine accepted the URL(s) for later
	// evaluation (HTTP 202).
	OutcomePending Outcome = "pending"

	// OutcomeBadRequest means the request was rejected as malformed (HTTP 400).
	OutcomeBadRequest Outcome = "bad_request"

	// OutcomeRejected means the request was rejected as invalid or unauthorized
	// (HTTP 403, HTTP 422).
	OutcomeRejected Outcome = "rejected"

	// OutcomeRateLimited means the request was throttled (HTTP 429).
	OutcomeRateLimited Outcome = "rate_limited"

	// OutcomeServerError means the search engine failed (HTTP 5xx).
	OutcomeServerError Outcome = "server_error"

	// OutcomeUnexpectedStatus means the search engine returned a status outside
	// the documented protocol.
	OutcomeUnexpectedStatus Outcome = "unexpected_status"

	// OutcomeTransportError means the request never completed (network failure,
	// timeout, cancellation, or refused redirect).
	OutcomeTransportError Outcome = "transport_error"
)

// AuditError distinguishes a rejected pre-send audit from a completion failure.
// Dispatched means the outbound operation was attempted; the accompanying
// response, when present, describes its result. Native diagnostics are omitted.
type AuditError struct {
	Dispatched bool
}

func (e *AuditError) Error() string {
	if e.Dispatched {
		return "indexnow: audit completion failed after dispatch; inspect the submission receipt"
	}
	return "indexnow: audit recording failed before dispatch"
}

func (e *AuditError) Unwrap() error { return ErrAuditUnavailable }

// Error describes a failed submission with retryability metadata. Messages are
// deliberately generic: API keys, URLs, and raw transport diagnostics are never
// included so the value is safe to surface, log, or persist.
type Error struct {
	// Outcome is the classification of the failure.
	Outcome Outcome

	// StatusCode is the HTTP status received, or 0 when no response arrived.
	StatusCode int

	// Retryable reports whether repeating the same request later may succeed.
	Retryable bool

	// RetryAfter is the delay requested via the Retry-After header, or 0 when
	// the server did not send one.
	RetryAfter time.Duration

	// Cause optionally names the failure category without transport detail
	// (for example "context canceled").
	Cause string
}

// Error implements the error interface with a scrubbed message.
func (e *Error) Error() string {
	msg := fmt.Sprintf("indexnow: submission failed (%s)", e.Outcome)
	if e.StatusCode != 0 {
		msg = fmt.Sprintf("%s status=%d", msg, e.StatusCode)
	}
	if e.Retryable {
		msg = fmt.Sprintf("%s retryable", msg)
	}
	if e.RetryAfter > 0 {
		msg = fmt.Sprintf("%s retry_after=%s", msg, e.RetryAfter)
	}
	if e.Cause != "" {
		msg = fmt.Sprintf("%s cause=%q", msg, e.Cause)
	}
	return msg
}

// Unwrap preserves cancellation identity using only fixed safe sentinels.
func (e *Error) Unwrap() error {
	switch e.Cause {
	case "context canceled":
		return context.Canceled
	case "deadline exceeded":
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// asOutcome maps an HTTP status code to the protocol outcome and retryability.
func asOutcome(statusCode int) (Outcome, bool) {
	switch {
	case statusCode == 200:
		return OutcomeAcknowledged, false
	case statusCode == 202:
		return OutcomePending, false
	case statusCode == 400:
		return OutcomeBadRequest, false
	case statusCode == 403 || statusCode == 422:
		return OutcomeRejected, false
	case statusCode == 429:
		return OutcomeRateLimited, true
	case statusCode >= 500 && statusCode <= 599:
		return OutcomeServerError, true
	default:
		return OutcomeUnexpectedStatus, false
	}
}
