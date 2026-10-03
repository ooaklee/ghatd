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

// MapRequestToUpdateUserEmailRequest binds authenticated context and route target
// independently of its email-only payload. Cookie-name arguments are retained
// for source compatibility, but credential cleanup no longer uses caller cookies.
func MapRequestToUpdateUserEmailRequest(request *http.Request, _, _ string, validator AccessmanagerValidator) (*UpdateUserEmailRequest, error) {
	if request == nil || request.URL == nil || nilAccessDependency(request.Body) {
		return nil, ErrBadRequest
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	actor := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(request.Context())
	if strings.TrimSpace(actor) == "" {
		return nil, ErrUnauthorizedUnableToAttainRequestorID
	}
	target, err := getUserIDFromURI(request)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Email string `json:"email"`
	}
	if err := toolbox.DecodeRequestBody(request, &payload); err != nil {
		return nil, ErrInvalidUserEmail
	}
	if nilAccessDependency(validator) {
		return nil, ErrBadRequest
	}
	result := &UpdateUserEmailRequest{ActorID: actor, TargetUserID: target, Email: payload.Email}
	if err := validator.Validate(result); err != nil {
		return nil, ErrBadRequest
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	return result, nil
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

// tokenManagementRequestIdentity binds the session caller separately from the
// URI owner. It does not decode payloads or treat an API token as a session.
func tokenManagementRequestIdentity(request *http.Request) (string, string, error) {
	if request == nil || request.URL == nil {
		return "", "", ErrBadRequest
	}
	if err := request.Context().Err(); err != nil {
		return "", "", err
	}
	actor := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(request.Context())
	if err := tokenManagementSession(request.Context(), actor, actor); err != nil {
		return "", "", err
	}
	owner, err := getUserIDFromURI(request)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(owner) == "" {
		return "", "", ErrInvalidUserID
	}
	if owner != actor {
		return "", "", ErrForbiddenUnableToAction
	}
	return actor, owner, nil
}

// validateTokenManagementRequest checks the final server-bound command without
// exposing validator diagnostics. Canceled requests never dispatch downstream.
func validateTokenManagementRequest(request *http.Request, validator AccessmanagerValidator, value any) error {
	if nilAccessDependency(validator) {
		return ErrBadRequest
	}
	if err := validator.Validate(value); err != nil {
		return ErrBadRequest
	}
	return request.Context().Err()
}

// MapRequestToGetUserAPITokenThresholdRequest binds self-service session identity.
func MapRequestToGetUserAPITokenThresholdRequest(request *http.Request, validator AccessmanagerValidator) (*GetUserAPITokenThresholdRequest, error) {
	actor, owner, err := tokenManagementRequestIdentity(request)
	if err != nil {
		return nil, err
	}
	result := &GetUserAPITokenThresholdRequest{ActorID: actor, UserID: owner}
	if err := validateTokenManagementRequest(request, validator, result); err != nil {
		return nil, err
	}
	return result, nil
}

// MapRequestToGetSpecificUserAPITokensRequest decodes only display filters.
// Embedded lower-domain identity fields never receive transport input.
func MapRequestToGetSpecificUserAPITokensRequest(request *http.Request, validator AccessmanagerValidator) (*GetSpecificUserAPITokensRequest, error) {
	actor, owner, err := tokenManagementRequestIdentity(request)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Order         string `query:"order"`
		PerPage       int    `query:"per_page"`
		Page          int    `query:"page"`
		Description   string `query:"description"`
		Status        string `query:"status"`
		Meta          bool   `query:"meta"`
		OnlyEphemeral bool   `query:"only_ephemeral"`
		OnlyPermanent bool   `query:"only_permanent"`
	}
	if err := querydecoder.New(request.URL.Query()).Decode(&payload); err != nil {
		return nil, ErrInvalidResultQueryParam
	}
	result := &GetSpecificUserAPITokensRequest{ActorID: actor, UserID: owner, GetAPITokensForRequest: &apitoken.GetAPITokensForRequest{Order: payload.Order, PerPage: payload.PerPage, Page: payload.Page, Description: payload.Description, Status: payload.Status, Meta: payload.Meta, OnlyEphemeral: payload.OnlyEphemeral, OnlyPermanent: payload.OnlyPermanent}}
	if err := validateTokenManagementRequest(request, validator, result); err != nil {
		return nil, err
	}
	return result, nil
}

// mapTokenStatusRequest binds both URI selectors and the route-owned status.
func mapTokenStatusRequest(request *http.Request, validator AccessmanagerValidator, status string) (*UserAPITokenStatusRequest, error) {
	actor, owner, err := tokenManagementRequestIdentity(request)
	if err != nil {
		return nil, err
	}
	tokenID, err := getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tokenID) == "" {
		return nil, ErrInvalidAPITokenID
	}
	result := &UserAPITokenStatusRequest{ActorID: actor, UserID: owner, APITokenID: tokenID, Status: status}
	if err := validateTokenManagementRequest(request, validator, result); err != nil {
		return nil, err
	}
	return result, nil
}

// MapRequestToRevokeUserAPITokenRequest never accepts a body-selected status.
func MapRequestToRevokeUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*UserAPITokenStatusRequest, error) {
	return mapTokenStatusRequest(request, validator, AccessManagerUserTokenStatusKeyRevoked)
}

// MapRequestToActivateUserAPITokenRequest requires a session for the URI owner.
func MapRequestToActivateUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*UserAPITokenStatusRequest, error) {
	return mapTokenStatusRequest(request, validator, AccessManagerUserTokenStatusKeyActive)
}

// MapRequestToDeleteUserAPITokenRequest binds an exact token and self-service owner.
func MapRequestToDeleteUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*DeleteUserAPITokenRequest, error) {
	actor, owner, err := tokenManagementRequestIdentity(request)
	if err != nil {
		return nil, err
	}
	tokenID, err := getTokenIDFromURI(request)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tokenID) == "" {
		return nil, ErrInvalidAPITokenID
	}
	result := &DeleteUserAPITokenRequest{ActorID: actor, UserID: owner, APITokenID: tokenID}
	if err := validateTokenManagementRequest(request, validator, result); err != nil {
		return nil, err
	}
	return result, nil
}

// MapRequestToCreateUserAPITokenRequest accepts only lifetime and description;
// omitted lifetime remains permanent, subject to the live issuance policy.
func MapRequestToCreateUserAPITokenRequest(request *http.Request, validator AccessmanagerValidator) (*CreateUserAPITokenRequest, error) {
	actor, owner, err := tokenManagementRequestIdentity(request)
	if err != nil {
		return nil, err
	}
	if nilAccessDependency(request.Body) {
		return nil, ErrInvalidCreateUserAPITokenBody
	}
	var payload struct {
		Ttl         int64  `json:"ttl"`
		Description string `json:"description,omitempty"`
	}
	if err := toolbox.DecodeRequestBody(request, &payload); err != nil {
		return nil, ErrInvalidCreateUserAPITokenBody
	}
	result := &CreateUserAPITokenRequest{ActorID: actor, UserID: owner, Ttl: payload.Ttl, Description: payload.Description}
	if err := validateTokenManagementRequest(request, validator, result); err != nil {
		return nil, err
	}
	return result, nil
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
