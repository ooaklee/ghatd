package blueprint

import (
	"github.com/ooaklee/reply/v2"
)

// blueprintErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var blueprintErrorMap = reply.ErrorManifest{
	ErrBlueprintError: {Title: "Bad Request", Detail: "Some blueprint error", StatusCode: 400},
}
