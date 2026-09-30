package accessmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// oauthLinkService keeps the existing AccessmanagerService interface compatible.
type oauthLinkService interface {
	OAuthProviders() []string
	OAuthLink(context.Context, *OauthLoginRequest, string) (*OauthLoginResponse, error)
}

// oauthHeaders prevents sensitive callback URLs and responses being cached or referred.
func oauthHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// setOAuthCookie scopes opaque state to the callback host and exact provider path.
func (h *Handler) setOAuthCookie(w http.ResponseWriter, cookie *http.Cookie, provider string) {
	cookie.Domain = ""
	cookie.Path = common.ApiV1UriPrefix + "/ams/oauth/" + provider + "/callback"
	cookie.HttpOnly = true
	cookie.Secure = h.Environment != "local" || strings.HasPrefix(h.OAuthOrigin, "https://") || provider == "apple"
	cookie.SameSite = http.SameSiteLaxMode
	if provider == "apple" {
		cookie.SameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, cookie)
}

// clearOAuthCookie removes transient state using the same attributes as initiation.
func (h *Handler) clearOAuthCookie(w http.ResponseWriter, name, provider string) {
	h.setOAuthCookie(w, &http.Cookie{Name: name, MaxAge: -1, Expires: time.Unix(1, 0)}, provider)
}

// OauthLogin redirects a validated browser transaction to its configured provider.
func (h *Handler) OauthLogin(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	request, err := MapRequestToOauthLoginRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.Service.OauthLogin(r.Context(), request)
	if err != nil {
		if request.Browser {
			h.oauthErrorRedirect(w, r, err, request.RequestUrl)
			return
		}
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.setOAuthCookie(w, response.CookieCore, request.Provider)
	http.Redirect(w, r, response.ProviderAuthCodeUrl, http.StatusFound)
}

// OAuthProviders exposes availability without exposing any provider credentials.
func (h *Handler) OAuthProviders(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	providers := []string{}
	if service, ok := h.Service.(oauthLinkService); ok {
		providers = service.OAuthProviders()
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, map[string]interface{}{"providers": providers})
}

// OAuthLink requires a same-origin POST and a fresh signed session cookie.
func (h *Handler) OAuthLink(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	if r.Method != http.MethodPost || h.OAuthOrigin == "" || r.Header.Get("Origin") != h.OAuthOrigin {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrForbiddenUnableToAction)
		return
	}
	service, ok := h.Service.(oauthLinkService)
	if !ok {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	provider, err := getProviderNameFromURI(r)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	var body struct {
		RequestURL string `json:"request_url"`
		Browser    bool   `json:"browser"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrBadRequest)
		return
	}
	path, err := oauth.ValidateSecureReturnPath(body.RequestURL)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	cookie, err := r.Cookie(h.CookiePrefixAuthToken)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrOAuthReauthenticationRequired)
		return
	}
	response, err := service.OAuthLink(r.Context(), &OauthLoginRequest{Provider: provider, RequestUrl: path, Browser: body.Browser}, cookie.Value)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.setOAuthCookie(w, response.CookieCore, provider)
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, map[string]interface{}{"redirect_url": response.ProviderAuthCodeUrl})
}

// OauthCallback completes browser redirects while retaining the default API token response.
func (h *Handler) OauthCallback(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	request, err := MapRequestToOauthCallbackRequest(r, h.Validator)
	if err != nil {
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			h.oauthErrorRedirect(w, r, err, "/app")
			return
		}
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := h.Service.OauthCallback(r.Context(), request)
	if response != nil && response.ProviderStateCookieKey != "" {
		h.clearOAuthCookie(w, response.ProviderStateCookieKey, request.Provider)
	}
	if response != nil && response.Mobile != nil {
		h.finishMobileCallback(w, r, response, err)
		return
	}
	if err != nil {
		if response != nil && response.Browser {
			h.oauthErrorRedirect(w, r, err, response.RequestUrl)
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			h.oauthErrorRedirect(w, r, err, "/app")
			return
		}
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	if !response.Linked {
		h.AddAuthCookies(w, response.AccessToken, response.AccessTokenExpiresAt, response.RefreshToken, response.RefreshTokenExpiresAt)
		toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, response.AccessTokenExpiresAt, response.RefreshTokenExpiresAt)
	}
	if response.Browser {
		http.Redirect(w, r, response.RequestUrl, http.StatusSeeOther)
		return
	}
	if response.RequestUrl != "" {
		w.Header().Add("Access-Control-Expose-Headers", common.WebLocationHttpRequestHeader)
		w.Header().Set(common.WebLocationHttpRequestHeader, response.RequestUrl)
	}
	if response.Linked {
		h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
		return
	}
	h.GetBaseResponseHandler().NewHTTPTokenResponse(w, http.StatusOK, fmt.Sprint(response.AccessTokenExpiresAt), fmt.Sprint(response.RefreshTokenExpiresAt))
}

// oauthErrorCode maps failures to a fixed vocabulary without leaking provider responses.
func oauthErrorCode(err error) string {
	switch {
	case errors.Is(err, oauth.ErrProviderCancelled):
		return "cancelled"
	case errors.Is(err, ErrOAuthReauthenticationRequired):
		return "reauth_required"
	case errors.Is(err, user.ErrOAuthLinkRequired), errors.Is(err, user.ErrOAuthIdentityConflict):
		return "link_required"
	case errors.Is(err, user.ErrOAuthRestricted):
		return "restricted"
	case errors.Is(err, oauth.ErrSecureIDTokenUnverified):
		return "unverified_email"
	case errors.Is(err, ErrProvidersPassedNotFound), errors.Is(err, oauth.ErrSecureProviderIncompleteConfig), errors.Is(err, user.ErrOAuthUnsupported):
		return "unavailable"
	case errors.Is(err, ErrBadRequest), errors.Is(err, ErrProviderCookieNotFound), errors.Is(err, oauth.ErrSecureTransactionInvalidState), errors.Is(err, oauth.ErrSecureTransactionNotFound), errors.Is(err, oauth.ErrSecureTransactionExpired), errors.Is(err, oauth.ErrSecureIDTokenInvalid):
		return "invalid"
	default:
		return "failed"
	}
}

// oauthErrorRedirect preserves only an independently validated local return path.
func (h *Handler) oauthErrorRedirect(w http.ResponseWriter, r *http.Request, err error, path string) {
	path, pathErr := oauth.ValidateSecureReturnPath(path)
	if pathErr != nil {
		path = "/app"
	}
	query := url.Values{"oauth_error": {oauthErrorCode(err)}, "request_url": {path}}
	http.Redirect(w, r, "/auth/login?"+query.Encode(), http.StatusSeeOther)
}
