package accessmanager

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
	"go.uber.org/zap"
)

// MapRequestToUpdateUserEmailRequest maps incoming UpdateUserEmail request to correct struct.
func MapRequestToUpdateUserEmailRequest(request *http.Request, cookiePrefixAuthToken, cookiePrefixRefreshToken string, validator AccessmanagerValidator) (*UpdateUserEmailRequest, error) {
	var (
		log *zap.Logger = logger.AcquireFrom(request.Context()).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
		parsedRequest *UpdateUserEmailRequest = &UpdateUserEmailRequest{}
		err           error
	)

	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		log.Error("unable-decode-request-body-for-updating-user-email")
		return nil, errors.New(ErrKeyInvalidUserEmail)
	}

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		log.Error("unable-get-requestor-user-id")
		return nil, errors.New(ErrKeyUnauthorizedUnableToAttainRequestorID)
	}

	parsedRequest.TargetUserId, err = getUserIDFromURI(request)
	if err != nil {
		log.Error("unable-get-target-user-id")
		return nil, err
	}

	// get the access token from the cookie
	// check to see if request is coming with cookies
	cookie, aTokenErr := request.Cookie(cookiePrefixAuthToken)
	if aTokenErr != nil {
		log.Error("unable-get-access-token-from-cookie", zap.String("user-id", parsedRequest.UserId), zap.String("target-user-id", parsedRequest.TargetUserId))
		return nil, aTokenErr
	}

	parsedRequest.AuthToken = cookie.Value

	refreshTokenCookie, rAuthErr := request.Cookie(cookiePrefixRefreshToken)
	if rAuthErr != nil {
		log.Error("unable-get-access-token-from-cookie", zap.String("user-id", parsedRequest.UserId), zap.String("target-user-id", parsedRequest.TargetUserId))
		return nil, rAuthErr
	}

	parsedRequest.RefreshToken = refreshTokenCookie.Value

	// Add request
	parsedRequest.Request = request

	err = validator.Validate(parsedRequest)
	if err != nil {
		log.Error("unable-validate-request-for-updating-user-email")
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToLogoutUserOthersRequest maps incoming LogOutUserOthers request to correct struct.
func MapRequestToLogoutUserOthersRequest(request *http.Request, validator AccessmanagerValidator, authCookiePrefix, refreshCookiePrefix string) (*LogoutUserOthersRequest, error) {
	var (
		log *zap.Logger = logger.AcquireFrom(request.Context()).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		parsedRequest *LogoutUserOthersRequest = &LogoutUserOthersRequest{}
	)

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())

	authTokenCookie, err := request.Cookie(authCookiePrefix)
	if err != nil {
		log.Error("unable-to-get-auth-token-cookie", zap.String("user-id", parsedRequest.UserId))
		return nil, errors.New(ErrKeyInvalidAuthToken)
	}

	refreshTokenCookie, err := request.Cookie(refreshCookiePrefix)
	if err != nil {
		log.Error("unable-to-get-refresh-token-cookie", zap.String("user-id", parsedRequest.UserId))
		return nil, errors.New(ErrKeyInvalidRefreshToken)
	}

	parsedRequest.AuthToken = authTokenCookie.Value
	parsedRequest.RefreshToken = refreshTokenCookie.Value

	if err := toolbox.ValidateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidLogOutUserOthersRequest)
	}

	if parsedRequest.UserId == "" {
		log.Error("unable-get-user-id")
		return nil, errors.New(ErrKeyInvalidUserID)
	}

	return parsedRequest, nil

}

// MapRequestToOauthCallbackRequest maps incoming OauthCallback request to correct struct
func MapRequestToOauthCallbackRequest(request *http.Request, validator AccessmanagerValidator) (*OauthCallbackRequest, error) {
	parsedRequest := &OauthCallbackRequest{}

	providerName, err := getProviderNameFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.UrlUri = request.URL.Query()
	parsedRequest.RequestCookies = request.Cookies()

	if providerName == "" || len(parsedRequest.UrlUri) == 0 || len(parsedRequest.RequestCookies) == 0 {
		return nil, errors.New(ErrKeyBadRequest)
	}

	parsedRequest.Provider = providerName

	return parsedRequest, nil
}

