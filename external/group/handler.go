package group

import (
	"context"
	"github.com/ooaklee/ghatd/external/logger"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// GroupService interface defines expected methods of a valid group service
type GroupService interface {
	// CreateGroup creates a new group from the request and returns it in the
	// response. The Service implementation resolves and validates the parent, name
	// and hierarchy rules, assembles a UniversalGroup, persists it via the
	// repository and emits an audit event when configured.
	CreateGroup(ctx context.Context, r *CreateGroupRequest) (*CreateGroupResponse, error)
	// GetGroupByID retrieves a single group by its identifier and returns it in the
	// response. The Service implementation loads it from the repository, reinjects
	// dependencies and optionally prefixes the displayed name per the request.
	GetGroupByID(ctx context.Context, r *GetGroupByIDRequest) (*GetGroupByIDResponse, error)
	// GetGroupLineage returns the group's root-first lineage including the group
	// itself. The Service implementation walks lineage identifiers, resolving each
	// node's name and, when AsUserID is supplied, its member, owner and admin flags
	// for that user.
	GetGroupLineage(ctx context.Context, r *GetGroupLineageRequest) (*GetGroupLineageResponse, error)
	// GetGroupDescendants returns descendant groups grouped by depth level, index 0
	// holding direct children. The Service implementation honors MaxDepth and
	// IncludeSelf, filters visibility when AsUserID is supplied, and sorts each
	// level by raw name then ID.
	GetGroupDescendants(ctx context.Context, r *GetGroupDescendantsRequest) (*GetGroupDescendantsResponse, error)
	// GetGroupByNanoID retrieves a single group by its nano identifier. The Service
	// implementation fetches it from the repository, reinjects dependencies and
	// optionally prefixes the displayed name per the request.
	GetGroupByNanoID(ctx context.Context, r *GetGroupByNanoIDRequest) (*GetGroupByNanoIDResponse, error)
	// UpdateGroup applies changes from the request to an existing group and returns
	// the updated group. The Service implementation revalidates changed names,
	// merges provided fields, updates timestamps and state, persists via the
	// repository and emits an audit event when configured.
	UpdateGroup(ctx context.Context, r *UpdateGroupRequest) (*UpdateGroupResponse, error)
	// DeleteGroup deletes the requested group, cascading over its descendants
	// deepest-first. The Service implementation performs either hard deletion or
	// soft deletion per the request, records deletion metadata, and emits audit
	// events for each affected group when configured.
	DeleteGroup(ctx context.Context, r *DeleteGroupRequest) (*DeleteGroupResponse, error)
	// GetGroups returns groups matching the request's filters with pagination. The
	// Service implementation applies page and page-size defaults, counts totals,
	// loads matching groups from the repository, reinjects dependencies and
	// optionally prefixes displayed names.
	GetGroups(ctx context.Context, r *GetGroupsRequest) (*GetGroupsResponse, error)
	// GetGroupsByUserID returns groups where the user is referenced as owner or
	// member. The Service implementation optionally includes per-root descendant
	// trees via GetGroupDescendants and prefixes displayed names when requested; an
	// empty userID is rejected as invalid.
	GetGroupsByUserID(ctx context.Context, r *GetGroupsByUserIDRequest) (*GetGroupsByUserIDResponse, error)
	// GetGroupsAwaitingAnswerForInvitationsByMemberID returns groups where the
	// member has a pending invitation. The Service implementation loads candidates
	// from the repository, keeps only groups where the member's invitation is still
	// pending, and optionally prefixes displayed names.
	GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx context.Context, r *GetGroupsAwaitingAnswerForInvitationsByMemberIDRequest) (*GetGroupsAwaitingAnswerForInvitationsByMemberIDResponse, error)
	// GetGroupsByMemberID returns paginated groups containing a specific member.
	// The Service implementation applies page defaults, counts totals, queries by
	// member ID and optional member type, and paginates the dependency-reinjected
	// results.
	GetGroupsByMemberID(ctx context.Context, r *GetGroupsRequest) (*GetGroupsResponse, error)
	// GetGroupsByLeaderID returns paginated groups where the request's owner ID
	// matches the group owner. The Service implementation applies page defaults,
	// counts totals, queries owned groups from the repository, and paginates the
	// dependency-reinjected results.
	GetGroupsByLeaderID(ctx context.Context, r *GetGroupsRequest) (*GetGroupsResponse, error)
	// SearchGroupsByExtension returns paginated groups matching an extension field
	// value. The Service implementation applies page defaults, counts totals, and
	// passes the request's first extension filter key and value to the repository
	// before paginating results.
	SearchGroupsByExtension(ctx context.Context, r *GetGroupsRequest) (*GetGroupsResponse, error)
	// AddMember adds a member to a group and returns the updated group. The Service
	// implementation validates the role against the group type, ensures root
	// membership for nested groups, applies member checks via the model, persists
	// the change and emits an audit event when configured.
	AddMember(ctx context.Context, r *AddMemberRequest) (*AddMemberResponse, error)
	// InviteUser adds a pending invitation for a normalised email address to a
	// top-level group. The Service implementation validates the group level and
	// optional role, persists the pending invite member, and emits an audit event
	// when configured.
	InviteUser(ctx context.Context, r *InviteUserRequest) (*InviteUserResponse, error)
	// UninviteUser removes a pending invitation for a normalised email address from
	// a top-level group. The Service implementation persists the change via the
	// repository and emits an audit event when configured; it requires a top-level
	// group and an existing invitation.
	UninviteUser(ctx context.Context, r *UninviteUserRequest) (*UninviteUserResponse, error)
	// AcceptInvite accepts a pending invitation for a normalised email and user,
	// converting it into membership. The Service implementation applies it on a
	// top-level group, persists the updated group, and emits an audit event when
	// configured.
	AcceptInvite(ctx context.Context, r *AcceptInviteRequest) (*AcceptInviteResponse, error)
	// RejectInvite removes a pending invitation for a normalised email address from
	// a top-level group. The Service implementation persists the change via the
	// repository and emits an audit event when configured; it requires a top-level
	// group and an existing invitation.
	RejectInvite(ctx context.Context, r *RejectInviteRequest) (*RejectInviteResponse, error)
	// RemoveMember removes a member from the group identified in the request and
	// returns the updated group; the Service implementation requires
	// ConfirmOwnerRemoval when removing the owner, persists the removal, cascades
	// to descendants for root groups, and writes audit events.
	RemoveMember(ctx context.Context, r *RemoveMemberRequest) (*RemoveMemberResponse, error)
	// UpdateMemberRole updates a member's role in a group after validating the new
	// role against the group type configuration, persists the change, and returns
	// the updated group; the Service also writes an audit event.
	UpdateMemberRole(ctx context.Context, r *UpdateMemberRoleRequest) (*UpdateMemberRoleResponse, error)
	// GetGroupMembers retrieves members of a group, optionally filtered by member
	// type and role, and returns the matching members with their count.
	GetGroupMembers(ctx context.Context, r *GetGroupMembersRequest) (*GetGroupMembersResponse, error)
	// UpdateOwner changes a group's owner to the requested owner ID, ensuring the
	// new owner is a member with ADMIN role (and a root member for nested groups),
	// persists the change, and returns the updated group.
	UpdateOwner(ctx context.Context, r *UpdateOwnerRequest) (*UpdateOwnerResponse, error)
	// RepairInvalidMembers removes members with empty or null IDs from stored
	// groups and returns the affected group IDs before and after the repair plus
	// the derived repaired set and counts; the Service writes an audit event.
	RepairInvalidMembers(ctx context.Context) (*RepairInvalidMembersResponse, error)
	// ArchiveGroup sets the identified group's status to archived, persists the
	// change, and returns the updated group; the Service writes an audit event.
	ArchiveGroup(ctx context.Context, r *ArchiveGroupRequest) (*ArchiveGroupResponse, error)
	// RestoreGroup sets an archived group's status back to active, persists the
	// change, and returns the updated group; the Service writes an audit event.
	RestoreGroup(ctx context.Context, r *RestoreGroupRequest) (*RestoreGroupResponse, error)
	// GetGroupStats retrieves statistics for the group with the given ID, including
	// total, user, group and subgroup member counts.
	GetGroupStats(ctx context.Context, groupID string) (*GetGroupStatsResponse, error)
	// GetGroupsStats retrieves aggregate statistics across all groups from
	// repository counts, returning them with snake-case-normalised keys; the
	// request is currently unused by the implementation.
	GetGroupsStats(ctx context.Context, r *GetGroupsStatsRequest) (*GetGroupsStatsResponse, error)
	// GetGroupsConfig returns the capabilities of the service's group
	// configuration, falling back to the default configuration when none is set;
	// request and context are unused.
	GetGroupsConfig(ctx context.Context, r *GetGroupsConfigRequest) (*GetGroupsConfigResponse, error)
	// ValidateGroupName previews the RawName and resolved kebab-case Name a group
	// would receive without persisting anything, reporting availability, root-type
	// conflicts, auto-adjusted suffixes and a user-facing hint.
	ValidateGroupName(ctx context.Context, r *ValidateGroupNameRequest) (*ValidateGroupNameResponse, error)
	// GetParentGroupsWithAutoJoinForEmail returns active groups whose auto-join or
	// auto-invite email-domain settings match the domain of the provided email;
	// returns an error for empty or domain-less emails.
	GetParentGroupsWithAutoJoinForEmail(ctx context.Context, email string) (*GetParentGroupsWithAutoJoinForEmailResponse, error)
	// EnableGroupAutoJoinByEmailDomain enables auto-join for the identified group
	// using the supplied email domains and default member role, disables
	// auto-invite to keep them mutually exclusive, validates and persists the
	// settings, and returns the updated group.
	EnableGroupAutoJoinByEmailDomain(ctx context.Context, r *EnableGroupAutoJoinByEmailDomainRequest) (*EnableGroupAutoJoinByEmailDomainResponse, error)
	// DisableGroupAutoJoinByEmailDomain disables auto-join for the identified
	// group, clears the configured email domains and default role, persists the
	// change, and returns the updated group.
	DisableGroupAutoJoinByEmailDomain(ctx context.Context, r *DisableGroupAutoJoinByEmailDomainRequest) (*DisableGroupAutoJoinByEmailDomainResponse, error)
	// EnableGroupAutoInviteByEmailDomain enables auto-invite for the identified
	// group using the supplied email domains and default member role, disables
	// auto-join to keep them mutually exclusive, validates and persists the
	// settings, and returns the updated group.
	EnableGroupAutoInviteByEmailDomain(ctx context.Context, r *EnableGroupAutoInviteByEmailDomainRequest) (*EnableGroupAutoInviteByEmailDomainResponse, error)
	// DisableGroupAutoInviteByEmailDomain disables auto-invite for the identified
	// group, clears the configured email domains and default role, persists the
	// change, and returns the updated group.
	DisableGroupAutoInviteByEmailDomain(ctx context.Context, r *DisableGroupAutoInviteByEmailDomainRequest) (*DisableGroupAutoInviteByEmailDomainResponse, error)
}

