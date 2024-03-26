package usermanager

import (
	"errors"
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
)

// MapRequestToUpdateUserProfileRequest maps incoming UpdateUserProfile request to correct
// struct.
func MapRequestToUpdateUserProfileRequest(request *http.Request, validator UsermanagerValidator) (*UpdateUserProfileRequest, error) {
	parsedRequest := &UpdateUserProfileRequest{}
	log := logger.AcquireFrom(request.Context())

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	if parsedRequest.FirstName == "" && parsedRequest.LastName == "" || parsedRequest.FirstName != "" && len(parsedRequest.FirstName) == 1 || parsedRequest.LastName != "" && len(parsedRequest.LastName) == 1 {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())

	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	return parsedRequest, nil
}

// MapRequestToGetUserMicroProfileRequest maps incoming GetUserMicroProfile request to correct
// struct.
func MapRequestToGetUserMicroProfileRequest(request *http.Request, validator UsermanagerValidator) (*GetUserMicroProfileRequest, error) {
	parsedRequest := &GetUserMicroProfileRequest{}
	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	log := logger.AcquireFrom(request.Context())

	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	return parsedRequest, nil
}

// MapRequestToGetUserProfileRequest maps incoming GetUserProfile request to correct
// struct.
func MapRequestToGetUserProfileRequest(request *http.Request, validator UsermanagerValidator) (*GetUserProfileRequest, error) {
	parsedRequest := &GetUserProfileRequest{}
	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	log := logger.AcquireFrom(request.Context())

	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	return parsedRequest, nil
}

// MapRequestToDeleteUserPermanentlyRequest maps incoming GetUserMicroProfile request to correct
// struct.
func MapRequestToDeleteUserPermanentlyRequest(request *http.Request, validator UsermanagerValidator) (*DeleteUserPermanentlyRequest, error) {
	parsedRequest := &DeleteUserPermanentlyRequest{}
	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	log := logger.AcquireFrom(request.Context())

	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	return parsedRequest, nil
}
