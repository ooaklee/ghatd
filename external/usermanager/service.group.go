package usermanager

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/logger"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

// GetEnrichedUserProfile handles fetching an enriched user profile with group membership data
func (s *Service) GetEnrichedUserProfile(ctx context.Context, r *GetEnrichedUserProfileRequest) (*GetEnrichedUserProfileResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	// Get base user profile
	userResp, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{
		ID: r.UserId,
	})
	if err != nil {
		log.Error("failed-to-get-user-profile", zap.String("user-id", r.UserId), zap.Error(err))
		return nil, err
	}

	enrichedProfile := &EnrichedUserProfile{
		ID:          userResp.User.ID,
		Email:       userResp.User.Email,
		FullName:    userResp.User.PersonalInfo.FullName,
		FirstName:   userResp.User.PersonalInfo.FirstName,
		LastName:    userResp.User.PersonalInfo.LastName,
		Status:      userResp.User.Status,
		Roles:       userResp.User.Roles,
		CreatedAt:   userResp.User.Metadata.CreatedAt,
		LastLoginAt: userResp.User.Metadata.LastLoginAt,
		Teams:       []UserGroupMembership{},
		Departments: []UserGroupMembership{},
		Groups:      []UserGroupMembership{},
	}

	// Fetch group memberships if requested
	if r.IncludeTeams || r.IncludeAllGroups {
		teams, err := s.getUserGroupsByType(ctx, r.UserId, group.GroupTypeTeam)
		if err != nil {
			log.Warn("failed-to-fetch-team-memberships", zap.String("user-id", r.UserId), zap.Error(err))
		} else {
			enrichedProfile.Teams = teams
		}
	}

	if r.IncludeDepartments || r.IncludeAllGroups {
		departments, err := s.getUserGroupsByType(ctx, r.UserId, group.GroupTypeDepartment)
		if err != nil {
			log.Warn("failed-to-fetch-department-memberships", zap.String("user-id", r.UserId), zap.Error(err))
		} else {
			enrichedProfile.Departments = departments
		}
	}

	if r.IncludeAllGroups {
		allGroups, err := s.getUserAllGroups(ctx, r.UserId)
		if err != nil {
			log.Warn("failed-to-fetch-all-group-memberships", zap.String("user-id", r.UserId), zap.Error(err))
		} else {
			enrichedProfile.Groups = allGroups
		}
	}

	return &GetEnrichedUserProfileResponse{
		Profile: enrichedProfile,
	}, nil
}

// GetUserGroups handles fetching groups for a user with filtering
func (s *Service) GetUserGroups(ctx context.Context, r *GetUserGroupsRequest) (*GetUserGroupsResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	groupReq := &group.GetGroupsRequest{
		MemberID:   r.UserId,
		MemberType: group.MemberTypeUser,
		Page:       r.Page,
		PerPage:    r.PerPage,
		Meta:       r.Meta,
	}

	if r.GroupType != "" {
		parts := strings.Split(r.GroupType, ",")
		types := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				types = append(types, t)
			}
		}
		if len(types) > 0 {
			groupReq.Types = types
		}
	}

	if r.Status != "" {
		groupReq.Statuses = []string{r.Status}
	}

	groupsResp, err := s.GroupService.GetGroups(ctx, groupReq)
	if err != nil {
		log.Error("failed-to-get-user-groups", zap.String("user-id", r.UserId), zap.Error(err))
		return nil, err
	}

	groupSummaries := make([]GroupSummary, 0, len(groupsResp.Groups))
	for _, g := range groupsResp.Groups {
		groupSummaries = append(groupSummaries, GroupSummary{
			ID:          g.ID,
			NanoID:      g.NanoID,
			Name:        g.Name,
			Type:        g.Type,
			Description: g.DisplayInfo.Description,
			MemberCount: g.GetMemberCount(),
			Status:      g.Status,
			CreatedAt:   g.Metadata.CreatedAt,
		})
	}

	response := &GetUserGroupsResponse{
		Groups: groupSummaries,
		Total:  groupsResp.Total,
	}

	if r.Meta {
		response.Meta = map[string]interface{}{
			"total_resources":    groupsResp.Total,
			"total_pages":        groupsResp.TotalPages,
			"resources_per_page": groupsResp.PerPage,
			"page":               groupsResp.Page,
		}
	}

	return response, nil
}

