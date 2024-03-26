package auth

import (
	"github.com/ooaklee/reply"
)

// AuthErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// Use https://docs.microsoft.com/en-us/troubleshoot/iis/http-status-code to expand messages i.e. AccessDenied1
var AuthErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyUnauthorized:                             {Title: "Unauthorized", StatusCode: 401},
	ErrKeyUnauthorizedNoTokenUUID:                  {Title: "Unauthorized", Code: "1", StatusCode: 401},
	ErrKeyUnauthorizedNoUserIDFound:                {Title: "Unauthorized", Code: "2", StatusCode: 401},
	ErrKeyUnauthorizedNoAdminInfoFound:             {Title: "Unauthorized", Code: "3", StatusCode: 401},
	ErrKeyUnauthorizedNoAuthorizationInfoFound:     {Title: "Unauthorized", Code: "4", StatusCode: 401},
	ErrKeyUnauthorizedRefreshTokenExpired:          {Title: "Unauthorized", Code: "5", StatusCode: 401},
	ErrKeyUnauthorizedParsedStringTokenExpired:     {Title: "Unauthorized", Code: "6", StatusCode: 401},
	ErrKeyUnauthorizedTokenUnexpectedSigningMethod: {Title: "Unauthorized", Code: "7", StatusCode: 401},
	ErrKeyUnauthorizedParsedStringUnknown:          {Title: "Unauthorized", Code: "8", StatusCode: 401},
	ErrKeyUnauthorizedMalformattedToken:            {Title: "Unauthorized", Code: "9", StatusCode: 401},
	ErrKeyNoBearerHeaderFound:                      {Title: "Unauthorized", Code: "10", StatusCode: 401},
}
