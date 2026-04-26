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

// CreateGroup handles creating a new group (admin only)
func (s *Service) CreateGroup(ctx context.Context, r *CreateGroupRequest) (*CreateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("admin-user-id", r.AdminUserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	// Build the group creation request
	createReq := &group.CreateGroupRequest{
		Name:        r.Name,
		Type:        r.Type,
		Description: r.Description,
		Email:       r.Email,
		Icon:        r.Icon,
		Visibility:  r.Visibility,
		Extensions:  r.Extensions,
		OwnerID:     r.OwnerID,
	}

	// Add initial members if provided
	if len(r.InitialMemberIDs) > 0 {
		defaultRole := r.InitialMemberRole
		if defaultRole == "" {
			defaultRole = group.MemberRoleMember
		}

		createReq.InitialMembers = make([]group.CreateMemberRequest, len(r.InitialMemberIDs))
		for i, memberID := range r.InitialMemberIDs {
			createReq.InitialMembers[i] = group.CreateMemberRequest{
				ID:   memberID,
				Type: group.MemberTypeUser,
				Role: defaultRole,
			}
		}
	}

	// Create the group via group service
	groupResp, err := s.GroupService.CreateGroup(ctx, createReq)
	if err != nil {
		log.Error("failed-to-create-group", zap.String("name", r.Name), zap.String("type", r.Type), zap.Error(err))
		return nil, err
	}

	// Convert to group summary
	groupSummary := &GroupSummary{
		ID:          groupResp.Group.ID,
		NanoID:      groupResp.Group.NanoID,
		Name:        groupResp.Group.Name,
		Type:        groupResp.Group.Type,
		Description: groupResp.Group.DisplayInfo.Description,
		MemberCount: groupResp.Group.GetMemberCount(),
		Status:      groupResp.Group.Status,
		CreatedAt:   groupResp.Group.Metadata.CreatedAt,
	}

	log.Info("group-created-successfully", zap.String("group-id", groupSummary.ID), zap.String("group-name", groupSummary.Name))

	return &CreateGroupResponse{
		Group: groupSummary,
	}, nil
}

// GetAdminGroupDetail returns an enriched group view for admin use — members and owner
// are resolved to user profiles in a single call, avoiding multiple round-trips from the client.
func (s *Service) GetAdminGroupDetail(ctx context.Context, r *AdminGetGroupDetailRequest) (*GetAdminGroupDetailResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("admin-user-id", r.AdminUserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	groupResp, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID})
	if err != nil || groupResp == nil || groupResp.Group == nil {
		log.Warn("admin-get-group-detail-not-found", zap.String("group-id", r.GroupID), zap.Error(err))
		return nil, errors.New(ErrKeyGroupNotFound)
	}

	g := groupResp.Group

	// Authoritative member source: load members via dedicated endpoint.
	// Some group documents may not include an embedded members array in GetGroupByID.
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

	return &GetAdminGroupDetailResponse{
		Detail: &AdminGroupDetail{
			Group:   g,
			Members: enrichedMembers,
			Owner:   owner,
		},
	}, nil
}

// AdminAddGroupMember adds a user to a group as an admin operation
func (s *Service) AdminAddGroupMember(ctx context.Context, r *AdminAddGroupMemberRequest) (*AdminAddGroupMemberResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("admin-user-id", r.AdminUserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	_, err := s.GroupService.AddMember(ctx, &group.AddMemberRequest{
		GroupID:  r.GroupID,
		MemberID: r.MemberID,
		Type:     group.MemberTypeUser,
		Role:     r.Role,
	})
	if err != nil {
		log.Error("admin-add-group-member-failed",
			zap.String("group-id", r.GroupID),
			zap.String("member-id", r.MemberID),
			zap.Error(err),
		)
		return nil, errors.New(ErrKeyFailedToAddUserToGroup)
	}

	return &AdminAddGroupMemberResponse{Success: true}, nil
}

// AdminRemoveGroupMember removes a user from a group as an admin operation
func (s *Service) AdminRemoveGroupMember(ctx context.Context, r *AdminRemoveGroupMemberRequest) (*AdminRemoveGroupMemberResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("admin-user-id", r.AdminUserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	_, err := s.GroupService.RemoveMember(ctx, &group.RemoveMemberRequest{
		GroupID:  r.GroupID,
		MemberID: r.MemberID,
	})
	if err != nil {
		log.Error("admin-remove-group-member-failed",
			zap.String("group-id", r.GroupID),
			zap.String("member-id", r.MemberID),
			zap.Error(err),
		)
		return nil, errors.New(ErrKeyFailedToRemoveUserFromGroup)
	}

	return &AdminRemoveGroupMemberResponse{Success: true}, nil
}

// AdminUpdateGroupOwner updates the owner of a group as an admin operation
func (s *Service) AdminUpdateGroupOwner(ctx context.Context, r *AdminUpdateGroupOwnerRequest) (*AdminUpdateGroupOwnerResponse, error) {
	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	if s.GroupService == nil {
		log.Error("group-service-not-enabled", zap.String("admin-user-id", r.AdminUserId))
		return nil, errors.New(ErrKeyGroupServiceNotEnabled)
	}

	_, err := s.GroupService.UpdateOwner(ctx, &group.UpdateOwnerRequest{
		GroupID: r.GroupID,
		OwnerID: r.OwnerID,
	})
	if err != nil {
		log.Error("admin-update-group-owner-failed",
			zap.String("group-id", r.GroupID),
			zap.Error(err),
		)
		return nil, errors.New(ErrKeyFailedToUpdateGroupOwner)
	}

	// Re-fetch and resolve the updated owner to return enriched data
	groupResp, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID})
	if err != nil || groupResp == nil || groupResp.Group == nil {
		return &AdminUpdateGroupOwnerResponse{Owner: &EnrichedOwner{}}, nil
	}

	owner := &EnrichedOwner{
		Owner: s.resolveUserToEnrichedMember(ctx, groupResp.Group.OwnerID),
	}

	return &AdminUpdateGroupOwnerResponse{Owner: owner}, nil
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
