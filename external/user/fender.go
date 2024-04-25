package user

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/toolbox"
)

const (
	GetUsersRequestParameterKeyDefaultOrder   = "order"
	GetUsersRequestParameterValueDefaultOrder = "created_at_desc"

	GetUsersRequestParameterKeyDefaultPerPage   = "per_page"
	GetUsersRequestParameterValueDefaultPerPage = 25

	GetUsersRequestParameterKeyDefaultPage   = "page"
	GetUsersRequestParameterValueDefaultPage = 1

	GetUsersRequestParameterKeyDefaultMeta   = "meta"
	GetUsersRequestParameterValueDefaultMeta = false

	GetUsersRequestParameterKeyDefaultFirstName   = "first_name"
	GetUsersRequestParameterValueDefaultFirstName = ""

	GetUsersRequestParameterKeyDefaultLastName   = "last_name"
	GetUsersRequestParameterValueDefaultLastName = ""

	GetUsersRequestParameterKeyDefaultStatus   = "status"
	GetUsersRequestParameterValueDefaultStatus = ""

	GetUsersRequestParameterKeyDefaultIsAdmin   = "is_admin"
	GetUsersRequestParameterValueDefaultIsAdmin = false

	GetUsersRequestParameterKeyDefaultEmail   = "email"
	GetUsersRequestParameterValueDefaultEmail = ""
)

// MapRequestToGetProfileRequest maps incoming GetUserProfile request to correct
// struct.
func MapRequestToGetProfileRequest(request *http.Request, validator UserValidator) (*GetProfileRequest, error) {
	parsedRequest := &GetProfileRequest{}

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.ID = userID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToGetMicroProfileRequest maps incoming GetUserByID request to correct
// struct.
func MapRequestToGetMicroProfileRequest(request *http.Request, validator UserValidator) (*GetMicroProfileRequest, error) {
	parsedRequest := &GetMicroProfileRequest{}

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.ID = userID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToGetUsersRequest maps incoming GetUsers request to correct
// struct.
func MapRequestToGetUsersRequest(request *http.Request, validator UserValidator) (*GetUsersRequest, error) {
	parsedRequest := &GetUsersRequest{}

	if order, ok := request.URL.Query()[GetUsersRequestParameterKeyDefaultOrder]; ok {
		parsedRequest.Order = toolbox.StringStandardisedToLower(order[0])
	} else {
		parsedRequest.Order = GetUsersRequestParameterValueDefaultOrder
	}

	if numberOfUsersPerPage, ok := request.URL.Query()[GetUsersRequestParameterKeyDefaultPerPage]; ok {
		parsedRequest.PerPage = toolbox.ConvertStringToIntOrDefault(numberOfUsersPerPage[0], GetUsersRequestParameterValueDefaultPerPage)
	} else {
		parsedRequest.PerPage = GetUsersRequestParameterValueDefaultPerPage
	}

	if numberOfPages, ok := request.URL.Query()[GetUsersRequestParameterKeyDefaultPage]; ok {
		parsedRequest.Page = toolbox.ConvertStringToIntOrDefault(numberOfPages[0], GetUsersRequestParameterValueDefaultPage)
	} else {
		parsedRequest.Page = GetUsersRequestParameterValueDefaultPage
	}

	if responseMeta, ok := request.URL.Query()[GetUsersRequestParameterKeyDefaultMeta]; ok {
		parsedRequest.Meta = toolbox.ConvertToBoolean(responseMeta[0])
	} else {
		parsedRequest.Meta = GetUsersRequestParameterValueDefaultMeta
	}

	if responseFirstName, ok := request.URL.Query()[GetUsersRequestParameterKeyDefaultFirstName]; ok {
		parsedRequest.FirstName = toolbox.StringStandardisedToLower(responseFirstName[0])
	} else {
		parsedRequest.FirstName = GetUsersRequestParameterValueDefaultFirstName
	}

	if responseLastName, ok := request.URL.Query()[GetUsersRequestParameterKeyDefaultLastName]; ok {
		parsedRequest.LastName = toolbox.StringStandardisedToLower(responseLastName[0])
	} else {
		parsedRequest.LastName = GetUsersRequestParameterValueDefaultLastName
	}

	if responseStatus, ok := request.URL.Query()[GetUsersRequestParameterKeyDefaultStatus]; ok {
		parsedRequest.Status = toolbox.StringStandardisedToUpper(responseStatus[0])
	} else {
		parsedRequest.Status = GetUsersRequestParameterValueDefaultStatus
	}

	if responseIsAdmin, ok := request.URL.Query()[GetUsersRequestParameterKeyDefaultIsAdmin]; ok {
		parsedRequest.IsAdmin = toolbox.ConvertToBoolean(responseIsAdmin[0])
	} else {
		parsedRequest.IsAdmin = GetUsersRequestParameterValueDefaultIsAdmin
	}

	if responseEmail, ok := request.URL.Query()[GetUsersRequestParameterKeyDefaultEmail]; ok {
		parsedRequest.Email = toolbox.StringStandardisedToLower(responseEmail[0])
	} else {
		parsedRequest.Email = GetUsersRequestParameterValueDefaultEmail
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	return parsedRequest, nil
}

// MapRequestToCreateUserRequest maps incoming CreateUser request to correct
// struct.
func MapRequestToCreateUserRequest(request *http.Request, validator UserValidator) (*CreateUserRequest, error) {
	parsedRequest := &CreateUserRequest{}

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// validateParsedRequest validates based on tags. On failure an error is returned
func validateParsedRequest(request interface{}, validator UserValidator) error {
	return validator.Validate(request)
}

// MapRequestToGetUserByIDRequest maps incoming GetUserByID request to correct
// struct.
func MapRequestToGetUserByIDRequest(request *http.Request, validator UserValidator) (*GetUserByIDRequest, error) {
	parsedRequest := &GetUserByIDRequest{}

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.ID = userID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// getUserIDFromURI pulls userID from URI. If fails, returns error
func getUserIDFromURI(request *http.Request) (string, error) {
	var userID string

	if userID = mux.Vars(request)[UserURIVariableID]; userID == "" {
		return "", errors.New(ErrKeyInvalidUserID)
	}

	return userID, nil
}

// MapRequestToUpdateUserRequest maps incoming UpdateUser request to corresponding
// struct
func MapRequestToUpdateUserRequest(request *http.Request, validator UserValidator) (*UpdateUserRequest, error) {
	parsedRequest := &UpdateUserRequest{}

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.ID = userID

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if parsedRequest.FirstName == "" && parsedRequest.LastName == "" || parsedRequest.FirstName != "" && len(parsedRequest.FirstName) == 1 || parsedRequest.LastName != "" && len(parsedRequest.LastName) == 1 {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// MapRequestToDeleteUserRequest maps incoming DeleteUser request to correct
// struct.
func MapRequestToDeleteUserRequest(request *http.Request, validator UserValidator) (*DeleteUserRequest, error) {
	parsedRequest := &DeleteUserRequest{}

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.ID = userID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}
