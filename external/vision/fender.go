package vision

import (
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
)

// MapRequestToCreateVisionRequest maps a create request.
func MapRequestToCreateVisionRequest(request *http.Request, validator visionValidator) (*CreateVisionRequest, error) {
	parsed := &CreateVisionRequest{}
	if err := toolbox.DecodeRequestBody(request, parsed); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	parsed.CreatedByUserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	return parsed, nil
}

// MapRequestToGetVisionsRequest maps list filters.
func MapRequestToGetVisionsRequest(request *http.Request, validator visionValidator) (*GetVisionsRequest, error) {
	parsed := &GetVisionsRequest{}
	if err := querydecoder.New(request.URL.Query()).Decode(parsed); err != nil {
		return nil, ErrVisionInvalidQueryParam
	}
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionInvalidQueryParam
	}
	return parsed, nil
}

// MapRequestToGetVisionByNanoIDRequest maps a vision path NanoID.
func MapRequestToGetVisionByNanoIDRequest(request *http.Request, validator visionValidator) (*GetVisionByNanoIDRequest, error) {
	nanoID, err := visionNanoIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	parsed := &GetVisionByNanoIDRequest{NanoID: nanoID}
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionNanoIDIsRequired
	}
	return parsed, nil
}

// MapRequestToUpdateVisionRequest maps a descriptive update.
func MapRequestToUpdateVisionRequest(request *http.Request, validator visionValidator) (*UpdateVisionRequest, error) {
	parsed := &UpdateVisionRequest{}
	if err := toolbox.DecodeRequestBody(request, parsed); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	nanoID, err := visionNanoIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	parsed.NanoID = nanoID
	parsed.UpdatedByUserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	return parsed, nil
}

// MapRequestToUpdateVisionStatusRequest maps a roadmap transition.
func MapRequestToUpdateVisionStatusRequest(request *http.Request, validator visionValidator) (*UpdateVisionStatusRequest, error) {
	parsed := &UpdateVisionStatusRequest{}
	if err := toolbox.DecodeRequestBody(request, parsed); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	nanoID, err := visionNanoIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	parsed.NanoID = nanoID
	parsed.UpdatedByUserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	return parsed, nil
}

// MapRequestToSetVisionVoteRequest maps a vote and always sources the user ID
// from authenticated context.
func MapRequestToSetVisionVoteRequest(request *http.Request, validator visionValidator) (*SetVisionVoteRequest, error) {
	parsed := &SetVisionVoteRequest{}
	if err := toolbox.DecodeRequestBody(request, parsed); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	nanoID, err := visionNanoIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	parsed.NanoID = nanoID
	parsed.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	return parsed, nil
}

// MapRequestToRemoveVisionVoteRequest maps vote removal.
func MapRequestToRemoveVisionVoteRequest(request *http.Request, validator visionValidator) (*RemoveVisionVoteRequest, error) {
	nanoID, err := visionNanoIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	parsed := &RemoveVisionVoteRequest{
		NanoID: nanoID,
		UserID: accessmanagerhelpers.AcquireFrom(request.Context()),
	}
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	return parsed, nil
}

// MapRequestToAddVisionCommentRequest maps a comment append.
func MapRequestToAddVisionCommentRequest(request *http.Request, validator visionValidator) (*AddVisionCommentRequest, error) {
	parsed := &AddVisionCommentRequest{}
	if err := toolbox.DecodeRequestBody(request, parsed); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	nanoID, err := visionNanoIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	parsed.NanoID = nanoID
	parsed.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	return parsed, nil
}

// MapRequestToSetVisionCommentVoteRequest maps a nested comment vote.
func MapRequestToSetVisionCommentVoteRequest(request *http.Request, validator visionValidator) (*SetVisionCommentVoteRequest, error) {
	parsed := &SetVisionCommentVoteRequest{}
	if err := toolbox.DecodeRequestBody(request, parsed); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	nanoID, err := visionNanoIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	commentID, err := visionCommentIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	parsed.NanoID = nanoID
	parsed.CommentID = commentID
	parsed.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	return parsed, nil
}

// MapRequestToRemoveVisionCommentVoteRequest maps nested comment vote removal.
func MapRequestToRemoveVisionCommentVoteRequest(request *http.Request, validator visionValidator) (*RemoveVisionCommentVoteRequest, error) {
	nanoID, err := visionNanoIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	commentID, err := visionCommentIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	parsed := &RemoveVisionCommentVoteRequest{
		NanoID:    nanoID,
		CommentID: commentID,
		UserID:    accessmanagerhelpers.AcquireFrom(request.Context()),
	}
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionInvalidPayload
	}
	return parsed, nil
}

// MapRequestToDeleteVisionRequest maps a delete path NanoID.
func MapRequestToDeleteVisionRequest(request *http.Request, validator visionValidator) (*DeleteVisionRequest, error) {
	nanoID, err := visionNanoIDFromRequest(request)
	if err != nil {
		return nil, err
	}
	parsed := &DeleteVisionRequest{NanoID: nanoID}
	if err := validateParsedRequest(parsed, validator); err != nil {
		return nil, ErrVisionNanoIDIsRequired
	}
	return parsed, nil
}

// visionNanoIDFromRequest extracts the vision NanoID from the request URI.
func visionNanoIDFromRequest(request *http.Request) (string, error) {
	nanoID, err := toolbox.GetVariableValueFromUri(request, VisionURIVariableNanoID)
	if err != nil || nanoID == "" {
		return "", ErrVisionNanoIDIsRequired
	}
	return nanoID, nil
}

// visionCommentIDFromRequest extracts the comment ID from the request URI.
func visionCommentIDFromRequest(request *http.Request) (string, error) {
	id, err := toolbox.GetVariableValueFromUri(request, VisionURIVariableCommentID)
	if err != nil || id == "" {
		return "", ErrVisionCommentNotFound
	}
	return id, nil
}

// validateParsedRequest runs the request through the validator when present.
func validateParsedRequest(request interface{}, validator visionValidator) error {
	if validator == nil {
		return nil
	}
	return validator.Validate(request)
}
