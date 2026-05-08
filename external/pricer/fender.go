package pricer

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
)

// PricerValidator interface defines expected methods of a valid validator.
type PricerValidator interface {
	Validate(s interface{}) error
}

// MapRequestToCreatePricePlanRequest maps incoming CreatePricePlan request to correct struct.
func MapRequestToCreatePricePlanRequest(request *http.Request, validator PricerValidator) (*CreatePricePlanRequest, error) {
	parsedRequest := &CreatePricePlanRequest{}
	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		return nil, ErrInvalidPricePlanPayload
	}

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserID == "" {
		return nil, ErrPriceUserIDRequired
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPricePlanPayload
	}

	return parsedRequest, nil
}

// MapRequestToUpdatePricePlanRequest maps incoming UpdatePricePlan request to correct struct.
func MapRequestToUpdatePricePlanRequest(request *http.Request, validator PricerValidator) (*UpdatePricePlanRequest, error) {
	parsedRequest := &UpdatePricePlanRequest{}

	id, err := toolbox.GetVariableValueFromUri(request, "id")
	if err != nil {
		return nil, ErrPricePlanIDRequired
	}
	parsedRequest.ID = id

	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		return nil, ErrInvalidPricePlanPayload
	}
	parsedRequest.ID = id

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserID == "" {
		return nil, ErrPriceUserIDRequired
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPricePlanPayload
	}

	return parsedRequest, nil
}

// MapRequestToGetPricePlanByIDRequest maps incoming GetPricePlanByID request to correct struct.
func MapRequestToGetPricePlanByIDRequest(request *http.Request, validator PricerValidator) (*GetPricePlanByIDRequest, error) {
	parsedRequest := &GetPricePlanByIDRequest{}

	id, err := toolbox.GetVariableValueFromUri(request, "id")
	if err != nil {
		return nil, ErrPricePlanIDRequired
	}
	parsedRequest.ID = id

	if err := decodeQuery(request, parsedRequest); err != nil {
		return nil, err
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrPricePlanIDRequired
	}

	return parsedRequest, nil
}

// MapRequestToGetPricePlanBySlugRequest maps incoming GetPricePlanBySlug request to correct struct.
func MapRequestToGetPricePlanBySlugRequest(request *http.Request, validator PricerValidator) (*GetPricePlanBySlugRequest, error) {
	parsedRequest := &GetPricePlanBySlugRequest{}

	slug, err := toolbox.GetVariableValueFromUri(request, "slug")
	if err != nil {
		return nil, ErrInvalidPriceSlug
	}
	parsedRequest.Slug = slug

	if err := decodeQuery(request, parsedRequest); err != nil {
		return nil, err
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPriceSlug
	}

	return parsedRequest, nil
}

// MapRequestToGetPricePlansRequest maps incoming GetPricePlans request to correct struct.
func MapRequestToGetPricePlansRequest(request *http.Request, validator PricerValidator) (*GetPricePlansRequest, error) {
	parsedRequest := &GetPricePlansRequest{}
	if err := decodeQuery(request, parsedRequest); err != nil {
		return nil, err
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPriceQueryParam
	}

	return parsedRequest, nil
}

// MapRequestToValidatePriceSlugRequest maps incoming ValidatePriceSlug request to correct struct.
func MapRequestToValidatePriceSlugRequest(request *http.Request, validator PricerValidator) (*ValidatePriceSlugRequest, error) {
	parsedRequest := &ValidatePriceSlugRequest{}
	if err := decodeQuery(request, parsedRequest); err != nil {
		return nil, err
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPriceQueryParam
	}

	return parsedRequest, nil
}

// MapRequestToPublishPricePlanRequest maps incoming PublishPricePlan request to correct struct.
func MapRequestToPublishPricePlanRequest(request *http.Request, validator PricerValidator) (*PublishPricePlanRequest, error) {
	parsedRequest := &PublishPricePlanRequest{}

	id, err := toolbox.GetVariableValueFromUri(request, "id")
	if err != nil {
		return nil, ErrPricePlanIDRequired
	}
	parsedRequest.ID = id

	if err := decodeOptionalBody(request, parsedRequest); err != nil {
		return nil, ErrInvalidPricePlanPayload
	}
	parsedRequest.ID = id

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserID == "" {
		return nil, ErrPriceUserIDRequired
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPricePlanPayload
	}

	return parsedRequest, nil
}

