package accessmanager

import (
	"github.com/ooaklee/ghatd/external/oauth"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
	"go.uber.org/zap"
)

// MapRequestToUpdateUserEmailRequest maps incoming UpdateUserEmail request to correct struct.
func MapRequestToUpdateUserEmailRequest(request *http.Request, cookiePrefixAuthToken, cookiePrefixRefreshToken string, validator AccessmanagerValidator) (*UpdateUserEmailRequest, error) {
	var (
		logger        *zap.Logger             = logger.AcquirePackageFrom(request.Context(), "external/accessmanager")
		parsedRequest *UpdateUserEmailRequest = &UpdateUserEmailRequest{}
		err           error
	)

	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		logger.Error("unable-decode-request-body-for-updating-user-email")
		return nil, ErrInvalidUserEmail
	}

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())
	if parsedRequest.UserId == "" {
		logger.Error("unable-get-requestor-user-id")
		return nil, ErrUnauthorizedUnableToAttainRequestorID
	}

	parsedRequest.TargetUserId, err = getUserIDFromURI(request)
	if err != nil {
		logger.Error("unable-get-target-user-id")
		return nil, err
	}

	// get the access token from the cookie
	// check to see if request is coming with cookies
	cookie, aTokenErr := request.Cookie(cookiePrefixAuthToken)
	if aTokenErr != nil {
		logger.Error("unable-get-access-token-from-cookie", zap.String("user-id", parsedRequest.UserId), zap.String("target-user-id", parsedRequest.TargetUserId))
		return nil, aTokenErr
	}

	parsedRequest.AuthToken = cookie.Value

	refreshTokenCookie, rAuthErr := request.Cookie(cookiePrefixRefreshToken)
	if rAuthErr != nil {
		logger.Error("unable-get-access-token-from-cookie", zap.String("user-id", parsedRequest.UserId), zap.String("target-user-id", parsedRequest.TargetUserId))
		return nil, rAuthErr
	}

	parsedRequest.RefreshToken = refreshTokenCookie.Value

	// Add request
	parsedRequest.Request = request

	err = validator.Validate(parsedRequest)
	if err != nil {
		logger.Error("unable-validate-request-for-updating-user-email")
		return nil, ErrBadRequest
	}

	return parsedRequest, nil
}

// MapRequestToLogoutUserOthersRequest maps incoming LogOutUserOthers request to correct struct.
func MapRequestToLogoutUserOthersRequest(request *http.Request, validator AccessmanagerValidator, authCookiePrefix, refreshCookiePrefix string) (*LogoutUserOthersRequest, error) {
	var (
		logger *zap.Logger = logger.AcquirePackageFrom(request.Context(), "external/accessmanager")

		parsedRequest *LogoutUserOthersRequest = &LogoutUserOthersRequest{}
	)

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(request.Context())

	authTokenCookie, err := request.Cookie(authCookiePrefix)
	if err != nil {
		logger.Error("unable-to-get-auth-token-cookie", zap.String("user-id", parsedRequest.UserId))
		return nil, ErrInvalidAuthToken
	}

	refreshTokenCookie, err := request.Cookie(refreshCookiePrefix)
	if err != nil {
		logger.Error("unable-to-get-refresh-token-cookie", zap.String("user-id", parsedRequest.UserId))
		return nil, ErrInvalidRefreshToken
	}

	parsedRequest.AuthToken = authTokenCookie.Value
	parsedRequest.RefreshToken = refreshTokenCookie.Value

	if err := toolbox.ValidateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrInvalidLogOutUserOthersRequest
	}

	if parsedRequest.UserId == "" {
		logger.Error("unable-get-user-id")
		return nil, ErrInvalidUserID
	}

	return parsedRequest, nil

}

