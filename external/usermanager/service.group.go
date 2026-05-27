package usermanager

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/logger"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

// GetEnrichedUserProfile handles fetching an enriched user profile with group membership data
func (s *Service) GetEnrichedUserProfile(ctx context.Context, r *GetEnrichedUserProfileRequest) (*GetEnrichedUserProfileResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	// Get base user profile
	userResp, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{
		ID: r.UserId,
	})
	if err != nil {
		logger.Error("failed-to-get-user-profile", zap.String("user-id", r.UserId), zap.Error(err))
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
		Groups:      []UserGroupMembership{},
	}

	// Fetch group memberships if requested
	if r.IncludeAllGroups {
		allGroups, err := s.getUserAllGroups(ctx, r.UserId, r.PrefixName)
		if err != nil {
			logger.Warn("failed-to-fetch-all-group-memberships", zap.String("user-id", r.UserId), zap.Error(err))
		} else {
			enrichedProfile.Groups = allGroups
		}
	}

	return &GetEnrichedUserProfileResponse{
		Profile: enrichedProfile,
	}, nil
}

// GetGroupLineage handles fetching the lineage for a group, gated by group access for non-admin requesters.
func (s *Service) GetGroupLineage(ctx context.Context, r *GetGroupLineageRequest) (*GetGroupLineageResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserId, logger)
	if !isAdmin {
		hasGroupAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserId, r.GetGroupLineageRequest.ID)
		if accessErr != nil {
			logger.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester-user-id", r.UserId),
				zap.String("group-id", r.GetGroupLineageRequest.ID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		if !hasGroupAccess.IsAccessible {
			return nil, ErrGroupNotFound
		}
	}

	r.GetGroupLineageRequest.AsUserID = r.UserId

	resp, err := s.GroupService.GetGroupLineage(ctx, r.GetGroupLineageRequest)
	if err != nil {
		logger.Error("failed-to-get-group-lineage", zap.String("group-id", r.GetGroupLineageRequest.ID), zap.Error(err))
		return nil, err
	}

	return &GetGroupLineageResponse{
		GetGroupLineageResponse: resp,
	}, nil
}

// GetGroupsByUserID handles fetching groups for a user with filtering
// GetGroupDescendants handles fetching group descendants with permission checks
func (s *Service) GetGroupDescendants(ctx context.Context, r *GetGroupDescendantsRequest) (*GetGroupDescendantsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserId, logger)
	if !isAdmin {
		hasGroupAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserId, r.GetGroupDescendantsRequest.ID)
		if accessErr != nil {
			logger.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester-user-id", r.UserId),
				zap.String("group-id", r.GetGroupDescendantsRequest.ID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		if !hasGroupAccess.IsAccessible {
			return nil, ErrGroupNotFound
		}
	}

	r.GetGroupDescendantsRequest.AsUserID = r.UserId

	resp, err := s.GroupService.GetGroupDescendants(ctx, r.GetGroupDescendantsRequest)
	if err != nil {
		logger.Error("failed-to-get-group-descendants", zap.String("group-id", r.GetGroupDescendantsRequest.ID), zap.Error(err))
		return nil, err
	}

	return &GetGroupDescendantsResponse{
		GetGroupDescendantsResponse: resp,
	}, nil
}

// GetGroupsByUserID handles fetching groups for a user with filtering
func (s *Service) GetGroupsByUserID(ctx context.Context, r *GetGroupsByUserIDRequest) (*GetGroupsByUserIDResponse, error) {
	if s.GroupService == nil {
		return nil, ErrGroupServiceNotEnabled
	}

	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if r.ID != r.UserID {
		isAdmin := s.isRequesterAdmin(ctx, r.ID, logger)
		if !isAdmin {
			return nil, group.ErrInsufficientPermissions
		}
	}

	resp, err := s.GroupService.GetGroupsByUserID(ctx, r.GetGroupsByUserIDRequest)
	if err != nil {
		return nil, err
	}

	return &GetGroupsByUserIDResponse{
		GetGroupsByUserIDResponse: resp,
	}, nil
}

