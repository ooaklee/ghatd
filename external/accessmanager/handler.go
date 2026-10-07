package accessmanager

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
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
	LogoutUser(ctx context.Context, r *LogoutUserRequest) error
	CreateUserAPIToken(ctx context.Context, r *CreateUserAPITokenRequest) (*CreateUserAPITokenResponse, error)
	DeleteUserAPIToken(ctx context.Context, r *DeleteUserAPITokenRequest) error
	UpdateUserAPITokenStatus(ctx context.Context, r *UserAPITokenStatusRequest) error
	GetSpecificUserAPITokens(ctx context.Context, r *GetSpecificUserAPITokensRequest) (*GetSpecificUserAPITokensResponse, error)
	GetUserAPITokenThreshold(ctx context.Context, r *GetUserAPITokenThresholdRequest) (*GetUserAPITokenThresholdResponse, error)
	OauthLogin(ctx context.Context, r *OauthLoginRequest) (*OauthLoginResponse, error)
	OauthCallback(ctx context.Context, r *OauthCallbackRequest) (*OauthCallbackResponse, error)
	LogoutUserOthers(ctx context.Context, r *LogoutUserOthersRequest) error
	UpdateUserEmail(ctx context.Context, r *UpdateUserEmailRequest) (*UpdateUserEmailResponse, error)
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

// UpdateUserEmail returns a confirmed mutation receipt with post-commit delivery
// flags. Errors use the shared manifest; neither local logs nor reply receive raw
// private diagnostics here. Only a valid self-change receipt clears caller cookies.
func (h *Handler) UpdateUserEmail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	request, err := MapRequestToUpdateUserEmailRequest(r, h.CookiePrefixAuthToken, h.CookiePrefixRefreshToken, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, userv2.ErrEmailChangeUnavailable)
		return
	}
	result, err := h.Service.UpdateUserEmail(r.Context(), request)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if result == nil || !result.Changed || result.SignOutRequired != (request.ActorID == request.TargetUserID) {
		h.NewHTTPErrorResponse(w, userv2.ErrEmailChangeUnavailable)
		return
	}
	if result.SignOutRequired {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, result)
}

// LogoutUserOthers requests an owner-scoped sweep retaining the two supplied,
// owner-verified records. It cannot prove they form the initiating session pair.
// It never clears cookies on failed authorization or storage work.
func (h *Handler) LogoutUserOthers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	request, err := MapRequestToLogoutUserOthersRequest(r, h.Validator, h.CookiePrefixAuthToken, h.CookiePrefixRefreshToken)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrSessionVerificationUnavailable)
		return
	}
	if err := h.Service.LogoutUserOthers(r.Context(), request); err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if err := r.Context().Err(); err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// GetUserAPITokenThreshold writes current display limits through reply. A
// missing adapter result is unavailable, never an unlimited allowance.
func (h *Handler) GetUserAPITokenThreshold(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	request, err := MapRequestToGetUserAPITokenThresholdRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrTokenPolicyUnavailable)
		return
	}
	result, err := h.Service.GetUserAPITokenThreshold(r.Context(), request)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if !validTokenThresholdResponse(result) {
		h.NewHTTPErrorResponse(w, ErrTokenPolicyUnavailable)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, result)
}

// GetSpecificUserAPITokens returns owner-checked display rows, never plaintext
// secrets. Both default and custom manager adapters pass the same projection.
func (h *Handler) GetSpecificUserAPITokens(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	request, err := MapRequestToGetSpecificUserAPITokensRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrTokenPolicyUnavailable)
		return
	}
	response, err := h.Service.GetSpecificUserAPITokens(r.Context(), request)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if response == nil {
		h.NewHTTPErrorResponse(w, ErrTokenPolicyUnavailable)
		return
	}
	safe, err := tokenListResponse(&apitoken.GetAPITokensForResponse{APITokens: response.UserAPITokens, Total: response.Total, TotalPages: response.TotalPages, Page: response.Page, APITokensPerPage: response.ResourcesPerPage}, request.UserID)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if request.Meta {
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, safe.UserAPITokens, reply.WithMeta(safe.GetMetaData()))
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, safe.UserAPITokens)
}

// RevokeUserAPIToken preserves the established 202 blank success contract.
// Native failures are resolved by the shared manifest without raw diagnostic logs.
func (h *Handler) RevokeUserAPIToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	request, err := MapRequestToRevokeUserAPITokenRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrTokenPolicyUnavailable)
		return
	}
	if err := h.Service.UpdateUserAPITokenStatus(r.Context(), request); err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// ActivateUserAPIToken preserves the established 202 blank success contract.
// Native failures are resolved by the shared manifest without raw diagnostic logs.
func (h *Handler) ActivateUserAPIToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	request, err := MapRequestToActivateUserAPITokenRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrTokenPolicyUnavailable)
		return
	}
	if err := h.Service.UpdateUserAPITokenStatus(r.Context(), request); err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// DeleteUserAPIToken preserves the established 202 blank success contract.
// Native failures are resolved by the shared manifest without raw diagnostic logs.
func (h *Handler) DeleteUserAPIToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	request, err := MapRequestToDeleteUserAPITokenRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrTokenPolicyUnavailable)
		return
	}
	if err := h.Service.DeleteUserAPIToken(r.Context(), request); err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted)
}

// CreateUserAPIToken publishes a validated creation-only secret and a 201
// response. Failure/uncertain outcomes do not expose any adapter-supplied secret.
func (h *Handler) CreateUserAPIToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	request, err := MapRequestToCreateUserAPITokenRequest(r, h.Validator)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrTokenPolicyUnavailable)
		return
	}
	response, err := h.Service.CreateUserAPIToken(r.Context(), request)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if response == nil {
		h.NewHTTPErrorResponse(w, ErrTokenPolicyUnavailable)
		return
	}
	safe, err := createdTokenResponse(&apitoken.CreateAPITokenResponse{APIToken: response.UserAPIToken}, request.UserID)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, safe.UserAPIToken)
}

