package auth

import "time"

const (
	// tokenHeaderKeyAlg holds the value for the algorithm header key
	tokenHeaderKeyAlg = "alg"

	// tokenClaimKeySub holds the value for sub key
	tokenClaimKeySub = "sub"

	// tokenClaimKeyAuthorized holds the value for authorized key
	tokenClaimKeyAuthorized = "authorized"

	// tokenClaimKeyAccessUUID holds the value for access UUID key
	tokenClaimKeyAccessUUID = "access_uuid"

	// tokenClaimKeyAdmin holds the value for admin key
	tokenClaimKeyAdmin = "admin"

	// tokenClaimKeyExp holds the value for Exp key (expiry)
	tokenClaimKeyExp = "exp"

	// tokenClaimKeyRefreshUUID holds the value for refresh UUID key
	tokenClaimKeyRefreshUUID = "refresh_uuid"

	// httpHeaderKeyAuthorization returns the key used for http authorisation
	httpHeaderKeyAuthorization = "Authorization"

	// userStatusKeyActivate holds the key that controls whether use is authorised
	// base on it value ( its based on user's status at time of token generation)
	userStatusKeyForAuthorisation = "ACTIVE"

	// accesstokenDefaultTTL holds the default time to live to apply to access
	// tokens
	accesstokenDefaultTTL time.Duration = time.Minute * 15

	// refreshtokenDefaultTTL holds the default time to live to apply to refresh
	// tokens
	refreshtokenDefaultTTL time.Duration = time.Hour * 24 * 7

	// initialTokenDefaultTTL holds the default time to live to apply to initial
	// token
	initialTokenDefaultTTL time.Duration = time.Minute * 5

	// emailVerificationTokenDefaultTTL holds the default time to live to apply to email
	// verification token
	emailVerificationTokenDefaultTTL time.Duration = time.Minute * 10
)

const (

	// ErrKeyUnauthorized returned when token is not valid
	ErrKeyUnauthorized = "Unauthorized"

	// ErrKeyUnauthorizedNoTokenUUID [code: 1] returned when token UUID is not found in claim
	ErrKeyUnauthorizedNoTokenUUID = "UnauthorizedNoTokenUUID"

	// ErrKeyUnauthorizedNoUserIDFound [code: 2] returned when user UUID is not found in claim
	ErrKeyUnauthorizedNoUserIDFound = "UnauthorizedNoUserIDFound"

	// ErrKeyUnauthorizedNoAdminInfoFound [code: 3] returned when whether claim belongs to an admin user cannot be found in claim
	ErrKeyUnauthorizedNoAdminInfoFound = "UnauthorizedNoAdminInfoFound"

	// ErrKeyUnauthorizedNoAuthorizationInfoFound [code: 4] returned when whether claim is authorized (user is active) cannot be found in claim
	ErrKeyUnauthorizedNoAuthorizationInfoFound = "UnauthorizedNoAuthorizationInfoFound"

	// ErrKeyUnauthorizedRefreshTokenExpired [code: 5] returned when refresh token is expired
	ErrKeyUnauthorizedRefreshTokenExpired = "UnauthorizedRefreshTokenExpired"

	// ErrKeyUnauthorizedParsedStringTokenExpired [code: 6] returned when parsed string token is expired
	ErrKeyUnauthorizedParsedStringTokenExpired = "UnauthorizedParsedStringTokenExpired"

	// ErrKeyUnauthorizedTokenUnexpectedSigningMethod [code: 7] returned when parsed string token signed using an unexpected method
	ErrKeyUnauthorizedTokenUnexpectedSigningMethod = "UnauthorizedTokenUnexpectedSigningMethod"

	// ErrKeyUnauthorizedParsedStringUnknown [code: 8] returned when parsing string token returned an unknown error
	ErrKeyUnauthorizedParsedStringUnknown = "UnauthorizedParsedStringUnknown"

	// ErrKeyUnauthorizedMalformattedToken [code: 9] returned when token contains invalid segments
	ErrKeyUnauthorizedMalformattedToken = "UnauthorizedMalformattedToken"

	// ErrKeyNoBearerHeaderFound [code: 10] returned when bearer token header is not found in the request
	ErrKeyNoBearerHeaderFound = "NoBearerHeaderFound"
)