// GetUserGroups handles fetching groups for a user with filtering
func (s *Service) GetUserGroups(ctx context.Context, r *GetUserGroupsRequest) (*GetUserGroupsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	groupReq := &group.GetGroupsRequest{
		MemberID:   r.UserId,
		MemberType: group.MemberTypeUser,
		Page:       r.Page,
		PerPage:    r.PerPage,
		Meta:       r.Meta,
		PrefixName: r.PrefixName,
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
		logger.Error("failed-to-get-user-groups", zap.String("user-id", r.UserId), zap.Error(err))
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

// GetLatestNotificationOverviews fetches latest group notification overviews for the requester.
func (s *Service) GetLatestNotificationOverviews(ctx context.Context, r *GetLatestNotificationOverviewsRequest) (*GetLatestNotificationOverviewsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	targetUserID := strings.TrimSpace(r.UserId)
	if r.GetLatestNotificationOverviewsRequest != nil && strings.TrimSpace(r.GetLatestNotificationOverviewsRequest.UserID) != "" {
		targetUserID = strings.TrimSpace(r.GetLatestNotificationOverviewsRequest.UserID)
	}

	userEmail := strings.TrimSpace(r.UserEmail)
	if userEmail == "" {
		userResponse, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: targetUserID})
		if err != nil {
			logger.Error("failed-to-resolve-user-email-for-notifications", zap.String("user-id", targetUserID), zap.Error(err))
			return nil, err
		}

		userEmail = strings.TrimSpace(userResponse.User.Email)
	}

	overviews, err := s.GroupService.GetLatestNotificationOverviews(ctx, &common.GetLatestNotificationOverviewsRequest{
		UserID:    targetUserID,
		UserEmail: userEmail,
		Kinds:     r.Kinds,
		Limit:     r.Limit,
	})
	if err != nil {
		logger.Error("failed-to-get-group-notification-overviews", zap.String("user-id", targetUserID), zap.Error(err))
		return nil, err
	}

	return &GetLatestNotificationOverviewsResponse{GetLatestNotificationOverviewsResponse: overviews}, nil
}

// GetMyGroupInvitations fetches outstanding group invitations for the requester.
func (s *Service) GetMyGroupInvitations(ctx context.Context, r *GetMyGroupInvitationsRequest) (*GetMyGroupInvitationsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	userResponse, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.UserId})
	if err != nil {
		logger.Error("failed-to-resolve-user-for-group-invitations", zap.String("user-id", r.UserId), zap.Error(err))
		return nil, err
	}

	inviteEmail := strings.TrimSpace(userResponse.User.Email)
	if inviteEmail == "" {
		return &GetMyGroupInvitationsResponse{Invitations: []PendingGroupInvitation{}}, nil
	}

	groupsResponse, err := s.GroupService.GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx, &group.GetGroupsAwaitingAnswerForInvitationsByMemberIDRequest{
		MemberID:   inviteEmail,
		PrefixName: r.PrefixName,
	})
	if err != nil {
		logger.Error("failed-to-get-my-group-invitations", zap.String("user-id", r.UserId), zap.Error(err))
		return nil, err
	}

	invitations := make([]PendingGroupInvitation, 0, len(groupsResponse.Groups))
	for i := range groupsResponse.Groups {
		invitationGroup := groupsResponse.Groups[i]
		member, memberErr := invitationGroup.GetMemberByID(inviteEmail)
		if memberErr != nil {
			continue
		}

		groupVisibility := ""
		if invitationGroup.Settings != nil {
			groupVisibility = invitationGroup.Settings.Visibility
		}

		groupDescription := ""
		if invitationGroup.DisplayInfo != nil {
			groupDescription = invitationGroup.DisplayInfo.Description
		}

		resolvedInviteEmail := inviteEmail
		if metaValue, metaErr := member.GetMemberMeta(group.MemberMetadataKeyInviteEmail); metaErr == nil {
			if metaEmail, ok := metaValue.(string); ok && strings.TrimSpace(metaEmail) != "" {
				resolvedInviteEmail = strings.TrimSpace(metaEmail)
			}
		}

		invitations = append(invitations, PendingGroupInvitation{
			GroupID:          invitationGroup.ID,
			GroupName:        invitationGroup.Name,
			GroupRawName:     invitationGroup.RawName,
			GroupType:        invitationGroup.Type,
			GroupStatus:      invitationGroup.Status,
			GroupVisibility:  groupVisibility,
			GroupDescription: groupDescription,
			InviteEmail:      resolvedInviteEmail,
			Role:             member.Role,
			InvitedAt:        member.InvitedAt,
			InvitationState:  member.InvitationState,
		})
	}

	return &GetMyGroupInvitationsResponse{Invitations: invitations}, nil
}

