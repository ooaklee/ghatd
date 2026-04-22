package group

import (
	"context"
	"net/http"

	"github.com/ooaklee/reply"
)

// GroupService interface defines expected methods of a valid group service
type GroupService interface {
	CreateGroup(ctx context.Context, r *CreateGroupRequest) (*CreateGroupResponse, error)
	GetGroupByID(ctx context.Context, r *GetGroupByIDRequest) (*GetGroupByIDResponse, error)
	GetGroupByNanoID(ctx context.Context, r *GetGroupByNanoIDRequest) (*GetGroupByNanoIDResponse, error)
	GetGroupByName(ctx context.Context, r *GetGroupByNameRequest) (*GetGroupByNameResponse, error)
	UpdateGroup(ctx context.Context, r *UpdateGroupRequest) (*UpdateGroupResponse, error)
	DeleteGroup(ctx context.Context, r *DeleteGroupRequest) (*DeleteGroupResponse, error)
	GetGroups(ctx context.Context, r *GetGroupsRequest) (*GetGroupsResponse, error)
	GetGroupsByMemberID(ctx context.Context, r *GetGroupsRequest) (*GetGroupsResponse, error)
	GetGroupsByLeaderID(ctx context.Context, r *GetGroupsRequest) (*GetGroupsResponse, error)
	SearchGroupsByExtension(ctx context.Context, r *GetGroupsRequest) (*GetGroupsResponse, error)
	AddMember(ctx context.Context, r *AddMemberRequest) (*AddMemberResponse, error)
	RemoveMember(ctx context.Context, r *RemoveMemberRequest) (*RemoveMemberResponse, error)
	UpdateMemberRole(ctx context.Context, r *UpdateMemberRoleRequest) (*UpdateMemberRoleResponse, error)
	GetGroupMembers(ctx context.Context, r *GetGroupMembersRequest) (*GetGroupMembersResponse, error)
	UpdateLeadership(ctx context.Context, r *UpdateLeadershipRequest) (*UpdateLeadershipResponse, error)
	ArchiveGroup(ctx context.Context, r *ArchiveGroupRequest) (*ArchiveGroupResponse, error)
	RestoreGroup(ctx context.Context, r *RestoreGroupRequest) (*RestoreGroupResponse, error)
	GetGroupStats(ctx context.Context, groupID string) (*GetGroupStatsResponse, error)
	GetGroupsStats(ctx context.Context, r *GetGroupsStatsRequest) (*GetGroupsStatsResponse, error)
	GetGroupsConfig(ctx context.Context, r *GetGroupsConfigRequest) (*GetGroupsConfigResponse, error)
}

// GroupValidator interface defines expected methods of a valid validator
type GroupValidator interface {
	Validate(s interface{}) error
}

// Handler manages group requests
type Handler struct {
	Service   GroupService
	Validator GroupValidator
	ErrorMaps []reply.ErrorManifest
}

// NewHandler returns a new group handler
func NewHandler(service GroupService, validator GroupValidator, errorMaps ...reply.ErrorManifest) *Handler {
	return &Handler{
		Service:   service,
		Validator: validator,
		ErrorMaps: errorMaps,
	}
}

// CreateGroup handles group creation
func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToCreateGroupRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateGroup(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Group)
}

// GetGroupByID handles getting a group by ID
func (h *Handler) GetGroupByID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupByIDRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupByID(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// GetGroupByNanoID handles getting a group by nano ID
func (h *Handler) GetGroupByNanoID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupByNanoIDRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupByNanoID(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// GetGroupByName handles getting a group by name
func (h *Handler) GetGroupByName(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupByNameRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupByName(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// GetGroups handles getting groups with filters and pagination
func (h *Handler) GetGroups(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupsRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroups(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	// Return with pagination metadata if requested
	if request.Meta {
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups, reply.WithMeta(response.GetMetaData()))
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups)
}

// GetGroupsByMemberID handles getting groups by member ID with pagination
func (h *Handler) GetGroupsByMemberID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupsRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsByMemberID(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	// Return with pagination metadata if requested
	if request.Meta {
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups, reply.WithMeta(response.GetMetaData()))
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups)
}

// GetGroupsByLeaderID handles getting groups by leader ID with pagination
func (h *Handler) GetGroupsByLeaderID(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupsRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsByLeaderID(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	// Return with pagination metadata if requested
	if request.Meta {
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups, reply.WithMeta(response.GetMetaData()))
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups)
}

// SearchGroupsByExtension handles searching groups by extension field with pagination
func (h *Handler) SearchGroupsByExtension(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupsRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.SearchGroupsByExtension(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	// Return with pagination metadata if requested
	if request.Meta {
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups, reply.WithMeta(response.GetMetaData()))
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups)
}

// UpdateGroup handles group updates
func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateGroupRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateGroup(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// DeleteGroup handles group deletion
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToDeleteGroupRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	_, err = h.Service.DeleteGroup(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusNoContent, nil)
}

// AddMember handles adding a member to a group
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToAddMemberRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.AddMember(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// RemoveMember handles removing a member from a group
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToRemoveMemberRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RemoveMember(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// UpdateMemberRole handles updating a member's role
func (h *Handler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateMemberRoleRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateMemberRole(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// GetGroupMembers handles getting group members
func (h *Handler) GetGroupMembers(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupMembersRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupMembers(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Members)
}

// UpdateLeadership handles updating group leadership
func (h *Handler) UpdateLeadership(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateLeadershipRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateLeadership(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// ArchiveGroup handles archiving a group
func (h *Handler) ArchiveGroup(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToArchiveGroupRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ArchiveGroup(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// RestoreGroup handles restoring an archived group
func (h *Handler) RestoreGroup(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToRestoreGroupRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RestoreGroup(r.Context(), request)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// GetGroupStats handles getting group statistics
func (h *Handler) GetGroupStats(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetGroupStatsRequest(r, h.Validator)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupStats(r.Context(), request.ID)
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetGroupsStats handles getting aggregate stats across all groups
func (h *Handler) GetGroupsStats(w http.ResponseWriter, r *http.Request) {
	response, err := h.Service.GetGroupsStats(r.Context(), &GetGroupsStatsRequest{})
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetGroupsConfig handles getting the group service config
func (h *Handler) GetGroupsConfig(w http.ResponseWriter, r *http.Request) {
	response, err := h.Service.GetGroupsConfig(r.Context(), &GetGroupsConfigRequest{})
	if err != nil {
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Config)
}

// getBaseResponseHandler returns response handler configured with group error maps
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.ErrorMaps)
}
