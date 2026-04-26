// Package group implements a universal group management system supporting
// teams, organizations, projects, and other hierarchical groupings.
//
// Groups can contain members (users or other groups), track ownership,
// and support custom extensions for domain-specific requirements. The package
// provides comprehensive CRUD operations, member management, and status lifecycle
// management.
package group

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

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
	GetGroupByNameAndParent(ctx context.Context, name, parentGroupID string, logError bool) (*UniversalGroup, error)
	GetGroupsByLineageAncestor(ctx context.Context, ancestorGroupID string) ([]UniversalGroup, error)
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
	HasGroupDependents(ctx context.Context, groupID string) (bool, error)
	AddMemberToGroup(ctx context.Context, groupID string, member Member) error
	RemoveMemberFromGroup(ctx context.Context, groupID, memberID string) error
	BulkUpdateGroupsStatus(ctx context.Context, groupIDs []string, status string) error
	GetGroupsStatsCounts(ctx context.Context) (*AllGroupsStats, error)
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
) (*Service, error) {
	if config == nil {
		config = DefaultGroupConfig()
	}

	if len(config.Tree) > 0 {
		if err := config.ValidateHierarchy(); err != nil {
			return nil, errors.New(ErrKeyInvalidGroupHierarchyTree)
		}
	}

	if config.DefaultRoles == nil {
		config.DefaultRoles = DefaultRoles
	}

	if config.TypeToRoleOverrides == nil || len(config.TypeToRoleOverrides) == 0 {
		config.TypeToRoleOverrides = map[string][]string{}
	}

	return &Service{
		GroupRepository: groupRepository,
		AuditService:    auditService,
		Config:          config,
		IDGenerator:     idGenerator,
		TimeProvider:    timeProvider,
		StringUtils:     stringUtils,
	}, nil
}

// validateHierarchyTreeConfig validates the configured group hierarchy tree
// when hierarchy rules are explicitly defined on the service config.
// It returns ErrKeyInvalidGroupHierarchyTree when validation fails.
func (s *Service) validateHierarchyTreeConfig(log *zap.Logger) error {
	if s.Config == nil || len(s.Config.Tree) == 0 {
		return nil
	}

	if err := s.Config.ValidateHierarchy(); err != nil {
		log.Error("group-hierarchy-tree-validation-failed", zap.Error(err))
		return errors.New(ErrKeyInvalidGroupHierarchyTree)
	}

	return nil
}

