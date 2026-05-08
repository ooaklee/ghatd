package user

import (
	"net/http"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
)

// MapRequestToGetProfileRequest maps incoming GetUserProfile request to correct
// struct.
func MapRequestToGetProfileRequest(request *http.Request, validator UserValidator) (*GetProfileRequest, error) {
	var err error
	parsedRequest := &GetProfileRequest{}

	// get user id from uri
	parsedRequest.Id, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, ErrInvalidUserID
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidUserID
	}

	return parsedRequest, nil
}

// MapRequestToGetMicroProfileRequest maps incoming GetUserByID request to correct
// struct.
func MapRequestToGetMicroProfileRequest(request *http.Request, validator UserValidator) (*GetMicroProfileRequest, error) {
	var err error
	parsedRequest := &GetMicroProfileRequest{}

	// get user id from uri
	parsedRequest.Id, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, ErrInvalidUserID
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidUserID
	}

	return parsedRequest, nil
}

// MapRequestToGetUsersRequest maps incoming GetUsers request to correct
// struct.
func MapRequestToGetUsersRequest(request *http.Request, validator UserValidator) (*GetUsersRequest, error) {

	var err error
	parsedRequest := &GetUsersRequest{}

	// get request queries
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(parsedRequest)
	if err != nil {
		return nil, ErrInvalidQueryParam
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrInvalidQueryParam
	}

	return parsedRequest, nil
}

// MapRequestToCreateUserRequest maps incoming CreateUser request to correct
// struct.
func MapRequestToCreateUserRequest(request *http.Request, validator UserValidator) (*CreateUserRequest, error) {
	parsedRequest := &CreateUserRequest{}

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, ErrInvalidUserBody
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidUserBody
	}

	return parsedRequest, nil
}

// validateParsedRequest validates based on tags. On failure an error is returned
func validateParsedRequest(request interface{}, validator UserValidator) error {
	return validator.Validate(request)
}

// MapRequestToGetUserByIdRequest maps incoming GetUserByID request to correct
// struct.
func MapRequestToGetUserByIdRequest(request *http.Request, validator UserValidator) (*GetUserByIdRequest, error) {
	var err error
	parsedRequest := &GetUserByIdRequest{}

	// get user id from uri
	parsedRequest.Id, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, ErrInvalidUserID
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidUserID
	}

	return parsedRequest, nil
}

// MapRequestToUpdateUserRequest maps incoming UpdateUser request to corresponding
// struct
func MapRequestToUpdateUserRequest(request *http.Request, validator UserValidator) (*UpdateUserRequest, error) {
	var err error
	parsedRequest := &UpdateUserRequest{}

	// get user id from uri
	parsedRequest.Id, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, ErrInvalidUserID
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, ErrInvalidUserBody
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidUserBody
	}

	return parsedRequest, nil
}

// MapRequestToDeleteUserRequest maps incoming DeleteUser request to correct
// struct.
func MapRequestToDeleteUserRequest(request *http.Request, validator UserValidator) (*DeleteUserRequest, error) {
	var err error
	parsedRequest := &DeleteUserRequest{}

	// get user id from uri
	parsedRequest.Id, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, ErrInvalidUserID
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidUserID
	}

	return parsedRequest, nil
}
