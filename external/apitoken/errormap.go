package apitoken

import (
	"github.com/ooaklee/reply"
)

// ApitokenErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var ApitokenErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyPageOutOfRange:               {Title: "Bad Request", Detail: "Page out of range", StatusCode: 400, Code: "APT0-001"},
	ErrKeyTokenStatusInvalid:           {Title: "Bad Request", Detail: "Please verify token status", StatusCode: 400, Code: "APT0-002"},
	ErrKeyNoMatchingUserAPITokenFound:  {Title: "Unauthorized", Detail: "Invalid credentials provided", StatusCode: 401, Code: "APT0-003"},
	ErrKeyUnableToValidateUserAPIToken: {Title: "Unauthorized", Detail: "Unable to validate credentials", StatusCode: 401, Code: "APT0-004"},
	ErrKeyUnableToFindRequiredHeaders:  {Title: "Unauthorized", Detail: "Missing required authentication headers", StatusCode: 401, Code: "APT0-005"},
	ErrKeyRequiredUserIDMissing:        {Title: "Bad Request", Detail: "Requirements unsatisfied", StatusCode: 400, Code: "APT0-006"},
	ErrKeyInvalidAPIFormatDetected:     {Title: "Bad Request", Detail: "Malformed API token provided", StatusCode: 400, Code: "APT0-007"},
	ErrKeyResourceNotFound:             {Title: "Not Found", Detail: "API token not found", StatusCode: 404, Code: "APT0-008"},
}
