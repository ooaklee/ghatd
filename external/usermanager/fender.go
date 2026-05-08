package usermanager

import (
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
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
		return nil, ErrUnableToIdentifyUser
	}

	err := toolbox.DecodeRequestBody(r, parsedRequest.UpdateUserRequest)
	if err != nil {
		return nil, ErrInvalidUserBody
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrInvalidUserBody
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
		return nil, ErrUnableToIdentifyUser
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupsByUserIDRequest maps incoming GetGroupsByUserID request to correct struct
func MapRequestToGetGroupsByUserIDRequest(r *http.Request, validator UsermanagerValidator) (*GetGroupsByUserIDRequest, error) {
	var parsedRequest GetGroupsByUserIDRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.ID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.ID == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	targetUserId, err := toolbox.GetVariableValueFromUri(r, "userId")
	if err != nil {
		log.Error("unable-get-user-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	baseRequest := group.GetGroupsByUserIDRequest{
		UserID: targetUserId,
	}

	// get request queries
	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.GetGroupsByUserIDRequest = &baseRequest

	if err := validateParsedRequest(&parsedRequest, validator); err != nil {
		log.Error("get-groups-by-user-id-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
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
		return nil, ErrUnableToIdentifyUser
	}

	// Extract target user ID from URL path
	targetUserId, err := toolbox.GetVariableValueFromUri(r, "userId")
	if err != nil {
		log.Error("unable-get-user-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	baseRequest.ID = targetUserId
	parsedRequest.GetUserByIDRequest = &baseRequest

	if err := validateParsedRequest(&parsedRequest, validator); err != nil {
		log.Error("get-user-by-id-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetUsersRequest maps incoming GetUsers request to correct struct.
func MapRequestToGetUsersRequest(r *http.Request, validator UsermanagerValidator) (*GetUsersRequest, error) {
	var parsedRequest GetUsersRequest = GetUsersRequest{
		GetUsersRequest: &userv2.GetUsersRequest{},
	}
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	// get request queries
	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(&parsedRequest, validator); err != nil {
		log.Error("get-users-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupLineageRequest maps incoming GetGroupLineage request to correct struct.
func MapRequestToGetGroupLineageRequest(r *http.Request, validator UsermanagerValidator) (*GetGroupLineageRequest, error) {
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	baseRequest := group.GetGroupLineageRequest{}
	parsedRequest := GetGroupLineageRequest{
		GetGroupLineageRequest: &baseRequest,
	}

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.GetGroupLineageRequest.ID = groupID

	query := r.URL.Query()
	if err := querydecoder.New(query).Decode(parsedRequest.GetGroupLineageRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupsConfigRequest maps incoming GetGroupsConfig request to correct struct.
// MapRequestToGetGroupDescendantsRequest maps incoming GetGroupDescendants request to correct struct.
func MapRequestToGetGroupDescendantsRequest(r *http.Request, validator UsermanagerValidator) (*GetGroupDescendantsRequest, error) {
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	baseRequest := group.GetGroupDescendantsRequest{}
	parsedRequest := GetGroupDescendantsRequest{
		GetGroupDescendantsRequest: &baseRequest,
	}

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.GetGroupDescendantsRequest.ID = groupID

	query := r.URL.Query()
	if err := querydecoder.New(query).Decode(parsedRequest.GetGroupDescendantsRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupsConfigRequest maps incoming GetGroupsConfig request to correct struct.
func MapRequestToGetGroupsConfigRequest(r *http.Request, validator UsermanagerValidator) (*GetGroupsConfigRequest, error) {
	var parsedRequest GetGroupsConfigRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
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
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.GetUserProfileRequest = &baseRequest

	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
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
		return nil, ErrUnableToIdentifyUser
	}

	parsedRequest.ID = parsedRequest.UserId

	err := toolbox.DecodeRequestBody(r, &parsedRequest)
	if err != nil {
		log.Error("unable-decode-request-body", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(&parsedRequest, validator); err != nil {
		log.Error("delete-user-permanently-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
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
		return nil, contacter.ErrInvalidCommsPayload
	}

	parsedRequest.CreateCommsRequest = &baseRequest

	if err := validateParsedRequest(&baseRequest, validator); err != nil {
		log.Error("create-comms-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
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
		return nil, contacter.ErrInvalidCommsPayload
	}

	parsedRequest.GetCommsRequest = &baseRequest

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrRequestFailedValidation
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
		return nil, contacter.ErrInvalidCommsPayload
	}

	parsedRequest.GetCommsStatsRequest = &baseRequest

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrRequestFailedValidation
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
		return nil, ErrRequestFailedValidation
	}

	baseRequest := contacter.UpdateCommsRequest{
		CommsId: commsId,
	}

	err = toolbox.DecodeRequestBody(r, &baseRequest)
	if err != nil {
		return nil, contacter.ErrInvalidCommsPayload
	}

	parsedRequest.UpdateCommsRequest = &baseRequest

	if err := validateParsedRequest(&baseRequest, validator); err != nil {
		log.Error("update-comms-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
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
		return nil, ErrUnableToIdentifyUser
	}

	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrRequestFailedValidation
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
		return nil, ErrUnableToIdentifyUser
	}

	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("get-user-groups-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetLatestNotificationOverviewsRequest maps incoming latest notifications request to the correct struct.
func MapRequestToGetLatestNotificationOverviewsRequest(r *http.Request, validator UsermanagerValidator) (*GetLatestNotificationOverviewsRequest, error) {
	var parsedRequest GetLatestNotificationOverviewsRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	baseRequest := common.GetLatestNotificationOverviewsRequest{}
	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.GetLatestNotificationOverviewsRequest = &baseRequest

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("get-latest-notification-overviews-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetMyGroupInvitationsRequest maps incoming my-group-invitations request to the correct struct.
func MapRequestToGetMyGroupInvitationsRequest(r *http.Request, validator UsermanagerValidator) (*GetMyGroupInvitationsRequest, error) {
	var parsedRequest GetMyGroupInvitationsRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	if err := querydecoder.New(r.URL.Query()).Decode(&parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("get-my-group-invitations-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToAcceptMyGroupInvitationRequest maps incoming accept-my-group-invitation request to the correct struct.
func MapRequestToAcceptMyGroupInvitationRequest(r *http.Request, validator UsermanagerValidator) (*AcceptMyGroupInvitationRequest, error) {
	var parsedRequest AcceptMyGroupInvitationRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, "groupID")
	if err != nil {
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.GroupID = groupID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("accept-my-group-invitation-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToRejectMyGroupInvitationRequest maps incoming reject-my-group-invitation request to the correct struct.
func MapRequestToRejectMyGroupInvitationRequest(r *http.Request, validator UsermanagerValidator) (*RejectMyGroupInvitationRequest, error) {
	var parsedRequest RejectMyGroupInvitationRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, "groupID")
	if err != nil {
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.GroupID = groupID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("reject-my-group-invitation-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetUserGroupMembershipsRequestRequest maps incoming memberships request to correct struct.
func MapRequestToGetUserGroupMembershipsRequestRequest(r *http.Request, validator UsermanagerValidator) (*GetUserGroupMembershipsRequest, error) {
	parsedRequest := GetUserGroupMembershipsRequest{
		GroupType:          "",
		IncludeDescendants: true,
	}
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("get-user-team-memberships-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
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
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.GroupID = groupID

	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrRequestFailedValidation
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
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.GroupID = groupID

	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrRequestFailedValidation
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
		return nil, ErrUnableToIdentifyUser
	}

	parsedRequest.CreateGroupRequest = &baseRequest

	err := toolbox.DecodeRequestBody(r, &parsedRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToUpdateGroupRequest maps incoming UpdateGroup request to correct struct
func MapRequestToUpdateGroupRequest(r *http.Request, validator UsermanagerValidator) (*UpdateGroupRequest, error) {
	var parsedRequest UpdateGroupRequest = UpdateGroupRequest{
		UpdateGroupRequest: &group.UpdateGroupRequest{},
	}
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.UpdateGroupRequest.ID = groupID

	if err := toolbox.DecodeRequestBody(r, parsedRequest.UpdateGroupRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToDeleteGroupRequest  maps incoming DeleteGroup request to correct struct
func MapRequestToDeleteGroupRequest(r *http.Request, validator UsermanagerValidator) (*DeleteGroupRequest, error) {
	var parsedRequest DeleteGroupRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	baseRequest := group.DeleteGroupRequest{}

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	baseRequest.ID = groupID

	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.DeleteGroupRequest = &baseRequest

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrRequestFailedValidation
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
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.GroupID = groupID

	if err := toolbox.DecodeRequestBody(r, &parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("add-group-member-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToRemoveGroupMemberRequest maps a remove-member request to the correct struct
func MapRequestToRemoveGroupMemberRequest(r *http.Request, validator UsermanagerValidator) (*RemoveGroupMemberRequest, error) {
	var parsedRequest RemoveGroupMemberRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	baseRequest := group.RemoveMemberRequest{}

	// get request queries
	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	baseRequest.GroupID = groupID

	memberID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableMemberID)
	if err != nil {
		log.Error("unable-get-member-id-from-uri")
		return nil, ErrInvalidMemberID
	}
	baseRequest.MemberID = memberID

	parsedRequest.RemoveMemberRequest = &baseRequest

	return &parsedRequest, nil
}

// MapRequestToUpdateGroupMemberRequest maps an update-member request to the correct struct
func MapRequestToUpdateGroupMemberRequest(r *http.Request, validator UsermanagerValidator) (*UpdateGroupMemberRequest, error) {
	var parsedRequest UpdateGroupMemberRequest = UpdateGroupMemberRequest{
		UpdateMemberRoleRequest: &group.UpdateMemberRoleRequest{},
	}
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.GroupID = groupID

	memberID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableMemberID)
	if err != nil {
		log.Error("unable-get-member-id-from-uri")
		return nil, ErrInvalidMemberID
	}
	parsedRequest.MemberID = memberID

	if err := toolbox.DecodeRequestBody(r, &parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("update-group-member-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

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
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		log.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.GroupID = groupID

	if err := toolbox.DecodeRequestBody(r, &parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToValidateGroupNameRequest maps incoming ValidateGroupName request to correct struct
func MapRequestToValidateGroupNameRequest(r *http.Request, validator UsermanagerValidator) (*ValidateGroupNameRequest, error) {
	var parsedRequest ValidateGroupNameRequest
	log := logger.AcquireFrom(r.Context()).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	if parsedRequest.UserID == "" {
		log.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	baseRequest := group.ValidateGroupNameRequest{}

	// get request queries
	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.ValidateGroupNameRequest = &baseRequest

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		log.Error("validate-group-name-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}