// GetGroupDetail handles fetching a single group for a requester with membership/leadership checks
func (s *Service) GetGroupDetail(ctx context.Context, r *GetGroupDetailRequest) (*GetGroupDetailResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	groupResp, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID})
	if err != nil || groupResp == nil || groupResp.Group == nil {
		log.Warn("failed-to-get-group-detail", zap.String("group-id", r.GroupID), zap.Error(err))
		return nil, errors.New(ErrKeyGroupNotFound)
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserId, log)
	if !s.userHasGroupAccess(r.UserId, groupResp.Group, isAdmin) {
		return nil, errors.New(ErrKeyGroupNotFound)
	}

	return &GetGroupDetailResponse{Group: groupResp.Group}, nil
}

// GetGroupStats handles fetching group stats for a requester with membership/leadership checks
func (s *Service) GetGroupStats(ctx context.Context, r *GetGroupStatsRequest) (*GetGroupStatsResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	log.Panic("test-ing-log-panic-in-group-stats")

	groupResp, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID})
	if err != nil || groupResp == nil || groupResp.Group == nil {
		log.Warn("failed-to-get-group-for-stats", zap.String("group-id", r.GroupID), zap.Error(err))
		return nil, errors.New(ErrKeyGroupNotFound)
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserId, log)
	if !s.userHasGroupAccess(r.UserId, groupResp.Group, isAdmin) {
		return nil, errors.New(ErrKeyGroupNotFound)
	}

	totalUsers, roleBreakdown := s.calculateGroupSeatUsage(ctx, groupResp.Group, log)

	capacity := 9999
	if groupResp.Group.Settings != nil && groupResp.Group.Settings.MaxMembers > 0 {
		capacity = groupResp.Group.Settings.MaxMembers
	}

	visibility := ""
	if groupResp.Group.Settings != nil {
		visibility = groupResp.Group.Settings.Visibility
	}

	email := ""
	if groupResp.Group.DisplayInfo != nil {
		email = groupResp.Group.DisplayInfo.Email
	}

	stats := GroupStats{
		ID:         groupResp.Group.ID,
		NanoID:     groupResp.Group.NanoID,
		TotalUsers: totalUsers,
		Seats: GroupSeatInfo{
			Used:     totalUsers,
			Capacity: capacity,
		},
		Visibility: visibility,
		Email:      email,
		Meta: GroupStatsMeta{
			MemberCount:   groupResp.Group.GetMemberCount(),
			RoleBreakdown: roleBreakdown,
			Metadata:      groupResp.Group.Metadata,
		},
	}

	return &GetGroupStatsResponse{Stats: stats}, nil
}