// CreateGroup creates a new group
func (s *Service) CreateGroup(ctx context.Context, req *CreateGroupRequest) (*CreateGroupResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "create-group")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("creating-group", zap.String("name", req.Name), zap.String("type", req.Type))

	if err := s.validateHierarchyTreeConfig(log); err != nil {
		return nil, err
	}

	var parentGroup *UniversalGroup
	if req.ParentGroupID != "" {
		parentGroupResult, parentErr := s.GroupRepository.GetGroupByID(ctx, req.ParentGroupID)
		if parentErr != nil {
			log.Error("failed-to-get-parent-group", zap.Error(parentErr), zap.String("parent_group_id", req.ParentGroupID))
			return nil, parentErr
		}
		parentGroup = parentGroupResult
	}

	nameValidation, err := s.ValidateGroupName(ctx, &ValidateGroupNameRequest{
		Name:          req.Name,
		Type:          req.Type,
		ParentGroupID: req.ParentGroupID,
	})
	if err != nil {
		log.Error("failed-to-validate-group-name", zap.Error(err), zap.String("name", req.Name))
		return nil, err
	}
	if !nameValidation.Available && nameValidation.IsRootType {
		log.Debug("group-with-name-already-exists", zap.String("name", nameValidation.Name))
		return nil, errors.New(ErrKeyNameAlreadyExists)
	}

	// Create new group with dependencies
	group := NewUniversalGroup(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Set basic fields
	group.RawName = nameValidation.RawName
	group.Name = nameValidation.Name
	group.Type = req.Type

	if parentGroup != nil {
		group.ParentGroupID = parentGroup.ID
		group.Lineage = parentGroup.BuildChildLineage()
	}

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

	// Set owner
	if req.OwnerID != "" {
		group.OwnerID = req.OwnerID
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

	if parentGroup != nil {
		if !s.Config.CanHaveChildType(parentGroup.Type, group.Type) {
			log.Error(
				"invalid-parent-child-group-relation",
				zap.String("parent_group_id", parentGroup.ID),
				zap.String("parent_group_type", parentGroup.Type),
				zap.String("child_group_type", group.Type),
			)
			return nil, errors.New(ErrKeyInvalidParentChildRelation)
		}
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

// GetGroupLineage retrieves the root-first lineage for a group, including the group itself.
func (s *Service) GetGroupLineage(ctx context.Context, req *GetGroupLineageRequest) (*GetGroupLineageResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-group-lineage")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug("getting-group-lineage", zap.String("id", req.ID))

	group, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-group-for-lineage", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	lineageIDs := append([]string{}, group.Lineage...)
	lineageIDs = append(lineageIDs, group.ID)

	lineage := make([]GroupLineageNode, 0, len(lineageIDs))
	for _, groupID := range lineageIDs {
		lineageGroup, lineageErr := s.GroupRepository.GetGroupByID(ctx, groupID)
		if lineageErr != nil {
			log.Warn("failed-to-get-lineage-group", zap.Error(lineageErr), zap.String("lineage_group_id", groupID))
			continue
		}

		lineage = append(lineage, GroupLineageNode{
			ID:            lineageGroup.ID,
			ParentGroupID: lineageGroup.ParentGroupID,
			Name:          lineageGroup.Name,
			RawName:       lineageGroup.RawName,
			Type:          lineageGroup.Type,
		})
	}

	return &GetGroupLineageResponse{Lineage: lineage}, nil
}

// GetGroupDescendants retrieves all descendants for a group grouped by depth level.
// Index 0 contains direct children, index 1 grandchildren, and so on.
func (s *Service) GetGroupDescendants(ctx context.Context, req *GetGroupDescendantsRequest) (*GetGroupDescendantsResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-group-descendants")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Debug(
		"getting-group-descendants",
		zap.String("id", req.ID),
		zap.Int("max_depth", req.MaxDepth),
		zap.Bool("include_self", req.IncludeSelf),
	)

	if req.MaxDepth < 0 {
		return nil, errors.New(ErrKeyInvalidQueryParam)
	}

	targetGroup, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-group-for-descendants", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	descendantGroups, err := s.GroupRepository.GetGroupsByLineageAncestor(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-descendant-groups", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	levelsMap := map[int][]GroupDescendantsNode{}
	maxLevel := -1

	if req.IncludeSelf {
		levelsMap[0] = append(levelsMap[0], GroupDescendantsNode{
			ID:            targetGroup.ID,
			ParentGroupID: targetGroup.ParentGroupID,
			Name:          targetGroup.Name,
			RawName:       targetGroup.RawName,
			Type:          targetGroup.Type,
		})
		maxLevel = 0
	}

	for _, group := range descendantGroups {
		ancestorIndex := -1
		for i, lineageID := range group.Lineage {
			if lineageID == req.ID {
				ancestorIndex = i
				break
			}
		}

		if ancestorIndex == -1 {
			continue
		}

		depth := len(group.Lineage) - ancestorIndex
		if req.MaxDepth > 0 && depth > req.MaxDepth {
			continue
		}

		levelsMap[depth] = append(levelsMap[depth], GroupDescendantsNode{
			ID:            group.ID,
			ParentGroupID: group.ParentGroupID,
			Name:          group.Name,
			RawName:       group.RawName,
			Type:          group.Type,
		})

		if depth > maxLevel {
			maxLevel = depth
		}
	}

	if maxLevel < 0 {
		return &GetGroupDescendantsResponse{Descendants: [][]GroupDescendantsNode{}}, nil
	}

	startLevel := 1
	if req.IncludeSelf {
		startLevel = 0
	}

	responseLevels := make([][]GroupDescendantsNode, 0, maxLevel-startLevel+1)
	for level := startLevel; level <= maxLevel; level++ {
		nodes := levelsMap[level]
		sort.Slice(nodes, func(i, j int) bool {
			leftRaw := strings.TrimSpace(nodes[i].RawName)
			rightRaw := strings.TrimSpace(nodes[j].RawName)
			if leftRaw == "" {
				leftRaw = nodes[i].Name
			}
			if rightRaw == "" {
				rightRaw = nodes[j].Name
			}

			if leftRaw != rightRaw {
				return leftRaw < rightRaw
			}

			return nodes[i].ID < nodes[j].ID
		})

		if len(nodes) > 0 {
			responseLevels = append(responseLevels, nodes)
		}
	}

	return &GetGroupDescendantsResponse{Descendants: responseLevels}, nil
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

	if err := s.validateHierarchyTreeConfig(log); err != nil {
		return nil, err
	}

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

	// Block deletion when other groups depend on this group.
	hasDependents, err := s.GroupRepository.HasGroupDependents(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-check-group-dependencies", zap.Error(err), zap.String("id", req.ID))
		return nil, errors.New(ErrKeyDatabaseError)
	}
	if hasDependents {
		log.Warn("group-deletion-blocked-due-to-dependencies", zap.String("id", req.ID))
		return nil, errors.New(ErrKeyGroupDependedOnByOtherGroups)
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

	// Validate role against group type configuration
	if err := s.isValidMemberRole(group.Type, req.Role); err != nil {
		log.Error("invalid-member-role", zap.Error(err), zap.String("role", req.Role), zap.String("group_type", group.Type))
		return nil, err
	}

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
func (s *Service) UpdateMemberRole(ctx context.Context, req *UpdateMemberRoleRequest) (*UpdateMemberRoleResponse, error) {
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

	// Validate new role against group type configuration
	if err := s.isValidMemberRole(group.Type, req.NewRole); err != nil {
		log.Error("invalid-member-role", zap.Error(err), zap.String("new_role", req.NewRole), zap.String("group_type", group.Type))
		return nil, err
	}

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

	return &UpdateMemberRoleResponse{Group: updatedGroup}, nil
}

// isValidMemberRole validates if a role is allowed for a given group type
// Returns error with ErrKeyInvalidMemberRole if role is not allowed
func (s *Service) isValidMemberRole(groupType, role string) error {
	cfg := s.Config
	if cfg == nil {
		cfg = DefaultGroupConfig()
	}

	// If TypeToRoleOverrides exists for this group type, validate against it
	if allowedRoles, exists := cfg.TypeToRoleOverrides[groupType]; exists {
		for _, allowedRole := range allowedRoles {
			if allowedRole == role {
				return nil // Role is valid
			}
		}
		return errors.New(ErrKeyInvalidMemberRole)
	}

	// If no override for group type, check DefaultRoles
	if len(cfg.DefaultRoles) > 0 {
		for _, defaultRole := range cfg.DefaultRoles {
			if defaultRole == role {
				return nil // Role is valid
			}
		}
		return errors.New(ErrKeyInvalidMemberRole)
	}

	// If no default roles defined, allow any role (backward compatibility)
	return nil
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
func (s *Service) UpdateLeadership(ctx context.Context, req *UpdateLeadershipRequest) (*UpdateLeadershipResponse, error) {
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

	// Update owner field
	hasChanges := false
	if req.OwnerID != nil && *req.OwnerID != group.OwnerID {
		group.OwnerID = *req.OwnerID
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

	return &UpdateLeadershipResponse{Group: updatedGroup}, nil
}

// ArchiveGroup archives a group
func (s *Service) ArchiveGroup(ctx context.Context, req *ArchiveGroupRequest) (*ArchiveGroupResponse, error) {
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

	return &ArchiveGroupResponse{Group: updatedGroup}, nil
}

// RestoreGroup restores an archived group
func (s *Service) RestoreGroup(ctx context.Context, req *RestoreGroupRequest) (*RestoreGroupResponse, error) {
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

	return &RestoreGroupResponse{Group: updatedGroup}, nil
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
	log.Debug("getting-groups-by-leader-id", zap.String("owner_id", req.OwnerID))

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

	leaderID := req.OwnerID

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

// GetGroupsStats retrieves aggregated statistics across all groups.
func (s *Service) GetGroupsStats(ctx context.Context, _ *GetGroupsStatsRequest) (*GetGroupsStatsResponse, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("method", "get-groups-stats")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	stats, err := s.GroupRepository.GetGroupsStatsCounts(ctx)
	if err != nil {
		log.Error("failed-to-get-groups-stats-counts", zap.Error(err))
		return nil, errors.New(ErrKeyDatabaseError)
	}

	response := &GetGroupsStatsResponse{AllGroupsStats: stats}
	response.NormaliseGroupsStatsKeysToSnakeCase()

	return response, nil
}

// ValidateGroupName returns what RawName and Name would be for a given input
// without persisting anything.  It mirrors the name-resolution logic inside
// CreateGroup so that front-end forms can show live feedback to the user.
func (s *Service) ValidateGroupName(ctx context.Context, req *ValidateGroupNameRequest) (*ValidateGroupNameResponse, error) {
	trimmedRawName := strings.TrimSpace(req.Name)
	trimmedParentGroupID := strings.TrimSpace(req.ParentGroupID)
	baseName, err := toolbox.StringConvertToKebabCase(trimmedRawName)
	if err != nil {
		return nil, errors.New(ErrKeyValidationFailed)
	}

	// Determine whether this type is a hierarchy root.
	independentRoots := s.Config.GetIndependentHierarchyTrees()
	isRootType := false
	for _, root := range independentRoots {
		if root == req.Type {
			isRootType = true
			break
		}
	}

	// Check if the base name is already taken. If parent is provided, enforce
	// uniqueness among siblings (same parent) regardless of child type.
	var existing *UniversalGroup
	if trimmedParentGroupID != "" {
		existing, _ = s.GroupRepository.GetGroupByNameAndParent(ctx, baseName, trimmedParentGroupID, false)
	} else {
		existing, _ = s.GroupRepository.GetGroupByName(ctx, baseName, req.Type, false)
	}
	available := existing == nil

	resolvedName := baseName
	adjusted := false

	if !available {
		if isRootType && trimmedParentGroupID == "" {
			// Root types require a unique name — report the conflict.
			return &ValidateGroupNameResponse{
				RawName:    trimmedRawName,
				Name:       baseName,
				Adjusted:   false,
				Available:  false,
				IsRootType: true,
				Hint:       fmt.Sprintf("The name %q is already taken. Root-type groups must have a unique name.", baseName),
			}, nil
		}

		// Non-root: find the next available auto-incremented name.
		for i := 1; ; i++ {
			candidate := fmt.Sprintf("%s-%d", baseName, i)

			var ex *UniversalGroup
			if trimmedParentGroupID != "" {
				ex, _ = s.GroupRepository.GetGroupByNameAndParent(ctx, candidate, trimmedParentGroupID, false)
			} else {
				ex, _ = s.GroupRepository.GetGroupByName(ctx, candidate, req.Type, false)
			}
			if ex == nil {
				resolvedName = candidate
				adjusted = true
				break
			}
		}
	}

	hint := ""
	if adjusted {
		hint = fmt.Sprintf("The name %q is already taken. Your group will be created as %q", baseName, resolvedName)
	} else if isRootType {
		hint = fmt.Sprintf("%s will be the internal reference name, which has been adjusted to comply with our naming rules", resolvedName)
	}

	return &ValidateGroupNameResponse{
		RawName:    trimmedRawName,
		Name:       resolvedName,
		Adjusted:   adjusted,
		Available:  available,
		IsRootType: isRootType,
		Hint:       hint,
	}, nil
}

// GetGroupsConfig returns the capabilities of the service's single group config.
func (s *Service) GetGroupsConfig(_ context.Context, _ *GetGroupsConfigRequest) (*GetGroupsConfigResponse, error) {
	cfg := s.Config
	if cfg == nil {
		cfg = DefaultGroupConfig()
	}

	return &GetGroupsConfigResponse{
		Config: &GroupConfigCapabilities{
			DefaultStatus:       cfg.DefaultStatus,
			StatusTransitions:   cfg.StatusTransitions,
			ValidTypes:          cfg.ValidTypes,
			ValidMemberTypes:    cfg.ValidMemberTypes,
			Tree:                cfg.Tree,
			AllowNestedGroups:   cfg.AllowNestedGroups,
			MaxNestingDepth:     cfg.MaxNestingDepth,
			RequiredFields:      cfg.RequiredFields,
			MultipleIdentifiers: cfg.MultipleIdentifiers,
			TypeToRoleOverrides: cfg.TypeToRoleOverrides,
			DefaultRoles:        cfg.DefaultRoles,
		},
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
