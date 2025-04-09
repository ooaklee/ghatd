package contacter

import "github.com/ooaklee/reply"

// ContacterErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var ContacterErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyInvalidCommsPayload: {Title: "Bad Request", Detail: "Invalid comms payload", StatusCode: 400, Code: "CT00-01"},
}
