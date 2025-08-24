package apitoken

import (
	"github.com/ooaklee/reply/v2"
)

// ApitokenErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var ApitokenErrorMap reply.ErrorManifest = map[error]reply.ErrorManifestItem{

	ErrNoMatchingUserAPITokenFound:  {Title: "Unauthorized", Code: "APT0-200", StatusCode: 401},
	ErrUnableToValidateUserAPIToken: {Title: "Unauthorized", Code: "APT0-201", StatusCode: 401},
	ErrUnableToFindRequiredHeaders:  {Title: "Unauthorized", Code: "APT0-202", StatusCode: 401},
	ErrInvalidAPIFormatDetected:     {Title: "Bad Request", Code: "APT0-203", Detail: "Malformed API token provided", StatusCode: 400},
	ErrResourceNotFound:             {Title: "Not Found", Code: "APT0-204", StatusCode: 404},
	ErrRequiredUserIDMissing:        {Title: "Bad Request", Code: "APT0-205", Detail: "Requirements unsatisfied", StatusCode: 400},
	ErrPageOutOfRange:               {Title: "Bad Request", Code: "APT0-206", Detail: "Page out of range", StatusCode: 400},
	ErrTokenStatusInvalid:           {Title: "Bad Request", Code: "APT0-207", Detail: "Please verify token status", StatusCode: 400},
}
