package usermanager

import (
	"context"
	"errors"
	"strings"

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

// GetGroupDetail handles fetching a single group for a requester with membership/owner checks
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
	if !isAdmin {
		hasAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserId, r.GroupID)
		if accessErr != nil {
			log.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester_user_id", r.UserId),
				zap.String("group_id", r.GroupID),
				zap.Error(accessErr),
			)
			return nil, errors.New(ErrKeyFailedToResolveGroupAccessMap)
		}

		if !hasAccess {
			return nil, errors.New(ErrKeyGroupNotFound)
		}
	}

	g := groupResp.Group

	// Authoritative member source: load members via dedicated endpoint.
	membersSource := g.Members
	membersResp, membersErr := s.GroupService.GetGroupMembers(ctx, &group.GetGroupMembersRequest{GroupID: r.GroupID})
	if membersErr == nil && membersResp != nil {
		membersSource = membersResp.Members
	}

	// Enrich members — resolve user profile for each USER-type member
	enrichedMembers := make([]EnrichedMember, 0, len(membersSource))
	for _, m := range membersSource {
		em := EnrichedMember{
			ID:       m.ID,
			Type:     m.Type,
			Role:     m.Role,
			JoinedAt: m.JoinedAt,
		}
		if m.Type == group.MemberTypeUser {
			if u := s.resolveUserToEnrichedMember(ctx, m.ID); u != nil {
				em.FullName = u.FullName
				em.Initials = u.Initials
				em.Email = u.Email
				em.Roles = u.Roles
				em.Type = u.Type
			}
		}
		enrichedMembers = append(enrichedMembers, em)
	}

	// Enrich owner details
	var owner *EnrichedOwner
	if g.OwnerID != "" {
		owner = &EnrichedOwner{
			Owner: s.resolveUserToEnrichedMember(ctx, g.OwnerID),
		}
	}

	return &GetGroupDetailResponse{
		Detail: &GroupDetail{
			Group:   g,
			Members: enrichedMembers,
			Owner:   owner,
		},
	}, nil
}

// GetGroupStats handles fetching group stats for a requester with membership/owner checks
func (s *Service) GetGroupStats(ctx context.Context, r *GetGroupStatsRequest) (*GetGroupStatsResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	groupResp, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID})
	if err != nil || groupResp == nil || groupResp.Group == nil {
		log.Warn("failed-to-get-group-for-stats", zap.String("group-id", r.GroupID), zap.Error(err))
		return nil, errors.New(ErrKeyGroupNotFound)
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserId, log)
	if !isAdmin {
		hasAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserId, r.GroupID)
		if accessErr != nil {
			log.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester_user_id", r.UserId),
				zap.String("group_id", r.GroupID),
				zap.Error(accessErr),
			)
			return nil, errors.New(ErrKeyFailedToResolveGroupAccessMap)
		}

		if !hasAccess {
			return nil, errors.New(ErrKeyGroupNotFound)
		}
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

// Helper functions

// getUserGroupsByType fetches groups of a specific type for a user
func (s *Service) getUserGroupsByType(ctx context.Context, userID, groupType string) ([]UserGroupMembership, error) {
	req := &GetUserGroupMembershipsRequest{
		UserID:             userID,
		GroupType:          groupType,
		IncludeDescendants: true,
	}
	resp, err := s.GetUserGroupMemberships(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Memberships, nil
}

// getUserAllGroups fetches all groups for a user
func (s *Service) getUserAllGroups(ctx context.Context, userID string) ([]UserGroupMembership, error) {

	req := &GetUserGroupMembershipsRequest{
		UserID:             userID,
		GroupType:          "",
		IncludeDescendants: false,
	}

	resp, err := s.GetUserGroupMemberships(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.Memberships, nil
}

// GetUserGroupMemberships fetches user-referenced groups and maps them to user-facing memberships.
func (s *Service) GetUserGroupMemberships(ctx context.Context, req *GetUserGroupMembershipsRequest) (*GetUserGroupMembershipsResponse, error) {

	userID, groupType, includeDescendants := req.UserID, req.GroupType, req.IncludeDescendants

	groupsWithDescendants, err := s.GroupService.GetGroupsByUserID(ctx, &group.GetGroupsByUserIDRequest{
		UserID:             userID,
		IncludeDescendants: includeDescendants,
	})
	if err != nil {
		return nil, err
	}

	memberships := make([]UserGroupMembership, 0, len(groupsWithDescendants.Groups))
	for _, g := range groupsWithDescendants.Groups {
		if groupType != "" && g.Type != groupType {
			continue
		}

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

		membership.IsOwner = g.OwnerID == userID

		memberships = append(memberships, membership)
	}

	return &GetUserGroupMembershipsResponse{
		Memberships: memberships,
	}, nil
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

func (s *Service) hasRequesterGroupAccess(ctx context.Context, userID, groupID string) (bool, error) {
	accessMap, err := s.GroupService.GetUserGroupAccessMap(ctx, userID)
	if err != nil {
		return false, err
	}

	groupAccess, exists := accessMap[groupID]
	if !exists {
		return false, nil
	}

	return groupAccess.IsAccessible, nil
}

// calculateGroupSeatUsage computes total unique users including nested group members and owner
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

		if g.OwnerID != "" {
			if _, exists := seenUsers[g.OwnerID]; !exists {
				seenUsers[g.OwnerID] = struct{}{}
			}
			roleBreakdown["OWNER"]++
		}
	}

	walk(grp)

	return len(seenUsers), roleBreakdown
}
