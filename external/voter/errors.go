package voter

import "errors"

var (
	// ErrInvalidRequest rejects malformed targets, values and unbounded batches.
	ErrInvalidRequest = errors.New("voter/invalid-request")
	// ErrActorInvalid rejects absent mutation actors or contradictory identity.
	ErrActorInvalid = errors.New("voter/invalid-actor")
	// ErrUnavailable reports invalid dependencies, receipts or stored results.
	ErrUnavailable = errors.New("voter/unavailable")
)