// GroupValidator interface defines expected methods of a valid validator
type GroupValidator interface {
	// Validate checks that the supplied value satisfies the validator's configured
	// requirements, returning an error describing the first unmet requirement.
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
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-create-group")
	request, err := MapRequestToCreateGroupRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.CreateGroup(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Group)
}

// GetGroupByID handles getting a group by ID
func (h *Handler) GetGroupByID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-group-by-id")
	request, err := MapRequestToGetGroupByIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupByID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// GetGroupLineage handles getting a group's root-first lineage
func (h *Handler) GetGroupLineage(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-group-lineage")
	request, err := MapRequestToGetGroupLineageRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupLineage(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Lineage)
}

// GetGroupDescendants handles getting a group's descendants grouped by depth level
func (h *Handler) GetGroupDescendants(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-group-descendants")
	request, err := MapRequestToGetGroupDescendantsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupDescendants(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Descendants)
}

// GetGroupByNanoID handles getting a group by nano ID
func (h *Handler) GetGroupByNanoID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-group-by-nano-id")
	request, err := MapRequestToGetGroupByNanoIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupByNanoID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// GetGroups handles getting groups with filters and pagination
func (h *Handler) GetGroups(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-groups")
	request, err := MapRequestToGetGroupsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroups(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	// Return with pagination metadata if requested
	if request.Meta {
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups, reply.WithMeta(response.GetMetaData()))
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups)
}

