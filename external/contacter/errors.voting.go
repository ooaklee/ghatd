package contacter

import "github.com/ooaklee/ghatd/external/voter"

var (
	// ErrCommsVoteInvalid identifies malformed contact voting input. The owning
	// manager supplies the communications-specific HTTP error mapping.
	ErrCommsVoteInvalid = voter.ErrInvalidRequest
	// ErrCommsVoteUnavailable identifies missing capabilities or unconfirmed
	// vote results. It must not be interpreted as a successful empty result.
	ErrCommsVoteUnavailable = voter.ErrUnavailable
)
