package accessmanager

import "errors"

var (

	// ErrConflictingUserState returned when user state is in an conflicting state for the requested
	// action (i.e. user is already active, email is already verified etc.)
	ErrConflictingUserState = errors.New("ConflictingUserState")

	// ErrBadRequest return when bad request occurs (basic)
	ErrBadRequest = errors.New("BadRequest")

	// ErrInvalidUserBody returned when error occurs while decoding user request body
	ErrInvalidUserBody = errors.New("InvalidCreateUserBody")

	// ErrInvalidVerificationToken returned when verification token missing or incorrectly formatted
	ErrInvalidVerificationToken = errors.New("InvalidVerificationToken")

	// ErrInvalidUserEmail returned when errors occurs while decoding email for  auth related requests
	// i.e, initial token / verification, changing email etc.
	ErrInvalidUserEmail = errors.New("KeyInvalidUserEmail")

	// ErrUserStatusUncaught returned when user status in an unexpected status, while the user attempts to initiate
	// the log in flow.
	ErrUserStatusUncaught = errors.New("UserStatusUncaught")

	// ErrInvalidRefreshToken returned when Refresh token missing or incorrectly formatted
	ErrInvalidRefreshToken = errors.New("InvalidRefreshToken")

	// ErrUnauthorizedRefreshTokenCacheDeletionFailure [code: 100] returned when refresh token fails to delete from ephemeral store
	ErrUnauthorizedRefreshTokenCacheDeletionFailure = errors.New("UnauthorizedRefreshTokenCacheDeletionFailure")

	// ErrUnauthorizedAccessTokenCacheDeletionFailure [code: 101] returned when access token fails to delete from ephemeral store
	ErrUnauthorizedAccessTokenCacheDeletionFailure = errors.New("UnauthorizedAccessTokenCacheDeletionFailure")

	// ErrUnauthorizedAdminAccessAttempted [code: 102] returned when non admin user attempts to access an admin only endpoint
	ErrUnauthorizedAdminAccessAttempted = errors.New("UnauthorizedAdminAccessAttempted")

	// ErrUnauthorizedNonActiveStatus [code: 103] returned when a non `ACTIVE` status user attempts to access an endpoint only available
	// to those that completely active their account
	ErrUnauthorizedNonActiveStatus = errors.New("UnauthorizedNonActiveStatus")

	// ErrUnauthorizedTokenNotFoundInStore [code: 104] returned when token cannot be found in ephemeral store
	ErrUnauthorizedTokenNotFoundInStore = errors.New("UnauthorizedTokenNotFoundInStore")

	// ErrUnauthorizedUnableToAttainRequestorID [code: 105] returned when expected ID for requester cannot be found
	ErrUnauthorizedUnableToAttainRequestorID = errors.New("UnauthorizedUnableToAttainRequestorID")

	// ErrForbiddenUnableToAction [code: 100] returned when requestor not authorised to carry out requested action
	ErrForbiddenUnableToAction = errors.New("ForbiddenUnableToAction")

	// ErrInvalidUserID returned when user ID missing or incorrectly formatted
	ErrInvalidUserID = errors.New("InvalidUserID")

	// ErrInvalidAPITokenID returned when api token ID missing or incorrectly formatted
	ErrInvalidAPITokenID = errors.New("InvalidAPITokenID")

	// ErrEphemeralAPITokenLimitReached returned when user attempts to create more ephemeral api tokens than their role allows
	ErrEphemeralAPITokenLimitReached = errors.New("EphemeralAPITokenLimitReached")

	// ErrInvalidCreateUserAPITokenBody is return when the body for creating an api token is incorrect
	ErrInvalidCreateUserAPITokenBody = errors.New("InvalidCreateUserAPITokenBody")

	// ErrPermanentAPITokenLimitReached returned when user attempts to create more permanent api tokens than their role allows
	ErrPermanentAPITokenLimitReached = errors.New("PermanentAPITokenLimitReached")

	// ErrAPITokenNotAssociatedWithUser returned when specified API token cannot not be found in user's collection
	ErrAPITokenNotAssociatedWithUser = errors.New("APITokenNotAssociatedWithUser")

	// ErrCreateUserAPITokenRequestTtlTooShort is returned when user is attempting to create a token
	// that shorter than their role allows
	ErrCreateUserAPITokenRequestTtlTooShort = errors.New("CreateUserAPITokenRequestTtlTooShort")

	// ErrCreateUserAPITokenRequestTtlTooLong is returned when user is attempting to create a token
	// that longer than their role allows
	ErrCreateUserAPITokenRequestTtlTooLong = errors.New("CreateUserAPITokenRequestTtlTooLong")

	// ErrCreateUserAPITokenRequestTtlOutsideAllowedIncrement is returned when user is attempting to create a token
	// that is beyond their allowed increment
	ErrCreateUserAPITokenRequestTtlOutsideAllowedIncrement = errors.New("CreateUserAPITokenRequestTtlOutsideAllowedIncrement")

	// ErrInvalidLogOutUserOthersRequest error when a user makes an LogoutUserOthers request and it is invalid
	// missing refresh token or auth token in headers
	ErrInvalidLogOutUserOthersRequest = errors.New("InvalidLogOutUserOthersRequest")

	// ErrInvalidAuthToken error when a user makes a request with invalid auth token
	ErrInvalidAuthToken = errors.New("InvalidAuthToken")

	// ErrInvalidResultQueryParam error when a user makes a request with invalid query param
	ErrInvalidResultQueryParam = errors.New("InvalidResultQueryParam")
)
