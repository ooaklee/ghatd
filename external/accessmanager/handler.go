package accessmanager

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/reply"
)

// AccessmanagerService manages business logic around accessmanager request
type AccessmanagerService interface {
	DeleteAuth(ctx context.Context, tokenID string) (int64, error)
	TokenAsStringValidator(ctx context.Context, r *TokenAsStringValidatorRequest) (*TokenAsStringValidatorResponse, error)
	CreateUser(ctx context.Context, r *CreateUserRequest) (*CreateUserResponse, error)
	ValidateEmailVerificationCode(ctx context.Context, r *ValidateEmailVerificationCodeRequest) (*ValidateEmailVerificationCodeResponse, error)
	CreateInitalLoginOrVerificationTokenEmail(ctx context.Context, r *CreateInitalLoginOrVerificationTokenEmailRequest) error
	LoginUser(ctx context.Context, r *LoginUserRequest) (*LoginUserResponse, error)
	RefreshToken(ctx context.Context, r *RefreshTokenRequest) (*RefreshTokenResponse, error)
	LogoutUser(ctx context.Context, r *http.Request) error
	CreateUserAPIToken(ctx context.Context, r *CreateUserAPITokenRequest) (*CreateUserAPITokenResponse, error)
	DeleteUserAPIToken(ctx context.Context, r *DeleteUserAPITokenRequest) error
	UpdateUserAPITokenStatus(ctx context.Context, r *UserAPITokenStatusRequest) error
	GetSpecificUserAPITokens(ctx context.Context, r *GetSpecificUserAPITokensRequest) (*GetSpecificUserAPITokensResponse, error)
	GetUserAPITokenThreshold(ctx context.Context, r *GetUserAPITokenThresholdRequest) (*GetUserAPITokenThresholdResponse, error)
	OauthLogin(ctx context.Context, r *OauthLoginRequest) (*OauthLoginResponse, error)
	OauthCallback(ctx context.Context, r *OauthCallbackRequest) (*OauthCallbackResponse, error)
	RemoveRefreshTokenWithCookieValue(ctx context.Context, refreshTokenCookieValue string) (auth.UserModel, string, error)
	LogoutUserOthers(ctx context.Context, r *LogoutUserOthersRequest) error
	UpdateUserEmail(ctx context.Context, r *UpdateUserEmailRequest) (bool, error)
}

// AccessmanagerValidator expected methods of a valid
type AccessmanagerValidator interface {
	Validate(s interface{}) error
}

// Handler manages accessmanager requests
type Handler struct {
	Service                  AccessmanagerService
	Validator                AccessmanagerValidator
	errorMaps                []reply.ErrorManifest
	CookiePrefixAuthToken    string
	CookiePrefixRefreshToken string
	Environment              string
	CookieDomain             string
}

// NewHandlerRequest holds things needed for creating a handler
type NewHandlerRequest struct {
	Service                  AccessmanagerService
	Validator                AccessmanagerValidator
	ErrorMaps                []reply.ErrorManifest
	Environment              string
	CookiePrefixAuthToken    string
	CookiePrefixRefreshToken string
	CookieDomain             string
}

// NewHandler returns accessmanager handler
func NewHandler(r *NewHandlerRequest) *Handler {

	r.ErrorMaps = append(r.ErrorMaps, AccessmanagerErrorMap)

	return &Handler{
		Service:                  r.Service,
		Validator:                r.Validator,
		errorMaps:                r.ErrorMaps,
		CookiePrefixAuthToken:    r.CookiePrefixAuthToken,
		CookiePrefixRefreshToken: r.CookiePrefixRefreshToken,
		Environment:              r.Environment,
		CookieDomain:             r.CookieDomain,
	}
}

