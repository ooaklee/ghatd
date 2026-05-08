package toolbox

import "github.com/ooaklee/reply/v2"

// ToolboxErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var ToolboxErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrPageOutOfRange:     {Title: "Bad Request", Detail: "Page out of range", StatusCode: 400, Code: "TLB-001"},
	ErrMissingUriVariable: {Title: "Bad Request", Detail: "Missing URI variable", StatusCode: 400, Code: "TLB-002"},
	ErrInvalidEmail:       {Title: "Bad Request", Detail: "Invalid email address", StatusCode: 400, Code: "TLB-003"},
}
