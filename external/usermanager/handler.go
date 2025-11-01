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
	CreateComms(ctx context.Context, req *CreateCommsRequest) (*CreateCommsResponse, error)
	GetComms(ctx context.Context, req *GetCommsRequest) (*GetCommsResponse, error)
	// Group/Team management methods
	GetEnrichedUserProfile(ctx context.Context, r *GetEnrichedUserProfileRequest) (*GetEnrichedUserProfileResponse, error)
	GetUserGroups(ctx context.Context, r *GetUserGroupsRequest) (*GetUserGroupsResponse, error)
	GetUserTeamMemberships(ctx context.Context, r *GetUserTeamMembershipsRequest) (*GetUserTeamMembershipsResponse, error)
	UpdateUserTeamMembership(ctx context.Context, r *UpdateUserTeamMembershipRequest) (*UpdateUserTeamMembershipResponse, error)
	RemoveUserFromGroup(ctx context.Context, r *RemoveUserFromGroupRequest) (*RemoveUserFromGroupResponse, error)
	FindUserInfo(ctx context.Context, r *FindUserInfoRequest) (*FindUserInfoResponse, error)
	BulkUpdateUserGroupMemberships(ctx context.Context, r *BulkUpdateUserGroupMembershipsRequest) (*BulkUpdateUserGroupMembershipsResponse, error)
	GetGroupsByType(ctx context.Context, r *GetGroupsByTypeRequest) (*GetGroupsByTypeResponse, error)
	CreateGroup(ctx context.Context, r *CreateGroupRequest) (*CreateGroupResponse, error)
}

// UsermanagerValidator expected methods of a valid
type UsermanagerValidator interface {
	Validate(s interface{}) error
}

// Handler manages usermanager requests
type Handler struct {
	Service                  UsermanagerService
	Validator                UsermanagerValidator
	ErrorMaps                []reply.ErrorManifest
	CookiePrefixAuthToken    string
	CookiePrefixRefreshToken string
	Environment              string
	CookieDomain             string
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

	return &Handler{
		Service:                  r.Service,
		Validator:                r.Validator,
		ErrorMaps:                r.ErrorMaps,
		CookiePrefixAuthToken:    r.CookiePrefixAuthToken,
		CookiePrefixRefreshToken: r.CookiePrefixRefreshToken,
		Environment:              r.Environment,
		CookieDomain:             r.CookieDomain,
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
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
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
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.MicroProfile)
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
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Profile)
}

// CreateComms handles the request to create a comms
func (h *Handler) CreateComms(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToCreateCommsRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	newCommsResponse, err := h.Service.CreateComms(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, newCommsResponse.Comms)
}

// GetComms handles the request to get a comms
func (h *Handler) GetComms(w http.ResponseWriter, r *http.Request) {

	request, err := mapGetCommsRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	getCommsResponse, err := h.Service.GetComms(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, getCommsResponse.Comms, reply.WithMeta(getCommsResponse.Meta))
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, getCommsResponse.Comms)
}

// GetEnrichedUserProfile handles the request to get an enriched user profile with group memberships
func (h *Handler) GetEnrichedUserProfile(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetEnrichedUserProfileRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetEnrichedUserProfile(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Profile)
}

// GetUserGroups handles the request to get a user's group memberships
func (h *Handler) GetUserGroups(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserGroupsRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserGroups(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups, reply.WithMeta(response.Meta))
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups)
}

// GetUserTeamMemberships handles the request to get team membership status for a user
func (h *Handler) GetUserTeamMemberships(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserTeamMembershipsRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserTeamMemberships(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Memberships)
}

// UpdateUserTeamMembership handles the request to update a user's team membership
func (h *Handler) UpdateUserTeamMembership(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateUserTeamMembershipRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateUserTeamMembership(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Membership)
}

// RemoveUserFromGroup handles the request to remove a user from a group
func (h *Handler) RemoveUserFromGroup(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToRemoveUserFromGroupRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RemoveUserFromGroup(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// FindUserInfo handles the request to find user information with optional group membership data
func (h *Handler) FindUserInfo(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToFindUserInfoRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.FindUserInfo(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)
}

// BulkUpdateUserGroupMemberships handles the request to perform bulk group membership operations
func (h *Handler) BulkUpdateUserGroupMemberships(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToBulkUpdateUserGroupMembershipsRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.BulkUpdateUserGroupMemberships(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	// Use 207 Multi-Status if there are failures, otherwise 200 OK
	statusCode := http.StatusOK
	if response.FailureCount > 0 {
		statusCode = http.StatusMultiStatus
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, statusCode, response)
}

// GetGroupsByType handles the request to get groups filtered by type
func (h *Handler) GetGroupsByType(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupsByTypeRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsByType(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups, reply.WithMeta(response.Meta))
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups)
}

// CreateGroup handles the admin request to create a new group
func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToCreateGroupRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateGroup(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Group)
}

// GetBaseResponseHandler returns response handler configured with auth error map
func (h *Handler) GetBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.ErrorMaps)
}

// RemoveAuthCookies is handling removing the cookies from the client
// cookie store regardless of what happens on the platform
func (h *Handler) RemoveAuthCookies(w http.ResponseWriter) {

	toolbox.RemoveAuthCookies(w, h.Environment, h.CookieDomain, h.CookiePrefixAuthToken, h.CookiePrefixRefreshToken)
}

// RemoveCookiesWithName is handling removing the cookies from the client
// cookie store regardless of what happens on the platform
func (h *Handler) RemoveCookiesWithName(w http.ResponseWriter, cookieName string) {

	toolbox.RemoveCookiesWithName(w, h.Environment, cookieName, h.CookieDomain)
}
