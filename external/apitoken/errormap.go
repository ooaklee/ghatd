package apitoken

import (
	"github.com/ooaklee/reply/v2"
)

// ApitokenErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var ApitokenErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrServiceUnavailable:                 {Title: "Service Unavailable", Detail: "Credential service is unavailable", StatusCode: 503, Code: "APT0-013"},
	ErrInventoryUnavailable:               {Title: "Service Unavailable", Detail: "Token inventory is not ready", StatusCode: 503, Code: "APT0-012"},
	ErrInvalidTokenQuery:                  {Title: "Bad Request", Detail: "Token query is invalid", StatusCode: 400, Code: "APT0-011"},
	ErrInvalidTokenTTL:                    {Title: "Bad Request", Detail: "Token lifetime must be zero or a supported positive number of seconds", StatusCode: 400, Code: "APT0-010"},
	ErrPageOutOfRange:                     {Title: "Bad Request", Detail: "Page out of range", StatusCode: 400, Code: "APT0-001"},
	ErrTokenStatusInvalid:                 {Title: "Bad Request", Detail: "Please verify token status", StatusCode: 400, Code: "APT0-002"},
	ErrNoMatchingUserAPITokenFound:        {Title: "Unauthorized", Detail: "Invalid credentials provided", StatusCode: 401, Code: "APT0-003"},
	ErrUnableToValidateUserAPIToken:       {Title: "Unauthorized", Detail: "Unable to validate credentials", StatusCode: 401, Code: "APT0-004"},
	ErrUnableToFindRequiredHeaders:        {Title: "Unauthorized", Detail: "Missing required authentication headers", StatusCode: 401, Code: "APT0-005"},
	ErrRequiredUserIDMissing:              {Title: "Bad Request", Detail: "Requirements unsatisfied", StatusCode: 400, Code: "APT0-006"},
	ErrInvalidAPIFormatDetected:           {Title: "Bad Request", Detail: "Malformed API token provided", StatusCode: 400, Code: "APT0-007"},
	ErrResourceNotFound:                   {Title: "Not Found", Detail: "API token not found", StatusCode: 404, Code: "APT0-008"},
	ErrErrorCreatingShortLivedAccessToken: {Title: "Internal Server Error", Detail: "Unable to create short lived API token", StatusCode: 500, Code: "APT0-009"},
}
