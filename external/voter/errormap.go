package voter

import "github.com/ooaklee/reply/v2"

// ErrorMap can be composed by consuming handlers; voter exposes no HTTP routes.
// Native datastore errors remain intact and require the host's safe mappings.
var ErrorMap = reply.ErrorManifest{
	ErrInvalidRequest: {Title: "Provide a valid vote request.", StatusCode: 400, Code: "VOTER-001"},
	ErrActorInvalid:   {Title: "A verified voter is required.", StatusCode: 401, Code: "VOTER-002"},
	ErrUnavailable:    {Title: "Voting could not be confirmed.", StatusCode: 503, Code: "VOTER-003"},
}