// GetGroupByNanoID fetches a group by its nano ID
func (s *Service) GetGroupByNanoID(ctx context.Context, r *group.GetGroupByNanoIDRequest) (*group.GetGroupByNanoIDResponse, error) {
	if s.GroupService == nil {
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	return s.GroupService.GetGroupByNanoID(ctx, r)
}

// GetGroupMembers fetches members for a group
func (s *Service) GetGroupMembers(ctx context.Context, r *group.GetGroupMembersRequest) (*group.GetGroupMembersResponse, error) {
	if s.GroupService == nil {
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	return s.GroupService.GetGroupMembers(ctx, r)
}

// GetUserTeamMemberships handles fetching team memberships for a user
func (s *Service) GetUserTeamMemberships(ctx context.Context, r *GetUserTeamMembershipsRequest) (*GetUserTeamMembershipsResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	targetUserID := r.UserId
	if r.TargetUserId != "" {
		targetUserID = r.TargetUserId
	}

	// Get all teams
	teamsReq := &group.GetGroupsRequest{
		Types:   []string{group.GroupTypeTeam},
		PerPage: 100, // Get all teams
	}

	if !r.IncludeInactive {
		teamsReq.Statuses = []string{group.GroupStatusActive}
	}

	teamsResp, err := s.GroupService.GetGroups(ctx, teamsReq)
	if err != nil {
		log.Error("failed-to-get-teams", zap.Error(err))
		return nil, err
	}

	memberships := make(map[string]UserGroupMembership)

	for _, team := range teamsResp.Groups {
		membership := UserGroupMembership{
			Group: GroupSummary{
				ID:          team.ID,
				NanoID:      team.NanoID,
				Name:        team.Name,
				Type:        team.Type,
				Description: team.DisplayInfo.Description,
				MemberCount: team.GetMemberCount(),
				Status:      team.Status,
				CreatedAt:   team.Metadata.CreatedAt,
			},
			IsMember: team.HasMember(targetUserID),
		}

		if membership.IsMember {
			// Find the member details
			member, err := team.GetMemberByID(targetUserID)
			if err == nil {
				membership.Role = member.Role
				membership.JoinedAt = member.JoinedAt
			}
		}

		// Check leadership roles
		if team.Leadership != nil {
			membership.IsOwner = team.Leadership.OwnerID == targetUserID
			membership.IsLead = team.Leadership.LeadID == targetUserID
			membership.IsHead = team.Leadership.HeadID == targetUserID
		}

		memberships[team.Name] = membership
	}

	return &GetUserTeamMembershipsResponse{
		Memberships: memberships,
		UserID:      targetUserID,
	}, nil
}

// UpdateUserTeamMembership handles updating a user's team membership
func (s *Service) UpdateUserTeamMembership(ctx context.Context, r *UpdateUserTeamMembershipRequest) (*UpdateUserTeamMembershipResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	// Get the target group
	groupResp, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{
		ID: r.GroupID,
	})
	if err != nil {
		log.Error("failed-to-get-group", zap.String("group-id", r.GroupID), zap.Error(err))
		return nil, errors.New(ErrKeyGroupNotFound)
	}

	targetGroup := groupResp.Group

	// Check if user is already a member
	isMember := targetGroup.HasMember(r.UserId)
	if isMember {
		log.Warn("user-already-member-of-group", zap.String("user-id", r.UserId), zap.String("group-id", r.GroupID))
		return nil, errors.New(ErrKeyUserAlreadyMemberOfGroup)
	}

	var previousMemberships []GroupSummary

	// If removing from other teams, fetch and remove
	if r.RemoveFromOtherTeams {
		currentTeams, err := s.getUserGroupsByType(ctx, r.UserId, targetGroup.Type)
		if err != nil {
			log.Warn("failed-to-fetch-current-memberships", zap.String("user-id", r.UserId), zap.Error(err))
		} else {
			for _, membership := range currentTeams {
				if membership.IsMember && membership.Group.ID != r.GroupID {
					// Remove from this group
					_, err := s.GroupService.RemoveMember(ctx, &group.RemoveMemberRequest{
						GroupID:  membership.Group.ID,
						MemberID: r.UserId,
					})
					if err != nil {
						log.Warn("failed-to-remove-from-previous-group", zap.String("group-id", membership.Group.ID), zap.Error(err))
					} else {
						previousMemberships = append(previousMemberships, membership.Group)
					}
				}
			}
		}
	}

	// Add user to the new group
	role := r.Role
	if role == "" {
		role = group.MemberRoleMember // Default role
	}

	_, err = s.GroupService.AddMember(ctx, &group.AddMemberRequest{
		GroupID:  r.GroupID,
		MemberID: r.UserId,
		Type:     group.MemberTypeUser,
		Role:     role,
	})
	if err != nil {
		log.Error("failed-to-add-member-to-group", zap.String("group-id", r.GroupID), zap.String("user-id", r.UserId), zap.Error(err))
		return nil, errors.New(ErrKeyFailedToAddUserToGroup)
	}

	// Fetch updated membership
	updatedGroup, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{
		ID: r.GroupID,
	})
	if err != nil {
		log.Warn("failed-to-fetch-updated-group", zap.String("group-id", r.GroupID), zap.Error(err))
	}

	membership := UserGroupMembership{
		Group: GroupSummary{
			ID:          targetGroup.ID,
			NanoID:      targetGroup.NanoID,
			Name:        targetGroup.Name,
			Type:        targetGroup.Type,
			Description: targetGroup.DisplayInfo.Description,
			Status:      targetGroup.Status,
			CreatedAt:   targetGroup.Metadata.CreatedAt,
		},
		IsMember: true,
		Role:     role,
	}

	if updatedGroup != nil {
		membership.Group.MemberCount = updatedGroup.Group.GetMemberCount()
		member, err := updatedGroup.Group.GetMemberByID(r.UserId)
		if err == nil {
			membership.JoinedAt = member.JoinedAt
		}
	}

	return &UpdateUserTeamMembershipResponse{
		Success:             true,
		Membership:          membership,
		PreviousMemberships: previousMemberships,
	}, nil
}

