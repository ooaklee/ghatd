package apitoken

// GetAPITokensRequest holds all the data needed to action request
type GetAPITokensRequest struct {
	// Order defines how should response be sorted. Default: newest -> oldest (created_at_desc)
	// Valid options: created_at_asc, created_at_desc, last_used_at_asc, last_used_at_desc
	// updated_at_asc, updated_at_desc
	Order string

	// Total number of apitokens to return per page, if available. Default 25.
	// Accepts anything between 1 and 100
	PerPage int

	// Page specifies the page results should be taken from. Default 1.
	Page int

	// Meta whether response should contain meta information
	Meta bool

	// CreatedByID specifies the user ID results should be linked / matched to
	CreatedByID string

	// Description specifies the description results should be like / match
	Description string

	// Status specified the statuses apitokens in response must be
	// Valid options: active, revoked
	Status string
}

// GetAPITokenRequest holds everything needed for GetAPIToken request
type GetAPITokenRequest struct {
	// ID the apittoken's UUID
	ID string
}

// GetAPITokensForRequest holds everything needed for GetAPITokensFor request
type GetAPITokensForRequest struct {
	// Order defines how should response be sorted. Default: newest -> oldest (created_at_desc)
	// Valid options: created_at_asc, created_at_desc, last_used_at_asc, last_used_at_desc
	// updated_at_asc, updated_at_desc
	Order string

	// ID the user's UUID
	ID string

	// NanoId is the User's nanoId
	NanoId string
}

// DeleteAPITokenRequest holds everything needed for DeleteAPIToken request
type DeleteAPITokenRequest struct {
	// APITokenID the apittoken's UUID
	APITokenID string

	// UserID the user's UUID
	UserID string
}

// updateAPITokenRequest holds everything expected for updating an User API token
type updateAPITokenRequest struct {
	APITokenID string `json:"api_token_id"`
	Status     string
}

// ActivateAPITokenRequest holds everything needed for ActivateAPIToken request
type ActivateAPITokenRequest struct {
	// ID the api token's UUID
	ID string
}

// RevokeAPITokenRequest holds everything needed for RevokeAPIToken request
type RevokeAPITokenRequest struct {
	// ID the api token's UUID
	ID string
}

// UpdateAPITokenLastUsedAtRequest holds everything needed for UpdateAPITokenLastUsedAt request
type UpdateAPITokenLastUsedAtRequest struct {
	// APITokenEncoded the secret passed by user, encoded.
	APITokenEncoded []byte

	// ClientID the user's UUID
	ClientID string
}

// CreateAPITokenRequest holds everything needed for creating an user API token
type CreateAPITokenRequest struct {
	// UserID the user's UUID
	UserID string

	// UserNanoId the user's nano id
	UserNanoId string

	// TokenTtl is how long the token is valid for
	// if 0 - forever
	// as seconds
	TokenTtl int64
}

// analyseTokenTTLDataRequest is holding attributes need to assess
// token's TTL
type AnalyseTokenTTLDataRequest struct {
	ApiTokens []UserAPIToken
}

// GetTotalApiTokensRequest is holding attributes need to get API
type GetTotalApiTokensRequest struct {
	// UserId the user's UUID
	UserId string

	To string

	From string

	OnlyEphemeral bool

	OnlyPermanent bool
}
