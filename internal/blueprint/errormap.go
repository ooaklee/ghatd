package blueprint

import (
	"github.com/ooaklee/reply"
)

// blueprintErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// nolint will be used later
var blueprintErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyBlueprintError: {Title: "Bad Request", Detail: "Some blueprint error", StatusCode: 400},
}
