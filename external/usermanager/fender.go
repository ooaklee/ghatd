package usermanager

import (
	"errors"
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
)

// MapRequestToUpdateUserProfileRequest maps incoming UpdateUserProfile request to correct
// struct.
func MapRequestToUpdateUserProfileRequest(request *http.Request, validator UsermanagerValidator) (*UpdateUserProfileRequest, error) {
	var parsedRequest UpdateUserProfileRequest
	log := logger.AcquireFrom(request.Context())

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return &parsedRequest, nil
}

// MapRequestToGetUserMicroProfileRequest maps incoming GetUserMicroProfile request to correct
// struct.
func MapRequestToGetUserMicroProfileRequest(request *http.Request, validator UsermanagerValidator) (*GetUserMicroProfileRequest, error) {
	var parsedRequest GetUserMicroProfileRequest
	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	log := logger.AcquireFrom(request.Context())

	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	return &parsedRequest, nil
}

// MapRequestToGetUserProfileRequest maps incoming GetUserProfile request to correct
// struct.
func MapRequestToGetUserProfileRequest(request *http.Request, validator UsermanagerValidator) (*GetUserProfileRequest, error) {
	var parsedRequest GetUserProfileRequest
	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	log := logger.AcquireFrom(request.Context())

	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	return &parsedRequest, nil
}

// MapRequestToDeleteUserPermanentlyRequest maps incoming GetUserMicroProfile request to correct
// struct.
func MapRequestToDeleteUserPermanentlyRequest(request *http.Request, validator UsermanagerValidator) (*DeleteUserPermanentlyRequest, error) {
	var parsedRequest DeleteUserPermanentlyRequest
	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	log := logger.AcquireFrom(request.Context())

	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	return &parsedRequest, nil
}

// MapRequestToCreateCommsRequest maps the request to a CreateCommsRequest
func MapRequestToCreateCommsRequest(r *http.Request, validator UsermanagerValidator) (*CreateCommsRequest, error) {

	parsedRequest := &CreateCommsRequest{
		CreateCommsRequest: &contacter.CreateCommsRequest{},
	}
	parsedRequest.CreateCommsRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	err := toolbox.DecodeRequestBody(r, parsedRequest)
	if err != nil {
		return nil, errors.New(contacter.ErrKeyInvalidCommsPayload)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return parsedRequest, nil
}

// mapGetCommsRequest maps the request to a GetCommsRequest
func mapGetCommsRequest(r *http.Request, validator UsermanagerValidator) (*GetCommsRequest, error) {

	parsedRequest := GetCommsRequest{
		GetCommsRequest: &contacter.GetCommsRequest{},
	}

	baseRequest := contacter.GetCommsRequest{}

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, errors.New(contacter.ErrKeyInvalidCommsPayload)
	}

	parsedRequest.GetCommsRequest = &baseRequest

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// validateParsedRequest validates based on tags. On failure an error is returned
func validateParsedRequest(request interface{}, validator UsermanagerValidator) error {
	return validator.Validate(request)
}
