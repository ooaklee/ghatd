package accessmanager

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// oauthConnectionsService is the optional web Settings contract for listing
// providers and verifying email before disconnection.
type oauthConnectionsService interface {
	OAuthConnections(context.Context, string) (*OAuthConnectionsResponse, error)
	OAuthConnectionsOrigin() string
	StartOAuthDisconnect(context.Context, string, string, string) (*OAuthDisconnectStartResponse, error)
	ConfirmOAuthDisconnect(context.Context, string, *OAuthDisconnectConfirmRequest, string) (*OAuthDisconnectResponse, error)
}

// uniqueConnectionCookie requires exactly one non-empty session cookie, avoiding
// ambiguous authentication when several cookies share the configured name.
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

// OAuthConnections returns the signed-in account's linked providers and the
// configured provider-management capabilities.
func (h *Handler) OAuthConnections(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	service, ok := h.Service.(oauthConnectionsService)
	if !ok {
		_ = h.NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	token, err := uniqueConnectionCookie(r, h.CookiePrefixAuthToken)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	response, err := service.OAuthConnections(r.Context(), token)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	response.DisconnectAvailable = response.DisconnectAvailable && h.OAuthOrigin != "" && h.OAuthOrigin == service.OAuthConnectionsOrigin()
	// Web Settings verification additionally needs an isolated, readable proof
	// store, so it is advertised separately from the disconnect capability and
	// only when the handler origin matches the configured connection origin.
	if verifier, ok := h.Service.(interface{ OAuthConnectionVerificationAvailable() bool }); ok {
		response.ConnectVerificationAvailable = verifier.OAuthConnectionVerificationAvailable() && h.OAuthOrigin != "" && h.OAuthOrigin == service.OAuthConnectionsOrigin()
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// decodeConnectionMutation requires the configured web origin, a bounded JSON
// object with no unknown fields, and one unambiguous session cookie.
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

// StartOAuthDisconnect starts email verification from web Settings after
// validating the request origin, JSON body and current session cookie.
func (h *Handler) StartOAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	var body struct {
		Email string `json:"email"`
	}
	service, provider, token, err := h.decodeConnectionMutation(w, r, &body)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	response, err := service.StartOAuthDisconnect(r.Context(), provider, body.Email, token)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusAccepted, response)
}

// ConfirmOAuthDisconnect confirms web Settings proof and writes replacement
// session cookies only when the service returns a completed session.
func (h *Handler) ConfirmOAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	var body OAuthDisconnectConfirmRequest
	service, provider, token, err := h.decodeConnectionMutation(w, r, &body)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	response, err := service.ConfirmOAuthDisconnect(r.Context(), provider, &body, token)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	h.writeOAuthDisconnectResponse(w, response)
}

// writeOAuthDisconnectResponse installs returned session cookies and exposes
// public verification metadata. A pending follow-up challenge receives 202;
// a completed disconnect or connection verification receives 200.
func (h *Handler) writeOAuthDisconnectResponse(w http.ResponseWriter, response *OAuthDisconnectResponse) {
	if response.Session != nil {
		tokens := response.Session
		h.AddAuthCookies(w, tokens.AccessToken, tokens.AtExpires, tokens.RefreshToken, tokens.RtExpires)
		toolbox.AddNonSecureAuthInfoCookie(w, h.CookieDomain, h.Environment, tokens.AtExpires, tokens.RtExpires)
	}
	// First-stage approvals stay 202 with the follow-up challenge; final
	// confirmations are 200 and disconnected.
	status := http.StatusOK
	if !response.Disconnected && !response.Reauthenticated {
		status = http.StatusAccepted
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, status, response)
}

