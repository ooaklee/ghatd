package usermanager

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/reply"
)

// UsermanagerService manages business logic around usermanager request
type UsermanagerService interface {
	GetUserMicroProfile(ctx context.Context, r *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error)
	GetUserProfile(ctx context.Context, r *GetUserProfileRequest) (*GetUserProfileResponse, error)
	UpdateUserProfile(ctx context.Context, r *UpdateUserProfileRequest) (*UpdateUserProfileResponse, error)
	DeleteUserPermanently(ctx context.Context, r *DeleteUserPermanentlyRequest) error
}

// UsermanagerValidator expected methods of a valid
type UsermanagerValidator interface {
	Validate(s interface{}) error
}

// Handler manages usermanager requests
type Handler struct {
	Service                  UsermanagerService
	Validator                UsermanagerValidator
	errorMaps                []reply.ErrorManifest
	cookiePrefixAuthToken    string
	cookiePrefixRefreshToken string
	environment              string
	cookieDomain             string
}

// NewHandlerRequest holds things needed for creating a handler
type NewHandlerRequest struct {
	Service                  UsermanagerService
	Validator                UsermanagerValidator
	ErrorMaps                []reply.ErrorManifest
	Environment              string
	CookiePrefixAuthToken    string
	CookiePrefixRefreshToken string
	CookieDomain             string
}

// NewHandler returns usermanager handler
func NewHandler(r *NewHandlerRequest) *Handler {

	r.ErrorMaps = append(r.ErrorMaps, usermanagerErrorMap)

	return &Handler{
		Service:                  r.Service,
		Validator:                r.Validator,
		errorMaps:                r.ErrorMaps,
		cookiePrefixAuthToken:    r.CookiePrefixAuthToken,
		cookiePrefixRefreshToken: r.CookiePrefixRefreshToken,
		environment:              r.Environment,
		cookieDomain:             r.CookieDomain,
	}
}

// DeleteUserPermanently returns response for request to get user's
// profile
func (h *Handler) DeleteUserPermanently(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToDeleteUserPermanentlyRequest(r, h.Validator)
	if err != nil {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	err = h.Service.DeleteUserPermanently(r.Context(), request)
	if err != nil {
		h.RemoveAuthCookies(w)
		h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
		h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.RemoveAuthCookies(w)
	h.RemoveCookiesWithName(w, common.AccessTokenAuthInfoCookieName)
	h.RemoveCookiesWithName(w, common.RefreshTokenAuthInfoCookieName)

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// UpdateUserProfile returns response for request to update updatedable attributes
// of the user's profile
func (h *Handler) UpdateUserProfile(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateUserProfileRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateUserProfile(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.UserProfile)
}

// GetUserMicroProfile returns response for request to get user's
// micro profile
func (h *Handler) GetUserMicroProfile(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserMicroProfileRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserMicroProfile(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.UserMicroProfile)
}

// GetUserProfile returns response for request to get user's
// profile
func (h *Handler) GetUserProfile(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserProfileRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserProfile(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.UserProfile)
}

// GetBaseResponseHandler returns response handler configured with auth error map
func (h *Handler) GetBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.errorMaps)
}

// RemoveAuthCookies is handling removing the cookies from the client
// cookie store regardless of what happens on the platform
func (h *Handler) RemoveAuthCookies(w http.ResponseWriter) {

	toolbox.RemoveAuthCookies(w, h.environment, h.cookieDomain, h.cookiePrefixAuthToken, h.cookiePrefixRefreshToken)
}

// RemoveCookiesWithName is handling removing the cookies from the client
// cookie store regardless of what happens on the platform
func (h *Handler) RemoveCookiesWithName(w http.ResponseWriter, cookieName string) {

	toolbox.RemoveCookiesWithName(w, h.environment, cookieName, h.cookieDomain)
}
