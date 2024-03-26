package auth

// mapAccessTokenClaimsRequest holds attributes needed to make an access token claim
type mapAccessTokenClaimsRequest struct {
	// UserStatus the status of the targetted user
	UserStatus string

	// AccessTokenUUID the ID of the access token
	AccessTokenUUID string

	// UserID the ID of the targetted user
	UserID string

	// IsAdmin whether the targetted user is an admin
	IsAdmin bool

	// AccessTokenTTLSeconds remaining seconds for access
	// token to live
	AccessTokenTTLSeconds int64
}

// mapAccessTokenClaims returns a map used to create token claims
//
// NOTE: Sets claim key tokenClaimKeyAuthorized based on user being `ACTIVE`
func mapAccessTokenClaims(request *mapAccessTokenClaimsRequest) (accessTokenMap map[string]interface{}) {

	accessTokenMap = map[string]interface{}{
		tokenClaimKeyAuthorized: false,
		tokenClaimKeyAccessUUID: request.AccessTokenUUID,
		tokenClaimKeySub:        request.UserID,
		tokenClaimKeyAdmin:      request.IsAdmin,
		tokenClaimKeyExp:        request.AccessTokenTTLSeconds,
	}

	if request.UserStatus == userStatusKeyForAuthorisation {
		accessTokenMap[tokenClaimKeyAuthorized] = true
	}

	return
}
