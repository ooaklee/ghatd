package accessmanager

const (
	// ErrKeyBadRequest return when bad request occurs (basic)
	ErrKeyBadRequest = "BadRequest"

	// ErrKeyInvalidUserBody returned when error occurs while decoding user request body
	ErrKeyInvalidUserBody string = "InvalidCreateUserBody"

	// ErrKeyInvalidVerificationToken returned when verification token missing or incorrectly formatted
	ErrKeyInvalidVerificationToken string = "InvalidVerificationToken"

	// ErrKeyInvalidUserEmail returned when errors occurs while decoding email for initial token / verification
	// request
	ErrKeyInvalidUserEmail string = "KeyInvalidUserEmail"

	// ErrKeyUserStatusUncaught returned when user status in an unexpected status, while the user attempts to initiate
	// the log in flow.
	ErrKeyUserStatusUncaught string = "UserStatusUncaught"

	// ErrKeyInvalidRefreshToken returned when Refresh token missing or incorrectly formatted
	ErrKeyInvalidRefreshToken string = "InvalidRefreshToken"

	// ErrKeyUnauthorizedRefreshTokenCacheDeletionFailure [code: 100] returned when refresh token fails to delete from ephemeral store
	ErrKeyUnauthorizedRefreshTokenCacheDeletionFailure = "UnauthorizedRefreshTokenCacheDeletionFailure"

	// ErrKeyUnauthorizedAccessTokenCacheDeletionFailure [code: 101] returned when access token fails to delete from ephemeral store
	ErrKeyUnauthorizedAccessTokenCacheDeletionFailure = "UnauthorizedAccessTokenCacheDeletionFailure"

	// ErrKeyUnauthorizedAdminAccessAttempted [code: 102] returned when non admin user attempts to access an admin only endpoint
	ErrKeyUnauthorizedAdminAccessAttempted = "UnauthorizedAdminAccessAttempted"

	// ErrKeyUnauthorizedNonActiveStatus [code: 103] returned when a non `ACTIVE` status user attempts to access an endpoint only available
	// to those that completely active their account
	ErrKeyUnauthorizedNonActiveStatus = "UnauthorizedNonActiveStatus"

	// ErrKeyUnauthorizedTokenNotFoundInStore [code: 104] returned when token cannot be found in ephemeral store
	ErrKeyUnauthorizedTokenNotFoundInStore = "UnauthorizedTokenNotFoundInStore"

	// ErrKeyUnauthorizedUnableToAttainRequestorID [code: 105] returned when expected ID for requester cannot be found
	ErrKeyUnauthorizedUnableToAttainRequestorID = "UnauthorizedUnableToAttainRequestorID"

	// ErrKeyForbiddenUnableToAction [code: 100] returned when requestor not authorised to carry out requested action
	ErrKeyForbiddenUnableToAction = "ForbiddenUnableToAction"

	// ErrKeyInvalidUserID returned when user ID missing or incorrectly formatted
	ErrKeyInvalidUserID = "InvalidUserID"

	// ErrKeyInvalidAPITokenID returned when api token ID missing or incorrectly formatted
	ErrKeyInvalidAPITokenID = "InvalidAPITokenID"

	// ErrKeyEphemeralAPITokenLimitReached returned when user attempts to create more ephemeral api tokens than their role allows
	ErrKeyEphemeralAPITokenLimitReached = "EphemeralAPITokenLimitReached"

	// ErrKeyInvalidCreateUserAPITokenBody is return when the body for creating an api token is incorrect
	ErrKeyInvalidCreateUserAPITokenBody = "InvalidCreateUserAPITokenBody"

	// ErrKeyPermanentAPITokenLimitReached returned when user attempts to create more permanent api tokens than their role allows
	ErrKeyPermanentAPITokenLimitReached = "PermanentAPITokenLimitReached"

	// ErrKeyAPITokenNotAssociatedWithUser returned when specified API token cannot not be found in user's collection
	ErrKeyAPITokenNotAssociatedWithUser = "APITokenNotAssociatedWithUser"

	// ErrKeyCreateUserAPITokenRequestTtlTooShort is returned when user is attempting to create a token
	// that shorter than their role allows
	ErrKeyCreateUserAPITokenRequestTtlTooShort = "CreateUserAPITokenRequestTtlTooShort"

	// ErrKeyCreateUserAPITokenRequestTtlTooLong is returned when user is attempting to create a token
	// that longer than their role allows
	ErrKeyCreateUserAPITokenRequestTtlTooLong = "CreateUserAPITokenRequestTtlTooLong"

	// ErrKeyCreateUserAPITokenRequestTtlOutsideAllowedIncrement is returned when user is attempting to create a token
	// that is beyond their allowed increment
	ErrKeyCreateUserAPITokenRequestTtlOutsideAllowedIncrement = "CreateUserAPITokenRequestTtlOutsideAllowedIncrement"

	// ErrKeyInvalidLogOutUserOthersRequest error when a user makes an LogoutUserOthers request and it is invalid
	// missing refresh token or auth token in headers
	ErrKeyInvalidLogOutUserOthersRequest string = "InvalidLogOutUserOthersRequest"

	// ErrKeyInvalidAuthToken error when a user makes a request with invalid auth token
	ErrKeyInvalidAuthToken string = "InvalidAuthToken"
)

const (
	// UserURIVariableID holds the identifier for the user ID in the URI
	UserURIVariableID = "userID"

	// APITokenURIVariableID holds the identifier for the apitoken ID in the URI
	APITokenURIVariableID = "apiTokenID"

	AccessManagerURIVariableID = "blankpackagID"
)

const (
	// CreateUserAPITokenTokenLimit the MAX number of tokens a user can have at any one time (regardless of state)
	CreateUserAPITokenTokenLimit = 3

	// AccessManagerUserTokenStatusKeyRevoked holds api token Revoked state key
	AccessManagerUserTokenStatusKeyRevoked = "REVOKED"

	// AccessManagerUserTokenStatusKeyActive holds api token Active state key
	AccessManagerUserTokenStatusKeyActive = "ACTIVE"
)

const (
	// UserRoleAdmin holds the identifier for user admin role
	UserRoleAdmin = "ADMIN"

	// UserRoleReader holds the identifier for user reader role
	UserRoleReader = "READER"
)
