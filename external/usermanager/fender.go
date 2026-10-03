package usermanager

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/reminder"
	"github.com/ooaklee/ghatd/external/streaker"
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
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
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
	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupsByUserIDRequest maps incoming GetGroupsByUserID request to correct struct
func MapRequestToGetGroupsByUserIDRequest(r *http.Request, validator UsermanagerValidator) (*GetGroupsByUserIDRequest, error) {
	var parsedRequest GetGroupsByUserIDRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	targetUserId, err := toolbox.GetVariableValueFromUri(r, "userId")
	if err != nil {
		logger.Error("unable-get-user-id-from-uri")
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
		logger.Error("get-groups-by-user-id-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetUserByIDRequest maps incoming GetUserByID request to correct struct
func MapRequestToGetUserByIDRequest(r *http.Request, validator UsermanagerValidator) (*GetUserByIDRequest, error) {
	var parsedRequest GetUserByIDRequest
	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	baseRequest := userv2.GetUserByIDRequest{}

	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	// Extract target user ID from URL path
	targetUserId, err := toolbox.GetVariableValueFromUri(r, "userId")
	if err != nil {
		logger.Error("unable-get-user-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	baseRequest.ID = targetUserId
	parsedRequest.GetUserByIDRequest = &baseRequest

	if err := validateParsedRequest(&parsedRequest, validator); err != nil {
		logger.Error("get-user-by-id-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetUsersRequest maps incoming GetUsers request to correct struct.
func MapRequestToGetUsersRequest(r *http.Request, validator UsermanagerValidator) (*GetUsersRequest, error) {
	var parsedRequest GetUsersRequest = GetUsersRequest{
		GetUsersRequest: &userv2.GetUsersRequest{},
	}
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	// get request queries
	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(&parsedRequest, validator); err != nil {
		logger.Error("get-users-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupLineageRequest maps incoming GetGroupLineage request to correct struct.
func MapRequestToGetGroupLineageRequest(r *http.Request, validator UsermanagerValidator) (*GetGroupLineageRequest, error) {
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	baseRequest := group.GetGroupLineageRequest{}
	parsedRequest := GetGroupLineageRequest{
		GetGroupLineageRequest: &baseRequest,
	}

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		logger.Error("unable-get-group-id-from-uri")
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
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	baseRequest := group.GetGroupDescendantsRequest{}
	parsedRequest := GetGroupDescendantsRequest{
		GetGroupDescendantsRequest: &baseRequest,
	}

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		logger.Error("unable-get-group-id-from-uri")
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
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	return &parsedRequest, nil
}

// MapRequestToGetUserProfileRequest maps incoming GetUserProfile request to correct
// struct.
func MapRequestToGetUserProfileRequest(r *http.Request, validator UsermanagerValidator) (*GetUserProfileRequest, error) {

	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")
	parsedRequest := GetUserProfileRequest{
		GetUserProfileRequest: &userv2.GetUserProfileRequest{},
	}

	baseRequest := userv2.GetUserProfileRequest{}

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())

	// get request queries
	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.GetUserProfileRequest = &baseRequest

	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	return &parsedRequest, nil
}

// MapRequestToDeleteUserPermanentlyRequest accepts a deletion reason and binds
// both actor and target to the authenticated caller. DELETE /me cannot select
// another account, even when called by an administrator.
func MapRequestToDeleteUserPermanentlyRequest(r *http.Request, validator UsermanagerValidator) (*DeleteUserPermanentlyRequest, error) {
	var parsedRequest DeleteUserPermanentlyRequest
	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	parsedRequest.ID = parsedRequest.ActorID

	var payload struct {
		// Reason explains the deletion without granting authority over its target.
		Reason string `json:"reason"`
	}
	err := toolbox.DecodeRequestBody(r, &payload)
	if err != nil {
		logger.Error("unable-decode-request-body", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.Reason = payload.Reason

	if err := validateParsedRequest(&parsedRequest, validator); err != nil {
		logger.Error("delete-user-permanently-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToCreateCommsRequest binds attribution after decoding the message.
// Anonymous submissions remain valid and cannot claim another user's identity.
func MapRequestToCreateCommsRequest(r *http.Request, validator UsermanagerValidator) (*CreateCommsRequest, error) {

	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest := &CreateCommsRequest{
		CreateCommsRequest: &contacter.CreateCommsRequest{},
	}

	baseRequest := contacter.CreateCommsRequest{}

	err := toolbox.DecodeRequestBody(r, &baseRequest)
	if err != nil {
		return nil, contacter.ErrInvalidCommsPayload
	}
	baseRequest.UserId = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())

	parsedRequest.CreateCommsRequest = &baseRequest

	if err := validateParsedRequest(&baseRequest, validator); err != nil {
		logger.Error("create-comms-request-validation-failed", zap.Error(err))
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

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		return nil, ErrUnableToIdentifyUser
	}

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

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		return nil, ErrUnableToIdentifyUser
	}

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

// MapRequestToUpdateCommsRequest binds the verified caller and route-selected
// contact record. Route middleware remains responsible for requiring an admin.
func MapRequestToUpdateCommsRequest(r *http.Request, validator UsermanagerValidator) (*UpdateCommsRequest, error) {

	var parsedRequest UpdateCommsRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")
	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		return nil, ErrUnableToIdentifyUser
	}

	// Extract comms ID from URL path
	commsId, err := toolbox.GetVariableValueFromUri(r, "id")
	if err != nil {
		logger.Error("unable-get-comms-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	baseRequest := contacter.UpdateCommsRequest{}

	err = toolbox.DecodeRequestBody(r, &baseRequest)
	if err != nil {
		return nil, contacter.ErrInvalidCommsPayload
	}
	baseRequest.CommsId = commsId

	parsedRequest.UpdateCommsRequest = &baseRequest

	if err := validateParsedRequest(&baseRequest, validator); err != nil {
		logger.Error("update-comms-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetEnrichedUserProfileRequest maps incoming GetEnrichedUserProfile request to correct struct
func MapRequestToGetEnrichedUserProfileRequest(r *http.Request, validator UsermanagerValidator) (*GetEnrichedUserProfileRequest, error) {
	var parsedRequest GetEnrichedUserProfileRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
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
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("get-user-groups-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetLatestNotificationOverviewsRequest binds self-service reads to
// the verified actor. Only administrative URLs retain recipient selectors; the
// service separately verifies live administrator authority before using them.
func MapRequestToGetLatestNotificationOverviewsRequest(r *http.Request, validator UsermanagerValidator) (*GetLatestNotificationOverviewsRequest, error) {
	var parsedRequest GetLatestNotificationOverviewsRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	requesterUserID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if requesterUserID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	baseRequest := common.GetLatestNotificationOverviewsRequest{}
	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.AdminView = isAdminNotificationRoute(r)
	targetUserID := requesterUserID
	if parsedRequest.AdminView {
		if pathUserID := getOptionalVariableValueFromURI(r, "userId"); pathUserID != "" {
			targetUserID = pathUserID
		} else if queryUserID := strings.TrimSpace(baseRequest.UserID); queryUserID != "" {
			targetUserID = queryUserID
		}
	} else {
		baseRequest.UserEmail = ""
	}
	baseRequest.UserID = targetUserID
	parsedRequest.ActorID = requesterUserID
	parsedRequest.GetLatestNotificationOverviewsRequest = &baseRequest

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("get-latest-notification-overviews-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetNotifierConfigRequest maps incoming notifier config request to the correct struct.
func MapRequestToGetNotifierConfigRequest(r *http.Request, validator UsermanagerValidator) (*GetNotifierConfigRequest, error) {
	var parsedRequest GetNotifierConfigRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}
	parsedRequest.GetNotifierConfigRequest = &notifier.GetNotifierConfigRequest{}

	return &parsedRequest, nil
}

// MapRequestToRegisterNotificationAddressRequest maps incoming notification address registration to the correct struct.
func MapRequestToRegisterNotificationAddressRequest(r *http.Request, validator UsermanagerValidator) (*RegisterNotificationAddressRequest, error) {
	var parsedRequest RegisterNotificationAddressRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	requesterUserID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if requesterUserID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	baseRequest := notifier.RegisterAddressRequest{}
	if err := toolbox.DecodeRequestBody(r, &baseRequest); err != nil {
		return nil, notifier.ErrInvalidNotificationAddressBody
	}
	userID := requesterUserID
	if isAdminNotificationRoute(r) {
		if queryUserID := strings.TrimSpace(r.URL.Query().Get("user_id")); queryUserID != "" {
			userID = queryUserID
		}
	}
	baseRequest.UserID = userID

	parsedRequest.ActorID = requesterUserID
	parsedRequest.RegisterAddressRequest = &baseRequest
	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("register-notification-address-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToListNotificationAddressesRequest maps incoming notification address list request to the correct struct.
func MapRequestToListNotificationAddressesRequest(r *http.Request, validator UsermanagerValidator) (*ListNotificationAddressesRequest, error) {
	var parsedRequest ListNotificationAddressesRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	requesterUserID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if requesterUserID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	baseRequest := notifier.ListNotificationAddressesRequest{}
	if err := querydecoder.New(r.URL.Query()).Decode(&baseRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	baseRequest.Channel = baseRequest.Channel.Normalised()
	baseRequest.Status = baseRequest.Status.Normalised()

	parsedRequest.ActorID = requesterUserID
	parsedRequest.AdminView = isAdminNotificationRoute(r)
	parsedRequest.IncludeUsers = parsedRequest.AdminView
	if includeUsers := strings.TrimSpace(r.URL.Query().Get("include_users")); includeUsers != "" {
		parsedRequest.IncludeUsers = !strings.EqualFold(includeUsers, "false") && includeUsers != "0"
	}

	if !parsedRequest.AdminView {
		baseRequest.UserID = requesterUserID
	}

	parsedRequest.ListNotificationAddressesRequest = &baseRequest
	return &parsedRequest, nil
}

// MapRequestToDeleteNotificationAddressRequest maps incoming notification address delete request to the correct struct.
func MapRequestToDeleteNotificationAddressRequest(r *http.Request, validator UsermanagerValidator) (*DeleteNotificationAddressRequest, error) {
	var parsedRequest DeleteNotificationAddressRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	requesterUserID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if requesterUserID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	addressID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableAddressID)
	if err != nil {
		logger.Error("unable-get-notification-address-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	targetUserID := getOptionalVariableValueFromURI(r, "userId")
	if targetUserID == "" {
		targetUserID = requesterUserID
	}

	parsedRequest.ActorID = requesterUserID
	parsedRequest.DeleteNotificationAddressRequest = &notifier.DeleteNotificationAddressRequest{
		UserID:    targetUserID,
		AddressID: addressID,
	}
	return &parsedRequest, nil
}

// MapRequestToGetNotificationPreferencesRequest maps incoming notification preferences request to the correct struct.
func MapRequestToGetNotificationPreferencesRequest(r *http.Request, validator UsermanagerValidator) (*GetNotificationPreferencesRequest, error) {
	var parsedRequest GetNotificationPreferencesRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	requesterUserID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if requesterUserID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	targetUserID := getOptionalVariableValueFromURI(r, "userId")
	if targetUserID == "" {
		targetUserID = requesterUserID
	}

	parsedRequest.ActorID = requesterUserID
	parsedRequest.IncludeUser = isAdminNotificationRoute(r)
	parsedRequest.GetNotificationPreferencesRequest = &notifier.GetNotificationPreferencesRequest{UserID: targetUserID}
	return &parsedRequest, nil
}

// MapRequestToUpdateNotificationPreferencesRequest maps incoming notification preferences update to the correct struct.
func MapRequestToUpdateNotificationPreferencesRequest(r *http.Request, validator UsermanagerValidator) (*UpdateNotificationPreferencesRequest, error) {
	var parsedRequest UpdateNotificationPreferencesRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	requesterUserID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if requesterUserID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	baseRequest := notifier.UpdateNotificationPreferencesRequest{}
	if err := toolbox.DecodeRequestBody(r, &baseRequest); err != nil {
		return nil, notifier.ErrInvalidNotificationPreferences
	}
	targetUserID := getOptionalVariableValueFromURI(r, "userId")
	if targetUserID == "" {
		targetUserID = requesterUserID
	}
	baseRequest.UserID = targetUserID

	parsedRequest.ActorID = requesterUserID
	parsedRequest.IncludeUser = isAdminNotificationRoute(r)
	parsedRequest.UpdateNotificationPreferencesRequest = &baseRequest
	return &parsedRequest, nil
}

// MapRequestToNotifyUserRequest maps incoming admin/service notification send request to the correct struct.
func MapRequestToNotifyUserRequest(r *http.Request, validator UsermanagerValidator) (*NotifyUserRequest, error) {
	var parsedRequest NotifyUserRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	targetUserID, err := toolbox.GetVariableValueFromUri(r, "userId")
	if err != nil {
		logger.Error("unable-get-target-user-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	baseRequest := notifier.NotifyUserRequest{}
	if err := toolbox.DecodeRequestBody(r, &baseRequest); err != nil {
		return nil, notifier.ErrInvalidNotificationAddressBody
	}
	baseRequest.UserID = targetUserID

	parsedRequest.NotifyUserRequest = &baseRequest
	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("notify-user-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToNotifyUsersRequest maps incoming admin notification dispatch request to the correct struct.
func MapRequestToNotifyUsersRequest(r *http.Request, validator UsermanagerValidator) (*NotifyUsersRequest, error) {
	var parsedRequest NotifyUsersRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	baseRequest := notifier.NotifyUsersRequest{}
	if err := toolbox.DecodeRequestBody(r, &baseRequest); err != nil {
		return nil, notifier.ErrInvalidNotificationAddressBody
	}

	parsedRequest.NotifyUsersRequest = &baseRequest
	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("notify-users-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetMyGroupInvitationsRequest maps incoming my-group-invitations request to the correct struct.
func MapRequestToGetMyGroupInvitationsRequest(r *http.Request, validator UsermanagerValidator) (*GetMyGroupInvitationsRequest, error) {
	var parsedRequest GetMyGroupInvitationsRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	if err := querydecoder.New(r.URL.Query()).Decode(&parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("get-my-group-invitations-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToAcceptMyGroupInvitationRequest maps incoming accept-my-group-invitation request to the correct struct.
func MapRequestToAcceptMyGroupInvitationRequest(r *http.Request, validator UsermanagerValidator) (*AcceptMyGroupInvitationRequest, error) {
	var parsedRequest AcceptMyGroupInvitationRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, "groupID")
	if err != nil {
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.GroupID = groupID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("accept-my-group-invitation-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToRejectMyGroupInvitationRequest maps incoming reject-my-group-invitation request to the correct struct.
func MapRequestToRejectMyGroupInvitationRequest(r *http.Request, validator UsermanagerValidator) (*RejectMyGroupInvitationRequest, error) {
	var parsedRequest RejectMyGroupInvitationRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, "groupID")
	if err != nil {
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.GroupID = groupID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("reject-my-group-invitation-request-validation-failed", zap.Error(err))
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
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	query := r.URL.Query()
	err := querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("get-user-team-memberships-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetGroupDetailRequest maps incoming GetGroupDetail request to correct struct
func MapRequestToGetGroupDetailRequest(r *http.Request, validator UsermanagerValidator) (*GetGroupDetailRequest, error) {
	var parsedRequest GetGroupDetailRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		logger.Error("unable-get-group-id-from-uri")
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
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		logger.Error("unable-get-group-id-from-uri")
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

// MapRequestToCreateGroupRequest decodes group data separately from the verified
// actor. Owner and parent selections remain inputs for service authorization.
func MapRequestToCreateGroupRequest(r *http.Request, validator UsermanagerValidator) (*CreateGroupRequest, error) {
	var parsedRequest CreateGroupRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	baseRequest := group.CreateGroupRequest{}

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	parsedRequest.CreateGroupRequest = &baseRequest

	err := toolbox.DecodeRequestBody(r, &baseRequest)
	if err != nil {
		return nil, ErrRequestFailedValidation
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToUpdateGroupRequest accepts editable fields, not a replacement
// group record. The authenticated caller and URL determine authorization and
// target identity; internal full-record updates are not part of this HTTP API.
func MapRequestToUpdateGroupRequest(r *http.Request, validator UsermanagerValidator) (*UpdateGroupRequest, error) {
	var parsedRequest UpdateGroupRequest = UpdateGroupRequest{
		UpdateGroupRequest: &group.UpdateGroupRequest{},
	}
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		logger.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	var payload groupUpdatePayload
	if err := toolbox.DecodeRequestBody(r, &payload); err != nil {
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.UpdateGroupRequest = &group.UpdateGroupRequest{
		ID: groupID, Name: payload.Name, Description: payload.Description,
		Email: payload.Email, Icon: payload.Icon, Visibility: payload.Visibility,
		Status: payload.Status, Extensions: payload.Extensions,
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToDeleteGroupRequest  maps incoming DeleteGroup request to correct struct
func MapRequestToDeleteGroupRequest(r *http.Request, validator UsermanagerValidator) (*DeleteGroupRequest, error) {
	var parsedRequest DeleteGroupRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	baseRequest := group.DeleteGroupRequest{}

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		logger.Error("unable-get-group-id-from-uri")
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

// MapRequestToAddGroupMemberRequest preserves the requested new member while
// binding the actor to verified context and the group to the URL after decoding.
func MapRequestToAddGroupMemberRequest(r *http.Request, validator UsermanagerValidator) (*AddGroupMemberRequest, error) {
	var parsedRequest AddGroupMemberRequest = AddGroupMemberRequest{
		AddMemberRequest: &group.AddMemberRequest{},
	}
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		logger.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	if err := toolbox.DecodeRequestBody(r, parsedRequest.AddMemberRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.GroupID = groupID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("add-group-member-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToRemoveGroupMemberRequest maps a remove-member request to the correct struct
func MapRequestToRemoveGroupMemberRequest(r *http.Request, validator UsermanagerValidator) (*RemoveGroupMemberRequest, error) {
	var parsedRequest RemoveGroupMemberRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
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
		logger.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	baseRequest.GroupID = groupID

	memberID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableMemberID)
	if err != nil {
		logger.Error("unable-get-member-id-from-uri")
		return nil, ErrInvalidMemberID
	}
	baseRequest.MemberID = memberID

	parsedRequest.RemoveMemberRequest = &baseRequest

	return &parsedRequest, nil
}

// MapRequestToUpdateGroupMemberRequest binds the verified actor and both URL
// identifiers independently of the requested role in the body.
func MapRequestToUpdateGroupMemberRequest(r *http.Request, validator UsermanagerValidator) (*UpdateGroupMemberRequest, error) {
	var parsedRequest UpdateGroupMemberRequest = UpdateGroupMemberRequest{
		UpdateMemberRoleRequest: &group.UpdateMemberRoleRequest{},
	}
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		logger.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	memberID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableMemberID)
	if err != nil {
		logger.Error("unable-get-member-id-from-uri")
		return nil, ErrInvalidMemberID
	}
	if err := toolbox.DecodeRequestBody(r, parsedRequest.UpdateMemberRoleRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.GroupID = groupID
	parsedRequest.MemberID = memberID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Error("update-group-member-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToUpdateGroupOwnerRequest accepts the proposed owner, but binds the
// acting user and group independently. The service authorizes the transfer.
func MapRequestToUpdateGroupOwnerRequest(r *http.Request, validator UsermanagerValidator) (*UpdateGroupOwnerRequest, error) {
	var parsedRequest UpdateGroupOwnerRequest = UpdateGroupOwnerRequest{
		UpdateOwnerRequest: &group.UpdateOwnerRequest{},
	}
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	groupID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableGroupID)
	if err != nil {
		logger.Error("unable-get-group-id-from-uri")
		return nil, ErrRequestFailedValidation
	}
	if err := toolbox.DecodeRequestBody(r, parsedRequest.UpdateOwnerRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	parsedRequest.GroupID = groupID

	return &parsedRequest, nil
}

// MapRequestToValidateGroupNameRequest maps incoming ValidateGroupName request to correct struct
func MapRequestToValidateGroupNameRequest(r *http.Request, validator UsermanagerValidator) (*ValidateGroupNameRequest, error) {
	var parsedRequest ValidateGroupNameRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
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
		logger.Error("validate-group-name-request-validation-failed", zap.Error(err))
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToCreateReminderRequest maps incoming create reminder request to the correct struct.
func MapRequestToCreateReminderRequest(r *http.Request, validator UsermanagerValidator) (*CreateReminderRequest, error) {
	var parsedRequest CreateReminderRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	userID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if userID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	baseRequest := reminder.CreateReminderRequest{}
	if err := toolbox.DecodeRequestBody(r, &baseRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	baseRequest.UserID = userID

	parsedRequest.ActorID = userID
	parsedRequest.CreateReminderRequest = &baseRequest

	return &parsedRequest, nil
}

// MapRequestToGetReminderByIDRequest maps incoming get-reminder-by-ID request to the correct struct.
func MapRequestToGetReminderByIDRequest(r *http.Request, validator UsermanagerValidator) (*GetReminderByIDRequest, error) {
	var parsedRequest GetReminderByIDRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	userID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if userID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	reminderID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableReminderID)
	if err != nil {
		logger.Error("unable-get-reminder-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.ActorID = userID
	parsedRequest.Id = reminderID

	return &parsedRequest, nil
}

// MapRequestToListRemindersRequest maps incoming reminder list request to the correct struct.
func MapRequestToListRemindersRequest(r *http.Request, validator UsermanagerValidator) (*ListRemindersRequest, error) {
	var parsedRequest ListRemindersRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	userID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if userID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	parsedRequest.ActorID = userID

	if err := querydecoder.New(r.URL.Query()).Decode(&parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToUpdateReminderByIDRequest maps incoming update-reminder-by-ID request to the correct struct.
func MapRequestToUpdateReminderByIDRequest(r *http.Request, validator UsermanagerValidator) (*UpdateReminderByIDRequest, error) {
	var parsedRequest UpdateReminderByIDRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	userID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if userID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	reminderID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableReminderID)
	if err != nil {
		logger.Error("unable-get-reminder-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	baseRequest := reminder.UpdateReminderByIDRequest{}
	if err := toolbox.DecodeRequestBody(r, &baseRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	baseRequest.UserID = userID
	baseRequest.Id = reminderID

	parsedRequest.ActorID = userID
	parsedRequest.Id = reminderID
	parsedRequest.UpdateReminderByIDRequest = &baseRequest

	return &parsedRequest, nil
}

// MapRequestToDeleteReminderByIDRequest maps incoming delete-reminder-by-ID request to the correct struct.
func MapRequestToDeleteReminderByIDRequest(r *http.Request, validator UsermanagerValidator) (*DeleteReminderByIDRequest, error) {
	var parsedRequest DeleteReminderByIDRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	userID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if userID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	reminderID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableReminderID)
	if err != nil {
		logger.Error("unable-get-reminder-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.ActorID = userID
	parsedRequest.Id = reminderID

	return &parsedRequest, nil
}

// MapRequestToDisableReminderByIDRequest maps incoming disable-reminder-by-ID request to the correct struct.
func MapRequestToDisableReminderByIDRequest(r *http.Request, validator UsermanagerValidator) (*DisableReminderByIDRequest, error) {
	var parsedRequest DisableReminderByIDRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	userID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if userID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	reminderID, err := toolbox.GetVariableValueFromUri(r, UserManagerURIVariableReminderID)
	if err != nil {
		logger.Error("unable-get-reminder-id-from-uri")
		return nil, ErrRequestFailedValidation
	}

	parsedRequest.ActorID = userID
	parsedRequest.Id = reminderID

	return &parsedRequest, nil
}

// MapRequestToGetReminderStatsRequest maps incoming reminder stats request to the correct struct.
func MapRequestToGetReminderStatsRequest(r *http.Request, validator UsermanagerValidator) (*GetReminderStatsRequest, error) {
	var parsedRequest GetReminderStatsRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	if err := querydecoder.New(r.URL.Query()).Decode(&parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	return &parsedRequest, nil
}

// MapRequestToGetDueRemindersRequest maps incoming due reminders request to the correct struct.
func MapRequestToGetDueRemindersRequest(r *http.Request, validator UsermanagerValidator) (*GetDueRemindersRequest, error) {
	var parsedRequest GetDueRemindersRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	if err := querydecoder.New(r.URL.Query()).Decode(&parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}

	if parsedRequest.Limit <= 0 {
		parsedRequest.Limit = 100
	}

	return &parsedRequest, nil
}

// MapRequestToRecordStreakRequest maps incoming record-streak request to the correct struct.
func MapRequestToRecordStreakRequest(r *http.Request, validator UsermanagerValidator) (*RecordStreakRequest, error) {
	var parsedRequest RecordStreakRequest
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	userID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if userID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	baseRequest := streaker.RecordStreakRequest{}
	if err := toolbox.DecodeRequestBody(r, &baseRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	baseRequest.OwnerId = userID
	baseRequest.CreatedByUserId = userID

	parsedRequest.ActorID = userID
	parsedRequest.RecordStreakRequest = &baseRequest

	return &parsedRequest, nil
}

// MapRequestToListStreaksRequest maps incoming list-streaks request to the correct struct.
func MapRequestToListStreaksRequest(r *http.Request, validator UsermanagerValidator) (*ListStreaksRequest, error) {
	parsedRequest := ListStreaksRequest{ListStreaksRequest: &streaker.ListStreaksRequest{}}
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	if err := querydecoder.New(r.URL.Query()).Decode(&parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	if err := querydecoder.New(r.URL.Query()).Decode(parsedRequest.ListStreaksRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	if parsedRequest.PeriodType == "" {
		parsedRequest.PeriodType = streaker.StreakPeriodTypeDaily
	}

	return &parsedRequest, nil
}

// MapRequestToGetCurrentStreakRequest maps incoming current-streak request to the correct struct.
func MapRequestToGetCurrentStreakRequest(r *http.Request, validator UsermanagerValidator) (*GetCurrentStreakRequest, error) {
	parsedRequest := GetCurrentStreakRequest{GetCurrentCountRequest: &streaker.GetCurrentCountRequest{}}
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	if err := querydecoder.New(r.URL.Query()).Decode(&parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	if err := querydecoder.New(r.URL.Query()).Decode(parsedRequest.GetCurrentCountRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	if parsedRequest.PeriodType == "" {
		parsedRequest.PeriodType = streaker.StreakPeriodTypeDaily
	}

	return &parsedRequest, nil
}

// MapRequestToGetLongestStreakRequest maps incoming longest-streak request to the correct struct.
func MapRequestToGetLongestStreakRequest(r *http.Request, validator UsermanagerValidator) (*GetLongestStreakRequest, error) {
	parsedRequest := GetLongestStreakRequest{GetLongestStreakRequest: &streaker.GetLongestStreakRequest{}}
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	if err := querydecoder.New(r.URL.Query()).Decode(&parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	if err := querydecoder.New(r.URL.Query()).Decode(parsedRequest.GetLongestStreakRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	if parsedRequest.PeriodType == "" {
		parsedRequest.PeriodType = streaker.StreakPeriodTypeDaily
	}

	return &parsedRequest, nil
}

// MapRequestToGetNumberOfStreaksRequest maps incoming streak-count request to the correct struct.
func MapRequestToGetNumberOfStreaksRequest(r *http.Request, validator UsermanagerValidator) (*GetNumberOfStreaksRequest, error) {
	parsedRequest := GetNumberOfStreaksRequest{GetNumberOfStreaksRequest: &streaker.GetNumberOfStreaksRequest{}}
	logger := logger.AcquirePackageFrom(r.Context(), "external/usermanager")

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if parsedRequest.ActorID == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrUnableToIdentifyUser
	}

	if err := querydecoder.New(r.URL.Query()).Decode(&parsedRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	if err := querydecoder.New(r.URL.Query()).Decode(parsedRequest.GetNumberOfStreaksRequest); err != nil {
		return nil, ErrRequestFailedValidation
	}
	if parsedRequest.PeriodType == "" {
		parsedRequest.PeriodType = streaker.StreakPeriodTypeDaily
	}

	return &parsedRequest, nil
}

// getOptionalVariableValueFromURI returns the trimmed mux path variable for the
// provided key, or an empty string when the variable is not present.
func getOptionalVariableValueFromURI(r *http.Request, key string) string {
	return strings.TrimSpace(mux.Vars(r)[key])
}

// isAdminNotificationRoute reports whether the request is for an admin
// notifications endpoint rather than the current user's notifications endpoint.
func isAdminNotificationRoute(r *http.Request) bool {
	path := strings.TrimRight(r.URL.Path, "/")
	if strings.Contains(path, "/me/notifications") {
		return false
	}
	return strings.HasSuffix(path, "/notifications") || strings.Contains(path, "/notifications/")
}