// RemoveUserFromGroup handles removing a user from a group
func (s *Service) RemoveUserFromGroup(ctx context.Context, r *RemoveUserFromGroupRequest) (*RemoveUserFromGroupResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	_, err := s.GroupService.RemoveMember(ctx, &group.RemoveMemberRequest{
		GroupID:  r.GroupID,
		MemberID: r.UserId,
	})
	if err != nil {
		log.Error("failed-to-remove-user-from-group", zap.String("group-id", r.GroupID), zap.String("user-id", r.UserId), zap.Error(err))
		return nil, errors.New(ErrKeyFailedToRemoveUserFromGroup)
	}

	return &RemoveUserFromGroupResponse{
		Success: true,
		GroupID: r.GroupID,
		Message: "Successfully removed from group",
	}, nil
}

// FindUserInfo handles finding user information with flexible lookup
func (s *Service) FindUserInfo(ctx context.Context, r *FindUserInfoRequest) (*FindUserInfoResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled")
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	var userResp *userv2.GetUserProfileResponse
	var err error

	// Attempt to find user by ID or email
	if r.UserId != "" {
		userResp, err = s.UserService.GetUserProfile(ctx, &userv2.GetUserProfileRequest{
			ID: r.UserId,
		})
	} else if r.Email != "" {
		emailResp, err := s.UserService.GetUserByEmail(ctx, &userv2.GetUserByEmailRequest{
			Email: r.Email,
		})
		if err == nil && emailResp != nil {
			userResp, err = s.UserService.GetUserProfile(ctx, &userv2.GetUserProfileRequest{
				ID: emailResp.User.ID,
			})
		}
	}

	if err != nil {
		log.Error("failed-to-find-user", zap.String("user-id", r.UserId), zap.String("email", r.Email), zap.Error(err))
		return nil, errors.New(ErrKeyUserNotFound)
	}

	response := &FindUserInfoResponse{
		User:                  userResp.Profile,
		MembershipDataFetched: false,
	}

	// Fetch team memberships if requested
	if r.IncludeTeamMemberships {
		teams, err := s.getUserGroupsByType(ctx, userResp.Profile.ID, group.GroupTypeTeam)
		if err != nil {
			log.Warn("failed-to-fetch-team-memberships", zap.String("user-id", userResp.Profile.ID), zap.Error(err))
		} else {
			response.TeamMemberships = teams
			response.MembershipDataFetched = true
		}
	}

	// Fetch all group memberships if requested
	if r.IncludeGroupMemberships {
		allGroups, err := s.getUserAllGroups(ctx, userResp.Profile.ID)
		if err != nil {
			log.Warn("failed-to-fetch-group-memberships", zap.String("user-id", userResp.Profile.ID), zap.Error(err))
		} else {
			response.GroupMemberships = allGroups
			response.MembershipDataFetched = true
		}
	}

	return response, nil
}

