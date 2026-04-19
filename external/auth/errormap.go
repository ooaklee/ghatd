package auth

import (
	"github.com/ooaklee/reply"
)

// AuthErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// Use https://docs.microsoft.com/en-us/troubleshoot/iis/http-status-code to expand messages i.e. AccessDenied1
var AuthErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyUnauthorized:                             {Title: "Unauthorized", Detail: "Invalid or expired credentials", StatusCode: 401, Code: "AUTH0-001"},
	ErrKeyUnauthorizedNoTokenUUID:                  {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-002"},
	ErrKeyUnauthorizedNoUserIDFound:                {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-003"},
	ErrKeyUnauthorizedNoAdminInfoFound:             {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-004"},
	ErrKeyUnauthorizedNoAuthorizationInfoFound:     {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-005"},
	ErrKeyUnauthorizedRefreshTokenExpired:          {Title: "Unauthorized", Detail: "Session has expired, please log in again", StatusCode: 401, Code: "AUTH0-006"},
	ErrKeyUnauthorizedParsedStringTokenExpired:     {Title: "Unauthorized", Detail: "Session has expired, please log in again", StatusCode: 401, Code: "AUTH0-007"},
	ErrKeyUnauthorizedTokenUnexpectedSigningMethod: {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-008"},
	ErrKeyUnauthorizedParsedStringUnknown:          {Title: "Unauthorized", Detail: "Authentication failed", StatusCode: 401, Code: "AUTH0-009"},
	ErrKeyUnauthorizedMalformattedToken:            {Title: "Unauthorized", Detail: "Invalid authentication token", StatusCode: 401, Code: "AUTH0-010"},
	ErrKeyNoBearerHeaderFound:                      {Title: "Unauthorized", Detail: "Missing authentication credentials", StatusCode: 401, Code: "AUTH0-011"},
}
