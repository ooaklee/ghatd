package contacter

import "github.com/ooaklee/reply"

// ContacterErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var ContacterErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyInvalidCommsPayload: {Title: "Bad Request", Detail: "Invalid communication payload provided", StatusCode: 400, Code: "CT00-01"},
}