// BulkUpdateUserGroupMemberships handles bulk updates of group memberships
func (s *Service) BulkUpdateUserGroupMemberships(ctx context.Context, r *BulkUpdateUserGroupMembershipsRequest) (*BulkUpdateUserGroupMembershipsResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("requesting-user-id", r.UserId), zap.String("target-user-id", r.TargetUserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	results := make([]GroupMembershipUpdateResult, 0, len(r.Actions))
	successCount := 0
	failureCount := 0

	for _, action := range r.Actions {
		result := GroupMembershipUpdateResult{
			GroupID:   action.GroupID,
			Action:    action.Action,
			UpdatedAt: time.Now().UTC(),
		}

		switch action.Action {
		case "ADD":
			role := action.Role
			if role == "" {
				role = group.MemberRoleMember
			}

			_, err := s.GroupService.AddMember(ctx, &group.AddMemberRequest{
				GroupID:  action.GroupID,
				MemberID: r.TargetUserId,
				Type:     group.MemberTypeUser,
				Role:     role,
			})
			if err != nil {
				result.Success = false
				result.Error = err.Error()
				failureCount++
				log.Warn("bulk-add-failed", zap.String("group-id", action.GroupID), zap.Error(err))
			} else {
				result.Success = true
				successCount++
			}

		case "REMOVE":
			_, err := s.GroupService.RemoveMember(ctx, &group.RemoveMemberRequest{
				GroupID:  action.GroupID,
				MemberID: r.TargetUserId,
			})
			if err != nil {
				result.Success = false
				result.Error = err.Error()
				failureCount++
				log.Warn("bulk-remove-failed", zap.String("group-id", action.GroupID), zap.Error(err))
			} else {
				result.Success = true
				successCount++
			}

		case "UPDATE_ROLE":
			_, err := s.GroupService.UpdateMemberRole(ctx, &group.UpdateMemberRoleRequest{
				GroupID:  action.GroupID,
				MemberID: r.TargetUserId,
				NewRole:  action.Role,
			})
			if err != nil {
				result.Success = false
				result.Error = err.Error()
				failureCount++
				log.Warn("bulk-update-role-failed", zap.String("group-id", action.GroupID), zap.Error(err))
			} else {
				result.Success = true
				successCount++
			}

		default:
			result.Success = false
			result.Error = "unknown action"
			failureCount++
		}

		results = append(results, result)
	}

	return &BulkUpdateUserGroupMembershipsResponse{
		Results:      results,
		SuccessCount: successCount,
		FailureCount: failureCount,
		TotalActions: len(r.Actions),
	}, nil
}

// GetGroupsByType handles fetching groups filtered by type
func (s *Service) GetGroupsByType(ctx context.Context, r *GetGroupsByTypeRequest) (*GetGroupsByTypeResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserId), zap.String("group-type", r.GroupType))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	groupReq := &group.GetGroupsRequest{
		Page:       r.Page,
		PerPage:    r.PerPage,
		OrderBy:    r.Order,
		Meta:       r.Meta,
		NameSearch: r.Name,
	}

	if r.GroupType != "" {
		parts := strings.Split(r.GroupType, ",")
		types := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				types = append(types, t)
			}
		}
		if len(types) > 0 {
			groupReq.Types = types
		}
	}

	if r.Status != "" {
		groupReq.Statuses = []string{r.Status}
	}

	if r.OnlyUserMemberships {
		groupReq.MemberID = r.UserId
		groupReq.MemberType = group.MemberTypeUser
	}

	groupsResp, err := s.GroupService.GetGroups(ctx, groupReq)
	if err != nil {
		log.Error("failed-to-get-groups-by-type", zap.String("type", r.GroupType), zap.Error(err))
		return nil, err
	}

	groupSummaries := make([]GroupSummary, 0, len(groupsResp.Groups))
	for _, g := range groupsResp.Groups {
		groupSummaries = append(groupSummaries, GroupSummary{
			ID:          g.ID,
			NanoID:      g.NanoID,
			Name:        g.Name,
			Type:        g.Type,
			Description: g.DisplayInfo.Description,
			MemberCount: g.GetMemberCount(),
			Status:      g.Status,
			CreatedAt:   g.Metadata.CreatedAt,
		})
	}

	response := &GetGroupsByTypeResponse{
		Groups: groupSummaries,
		Total:  groupsResp.Total,
	}

	if r.Meta {
		response.Meta = map[string]interface{}{
			"total_resources":    groupsResp.Total,
			"total_pages":        groupsResp.TotalPages,
			"resources_per_page": groupsResp.PerPage,
			"page":               groupsResp.Page,
		}
	}

	return response, nil
}

