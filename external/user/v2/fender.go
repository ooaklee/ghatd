package user

import (
	"errors"
	"net/http"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
)

// MapRequestToCreateUserRequest maps incoming CreateUser request to correct struct
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

// MapRequestToGetUserByIDRequest maps incoming GetUserByID request to correct struct
func MapRequestToGetUserByIDRequest(request *http.Request, validator UserValidator) (*GetUserByIDRequest, error) {
	var err error
	parsedRequest := &GetUserByIDRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToGetUserByNanoIDRequest maps incoming GetUserByNanoID request to correct struct
func MapRequestToGetUserByNanoIDRequest(request *http.Request, validator UserValidator) (*GetUserByNanoIDRequest, error) {
	var err error
	parsedRequest := &GetUserByNanoIDRequest{}

	// get nano id from uri
	parsedRequest.NanoID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableNanoID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidNanoID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidNanoID)
	}

	return parsedRequest, nil
}

// MapRequestToGetUserByEmailRequest maps incoming GetUserByEmail request to correct struct
func MapRequestToGetUserByEmailRequest(request *http.Request, validator UserValidator) (*GetUserByEmailRequest, error) {
	parsedRequest := &GetUserByEmailRequest{}

	// get email from query parameter
	email := request.URL.Query().Get("email")
	if email == "" {
		return nil, errors.New(ErrKeyInvalidEmail)
	}

	parsedRequest.Email = email

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidEmail)
	}

	return parsedRequest, nil
}

