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

// MapRequestToGetUserTeamMembershipsRequest maps incoming GetUserTeamMemberships request to correct struct
func MapRequestToGetUserTeamMembershipsRequest(r *http.Request, validator UsermanagerValidator) (*GetUserTeamMembershipsRequest, error) {
	var parsedRequest GetUserTeamMembershipsRequest
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

// MapRequestToUpdateUserTeamMembershipRequest maps incoming UpdateUserTeamMembership request to correct struct
func MapRequestToUpdateUserTeamMembershipRequest(r *http.Request, validator UsermanagerValidator) (*UpdateUserTeamMembershipRequest, error) {
	var parsedRequest UpdateUserTeamMembershipRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	var err error
	parsedRequest.GroupID, err = toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	err = toolbox.DecodeRequestBody(r, &parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToRemoveUserFromGroupRequest maps incoming RemoveUserFromGroup request to correct struct
func MapRequestToRemoveUserFromGroupRequest(r *http.Request, validator UsermanagerValidator) (*RemoveUserFromGroupRequest, error) {
	var parsedRequest RemoveUserFromGroupRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	var err error
	parsedRequest.GroupID, err = toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
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
func MapRequestToFindUserInfoRequest(r *http.Request, validator UsermanagerValidator) (*FindUserInfoRequest, error) {
	var parsedRequest FindUserInfoRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// This endpoint doesn't require user ID from context for admin lookups
	_ = log

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

// MapRequestToBulkUpdateUserGroupMembershipsRequest maps incoming BulkUpdateUserGroupMemberships request to correct struct
func MapRequestToBulkUpdateUserGroupMembershipsRequest(r *http.Request, validator UsermanagerValidator) (*BulkUpdateUserGroupMembershipsRequest, error) {
	var parsedRequest BulkUpdateUserGroupMembershipsRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	var err error
	parsedRequest.TargetUserId, err = toolbox.GetVariableValueFromUri(r, userv2.UserURIVariableID)
	if err != nil {
		log.Error("unable-get-target-user-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	err = toolbox.DecodeRequestBody(r, &parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupsByTypeRequest maps incoming GetGroupsByType request to correct struct
func MapRequestToGetGroupsByTypeRequest(r *http.Request, validator UsermanagerValidator) (*GetGroupsByTypeRequest, error) {
	var parsedRequest GetGroupsByTypeRequest
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
		log.Error("get-groups-by-type-request-validation-failed", zap.Error(err))
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToCreateGroupRequest maps incoming CreateGroup request to correct struct
func MapRequestToCreateGroupRequest(r *http.Request, validator UsermanagerValidator) (*CreateGroupRequest, error) {
	var parsedRequest CreateGroupRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.AdminUserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.AdminUserId == "" {
		log.Error("unable-get-admin-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	err := toolbox.DecodeRequestBody(r, &parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToAdminGetGroupDetailRequest maps an admin get-group-detail request to the correct struct
func MapRequestToAdminGetGroupDetailRequest(r *http.Request, validator UsermanagerValidator) (*AdminGetGroupDetailRequest, error) {
	var parsedRequest AdminGetGroupDetailRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.AdminUserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.AdminUserId == "" {
		log.Error("unable-get-admin-user-id")
		return nil, errors.New(ErrKeyUnableToIdentifyUser)
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}
	parsedRequest.GroupID = groupID

	return &parsedRequest, nil
}

// MapRequestToAdminAddGroupMemberRequest maps an admin add-member request to the correct struct
func MapRequestToAdminAddGroupMemberRequest(r *http.Request, validator UsermanagerValidator) (*AdminAddGroupMemberRequest, error) {
	var parsedRequest AdminAddGroupMemberRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.AdminUserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.AdminUserId == "" {
		log.Error("unable-get-admin-user-id")
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
		log.Error("admin-add-group-member-request-validation-failed", zap.Error(err))
		return nil, errors.New(ErrKeyRequestFailedValidation)
	}

	return &parsedRequest, nil
}

// MapRequestToAdminRemoveGroupMemberRequest maps an admin remove-member request to the correct struct
func MapRequestToAdminRemoveGroupMemberRequest(r *http.Request, validator UsermanagerValidator) (*AdminRemoveGroupMemberRequest, error) {
	var parsedRequest AdminRemoveGroupMemberRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.AdminUserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.AdminUserId == "" {
		log.Error("unable-get-admin-user-id")
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

// MapRequestToAdminUpdateGroupOwnerRequest maps an admin update-ownership request to the correct struct
func MapRequestToAdminUpdateGroupOwnerRequest(r *http.Request, validator UsermanagerValidator) (*AdminUpdateGroupOwnerRequest, error) {
	var parsedRequest AdminUpdateGroupOwnerRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.AdminUserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.AdminUserId == "" {
		log.Error("unable-get-admin-user-id")
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
