package indexnow

import (
	"context"
	"time"
)

// Attempt records the lifecycle of a single outbound IndexNow submission.
// It intentionally never stores the API key, request body, or response body.
type Attempt struct {
	// ID is a unique identifier generated before the outbound call.
	ID string `json:"id" bson:"_id"`

	// URLs is the validated list of submitted URLs.
	URLs []string `json:"urls" bson:"urls"`

	// Method is the HTTP method used for the submission ("POST" for batches,
	// "GET" for single-URL submissions).
	Method string `json:"method" bson:"method"`

	// StartedAt is when the outbound request was initiated.
	StartedAt time.Time `json:"started_at" bson:"started_at"`

	// FinishedAt is when the response or failure was recorded; zero while the
	// attempt is in flight or when a crash prevented completion.
	FinishedAt time.Time `json:"finished_at,omitempty" bson:"finished_at,omitempty"`

	// StatusCode is the HTTP status received, or 0 on transport failure.
	StatusCode int `json:"status_code" bson:"status_code"`

	// Outcome classifies the attempt result.
	Outcome Outcome `json:"outcome" bson:"outcome"`

	// Success reports whether the protocol acknowledged or accepted the URLs
	// (HTTP 200 or 202). It never implies the URLs were indexed.
	Success bool `json:"success" bson:"success"`

	// FailureMessage is a scrubbed, human-readable error summary; empty on
	// success.
	FailureMessage string `json:"failure_message,omitempty" bson:"failure_message,omitempty"`

	// Retryable reports whether the same request may be retried later.
	Retryable bool `json:"retryable" bson:"retryable"`

	// RetryAfter preserves the endpoint's requested retry delay, when provided.
	RetryAfter time.Duration `json:"retry_after,omitempty" bson:"retry_after,omitempty"`
}

// AttemptRepository persists submission attempt lifecycle records. The service
// records an attempt before the outbound request and completes it after the
// outcome is known; see the package README for the exact crash-window
// guarantees. Implementations must be safe for concurrent use.
type AttemptRepository interface {
	// RecordAttempt persists the in-flight attempt identified by its ID before
	// the outbound HTTP request is sent. A returned error aborts the submission
	// before any external side effect.
	RecordAttempt(ctx context.Context, attempt *Attempt) error

	// FinishAttempt updates the stored attempt with the final outcome fields.
	// A returned error is surfaced to the caller alongside the submission
	// result; it must not be interpreted as "the submission did not happen".
	FinishAttempt(ctx context.Context, attempt *Attempt) error
}
