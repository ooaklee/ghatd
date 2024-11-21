package accessmanager

import (
	"github.com/ooaklee/reply"
)

// AccessmanagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// TODO: remove nolint
// nolint will be used later
var AccessmanagerErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyBadRequest:                                          {Title: "Bad Request", StatusCode: 400},
	ErrKeyInvalidUserBody:                                     {Title: "Bad Request", Detail: "Check submitted user information", StatusCode: 400},
	ErrKeyInvalidVerificationToken:                            {Title: "Bad Request", Detail: "User token missing or malformatted", StatusCode: 400},
	ErrKeyInvalidRefreshToken:                                 {Title: "Bad Request", Detail: "Refresh token missing or malformatted", StatusCode: 400},
	ErrKeyInvalidUserEmail:                                    {Title: "Bad Request", Detail: "User email address missing or malformatted", StatusCode: 400},
	ErrKeyConflictingUserState:                                {Title: "Conflict", Detail: "User in conflicting state for requested action", StatusCode: 409},
	ErrKeyUserStatusUncaught:                                  {Title: "Conflict", Detail: "Conflict was detected. Please contact support", StatusCode: 409},
	ErrKeyUnauthorizedRefreshTokenCacheDeletionFailure:        {Title: "Unauthorized", Code: "100", StatusCode: 401},
	ErrKeyUnauthorizedAccessTokenCacheDeletionFailure:         {Title: "Unauthorized", Code: "101", StatusCode: 401},
	ErrKeyUnauthorizedAdminAccessAttempted:                    {Title: "Unauthorized", Code: "102", StatusCode: 401},
	ErrKeyUnauthorizedNonActiveStatus:                         {Title: "Unauthorized", Code: "103", StatusCode: 401},
	ErrKeyUnauthorizedTokenNotFoundInStore:                    {Title: "Unauthorized", Code: "104", StatusCode: 401},
	ErrKeyUnauthorizedUnableToAttainRequestorID:               {Title: "Unauthorized", Code: "105", StatusCode: 401},
	ErrKeyInvalidUserID:                                       {Title: "Bad Request", Detail: "User ID missing or malformatted", StatusCode: 400},
	ErrKeyInvalidAPITokenID:                                   {Title: "Bad Request", Detail: "API token ID missing or malformatted", StatusCode: 400},
	ErrKeyForbiddenUnableToAction:                             {Title: "Forbidden", Code: "100", StatusCode: 403},
	ErrKeyPermanentAPITokenLimitReached:                       {Title: "Permanent token limit reached.", StatusCode: 409},
	ErrKeyEphemeralAPITokenLimitReached:                       {Title: "Ephemeral token limit reached.", StatusCode: 409},
	ErrKeyAPITokenNotAssociatedWithUser:                       {Title: "Token association not found.", StatusCode: 404},
	ErrKeyInvalidCreateUserAPITokenBody:                       {Title: "Token creation body malformatted", StatusCode: 400},
	ErrKeyCreateUserAPITokenRequestTtlTooShort:                {Title: "Minimum allowed time to live permitted by your role breached", StatusCode: 400},
	ErrKeyCreateUserAPITokenRequestTtlTooLong:                 {Title: "Maximimum allowed time to live permitted by your role exceeded", StatusCode: 400},
	ErrKeyCreateUserAPITokenRequestTtlOutsideAllowedIncrement: {Title: "Requested Time to live is not within the allowed increment permitted by your role", StatusCode: 400},
	ErrKeyInvalidLogOutUserOthersRequest:                      {Title: "Bad request to log off other devices", StatusCode: 400},
	ErrKeyInvalidAuthToken:                                    {Title: "Invalid authorization", StatusCode: 401},
}
