package usermanager

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/logger"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

// GetGroupsConfig retrieves the group service configuration capabilities.
func (s *Service) GetGroupsConfig(ctx context.Context, _ *GetGroupsConfigRequest) (*GetGroupsConfigResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled")
		return nil, ErrGroupServiceNotEnabled
	}

	resp, err := s.GroupService.GetGroupsConfig(ctx, &group.GetGroupsConfigRequest{})
	if err != nil {
		log.Error("failed-to-get-groups-config", zap.Error(err))
		return nil, err
	}

	return &GetGroupsConfigResponse{
		GetGroupsConfigResponse: resp,
	}, nil
}

// ValidateGroupName validates a proposed group name through the group service.
// Admin requesters can validate directly. Non-admin requesters must have
// access to the provided parent_group_id (when supplied).
func (s *Service) ValidateGroupName(ctx context.Context, r *ValidateGroupNameRequest) (*ValidateGroupNameResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserID))
		return nil, ErrGroupServiceNotEnabled
	}

	if r.ValidateGroupNameRequest == nil {
		return nil, ErrRequestFailedValidation
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserID, log)
	if !isAdmin {
		parentGroupID := strings.TrimSpace(r.ParentGroupID)
		if parentGroupID != "" {
			hasAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserID, parentGroupID)
			if accessErr != nil {
				log.Error(
					"failed-to-resolve-requester-group-access-map",
					zap.String("requester_user_id", r.UserID),
					zap.String("group_id", parentGroupID),
					zap.Error(accessErr),
				)
				return nil, ErrFailedToResolveGroupAccessMap
			}

			if !hasAccess.IsAccessible {
				return nil, group.ErrInsufficientPermissions
			}
		}
	}

	resp, err := s.GroupService.ValidateGroupName(ctx, r.ValidateGroupNameRequest)
	if err != nil {
		log.Error("failed-to-validate-group-name", zap.Error(err))
		return nil, err
	}

	return &ValidateGroupNameResponse{ValidateGroupNameResponse: resp}, nil
}

// CreateGroup handles creating a new group with the provided details. Only admins or users with
// access to the parent group (if specified) can create groups.
func (s *Service) CreateGroup(ctx context.Context, r *CreateGroupRequest) (*CreateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserID))
		return nil, ErrGroupServiceNotEnabled
	}

	// Auth check: only allow if requester is admin or
	// has access to parent group (if specified)
	isAdmin := s.isRequesterAdmin(ctx, r.UserID, log)
	if !isAdmin {

		if r.ParentGroupID == "" {
			log.Error("non-user-attempting-to-create-group-without-parent", zap.String("user-id", r.UserID))
			return nil, group.ErrInsufficientPermissions
		}

		hasGroupAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserID, r.ParentGroupID)
		if accessErr != nil {
			log.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester_user_id", r.UserID),
				zap.String("group_id", r.ParentGroupID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		if !hasGroupAccess.IsAdmin {
			return nil, group.ErrInsufficientPermissions
		}

		// For non-admins, they should be able to set the owner to themselves or anyone else
		// explicitly (the search endpoint should allow them to find users they have access
		// to), if they leave it blank (which will default to themselves).
		if r.CreateGroupRequest.OwnerID == "" {
			r.CreateGroupRequest.OwnerID = r.UserID
		}
	}

	// Create the group via group service
	groupResp, err := s.GroupService.CreateGroup(ctx, r.CreateGroupRequest)
	if err != nil {
		log.Error("failed-to-create-group", zap.String("name", r.CreateGroupRequest.Name), zap.String("type", r.CreateGroupRequest.Type), zap.Error(err))
		return nil, err
	}

	return &CreateGroupResponse{
		Group: groupResp.Group,
	}, nil
}