// Helper functions

// getUserGroupsByType fetches groups of a specific type for a user
func (s *Service) getUserGroupsByType(ctx context.Context, userID, groupType string) ([]UserGroupMembership, error) {
	groupReq := &group.GetGroupsRequest{
		Types:      []string{groupType},
		MemberID:   userID,
		MemberType: group.MemberTypeUser,
		Statuses:   []string{group.GroupStatusActive},
		PerPage:    100,
	}

	groupsResp, err := s.GroupService.GetGroups(ctx, groupReq)
	if err != nil {
		return nil, err
	}

	memberships := make([]UserGroupMembership, 0, len(groupsResp.Groups))
	for _, g := range groupsResp.Groups {
		member, err := g.GetMemberByID(userID)

		membership := UserGroupMembership{
			Group: GroupSummary{
				ID:          g.ID,
				NanoID:      g.NanoID,
				Name:        g.Name,
				Type:        g.Type,
				Description: g.DisplayInfo.Description,
				MemberCount: g.GetMemberCount(),
				Status:      g.Status,
				CreatedAt:   g.Metadata.CreatedAt,
			},
			IsMember: true,
		}

		if err == nil {
			membership.Role = member.Role
			membership.JoinedAt = member.JoinedAt
		}

		if g.Leadership != nil {
			membership.IsOwner = g.Leadership.OwnerID == userID
			membership.IsLead = g.Leadership.LeadID == userID
			membership.IsHead = g.Leadership.HeadID == userID
		}

		memberships = append(memberships, membership)
	}

	return memberships, nil
}

// getUserAllGroups fetches all groups for a user
func (s *Service) getUserAllGroups(ctx context.Context, userID string) ([]UserGroupMembership, error) {
	groupReq := &group.GetGroupsRequest{
		MemberID:   userID,
		MemberType: group.MemberTypeUser,
		Statuses:   []string{group.GroupStatusActive},
		PerPage:    100,
	}

	groupsResp, err := s.GroupService.GetGroups(ctx, groupReq)
	if err != nil {
		return nil, err
	}

	memberships := make([]UserGroupMembership, 0, len(groupsResp.Groups))
	for _, g := range groupsResp.Groups {
		member, err := g.GetMemberByID(userID)

		membership := UserGroupMembership{
			Group: GroupSummary{
				ID:          g.ID,
				NanoID:      g.NanoID,
				Name:        g.Name,
				Type:        g.Type,
				Description: g.DisplayInfo.Description,
				MemberCount: g.GetMemberCount(),
				Status:      g.Status,
				CreatedAt:   g.Metadata.CreatedAt,
			},
			IsMember: true,
		}

		if err == nil {
			membership.Role = member.Role
			membership.JoinedAt = member.JoinedAt
		}

		if g.Leadership != nil {
			membership.IsOwner = g.Leadership.OwnerID == userID
			membership.IsLead = g.Leadership.LeadID == userID
			membership.IsHead = g.Leadership.HeadID == userID
		}

		memberships = append(memberships, membership)
	}

	return memberships, nil
}

