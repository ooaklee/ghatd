package usermanager

import (
	"github.com/ooaklee/ghatd/external/contacter"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// GetUserMicroProfileResponse holds response data for GetUserMicroProfile request
type GetUserMicroProfileResponse struct {
	*userv2.GetUserMicroProfileResponse
}

// GetUserProfileResponse holds response data for GetUserProfile request
type GetUserProfileResponse struct {
	*userv2.GetUserProfileResponse
}

// UpdateUserProfileResponse holds response data for UpdateUserProfile request
type UpdateUserProfileResponse struct {
	*userv2.UpdateUserResponse
}

// CreateCommsResponse holds the response from creating a comms
type CreateCommsResponse struct {
	Comms *contacter.Comms `json:"comms"`
}

// GetCommsResponse holds the response from getting a comms
type GetCommsResponse struct {
	Comms []contacter.Comms      `json:"comms"`
	Meta  map[string]interface{} `json:"-"`
}
