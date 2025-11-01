package usermanager

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/logger"
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
		HeadID:      r.HeadID,
		LeadID:      r.LeadID,
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
