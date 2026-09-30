package accessmanager

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

type oauthConnectionsService interface {
	OAuthConnections(context.Context, string) (*OAuthConnectionsResponse, error)
	OAuthConnectionsOrigin() string
	StartOAuthDisconnect(context.Context, string, string, string) (*OAuthDisconnectStartResponse, error)
	ConfirmOAuthDisconnect(context.Context, string, *OAuthDisconnectConfirmRequest, string) (*OAuthDisconnectResponse, error)
}

func uniqueConnectionCookie(r *http.Request, name string) (string, error) {
	var value string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			count++
			value = cookie.Value
		}
	}
	if count != 1 || value == "" {
		return "", ErrOAuthReauthenticationRequired
	}
	return value, nil
}
func (h *Handler) OAuthConnections(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	service, ok := h.Service.(oauthConnectionsService)
	if !ok {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	token, err := uniqueConnectionCookie(r, h.CookiePrefixAuthToken)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := service.OAuthConnections(r.Context(), token)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response.DisconnectAvailable = response.DisconnectAvailable && h.OAuthOrigin != "" && h.OAuthOrigin == service.OAuthConnectionsOrigin()
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}
func (h *Handler) decodeConnectionMutation(w http.ResponseWriter, r *http.Request, body interface{}) (oauthConnectionsService, string, string, error) {
	service, ok := h.Service.(oauthConnectionsService)
	if !ok {
		return nil, "", "", user.ErrOAuthUnsupported
	}
	if r.Method != http.MethodPost || h.OAuthOrigin == "" || r.Header.Get("Origin") != h.OAuthOrigin || service.OAuthConnectionsOrigin() != h.OAuthOrigin {
		return nil, "", "", ErrForbiddenUnableToAction
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, "", "", ErrBadRequest
	}
	token, err := uniqueConnectionCookie(r, h.CookiePrefixAuthToken)
	if err != nil {
		return nil, "", "", err
	}
	provider, err := getProviderNameFromURI(r)
	if err != nil {
		return nil, "", "", err
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(body) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, "", "", ErrBadRequest
	}
	return service, provider, token, nil
}
func (h *Handler) StartOAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	var body struct {
		Email string `json:"email"`
	}
	service, provider, token, err := h.decodeConnectionMutation(w, r, &body)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := service.StartOAuthDisconnect(r.Context(), provider, body.Email, token)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusAccepted, response)
}
func (h *Handler) ConfirmOAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	var body OAuthDisconnectConfirmRequest
	service, provider, token, err := h.decodeConnectionMutation(w, r, &body)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	response, err := service.ConfirmOAuthDisconnect(r.Context(), provider, &body, token)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}
	if response.Session != nil {
		tokens := response.Session
		h.AddAuthCookies(w, tokens.AccessToken, tokens.AtExpires, tokens.RefreshToken, tokens.RtExpires)
		toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, tokens.AtExpires, tokens.RtExpires)
	}
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}
