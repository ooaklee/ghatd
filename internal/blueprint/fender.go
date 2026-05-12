package blueprint

import (
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
)

// MapRequestToCreateBlueprintRequest maps an incoming CreateBlueprint request.
func MapRequestToCreateBlueprintRequest(request *http.Request, validator blueprintValidator) (*CreateBlueprintRequest, error) {
	parsedRequest := &CreateBlueprintRequest{}

	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		return nil, ErrBlueprintInvalidPayload
	}

	parsedRequest.CreatedByUserID = accessmanagerhelpers.AcquireFrom(request.Context())

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrBlueprintInvalidPayload
	}

	return parsedRequest, nil
}

// MapRequestToGetBlueprintsRequest maps an incoming GetBlueprints request.
func MapRequestToGetBlueprintsRequest(request *http.Request, validator blueprintValidator) (*GetBlueprintsRequest, error) {
	parsedRequest := &GetBlueprintsRequest{}

	if err := querydecoder.New(request.URL.Query()).Decode(parsedRequest); err != nil {
		return nil, ErrBlueprintInvalidQueryParam
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrBlueprintInvalidQueryParam
	}

	return parsedRequest, nil
}

// MapRequestToGetBlueprintByIDRequest maps an incoming GetBlueprintByID request.
func MapRequestToGetBlueprintByIDRequest(request *http.Request, validator blueprintValidator) (*GetBlueprintByIDRequest, error) {
	parsedRequest := &GetBlueprintByIDRequest{}

	id, err := toolbox.GetVariableValueFromUri(request, BlueprintURIVariableID)
	if err != nil {
		return nil, ErrBlueprintIDIsRequired
	}
	parsedRequest.ID = id

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserID == "" {
		return nil, ErrBlueprintUserIDIsRequired
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrBlueprintIDIsRequired
	}

	return parsedRequest, nil
}

// validateParsedRequest validates mapped requests using the package validator.
func validateParsedRequest(request interface{}, validator blueprintValidator) error {
	if validator == nil {
		return nil
	}

	return validator.Validate(request)
}