// MapRequestToOauthCallbackRequest maps incoming OauthCallback request to correct struct
func MapRequestToOauthCallbackRequest(request *http.Request, validator AccessmanagerValidator) (*OauthCallbackRequest, error) {
	provider, err := getProviderNameFromURI(request)
	if err != nil {
		return nil, err
	}
	values := request.URL.Query()
	if request.Method == http.MethodPost {
		if request.URL.RawQuery != "" {
			return nil, ErrBadRequest
		}
		request.Body = http.MaxBytesReader(nil, request.Body, 16384)
		if err = request.ParseForm(); err != nil {
			return nil, ErrBadRequest
		}
		values = request.PostForm
	}
	if len(values) == 0 {
		return nil, ErrBadRequest
	}
	return &OauthCallbackRequest{Provider: provider, UrlUri: values, RequestCookies: request.Cookies(), Method: request.Method}, nil
}

// MapRequestToOauthLoginRequest decodes parameters exactly once and validates a same-origin return path.
func MapRequestToOauthLoginRequest(request *http.Request, validator AccessmanagerValidator) (*OauthLoginRequest, error) {
	provider, err := getProviderNameFromURI(request)
	if err != nil {
		return nil, err
	}
	query := request.URL.Query()
	if len(query["request_url"]) > 1 || len(query["browser"]) > 1 {
		return nil, ErrBadRequest
	}
	path, err := oauth.ValidateSecureReturnPath(query.Get("request_url"))
	if err != nil {
		return nil, err
	}
	browser := query.Get("browser")
	if browser != "" && browser != "true" && browser != "false" {
		return nil, ErrBadRequest
	}
	return &OauthLoginRequest{Provider: provider, RequestUrl: path, Browser: browser == "true"}, nil
}

// MapRequestToGetUserAPITokenThresholdRequest maps incoming GetUserAPITokenThreshold request to correct
// struct.
// TODO: Refactor
func MapRequestToGetUserAPITokenThresholdRequest(request *http.Request, validator AccessmanagerValidator) (*GetUserAPITokenThresholdRequest, error) {
	parsedRequest := &GetUserAPITokenThresholdRequest{}

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, ErrUnauthorizedUnableToAttainRequestorID
	}

	userId, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	if userId != requestorID {
		return nil, ErrForbiddenUnableToAction
	}

	parsedRequest.UserId = userId

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrBadRequest
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
		return nil, ErrUnauthorizedUnableToAttainRequestorID
	}

	// Add used Id from uri
	parsedRequest.UserID, err = toolbox.GetVariableValueFromUri(request, UserURIVariableID)
	if err != nil {
		return nil, err
	}

	if parsedRequest.UserID != requestorID {
		return nil, ErrForbiddenUnableToAction
	}

	// get query params from request
	query := request.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		return nil, ErrInvalidResultQueryParam
	}

	parsedRequest.GetAPITokensForRequest = &baseRequest

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrBadRequest
	}

	return &parsedRequest, nil
}

// MapRequestToRevokeUserAPITokenRequest binds the URI owner to the authenticated
// requester and preserves that owner for the atomic credential mutation.
func MapRequestToRevokeUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*UserAPITokenStatusRequest, error) {
	var (
		parsedRequest = &UserAPITokenStatusRequest{}
		err           error
	)

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, ErrUnauthorizedUnableToAttainRequestorID
	}

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	if userID != requestorID {
		return nil, ErrForbiddenUnableToAction
	}

	parsedRequest.APITokenID, err = getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.Status = AccessManagerUserTokenStatusKeyRevoked
	parsedRequest.UserID = userID

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrBadRequest
	}

	return parsedRequest, nil
}

// MapRequestToActivateUserAPITokenRequest binds the URI owner to the authenticated
// requester; a credential ID alone never authorizes activation.
func MapRequestToActivateUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*UserAPITokenStatusRequest, error) {

	var (
		parsedRequest = &UserAPITokenStatusRequest{}
		err           error
	)

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, ErrUnauthorizedUnableToAttainRequestorID
	}

	userID, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	if userID != requestorID {
		return nil, ErrForbiddenUnableToAction
	}

	parsedRequest.APITokenID, err = getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}

	parsedRequest.Status = AccessManagerUserTokenStatusKeyActive
	parsedRequest.UserID = userID

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrBadRequest
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
		return nil, ErrUnauthorizedUnableToAttainRequestorID
	}

	parsedRequest.UserID, err = getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	if parsedRequest.UserID != requestorID {
		return nil, ErrForbiddenUnableToAction
	}

	parsedRequest.APITokenID, err = getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrBadRequest
	}

	return parsedRequest, nil
}

