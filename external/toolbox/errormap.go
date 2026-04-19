package toolbox

import "github.com/ooaklee/reply"

// ToolboxErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var ToolboxErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrKeyPageOutOfRange:     {Title: "Bad Request", Detail: "Page out of range", StatusCode: 400, Code: "TLB-001"},
	ErrKeyMissingUriVariable: {Title: "Bad Request", Detail: "Missing URI variable", StatusCode: 400, Code: "TLB-002"},
}
