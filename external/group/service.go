// Package group implements a universal group management system supporting
// teams, organizations, projects, and other hierarchical groupings.
//
// Groups can contain members (users or other groups), have leadership structures,
// and support custom extensions for domain-specific requirements. The package
// provides comprehensive CRUD operations, member management, and status lifecycle
// management.
package group

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// AuditService defines the interface for logging audit events.
// Implementations should ensure audit logs are persisted reliably.
type AuditService interface {
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
}

// GroupRepository defines the interface for group data persistence.
// Implementations handle storage and retrieval of group data with support
// for filtering, pagination, and complex queries.
type GroupRepository interface {
	CreateGroup(ctx context.Context, group *UniversalGroup) (*UniversalGroup, error)
	GetGroupByID(ctx context.Context, id string) (*UniversalGroup, error)
	GetGroupByNanoID(ctx context.Context, nanoID string) (*UniversalGroup, error)
	GetGroupByName(ctx context.Context, name, groupType string, logError bool) (*UniversalGroup, error)
	UpdateGroup(ctx context.Context, group *UniversalGroup) (*UniversalGroup, error)
	DeleteGroupByID(ctx context.Context, id string) error
	SoftDeleteGroup(ctx context.Context, id, deletedByID string, deletedAt string) error
	GetGroups(ctx context.Context, req *GetGroupsRequest) ([]UniversalGroup, error)
	GetTotalGroups(ctx context.Context, req *GetGroupsRequest) (int64, error)
	GetGroupsByType(ctx context.Context, groupType string, page, pageSize int, order string) ([]UniversalGroup, error)
	GetGroupsByStatus(ctx context.Context, status string, page, pageSize int, order string) ([]UniversalGroup, error)
	GetGroupsByMemberID(ctx context.Context, memberID string, memberType string, page, pageSize int) ([]UniversalGroup, error)
	GetGroupsByLeaderID(ctx context.Context, leaderID string, page, pageSize int) ([]UniversalGroup, error)
	SearchGroupsByExtension(ctx context.Context, key string, value interface{}, page, pageSize int) ([]UniversalGroup, error)
	AddMemberToGroup(ctx context.Context, groupID string, member Member) error
	RemoveMemberFromGroup(ctx context.Context, groupID, memberID string) error
	BulkUpdateGroupsStatus(ctx context.Context, groupIDs []string, status string) error
}

// Service manages group business logic and orchestrates group operations.
//
// The service handles validation, audit logging, and coordination between
// the repository layer and application logic.
type Service struct {
	GroupRepository GroupRepository
	AuditService    AuditService
	Config          *GroupConfig
	IDGenerator     IDGenerator
	TimeProvider    TimeProvider
	StringUtils     StringUtils
}

// NewService creates a new group service
func NewService(
	groupRepository GroupRepository,
	auditService AuditService,
	config *GroupConfig,
	idGenerator IDGenerator,
	timeProvider TimeProvider,
	stringUtils StringUtils,
) *Service {
	if config == nil {
		config = DefaultGroupConfig()
	}

	return &Service{
		GroupRepository: groupRepository,
		AuditService:    auditService,
		Config:          config,
		IDGenerator:     idGenerator,
		TimeProvider:    timeProvider,
		StringUtils:     stringUtils,
	}
}