// MapRequestToOauthLoginRequest maps incoming OauthLogin request to correct struct
func MapRequestToOauthLoginRequest(request *http.Request, validator AccessmanagerValidator) (*OauthLoginRequest, error) {
	var (
		log *zap.Logger = logger.AcquireFrom(request.Context()).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		parsedRequest OauthLoginRequest = OauthLoginRequest{}
	)

	providerName, err := getProviderNameFromURI(request)
	if err != nil {
		return nil, err
	}

	// get query params from request
	query := request.URL.Query()
	_ = querydecoder.New(query).Decode(&parsedRequest)

	if parsedRequest.RequestUrl != "" {

		decodedUriValue, err := url.PathUnescape(parsedRequest.RequestUrl)
		if err != nil {
			log.Warn("failed-to-decode-request-url-uri-for-sso-login", zap.String("encoded-request-url", parsedRequest.RequestUrl))
		}

		if err == nil {
			log.Info("request-url-uri-decoded-for-sso-login", zap.String("encoded-request-url", parsedRequest.RequestUrl))
			parsedRequest.RequestUrl = decodedUriValue
		}
	}

	if providerName == "" {
		return nil, errors.New(ErrKeyBadRequest)
	}

	parsedRequest.Provider = providerName

	return &parsedRequest, nil
}

// MapRequestToGetUserAPITokenThresholdRequest maps incoming GetUserAPITokenThreshold request to correct
// struct.
// TODO: Refactor
func MapRequestToGetUserAPITokenThresholdRequest(request *http.Request, validator AccessmanagerValidator) (*GetUserAPITokenThresholdRequest, error) {
	parsedRequest := &GetUserAPITokenThresholdRequest{}

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, errors.New(ErrKeyUnauthorizedUnableToAttainRequestorID)
	}

	userId, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	if userId != requestorID {
		return nil, errors.New(ErrKeyForbiddenUnableToAction)
	}

	parsedRequest.UserId = userId

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToGetSpecificUserAPITokensRequest maps incoming GetSpecificUserAPITokens request to correct
// struct.
func MapRequestToGetSpecificUserAPITokensRequest(request *http.Request, validator AccessmanagerValidator) (*GetSpecificUserAPITokensRequest, error) {

	var err error
	parsedRequest := GetSpecificUserAPITokensRequest{}
	baseRequest := apitoken.GetAPITokensForRequest{}

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, errors.New(ErrKeyUnauthorizedUnableToAttainRequestorID)
	}

	// Add used Id from uri
	parsedRequest.UserID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, err
	}

	if parsedRequest.UserID != requestorID {
		return nil, errors.New(ErrKeyForbiddenUnableToAction)
	}

	// get query params from request
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidResultQueryParam)
	}

	parsedRequest.GetAPITokensForRequest = &baseRequest

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return &parsedRequest, nil
}

// MapRequestToRevokeUserAPITokenRequest maps incoming ActivateUserAPIToken request to correct
// struct.
// TODO: Refactor
func MapRequestToRevokeUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*UserAPITokenStatusRequest, error) {
	var (
		parsedRequest = &UserAPITokenStatusRequest{}
		err           error
	)

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, errors.New(ErrKeyUnauthorizedUnableToAttainRequestorID)
	}

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	if userID != requestorID {
		return nil, errors.New(ErrKeyForbiddenUnableToAction)
	}

	parsedRequest.APITokenID, err = getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.Status = AccessManagerUserTokenStatusKeyRevoked

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToActivateUserAPITokenRequest maps incoming ActivateUserAPIToken request to correct
// struct.
// TODO: Refactor
func MapRequestToActivateUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*UserAPITokenStatusRequest, error) {

	var (
		parsedRequest = &UserAPITokenStatusRequest{}
		err           error
	)

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, errors.New(ErrKeyUnauthorizedUnableToAttainRequestorID)
	}

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	if userID != requestorID {
		return nil, errors.New(ErrKeyForbiddenUnableToAction)
	}

	parsedRequest.APITokenID, err = getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.Status = AccessManagerUserTokenStatusKeyActive

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToDeleteUserAPITokenRequest maps incoming DeleteUserAPIToken request to correct
// struct.
func MapRequestToDeleteUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*DeleteUserAPITokenRequest, error) {
	var (
		parsedRequest = &DeleteUserAPITokenRequest{}
		err           error
	)

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, errors.New(ErrKeyUnauthorizedUnableToAttainRequestorID)
	}

	parsedRequest.UserID, err = getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	if parsedRequest.UserID != requestorID {
		return nil, errors.New(ErrKeyForbiddenUnableToAction)
	}

	parsedRequest.APITokenID, err = getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToCreateUserAPITokenRequest maps incoming CreateUserAPIToken request to correct