// UpdateUserEmail handles updating a user's email address. If the update requires the user to sign out,
// it will remove the user's auth cookies and redirect them to the home page. Otherwise, it will return
// a blank response with a 200 status code.
func (h *Handler) UpdateUserEmail(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateUserEmailRequest(r, h.CookiePrefixAuthToken, h.CookiePrefixRefreshToken, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	signOutRequired, err := h.Service.UpdateUserEmail(r.Context(), request)
	if err != nil && !signOutRequired {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	if err != nil && signOutRequired {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if signOutRequired {
		// complete the cleanup process of removing the cookies
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// LogoutUserOthers handles logging out all other sessions for a user
func (h *Handler) LogoutUserOthers(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToLogoutUserOthersRequest(r, h.Validator, h.CookiePrefixAuthToken, h.CookiePrefixRefreshToken)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.LogoutUserOthers(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// OauthCallback returns a redirect to the respective providers login page
// TODO: Create tests
func (h *Handler) OauthLogin(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToOauthLoginRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.OauthLogin(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	// Shape the cookie
	oauthInitCookie := response.CookieCore
	oauthInitCookie.Domain = h.CookieDomain
	oauthInitCookie.Path = "/"
	oauthInitCookie.Secure = func(env string) bool {
		return env != "local"
	}(h.Environment)
	oauthInitCookie.HttpOnly = true
	oauthInitCookie.SameSite = func(env string) http.SameSite {
		if env != "local" {
			return http.SameSiteStrictMode
		}
		return http.SameSiteLaxMode
	}(h.Environment)

	// Get the provider login Url
	logInUrl := response.ProviderAuthCodeUrl

	// set cookie
	http.SetCookie(w, oauthInitCookie)

	// send back redirect
	http.Redirect(w, r, logInUrl, http.StatusTemporaryRedirect)
}

// OauthCallback returns user access & refresh tokens if the user making the
// request is valid
// TODO: Create tests
func (h *Handler) OauthCallback(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToOauthCallbackRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.OauthCallback(r.Context(), request)
	if err != nil {

		if response != nil && response.ProviderStateCookieKey != "" {
			h.RemoveCookiesWithName(w, response.ProviderStateCookieKey)
		}

		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.RemoveCookiesWithName(w, response.ProviderStateCookieKey)
	h.AddAuthCookies(w, response.AccessToken, response.AccessTokenExpiresAt, response.RefreshToken, response.RefreshTokenExpiresAt)
	toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, response.AccessTokenExpiresAt, response.RefreshTokenExpiresAt)

	if response.RequestUrl != "" {
		// allow API to pass back accessible header
		w.Header().Add("Access-Control-Expose-Headers", common.WebLocationHttpRequestHeader)
		w.Header().Add(common.WebLocationHttpRequestHeader, response.RequestUrl)
	}
	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPTokenResponse(w, http.StatusOK, fmt.Sprint(response.AccessTokenExpiresAt), fmt.Sprint(response.RefreshTokenExpiresAt))
}

// GetUserAPITokenThreshold returns user's API tokens
// User requesting must be active & be the same person as target
// TODO: Create tests
func (h *Handler) GetUserAPITokenThreshold(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToGetUserAPITokenThresholdRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	userTokenThreshold, err := h.Service.GetUserAPITokenThreshold(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, userTokenThreshold)
}

// GetSpecificUserAPITokens returns user's API tokens
// User requesting must be active & be the same person as target
// TODO: Create tests
func (h *Handler) GetSpecificUserAPITokens(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToGetSpecificUserAPITokensRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetSpecificUserAPITokens(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.UserAPITokens, reply.WithMeta(response.GetMetaData()))
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.UserAPITokens)
}

// RevokeUserAPIToken returns whether a request to revoke an API token was successful.
// User requesting must be active and must be the same person as target.
// TODO: Create tests
func (h *Handler) RevokeUserAPIToken(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToRevokeUserAPITokenRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.UpdateUserAPITokenStatus(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// ActivateUserAPIToken returns whether a request to activate an API token was successful.
// User requesting must be active and must be the same person as target.
// TODO: Create tests
func (h *Handler) ActivateUserAPIToken(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToActivateUserAPITokenRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.UpdateUserAPITokenStatus(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// DeleteUserAPIToken returns whether a request to delete an API token was successful.
// User requesting must be active and must be the same person as target.
// TODO: Create tests
func (h *Handler) DeleteUserAPIToken(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToDeleteUserAPITokenRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.DeleteUserAPIToken(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// CreateUserAPIToken returns whether a request to create an API token was successful.
// User requesting must be active & be the same person as target
// TODO: Create tests
func (h *Handler) CreateUserAPIToken(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToCreateUserAPITokenRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateUserAPIToken(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.UserAPIToken)

}

// LogoutUser returns reponse from user logout request.
// TODO: Create tests
func (h *Handler) LogoutUser(w http.ResponseWriter, r *http.Request) {

	log := logger.AcquireFrom(r.Context())

	// Check if there is a refresh token cookie
	// although it should be there, there is no guarantee that it will be
	// there
	refreshTokenCookie, _ := r.Cookie(h.CookiePrefixRefreshToken)
	if refreshTokenCookie != nil {

		log.Info("refresh-token-cookie-found-while-logging-out-will-be-removed")
		_, _, _ = h.Service.RemoveRefreshTokenWithCookieValue(r.Context(), refreshTokenCookie.Value)
	}

	accessTokenCookie, err := r.Cookie(h.CookiePrefixAuthToken)

	if err != nil && err != http.ErrNoCookie {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		if ok := redirectToHomeIfPlatformHeaderDetected(w, r); ok {
			return
		}
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if accessTokenCookie != nil {
		r.Header["Authorization"] = []string{"Bearer " + accessTokenCookie.Value}
	}

	if err == http.ErrNoCookie {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		if ok := redirectToHomeIfPlatformHeaderDetected(w, r); ok {
			return
		}
		h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
		return
	}

	h.RemoveAuthCookies(w)
	h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
	h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

	err = h.Service.LogoutUser(r.Context(), r)
	if err != nil {
		if ok := redirectToHomeIfPlatformHeaderDetected(w, r); ok {
			return
		}

		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if ok := redirectToHomeIfPlatformHeaderDetected(w, r); ok {
		return
	}
	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// RefreshToken returns reponse from user's request to refresh their token
// TODO: Create tests
func (h *Handler) RefreshToken(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToRefreshTokenRequest(r, h.CookiePrefixRefreshToken, h.CookiePrefixAuthToken, h.Validator)
	if err != nil {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RefreshToken(r.Context(), request)
	if err != nil {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.AddAuthCookies(w, response.AccessToken, response.AccessTokenExpiresAt, response.RefreshToken, response.RefreshTokenExpiresAt)
	toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, response.AccessTokenExpiresAt, response.RefreshTokenExpiresAt)

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPTokenResponse(w, http.StatusOK, fmt.Sprint(response.AccessTokenExpiresAt), fmt.Sprint(response.RefreshTokenExpiresAt))
}

// LoginUser returns reponse from user login.
// Verifies a valid InitalLoginToken was provided and if so provides user's
// access and refresh token
// TODO: Create tests
func (h *Handler) LoginUser(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToLoginUserRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.LoginUser(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.AddAuthCookies(w, response.AccessToken, response.AccessTokenExpiresAt, response.RefreshToken, response.RefreshTokenExpiresAt)
	toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, response.AccessTokenExpiresAt, response.RefreshTokenExpiresAt)

	// get next step query param from request if available
	if nextStepQueryParam := r.URL.Query()[common.WebNextStepsHttpQueryParam]; len(nextStepQueryParam) > 0 && nextStepQueryParam[0] != "" {
		http.Redirect(w, r, nextStepQueryParam[0], http.StatusTemporaryRedirect)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPTokenResponse(w, http.StatusOK, fmt.Sprint(response.AccessTokenExpiresAt), fmt.Sprint(response.RefreshTokenExpiresAt))
}

// CreateInitalLoginOrVerificationToken dependent on the user's account status,
// this handles sending users verification emails where they can `ACTIVATE` their
// account if their account is `PROVISIONED`, or creates a temporary token which will be sent to user's email
// to verify their identity and allow them to sign in to the platform.
//
// Should always return 202 unless mapping request fails. (makes bad actors finding out users on platform harder)
// TODO: Create tests
func (h *Handler) CreateInitalLoginOrVerificationTokenEmail(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToCreateInitalLoginOrVerificationTokenEmailRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.CreateInitalLoginOrVerificationTokenEmail(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// CreateUser returns reponse from user creation
// TODO: Create tests
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToCreateUserRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateUser(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.User)
}

// ValidateEmailVerificationCode handles requests to verify a user's email by validating a correct token was provided.
// If the token is validated, the user's account status is updated to `ACTIVE`  & the user is
// returned a pair of access and refresh tokens
// TODO: Create tests
func (h *Handler) ValidateEmailVerificationCode(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToValidateEmailVerificationCodeRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	revisions, err := h.Service.ValidateEmailVerificationCode(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.AddAuthCookies(w, revisions.AccessToken, revisions.AccessTokenExpiresAt, revisions.RefreshToken, revisions.RefreshTokenExpiresAt)
	toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, revisions.AccessTokenExpiresAt, revisions.RefreshTokenExpiresAt)

	// get next step query param from request if available
	if nextStepQueryParam := r.URL.Query()[common.WebNextStepsHttpQueryParam]; nextStepQueryParam[0] != "" {
		http.Redirect(w, r, nextStepQueryParam[0], http.StatusTemporaryRedirect)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPTokenResponse(w, http.StatusOK, fmt.Sprint(revisions.AccessTokenExpiresAt), fmt.Sprint(revisions.RefreshTokenExpiresAt))
}

// GetBaseResponseHandler returns response handler configured with auth error map
func (h *Handler) GetBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.errorMaps)
}

// RemoveAuthCookies is handling removing the cookies from the client
// cookie store regardless of what happens on the platform
func (h *Handler) RemoveAuthCookies(w http.ResponseWriter) {

	toolbox.RemoveAuthCookies(w, h.Environment, h.CookieDomain, h.CookiePrefixAuthToken, h.CookiePrefixRefreshToken)
}

// AddAuthCookies is handling adding the cookies to the response
func (h *Handler) AddAuthCookies(w http.ResponseWriter, accessToken string, accessTokenExpiresAt int64, refressToken string, refressTokenExpiresAt int64) {

	toolbox.AddAuthCookies(w, h.Environment, h.CookieDomain, h.CookiePrefixAuthToken, accessToken, accessTokenExpiresAt, h.CookiePrefixRefreshToken, refressToken, refressTokenExpiresAt)
}

// RemoveCookiesWithName is handling removing the cookies from the client
// cookie store regardless of what happens on the platform
func (h *Handler) RemoveCookiesWithName(w http.ResponseWriter, cookieName string) {

	toolbox.RemoveCookiesWithName(w, h.Environment, cookieName, h.CookieDomain)
}

// redirectToHomeIfPlatformHeaderDetected checks the request headers for the presence of the web platform or HTMX headers.
// If either header is present, the function redirects the user to the home page (/) and returns true.
// Otherwise, it returns false.
func redirectToHomeIfPlatformHeaderDetected(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get(common.WebPlatformHttpRequestHeader) != "" || r.Header.Get(common.HtmxHttpRequestHeader) != "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return true
	}
	return false
}
