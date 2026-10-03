package accesspolicy

import (
	"net/http"

	"github.com/ooaklee/reply/v2"
)

// AccessPolicyErrorMap supplies reusable public classifications for domain
// failures. Keep it immutable while serving requests and compose host/operation
// overrides afterward. Unknown persistence errors must retain a generic failure;
// this map neither grants authority nor proves a failed transaction never committed.
// Use the shared manifest writer so an unknown joined cause cannot be hidden by
// a mapped denial; single-cause wrappers still retain their public classification.
var AccessPolicyErrorMap = reply.ErrorManifest{
	ErrDenied:        {Title: "Access denied", Detail: "Current policy does not permit this operation", StatusCode: http.StatusForbidden, Code: "ACP0-001"},
	ErrConfiguration: {Title: "Policy unavailable", Detail: "The policy configuration is unavailable", StatusCode: http.StatusServiceUnavailable, Code: "ACP0-002"},
	ErrConflict:      {Title: "Policy conflict", Detail: "Review the current policy or operation before retrying", StatusCode: http.StatusConflict, Code: "ACP0-003"},
	ErrLimitReached:  {Title: "Usage limit reached", Detail: "The current policy budget has been exhausted", StatusCode: http.StatusTooManyRequests, Code: "ACP0-004"},
}
