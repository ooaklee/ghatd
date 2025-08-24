package accessmanager

import (
	"github.com/ooaklee/reply/v2"
)

// AccessmanagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// TODO: remove nolint
// nolint will be used later
var AccessmanagerErrorMap reply.ErrorManifest = map[error]reply.ErrorManifestItem{
	ErrBadRequest:                                          {Title: "Bad Request", StatusCode: 400, Code: "AM00-001"},
	ErrInvalidUserBody:                                     {Title: "Bad Request", Detail: "Check submitted user information", StatusCode: 400, Code: "AM00-002"},
	ErrInvalidVerificationToken:                            {Title: "Bad Request", Detail: "User token missing or malformatted", StatusCode: 400, Code: "AM00-003"},
	ErrInvalidRefreshToken:                                 {Title: "Bad Request", Detail: "Refresh token missing or malformatted", StatusCode: 400, Code: "AM00-004"},
	ErrInvalidUserEmail:                                    {Title: "Bad Request", Detail: "User email address missing or malformatted", StatusCode: 400, Code: "AM00-005"},
	ErrConflictingUserState:                                {Title: "Conflict", Detail: "User in conflicting state for requested action", StatusCode: 409, Code: "AM00-006"},
	ErrUserStatusUncaught:                                  {Title: "Conflict", Detail: "Conflict was detected. Please contact support", StatusCode: 409, Code: "AM00-007"},
	ErrUnauthorizedRefreshTokenCacheDeletionFailure:        {Title: "Unauthorized", StatusCode: 401, Code: "AM00-008"},
	ErrUnauthorizedAccessTokenCacheDeletionFailure:         {Title: "Unauthorized", StatusCode: 401, Code: "AM00-009"},
	ErrUnauthorizedAdminAccessAttempted:                    {Title: "Unauthorized", StatusCode: 401, Code: "AM00-010"},
	ErrUnauthorizedNonActiveStatus:                         {Title: "Unauthorized", StatusCode: 401, Code: "AM00-011"},
	ErrUnauthorizedTokenNotFoundInStore:                    {Title: "Unauthorized", StatusCode: 401, Code: "AM00-012"},
	ErrUnauthorizedUnableToAttainRequestorID:               {Title: "Unauthorized", StatusCode: 401, Code: "AM00-013"},
	ErrInvalidUserID:                                       {Title: "Bad Request", Detail: "User ID missing or malformatted", StatusCode: 400, Code: "AM00-014"},
	ErrInvalidAPITokenID:                                   {Title: "Bad Request", Detail: "API token ID missing or malformatted", StatusCode: 400, Code: "AM00-015"},
	ErrForbiddenUnableToAction:                             {Title: "Forbidden", StatusCode: 403, Code: "AM00-016"},
	ErrPermanentAPITokenLimitReached:                       {Title: "Permanent token limit reached.", StatusCode: 409, Code: "AM00-017"},
	ErrEphemeralAPITokenLimitReached:                       {Title: "Ephemeral token limit reached.", StatusCode: 409, Code: "AM00-018"},
	ErrAPITokenNotAssociatedWithUser:                       {Title: "Token association not found.", StatusCode: 404, Code: "AM00-019"},
	ErrInvalidCreateUserAPITokenBody:                       {Title: "Token creation body malformatted", StatusCode: 400, Code: "AM00-020"},
	ErrCreateUserAPITokenRequestTtlTooShort:                {Title: "Minimum allowed time to live permitted by your role breached", StatusCode: 400, Code: "AM00-021"},
	ErrCreateUserAPITokenRequestTtlTooLong:                 {Title: "Maximimum allowed time to live permitted by your role exceeded", StatusCode: 400, Code: "AM00-022"},
	ErrCreateUserAPITokenRequestTtlOutsideAllowedIncrement: {Title: "Requested Time to live is not within the allowed increment permitted by your role", StatusCode: 400, Code: "AM00-023"},
	ErrInvalidLogOutUserOthersRequest:                      {Title: "Bad request to log off other devices", StatusCode: 400, Code: "AM00-024"},
	ErrInvalidAuthToken:                                    {Title: "Invalid authorization", StatusCode: 401, Code: "AM00-025"},
	ErrInvalidResultQueryParam:                             {Title: "Invalid result query param", StatusCode: 400, Code: "AM00-026"},
}
