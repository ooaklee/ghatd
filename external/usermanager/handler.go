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
	GetUserByID(ctx context.Context, r *GetUserByIDRequest) (*GetUserByIDResponse, error)
	UpdateUserProfile(ctx context.Context, r *UpdateUserProfileRequest) (*UpdateUserProfileResponse, error)
	DeleteUserPermanently(ctx context.Context, r *DeleteUserPermanentlyRequest) error
	CreateComms(ctx context.Context, req *CreateCommsRequest) (*CreateCommsResponse, error)
	GetComms(ctx context.Context, req *GetCommsRequest) (*GetCommsResponse, error)
	UpdateComms(ctx context.Context, req *UpdateCommsRequest) (*UpdateCommsResponse, error)
	GetCommsStats(ctx context.Context, req *GetCommsStatsRequest) (*GetCommsStatsResponse, error)
	// Group/Team management methods
	GetEnrichedUserProfile(ctx context.Context, r *GetEnrichedUserProfileRequest) (*GetEnrichedUserProfileResponse, error)
	GetUserGroupMemberships(ctx context.Context, r *GetUserGroupMembershipsRequest) (*GetUserGroupMembershipsResponse, error)
	GetUserGroups(ctx context.Context, r *GetUserGroupsRequest) (*GetUserGroupsResponse, error)
	GetGroupDetail(ctx context.Context, r *GetGroupDetailRequest) (*GetGroupDetailResponse, error)
	GetGroupStats(ctx context.Context, r *GetGroupStatsRequest) (*GetGroupStatsResponse, error)
	CreateGroup(ctx context.Context, r *CreateGroupRequest) (*CreateGroupResponse, error)
	// Group management methods
	AddGroupMember(ctx context.Context, r *AddGroupMemberRequest) (*AddGroupMemberResponse, error)
	RemoveGroupMember(ctx context.Context, r *RemoveGroupMemberRequest) (*RemoveGroupMemberResponse, error)
	UpdateGroupOwner(ctx context.Context, r *UpdateGroupOwnerRequest) (*UpdateGroupOwnerResponse, error)
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

// GetUserByID returns response for request to get user by ID
func (h *Handler) GetUserByID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserByIDRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserByID(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.User)

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

// GetCommsStats handles the request to get comms stats
func (h *Handler) GetCommsStats(w http.ResponseWriter, r *http.Request) {

	request, err := mapGetCommsStatsRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	getCommsStatsResponse, err := h.Service.GetCommsStats(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, getCommsStatsResponse.Stats)
}

// UpdateComms handles the request to update a comms
func (h *Handler) UpdateComms(w http.ResponseWriter, r *http.Request) {

	request, err := MapRequestToUpdateCommsRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	updateCommsResponse, err := h.Service.UpdateComms(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, updateCommsResponse.Comms)
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

// GetUserTeamMemberships handles the request to get a user's team memberships.
func (h *Handler) GetUserTeamMemberships(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserTeamMembershipsRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUserGroupMemberships(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetGroupDetail handles the request to fetch a group's details for the requester
func (h *Handler) GetGroupDetail(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupDetailRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupDetail(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Detail)
}

// GetGroupStats handles the request to fetch a group's stats for the requester
func (h *Handler) GetGroupStats(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupStatsRequest(r, h.Validator)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupStats(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Stats)
}

// CreateGroup handles the request to create a new group
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

// AddGroupMember handles the request to add a member to a group
func (h *Handler) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToAddGroupMemberRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.AddGroupMember(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// RemoveGroupMember handles the request to remove a member from a group
func (h *Handler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToRemoveGroupMemberRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RemoveGroupMember(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// UpdateGroupOwner handles the request to update group ownership
func (h *Handler) UpdateGroupOwner(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateGroupOwnerRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateGroupOwner(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
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
