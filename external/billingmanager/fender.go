package billingmanager

import (
	"errors"
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/logger"
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
		return nil, errors.New(ErrKeyBillingManagerUnableToGetProviderNameFromURI)
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
		return nil, errors.New(ErrKeyBillingManagerUnableToIdentifyUser)
	}

	userId, err := toolbox.GetVariableValueFromUri(request, "userId")
	if err != nil {
		log.Error("unable-get-user-id-from-uri", zap.Any("request", request), zap.Error(err))
		return nil, errors.New(ErrKeyBillingManagerUnableToGetUserIdFromURI)
	}

	query := request.URL.Query()
	err = querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		log.Error("unable-to-decode-query-to-billing-events-request", zap.Any("request", request), zap.Error(err))
		return nil, errors.New(ErrKeyInvalidBillingManagerRequestPayload)
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
		return nil, errors.New(ErrKeyBillingManagerUnableToIdentifyUser)
	}

	userId, err := toolbox.GetVariableValueFromUri(request, "userId")
	if err != nil {
		log.Error("unable-get-user-id-from-uri", zap.Any("request", request), zap.Error(err))
		return nil, errors.New(ErrKeyBillingManagerUnableToGetUserIdFromURI)
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
		return nil, errors.New(ErrKeyBillingManagerUnableToIdentifyUser)
	}

	userId, err := toolbox.GetVariableValueFromUri(request, "userId")
	if err != nil {
		log.Error("unable-get-user-id-from-uri", zap.Any("request", request), zap.Error(err))
		return nil, errors.New(ErrKeyBillingManagerUnableToGetUserIdFromURI)
	}

	parsedRequest.UserID = userId
	parsedRequest.RequestingUserID = requestingUserId

	return &parsedRequest, nil
}
