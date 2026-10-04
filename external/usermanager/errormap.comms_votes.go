package usermanager

import (
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/reply/v2"
)

// commsVoteErrorMap preserves communications wire codes for shared failures.
// Callers compose this after voter defaults and before host overrides.
func commsVoteErrorMap() reply.ErrorManifest {
	return reply.ErrorManifest{
		contacter.ErrCommsVoteInvalid:     {Title: "Provide a valid conversation vote.", StatusCode: 400, Code: "HOST_COMMS_VOTE_INVALID"},
		contacter.ErrCommsVoteUnavailable: {Title: "Conversation voting could not be confirmed.", StatusCode: 503, Code: "HOST_COMMS_VOTE_UNAVAILABLE"},
	}
}