// GetGroupsByUserID handles getting groups referenced by a user ID.
func (h *Handler) GetGroupsByUserID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-groups-by-user-id")
	request, err := MapRequestToGetGroupsByUserIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsByUserID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetGroupsAwaitingAnswerForInvitationsByMemberID handles getting groups
// with pending invitations matching the provided member ID.
func (h *Handler) GetGroupsAwaitingAnswerForInvitationsByMemberID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-groups-awaiting-answer-for-invitations-by-member-id")
	request, err := MapRequestToGetGroupsAwaitingAnswerForInvitationsByMemberIDRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsAwaitingAnswerForInvitationsByMemberID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Groups)
}

// GetGroupsByMemberID handles getting groups by member ID with pagination
func (h *Handler) GetGroupsByMemberID(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-groups-by-member-id")
	request, err := MapRequestToGetGroupsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsByMemberID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
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
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-groups-by-leader-id")
	request, err := MapRequestToGetGroupsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupsByLeaderID(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
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
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-search-groups-by-extension")
	request, err := MapRequestToGetGroupsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.SearchGroupsByExtension(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
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
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-update-group")
	request, err := MapRequestToUpdateGroupRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateGroup(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// DeleteGroup handles group deletion
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-delete-group")
	request, err := MapRequestToDeleteGroupRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	_, err = h.Service.DeleteGroup(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusNoContent, nil)
}

// AddMember handles adding a member to a group
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-add-member")
	request, err := MapRequestToAddMemberRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.AddMember(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// InviteUser handles inviting a user to a group
func (h *Handler) InviteUser(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-invite-user")
	request, err := MapRequestToInviteUserRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.InviteUser(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// UninviteUser handles revoking a pending invite from a group
func (h *Handler) UninviteUser(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-uninvite-user")
	request, err := MapRequestToUninviteUserRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UninviteUser(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// AcceptInvite handles accepting a pending invite for a group
func (h *Handler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-accept-invite")
	request, err := MapRequestToAcceptInviteRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.AcceptInvite(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// RejectInvite handles rejecting a pending invite for a group
func (h *Handler) RejectInvite(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-reject-invite")
	request, err := MapRequestToRejectInviteRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RejectInvite(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// RemoveMember handles removing a member from a group
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-remove-member")
	request, err := MapRequestToRemoveMemberRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RemoveMember(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// UpdateMemberRole handles updating a member's role
func (h *Handler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-update-member-role")
	request, err := MapRequestToUpdateMemberRoleRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateMemberRole(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// GetGroupMembers handles getting group members
func (h *Handler) GetGroupMembers(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-group-members")
	request, err := MapRequestToGetGroupMembersRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupMembers(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Members)
}

// UpdateOwner handles updating group owner
func (h *Handler) UpdateOwner(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-update-owner")
	request, err := MapRequestToUpdateOwnerRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.UpdateOwner(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// RepairInvalidMembers handles repairing groups that contain members with empty or null IDs.
func (h *Handler) RepairInvalidMembers(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-repair-invalid-members")
	response, err := h.Service.RepairInvalidMembers(r.Context())
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// ArchiveGroup handles archiving a group
func (h *Handler) ArchiveGroup(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-archive-group")
	request, err := MapRequestToArchiveGroupRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ArchiveGroup(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// RestoreGroup handles restoring an archived group
func (h *Handler) RestoreGroup(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-restore-group")
	request, err := MapRequestToRestoreGroupRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.RestoreGroup(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Group)
}

// GetGroupStats handles getting group statistics
func (h *Handler) GetGroupStats(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-group-stats")
	request, err := MapRequestToGetGroupStatsRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetGroupStats(r.Context(), request.ID)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetGroupsStats handles getting aggregate stats across all groups
func (h *Handler) GetGroupsStats(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-groups-stats")
	response, err := h.Service.GetGroupsStats(r.Context(), &GetGroupsStatsRequest{})
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetGroupsConfig handles getting the group service config
func (h *Handler) GetGroupsConfig(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-get-groups-config")
	response, err := h.Service.GetGroupsConfig(r.Context(), &GetGroupsConfigRequest{})
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Config)
}

// ValidateGroupName handles validating a proposed group name without persisting anything.
// Front-end forms can call this to preview what RawName and Name will be stored.
func (h *Handler) ValidateGroupName(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-validate-group-name")
	request, err := MapRequestToValidateGroupNameRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.ValidateGroupName(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// EnableGroupAutoJoinByEmailDomain enables auto-join for a group
func (h *Handler) EnableGroupAutoJoinByEmailDomain(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-enable-group-auto-join-by-email-domain")
	request, err := MapRequestToEnableGroupAutoJoinByEmailDomainRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.EnableGroupAutoJoinByEmailDomain(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// DisableGroupAutoJoinByEmailDomain disables auto-join for a group
func (h *Handler) DisableGroupAutoJoinByEmailDomain(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-disable-group-auto-join-by-email-domain")
	request, err := MapRequestToDisableGroupAutoJoinByEmailDomainRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.DisableGroupAutoJoinByEmailDomain(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// EnableGroupAutoInviteByEmailDomain enables auto-invite for a group
func (h *Handler) EnableGroupAutoInviteByEmailDomain(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-enable-group-auto-invite-by-email-domain")
	request, err := MapRequestToEnableGroupAutoInviteByEmailDomainRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.EnableGroupAutoInviteByEmailDomain(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// DisableGroupAutoInviteByEmailDomain disables auto-invite for a group
func (h *Handler) DisableGroupAutoInviteByEmailDomain(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/group", "handle-disable-group-auto-invite-by-email-domain")
	request, err := MapRequestToDisableGroupAutoInviteByEmailDomainRequest(r, h.Validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.DisableGroupAutoInviteByEmailDomain(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Errors("errors", errormanifest.ResponseErrors(err, h.responseManifests())))
		h.NewHTTPErrorResponse(w, err)
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// getBaseResponseHandler returns response handler configured with group error maps
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.responseManifests())
}

// responseManifests keeps success factories and error writers on the same
// domain base and last-wins caller override layers.
func (h *Handler) responseManifests() []reply.ErrorManifest {
	return errormanifest.NewComposer().Add(GroupErrorMap).AddOverrides(h.ErrorMaps...).Build()
}

// NewHTTPErrorResponse preserves mapped wrappers and validation collections.
// It returns writer failures and never passes raw diagnostic causes to reply.
func (h *Handler) NewHTTPErrorResponse(w http.ResponseWriter, err error, attributes ...reply.ResponseAttributes) error {
	return errormanifest.WriteHTTPError(w, err, h.responseManifests(), attributes...)
}
