package user

// CreateUserResponse holds response data for CreateUserResponse request
type CreateUserResponse struct {
	User User
}

// GetUsersResponse returns data required all the data needed to act
type GetUsersResponse struct {
	Users []User

	// Total - number of users found
	Total int

	// TotalPages pages available
	TotalPages int

	UsersPerPage int

	// Page specifies the page results should be taken from. Default 1.
	Page int
}

func (g *GetUsersResponse) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap[ResponseMetaKeyUsersPerPage] = g.UsersPerPage
	responseMap[ResponseMetaKeyTotalUsers] = g.Total
	responseMap[ResponseMetaKeyTotalPages] = g.TotalPages
	responseMap[ResponseMetaKeyPage] = g.Page

	return responseMap
}

// GetUserByIDResponse holds response data for GetUserByID request
type GetUserByIDResponse struct {
	User User
}

// UpdateUserResponse holds response data for UpdateUser request
type UpdateUserResponse struct {
	User User
}

// GetUserByEmailResponse holds response data for GetUserByEmail request
type GetUserByEmailResponse struct {
	User User
}

// GetMicroProfileResponse holds response data for GetMicroProfile request
type GetMicroProfileResponse struct {
	MicroProfile UserMicroProfile
}

// GetProfileResponse holds response data for GetProfile request
type GetProfileResponse struct {
	Profile UserProfile
}

// GetUsersPaginationResponse is the pagination response
type GetUsersPaginationResponse struct {
	// Resources is the collection of the resource to paginate
	Resources []User

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
