package billingmanager

import (
	"net/http"
	"strings"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
	"go.uber.org/zap"
)

// mapRequestToProcessBillingProviderCheckoutRequest maps authenticated checkout
// transport inputs. Catalogue, user-email, return-URL, and provider validation
// remain service responsibilities.
func mapRequestToProcessBillingProviderCheckoutRequest(request *http.Request, validator BillingManagerValidator) (*ProcessBillingProviderCheckoutRequest, error) {
	if request == nil {
		return nil, ErrInvalidBillingManagerRequestPayload
	}

	parsedRequest := &ProcessBillingProviderCheckoutRequest{}
	providerName, err := toolbox.GetVariableValueFromUri(request, "providerName")
	if err != nil {
		return nil, ErrBillingManagerUnableToGetProviderNameFromURI
	}
	userID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(request.Context())
	if strings.TrimSpace(userID) == "" {
		return nil, ErrBillingManagerUnableToIdentifyUser
	}
	if request.URL == nil || querydecoder.New(request.URL.Query()).Decode(parsedRequest) != nil {
		return nil, ErrInvalidBillingManagerRequestPayload
	}

	parsedRequest.UserID = strings.TrimSpace(userID)
	parsedRequest.ProviderName = normaliseCheckoutProviderName(providerName)
	parsedRequest.PriceID = strings.TrimSpace(parsedRequest.PriceID)
	parsedRequest.IdempotencyKey = strings.TrimSpace(request.Header.Get(common.IdempotencyKeyHttpHeader))
	parsedRequest.Origin = strings.TrimSpace(request.Header.Get("Origin"))
	parsedRequest.SecFetchSite = strings.TrimSpace(request.Header.Get("Sec-Fetch-Site"))
	if !checkoutProviderNamePattern.MatchString(parsedRequest.ProviderName) || parsedRequest.PriceID == "" ||
		len(parsedRequest.PriceID) > checkoutMaxPriceIDLength || len(parsedRequest.IdempotencyKey) > checkoutMaxClientKeyLength ||
		len(parsedRequest.Origin) > checkoutMaxOriginLength || len(parsedRequest.SecFetchSite) > checkoutMaxFetchSiteLength {
		return nil, ErrInvalidBillingManagerRequestPayload
	}
	if validator != nil && validator.Validate(parsedRequest) != nil {
		return nil, ErrInvalidBillingManagerRequestPayload
	}

	return parsedRequest, nil
}

// mapRequestToProcessBillingProviderPortalRequest maps only authenticated and
// trusted transport values. Customer identifiers and return URLs are never
// accepted from the browser.
func mapRequestToProcessBillingProviderPortalRequest(request *http.Request, validator BillingManagerValidator) (*ProcessBillingProviderPortalRequest, error) {
	if request == nil {
		return nil, ErrInvalidBillingManagerRequestPayload
	}
	providerName, err := toolbox.GetVariableValueFromUri(request, "providerName")
	if err != nil {
		return nil, ErrBillingManagerUnableToGetProviderNameFromURI
	}
	userID := strings.TrimSpace(accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(request.Context()))
	if userID == "" {
		return nil, ErrBillingManagerUnableToIdentifyUser
	}
	parsedRequest := &ProcessBillingProviderPortalRequest{
		UserID:       userID,
		ProviderName: normaliseCheckoutProviderName(providerName),
		Origin:       strings.TrimSpace(request.Header.Get("Origin")),
		SecFetchSite: strings.TrimSpace(request.Header.Get("Sec-Fetch-Site")),
	}
	if !checkoutProviderNamePattern.MatchString(parsedRequest.ProviderName) ||
		len(parsedRequest.Origin) > checkoutMaxOriginLength || len(parsedRequest.SecFetchSite) > checkoutMaxFetchSiteLength {
		return nil, ErrInvalidBillingManagerRequestPayload
	}
	if validator != nil && validator.Validate(parsedRequest) != nil {
		return nil, ErrInvalidBillingManagerRequestPayload
	}
	return parsedRequest, nil
}

// mapRequestToProcessBillingProviderWebhooksRequest maps incoming request to the correct struct
func mapRequestToProcessBillingProviderWebhooksRequest(request *http.Request, validator BillingManagerValidator) (*ProcessBillingProviderWebhooksRequest, error) {

	var parsedRequest ProcessBillingProviderWebhooksRequest
	requestPath := logger.RequestPath(request)
	logger := logger.AcquirePackageFrom(request.Context(), "external/billingmanager")

	providerName, err := toolbox.GetVariableValueFromUri(request, "providerName")
	if err != nil {
		logger.Error("unable-get-provider-name-from-uri", zap.String("method", request.Method), zap.String("path", requestPath), zap.Error(err))
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
	requestPath := logger.RequestPath(request)
	logger := logger.AcquirePackageFrom(request.Context(), "external/billingmanager")
	requestingUserId := accessmanagerhelpers.AcquireFrom(request.Context())

	if requestingUserId == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrBillingManagerUnableToIdentifyUser
	}

	userId, err := toolbox.GetVariableValueFromUri(request, "userId")
	if err != nil {
		logger.Error("unable-get-user-id-from-uri", zap.String("method", request.Method), zap.String("path", requestPath), zap.Error(err))
		return nil, ErrBillingManagerUnableToGetUserIdFromURI
	}

	query := request.URL.Query()
	err = querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		logger.Error("unable-to-decode-query-to-billing-events-request", zap.String("method", request.Method), zap.String("path", requestPath), zap.Error(err))
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
	requestPath := logger.RequestPath(request)
	logger := logger.AcquirePackageFrom(request.Context(), "external/billingmanager")
	requestingUserId := accessmanagerhelpers.AcquireFrom(request.Context())

	if requestingUserId == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrBillingManagerUnableToIdentifyUser
	}

	userId, err := toolbox.GetVariableValueFromUri(request, "userId")
	if err != nil {
		logger.Error("unable-get-user-id-from-uri", zap.String("method", request.Method), zap.String("path", requestPath), zap.Error(err))
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
	requestPath := logger.RequestPath(request)
	logger := logger.AcquirePackageFrom(request.Context(), "external/billingmanager")
	requestingUserId := accessmanagerhelpers.AcquireFrom(request.Context())

	if requestingUserId == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrBillingManagerUnableToIdentifyUser
	}

	userId, err := toolbox.GetVariableValueFromUri(request, "userId")
	if err != nil {
		logger.Error("unable-get-user-id-from-uri", zap.String("method", request.Method), zap.String("path", requestPath), zap.Error(err))
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
