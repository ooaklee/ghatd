package user

import "github.com/ooaklee/ghatd/external/toolbox"

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

	// PerPage number of users to be returned per page
	PerPage int

	// Page specifies the page results should be taken from. Default 1.
	Page int
}

func (g *GetUsersResponse) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap[string(toolbox.ResponseMetaKeyResourcePerPage)] = g.PerPage
	responseMap[string(toolbox.ResponseMetaKeyTotalResources)] = g.Total
	responseMap[string(toolbox.ResponseMetaKeyTotalPages)] = g.TotalPages
	responseMap[string(toolbox.ResponseMetaKeyPage)] = g.Page

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
