package apitoken

import (
	"github.com/ooaklee/reply"
)

// ApitokenErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var ApitokenErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyPageOutOfRange:               {Title: "Bad Request", Detail: "Page out of range", StatusCode: 400},
	ErrKeyTokenStatusInvalid:           {Title: "Bad Request", Detail: "Please verify token status", StatusCode: 400},
	ErrKeyNoMatchingUserAPITokenFound:  {Title: "Unauthorized", Code: "200", StatusCode: 401},
	ErrKeyUnableToValidateUserAPIToken: {Title: "Unauthorized", Code: "201", StatusCode: 401},
	ErrKeyUnableToFindRequiredHeaders:  {Title: "Unauthorized", Code: "202", StatusCode: 401},
	ErrKeyRequiredUserIDMissing:        {Title: "Bad Request", Detail: "Requirements unsatisfied", StatusCode: 400},
	ErrKeyInvalidAPIFormatDetected:     {Title: "Bad Request", Code: "203", Detail: "Malformed API token provided", StatusCode: 400},
}
