package usermanager

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/reply/v2"
)

// UsermanagerService manages business logic around usermanager request
type UsermanagerService interface {
	GetUserMicroProfile(ctx context.Context, r *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error)
	GetUserProfile(ctx context.Context, r *GetUserProfileRequest) (*GetUserProfileResponse, error)
	GetUserByID(ctx context.Context, r *GetUserByIDRequest) (*GetUserByIDResponse, error)
	GetUsers(ctx context.Context, r *GetUsersRequest) (*GetUsersResponse, error)
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
	GetLatestNotificationOverviews(ctx context.Context, r *GetLatestNotificationOverviewsRequest) (*GetLatestNotificationOverviewsResponse, error)
	GetNotifierConfig(ctx context.Context, r *GetNotifierConfigRequest) (*GetNotifierConfigResponse, error)
	RegisterNotificationAddress(ctx context.Context, r *RegisterNotificationAddressRequest) (*RegisterNotificationAddressResponse, error)
	ListNotificationAddresses(ctx context.Context, r *ListNotificationAddressesRequest) (*ListNotificationAddressesResponse, error)
	DeleteNotificationAddress(ctx context.Context, r *DeleteNotificationAddressRequest) error
	GetNotificationPreferences(ctx context.Context, r *GetNotificationPreferencesRequest) (*GetNotificationPreferencesResponse, error)
	UpdateNotificationPreferences(ctx context.Context, r *UpdateNotificationPreferencesRequest) (*UpdateNotificationPreferencesResponse, error)
	NotifyUser(ctx context.Context, r *NotifyUserRequest) (*NotifyUserResponse, error)
	NotifyUsers(ctx context.Context, r *NotifyUsersRequest) (*NotifyUsersResponse, error)
	GetMyGroupInvitations(ctx context.Context, r *GetMyGroupInvitationsRequest) (*GetMyGroupInvitationsResponse, error)
	AcceptMyGroupInvitation(ctx context.Context, r *AcceptMyGroupInvitationRequest) (*AcceptMyGroupInvitationResponse, error)
	RejectMyGroupInvitation(ctx context.Context, r *RejectMyGroupInvitationRequest) (*RejectMyGroupInvitationResponse, error)
	GetGroupDetail(ctx context.Context, r *GetGroupDetailRequest) (*GetGroupDetailResponse, error)
	GetGroupStats(ctx context.Context, r *GetGroupStatsRequest) (*GetGroupStatsResponse, error)
	CreateGroup(ctx context.Context, r *CreateGroupRequest) (*CreateGroupResponse, error)
	UpdateGroup(ctx context.Context, r *UpdateGroupRequest) (*UpdateGroupResponse, error)
	DeleteGroup(ctx context.Context, r *DeleteGroupRequest) (*DeleteGroupResponse, error)
	GetGroupsByUserID(ctx context.Context, r *GetGroupsByUserIDRequest) (*GetGroupsByUserIDResponse, error)
	GetGroupsConfig(ctx context.Context, r *GetGroupsConfigRequest) (*GetGroupsConfigResponse, error)
	GetGroupLineage(ctx context.Context, r *GetGroupLineageRequest) (*GetGroupLineageResponse, error)
	GetGroupDescendants(ctx context.Context, r *GetGroupDescendantsRequest) (*GetGroupDescendantsResponse, error)
	ValidateGroupName(ctx context.Context, r *ValidateGroupNameRequest) (*ValidateGroupNameResponse, error)
	// Group management methods
	AddGroupMember(ctx context.Context, r *AddGroupMemberRequest) (*AddGroupMemberResponse, error)
	RemoveGroupMember(ctx context.Context, r *RemoveGroupMemberRequest) (*RemoveGroupMemberResponse, error)
	UpdateGroupMember(ctx context.Context, r *UpdateGroupMemberRequest) (*UpdateGroupMemberResponse, error)
	UpdateGroupOwner(ctx context.Context, r *UpdateGroupOwnerRequest) (*UpdateGroupOwnerResponse, error)
	// Reminder methods
	CreateReminder(ctx context.Context, r *CreateReminderRequest) (*CreateReminderResponse, error)
	GetReminderByID(ctx context.Context, r *GetReminderByIDRequest) (*GetReminderByIDResponse, error)
	ListReminders(ctx context.Context, r *ListRemindersRequest) (*ListRemindersResponse, error)
	UpdateReminderByID(ctx context.Context, r *UpdateReminderByIDRequest) (*UpdateReminderByIDResponse, error)
	DeleteReminderByID(ctx context.Context, r *DeleteReminderByIDRequest) error
	DisableReminderByID(ctx context.Context, r *DisableReminderByIDRequest) (*UpdateReminderByIDResponse, error)
	GetReminderStats(ctx context.Context, r *GetReminderStatsRequest) (*GetReminderStatsResponse, error)
	GetDueReminders(ctx context.Context, r *GetDueRemindersRequest) (*GetDueRemindersResponse, error)
	// Streak methods
	RecordStreak(ctx context.Context, r *RecordStreakRequest) (*RecordStreakResponse, error)
	ListStreaks(ctx context.Context, r *ListStreaksRequest) (*ListStreaksResponse, error)
	GetCurrentStreak(ctx context.Context, r *GetCurrentStreakRequest) (*GetCurrentStreakResponse, error)
	GetLongestStreak(ctx context.Context, r *GetLongestStreakRequest) (*GetLongestStreakResponse, error)
	GetNumberOfStreaks(ctx context.Context, r *GetNumberOfStreaksRequest) (*GetNumberOfStreaksResponse, error)
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

// GetGroupLineage handles the request to get a group's lineage
func (h *Handler) GetGroupLineage(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupLineageRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupLineage(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Lineage)
}

// GetGroupsByUserID handles the request to get groups by user ID
// GetGroupDescendants handles the request to get group descendants
func (h *Handler) GetGroupDescendants(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupDescendantsRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupDescendants(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Descendants)
}

// GetGroupsByUserID handles the request to get groups by user ID.
func (h *Handler) GetGroupsByUserID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupsByUserIDRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsByUserID(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetGroupsConfig handles the request to get the group service config
func (h *Handler) GetGroupsConfig(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupsConfigRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsConfig(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Config)
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

// GetUsers returns response for request to get users
func (h *Handler) GetUsers(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUsersRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetUsers(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if request.IncludeMeta {
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users, reply.WithMeta(response.Meta.GetMetaData()))
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Users)
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

// GetUserGroupMembershipsRequest handles the request to get a user's team memberships.
func (h *Handler) GetUserGroupMembershipsRequest(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetUserGroupMembershipsRequestRequest(r, h.Validator)
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

// GetLatestNotificationOverviews handles the request to get latest notification overviews.
func (h *Handler) GetLatestNotificationOverviews(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetLatestNotificationOverviewsRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetLatestNotificationOverviews(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if response == nil || response.GetLatestNotificationOverviewsResponse == nil {
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, []common.NotificationOverview{})
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Overviews)
}

// GetNotifierConfig handles the request to get notifier config.
func (h *Handler) GetNotifierConfig(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetNotifierConfigRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetNotifierConfig(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Config)
}

// RegisterNotificationAddress handles notification address registration.
func (h *Handler) RegisterNotificationAddress(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToRegisterNotificationAddressRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RegisterNotificationAddress(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Address)
}

// ListNotificationAddresses handles notification address listing.
func (h *Handler) ListNotificationAddresses(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToListNotificationAddressesRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ListNotificationAddresses(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	addresses := response.Addresses
	if addresses == nil && response.ListNotificationAddressesResponse != nil {
		addresses = make([]NotificationAddressWithUser, 0, len(response.ListNotificationAddressesResponse.Addresses))
		for _, address := range response.ListNotificationAddressesResponse.Addresses {
			addresses = append(addresses, NotificationAddressWithUser{NotificationAddressSummary: address})
		}
	}

	if request.ListNotificationAddressesRequest != nil && request.ListNotificationAddressesRequest.Meta {
		meta := response.Meta
		if meta == nil && response.ListNotificationAddressesResponse != nil {
			meta = response.ListNotificationAddressesResponse.GetMetaData()
		}
		if meta != nil {
			h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, addresses, reply.WithMeta(meta))
			return
		}
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, addresses)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, addresses)
}

// DeleteNotificationAddress handles notification address deletion.
func (h *Handler) DeleteNotificationAddress(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToDeleteNotificationAddressRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if err := h.Service.DeleteNotificationAddress(r.Context(), request); err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// GetNotificationPreferences handles notification preference lookup.
func (h *Handler) GetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetNotificationPreferencesRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetNotificationPreferences(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	preferences := response.Preferences
	if preferences == nil && response.GetNotificationPreferencesResponse != nil {
		preferences = &NotificationPreferencesWithUser{NotificationPreferences: response.GetNotificationPreferencesResponse.Preferences}
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, preferences)
}

// UpdateNotificationPreferences handles notification preference updates.
func (h *Handler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateNotificationPreferencesRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateNotificationPreferences(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	preferences := response.Preferences
	if preferences == nil && response.UpdateNotificationPreferencesResponse != nil {
		preferences = &NotificationPreferencesWithUser{NotificationPreferences: response.UpdateNotificationPreferencesResponse.Preferences}
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, preferences)
}

// NotifyUser handles admin/service notification sends.
func (h *Handler) NotifyUser(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToNotifyUserRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.NotifyUser(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Results)
}

// NotifyUsers handles admin notification dispatches to multiple users.
func (h *Handler) NotifyUsers(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToNotifyUsersRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.NotifyUsers(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Results)
}

// GetMyGroupInvitations handles the request to get the current user's outstanding group invitations.
func (h *Handler) GetMyGroupInvitations(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetMyGroupInvitationsRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetMyGroupInvitations(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Invitations)
}

// AcceptMyGroupInvitation handles the request to accept one of the current user's group invitations.
func (h *Handler) AcceptMyGroupInvitation(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToAcceptMyGroupInvitationRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.AcceptMyGroupInvitation(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.AcceptInviteResponse)
}

// RejectMyGroupInvitation handles the request to reject one of the current user's group invitations.
func (h *Handler) RejectMyGroupInvitation(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToRejectMyGroupInvitationRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RejectMyGroupInvitation(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.RejectInviteResponse)
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

// ValidateGroupName handles the request to validate a proposed group name
func (h *Handler) ValidateGroupName(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToValidateGroupNameRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ValidateGroupName(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.ValidateGroupNameResponse)
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

// UpdateGroup handles the request to update an existing group
func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateGroupRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateGroup(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// DeleteGroup handles the request to delete a group
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToDeleteGroupRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.DeleteGroup(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
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

// UpdateGroupMember handles the request to update a member role in a group
func (h *Handler) UpdateGroupMember(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateGroupMemberRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateGroupMember(r.Context(), request)
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

// CreateReminder handles the request to create a reminder.
func (h *Handler) CreateReminder(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToCreateReminderRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateReminder(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Reminder)
}

// GetReminderByID handles the request to get a reminder by ID.
func (h *Handler) GetReminderByID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetReminderByIDRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetReminderByID(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Reminder)
}

// ListReminders handles the request to list reminders.
func (h *Handler) ListReminders(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToListRemindersRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ListReminders(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Reminders)
}

// UpdateReminderByID handles the request to update a reminder by ID.
func (h *Handler) UpdateReminderByID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateReminderByIDRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateReminderByID(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Reminder)
}

// DeleteReminderByID handles the request to delete a reminder by ID.
func (h *Handler) DeleteReminderByID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToDeleteReminderByIDRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if err := h.Service.DeleteReminderByID(r.Context(), request); err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// DisableReminderByID handles the request to disable a reminder by ID.
func (h *Handler) DisableReminderByID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToDisableReminderByIDRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.DisableReminderByID(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Reminder)
}

// GetReminderStats handles the request to get reminder statistics.
func (h *Handler) GetReminderStats(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetReminderStatsRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetReminderStats(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Stats)
}

// GetDueReminders handles the request to get due reminders.
func (h *Handler) GetDueReminders(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetDueRemindersRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetDueReminders(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Reminders)
}

// RecordStreak handles the request to record a streak.
func (h *Handler) RecordStreak(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToRecordStreakRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RecordStreak(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Streak)
}

// ListStreaks handles the request to list streaks.
func (h *Handler) ListStreaks(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToListStreaksRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ListStreaks(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Streaks)
}

// GetCurrentStreak handles the request to get the current streak count.
func (h *Handler) GetCurrentStreak(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetCurrentStreakRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetCurrentStreak(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.GetCurrentCountResponse)
}

// GetLongestStreak handles the request to get the longest streak count.
func (h *Handler) GetLongestStreak(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetLongestStreakRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetLongestStreak(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.GetLongestStreakResponse)
}

// GetNumberOfStreaks handles the request to count streak entries.
func (h *Handler) GetNumberOfStreaks(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetNumberOfStreaksRequest(r, h.Validator)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetNumberOfStreaks(r.Context(), request)
	if err != nil {
		h.GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.GetNumberOfStreaksResponse)
}

// GetBaseResponseHandler returns response handler configured with auth error map
func (h *Handler) GetBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(
		errormanifest.NewComposer().
			Add(UsermanagerErrorMap).
			AddOverrides(h.ErrorMaps...).
			Build(),
	)
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