// CreateGroup creates a new group
func (s *Service) CreateGroup(ctx context.Context, req *CreateGroupRequest) (*CreateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "create-group")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("creating-group", zap.String("name", req.Name), zap.String("type", req.Type))

	// Check if group with same name already exists
	existingGroup, err := s.GroupRepository.GetGroupByName(ctx, req.Name, req.Type, false)
	if err == nil && existingGroup != nil {
		log.Debug("group-with-name-already-exists", zap.String("name", req.Name))
		return nil, errors.New(ErrKeyNameAlreadyExists)
	}

	// Create new group with dependencies
	group := NewUniversalGroup(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Set basic fields
	group.Name = req.Name
	group.Type = req.Type

	// Set display info if provided
	if req.Description != "" || req.Email != "" || req.Icon != "" {
		group.DisplayInfo.Description = req.Description
		group.DisplayInfo.Email = req.Email
		group.DisplayInfo.Icon = req.Icon
	}

	// Set visibility
	if req.Visibility != "" {
		group.Settings.Visibility = req.Visibility
	} else {
		group.Settings.Visibility = VisibilityPrivate // Default
	}

	// Set extensions
	if len(req.Extensions) > 0 {
		group.Extensions = req.Extensions
	}

	// Set leadership
	if req.OwnerID != "" || req.HeadID != "" || req.LeadID != "" {
		group.Leadership.OwnerID = req.OwnerID
		group.Leadership.HeadID = req.HeadID
		group.Leadership.LeadID = req.LeadID
	}

	// Generate IDs
	group.GenerateNewUUID()
	if s.Config.MultipleIdentifiers {
		group.GenerateNewNanoID()
	}

	// Set initial timestamps and state
	group.SetInitialState()

	// Add initial members if provided
	for _, memberReq := range req.InitialMembers {
		if _, err := group.AddMember(memberReq.ID, memberReq.Type, memberReq.Role); err != nil {
			log.Error("failed-to-add-initial-member", zap.Error(err), zap.String("member_id", memberReq.ID))
			return nil, err
		}
	}

	// Validate group
	if err := group.Validate(); err != nil {
		log.Error("group-validation-failed", zap.Error(err))
		return nil, err
	}

	// Create group in repository
	createdGroup, err := s.GroupRepository.CreateGroup(ctx, group)
	if err != nil {
		log.Error("failed-to-create-group-in-repository", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:   "group.created",
			TargetId: createdGroup.ID,
			Details: map[string]interface{}{
				"group_name": createdGroup.Name,
				"group_type": createdGroup.Type,
			},
		})
	}

	return &CreateGroupResponse{Group: createdGroup}, nil
}

// GetGroupByID retrieves a group by ID
func (s *Service) GetGroupByID(ctx context.Context, req *GetGroupByIDRequest) (*GetGroupByIDResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-group-by-id")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("getting-group-by-id", zap.String("id", req.ID))

	group, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-group-by-id", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	return &GetGroupByIDResponse{Group: group}, nil
}

// GetGroupByName retrieves a group by its name
func (s *Service) GetGroupByNanoID(ctx context.Context, req *GetGroupByNanoIDRequest) (*GetGroupByNanoIDResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-group-by-nano-id")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("getting-group-by-nano-id", zap.String("nano_id", req.NanoID))

	group, err := s.GroupRepository.GetGroupByNanoID(ctx, req.NanoID)
	if err != nil {
		log.Error("failed-to-get-group-by-nano-id", zap.Error(err), zap.String("nano_id", req.NanoID))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	return &GetGroupByNanoIDResponse{Group: group}, nil
}

// GetGroupByName retrieves a group by name and optional type
func (s *Service) GetGroupByName(ctx context.Context, req *GetGroupByNameRequest) (*GetGroupByNameResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-group-by-name")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("getting-group-by-name", zap.String("name", req.Name), zap.String("type", req.Type))

	group, err := s.GroupRepository.GetGroupByName(ctx, req.Name, req.Type, true)
	if err != nil {
		log.Error("failed-to-get-group-by-name", zap.Error(err), zap.String("name", req.Name))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	return &GetGroupByNameResponse{Group: group}, nil
}

// UpdateGroup updates an existing group
func (s *Service) UpdateGroup(ctx context.Context, req *UpdateGroupRequest) (*UpdateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "update-group")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("updating-group", zap.String("id", req.ID))

	targetGroupID := req.ID
	if req.Group != nil && req.Group.ID != "" {
		targetGroupID = req.Group.ID
	}

	// Get existing group
	group, err := s.GroupRepository.GetGroupByID(ctx, targetGroupID)
	if err != nil {
		log.Error("failed-to-get-group-for-update", zap.Error(err), zap.String("id", targetGroupID))
		return nil, err
	}

	if req.Group != nil {
		groupWithProvidedData := req.Group

		groupWithProvidedData.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

		if groupWithProvidedData.Name != "" && groupWithProvidedData.Name != group.Name {
			// Check if new name already exists
			existingGroup, _ := s.GroupRepository.GetGroupByName(ctx, groupWithProvidedData.Name, groupWithProvidedData.Type, false)
			if existingGroup != nil && existingGroup.ID != group.ID {
				return nil, errors.New(ErrKeyNameAlreadyExists)
			}
			group.Name = groupWithProvidedData.Name
		}

		group = groupWithProvidedData
	}

	if req.Group == nil {
		group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

		// Track if any changes were made
		hasChanges := false

		// Update fields if provided
		if req.Name != nil && *req.Name != group.Name {
			group.Name = *req.Name
			hasChanges = true
		}

		if req.Description != nil && *req.Description != group.DisplayInfo.Description {
			group.DisplayInfo.Description = *req.Description
			hasChanges = true
		}

		if req.Email != nil && *req.Email != group.DisplayInfo.Email {
			group.DisplayInfo.Email = *req.Email
			hasChanges = true
		}

		if req.Icon != nil && *req.Icon != group.DisplayInfo.Icon {
			group.DisplayInfo.Icon = *req.Icon
			hasChanges = true
		}

		if req.Visibility != nil && *req.Visibility != group.Settings.Visibility {
			group.Settings.Visibility = *req.Visibility
			hasChanges = true
		}

		if req.Status != nil && *req.Status != group.Status {
			_, err := group.UpdateStatus(*req.Status)
			if err != nil {
				log.Error("failed-to-update-group-status", zap.Error(err))
				return nil, err
			}
			hasChanges = true
		}

		// Update extensions
		if len(req.Extensions) > 0 {
			for key, value := range req.Extensions {
				group.SetExtension(key, value)
			}
			hasChanges = true
		}

		if !hasChanges {
			log.Debug("no-changes-detected-for-group-update", zap.String("id", req.ID))
			return &UpdateGroupResponse{Group: group}, nil
		}
	}

	// Update timestamps
	group.SetUpdatedAtNow()

	// Validate group
	if err := group.Validate(); err != nil {
		log.Error("group-validation-failed", zap.Error(err))
		return nil, err
	}

	// Ensure version is set to 1
	group.EnsureVersion()

	// Update in repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		log.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:   "group.updated",
			TargetId: updatedGroup.ID,
			Details: map[string]interface{}{
				"group_name": updatedGroup.Name,
			},
		})
	}

	log.Info("group-updated-successfully", zap.String("group-id", updatedGroup.ID))

	return &UpdateGroupResponse{Group: updatedGroup}, nil
}

