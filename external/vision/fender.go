package vision

import (
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
	"go.uber.org/zap"
)

// MapRequestToCreateVisionRequest maps an incoming CreateVision request.
func MapRequestToCreateVisionRequest(request *http.Request, validator visionValidator) (*CreateVisionRequest, error) {
	parsedRequest := &CreateVisionRequest{}
	logger := logger.AcquireOperationFrom(request.Context(), "internal/vision", "map-create-vision-request")

	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		logger.Warn("vision-request-body-decode-failed", zap.Error(err))
		return nil, ErrVisionInvalidPayload
	}

	parsedRequest.CreatedByUserID = accessmanagerhelpers.AcquireFrom(request.Context())

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Warn("vision-request-validation-failed", zap.Error(err))
		return nil, ErrVisionInvalidPayload
	}

	logger.Debug("vision-create-request-mapped", zap.String("created-by-user-id", parsedRequest.CreatedByUserID), zap.String("kind", normaliseVisionKind(parsedRequest.Kind)))
	return parsedRequest, nil
}

// MapRequestToGetVisionsRequest maps an incoming GetVisions request.
func MapRequestToGetVisionsRequest(request *http.Request, validator visionValidator) (*GetVisionsRequest, error) {
	parsedRequest := &GetVisionsRequest{}
	logger := logger.AcquireOperationFrom(request.Context(), "internal/vision", "map-get-visions-request")

	if err := querydecoder.New(request.URL.Query()).Decode(parsedRequest); err != nil {
		logger.Warn("vision-query-decode-failed", zap.Error(err))
		return nil, ErrVisionInvalidQueryParam
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Warn("vision-query-validation-failed", zap.Error(err))
		return nil, ErrVisionInvalidQueryParam
	}

	logger.Debug("vision-list-request-mapped", zap.Int64("page", parsedRequest.Page), zap.Int64("page-size", parsedRequest.PageSize))
	return parsedRequest, nil
}

// MapRequestToGetVisionByIDRequest maps an incoming GetVisionByID request.
func MapRequestToGetVisionByIDRequest(request *http.Request, validator visionValidator) (*GetVisionByIDRequest, error) {
	parsedRequest := &GetVisionByIDRequest{}
	logger := logger.AcquireOperationFrom(request.Context(), "internal/vision", "map-get-vision-by-id-request")

	id, err := toolbox.GetVariableValueFromUri(request, VisionURIVariableID)
	if err != nil {
		logger.Warn("vision-id-uri-variable-missing", zap.Error(err))
		return nil, ErrVisionIDIsRequired
	}
	parsedRequest.ID = id

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserID == "" {
		logger.Warn("vision-request-user-id-missing", zap.String("vision-id", parsedRequest.ID))
		return nil, ErrVisionUserIDIsRequired
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		logger.Warn("vision-id-request-validation-failed", zap.String("vision-id", parsedRequest.ID), zap.Error(err))
		return nil, ErrVisionIDIsRequired
	}

	logger.Debug("vision-id-request-mapped", zap.String("vision-id", parsedRequest.ID), zap.String("user-id", parsedRequest.UserID))
	return parsedRequest, nil
}

// validateParsedRequest validates mapped requests using the package validator.
func validateParsedRequest(request interface{}, validator visionValidator) error {
	if validator == nil {
		return nil
	}

	return validator.Validate(request)
}
