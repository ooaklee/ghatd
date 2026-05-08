package user

import (
	"github.com/ooaklee/reply/v2"
)

// UserErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// Use https://docs.microsoft.com/en-us/troubleshoot/iis/http-status-code to expand messages i.e. AccessDenied1
var UserErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrInvalidUserBody:         {Title: "Bad Request", Detail: "Check submitted user information.", StatusCode: 400, Code: "U0-001"},
	ErrInvalidUserID:           {Title: "Bad Request", Detail: "User ID missing or malformatted.", StatusCode: 400, Code: "U0-002"},
	ErrUserNeverActivated:      {Title: "Invalid User State", Detail: "User resource state conflicts with request.", StatusCode: 409, Code: "U0-003"},
	ErrInvalidUserOriginStatus: {Title: "Invalid User State", Detail: "User resource state conflicts with request.", StatusCode: 409, Code: "U0-004"},
	ErrInvalidQueryParam:       {Title: "Bad Request.", Detail: "Invalid query param(s) passed.", StatusCode: 400, Code: "U0-005"},
	ErrPageOutOfRange:          {Title: "Bad Request.", Detail: "Page out of range.", StatusCode: 400, Code: "U0-006"},
	ErrResourceConflict:        {Title: "Conflict", Detail: "User already exists on system.", StatusCode: 409, Code: "U0-007"},
	ErrResourceNotFound:        {Title: "Not Found", Detail: "User resource not found.", StatusCode: 404, Code: "U0-008"},
	ErrNoChangesDetected:       {Title: "Bad Request", Detail: "No changes detected.", StatusCode: 400, Code: "U0-009"},
}
