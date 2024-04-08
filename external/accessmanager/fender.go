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
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

const (
	AccessManagerRequestParameterKeyDefaultToken = "t"
)

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
	parsedRequest := &OauthLoginRequest{}
	log := logger.AcquireFrom(request.Context())

	providerName, err := getProviderNameFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequestUrlUri := getRequestUrlFromURI(request)

	if parsedRequestUrlUri != "" {

		decodedUriValue, err := url.PathUnescape(parsedRequestUrlUri)
		if err != nil {
			log.Warn("failed-to-decode-request-url-uri-for-sso-login", zap.String("encoded-request-url", parsedRequestUrlUri))
		}

		if err == nil {
			log.Info("request-url-uri-decoded-for-sso-login", zap.String("encoded-request-url", parsedRequestUrlUri))
			parsedRequest.RequestUrl = decodedUriValue
		}
	}

	if providerName == "" {
		return nil, errors.New(ErrKeyBadRequest)
	}

	parsedRequest.Provider = providerName

	return parsedRequest, nil
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

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToGetSpecificUserAPITokensRequest maps incoming GetSpecificUserAPITokens request to correct
// struct.
// TODO: Refactor
func MapRequestToGetSpecificUserAPITokensRequest(request *http.Request, validator AccessmanagerValidator) (*GetSpecificUserAPITokensRequest, error) {
	parsedRequest := &GetSpecificUserAPITokensRequest{}

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

	parsedRequest.UserID = userID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToRevokeUserAPITokenRequest maps incoming ActivateUserAPIToken request to correct
// struct.
// TODO: Refactor
func MapRequestToRevokeUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*UserAPITokenStatusRequest, error) {
	parsedRequest := &UserAPITokenStatusRequest{}

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

	tokenID, err := getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.APITokenID = tokenID

	parsedRequest.Status = AccessManagerUserTokenStatusKeyRevoked

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToActivateUserAPITokenRequest maps incoming ActivateUserAPIToken request to correct
// struct.
// TODO: Refactor
func MapRequestToActivateUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*UserAPITokenStatusRequest, error) {
	parsedRequest := &UserAPITokenStatusRequest{}

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

	tokenID, err := getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.APITokenID = tokenID

	parsedRequest.Status = AccessManagerUserTokenStatusKeyActive

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToDeleteUserAPITokenRequest maps incoming DeleteUserAPIToken request to correct
// struct.
func MapRequestToDeleteUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*DeleteUserAPITokenRequest, error) {
	parsedRequest := &DeleteUserAPITokenRequest{}

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, errors.New(ErrKeyUnauthorizedUnableToAttainRequestorID)
	}

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.UserID = userID

	if userID != requestorID {
		return nil, errors.New(ErrKeyForbiddenUnableToAction)
	}

	tokenID, err := getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.APITokenID = tokenID

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToCreateUserAPITokenRequest maps incoming CreateUserAPIToken request to correct
// struct.
func MapRequestToCreateUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*CreateUserAPITokenRequest, error) {
	parsedRequest := &CreateUserAPITokenRequest{}
	log := logger.AcquireFrom(request.Context())

	// Default to permanent
	parsedRequest.Ttl = 0

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, errors.New(ErrKeyUnauthorizedUnableToAttainRequestorID)
	}

	err := toolbox.DecodeRequestBody(request, parsedRequest)
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

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.UserID = userID

	if userID != requestorID {
		return nil, errors.New(ErrKeyForbiddenUnableToAction)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyBadRequest)
	}

	return parsedRequest, nil
}

// MapRequestToRefreshTokenRequest maps incoming RefreshToken request to correct
// struct.
func MapRequestToRefreshTokenRequest(request *http.Request, refreshCookieName string, validator AccessmanagerValidator) (*RefreshTokenRequest, error) {
	parsedRequest := &RefreshTokenRequest{}

	// TODO: Create a NoAuth Middleware where this can be done
	cookie, err := request.Cookie(refreshCookieName)

	if err != nil && err != http.ErrNoCookie {
		return nil, err
	}

	if cookie != nil {
		parsedRequest.RefreshToken = cookie.Value
	}

	if err == http.ErrNoCookie {
		err := toolbox.DecodeRequestBody(request, parsedRequest)
		if err != nil {
			return nil, errors.New(ErrKeyInvalidRefreshToken)
		}
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidRefreshToken)
	}

	return parsedRequest, nil
}

// MapRequestToCreateInitalLoginOrVerificationTokenEmailRequest maps incoming CreateInitalLoginOrVerificationTokenEmail request
// to correct struct
func MapRequestToCreateInitalLoginOrVerificationTokenEmailRequest(request *http.Request, validator AccessmanagerValidator) (*CreateInitalLoginOrVerificationTokenEmailRequest, error) {
	parsedRequest := &CreateInitalLoginOrVerificationTokenEmailRequest{}
	log := logger.AcquireFrom(request.Context())

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, errors.New(ErrKeyInvalidUserEmail)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserEmail)
	}

	log.Info("login-request-submitted.", zap.String("email:", parsedRequest.Email))

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

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	return parsedRequest, nil
}

// MapRequestToValidateEmailVerificationCodeRequest maps incoming ValidateEmailVerificationCode request to correct
// struct.
func MapRequestToValidateEmailVerificationCodeRequest(request *http.Request, validator AccessmanagerValidator) (*ValidateEmailVerificationCodeRequest, error) {
	parsedRequest := &ValidateEmailVerificationCodeRequest{}

	if token, ok := request.URL.Query()[AccessManagerRequestParameterKeyDefaultToken]; ok {
		parsedRequest.Token = token[0]
	} else {
		return nil, errors.New(ErrKeyInvalidVerificationToken)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidVerificationToken)
	}

	return parsedRequest, nil
}

// MapRequestToLoginUserRequest maps incoming LoginUser request to correct
// struct.
func MapRequestToLoginUserRequest(request *http.Request, validator AccessmanagerValidator) (*LoginUserRequest, error) {
	parsedRequest := &LoginUserRequest{}

	if token, ok := request.URL.Query()[AccessManagerRequestParameterKeyDefaultToken]; ok {
		parsedRequest.Token = token[0]
	} else {
		return nil, errors.New(ErrKeyInvalidVerificationToken)
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidVerificationToken)
	}

	return parsedRequest, nil
}

// validateParsedRequest validates based on tags. On failure an error is returned
func validateParsedRequest(request interface{}, validator AccessmanagerValidator) error {
	return validator.Validate(request)
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

// getRequestUrlFromURI pulls request url from URI. If fails, returns an empty string
func getRequestUrlFromURI(request *http.Request) string {

	if requestUrlUriValues, ok := request.URL.Query()["request_url"]; ok {
		return requestUrlUriValues[0]
	}

	return ""
}