// AcceptMyGroupInvitation accepts a pending group invitation for the requester.
func (s *Service) AcceptMyGroupInvitation(ctx context.Context, r *AcceptMyGroupInvitationRequest) (*AcceptMyGroupInvitationResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	userResponse, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.UserId})
	if err != nil {
		logger.Error("failed-to-resolve-user-for-accept-group-invitation", zap.String("user-id", r.UserId), zap.Error(err))
		return nil, err
	}

	resp, err := s.GroupService.AcceptInvite(ctx, &group.AcceptInviteRequest{
		GroupID:     r.GroupID,
		InviteEmail: strings.TrimSpace(userResponse.User.Email),
		UserID:      r.UserId,
	})
	if err != nil {
		logger.Error("failed-to-accept-my-group-invitation", zap.String("user-id", r.UserId), zap.String("group-id", r.GroupID), zap.Error(err))
		return nil, err
	}

	return &AcceptMyGroupInvitationResponse{AcceptInviteResponse: resp}, nil
}

// RejectMyGroupInvitation rejects a pending group invitation for the requester.
func (s *Service) RejectMyGroupInvitation(ctx context.Context, r *RejectMyGroupInvitationRequest) (*RejectMyGroupInvitationResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	userResponse, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.UserId})
	if err != nil {
		logger.Error("failed-to-resolve-user-for-reject-group-invitation", zap.String("user-id", r.UserId), zap.Error(err))
		return nil, err
	}

	resp, err := s.GroupService.RejectInvite(ctx, &group.RejectInviteRequest{
		GroupID:      r.GroupID,
		InviteEmail:  strings.TrimSpace(userResponse.User.Email),
		RejectedByID: r.UserId,
	})
	if err != nil {
		logger.Error("failed-to-reject-my-group-invitation", zap.String("user-id", r.UserId), zap.String("group-id", r.GroupID), zap.Error(err))
		return nil, err
	}

	return &RejectMyGroupInvitationResponse{RejectInviteResponse: resp}, nil
}

// GetGroupDetail handles fetching a single group for a requester with membership/owner checks
func (s *Service) GetGroupDetail(ctx context.Context, r *GetGroupDetailRequest) (*GetGroupDetailResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	groupResp, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID, PrefixName: r.PrefixName})
	if err != nil || groupResp == nil || groupResp.Group == nil {
		logger.Warn("failed-to-get-group-detail", zap.String("group-id", r.GroupID), zap.Error(err))
		return nil, ErrGroupNotFound
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserId, logger)
	if !isAdmin {
		hasGroupAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserId, r.GroupID)
		if accessErr != nil {
			logger.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester-user-id", r.UserId),
				zap.String("group-id", r.GroupID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		if !hasGroupAccess.IsAccessible {
			return nil, ErrGroupNotFound
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
	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", r.UserId))
		return nil, ErrGroupServiceNotEnabled
	}

	groupResp, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID, PrefixName: r.PrefixName})
	if err != nil || groupResp == nil || groupResp.Group == nil {
		logger.Warn("failed-to-get-group-for-stats", zap.String("group-id", r.GroupID), zap.Error(err))
		return nil, ErrGroupNotFound
	}

	isAdmin := s.isRequesterAdmin(ctx, r.UserId, logger)
	if !isAdmin {
		hasGroupAccess, accessErr := s.hasRequesterGroupAccess(ctx, r.UserId, r.GroupID)
		if accessErr != nil {
			logger.Error(
				"failed-to-resolve-requester-group-access-map",
				zap.String("requester-user-id", r.UserId),
				zap.String("group-id", r.GroupID),
				zap.Error(accessErr),
			)
			return nil, ErrFailedToResolveGroupAccessMap
		}

		if !hasGroupAccess.IsAccessible {
			return nil, ErrGroupNotFound
		}
	}

	totalUsers, roleBreakdown := s.calculateGroupSeatUsage(ctx, groupResp.Group, logger)

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
		return nil, ErrGroupServiceNotEnabled
	}

	return s.GroupService.GetGroupByNanoID(ctx, r)
}