// MapRequestToCreateUserAPITokenRequest maps incoming CreateUserAPIToken request to correct
// struct.
func MapRequestToCreateUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*CreateUserAPITokenRequest, error) {

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(request.Context(), "external/accessmanager")

		parsedRequest *CreateUserAPITokenRequest = &CreateUserAPITokenRequest{}
		err           error
	)

	// Default to permanent
	parsedRequest.Ttl = 0

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	if requestorID == "" {
		return nil, ErrUnauthorizedUnableToAttainRequestorID
	}

	err = toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		logger.Warn("unable-to-decode-create-user-api-token-request")
		return nil, ErrInvalidCreateUserAPITokenBody
	}

	parsedRequest.UserID, err = getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}

	if parsedRequest.UserID != requestorID {
		return nil, ErrForbiddenUnableToAction
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrBadRequest
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
			return nil, ErrInvalidRefreshToken
		}
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrInvalidRefreshToken
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
		logger *zap.Logger = logger.AcquirePackageFrom(request.Context(), "external/accessmanager")

		parsedRequest *CreateInitalLoginOrVerificationTokenEmailRequest = &CreateInitalLoginOrVerificationTokenEmailRequest{}
	)

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, ErrInvalidUserEmail
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrInvalidUserEmail
	}

	logger.Debug("login-request-submitted.", emailLogFields("email", parsedRequest.Email)...)

	return parsedRequest, nil

}

// MapRequestToCreateUserRequest maps incoming CreateUser request to correct
// struct.
func MapRequestToCreateUserRequest(request *http.Request, validator AccessmanagerValidator) (*CreateUserRequest, error) {
	parsedRequest := &CreateUserRequest{}

	err := toolbox.DecodeRequestBody(request, parsedRequest)
	if err != nil {
		return nil, ErrInvalidUserBody
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrInvalidUserBody
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
		return nil, ErrInvalidVerificationToken
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrInvalidVerificationToken
	}

	if parsedRequest.Token == "" && parsedRequest.Code == "" {
		return nil, ErrMissingVerificationCredentials
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
		return nil, ErrInvalidVerificationToken
	}

	err = validator.Validate(parsedRequest)
	if err != nil {
		return nil, ErrInvalidVerificationToken
	}

	if parsedRequest.Token == "" && parsedRequest.Code == "" {
		return nil, ErrMissingVerificationCredentials
	}

	return &parsedRequest, nil
}

// getUserIDFromURI pulls userID from URI. If fails, returns error
func getUserIDFromURI(request *http.Request) (string, error) {
	var userID string

	if userID = mux.Vars(request)[UserURIVariableID]; userID == "" {
		return "", ErrInvalidUserID
	}

	return userID, nil
}

// getTokenIDFromURI pulls token ID from URI. If fails, returns error
func getTokenIDFromURI(request *http.Request) (string, error) {
	var tokenID string

	if tokenID = mux.Vars(request)[APITokenURIVariableID]; tokenID == "" {
		return "", ErrInvalidAPITokenID
	}

	return tokenID, nil
}

// getProviderNameFromURI pulls the provider name from Uri. If fails, returns error
func getProviderNameFromURI(request *http.Request) (string, error) {
	if provider := mux.Vars(request)["provider"]; provider == "google" || provider == "apple" {
		return provider, nil
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	for i, part := range parts {
		if part == "oauth" && i+1 < len(parts) {
			provider := parts[i+1]
			if provider == "google" || provider == "apple" {
				return provider, nil
			}
		}
	}
	return "", ErrBadRequest
}
