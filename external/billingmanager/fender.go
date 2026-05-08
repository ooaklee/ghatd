package billingmanager

import (
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
	"go.uber.org/zap"
)

// mapRequestToProcessBillingProviderWebhooksRequest maps incoming request to the correct struct
func mapRequestToProcessBillingProviderWebhooksRequest(request *http.Request, validator BillingManagerValidator) (*ProcessBillingProviderWebhooksRequest, error) {

	var parsedRequest ProcessBillingProviderWebhooksRequest
	log := logger.AcquireFrom(request.Context())

	providerName, err := toolbox.GetVariableValueFromUri(request, "providerName")
	if err != nil {
		log.Error("unable-get-provider-name-from-uri", zap.Any("request", request), zap.Error(err))
		return nil, ErrBillingManagerUnableToGetProviderNameFromURI
	}

	parsedRequest.ProviderName = providerName
	parsedRequest.Request = request

	return &parsedRequest, nil
}

// mapRequestToGetUserBillingEventsRequest maps incoming GetUserBillingEvents request to correct
// struct.
func mapRequestToGetUserBillingEventsRequest(request *http.Request, validator BillingManagerValidator) (*GetUserBillingEventsRequest, error) {
	var parsedRequest GetUserBillingEventsRequest
	log := logger.AcquireFrom(request.Context())
	requestingUserId := accessmanagerhelpers.AcquireFrom(request.Context())

	if requestingUserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrBillingManagerUnableToIdentifyUser
	}

	userId, err := toolbox.GetVariableValueFromUri(request, "userId")
	if err != nil {
		log.Error("unable-get-user-id-from-uri", zap.Any("request", request), zap.Error(err))
		return nil, ErrBillingManagerUnableToGetUserIdFromURI
	}

	query := request.URL.Query()
	err = querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		log.Error("unable-to-decode-query-to-billing-events-request", zap.Any("request", request), zap.Error(err))
		return nil, ErrInvalidBillingManagerRequestPayload
	}

	parsedRequest.UserID = userId
	parsedRequest.RequestingUserID = requestingUserId

	return &parsedRequest, nil
}

// mapRequestToGetUserSubscriptionStatusRequest maps incoming GetUserSubscriptionStatus request to correct
// struct.
func mapRequestToGetUserSubscriptionStatusRequest(request *http.Request, validator BillingManagerValidator) (*GetUserSubscriptionStatusRequest, error) {
	var parsedRequest GetUserSubscriptionStatusRequest
	log := logger.AcquireFrom(request.Context())
	requestingUserId := accessmanagerhelpers.AcquireFrom(request.Context())

	if requestingUserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrBillingManagerUnableToIdentifyUser
	}

	userId, err := toolbox.GetVariableValueFromUri(request, "userId")
	if err != nil {
		log.Error("unable-get-user-id-from-uri", zap.Any("request", request), zap.Error(err))
		return nil, ErrBillingManagerUnableToGetUserIdFromURI
	}

	parsedRequest.UserID = userId
	parsedRequest.RequestingUserID = requestingUserId

	return &parsedRequest, nil
}

// mapRequestToGetUserBillingDetailRequest maps incoming GetUserBillingDetail request to correct
// struct.
func mapRequestToGetUserBillingDetailRequest(request *http.Request, validator BillingManagerValidator) (*GetUserBillingDetailRequest, error) {
	var parsedRequest GetUserBillingDetailRequest
	log := logger.AcquireFrom(request.Context())
	requestingUserId := accessmanagerhelpers.AcquireFrom(request.Context())

	if requestingUserId == "" {
		log.Error("unable-get-user-id")
		return nil, ErrBillingManagerUnableToIdentifyUser
	}

	userId, err := toolbox.GetVariableValueFromUri(request, "userId")
	if err != nil {
		log.Error("unable-get-user-id-from-uri", zap.Any("request", request), zap.Error(err))
		return nil, ErrBillingManagerUnableToGetUserIdFromURI
	}

	parsedRequest.UserID = userId
	parsedRequest.RequestingUserID = requestingUserId

	return &parsedRequest, nil
}

// MapRequestToGetPricingPlansRequest maps incoming BMS pricing plan list requests.
func MapRequestToGetPricingPlansRequest(request *http.Request, validator BillingManagerValidator) (*GetPricingPlansRequest, error) {
	parsedRequest, err := pricer.MapRequestToGetPricePlansRequest(request, validator)
	if err != nil {
		return nil, err
	}

	return &GetPricingPlansRequest{
		UserID:               accessmanagerhelpers.AcquireFrom(request.Context()),
		GetPricePlansRequest: parsedRequest,
	}, nil
}

// MapRequestToGetPricePlanBySlugRequest maps incoming BMS price plan slug requests.
func MapRequestToGetPricePlanBySlugRequest(request *http.Request, validator BillingManagerValidator) (*GetPricePlanBySlugRequest, error) {
	parsedRequest, err := pricer.MapRequestToGetPricePlanBySlugRequest(request, validator)
	if err != nil {
		return nil, err
	}

	return &GetPricePlanBySlugRequest{
		UserID:                    accessmanagerhelpers.AcquireFrom(request.Context()),
		GetPricePlanBySlugRequest: parsedRequest,
	}, nil
}

// MapRequestToGetPriceFeaturesRequest maps incoming BMS price feature list requests.
func MapRequestToGetPriceFeaturesRequest(request *http.Request, validator BillingManagerValidator) (*GetPriceFeaturesRequest, error) {
	parsedRequest, err := pricer.MapRequestToGetFeaturesRequest(request, validator)
	if err != nil {
		return nil, err
	}

	return &GetPriceFeaturesRequest{
		UserID:             accessmanagerhelpers.AcquireFrom(request.Context()),
		GetFeaturesRequest: parsedRequest,
	}, nil
}