// MapRequestToArchivePricePlanRequest maps incoming ArchivePricePlan request to correct struct.
func MapRequestToArchivePricePlanRequest(request *http.Request, validator PricerValidator) (*ArchivePricePlanRequest, error) {
	parsedRequest := &ArchivePricePlanRequest{}

	id, err := toolbox.GetVariableValueFromUri(request, "id")
	if err != nil {
		return nil, ErrPricePlanIDRequired
	}
	parsedRequest.ID = id

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserID == "" {
		return nil, ErrPriceUserIDRequired
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPricePlanPayload
	}

	return parsedRequest, nil
}

// MapRequestToDeletePricePlanRequest maps incoming DeletePricePlan request to correct struct.
func MapRequestToDeletePricePlanRequest(request *http.Request, validator PricerValidator) (*DeletePricePlanRequest, error) {
	parsedRequest := &DeletePricePlanRequest{}

	id, err := toolbox.GetVariableValueFromUri(request, "id")
	if err != nil {
		return nil, ErrPricePlanIDRequired
	}
	parsedRequest.ID = id

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserID == "" {
		return nil, ErrPriceUserIDRequired
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPricePlanPayload
	}

	return parsedRequest, nil
}

// MapRequestToCreateFeatureRequest maps incoming CreateFeature request to correct struct.
func MapRequestToCreateFeatureRequest(request *http.Request, validator PricerValidator) (*CreateFeatureRequest, error) {
	parsedRequest := &CreateFeatureRequest{}
	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		return nil, ErrInvalidPriceFeaturePayload
	}

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserID == "" {
		return nil, ErrPriceUserIDRequired
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPriceFeaturePayload
	}

	return parsedRequest, nil
}

// MapRequestToUpdateFeatureRequest maps incoming UpdateFeature request to correct struct.
func MapRequestToUpdateFeatureRequest(request *http.Request, validator PricerValidator) (*UpdateFeatureRequest, error) {
	parsedRequest := &UpdateFeatureRequest{}

	id, err := toolbox.GetVariableValueFromUri(request, "id")
	if err != nil {
		return nil, ErrPriceFeatureIDRequired
	}
	parsedRequest.ID = id

	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		return nil, ErrInvalidPriceFeaturePayload
	}
	parsedRequest.ID = id

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserID == "" {
		return nil, ErrPriceUserIDRequired
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPriceFeaturePayload
	}

	return parsedRequest, nil
}

// MapRequestToGetFeaturesRequest maps incoming GetFeatures request to correct struct.
func MapRequestToGetFeaturesRequest(request *http.Request, validator PricerValidator) (*GetFeaturesRequest, error) {
	parsedRequest := &GetFeaturesRequest{}
	if err := decodeQuery(request, parsedRequest); err != nil {
		return nil, err
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPriceQueryParam
	}

	return parsedRequest, nil
}

// MapRequestToDeleteFeatureRequest maps incoming DeleteFeature request to correct struct.
func MapRequestToDeleteFeatureRequest(request *http.Request, validator PricerValidator) (*DeleteFeatureRequest, error) {
	parsedRequest := &DeleteFeatureRequest{}

	id, err := toolbox.GetVariableValueFromUri(request, "id")
	if err != nil {
		return nil, ErrPriceFeatureIDRequired
	}
	parsedRequest.ID = id

	parsedRequest.UserID = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserID == "" {
		return nil, ErrPriceUserIDRequired
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidPriceFeaturePayload
	}

	return parsedRequest, nil
}

func validateParsedRequest(request interface{}, validator PricerValidator) error {
	if validator == nil {
		return nil
	}

	return validator.Validate(request)
}

func decodeQuery(request *http.Request, parsedRequest interface{}) error {
	if err := querydecoder.New(request.URL.Query()).Decode(parsedRequest); err != nil {
		return ErrInvalidPriceQueryParam
	}

	return nil
}

func decodeOptionalBody(request *http.Request, parsedRequest interface{}) error {
	if request.Body == nil || request.ContentLength == 0 {
		return nil
	}

	err := json.NewDecoder(request.Body).Decode(parsedRequest)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	return nil
}
