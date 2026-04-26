package group

import (
	"errors"
	"net/http"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
)

// MapRequestToCreateGroupRequest maps incoming CreateGroup request to correct struct
func MapRequestToCreateGroupRequest(request *http.Request, validator GroupValidator) (*CreateGroupRequest, error) {
	parsedRequest := &CreateGroupRequest{}

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyValidationFailed)
	}

	return parsedRequest, nil
}

// MapRequestToGetGroupByIDRequest maps incoming GetGroupByID request to correct struct
func MapRequestToGetGroupByIDRequest(request *http.Request, validator GroupValidator) (*GetGroupByIDRequest, error) {
	var err error
	parsedRequest := &GetGroupByIDRequest{}

	// get group id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	return parsedRequest, nil
}

// MapRequestToGetGroupLineageRequest maps incoming GetGroupLineage request to correct struct
func MapRequestToGetGroupLineageRequest(request *http.Request, validator GroupValidator) (*GetGroupLineageRequest, error) {
	var err error
	parsedRequest := &GetGroupLineageRequest{}

	// get group id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	return parsedRequest, nil
}

// MapRequestToGetGroupDescendantsRequest maps incoming GetGroupDescendants request to correct struct
func MapRequestToGetGroupDescendantsRequest(request *http.Request, validator GroupValidator) (*GetGroupDescendantsRequest, error) {
	var err error
	parsedRequest := &GetGroupDescendantsRequest{}

	// get group id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	query := request.URL.Query()
	err = querydecoder.New(query).Decode(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyValidationFailed)
	}

	return parsedRequest, nil
}

// MapRequestToGetGroupByNanoIDRequest maps incoming GetGroupByNanoID request to correct struct
func MapRequestToGetGroupByNanoIDRequest(request *http.Request, validator GroupValidator) (*GetGroupByNanoIDRequest, error) {
	var err error
	parsedRequest := &GetGroupByNanoIDRequest{}

	// get nano id from uri
	parsedRequest.NanoID, err = toolbox.GetVariableValueFromUri(request, "groupNanoID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidNanoID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidNanoID)
	}

	return parsedRequest, nil
}

// MapRequestToUpdateGroupRequest maps incoming UpdateGroup request to correct struct
func MapRequestToUpdateGroupRequest(request *http.Request, validator GroupValidator) (*UpdateGroupRequest, error) {
	var err error
	parsedRequest := &UpdateGroupRequest{}

	// get group id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyValidationFailed)
	}

	return parsedRequest, nil
}

// MapRequestToDeleteGroupRequest maps incoming DeleteGroup request to correct struct
func MapRequestToDeleteGroupRequest(request *http.Request, validator GroupValidator) (*DeleteGroupRequest, error) {
	var err error
	parsedRequest := &DeleteGroupRequest{}

	// get group id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	// get query parameters
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	return parsedRequest, nil
}

// MapRequestToGetGroupsRequest maps incoming GetGroups request to correct struct
func MapRequestToGetGroupsRequest(request *http.Request, validator GroupValidator) (*GetGroupsRequest, error) {
	var err error
	parsedRequest := &GetGroupsRequest{}

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

// MapRequestToAddMemberRequest maps incoming AddMember request to correct struct
func MapRequestToAddMemberRequest(request *http.Request, validator GroupValidator) (*AddMemberRequest, error) {
	var err error
	parsedRequest := &AddMemberRequest{}

	// get group id from uri
	parsedRequest.GroupID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyValidationFailed)
	}

	return parsedRequest, nil
}

// MapRequestToRemoveMemberRequest maps incoming RemoveMember request to correct struct
func MapRequestToRemoveMemberRequest(request *http.Request, validator GroupValidator) (*RemoveMemberRequest, error) {
	var err error
	parsedRequest := &RemoveMemberRequest{}
	query := request.URL.Query()

	// get group id from uri
	parsedRequest.GroupID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	// get member id from uri
	parsedRequest.MemberID, err = toolbox.GetVariableValueFromUri(request, "memberID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidMemberID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidMemberID)
	}

	err = querydecoder.New(query).Decode(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	return parsedRequest, nil
}

// MapRequestToUpdateMemberRoleRequest maps incoming UpdateMemberRole request to correct struct
func MapRequestToUpdateMemberRoleRequest(request *http.Request, validator GroupValidator) (*UpdateMemberRoleRequest, error) {
	var err error
	parsedRequest := &UpdateMemberRoleRequest{}

	// get group id from uri
	parsedRequest.GroupID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	// get member id from uri
	parsedRequest.MemberID, err = toolbox.GetVariableValueFromUri(request, "memberID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidMemberID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyValidationFailed)
	}

	return parsedRequest, nil
}

// MapRequestToGetGroupMembersRequest maps incoming GetGroupMembers request to correct struct
func MapRequestToGetGroupMembersRequest(request *http.Request, validator GroupValidator) (*GetGroupMembersRequest, error) {
	var err error
	parsedRequest := &GetGroupMembersRequest{}

	// get group id from uri
	parsedRequest.GroupID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	// get query parameters
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	return parsedRequest, nil
}

// MapRequestToUpdateLeadershipRequest maps incoming UpdateLeadership request to correct struct
func MapRequestToUpdateLeadershipRequest(request *http.Request, validator GroupValidator) (*UpdateLeadershipRequest, error) {
	var err error
	parsedRequest := &UpdateLeadershipRequest{}

	// get group id from uri
	parsedRequest.GroupID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupBody)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyValidationFailed)
	}

	return parsedRequest, nil
}

// MapRequestToArchiveGroupRequest maps incoming ArchiveGroup request to correct struct
func MapRequestToArchiveGroupRequest(request *http.Request, validator GroupValidator) (*ArchiveGroupRequest, error) {
	var err error
	parsedRequest := &ArchiveGroupRequest{}

	// get group id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	return parsedRequest, nil
}

// MapRequestToRestoreGroupRequest maps incoming RestoreGroup request to correct struct
func MapRequestToRestoreGroupRequest(request *http.Request, validator GroupValidator) (*RestoreGroupRequest, error) {
	var err error
	parsedRequest := &RestoreGroupRequest{}

	// get group id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	return parsedRequest, nil
}

// MapRequestToGetGroupStatsRequest maps incoming GetGroupStats request to correct struct
func MapRequestToGetGroupStatsRequest(request *http.Request, validator GroupValidator) (*GetGroupStatsRequest, error) {
	var err error
	parsedRequest := &GetGroupStatsRequest{}

	// get group id from uri
	parsedRequest.ID, err = toolbox.GetVariableValueFromUri(request, "groupID")
	if err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidGroupID)
	}

	return parsedRequest, nil
}

// MapRequestToValidateGroupNameRequest maps incoming ValidateGroupName request to correct struct
func MapRequestToValidateGroupNameRequest(request *http.Request, validator GroupValidator) (*ValidateGroupNameRequest, error) {
	parsedRequest := &ValidateGroupNameRequest{}

	query := request.URL.Query()
	err := querydecoder.New(query).Decode(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyValidationFailed)
	}

	return parsedRequest, nil
}

// validateParsedRequest validates the parsed request using the validator
func validateParsedRequest(request interface{}, validator GroupValidator) error {
	if validator == nil {
		return nil
	}

	return validator.Validate(request)
}
