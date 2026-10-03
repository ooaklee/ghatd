package accessmanager

import (
	"net/http"

	"github.com/ooaklee/ghatd/external/oauth"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

// AccessmanagerErrorMap holds Error keys, their corresponding human-friendly message, and response status code
// TODO: remove nolint
// nolint will be used later
var AccessmanagerErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrRefreshTemporarilyUnavailable:                       {Title: "Service Unavailable", Detail: "Session refresh is temporarily unavailable", StatusCode: 503, Code: "AM00-040"},
	ErrSessionVerificationUnavailable:                      {Title: "Service Unavailable", Detail: "Session verification is unavailable", StatusCode: 503, Code: "AM00-039"},
	ErrOAuthDisconnectSessionRequired:                      {Title: "Provider disconnected. Sign in with your verified email.", StatusCode: 503, Code: "OAuthDisconnectSessionRequired"},
	oauth.ErrDisconnectProofInvalid:                        {Title: "Invalid or expired verification", StatusCode: 400, Code: "DisconnectProofInvalid"},
	oauth.ErrDisconnectChallengeLocked:                     {Title: "Request a new verification email", StatusCode: 423, Code: "DisconnectChallengeLocked"},
	oauth.ErrDisconnectCooldown:                            {Title: "Wait before requesting another email", StatusCode: 429, Code: "DisconnectCooldown"},
	user.ErrOAuthReplacementEmailRequired:                  {Title: "Verify an independent sign-in email before disconnecting this provider", StatusCode: 409, Code: "OAuthReplacementEmailRequired"},
	user.ErrOAuthConnectionConflict:                        {Title: "Your sign-in methods changed; please try again", StatusCode: 409, Code: "OAuthConnectionConflict"},
	user.ErrEmailAlreadyExists:                             {Title: "This email is already used by another account", StatusCode: 409, Code: "OAuthEmailConflict"},
	ErrOAuthDisconnectDelivery:                             {Title: "Verification email could not be sent", StatusCode: 503, Code: "OAuthDisconnectDeliveryFailed"},
	ErrOAuthDisconnectChallengeNotFound:                    {Title: "This verification request does not exist or has expired", StatusCode: 404, Code: "OAuthDisconnectChallengeNotFound"},
	ErrOAuthReauthenticationRequired:                       {Title: "Sign in again before managing providers", StatusCode: 401, Code: "OAuthReauthenticationRequired"},
	user.ErrOAuthLinkRequired:                              {Title: "Sign in to your existing account before connecting this provider", StatusCode: 409, Code: "OAuthLinkRequired"},
	user.ErrOAuthIdentityConflict:                          {Title: "This provider account is already connected", StatusCode: 409, Code: "OAuthIdentityConflict"},
	user.ErrOAuthRestricted:                                {Title: "This account cannot sign in", StatusCode: 403, Code: "OAuthRestricted"},
	user.ErrOAuthUnsupported:                               {Title: "Provider sign-in is unavailable", StatusCode: 503, Code: "OAuthUnavailable"},
	user.ErrOAuthIndexesRequired:                           {Title: "Provider sign-in is unavailable", StatusCode: 503, Code: "OAuthUnavailable"},
	oauth.ErrSecureProviderIncompleteConfig:                {Title: "Provider sign-in is unavailable", StatusCode: 503, Code: "OAuthUnavailable"},
	oauth.ErrSecureTransactionNotFound:                     {Title: "Sign-in expired; please try again", StatusCode: 400, Code: "OAuthInvalid"},
	oauth.ErrSecureTransactionExpired:                      {Title: "Sign-in expired; please try again", StatusCode: 400, Code: "OAuthInvalid"},
	oauth.ErrSecureTransactionInvalidState:                 {Title: "Invalid sign-in response", StatusCode: 400, Code: "OAuthInvalid"},
	oauth.ErrSecureReturnPathInvalid:                       {Title: "Invalid return path", StatusCode: 400, Code: "OAuthInvalid"},
	oauth.ErrSecureIDTokenInvalid:                          {Title: "Invalid sign-in response", StatusCode: 400, Code: "OAuthInvalid"},
	oauth.ErrSecureIDTokenUnverified:                       {Title: "The provider must verify your email", StatusCode: 403, Code: "OAuthUnverifiedEmail"},
	oauth.ErrProviderCancelled:                             {Title: "Sign-in cancelled", StatusCode: 400, Code: "OAuthCancelled"},
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
	ErrUnauthorizedUnableToAttainRequestorID:               {Title: "Unauthorized", StatusCode: http.StatusUnauthorized, Code: "AM00-013"},
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
	ErrEmptyRefreshToken:                                   {Title: "Unauthorized", StatusCode: 401, Code: "AM00-027"},
	ErrMissingVerificationCredentials:                      {Title: "Bad Request", Detail: "Either a token or code must be provided", StatusCode: 400, Code: "AM00-028"},
	ErrInvalidVerificationCode:                             {Title: "Bad Request", Detail: "The provided verification code is invalid or has expired", StatusCode: 400, Code: "AM00-029"},
	ErrNoOauthProvidersDetected:                            {Title: "Bad Request", Detail: "OAuth login is not configured", StatusCode: 400, Code: "AM00-030"},
	ErrProviderCookieNotFound:                              {Title: "Bad Request", Detail: "OAuth state cookie is missing", StatusCode: 400, Code: "AM00-031"},
	ErrProviderInvalidProtectionStateToken:                 {Title: "Bad Request", Detail: "OAuth state token is invalid", StatusCode: 400, Code: "AM00-032"},
	ErrProvidersPassedNotFound:                             {Title: "Not Found", Detail: "OAuth provider not found", StatusCode: 404, Code: "AM00-033"},
	oauth.ErrProviderCodeNotDetected:                       {Title: "Bad Request", Detail: "OAuth provider code is missing", StatusCode: 400, Code: "AM00-034"},
	oauth.ErrProviderCodeExchangeIncorrect:                 {Title: "Bad Request", Detail: "OAuth provider code exchange failed", StatusCode: 400, Code: "AM00-035"},
	oauth.ErrProviderFailedGettingUserInfo:                 {Title: "Bad Request", Detail: "Unable to get OAuth provider user information", StatusCode: 400, Code: "AM00-036"},
	oauth.ErrProviderFailedToMarshallUserInfo:              {Title: "Bad Request", Detail: "Unable to decode OAuth provider user information", StatusCode: 400, Code: "AM00-037"},
}