// UpdateGroup handles updating an existing group. Admin users can update any group.
// Non-admin users must have effective admin-level access to the target group,
// which also covers admin/owner permissions inherited from parent groups.
func (s *Service) UpdateGroup(ctx context.Context, r *UpdateGroupRequest) (*UpdateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	if r.UpdateGroupRequest == nil {
		return nil, ErrRequestFailedValidation
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserId, log)
	if !isAdmin {
		accessMap, accessErr := s.GroupService.GetUserGroupAccessMap(ctx, r.UserId)
		if accessErr != nil {
			log.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester_user_id", r.UserId),
				zap.String("group_id", r.UpdateGroupRequest.ID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		groupAccess, ok := accessMap[r.UpdateGroupRequest.ID]
		if !ok || !groupAccess.IsAccessible || !groupAccess.IsAdmin {
			return nil, group.ErrInsufficientPermissions
		}
	}

	resp, err := s.GroupService.UpdateGroup(ctx, r.UpdateGroupRequest)
	if err != nil {
		log.Error("failed-to-update-group", zap.String("group_id", r.UpdateGroupRequest.ID), zap.Error(err))
		return nil, err
	}

	return &UpdateGroupResponse{UpdateGroupResponse: resp}, nil
}

// DeleteGroup handles deleting a group. Admin users may request hard/soft delete;
// non-admin users must be the owner of the target group and are forced to hard-delete.
func (s *Service) DeleteGroup(ctx context.Context, r *DeleteGroupRequest) (*DeleteGroupResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserID))
		return nil, ErrGroupServiceNotEnabled
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserID, log)
	if !isAdmin {
		accessMap, accessErr := s.GroupService.GetUserGroupAccessMap(ctx, r.UserID)
		if accessErr != nil {
			log.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester_user_id", r.UserID),
				zap.String("group_id", r.DeleteGroupRequest.ID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		groupAccess, ok := accessMap[r.DeleteGroupRequest.ID]
		if !ok || !groupAccess.IsAccessible || !groupAccess.IsAdmin {
			return nil, group.ErrInsufficientPermissions
		}

		groupResp, groupErr := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.DeleteGroupRequest.ID})
		if groupErr != nil {
			log.Error(
				"failed-to-resolve-group-for-delete-ownership-check",
				zap.String("requester_user_id", r.UserID),
				zap.String("group_id", r.DeleteGroupRequest.ID),
				zap.Error(groupErr),
			)
			return nil, groupErr
		}

		if strings.TrimSpace(groupResp.Group.OwnerID) != strings.TrimSpace(r.UserID) {
			return nil, group.ErrInsufficientPermissions
		}

		// Non-admin users are restricted to hard delete.
		r.DeleteGroupRequest.HardDelete = true
	}

	// Always attribute deletion to the requester at the usermanager boundary.
	r.DeleteGroupRequest.DeletedByID = r.UserID

	groupResp, err := s.GroupService.DeleteGroup(ctx, r.DeleteGroupRequest)
	if err != nil {
		log.Error("failed-to-delete-group", zap.String("group_id", r.DeleteGroupRequest.ID), zap.Error(err))
		return nil, err
	}

	return &DeleteGroupResponse{DeleteGroupResponse: groupResp}, nil
}

// AddGroupMember adds a user to a group
func (s *Service) AddGroupMember(ctx context.Context, r *AddGroupMemberRequest) (*AddGroupMemberResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserID))
		return nil, ErrGroupServiceNotEnabled
	}

	// Auth check: only allow if requester is admin or
	// has access to parent group (if specified)
	isAdmin := s.isRequesterAdmin(ctx, r.UserID, log)
	if !isAdmin {

		hasGroupAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserID, r.GroupID)
		if accessErr != nil {
			log.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester_user_id", r.UserID),
				zap.String("group_id", r.GroupID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		if !hasGroupAccess.IsAdmin {
			return nil, group.ErrInsufficientPermissions
		}
	}

	r.AddMemberRequest.Type = group.MemberTypeUser
	_, err := s.GroupService.AddMember(ctx, r.AddMemberRequest)
	if err != nil {
		log.Error("add-group-member-failed",
			zap.String("group-id", r.GroupID),
			zap.String("member-id", r.MemberID),
			zap.Error(err),
		)
		return nil, ErrFailedToAddUserToGroup
	}

	return &AddGroupMemberResponse{Success: true}, nil
}

// RemoveGroupMember removes a user from a group
func (s *Service) RemoveGroupMember(ctx context.Context, r *RemoveGroupMemberRequest) (*RemoveGroupMemberResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserID))
		return nil, ErrGroupServiceNotEnabled
	}

	// Auth check: only allow if requester is admin or
	// has access to parent group (if specified)
	isAdmin := s.isRequesterAdmin(ctx, r.UserID, log)
	if !isAdmin {

		hasGroupAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserID, r.GroupID)
		if accessErr != nil {
			log.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester_user_id", r.UserID),
				zap.String("group_id", r.GroupID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		if !hasGroupAccess.IsAdmin {
			return nil, group.ErrInsufficientPermissions
		}
	}

	_, err := s.GroupService.RemoveMember(ctx, r.RemoveMemberRequest)
	if err != nil {
		log.Error("remove-group-member-failed",
			zap.String("group-id", r.GroupID),
			zap.String("member-id", r.MemberID),
			zap.Error(err),
		)
		return nil, ErrFailedToRemoveUserFromGroup
	}

	return &RemoveGroupMemberResponse{Success: true}, nil
}

