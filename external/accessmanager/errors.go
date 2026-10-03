package accessmanager

import "errors"

var (
	// ErrRefreshTemporarilyUnavailable means a concurrent rotation did not
	// produce an observable result before this request's bounded wait ended.
	// It does not prove that the credential is invalid or that rotation failed.
	ErrRefreshTemporarilyUnavailable = errors.New("accessmanager/refresh-temporarily-unavailable")
	// ErrSessionVerificationUnavailable rejects incomplete live-session wiring.
	// It is an operational failure, not evidence that a user has lost authority.
	ErrSessionVerificationUnavailable = errors.New("accessmanager/session-verification-unavailable")
	// ErrTokenPolicyUnavailable means configured policy/inventory cannot safely admit creation.
	ErrTokenPolicyUnavailable                              = errors.New("accessmanager/token-policy-unavailable")
	ErrAPITokenNotAssociatedWithUser                       = errors.New(ErrKeyAPITokenNotAssociatedWithUser)
	ErrBadRequest                                          = errors.New(ErrKeyBadRequest)
	ErrConflictingUserState                                = errors.New(ErrKeyConflictingUserState)
	ErrCreateUserAPITokenRequestTtlOutsideAllowedIncrement = errors.New(ErrKeyCreateUserAPITokenRequestTtlOutsideAllowedIncrement)
	ErrCreateUserAPITokenRequestTtlTooLong                 = errors.New(ErrKeyCreateUserAPITokenRequestTtlTooLong)
	ErrCreateUserAPITokenRequestTtlTooShort                = errors.New(ErrKeyCreateUserAPITokenRequestTtlTooShort)
	ErrEmptyRefreshToken                                   = errors.New(ErrKeyEmptyRefreshToken)
	ErrEphemeralAPITokenLimitReached                       = errors.New(ErrKeyEphemeralAPITokenLimitReached)
	ErrForbiddenUnableToAction                             = errors.New(ErrKeyForbiddenUnableToAction)
	ErrInvalidAPITokenID                                   = errors.New(ErrKeyInvalidAPITokenID)
	ErrInvalidAuthToken                                    = errors.New(ErrKeyInvalidAuthToken)
	ErrInvalidCreateUserAPITokenBody                       = errors.New(ErrKeyInvalidCreateUserAPITokenBody)
	ErrInvalidLogOutUserOthersRequest                      = errors.New(ErrKeyInvalidLogOutUserOthersRequest)
	ErrInvalidRefreshToken                                 = errors.New(ErrKeyInvalidRefreshToken)
	ErrInvalidResultQueryParam                             = errors.New(ErrKeyInvalidResultQueryParam)
	ErrInvalidUserBody                                     = errors.New(ErrKeyInvalidUserBody)
	ErrInvalidUserEmail                                    = errors.New(ErrKeyInvalidUserEmail)
	ErrInvalidUserID                                       = errors.New(ErrKeyInvalidUserID)
	ErrInvalidVerificationCode                             = errors.New(ErrKeyInvalidVerificationCode)
	ErrInvalidVerificationToken                            = errors.New(ErrKeyInvalidVerificationToken)
	ErrMissingVerificationCredentials                      = errors.New(ErrKeyMissingVerificationCredentials)
	ErrNoOauthProvidersDetected                            = errors.New(ErrKeyNoOauthProvidersDetected)
	ErrPermanentAPITokenLimitReached                       = errors.New(ErrKeyPermanentAPITokenLimitReached)
	ErrProviderCookieNotFound                              = errors.New(ErrKeyProviderCookieNotFound)
	ErrProviderInvalidProtectionStateToken                 = errors.New(ErrKeyProviderInvalidProtectionStateToken)
	ErrProvidersPassedNotFound                             = errors.New(ErrKeyProvidersPassedNotFound)
	ErrUnauthorizedAccessTokenCacheDeletionFailure         = errors.New(ErrKeyUnauthorizedAccessTokenCacheDeletionFailure)
	ErrUnauthorizedAdminAccessAttempted                    = errors.New(ErrKeyUnauthorizedAdminAccessAttempted)
	ErrUnauthorizedNonActiveStatus                         = errors.New(ErrKeyUnauthorizedNonActiveStatus)
	ErrUnauthorizedRefreshTokenCacheDeletionFailure        = errors.New(ErrKeyUnauthorizedRefreshTokenCacheDeletionFailure)
	ErrUnauthorizedTokenNotFoundInStore                    = errors.New(ErrKeyUnauthorizedTokenNotFoundInStore)
	ErrUnauthorizedUnableToAttainRequestorID               = errors.New(ErrKeyUnauthorizedUnableToAttainRequestorID)
	ErrUserStatusUncaught                                  = errors.New(ErrKeyUserStatusUncaught)
)
