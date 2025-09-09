package accessmanager

import (
	"github.com/ooaklee/reply"
)

// AccessmanagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// TODO: remove nolint
// nolint will be used later
var AccessmanagerErrorMap reply.ErrorManifest = map[string]reply.ErrorManifestItem{
	ErrKeyBadRequest:                                          {Title: "Bad Request", StatusCode: 400, Code: "AM00-001"},
	ErrKeyInvalidUserBody:                                     {Title: "Bad Request", Detail: "Check submitted user information", StatusCode: 400, Code: "AM00-002"},
	ErrKeyInvalidVerificationToken:                            {Title: "Bad Request", Detail: "User token missing or malformatted", StatusCode: 400, Code: "AM00-003"},
	ErrKeyInvalidRefreshToken:                                 {Title: "Bad Request", Detail: "Refresh token missing or malformatted", StatusCode: 400, Code: "AM00-004"},
	ErrKeyInvalidUserEmail:                                    {Title: "Bad Request", Detail: "User email address missing or malformatted", StatusCode: 400, Code: "AM00-005"},
	ErrKeyConflictingUserState:                                {Title: "Conflict", Detail: "User in conflicting state for requested action", StatusCode: 409, Code: "AM00-006"},
	ErrKeyUserStatusUncaught:                                  {Title: "Conflict", Detail: "Conflict was detected. Please contact support", StatusCode: 409, Code: "AM00-007"},
	ErrKeyUnauthorizedRefreshTokenCacheDeletionFailure:        {Title: "Unauthorized", StatusCode: 401, Code: "AM00-008"},
	ErrKeyUnauthorizedAccessTokenCacheDeletionFailure:         {Title: "Unauthorized", StatusCode: 401, Code: "AM00-009"},
	ErrKeyUnauthorizedAdminAccessAttempted:                    {Title: "Unauthorized", StatusCode: 401, Code: "AM00-010"},
	ErrKeyUnauthorizedNonActiveStatus:                         {Title: "Unauthorized", StatusCode: 401, Code: "AM00-011"},
	ErrKeyUnauthorizedTokenNotFoundInStore:                    {Title: "Unauthorized", StatusCode: 401, Code: "AM00-012"},
	ErrKeyUnauthorizedUnableToAttainRequestorID:               {Title: "Unauthorized", StatusCode: 401, Code: "AM00-013"},
	ErrKeyInvalidUserID:                                       {Title: "Bad Request", Detail: "User ID missing or malformatted", StatusCode: 400, Code: "AM00-014"},
	ErrKeyInvalidAPITokenID:                                   {Title: "Bad Request", Detail: "API token ID missing or malformatted", StatusCode: 400, Code: "AM00-015"},
	ErrKeyForbiddenUnableToAction:                             {Title: "Forbidden", StatusCode: 403, Code: "AM00-016"},
	ErrKeyPermanentAPITokenLimitReached:                       {Title: "Permanent token limit reached.", StatusCode: 409, Code: "AM00-017"},
	ErrKeyEphemeralAPITokenLimitReached:                       {Title: "Ephemeral token limit reached.", StatusCode: 409, Code: "AM00-018"},
	ErrKeyAPITokenNotAssociatedWithUser:                       {Title: "Token association not found.", StatusCode: 404, Code: "AM00-019"},
	ErrKeyInvalidCreateUserAPITokenBody:                       {Title: "Token creation body malformatted", StatusCode: 400, Code: "AM00-020"},
	ErrKeyCreateUserAPITokenRequestTtlTooShort:                {Title: "Minimum allowed time to live permitted by your role breached", StatusCode: 400, Code: "AM00-021"},
	ErrKeyCreateUserAPITokenRequestTtlTooLong:                 {Title: "Maximimum allowed time to live permitted by your role exceeded", StatusCode: 400, Code: "AM00-022"},
	ErrKeyCreateUserAPITokenRequestTtlOutsideAllowedIncrement: {Title: "Requested Time to live is not within the allowed increment permitted by your role", StatusCode: 400, Code: "AM00-023"},
	ErrKeyInvalidLogOutUserOthersRequest:                      {Title: "Bad request to log off other devices", StatusCode: 400, Code: "AM00-024"},
	ErrKeyInvalidAuthToken:                                    {Title: "Invalid authorization", StatusCode: 401, Code: "AM00-025"},
	ErrKeyInvalidResultQueryParam:                             {Title: "Invalid result query param", StatusCode: 400, Code: "AM00-026"},
}