// UpdateGroupMember updates a user's role in a group
func (s *Service) UpdateGroupMember(ctx context.Context, r *UpdateGroupMemberRequest) (*UpdateGroupMemberResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserID))
		return nil, ErrGroupServiceNotEnabled
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserID, log)
	if !isAdmin {

		hasGroupAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserID, r.GroupID)
		if accessErr != nil {
			log.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester_user_id", r.UserID),
				zap.String("group_id", r.GroupID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		if !hasGroupAccess.IsAdmin {
			return nil, group.ErrInsufficientPermissions
		}
	}

	_, err := s.GroupService.UpdateMemberRole(ctx, r.UpdateMemberRoleRequest)
	if err != nil {
		log.Error("update-group-member-failed",
			zap.String("group-id", r.GroupID),
			zap.String("member-id", r.MemberID),
			zap.String("new-role", r.NewRole),
			zap.Error(err),
		)
		return nil, err
	}

	return &UpdateGroupMemberResponse{Success: true}, nil
}

// UpdateGroupOwner updates the owner of a group
func (s *Service) UpdateGroupOwner(ctx context.Context, r *UpdateGroupOwnerRequest) (*UpdateGroupOwnerResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserID))
		return nil, ErrGroupServiceNotEnabled
	}

	// Auth check: only allow if requester is admin or
	// has access to parent group (if specified)
	isAdmin := s.isRequesterAdmin(ctx, r.UserID, log)
	if !isAdmin {

		hasGroupAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserID, r.GroupID)
		if accessErr != nil {
			log.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester_user_id", r.UserID),
				zap.String("group_id", r.GroupID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		if !hasGroupAccess.IsAdmin {
			return nil, group.ErrInsufficientPermissions
		}
	}

	_, err := s.GroupService.UpdateOwner(ctx, &group.UpdateOwnerRequest{
		GroupID: r.GroupID,
		OwnerID: r.OwnerID,
	})
	if err != nil {
		log.Error("update-group-owner-failed",
			zap.String("group-id", r.GroupID),
			zap.Error(err),
		)
		return nil, ErrFailedToUpdateGroupOwner
	}

	// Re-fetch and resolve the updated owner to return enriched data
	groupResp, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID})
	if err != nil || groupResp == nil || groupResp.Group == nil {
		return &UpdateGroupOwnerResponse{Owner: &EnrichedOwner{}}, nil
	}

	owner := &EnrichedOwner{
		Owner: s.resolveUserToEnrichedMember(ctx, groupResp.Group.OwnerID),
	}

	return &UpdateGroupOwnerResponse{Owner: owner}, nil
}

// resolveUserToEnrichedMember looks up a user by ID and returns an EnrichedMember.
// Returns nil when the ID is empty; on lookup failure it returns a stub with only the ID set
// so the caller still has something useful to display.
func (s *Service) resolveUserToEnrichedMember(ctx context.Context, userID string) *EnrichedMember {
	if userID == "" {
		return nil
	}
	em := &EnrichedMember{ID: userID}
	if s.UserService == nil {
		return em
	}
	userResp, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: userID})
	if err == nil && userResp != nil && userResp.User != nil {
		em.Email = userResp.User.Email
		if userResp.User.Type != "" {
			em.Type = userResp.User.Type
		}
		em.Roles = userResp.User.Roles
		if userResp.User.PersonalInfo != nil {
			em.FullName = userResp.User.PersonalInfo.FullName
			if em.FullName == "" {
				em.FullName = strings.TrimSpace(strings.TrimSpace(userResp.User.PersonalInfo.FirstName) + " " + strings.TrimSpace(userResp.User.PersonalInfo.LastName))
			}
		}
		if em.FullName == "" {
			em.FullName = em.Email
		}
		em.Initials = deriveInitials(em.FullName)
	}
	return em
}

func deriveInitials(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 1 {
		runes := []rune(parts[0])
		if len(runes) == 0 {
			return ""
		}
		return strings.ToUpper(string(runes[0]))
	}
	first := []rune(parts[0])
	last := []rune(parts[len(parts)-1])
	if len(first) == 0 || len(last) == 0 {
		return ""
	}
	return strings.ToUpper(string(first[0]) + string(last[0]))
}
