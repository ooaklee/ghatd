package usermanager

import (
	"errors"
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ritwickdey/querydecoder"
)

// MapRequestToUpdateUserProfileRequest maps incoming UpdateUserProfile request to correct
// struct.
func MapRequestToUpdateUserProfileRequest(request *http.Request, validator UsermanagerValidator) (*UpdateUserProfileRequest, error) {
	var parsedRequest = UpdateUserProfileRequest{
		UpdateUserRequest: &userv2.UpdateUserRequest{},
	}
	log := logger.AcquireFrom(request.Context())

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	err := toolbox.DecodeRequestBody(request, parsedRequest.UpdateUserRequest)
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

// MapRequestToGetEnrichedUserProfileRequest maps incoming GetEnrichedUserProfile request to correct struct
func MapRequestToGetEnrichedUserProfileRequest(request *http.Request, validator UsermanagerValidator) (*GetEnrichedUserProfileRequest, error) {
	var parsedRequest GetEnrichedUserProfileRequest
	log := logger.AcquireFrom(request.Context())

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	query := request.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToGetUserGroupsRequest maps incoming GetUserGroups request to correct struct
func MapRequestToGetUserGroupsRequest(request *http.Request, validator UsermanagerValidator) (*GetUserGroupsRequest, error) {
	var parsedRequest GetUserGroupsRequest
	log := logger.AcquireFrom(request.Context())

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	query := request.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToGetUserTeamMembershipsRequest maps incoming GetUserTeamMemberships request to correct struct
func MapRequestToGetUserTeamMembershipsRequest(request *http.Request, validator UsermanagerValidator) (*GetUserTeamMembershipsRequest, error) {
	var parsedRequest GetUserTeamMembershipsRequest
	log := logger.AcquireFrom(request.Context())

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	query := request.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToUpdateUserTeamMembershipRequest maps incoming UpdateUserTeamMembership request to correct struct
func MapRequestToUpdateUserTeamMembershipRequest(request *http.Request, validator UsermanagerValidator) (*UpdateUserTeamMembershipRequest, error) {
	var parsedRequest UpdateUserTeamMembershipRequest
	log := logger.AcquireFrom(request.Context())

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	var err error
	parsedRequest.GroupID, err = toolbox.GetVariableValueFromUri(request, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	err = toolbox.DecodeRequestBody(request, &parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToRemoveUserFromGroupRequest maps incoming RemoveUserFromGroup request to correct struct
func MapRequestToRemoveUserFromGroupRequest(request *http.Request, validator UsermanagerValidator) (*RemoveUserFromGroupRequest, error) {
	var parsedRequest RemoveUserFromGroupRequest
	log := logger.AcquireFrom(request.Context())

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	var err error
	parsedRequest.GroupID, err = toolbox.GetVariableValueFromUri(request, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToFindUserInfoRequest maps incoming FindUserInfo request to correct struct
func MapRequestToFindUserInfoRequest(request *http.Request, validator UsermanagerValidator) (*FindUserInfoRequest, error) {
	var parsedRequest FindUserInfoRequest
	log := logger.AcquireFrom(request.Context())

	// This endpoint doesn't require user ID from context for admin lookups
	_ = log

	query := request.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToBulkUpdateUserGroupMembershipsRequest maps incoming BulkUpdateUserGroupMemberships request to correct struct
func MapRequestToBulkUpdateUserGroupMembershipsRequest(request *http.Request, validator UsermanagerValidator) (*BulkUpdateUserGroupMembershipsRequest, error) {
	var parsedRequest BulkUpdateUserGroupMembershipsRequest
	log := logger.AcquireFrom(request.Context())

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	var err error
	parsedRequest.TargetUserId, err = toolbox.GetVariableValueFromUri(request, userv2.UserURIVariableID)
	if err != nil {
		log.Error("unable-get-target-user-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	err = toolbox.DecodeRequestBody(request, &parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupsByTypeRequest maps incoming GetGroupsByType request to correct struct
func MapRequestToGetGroupsByTypeRequest(request *http.Request, validator UsermanagerValidator) (*GetGroupsByTypeRequest, error) {
	var parsedRequest GetGroupsByTypeRequest
	log := logger.AcquireFrom(request.Context())

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	var err error
	parsedRequest.GroupType, err = toolbox.GetVariableValueFromUri(request, UserManagerURIVariableGroupType)
	if err != nil {
		log.Error("unable-get-group-type-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	query := request.URL.Query()
	err = querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToCreateGroupRequest maps incoming CreateGroup request to correct struct
func MapRequestToCreateGroupRequest(request *http.Request, validator UsermanagerValidator) (*CreateGroupRequest, error) {
	var parsedRequest CreateGroupRequest
	log := logger.AcquireFrom(request.Context())

	parsedRequest.AdminUserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.AdminUserId == "" {
		log.Error("unable-get-admin-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	err := toolbox.DecodeRequestBody(request, &parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}