// GetGroupMembers fetches members for a group
func (s *Service) GetGroupMembers(ctx context.Context, r *group.GetGroupMembersRequest) (*group.GetGroupMembersResponse, error) {
	if s.GroupService == nil {
		return nil, ErrGroupServiceNotEnabled
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
func (s *Service) getUserAllGroups(ctx context.Context, userID string, prefixName bool) ([]UserGroupMembership, error) {

	req := &GetUserGroupMembershipsRequest{
		UserID:             userID,
		GroupType:          "",
		IncludeDescendants: false,
		PrefixName:         prefixName,
	}

	resp, err := s.GetUserGroupMemberships(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.Memberships, nil
}

// GetUserGroupMemberships fetches user-referenced groups and maps them to user-facing memberships.
func (s *Service) GetUserGroupMemberships(ctx context.Context, req *GetUserGroupMembershipsRequest) (*GetUserGroupMembershipsResponse, error) {
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", req.UserID))
		return nil, ErrGroupServiceNotEnabled
	}

	userID, groupType, includeDescendants, prefixName := req.UserID, req.GroupType, req.IncludeDescendants, req.PrefixName

	groupsWithDescendants, err := s.GroupService.GetGroupsByUserID(ctx, &group.GetGroupsByUserIDRequest{
		UserID:             userID,
		IncludeDescendants: includeDescendants,
		PrefixName:         prefixName,
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
			GroupSummary: &GroupSummary{
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
func (s *Service) isRequesterAdmin(ctx context.Context, userID string, logger *zap.Logger) bool {
	if s.UserService == nil {
		return false
	}

	userResp, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: userID})
	if err != nil || userResp == nil || userResp.User == nil {
		logger.Warn("unable-to-resolve-requester-for-admin-check", zap.String("user-id", userID), zap.Error(err))
		return false
	}

	return userResp.User.IsAdmin()
}

// hasRequesterGroupAccess checks if the requester has access to the group either as a member, admin or owner
func (s *Service) hasRequesterGroupAccess(ctx context.Context, userID, groupID string) (*group.UserGroupAccessSummary, error) {

	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/usermanager")

	if s.GroupService == nil {
		logger.Error("group-service-not-enabled", zap.String("user-id", userID))
		return nil, ErrGroupServiceNotEnabled
	}

	accessMap, err := s.GroupService.GetUserGroupAccessMap(ctx, userID)
	if err != nil {
		return nil, err
	}

	groupAccess, exists := accessMap[groupID]
	if !exists {
		return nil, group.ErrInsufficientPermissions
	}

	return &groupAccess, nil
}

// calculateGroupSeatUsage computes total unique users including nested group members and owner
func (s *Service) calculateGroupSeatUsage(ctx context.Context, grp *group.UniversalGroup, logger *zap.Logger) (int, map[string]int) {
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
					logger.Warn("failed-to-get-nested-group", zap.String("child-group-id", member.ID), zap.Error(err))
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
