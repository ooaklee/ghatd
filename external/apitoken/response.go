package apitoken

// GetAPITokensResponse returns data required to action GetAPITokens Response
type GetAPITokensResponse struct {
	APITokens []UserAPIToken

	// Total - number of api tokens found
	Total int

	// TotalPages pages available
	TotalPages int

	APITokensPerPage int

	// Page specifies the page results should be taken from. Default 1.
	Page int
}

// GetAPITokenResponse holds response data for GetAPIToken request
type GetAPITokenResponse struct {
	// APIToken token matching ID
	APIToken UserAPIToken
}

// GetAPITokensForResponse holds response data for GetAPITokensFor request
type GetAPITokensForResponse struct {
	// APITokens tokens belonging to user ID in request
	APITokens []UserAPIToken

	// Total - number of api tokens found
	Total int

	// TotalPages pages available
	TotalPages int

	APITokensPerPage int

	// Page specifies the page results should be taken from. Default 1.
	Page int
}

// updateAPITokenResponse holds response data for  updateAPIToken request
type updateAPITokenResponse struct {
	// APIToken token updated
	APIToken UserAPIToken
}

// CreateAPITokenResponse holds response data for CreateAPIToken request
type CreateAPITokenResponse struct {
	// APIToken created token
	APIToken UserAPIToken
}

// GetAPITokensPaginationResponse is the pagination response
type GetAPITokensPaginationResponse struct {
	// Resources is the collection of the resource to paginate
	Resources []UserAPIToken

	// Total - number of resources found
	Total int

	// TotalPages pages available
	TotalPages int

	// ResourcePerPage is how many many resources
	// are in the page
	ResourcePerPage int

	// Page specifies the page results should be taken from. Default 1.
	Page int
}

// GetAPITokensForPaginationResponse is the pagination response
type GetAPITokensForPaginationResponse struct {
	// Resources is the collection of the resource to paginate
	Resources []UserAPIToken

	// Total - number of resources found
	Total int

	// TotalPages pages available
	TotalPages int

	// ResourcePerPage is how many many resources
	// are in the page
	ResourcePerPage int

	// Page specifies the page results should be taken from. Default 1.
	Page int
}
