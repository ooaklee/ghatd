package usermanager

import (
	"errors"
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ritwickdey/querydecoder"
	"go.uber.org/zap"
)

// MapRequestToUpdateUserProfileRequest maps incoming UpdateUserProfile request to correct
// struct.
func MapRequestToUpdateUserProfileRequest(r *http.Request, validator UsermanagerValidator) (*UpdateUserProfileRequest, error) {
	var parsedRequest = UpdateUserProfileRequest{
		UpdateUserRequest: &userv2.UpdateUserRequest{},
	}
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	err := toolbox.DecodeRequestBody(r, parsedRequest.UpdateUserRequest)
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
func MapRequestToGetUserMicroProfileRequest(r *http.Request, validator UsermanagerValidator) (*GetUserMicroProfileRequest, error) {
	var parsedRequest GetUserMicroProfileRequest
	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	return &parsedRequest, nil
}

// MapRequestToGetUserByIDRequest maps incoming GetUserByID request to correct struct
func MapRequestToGetUserByIDRequest(r *http.Request, validator UsermanagerValidator) (*GetUserByIDRequest, error) {
	var parsedRequest GetUserByIDRequest
	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	baseRequest := userv2.GetUserByIDRequest{}

	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	// Extract target user ID from URL path
	targetUserId, err := toolbox.GetVariableValueFromUri(r, "userId")
	if err != nil {
		log.Error("unable-get-user-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	baseRequest.ID = targetUserId
	parsedRequest.GetUserByIDRequest = &baseRequest

	if err := validateParsedRequest(&parsedRequest, validator); err != nil {
		log.Error("get-user-by-id-request-validation-failed", zap.Error(err))
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToGetUserProfileRequest maps incoming GetUserProfile request to correct
// struct.
func MapRequestToGetUserProfileRequest(r *http.Request, validator UsermanagerValidator) (*GetUserProfileRequest, error) {

	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	parsedRequest := GetUserProfileRequest{
		GetUserProfileRequest: &userv2.GetUserProfileRequest{},
	}

	baseRequest := userv2.GetUserProfileRequest{}

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	// get request queries
	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	parsedRequest.GetUserProfileRequest = &baseRequest

	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	return &parsedRequest, nil
}

// MapRequestToDeleteUserPermanentlyRequest maps incoming DeleteUserPermanently request to correct
// struct.
func MapRequestToDeleteUserPermanentlyRequest(r *http.Request, validator UsermanagerValidator) (*DeleteUserPermanentlyRequest, error) {
	var parsedRequest DeleteUserPermanentlyRequest
	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	parsedRequest.ID = parsedRequest.UserId

	return &parsedRequest, nil
}

// MapRequestToCreateCommsRequest maps the request to a CreateCommsRequest
func MapRequestToCreateCommsRequest(r *http.Request, validator UsermanagerValidator) (*CreateCommsRequest, error) {

	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest := &CreateCommsRequest{
		CreateCommsRequest: &contacter.CreateCommsRequest{},
	}

	baseRequest := contacter.CreateCommsRequest{}

	baseRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	err := toolbox.DecodeRequestBody(r, &baseRequest)
	if err != nil {
		return nil, errors.New(contacter.ErrKeyInvalidCommsPayload)
	}

	parsedRequest.CreateCommsRequest = &baseRequest

	if err := validateParsedRequest(&baseRequest, validator); err != nil {
		log.Error("create-comms-request-validation-failed", zap.Error(err))
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

// mapGetCommsStatsRequest maps the request to a GetCommsStatsRequest
func mapGetCommsStatsRequest(r *http.Request, validator UsermanagerValidator) (*GetCommsStatsRequest, error) {

	parsedRequest := GetCommsStatsRequest{
		GetCommsStatsRequest: &contacter.GetCommsStatsRequest{},
	}

	baseRequest := contacter.GetCommsStatsRequest{}

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, errors.New(contacter.ErrKeyInvalidCommsPayload)
	}

	parsedRequest.GetCommsStatsRequest = &baseRequest

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// validateParsedRequest validates based on tags. On failure an error is returned
func validateParsedRequest(request interface{}, validator UsermanagerValidator) error {
	return validator.Validate(request)
}

// MapRequestToUpdateCommsRequest maps the request to an UpdateCommsRequest
func MapRequestToUpdateCommsRequest(r *http.Request, validator UsermanagerValidator) (*UpdateCommsRequest, error) {

	var parsedRequest UpdateCommsRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Extract comms ID from URL path
	commsId, err := toolbox.GetVariableValueFromUri(r, "id")
	if err != nil {
		log.Error("unable-get-comms-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	baseRequest := contacter.UpdateCommsRequest{
		CommsId: commsId,
	}

	err = toolbox.DecodeRequestBody(r, &baseRequest)
	if err != nil {
		return nil, errors.New(contacter.ErrKeyInvalidCommsPayload)
	}

	parsedRequest.UpdateCommsRequest = &baseRequest

	if err := validateParsedRequest(&baseRequest, validator); err != nil {
		log.Error("update-comms-request-validation-failed", zap.Error(err))
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToGetEnrichedUserProfileRequest maps incoming GetEnrichedUserProfile request to correct struct
func MapRequestToGetEnrichedUserProfileRequest(r *http.Request, validator UsermanagerValidator) (*GetEnrichedUserProfileRequest, error) {
	var parsedRequest GetEnrichedUserProfileRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	query := r.URL.Query()
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
func MapRequestToGetUserGroupsRequest(r *http.Request, validator UsermanagerValidator) (*GetUserGroupsRequest, error) {
	var parsedRequest GetUserGroupsRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("get-user-groups-request-validation-failed", zap.Error(err))
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToGetUserTeamMembershipsRequest maps incoming memberships request to correct struct.
func MapRequestToGetUserTeamMembershipsRequest(r *http.Request, validator UsermanagerValidator) (*GetUserGroupMembershipsRequest, error) {
	parsedRequest := GetUserGroupMembershipsRequest{
		GroupType:          group.GroupTypeTeam,
		IncludeDescendants: true,
	}
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("get-user-team-memberships-request-validation-failed", zap.Error(err))
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupDetailRequest maps incoming GetGroupDetail request to correct struct
func MapRequestToGetGroupDetailRequest(r *http.Request, validator UsermanagerValidator) (*GetGroupDetailRequest, error) {
	var parsedRequest GetGroupDetailRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	parsedRequest.GroupID = groupID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupStatsRequest maps incoming GetGroupStats request to correct struct
func MapRequestToGetGroupStatsRequest(r *http.Request, validator UsermanagerValidator) (*GetGroupStatsRequest, error) {
	var parsedRequest GetGroupStatsRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	parsedRequest.GroupID = groupID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToCreateGroupRequest maps incoming CreateGroup request to correct struct
func MapRequestToCreateGroupRequest(r *http.Request, validator UsermanagerValidator) (*CreateGroupRequest, error) {
	var parsedRequest CreateGroupRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	baseRequest := group.CreateGroupRequest{}

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	parsedRequest.CreateGroupRequest = &baseRequest

	err := toolbox.DecodeRequestBody(r, &parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToAddGroupMemberRequest maps an add-member request to the correct struct
func MapRequestToAddGroupMemberRequest(r *http.Request, validator UsermanagerValidator) (*AddGroupMemberRequest, error) {
	var parsedRequest AddGroupMemberRequest = AddGroupMemberRequest{
		AddMemberRequest: &group.AddMemberRequest{},
	}
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}
	parsedRequest.GroupID = groupID

	if err := toolbox.DecodeRequestBody(r, &parsedRequest); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("add-group-member-request-validation-failed", zap.Error(err))
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToRemoveGroupMemberRequest maps a remove-member request to the correct struct
func MapRequestToRemoveGroupMemberRequest(r *http.Request, validator UsermanagerValidator) (*RemoveGroupMemberRequest, error) {
	var parsedRequest RemoveGroupMemberRequest = RemoveGroupMemberRequest{
		RemoveMemberRequest: &group.RemoveMemberRequest{},
	}
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}
	parsedRequest.GroupID = groupID

	memberID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableMemberID)
	if err != nil {
		log.Error("unable-get-member-id-from-uri")
		return nil, errors.New(ErrKeyInvalidMemberID)
	}
	parsedRequest.MemberID = memberID

	return &parsedRequest, nil
}

// MapRequestToUpdateGroupOwnerRequest maps an update-ownership request to the correct struct
func MapRequestToUpdateGroupOwnerRequest(r *http.Request, validator UsermanagerValidator) (*UpdateGroupOwnerRequest, error) {
	var parsedRequest UpdateGroupOwnerRequest = UpdateGroupOwnerRequest{
		UpdateOwnerRequest: &group.UpdateOwnerRequest{},
	}
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}
	parsedRequest.GroupID = groupID

	if err := toolbox.DecodeRequestBody(r, &parsedRequest); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}