// MapRequestToUpdateUserRequest maps incoming UpdateUser request to correct struct
func MapRequestToUpdateUserRequest(request *http.Request, validator UserValidator) (*UpdateUserRequest, error) {
	var err error
	parsedRequest := &UpdateUserRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// MapRequestToDeleteUserRequest maps incoming DeleteUser request to correct struct
func MapRequestToDeleteUserRequest(request *http.Request, validator UserValidator) (*DeleteUserRequest, error) {
	var err error
	parsedRequest := &DeleteUserRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToGetUsersRequest maps incoming GetUsers request to correct struct
func MapRequestToGetUsersRequest(request *http.Request, validator UserValidator) (*GetUsersRequest, error) {
	var err error
	parsedRequest := &GetUsersRequest{}

	// get request queries
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	return parsedRequest, nil
}

// MapRequestToUpdateUserStatusRequest maps incoming UpdateUserStatus request to correct struct
func MapRequestToUpdateUserStatusRequest(request *http.Request, validator UserValidator) (*UpdateUserStatusRequest, error) {
	var err error
	parsedRequest := &UpdateUserStatusRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// MapRequestToAddUserRoleRequest maps incoming AddUserRole request to correct struct
func MapRequestToAddUserRoleRequest(request *http.Request, validator UserValidator) (*AddUserRoleRequest, error) {
	var err error
	parsedRequest := &AddUserRoleRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// MapRequestToRemoveUserRoleRequest maps incoming RemoveUserRole request to correct struct
func MapRequestToRemoveUserRoleRequest(request *http.Request, validator UserValidator) (*RemoveUserRoleRequest, error) {
	var err error
	parsedRequest := &RemoveUserRoleRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// MapRequestToVerifyUserEmailRequest maps incoming VerifyUserEmail request to correct struct
func MapRequestToVerifyUserEmailRequest(request *http.Request, validator UserValidator) (*VerifyUserEmailRequest, error) {
	var err error
	parsedRequest := &VerifyUserEmailRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToUnverifyUserEmailRequest maps incoming UnverifyUserEmail request to correct struct
func MapRequestToUnverifyUserEmailRequest(request *http.Request, validator UserValidator) (*UnverifyUserEmailRequest, error) {
	var err error
	parsedRequest := &UnverifyUserEmailRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToVerifyUserPhoneRequest maps incoming VerifyUserPhone request to correct struct
func MapRequestToVerifyUserPhoneRequest(request *http.Request, validator UserValidator) (*VerifyUserPhoneRequest, error) {
	var err error
	parsedRequest := &VerifyUserPhoneRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToRecordUserLoginRequest maps incoming RecordUserLogin request to correct struct
func MapRequestToRecordUserLoginRequest(request *http.Request, validator UserValidator) (*RecordUserLoginRequest, error) {
	var err error
	parsedRequest := &RecordUserLoginRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToGetUserProfileRequest maps incoming GetUserProfile request to correct struct
func MapRequestToGetUserProfileRequest(request *http.Request, validator UserValidator) (*GetUserProfileRequest, error) {
	var err error
	parsedRequest := &GetUserProfileRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToGetUserMicroProfileRequest maps incoming GetUserMicroProfile request to correct struct
func MapRequestToGetUserMicroProfileRequest(request *http.Request, validator UserValidator) (*GetUserMicroProfileRequest, error) {
	var err error
	parsedRequest := &GetUserMicroProfileRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToSetUserExtensionRequest maps incoming SetUserExtension request to correct struct
func MapRequestToSetUserExtensionRequest(request *http.Request, validator UserValidator) (*SetUserExtensionRequest, error) {
	var err error
	parsedRequest := &SetUserExtensionRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// MapRequestToGetUserExtensionRequest maps incoming GetUserExtension request to correct struct
func MapRequestToGetUserExtensionRequest(request *http.Request, validator UserValidator) (*GetUserExtensionRequest, error) {
	var err error
	parsedRequest := &GetUserExtensionRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	// get extension key from uri
	parsedRequest.Key, err = toolbox.GetVariableValueFromUri(request, UserURIVariableExtensionKey)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	return parsedRequest, nil
}

// MapRequestToUpdateUserPersonalInfoRequest maps incoming UpdateUserPersonalInfo request to correct struct
func MapRequestToUpdateUserPersonalInfoRequest(request *http.Request, validator UserValidator) (*UpdateUserPersonalInfoRequest, error) {
	var err error
	parsedRequest := &UpdateUserPersonalInfoRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// MapRequestToValidateUserRequest maps incoming ValidateUser request to correct struct
func MapRequestToValidateUserRequest(request *http.Request, validator UserValidator) (*ValidateUserRequest, error) {
	var err error
	parsedRequest := &ValidateUserRequest{}

	// get user id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil
}

// MapRequestToSearchUsersByExtensionRequest maps incoming SearchUsersByExtension request to correct struct
func MapRequestToSearchUsersByExtensionRequest(request *http.Request, validator UserValidator) (*SearchUsersByExtensionRequest, error) {
	var err error
	parsedRequest := &SearchUsersByExtensionRequest{}

	// get request queries
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	return parsedRequest, nil
}

// MapRequestToBulkUpdateUsersStatusRequest maps incoming BulkUpdateUsersStatus request to correct struct
func MapRequestToBulkUpdateUsersStatusRequest(request *http.Request, validator UserValidator) (*BulkUpdateUsersStatusRequest, error) {
	parsedRequest := &BulkUpdateUsersStatusRequest{}

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// MapRequestToGetUsersByRolesRequest maps incoming GetUsersByRoles request to correct struct
func MapRequestToGetUsersByRolesRequest(request *http.Request, validator UserValidator) (*GetUsersByRolesRequest, error) {
	var err error
	parsedRequest := &GetUsersByRolesRequest{}

	// get request queries
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	return parsedRequest, nil
}

// MapRequestToGetUsersByStatusRequest maps incoming GetUsersByStatus request to correct struct
func MapRequestToGetUsersByStatusRequest(request *http.Request, validator UserValidator) (*GetUsersByStatusRequest, error) {
	var err error
	parsedRequest := &GetUsersByStatusRequest{}

	// get request queries
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	return parsedRequest, nil
}

// validateParsedRequest validates based on tags. On failure an error is returned
func validateParsedRequest(request interface{}, validator UserValidator) error {
	return validator.Validate(request)
}