// DeleteGroup deletes a group
func (s *Service) DeleteGroup(ctx context.Context, req *DeleteGroupRequest) (*DeleteGroupResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "delete-group")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("deleting-group", zap.String("id", req.ID), zap.Bool("hard_delete", req.HardDelete))

	// Verify group exists
	_, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		log.Error("group-not-found-for-deletion", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	if req.HardDelete {
		// Hard delete - permanently remove
		err = s.GroupRepository.DeleteGroupByID(ctx, req.ID)
	} else {
		// Soft delete - mark as deleted
		deletedAt := s.TimeProvider.NowUTC()
		err = s.GroupRepository.SoftDeleteGroup(ctx, req.ID, req.DeletedByID, deletedAt)
	}

	if err != nil {
		log.Error("failed-to-delete-group", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:   "group.deleted",
			TargetId: req.ID,
			Details: map[string]interface{}{
				"hard_delete":   req.HardDelete,
				"deleted_by_id": req.DeletedByID,
			},
		})
	}

	return &DeleteGroupResponse{
		Success: true,
		Message: "Group deleted successfully",
	}, nil
}

// GetGroups retrieves groups with filters and pagination
func (s *Service) GetGroups(ctx context.Context, req *GetGroupsRequest) (*GetGroupsResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-groups")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("getting-groups", zap.Int("page", req.Page), zap.Int("page_size", req.PageSize))

	// Normalize PerPage to PageSize
	if req.PerPage > 0 {
		req.PageSize = req.PerPage
	}

	// Set defaults
	if req.PageSize == 0 {
		req.PageSize = 20
	}
	if req.Page == 0 {
		req.Page = 1
	}

	// Get total count
	totalGroups, err := s.GroupRepository.GetTotalGroups(ctx, req)
	if err != nil {
		log.Error("failed-to-get-total-groups-count", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	req.TotalCount = int(totalGroups)
	log.Debug("handling-get-groups-request-total-groups-found", zap.Int64("total", totalGroups), zap.Any("request", req))

	// Get groups
	groups, err := s.GroupRepository.GetGroups(ctx, req)
	if err != nil {
		log.Error("failed-to-get-groups", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Reinject dependencies for all groups
	groupPointers := make([]*UniversalGroup, len(groups))
	for i := range groups {
		groups[i].SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
		groupPointers[i] = &groups[i]
	}

	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: req.PageSize,
		Page:    req.Page,
	}, groupPointers, req.TotalCount)

	if err != nil {
		return nil, err
	}

	return &GetGroupsResponse{
		Total:      paginatedResponse.Total,
		TotalPages: paginatedResponse.TotalPages,
		Groups:     paginatedResponse.Resources,
		Page:       paginatedResponse.Page,
		PerPage:    paginatedResponse.ResourcePerPage,
	}, nil
}

// AddMember adds a member to a group
func (s *Service) AddMember(ctx context.Context, req *AddMemberRequest) (*AddMemberResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "add-member")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("adding-member-to-group", zap.String("group_id", req.GroupID), zap.String("member_id", req.MemberID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		log.Error("failed-to-get-group", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Add member
	if _, err := group.AddMember(req.MemberID, req.Type, req.Role); err != nil {
		log.Error("failed-to-add-member", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		log.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Get the added member
	addedMember, _ := group.GetMemberByID(req.MemberID)

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:   "group.member.added",
			TargetId: req.GroupID,
			Details: map[string]interface{}{
				"member": addedMember,
			},
		})
	}

	return &AddMemberResponse{
		Group: updatedGroup,
	}, nil
}

// RemoveMember removes a member from a group
func (s *Service) RemoveMember(ctx context.Context, req *RemoveMemberRequest) (*RemoveMemberResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "remove-member")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("removing-member-from-group", zap.String("group_id", req.GroupID), zap.String("member_id", req.MemberID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		log.Error("failed-to-get-group", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Remove member
	if group, err = group.RemoveMember(req.MemberID); err != nil {
		log.Error("failed-to-remove-member", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		log.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:   "group.member.removed",
			TargetId: req.GroupID,
			Details: map[string]interface{}{
				"member": map[string]string{
					"id": req.MemberID,
				},
			},
		})
	}

	return &RemoveMemberResponse{
		Group: updatedGroup,
	}, nil
}

// UpdateMemberRole updates a member's role in a group
func (s *Service) UpdateMemberRole(ctx context.Context, req *UpdateMemberRoleRequest) (*UpdateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "update-member-role")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("updating-member-role", zap.String("group_id", req.GroupID), zap.String("member_id", req.MemberID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		log.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Update role
	if group, err = group.UpdateMemberRole(req.MemberID, req.NewRole); err != nil {
		log.Error("failed-to-update-member-role", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		log.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	memberInfo, _ := group.GetMemberByID(req.MemberID)

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:   "group.member.role_updated",
			TargetId: req.GroupID,
			Details: map[string]interface{}{
				"member": memberInfo,
			},
		})
	}

	return &UpdateGroupResponse{Group: updatedGroup}, nil
}

// GetGroupMembers retrieves members of a group with optional filters
func (s *Service) GetGroupMembers(ctx context.Context, req *GetGroupMembersRequest) (*GetGroupMembersResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-group-members")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("getting-group-members", zap.String("group_id", req.GroupID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		log.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Filter members
	var members []Member
	if req.MemberType != "" {
		members = group.GetMembersByType(req.MemberType)
	} else {
		members = group.Members
	}

	// Further filter by role if specified
	if req.Role != "" {
		filteredMembers := []Member{}
		for _, member := range members {
			if member.Role == req.Role {
				filteredMembers = append(filteredMembers, member)
			}
		}
		members = filteredMembers
	}

	return &GetGroupMembersResponse{
		Members: members,
		Count:   len(members),
	}, nil
}

// UpdateLeadership updates group leadership
func (s *Service) UpdateLeadership(ctx context.Context, req *UpdateLeadershipRequest) (*UpdateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "update-leadership")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("updating-group-leadership", zap.String("group_id", req.GroupID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		log.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Update leadership fields
	hasChanges := false
	if req.OwnerID != nil && *req.OwnerID != group.Leadership.OwnerID {
		group.Leadership.OwnerID = *req.OwnerID
		hasChanges = true
	}
	if req.HeadID != nil && *req.HeadID != group.Leadership.HeadID {
		group.Leadership.HeadID = *req.HeadID
		hasChanges = true
	}
	if req.LeadID != nil && *req.LeadID != group.Leadership.LeadID {
		group.Leadership.LeadID = *req.LeadID
		hasChanges = true
	}

	if !hasChanges {
		return nil, errors.New(ErrKeyNoChangesDetected)
	}

	// Update timestamps
	group.SetUpdatedAtNow()

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		log.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:   "group.leadership.updated",
			TargetId: req.GroupID,
		})
	}

	return &UpdateGroupResponse{Group: updatedGroup}, nil
}

// ArchiveGroup archives a group
func (s *Service) ArchiveGroup(ctx context.Context, req *ArchiveGroupRequest) (*UpdateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "archive-group")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("archiving-group", zap.String("id", req.ID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Update status to archived
	_, err = group.UpdateStatus(GroupStatusArchived)
	if err != nil {
		log.Error("failed-to-archive-group", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		log.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:   "group.archived",
			TargetId: req.ID,
		})
	}

	return &UpdateGroupResponse{Group: updatedGroup}, nil
}

// RestoreGroup restores an archived group
func (s *Service) RestoreGroup(ctx context.Context, req *RestoreGroupRequest) (*UpdateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "restore-group")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("restoring-group", zap.String("id", req.ID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Update status to active
	_, err = group.UpdateStatus(GroupStatusActive)
	if err != nil {
		log.Error("failed-to-restore-group", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		log.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action:   "group.restored",
			TargetId: req.ID,
		})
	}

	return &UpdateGroupResponse{Group: updatedGroup}, nil
}

// GetGroupsByMemberID retrieves groups that contain a specific member
func (s *Service) GetGroupsByMemberID(ctx context.Context, req *GetGroupsRequest) (*GetGroupsResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-groups-by-member-id")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("getting-groups-by-member-id", zap.String("member_id", req.MemberID))

	// Normalize PerPage to PageSize
	if req.PerPage > 0 {
		req.PageSize = req.PerPage
	}

	// Set defaults
	if req.PageSize == 0 {
		req.PageSize = 20
	}
	if req.Page == 0 {
		req.Page = 1
	}

	// Get total count using GetTotalGroups
	totalGroups, err := s.GroupRepository.GetTotalGroups(ctx, req)
	if err != nil {
		log.Error("failed-to-get-total-groups-count-by-member-id", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	req.TotalCount = int(totalGroups)
	log.Debug("handling-get-groups-by-member-id-total-groups-found", zap.Int64("total", totalGroups), zap.Any("request", req))

	groups, err := s.GroupRepository.GetGroupsByMemberID(ctx, req.MemberID, req.MemberType, req.Page, req.PageSize)
	if err != nil {
		log.Error("failed-to-get-groups-by-member-id", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Reinject dependencies
	groupPointers := make([]*UniversalGroup, len(groups))
	for i := range groups {
		groups[i].SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
		groupPointers[i] = &groups[i]
	}

	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: req.PageSize,
		Page:    req.Page,
	}, groupPointers, req.TotalCount)

	if err != nil {
		return nil, err
	}

	return &GetGroupsResponse{
		Total:      paginatedResponse.Total,
		TotalPages: paginatedResponse.TotalPages,
		Groups:     paginatedResponse.Resources,
		Page:       paginatedResponse.Page,
		PerPage:    paginatedResponse.ResourcePerPage,
	}, nil
}

// GetGroupsByLeaderID retrieves groups where user is a leader
func (s *Service) GetGroupsByLeaderID(ctx context.Context, req *GetGroupsRequest) (*GetGroupsResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-groups-by-leader-id")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("getting-groups-by-leader-id", zap.String("owner_id", req.OwnerID), zap.String("head_id", req.HeadID), zap.String("lead_id", req.LeadID))

	// Normalize PerPage to PageSize
	if req.PerPage > 0 {
		req.PageSize = req.PerPage
	}

	// Set defaults
	if req.PageSize == 0 {
		req.PageSize = 20
	}
	if req.Page == 0 {
		req.Page = 1
	}

	// Get total count using GetTotalGroups
	totalGroups, err := s.GroupRepository.GetTotalGroups(ctx, req)
	if err != nil {
		log.Error("failed-to-get-total-groups-count-by-leader-id", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	req.TotalCount = int(totalGroups)
	log.Debug("handling-get-groups-by-leader-id-total-groups-found", zap.Int64("total", totalGroups), zap.Any("request", req))

	// Determine which leader field to use
	leaderID := req.OwnerID
	if leaderID == "" {
		leaderID = req.HeadID
	}
	if leaderID == "" {
		leaderID = req.LeadID
	}

	groups, err := s.GroupRepository.GetGroupsByLeaderID(ctx, leaderID, req.Page, req.PageSize)
	if err != nil {
		log.Error("failed-to-get-groups-by-leader-id", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Reinject dependencies
	groupPointers := make([]*UniversalGroup, len(groups))
	for i := range groups {
		groups[i].SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
		groupPointers[i] = &groups[i]
	}

	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: req.PageSize,
		Page:    req.Page,
	}, groupPointers, req.TotalCount)

	if err != nil {
		return nil, err
	}

	return &GetGroupsResponse{
		Total:      paginatedResponse.Total,
		TotalPages: paginatedResponse.TotalPages,
		Groups:     paginatedResponse.Resources,
		Page:       paginatedResponse.Page,
		PerPage:    paginatedResponse.ResourcePerPage,
	}, nil
}

// GetGroupStats retrieves statistics for a group
func (s *Service) GetGroupStats(ctx context.Context, groupID string) (*GetGroupStatsResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-group-stats")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("getting-group-stats", zap.String("group_id", groupID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, groupID)
	if err != nil {
		log.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Calculate stats
	userCount := len(group.GetUserMemberIDs())
	groupCount := len(group.GetGroupMemberIDs())

	return &GetGroupStatsResponse{
		GroupID:          group.ID,
		MemberCount:      group.GetMemberCount(),
		UserMemberCount:  userCount,
		GroupMemberCount: groupCount,
		SubgroupCount:    groupCount,
	}, nil
}

// SetGroupExtension sets an extension field value
func (s *Service) SetGroupExtension(ctx context.Context, groupID, key string, value interface{}) (*UpdateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "set-group-extension")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("setting-group-extension", zap.String("group_id", groupID), zap.String("key", key))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, groupID)
	if err != nil {
		log.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Set extension
	group.SetExtension(key, value)

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		log.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	return &UpdateGroupResponse{Group: updatedGroup}, nil
}

// SearchGroupsByExtension searches for groups by extension field value
func (s *Service) SearchGroupsByExtension(ctx context.Context, req *GetGroupsRequest) (*GetGroupsResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "search-groups-by-extension")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("searching-groups-by-extension", zap.Any("extension_filters", req.ExtensionFilters))

	// Normalize PerPage to PageSize
	if req.PerPage > 0 {
		req.PageSize = req.PerPage
	}

	// Set defaults
	if req.PageSize == 0 {
		req.PageSize = 20
	}
	if req.Page == 0 {
		req.Page = 1
	}

	// Get total count using GetTotalGroups
	totalGroups, err := s.GroupRepository.GetTotalGroups(ctx, req)
	if err != nil {
		log.Error("failed-to-get-total-groups-count-by-extension", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	req.TotalCount = int(totalGroups)
	log.Debug("handling-search-groups-by-extension-total-groups-found", zap.Int64("total", totalGroups), zap.Any("request", req))

	// Extract first extension filter for backward compatibility with repository method
	var key string
	var value interface{}
	for k, v := range req.ExtensionFilters {
		key = k
		value = v
		break
	}

	groups, err := s.GroupRepository.SearchGroupsByExtension(ctx, key, value, req.Page, req.PageSize)
	if err != nil {
		log.Error("failed-to-search-groups-by-extension", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	// Reinject dependencies
	groupPointers := make([]*UniversalGroup, len(groups))
	for i := range groups {
		groups[i].SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
		groupPointers[i] = &groups[i]
	}

	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: req.PageSize,
		Page:    req.Page,
	}, groupPointers, req.TotalCount)

	if err != nil {
		return nil, err
	}

	return &GetGroupsResponse{
		Total:      paginatedResponse.Total,
		TotalPages: paginatedResponse.TotalPages,
		Groups:     paginatedResponse.Resources,
		Page:       paginatedResponse.Page,
		PerPage:    paginatedResponse.ResourcePerPage,
	}, nil
}

// BulkUpdateGroupsStatus updates status for multiple groups
func (s *Service) BulkUpdateGroupsStatus(ctx context.Context, groupIDs []string, status string) error {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "bulk-update-groups-status")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("bulk-updating-group-status", zap.Int("count", len(groupIDs)), zap.String("status", status))

	err := s.GroupRepository.BulkUpdateGroupsStatus(ctx, groupIDs, status)
	if err != nil {
		log.Error("failed-to-bulk-update-groups-status", zap.Error(err))
		return errors.New(ErrKeyDatabaseError)
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			Action: "groups.bulk_status_updated",
			Details: map[string]interface{}{
				"group_ids": groupIDs,
				"status":    status,
			},
		})
	}

	return nil
}
