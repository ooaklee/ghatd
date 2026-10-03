package accessmanager

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
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
	// OAuthOrigin is the configured browser origin used for linking CSRF checks.
	OAuthOrigin string
	mobileOAuth *MobileOAuthConfig
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
	// OAuthOrigin is the configured browser origin used for linking CSRF checks.
	OAuthOrigin string
}

// NewHandler returns accessmanager handler
func NewHandler(r *NewHandlerRequest) *Handler {

	return &Handler{
		Service:                  r.Service,
		Validator:                r.Validator,
		errorMaps:                r.ErrorMaps,
		CookiePrefixAuthToken:    r.CookiePrefixAuthToken,
		CookiePrefixRefreshToken: r.CookiePrefixRefreshToken,
		Environment:              r.Environment,
		CookieDomain:             r.CookieDomain,
		OAuthOrigin:              r.OAuthOrigin,
	}
}

// UpdateUserEmail handles updating a user's email address. If the update requires the user to sign out,
// it will remove the user's auth cookies and redirect them to the home page. Otherwise, it will return
// a blank response with a 200 status code.
func (h *Handler) UpdateUserEmail(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-update-user-email")
	request, err := MapRequestToUpdateUserEmailRequest(r, h.CookiePrefixAuthToken, h.CookiePrefixRefreshToken, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	signOutRequired, err := h.Service.UpdateUserEmail(r.Context(), request)
	if err != nil && !signOutRequired {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if err != nil && signOutRequired {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if signOutRequired {
		// complete the cleanup process of removing the cookies
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

	}

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// LogoutUserOthers handles logging out all other sessions for a user
func (h *Handler) LogoutUserOthers(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-logout-user-others")

	request, err := MapRequestToLogoutUserOthersRequest(r, h.Validator, h.CookiePrefixAuthToken, h.CookiePrefixRefreshToken)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.LogoutUserOthers(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// GetUserAPITokenThreshold returns user's API tokens
// User requesting must be active & be the same person as target
// TODO: Create tests
func (h *Handler) GetUserAPITokenThreshold(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-get-user-api-token-threshold")

	request, err := MapRequestToGetUserAPITokenThresholdRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	userTokenThreshold, err := h.Service.GetUserAPITokenThreshold(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, userTokenThreshold)
}

// GetSpecificUserAPITokens returns user's API tokens
// User requesting must be active & be the same person as target
// TODO: Create tests
func (h *Handler) GetSpecificUserAPITokens(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-get-specific-user-api-tokens")

	request, err := MapRequestToGetSpecificUserAPITokensRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetSpecificUserAPITokens(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.UserAPITokens, reply.WithMeta(response.GetMetaData()))
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.UserAPITokens)
}

// RevokeUserAPIToken returns whether a request to revoke an API token was successful.
// User requesting must be active and must be the same person as target.
// TODO: Create tests
func (h *Handler) RevokeUserAPIToken(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-revoke-user-api-token")
	request, err := MapRequestToRevokeUserAPITokenRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.UpdateUserAPITokenStatus(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// ActivateUserAPIToken returns whether a request to activate an API token was successful.
// User requesting must be active and must be the same person as target.
// TODO: Create tests
func (h *Handler) ActivateUserAPIToken(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-activate-user-api-token")

	request, err := MapRequestToActivateUserAPITokenRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.UpdateUserAPITokenStatus(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// DeleteUserAPIToken returns whether a request to delete an API token was successful.
// User requesting must be active and must be the same person as target.
// TODO: Create tests
func (h *Handler) DeleteUserAPIToken(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-delete-user-api-token")

	request, err := MapRequestToDeleteUserAPITokenRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.DeleteUserAPIToken(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// CreateUserAPIToken returns whether a request to create an API token was successful.
// User requesting must be active & be the same person as target
// TODO: Create tests
func (h *Handler) CreateUserAPIToken(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-create-user-api-token")

	request, err := MapRequestToCreateUserAPITokenRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateUserAPIToken(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.UserAPIToken)

}

// LogoutUser returns reponse from user logout request.
// TODO: Create tests
func (h *Handler) LogoutUser(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-logout-user")

	// Check if there is a refresh token cookie
	// although it should be there, there is no guarantee that it will be
	// there
	refreshTokenCookie, _ := r.Cookie(h.CookiePrefixRefreshToken)
	if refreshTokenCookie != nil {

		logger.Info("refresh-token-cookie-found-while-logging-out-will-be-removed")
		_, _, err := h.Service.RemoveRefreshTokenWithCookieValue(r.Context(), refreshTokenCookie.Value)
		if err != nil {
			logger.Warn("failed-to-remove-refresh-token-from-store-during-logout", zap.Error(err))
		}
	}

	accessTokenCookie, err := r.Cookie(h.CookiePrefixAuthToken)

	if err != nil && err != http.ErrNoCookie {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		if ok := redirectToHomeIfPlatformHeaderDetected(w, r); ok {
			return
		}
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
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
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	if ok := redirectToHomeIfPlatformHeaderDetected(w, r); ok {
		return
	}
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// RefreshToken rotates the selected cookie pair and publishes non-cacheable
// expiry metadata. Known credential rejections clear authentication cookies;
// account denials and operational failures preserve them for reconciliation.
func (h *Handler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-refresh-token")
	w.Header().Set("Cache-Control", "no-store")
	if err := r.Context().Err(); err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}

	request, err := MapRequestToRefreshTokenRequest(r, h.CookiePrefixRefreshToken, h.CookiePrefixAuthToken, h.Validator)
	if err != nil {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RefreshToken(r.Context(), request)
	if err != nil {
		if contextErr := r.Context().Err(); contextErr != nil {
			err = contextErr
		}
		kind := ClassifySessionError(err)
		if kind == SessionErrorInvalidCredential || kind == SessionErrorRefreshable {
			h.RemoveAuthCookies(w)
			h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
			h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)
		}

		logger.Warn("refresh-request-rejected")
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if err := r.Context().Err(); err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if response == nil || response.AccessToken == "" || response.RefreshToken == "" {
		h.NewHTTPErrorResponse(w, ErrSessionVerificationUnavailable)
		return
	}

	h.AddAuthCookies(w, response.AccessToken, response.AccessTokenExpiresAt, response.RefreshToken, response.RefreshTokenExpiresAt)
	toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, response.AccessTokenExpiresAt, response.RefreshTokenExpiresAt)

	h.GetBaseResponseHandler().NewHTTPTokenResponse(w, http.StatusOK, fmt.Sprint(response.AccessTokenExpiresAt), fmt.Sprint(response.RefreshTokenExpiresAt))
}

// LoginUser exchanges an email proof for session cookies. Error details come
// only from manifests; proof values and private adapter diagnostics are not logged.
func (h *Handler) LoginUser(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-login-user")

	request, err := MapRequestToLoginUserRequest(r, h.Validator)
	if err != nil {
		logger.Warn("login-request-rejected")
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.LoginUser(r.Context(), request)
	if err != nil {
		logger.Warn("login-proof-exchange-failed")
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if response == nil || response.AccessToken == "" || response.RefreshToken == "" {
		h.NewHTTPErrorResponse(w, ErrSessionVerificationUnavailable)
		return
	}

	h.AddAuthCookies(w, response.AccessToken, response.AccessTokenExpiresAt, response.RefreshToken, response.RefreshTokenExpiresAt)
	toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, response.AccessTokenExpiresAt, response.RefreshTokenExpiresAt)

	// get next step query param from request if available
	if nextStepQueryParam := r.URL.Query()[common.WebNextStepsHttpQueryParam]; len(nextStepQueryParam) > 0 && nextStepQueryParam[0] != "" {
		http.Redirect(w, r, nextStepQueryParam[0], http.StatusTemporaryRedirect)
		return
	}

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
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-create-initial-login-or-verification-token-email")

	request, err := MapRequestToCreateInitalLoginOrVerificationTokenEmailRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.CreateInitalLoginOrVerificationTokenEmail(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-accepted-after-error", zap.Error(err))
		h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
		return
	}

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// CreateUser returns reponse from user creation
// TODO: Create tests
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-create-user")

	request, err := MapRequestToCreateUserRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateUser(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.User)
}

// ValidateEmailVerificationCode delegates one-use proof admission and account
// activation to the service, publishing cookies only for a complete session.
func (h *Handler) ValidateEmailVerificationCode(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-validate-email-verification-code")

	request, err := MapRequestToValidateEmailVerificationCodeRequest(r, h.Validator)
	if err != nil {
		logger.Warn("email-verification-request-rejected")
		h.NewHTTPErrorResponse(w, err)
		return
	}

	revisions, err := h.Service.ValidateEmailVerificationCode(r.Context(), request)
	if err != nil {
		logger.Warn("email-verification-proof-exchange-failed")
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if revisions == nil || revisions.AccessToken == "" || revisions.RefreshToken == "" {
		h.NewHTTPErrorResponse(w, ErrSessionVerificationUnavailable)
		return
	}

	h.AddAuthCookies(w, revisions.AccessToken, revisions.AccessTokenExpiresAt, revisions.RefreshToken, revisions.RefreshTokenExpiresAt)
	toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, revisions.AccessTokenExpiresAt, revisions.RefreshTokenExpiresAt)

	// get next step query param from request if available
	if nextStepQueryParam := r.URL.Query()[common.WebNextStepsHttpQueryParam]; len(nextStepQueryParam) > 0 && nextStepQueryParam[0] != "" {
		http.Redirect(w, r, nextStepQueryParam[0], http.StatusTemporaryRedirect)
		return
	}

	h.GetBaseResponseHandler().NewHTTPTokenResponse(w, http.StatusOK, fmt.Sprint(revisions.AccessTokenExpiresAt), fmt.Sprint(revisions.RefreshTokenExpiresAt))
}

// GetBaseResponseHandler composes manager and dependency maps, then host overrides.
func (h *Handler) GetBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(
		h.responseManifests(),
	)
}

// responseManifests shares canonical domain keys and host overrides across
// direct replies and wrapped-error resolution. Never mutate host manifests.
func (h *Handler) responseManifests() []reply.ErrorManifest {
	return errormanifest.NewComposer().Add(AccessmanagerErrorMap).Add(DependencyErrorMaps()...).AddOverrides(h.errorMaps...).Build()
}

// NewHTTPErrorResponse resolves wrapped domain errors before handing formatting
// to reply. Unknown or ambiguous causes retain the generic fallback; public
// details come only from manifests, never from wrapped diagnostic strings.
func (h *Handler) NewHTTPErrorResponse(w http.ResponseWriter, err error, attributes ...reply.ResponseAttributes) error {
	manifests := h.responseManifests()
	return reply.NewReplier(manifests).NewHTTPErrorResponse(w, errormanifest.CanonicalError(err, manifests), attributes...)
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

// redirectToHomeIfPlatformHeaderDetected checks the request headers for browser-owned
// logout flows that expect a navigation redirect rather than an API response.
// Otherwise, it returns false.
func redirectToHomeIfPlatformHeaderDetected(w http.ResponseWriter, r *http.Request) bool {
	if shouldRedirectToHomeForRequest(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return true
	}
	return false
}

// shouldRedirectToHomeForRequest reports whether the request expects a browser navigation redirect.
func shouldRedirectToHomeForRequest(r *http.Request) bool {
	if r.Header.Get(common.HtmxHttpRequestHeader) != "" {
		return true
	}

	switch strings.ToLower(strings.TrimSpace(r.Header.Get(common.WebPlatformHttpRequestHeader))) {
	case common.PlatformWeb:
		return true
	default:
		return false
	}
}
