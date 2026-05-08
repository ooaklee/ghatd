package ephemeral

import (
	"github.com/ooaklee/reply/v2"
)

// EphemeralStoreErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// TODO: remove nolint
// nolint will be used later
var EphemeralStoreErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrRequestorLimitExceeded:    {Title: "Rate Limited", Detail: "You have used up allocated requests allowance; please try again later or verify you have authenticated yourself.", StatusCode: 429, Code: "EPH0-001"},
	ErrHardenedRateLimitExceeded: {Title: "Rate Limited", Detail: "Too many verification attempts detected. Please wait before trying again.", StatusCode: 429, Code: "EPH0-002"},
}
