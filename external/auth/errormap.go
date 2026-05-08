package auth

import (
	"github.com/ooaklee/reply/v2"
)

// AuthErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// Use https://docs.microsoft.com/en-us/troubleshoot/iis/http-status-code to expand messages i.e. AccessDenied1
var AuthErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrUnauthorized:                             {Title: "Unauthorized", Detail: "Invalid or expired credentials", StatusCode: 401, Code: "AUTH0-001"},
	ErrUnauthorizedNoTokenUUID:                  {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-002"},
	ErrUnauthorizedNoUserIDFound:                {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-003"},
	ErrUnauthorizedNoAdminInfoFound:             {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-004"},
	ErrUnauthorizedNoAuthorizationInfoFound:     {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-005"},
	ErrUnauthorizedRefreshTokenExpired:          {Title: "Unauthorized", Detail: "Session has expired, please log in again", StatusCode: 401, Code: "AUTH0-006"},
	ErrUnauthorizedParsedStringTokenExpired:     {Title: "Unauthorized", Detail: "Session has expired, please log in again", StatusCode: 401, Code: "AUTH0-007"},
	ErrUnauthorizedTokenUnexpectedSigningMethod: {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-008"},
	ErrUnauthorizedParsedStringUnknown:          {Title: "Unauthorized", Detail: "Authentication failed", StatusCode: 401, Code: "AUTH0-009"},
	ErrUnauthorizedMalformattedToken:            {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-010"},
	ErrNoBearerHeaderFound:                      {Title: "Unauthorized", Detail: "Missing authentication credentials", StatusCode: 401, Code: "AUTH0-011"},
}
