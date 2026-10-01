package accessmanager

import (
	"context"
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// Native transport is opt-in through exact Settings return addresses. It uses
// the same account/session/challenge service, never a fabricated browser Origin.
type mobileOAuthConnectionsService interface {
	OAuthConnections(context.Context, string) (*OAuthConnectionsResponse, error)
	MobileOAuthDisconnectRedirectAllowed(string) bool
	StartMobileOAuthDisconnect(context.Context, string, string, string, string) (*OAuthDisconnectStartResponse, error)
	ConfirmMobileOAuthDisconnect(context.Context, string, *OAuthDisconnectConfirmRequest, string, string) (*OAuthDisconnectResponse, error)
	ReviewMobileOAuthDisconnectChallenge(context.Context, string, string, string, string) (*OAuthDisconnectStartResponse, error)
}

func nativeConnectionRedirect(r *http.Request) (string, error) {
	if r.Method != http.MethodGet || len(r.Header.Values("Origin")) != 0 {
		return "", ErrForbiddenUnableToAction
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(values) != 1 || len(values["redirect_uri"]) != 1 || values.Get("redirect_uri") == "" {
		return "", ErrBadRequest
	}
	return values.Get("redirect_uri"), nil
}

// MobileOAuthConnections retains connected identities even when native
// disconnects or a provider have been disabled by the deployment.
func (h *Handler) MobileOAuthConnections(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	redirect, err := nativeConnectionRedirect(r)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(mobileOAuthConnectionsService)
	if !ok {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	token, err := uniqueConnectionCookie(r, h.CookiePrefixAuthToken)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := service.OAuthConnections(r.Context(), token)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response.DisconnectAvailable = response.DisconnectAvailable && service.MobileOAuthDisconnectRedirectAllowed(redirect)
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

func (h *Handler) StartMobileOAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	var body struct {
		Email       string `json:"email"`
		RedirectURI string `json:"redirect_uri"`
	}
	// JSON-only POST plus rejecting any Origin follows the existing native OAuth
	// guards: cross-site forms cannot supply JSON and fetches carry an Origin.
	if !decodeMobileJSON(w, r, &body) {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrBadRequest)
		return
	}
	service, ok := h.Service.(mobileOAuthConnectionsService)
	if !ok {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	token, err := uniqueConnectionCookie(r, h.CookiePrefixAuthToken)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := service.StartMobileOAuthDisconnect(r.Context(), mux.Vars(r)["provider"], body.Email, body.RedirectURI, token)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusAccepted, response)
}

func (h *Handler) ConfirmMobileOAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	var body struct {
		OAuthDisconnectConfirmRequest
		RedirectURI string `json:"redirect_uri"`
	}
	if !decodeMobileJSON(w, r, &body) {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, ErrBadRequest)
		return
	}
	service, ok := h.Service.(mobileOAuthConnectionsService)
	if !ok {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	token, err := uniqueConnectionCookie(r, h.CookiePrefixAuthToken)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := service.ConfirmMobileOAuthDisconnect(r.Context(), mux.Vars(r)["provider"], &body.OAuthDisconnectConfirmRequest, body.RedirectURI, token)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.writeOAuthDisconnectResponse(w, response)
}

// ReviewMobileOAuthDisconnectChallenge never accepts proof or changes state.
// Links open this review through the initiating app's cookie jar, then require
// an explicit confirmation POST before any proof is consumed.
func (h *Handler) ReviewMobileOAuthDisconnectChallenge(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	redirect, err := nativeConnectionRedirect(r)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(mobileOAuthConnectionsService)
	if !ok {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	token, err := uniqueConnectionCookie(r, h.CookiePrefixAuthToken)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	vars := mux.Vars(r)
	response, err := service.ReviewMobileOAuthDisconnectChallenge(r.Context(), vars["provider"], vars["challengeID"], redirect, token)
	if err != nil {
		_ = h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}