// isRequesterAdmin safely checks if the requester has admin privileges
func (s *Service) isRequesterAdmin(ctx context.Context, userID string, log *zap.Logger) bool {
	if s.UserService == nil {
		return false
	}

	userResp, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: userID})
	if err != nil || userResp == nil || userResp.User == nil {
		log.Warn("unable-to-resolve-requester-for-admin-check", zap.String("user-id", userID), zap.Error(err))
		return false
	}

	return userResp.User.IsAdmin()
}

// userHasGroupAccess verifies membership or leadership (or admin override)
func (s *Service) userHasGroupAccess(userID string, grp *group.UniversalGroup, isAdmin bool) bool {
	if grp == nil {
		return false
	}

	if isAdmin {
		return true
	}

	if grp.HasMember(userID) {
		return true
	}

	if grp.Leadership != nil {
		if grp.Leadership.OwnerID == userID || grp.Leadership.HeadID == userID || grp.Leadership.LeadID == userID {
			return true
		}

		for _, adminID := range grp.Leadership.AdminIDs {
			if adminID == userID {
				return true
			}
		}

		for _, moderatorID := range grp.Leadership.ModeratorIDs {
			if moderatorID == userID {
				return true
			}
		}
	}

	return false
}

// calculateGroupSeatUsage computes total unique users including nested group members and leadership
func (s *Service) calculateGroupSeatUsage(ctx context.Context, grp *group.UniversalGroup, log *zap.Logger) (int, map[string]int) {
	seenGroups := make(map[string]struct{})
	seenUsers := make(map[string]struct{})
	roleBreakdown := make(map[string]int)

	var walk func(*group.UniversalGroup)
	walk = func(g *group.UniversalGroup) {
		if g == nil {
			return
		}

		if _, exists := seenGroups[g.ID]; exists {
			return
		}
		seenGroups[g.ID] = struct{}{}

		for _, member := range g.Members {
			switch member.Type {
			case group.MemberTypeUser:
				if _, exists := seenUsers[member.ID]; !exists {
					seenUsers[member.ID] = struct{}{}
				}
				if member.Role != "" {
					roleKey := strings.ToUpper(member.Role)
					roleBreakdown[roleKey]++
				}
			case group.MemberTypeGroup:
				childResp, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: member.ID})
				if err != nil || childResp == nil || childResp.Group == nil {
					log.Warn("failed-to-get-nested-group", zap.String("child-group-id", member.ID), zap.Error(err))
					continue
				}
				walk(childResp.Group)
			}
		}

		if g.Leadership != nil {
			leadRoles := []struct {
				id   string
				role string
			}{
				{id: g.Leadership.OwnerID, role: "OWNER"},
				{id: g.Leadership.HeadID, role: "HEAD"},
				{id: g.Leadership.LeadID, role: "LEAD"},
			}

			for _, lead := range leadRoles {
				if lead.id == "" {
					continue
				}

				if _, exists := seenUsers[lead.id]; !exists {
					seenUsers[lead.id] = struct{}{}
				}
				roleBreakdown[lead.role]++
			}

			for _, adminID := range g.Leadership.AdminIDs {
				if adminID == "" {
					continue
				}
				if _, exists := seenUsers[adminID]; !exists {
					seenUsers[adminID] = struct{}{}
				}
				roleBreakdown["ADMIN"]++
			}

			for _, moderatorID := range g.Leadership.ModeratorIDs {
				if moderatorID == "" {
					continue
				}
				if _, exists := seenUsers[moderatorID]; !exists {
					seenUsers[moderatorID] = struct{}{}
				}
				roleBreakdown["MODERATOR"]++
			}
		}
	}

	walk(grp)

	return len(seenUsers), roleBreakdown
}