// struct.
func MapRequestToCreateUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*CreateUserAPITokenRequest, error) {

	var (
		log *zap.Logger = logger.AcquireFrom(request.Context()).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		parsedRequest *CreateUserAPITokenRequest = &CreateUserAPITokenRequest{}
		err           error
	)

	// Default to permanent
	parsedRequest.Ttl = 0

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, errors.New(ErrKeyUnauthorizedUnableToAttainRequestorID)
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		bodyBytes, err := ioutil.ReadAll(request.Body)
		if err != nil {
			log.Error("unable-to-decode-create-user-api-token-request")
		}

		if err == nil {
			log.Error("unable-to-decode-create-user-api-token-request", zap.Any("request-body", bodyBytes))
		}
		return nil, errors.New(ErrKeyInvalidCreateUserAPITokenBody)
	}

	parsedRequest.UserID, err = getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	if parsedRequest.UserID != requestorID {
		return nil, errors.New(ErrKeyForbiddenUnableToAction)
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToRefreshTokenRequest maps incoming RefreshToken request to correct
// struct.
func MapRequestToRefreshTokenRequest(request *http.Request, refreshCookieName, accessCookieName string, validator AccessmanagerValidator) (*RefreshTokenRequest, error) {
	parsedRequest := &RefreshTokenRequest{}

	// TODO: Create a NoAuth Middleware where this can be done
	refreshCookie, err := request.Cookie(refreshCookieName)
	if err != nil && err != http.ErrNoCookie {
		return nil, err
	}
	if refreshCookie != nil {
		parsedRequest.RefreshToken = refreshCookie.Value
	}

	if err == http.ErrNoCookie {
		err := toolbox.DecodeRequestBody(request, parsedRequest)
		if err != nil {
			return nil, errors.New(ErrKeyInvalidRefreshToken)
		}
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidRefreshToken)
	}

	// Check to see if we have an access token with request
	accessCookie, err := request.Cookie(accessCookieName)
	if err != nil {
		return parsedRequest, nil
	}

	parsedRequest.AccessToken = accessCookie.Value

	return parsedRequest, nil
}

// MapRequestToCreateInitalLoginOrVerificationTokenEmailRequest maps incoming CreateInitalLoginOrVerificationTokenEmail request
// to correct struct
func MapRequestToCreateInitalLoginOrVerificationTokenEmailRequest(request *http.Request, validator AccessmanagerValidator) (*CreateInitalLoginOrVerificationTokenEmailRequest, error) {

	var (
		log *zap.Logger = logger.AcquireFrom(request.Context()).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		parsedRequest *CreateInitalLoginOrVerificationTokenEmailRequest = &CreateInitalLoginOrVerificationTokenEmailRequest{}
	)

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserEmail)
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserEmail)
	}

	log.Debug("login-request-submitted.", zap.String("email", parsedRequest.Email))

	return parsedRequest, nil

}

// MapRequestToCreateUserRequest maps incoming CreateUser request to correct
// struct.
func MapRequestToCreateUserRequest(request *http.Request, validator AccessmanagerValidator) (*CreateUserRequest, error) {
	parsedRequest := &CreateUserRequest{}

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// MapRequestToValidateEmailVerificationCodeRequest maps incoming ValidateEmailVerificationCode request to correct
// struct.
func MapRequestToValidateEmailVerificationCodeRequest(request *http.Request, validator AccessmanagerValidator) (*ValidateEmailVerificationCodeRequest, error) {
	var err error
	parsedRequest := ValidateEmailVerificationCodeRequest{}

	// get query params from request
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidVerificationToken)
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidVerificationToken)
	}

	return &parsedRequest, nil
}

// MapRequestToLoginUserRequest maps incoming LoginUser request to correct
// struct.
func MapRequestToLoginUserRequest(request *http.Request, validator AccessmanagerValidator) (*LoginUserRequest, error) {
	var err error
	parsedRequest := LoginUserRequest{}

	// get query params from request
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(&parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidVerificationToken)
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidVerificationToken)
	}

	return &parsedRequest, nil
}

// getUserIDFromURI pulls userID from URI. If fails, returns error
func getUserIDFromURI(request *http.Request) (string, error) {
	var userID string

	if userID = mux.Vars(request)[UserURIVariableID]; userID == "" {
		return "", errors.New(ErrKeyInvalidUserID)
	}

	return userID, nil
}

// getTokenIDFromURI pulls token ID from URI. If fails, returns error
func getTokenIDFromURI(request *http.Request) (string, error) {
	var tokenID string

	if tokenID = mux.Vars(request)[APITokenURIVariableID]; tokenID == "" {
		return "", errors.New(ErrKeyInvalidAPITokenID)
	}

	return tokenID, nil
}

// getProviderNameFromURI pulls the provider name from Uri. If fails, returns error
func getProviderNameFromURI(request *http.Request) (string, error) {

	providerName := strings.Split(
		strings.ReplaceAll(request.RequestURI, fmt.Sprintf("%s/ams/oauth/", common.ApiV1UriPrefix), ""),
		"/",
	)[0]

	return providerName, nil
}
