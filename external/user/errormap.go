package user

import (
	"github.com/ooaklee/reply"
)

// UserErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// Use https://docs.microsoft.com/en-us/troubleshoot/iis/http-status-code to expand messages i.e. AccessDenied1
var UserErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyInvalidUserBody:         {Title: "Bad Request", Detail: "Check submitted user information.", StatusCode: 400},
	ErrKeyInvalidUserID:           {Title: "Bad Request", Detail: "User ID missing or malformatted.", StatusCode: 400},
	ErrKeyUserNeverActivated:      {Title: "Invalid User State", Detail: "User resource state conflicts with request.", StatusCode: 409},
	ErrKeyInvalidUserOriginStatus: {Title: "Invalid User State", Detail: "User resource state conflicts with request.", StatusCode: 409},
	ErrKeyInvalidQueryParam:       {Title: "Bad Request.", Detail: "Invalid query param(s) passed.", StatusCode: 400},
	ErrKeyPageOutOfRange:          {Title: "Bad Request.", Detail: "Page out of range.", StatusCode: 400},
	ErrKeyResourceConflict:        {Title: "User registered on system.", StatusCode: 409},
	ErrKeyResourceNotFound:        {Title: "User resource not found.", StatusCode: 404},
}
