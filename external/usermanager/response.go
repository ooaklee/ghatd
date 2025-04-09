package usermanager

import (
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/user"
)

// GetUserMicroProfileResponse holds response data for GetUserMicroProfile request
type GetUserMicroProfileResponse struct {
	*user.GetMicroProfileResponse
}

// GetUserProfileResponse holds response data for GetUserProfile request
type GetUserProfileResponse struct {
	*user.GetProfileResponse
}

// UpdateUserProfileResponse holds response data for UpdateUserProfile request
type UpdateUserProfileResponse struct {
	*user.UpdateUserResponse
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
