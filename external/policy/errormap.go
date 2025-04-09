package policy

import (
	"github.com/ooaklee/reply"
)

// PolicyErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var PolicyErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyPolicyError:       {Title: "Bad Request", Detail: "Some policy error", StatusCode: 400, Code: "P0-001"},
	ErrKeyPolicyNotFound:    {Title: "Not Found", Detail: "Policy not found", StatusCode: 404, Code: "P0-002"},
	ErrKeyInvalidpolicyName: {Title: "Bad Request", Detail: "Invalid policy name", StatusCode: 400, Code: "P0-003"},
}
