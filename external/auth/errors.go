package auth

import "errors"

var (
	ErrNoBearerHeaderFound                      = errors.New(ErrKeyNoBearerHeaderFound)
	ErrUnauthorized                             = errors.New(ErrKeyUnauthorized)
	ErrUnauthorizedMalformattedToken            = errors.New(ErrKeyUnauthorizedMalformattedToken)
	ErrUnauthorizedNoAdminInfoFound             = errors.New(ErrKeyUnauthorizedNoAdminInfoFound)
	ErrUnauthorizedNoAuthorizationInfoFound     = errors.New(ErrKeyUnauthorizedNoAuthorizationInfoFound)
	ErrUnauthorizedNoTokenUUID                  = errors.New(ErrKeyUnauthorizedNoTokenUUID)
	ErrUnauthorizedNoUserIDFound                = errors.New(ErrKeyUnauthorizedNoUserIDFound)
	ErrUnauthorizedParsedStringTokenExpired     = errors.New(ErrKeyUnauthorizedParsedStringTokenExpired)
	ErrUnauthorizedParsedStringUnknown          = errors.New(ErrKeyUnauthorizedParsedStringUnknown)
	ErrUnauthorizedRefreshTokenExpired          = errors.New(ErrKeyUnauthorizedRefreshTokenExpired)
	ErrUnauthorizedTokenUnexpectedSigningMethod = errors.New(ErrKeyUnauthorizedTokenUnexpectedSigningMethod)
)
