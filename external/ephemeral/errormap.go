package ephemeral

import (
	"github.com/ooaklee/reply"
)

// EphemeralStoreErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// TODO: remove nolint
// nolint will be used later
var EphemeralStoreErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyRequestorLimitExceeded: {Title: "Rate Limited", Detail: "You have used up allocated requests allowance; please try again later or verify you have authenticated yourself.", StatusCode: 429},
}