// LogoutUser clears client cookies on every outcome. Credential selection is
// transport-only; the manager verifies deletion authority and performs cleanup.
// Native operational failures never become blank success or a web redirect.
func (h *Handler) LogoutUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	h.RemoveAuthCookies(w)
	h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
	h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)
	request, err := MapRequestToLogoutUserRequest(r, h.CookiePrefixAuthToken, h.CookiePrefixRefreshToken)
	if err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrSessionVerificationUnavailable)
		return
	}
	if err := h.Service.LogoutUser(r.Context(), request); err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if err := r.Context().Err(); err != nil {
		h.NewHTTPErrorResponse(w, err)
		return
	}
	if redirectToHomeIfPlatformHeaderDetected(w, r) {
		return
	}
	status := http.StatusOK
	if request.AccessToken == "" {
		status = http.StatusAccepted
	}
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, status)
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
	w.Header().Set("Cache-Control", "no-store")
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-login-user")

	request, err := MapRequestToLoginUserRequest(r, h.Validator)
	if err != nil {
		logger.Warn("login-request-rejected")
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrSessionVerificationUnavailable, reply.WithContext(r.Context()))
		return
	}

	response, err := h.Service.LoginUser(r.Context(), request)
	if err != nil {
		logger.Warn("login-proof-exchange-failed")
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}
	if response == nil || response.AccessToken == "" || response.RefreshToken == "" {
		h.NewHTTPErrorResponse(w, ErrSessionVerificationUnavailable, reply.WithContext(r.Context()))
		return
	}

	h.AddAuthCookies(w, response.AccessToken, response.AccessTokenExpiresAt, response.RefreshToken, response.RefreshTokenExpiresAt)
	toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, response.AccessTokenExpiresAt, response.RefreshTokenExpiresAt)

	// get next step query param from request if available
	if nextStepQueryParam := r.URL.Query()[common.WebNextStepsHttpQueryParam]; len(nextStepQueryParam) > 0 && nextStepQueryParam[0] != "" {
		http.Redirect(w, r, nextStepQueryParam[0], http.StatusTemporaryRedirect)
		return
	}

	h.GetBaseResponseHandler().NewHTTPTokenResponse(w, http.StatusOK, fmt.Sprint(response.AccessTokenExpiresAt), fmt.Sprint(response.RefreshTokenExpiresAt), reply.WithContext(r.Context()))
}

// CreateInitalLoginOrVerificationTokenEmail returns the blank reply 202 for every
// service outcome after valid request mapping, including absence and outages.
// This is an enumeration-resistant receipt, not confirmation of email delivery.
// The legacy blank reply is {"data":"{}"}, not an error or account payload.
// Only mapping failures use public error manifests; credentials are never set.
func (h *Handler) CreateInitalLoginOrVerificationTokenEmail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-create-initial-login-or-verification-token-email")

	request, err := MapRequestToCreateInitalLoginOrVerificationTokenEmailRequest(r, h.Validator)
	if err != nil {
		logger.Warn("login-email-request-rejected")
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}
	if nilAccessDependency(h.Service) {
		err = ErrLoginEmailUnavailable
	} else {
		err = h.Service.CreateInitalLoginOrVerificationTokenEmail(r.Context(), request)
	}
	if err != nil {
		logger.Warn("login-email-delivery-not-confirmed")
	}
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusAccepted, reply.WithContext(r.Context()))
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
	if h.Service == nil {
		h.NewHTTPErrorResponse(w, ErrSessionVerificationUnavailable)
		return
	}
	if capture, ok := h.Service.(interface{ readSignupEvidence([]*http.Cookie) string }); ok {
		request.AttributionEvidence = capture.readSignupEvidence(r.Cookies())
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
	w.Header().Set("Cache-Control", "no-store")
	logger := logger.AcquireOperationFrom(r.Context(), "external/accessmanager", "handle-validate-email-verification-code")

	request, err := MapRequestToValidateEmailVerificationCodeRequest(r, h.Validator)
	if err != nil {
		logger.Warn("email-verification-request-rejected")
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}
	if nilAccessDependency(h.Service) {
		h.NewHTTPErrorResponse(w, ErrSessionVerificationUnavailable, reply.WithContext(r.Context()))
		return
	}

	revisions, err := h.Service.ValidateEmailVerificationCode(r.Context(), request)
	if err != nil {
		logger.Warn("email-verification-proof-exchange-failed")
		h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}
	if revisions == nil || revisions.AccessToken == "" || revisions.RefreshToken == "" {
		h.NewHTTPErrorResponse(w, ErrSessionVerificationUnavailable, reply.WithContext(r.Context()))
		return
	}

	h.AddAuthCookies(w, revisions.AccessToken, revisions.AccessTokenExpiresAt, revisions.RefreshToken, revisions.RefreshTokenExpiresAt)
	toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, revisions.AccessTokenExpiresAt, revisions.RefreshTokenExpiresAt)

	// get next step query param from request if available
	if nextStepQueryParam := r.URL.Query()[common.WebNextStepsHttpQueryParam]; len(nextStepQueryParam) > 0 && nextStepQueryParam[0] != "" {
		http.Redirect(w, r, nextStepQueryParam[0], http.StatusTemporaryRedirect)
		return
	}

	h.GetBaseResponseHandler().NewHTTPTokenResponse(w, http.StatusOK, fmt.Sprint(revisions.AccessTokenExpiresAt), fmt.Sprint(revisions.RefreshTokenExpiresAt), reply.WithContext(r.Context()))
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
