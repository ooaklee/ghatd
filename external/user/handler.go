package user

import (
	"context"
	"net/http"

	"github.com/ooaklee/reply"
)

type UserService interface {
	CreateUser(ctx context.Context, r *CreateUserRequest) (*CreateUserResponse, error)
	GetUsers(ctx context.Context, r *GetUsersRequest) (*GetUsersResponse, error)
	GetUserByID(ctx context.Context, r *GetUserByIDRequest) (*GetUserByIDResponse, error)
	UpdateUser(ctx context.Context, r *UpdateUserRequest) (*UpdateUserResponse, error)
	DeleteUser(ctx context.Context, r *DeleteUserRequest) error
	GetMicroProfile(ctx context.Context, r *GetMicroProfileRequest) (*GetMicroProfileResponse, error)
	GetProfile(ctx context.Context, r *GetProfileRequest) (*GetProfileResponse, error)
}

// UserValidator expected methods of a valid
type UserValidator interface {
	Validate(s interface{}) error
}

// Handler manages user requests
type Handler struct {
	Service   UserService
	Validator UserValidator
}

// NewHandler returns user handler
func NewHandler(service UserService, validator UserValidator) *Handler {
	return &Handler{
		Service:   service,
		Validator: validator,
	}
}

// GetProfile returns a user profile of the user that matches id
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToGetProfileRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetProfile(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Profile)

}

// GetMicroProfile returns a user micro profile of the user that matches id
func (h *Handler) GetMicroProfile(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToGetMicroProfileRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetMicroProfile(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.MicroProfile)

}

// CreateUser returns reponse from user creation
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToCreateUserRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateUser(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.User)
}

// GetUsers returns all the users
func (h *Handler) GetUsers(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToGetUsersRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	users, err := h.Service.GetUsers(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	returnGetUsersSuccessResponse(w, http.StatusOK, users, request.Meta)
}

// GetUserByID returns an user if it matches id
func (h *Handler) GetUserByID(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToGetUserByIDRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserByID(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)

}

// returnGetUsersSuccessResponse returns the appropiate response dependent on if
// client requested with/without meta
func returnGetUsersSuccessResponse(w http.ResponseWriter, statusCode int, response *GetUsersResponse, withMeta bool) {

	if withMeta {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPDataResponse(w, statusCode, response.Users, reply.WithMeta(response.GetMetaData()))
		return
	}

	//nolint will set up default fallback later
	GetBaseResponseHandler().NewHTTPDataResponse(w, statusCode, response.Users)
}

// UpdateUser returns reponse from user update request
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToUpdateUserRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateUser(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// DeleteUser returns reponse after user delete request
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToDeleteUserRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.DeleteUser(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// GetBaseResponseHandler returns response handler configured with user error map
func GetBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(append([]reply.ErrorManifest{}, UserErrorMap))
}
