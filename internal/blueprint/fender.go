package blueprint

import (
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
	"go.uber.org/zap"
)

// MapRequestToCreateBlueprintRequest maps an incoming CreateBlueprint request.
func MapRequestToCreateBlueprintRequest(request *http.Request, validator blueprintValidator) (*CreateBlueprintRequest, error) {
	if err := validateBlueprintHTTPRequest(request, true); err != nil {
		return nil, err
	}
	if nilBlueprintDependency(request.Body) {
		return nil, ErrBlueprintInvalidPayload
	}
	parsedRequest := &CreateBlueprintRequest{}
	logger := logger.AcquireOperationFrom(request.Context(), "internal/blueprint", "map-create-blueprint-request")

	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		logger.Warn("blueprint-request-body-decode-failed")
		return nil, ErrBlueprintInvalidPayload
	}

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(request.Context())

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Warn("blueprint-request-validation-failed")
		return nil, ErrBlueprintInvalidPayload
	}

	logger.Debug("blueprint-create-request-mapped")
	return parsedRequest, nil
}

// MapRequestToGetBlueprintsRequest maps an incoming GetBlueprints request.
func MapRequestToGetBlueprintsRequest(request *http.Request, validator blueprintValidator) (*GetBlueprintsRequest, error) {
	if err := validateBlueprintHTTPRequest(request, false); err != nil {
		return nil, err
	}
	parsedRequest := &GetBlueprintsRequest{}
	logger := logger.AcquireOperationFrom(request.Context(), "internal/blueprint", "map-get-blueprints-request")

	if err := querydecoder.New(request.URL.Query()).Decode(parsedRequest); err != nil {
		logger.Warn("blueprint-query-decode-failed")
		return nil, ErrBlueprintInvalidQueryParam
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Warn("blueprint-query-validation-failed")
		return nil, ErrBlueprintInvalidQueryParam
	}

	logger.Debug("blueprint-list-request-mapped", zap.Int64("page", parsedRequest.Page), zap.Int64("page-size", parsedRequest.PageSize))
	return parsedRequest, nil
}

// MapRequestToGetBlueprintByIDRequest maps an incoming GetBlueprintByID request.
func MapRequestToGetBlueprintByIDRequest(request *http.Request, validator blueprintValidator) (*GetBlueprintByIDRequest, error) {
	if err := validateBlueprintHTTPRequest(request, true); err != nil {
		return nil, err
	}
	parsedRequest := &GetBlueprintByIDRequest{}
	logger := logger.AcquireOperationFrom(request.Context(), "internal/blueprint", "map-get-blueprint-by-id-request")

	id, err := toolbox.GetVariableValueFromUri(request, BlueprintURIVariableID)
	if err != nil {
		logger.Warn("blueprint-id-uri-variable-missing")
		return nil, ErrBlueprintIDIsRequired
	}
	parsedRequest.ID = id

	parsedRequest.ActorID = accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(request.Context())

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Warn("blueprint-id-request-validation-failed")
		return nil, ErrBlueprintIDIsRequired
	}

	logger.Debug("blueprint-id-request-mapped")
	return parsedRequest, nil
}

// validateParsedRequest validates mapped requests using the package validator.
func validateParsedRequest(request interface{}, validator blueprintValidator) error {
	if validator == nil {
		return nil
	}

	return validator.Validate(request)
}