// ReviewOAuthDisconnectChallenge backs the magic-link review screen. It is an
// authenticated, read-only GET: no emailed proof is accepted, nothing is
// consumed or mutated, and only the public start-response shape is returned.
func (h *Handler) ReviewOAuthDisconnectChallenge(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	service, ok := h.Service.(interface {
		ReviewOAuthDisconnectChallenge(context.Context, string, string, string) (*OAuthDisconnectStartResponse, error)
	})
	if !ok {
		_ = h.NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	provider := mux.Vars(r)["provider"]
	if provider != "google" && provider != "apple" {
		_ = h.NewHTTPErrorResponse(w, ErrBadRequest)
		return
	}
	token, err := uniqueConnectionCookie(r, h.CookiePrefixAuthToken)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	response, err := service.ReviewOAuthDisconnectChallenge(r.Context(), provider, mux.Vars(r)["challengeID"], token)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// webConnectionVerificationService is the optional handler capability for web
// Settings reauthentication before connecting Google or Apple. Hosts without it
// retain existing behaviour and receive an unsupported response here.
type webConnectionVerificationService interface {
	OAuthConnectionsOrigin() string
	StartOAuthConnectionVerification(context.Context, string, string) (*OAuthDisconnectStartResponse, error)
	ReviewOAuthConnectionVerification(context.Context, string, string, string) (*OAuthDisconnectStartResponse, error)
	ConfirmOAuthConnectionVerification(context.Context, string, *OAuthDisconnectConfirmRequest, string) (*OAuthDisconnectResponse, error)
}

// OAuthConnectionVerification serves web Settings reauthentication: GET
// reviews a challenge without accepting proof, POST starts one with an empty
// JSON body, and POST to /confirm consumes exactly one emailed proof. Every
// mutation requires the configured web Origin and strict JSON. Every request
// requires one unambiguous session cookie and matching origin configuration;
// the return address is derived from configuration.
func (h *Handler) OAuthConnectionVerification(w http.ResponseWriter, r *http.Request) {
	oauthHeaders(w)
	service, ok := h.Service.(webConnectionVerificationService)
	if !ok {
		_ = h.NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	if r.Method == http.MethodGet {
		if r.URL.RawQuery != "" {
			_ = h.NewHTTPErrorResponse(w, ErrBadRequest)
			return
		}
		provider := mux.Vars(r)["provider"]
		if provider != "google" && provider != "apple" {
			_ = h.NewHTTPErrorResponse(w, ErrBadRequest)
			return
		}
		// A read-only review still requires the handler and service to agree on
		// the configured web origin; callers cannot supply a return address.
		if h.OAuthOrigin == "" || service.OAuthConnectionsOrigin() != h.OAuthOrigin || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != h.OAuthOrigin) {
			_ = h.NewHTTPErrorResponse(w, ErrForbiddenUnableToAction)
			return
		}
		token, err := uniqueConnectionCookie(r, h.CookiePrefixAuthToken)
		if err != nil {
			_ = h.NewHTTPErrorResponse(w, err)
			return
		}
		response, err := service.ReviewOAuthConnectionVerification(r.Context(), provider, mux.Vars(r)["challengeID"], token)
		if err != nil {
			_ = h.NewHTTPErrorResponse(w, err)
			return
		}
		_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/confirm") {
		var body OAuthDisconnectConfirmRequest
		mutateService, provider, token, err := h.decodeConnectionMutation(w, r, &body)
		if err != nil {
			_ = h.NewHTTPErrorResponse(w, err)
			return
		}
		verifier, ok := mutateService.(webConnectionVerificationService)
		if !ok {
			_ = h.NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
			return
		}
		response, err := verifier.ConfirmOAuthConnectionVerification(r.Context(), provider, &body, token)
		if err != nil {
			_ = h.NewHTTPErrorResponse(w, err)
			return
		}
		h.writeOAuthDisconnectResponse(w, response)
		return
	}
	// Starting verification takes no client input; only the empty object is
	// accepted so callers cannot smuggle a return address or email.
	var body map[string]json.RawMessage
	mutateService, provider, token, err := h.decodeConnectionMutation(w, r, &body)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	if body == nil || len(body) != 0 {
		_ = h.NewHTTPErrorResponse(w, ErrBadRequest)
		return
	}
	verifier, ok := mutateService.(webConnectionVerificationService)
	if !ok {
		_ = h.NewHTTPErrorResponse(w, user.ErrOAuthUnsupported)
		return
	}
	response, err := verifier.StartOAuthConnectionVerification(r.Context(), provider, token)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	_ = h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusAccepted, response)
}
