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
	"fmt"
	"sort"
	"strings"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/common"
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
	GetGroupsByReferencedUserID(ctx context.Context, userID string) ([]UniversalGroup, error)
	GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx context.Context, memberID string) ([]UniversalGroup, error)
	GetGroupsByMemberID(ctx context.Context, memberID string, memberType string, page, pageSize int) ([]UniversalGroup, error)
	GetGroupsByLeaderID(ctx context.Context, leaderID string, page, pageSize int) ([]UniversalGroup, error)
	SearchGroupsByExtension(ctx context.Context, key string, value interface{}, page, pageSize int) ([]UniversalGroup, error)
	HasGroupDependents(ctx context.Context, groupID string) (bool, error)
	AddMemberToGroup(ctx context.Context, groupID string, member Member) error
	RemoveMemberFromGroup(ctx context.Context, groupID, memberID string) error
	ClearOwnerFromGroup(ctx context.Context, groupID, ownerID string) error
	GetGroupIDsWithInvalidMembers(ctx context.Context) ([]string, error)
	RepairInvalidMembers(ctx context.Context) error
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
			return nil, ErrInvalidGroupHierarchyTree
		}
	}

	if config.DefaultRoles == nil {
		config.DefaultRoles = DefaultRoles
	}

	if len(config.TypeToRoleOverrides) == 0 {
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
func (s *Service) validateHierarchyTreeConfig(logger *zap.Logger) error {
	if s.Config == nil || len(s.Config.Tree) == 0 {
		return nil
	}

	if err := s.Config.ValidateHierarchy(); err != nil {
		logger.Error("group-hierarchy-tree-validation-failed", zap.Error(err))
		return ErrInvalidGroupHierarchyTree
	}

	return nil
}

// CreateGroup creates a new group
func (s *Service) CreateGroup(ctx context.Context, req *CreateGroupRequest) (*CreateGroupResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "create-group"))
	logger.Debug("creating-group", zap.String("name", req.Name), zap.String("type", req.Type))

	if err := s.validateHierarchyTreeConfig(logger); err != nil {
		return nil, err
	}

	var parentGroup *UniversalGroup
	if req.ParentGroupID != "" {
		parentGroupResult, parentErr := s.GroupRepository.GetGroupByID(ctx, req.ParentGroupID)
		if parentErr != nil {
			logger.Error("failed-to-get-parent-group", zap.Error(parentErr), zap.String("parent_group_id", req.ParentGroupID))
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
		logger.Error("failed-to-validate-group-name", zap.Error(err), zap.String("name", req.Name))
		return nil, err
	}
	if !nameValidation.Available && nameValidation.IsRootType {
		logger.Debug("group-with-name-already-exists", zap.String("name", nameValidation.Name))
		return nil, ErrNameAlreadyExists
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
			logger.Error("failed-to-add-initial-member", zap.Error(err), zap.String("member_id", memberReq.ID))
			return nil, err
		}
	}

	if group.OwnerID != "" {
		if err := s.ensureOwnerMembershipAndAdminRole(ctx, group, group.OwnerID); err != nil {
			logger.Error("failed-to-ensure-owner-membership-on-create", zap.Error(err), zap.String("owner_id", group.OwnerID))
			return nil, err
		}
	}

	// Validate group
	if err := group.Validate(); err != nil {
		logger.Error("group-validation-failed", zap.Error(err))
		return nil, err
	}

	if parentGroup != nil {
		if !s.Config.CanHaveChildType(parentGroup.Type, group.Type) {
			logger.Error(
				"invalid-parent-child-group-relation",
				zap.String("parent_group_id", parentGroup.ID),
				zap.String("parent_group_type", parentGroup.Type),
				zap.String("child_group_type", group.Type),
			)
			return nil, ErrInvalidParentChildRelation
		}
	}

	// Create group in repository
	createdGroup, err := s.GroupRepository.CreateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-create-group-in-repository", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.created",
			TargetId:   createdGroup.ID,
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
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-group-by-id"))
	logger.Debug("getting-group-by-id", zap.String("id", req.ID))

	group, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-group-by-id", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
	if req.PrefixName {
		group.Name = s.getPrefixName(ctx, group, map[string]string{})
	}

	return &GetGroupByIDResponse{Group: group}, nil
}

// GetGroupLineage retrieves the root-first lineage for a group, including the group itself.
func (s *Service) GetGroupLineage(ctx context.Context, req *GetGroupLineageRequest) (*GetGroupLineageResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-group-lineage"))
	asUserID := strings.TrimSpace(req.AsUserID)
	logger.Debug("getting-group-lineage", zap.String("id", req.ID), zap.String("as_user_id", asUserID))

	group, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-group-for-lineage", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	lineageIDs := append([]string{}, group.Lineage...)
	lineageIDs = append(lineageIDs, group.ID)

	lineage := make([]GroupLineageNode, 0, len(lineageIDs))
	prefixNameByRootID := make(map[string]string)
	for _, groupID := range lineageIDs {
		lineageGroup, lineageErr := s.GroupRepository.GetGroupByID(ctx, groupID)
		if lineageErr != nil {
			logger.Warn("failed-to-get-lineage-group", zap.Error(lineageErr), zap.String("lineage_group_id", groupID))
			continue
		}

		_, isAdmin := s.resolveUserRoleForGroup(lineageGroup, asUserID)
		lineageGroupName := lineageGroup.Name
		if req.PrefixName {
			lineageGroupName = s.getPrefixName(ctx, lineageGroup, prefixNameByRootID)
		}

		lineage = append(lineage, GroupLineageNode{
			ID:            lineageGroup.ID,
			ParentGroupID: lineageGroup.ParentGroupID,
			Name:          lineageGroupName,
			RawName:       lineageGroup.RawName,
			Type:          lineageGroup.Type,
			IsMember:      asUserID != "" && lineageGroup.HasMember(asUserID),
			IsOwner:       asUserID != "" && strings.TrimSpace(lineageGroup.OwnerID) == asUserID,
			IsAdmin:       asUserID != "" && isAdmin,
		})
	}

	return &GetGroupLineageResponse{Lineage: lineage}, nil
}

// GetGroupDescendants retrieves all descendants for a group grouped by depth level.
// Index 0 contains direct children, index 1 grandchildren, and so on.
func (s *Service) GetGroupDescendants(ctx context.Context, req *GetGroupDescendantsRequest) (*GetGroupDescendantsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-group-descendants"))
	asUserID := strings.TrimSpace(req.AsUserID)
	logger.Debug(
		"getting-group-descendants",
		zap.String("id", req.ID),
		zap.Int("max_depth", req.MaxDepth),
		zap.Bool("include_self", req.IncludeSelf),
		zap.String("as_user_id", asUserID),
	)

	if req.MaxDepth < 0 {
		return nil, ErrInvalidQueryParam
	}

	targetGroup, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-group-for-descendants", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	descendantGroups, err := s.GroupRepository.GetGroupsByLineageAncestor(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-descendant-groups", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	canSeeRequestedGroup := true
	if asUserID != "" {
		descendantGroups, canSeeRequestedGroup = s.filterVisibleDescendantsForUser(targetGroup, asUserID, descendantGroups)
	}

	levelsMap := map[int][]GroupDescendantsNode{}
	maxLevel := -1
	prefixNameByRootID := make(map[string]string)

	if req.IncludeSelf && (asUserID == "" || canSeeRequestedGroup) {
		_, isAdmin := s.resolveUserRoleForGroup(targetGroup, asUserID)
		targetGroupName := targetGroup.Name
		if req.PrefixName {
			targetGroupName = s.getPrefixName(ctx, targetGroup, prefixNameByRootID)
		}

		levelsMap[0] = append(levelsMap[0], GroupDescendantsNode{
			ID:            targetGroup.ID,
			ParentGroupID: targetGroup.ParentGroupID,
			Name:          targetGroupName,
			RawName:       targetGroup.RawName,
			Type:          targetGroup.Type,
			IsMember:      asUserID != "" && targetGroup.HasMember(asUserID),
			IsOwner:       asUserID != "" && strings.TrimSpace(targetGroup.OwnerID) == asUserID,
			IsAdmin:       asUserID != "" && isAdmin,
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

		_, isAdmin := s.resolveUserRoleForGroup(&group, asUserID)
		groupName := group.Name
		if req.PrefixName {
			groupName = s.getPrefixName(ctx, &group, prefixNameByRootID)
		}

		levelsMap[depth] = append(levelsMap[depth], GroupDescendantsNode{
			ID:            group.ID,
			ParentGroupID: group.ParentGroupID,
			Name:          groupName,
			RawName:       group.RawName,
			Type:          group.Type,
			IsMember:      asUserID != "" && group.HasMember(asUserID),
			IsOwner:       asUserID != "" && strings.TrimSpace(group.OwnerID) == asUserID,
			IsAdmin:       asUserID != "" && isAdmin,
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

// filterVisibleDescendantsForUser applies visibility constraints for descendant queries when
// a request is made as a specific user. PRIVATE ("secret") groups remain membership-
// restricted, while INTERNAL groups stay discoverable through a visible hierarchy.
func (s *Service) filterVisibleDescendantsForUser(rootGroup *UniversalGroup, asUserID string, descendants []UniversalGroup) ([]UniversalGroup, bool) {
	if asUserID == "" {
		return descendants, true
	}

	// Root-level OWNER/ADMIN access should unlock full visibility across that
	// requested hierarchy tree, including PRIVATE descendants.
	if rootRole, rootIsAdmin := s.resolveUserRoleForGroup(rootGroup, asUserID); rootRole == MemberRoleOwner || rootIsAdmin {
		return descendants, true
	}

	groupsByID := make(map[string]*UniversalGroup, len(descendants))
	for i := range descendants {
		groupsByID[descendants[i].ID] = &descendants[i]
	}

	// requiresMembership returns true for visibility modes that should stay hidden
	// unless the requester has a direct membership connection to that branch.
	requiresMembership := func(vis string) bool {
		return vis == VisibilityPrivate
	}

	visibleRestrictedGroupIDs := make(map[string]struct{})
	canSeeRootGroup := !requiresMembership(s.groupVisibility(rootGroup))
	if rootGroup.HasMember(asUserID) {
		canSeeRootGroup = true
	}

	for i := range descendants {
		group := &descendants[i]
		if !group.HasMember(asUserID) {
			continue
		}

		if requiresMembership(s.groupVisibility(group)) {
			visibleRestrictedGroupIDs[group.ID] = struct{}{}
		}

		for _, lineageID := range group.Lineage {
			if lineageID == rootGroup.ID {
				if requiresMembership(s.groupVisibility(rootGroup)) {
					canSeeRootGroup = true
				}
				continue
			}

			ancestor, exists := groupsByID[lineageID]
			if !exists {
				continue
			}

			if requiresMembership(s.groupVisibility(ancestor)) {
				visibleRestrictedGroupIDs[ancestor.ID] = struct{}{}
			}
		}
	}

	filtered := make([]UniversalGroup, 0, len(descendants))
	for i := range descendants {
		group := descendants[i]
		groupVisibility := s.groupVisibility(&group)

		if requiresMembership(groupVisibility) {
			_, isVisibleRestrictedGroup := visibleRestrictedGroupIDs[group.ID]
			if !group.HasMember(asUserID) && !isVisibleRestrictedGroup {
				continue
			}
		}

		isBlockedByHiddenAncestor := false
		for _, lineageID := range group.Lineage {
			if lineageID == rootGroup.ID {
				if requiresMembership(s.groupVisibility(rootGroup)) && !canSeeRootGroup {
					isBlockedByHiddenAncestor = true
					break
				}
				continue
			}

			ancestor, exists := groupsByID[lineageID]
			if !exists {
				continue
			}

			if !requiresMembership(s.groupVisibility(ancestor)) {
				continue
			}

			if _, isVisibleRestrictedAncestor := visibleRestrictedGroupIDs[ancestor.ID]; !isVisibleRestrictedAncestor {
				isBlockedByHiddenAncestor = true
				break
			}
		}

		if isBlockedByHiddenAncestor {
			continue
		}

		filtered = append(filtered, group)
	}

	return filtered, canSeeRootGroup
}

// groupVisibility returns the normalised visibility for a group.
func (s *Service) groupVisibility(group *UniversalGroup) string {
	if group == nil || group.Settings == nil {
		return VisibilityPublic
	}

	visibility := strings.TrimSpace(strings.ToUpper(group.Settings.Visibility))
	if visibility == "" {
		return VisibilityPublic
	}

	return visibility
}

// GetGroupByNanoID retrieves a group by nano ID.
func (s *Service) GetGroupByNanoID(ctx context.Context, req *GetGroupByNanoIDRequest) (*GetGroupByNanoIDResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-group-by-nano-id"))
	logger.Debug("getting-group-by-nano-id", zap.String("nano_id", req.NanoID))

	group, err := s.GroupRepository.GetGroupByNanoID(ctx, req.NanoID)
	if err != nil {
		logger.Error("failed-to-get-group-by-nano-id", zap.Error(err), zap.String("nano_id", req.NanoID))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
	if req.PrefixName {
		group.Name = s.getPrefixName(ctx, group, map[string]string{})
	}

	return &GetGroupByNanoIDResponse{Group: group}, nil
}

// UpdateGroup updates an existing group
func (s *Service) UpdateGroup(ctx context.Context, req *UpdateGroupRequest) (*UpdateGroupResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "update-group"))
	logger.Debug("updating-group", zap.String("id", req.ID))

	if err := s.validateHierarchyTreeConfig(logger); err != nil {
		return nil, err
	}

	targetGroupID := req.ID
	if req.Group != nil && req.Group.ID != "" {
		targetGroupID = req.Group.ID
	}

	// Get existing group
	group, err := s.GroupRepository.GetGroupByID(ctx, targetGroupID)
	if err != nil {
		logger.Error("failed-to-get-group-for-update", zap.Error(err), zap.String("id", targetGroupID))
		return nil, err
	}

	if req.Group != nil {
		groupWithProvidedData := req.Group

		groupWithProvidedData.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

		if groupWithProvidedData.Name != "" {
			// Normalises the incoming name using the same logic as ValidateGroupName so
			// the comparison is always consistent. Errors here mean the name is
			// unparseable, so treat the slugs as different and let full validation
			// report the problem.
			normalisedIncoming := group.Name // fallback: treat as unchanged
			if precheck, precheckErr := s.ValidateGroupName(ctx, &ValidateGroupNameRequest{
				Name:          groupWithProvidedData.Name,
				Type:          group.Type,
				ParentGroupID: group.ParentGroupID,
			}); precheckErr == nil {
				normalisedIncoming = precheck.Name
			}
			if normalisedIncoming != group.Name {
				nameValidation, nameValidationErr := s.ValidateGroupName(ctx, &ValidateGroupNameRequest{
					Name:          groupWithProvidedData.Name,
					Type:          group.Type,
					ParentGroupID: group.ParentGroupID,
				})
				if nameValidationErr != nil {
					logger.Error("failed-to-validate-group-name-for-update", zap.Error(nameValidationErr), zap.String("name", groupWithProvidedData.Name))
					return nil, nameValidationErr
				}
				if !nameValidation.Available && nameValidation.IsRootType {
					return nil, ErrNameAlreadyExists
				}
				groupWithProvidedData.RawName = nameValidation.RawName
				groupWithProvidedData.Name = nameValidation.Name
			} else {
				// Slug unchanged — preserve the stored name values; allow the raw display
				// name to be updated in case casing/spacing was adjusted.
				groupWithProvidedData.Name = group.Name
				if groupWithProvidedData.RawName == "" {
					groupWithProvidedData.RawName = group.RawName
				}
			}
		}

		group = groupWithProvidedData
	}

	if req.Group == nil {
		group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

		// Track if any changes were made
		hasChanges := false

		// Update fields if provided
		if req.Name != nil {
			// Normalize using the same logic as ValidateGroupName so the comparison
			// is always consistent. Errors fall through to full validation below.
			normalisedIncoming := group.Name // fallback: treat as unchanged
			if precheck, precheckErr := s.ValidateGroupName(ctx, &ValidateGroupNameRequest{
				Name:          *req.Name,
				Type:          group.Type,
				ParentGroupID: group.ParentGroupID,
			}); precheckErr == nil {
				normalisedIncoming = precheck.Name
			}
			if normalisedIncoming != group.Name {
				nameValidation, nameValidationErr := s.ValidateGroupName(ctx, &ValidateGroupNameRequest{
					Name:          *req.Name,
					Type:          group.Type,
					ParentGroupID: group.ParentGroupID,
				})
				if nameValidationErr != nil {
					logger.Error("failed-to-validate-group-name-for-update", zap.Error(nameValidationErr), zap.String("name", *req.Name))
					return nil, nameValidationErr
				}
				if !nameValidation.Available && nameValidation.IsRootType {
					return nil, ErrNameAlreadyExists
				}
				if nameValidation.Name != group.Name || nameValidation.RawName != group.RawName {
					group.Name = nameValidation.Name
					group.RawName = nameValidation.RawName
					hasChanges = true
				}
			}
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
				logger.Error("failed-to-update-group-status", zap.Error(err))
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
			logger.Debug("no-changes-detected-for-group-update", zap.String("id", req.ID))
			return &UpdateGroupResponse{Group: group}, nil
		}
	}

	// Update timestamps
	group.SetUpdatedAtNow()

	// Validate group
	if err := group.Validate(); err != nil {
		logger.Error("group-validation-failed", zap.Error(err))
		return nil, err
	}

	// Ensure version is set to 1
	group.EnsureVersion()

	// Update in repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.updated",
			TargetId:   updatedGroup.ID,
			Details: map[string]interface{}{
				"group_name": updatedGroup.Name,
			},
		})
	}

	logger.Info("group-updated-successfully", zap.String("group-id", updatedGroup.ID))

	return &UpdateGroupResponse{Group: updatedGroup}, nil
}

// DeleteGroup deletes a group
func (s *Service) DeleteGroup(ctx context.Context, req *DeleteGroupRequest) (*DeleteGroupResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "delete-group"))
	logger.Debug("deleting-group", zap.String("id", req.ID), zap.Bool("hard_delete", req.HardDelete))

	// Verify group exists and capture the root snapshot.
	targetGroup, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		logger.Error("group-not-found-for-deletion", zap.Error(err), zap.String("id", req.ID))
		return nil, err
	}

	// Cascade includes all active descendants for the target group.
	descendants, err := s.GroupRepository.GetGroupsByLineageAncestor(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-groups-by-lineage-ancestor", zap.Error(err), zap.String("id", req.ID))
		return nil, ErrDatabaseError
	}

	groupsToDelete := make([]*UniversalGroup, 0, len(descendants)+1)
	groupsToDelete = append(groupsToDelete, targetGroup)
	for i := range descendants {
		groupSnapshot := descendants[i]
		groupsToDelete = append(groupsToDelete, &groupSnapshot)
	}

	// Delete deepest descendants first, root group last.
	sort.Slice(groupsToDelete, func(i, j int) bool {
		return len(groupsToDelete[i].Lineage) > len(groupsToDelete[j].Lineage)
	})

	deletedAt := ""
	if s.TimeProvider != nil {
		deletedAt = s.TimeProvider.NowUTC()
	}

	for _, groupSnapshot := range groupsToDelete {
		if groupSnapshot == nil {
			continue
		}

		if req.HardDelete {
			err = s.GroupRepository.DeleteGroupByID(ctx, groupSnapshot.ID)
		} else {
			err = s.GroupRepository.SoftDeleteGroup(ctx, groupSnapshot.ID, req.DeletedByID, deletedAt)
		}

		if err != nil {
			logger.Error("failed-to-delete-group", zap.String("group_id", groupSnapshot.ID), zap.Error(err))
			return nil, ErrDatabaseError
		}

		if s.AuditService != nil {
			s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
				TargetType: "group",
				Domain:     "group",
				Action: func() audit.AuditAction {
					if req.HardDelete {
						return "group.hard_deleted"
					} else {
						return "group.soft_deleted"
					}
				}(),
				TargetId: groupSnapshot.ID,
				Details: map[string]interface{}{
					"hard_delete":       req.HardDelete,
					"deleted_by_id":     req.DeletedByID,
					"deleted_at":        deletedAt,
					"cascade_root_id":   req.ID,
					"is_cascade_target": groupSnapshot.ID != req.ID,
					"final_snapshot":    groupSnapshot,
				},
			})
		}
	}

	return &DeleteGroupResponse{
		Success: true,
		Message: "Group deleted successfully",
	}, nil
}

// GetGroups retrieves groups with filters and pagination
func (s *Service) GetGroups(ctx context.Context, req *GetGroupsRequest) (*GetGroupsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-groups"))
	logger.Debug("getting-groups", zap.Int("page", req.Page), zap.Int("page_size", req.PageSize))

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
		logger.Error("failed-to-get-total-groups-count", zap.Error(err))
		return nil, ErrDatabaseError
	}

	req.TotalCount = int(totalGroups)
	logger.Debug("handling-get-groups-request-total-groups-found", zap.Int64("total", totalGroups), zap.Any("request", safeLogValue(req)))

	// Get groups
	groups, err := s.GroupRepository.GetGroups(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-groups", zap.Error(err))
		return nil, ErrDatabaseError
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

	if req.PrefixName {
		prefixNameByRootID := make(map[string]string)
		for _, grp := range paginatedResponse.Resources {
			if grp == nil {
				continue
			}
			grp.Name = s.getPrefixName(ctx, grp, prefixNameByRootID)
		}
	}

	return &GetGroupsResponse{
		Total:      paginatedResponse.Total,
		TotalPages: paginatedResponse.TotalPages,
		Groups:     paginatedResponse.Resources,
		Page:       paginatedResponse.Page,
		PerPage:    paginatedResponse.ResourcePerPage,
	}, nil
}

// GetGroupsByUserID retrieves groups where the user is referenced as owner or member.
func (s *Service) GetGroupsByUserID(ctx context.Context, req *GetGroupsByUserIDRequest) (*GetGroupsByUserIDResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-groups-by-user-id"))
	userID := strings.TrimSpace(req.UserID)
	logger.Debug("getting-groups-by-user-id", zap.String("user_id", userID), zap.Bool("include_descendants", req.IncludeDescendants))

	if userID == "" {
		return nil, ErrInvalidQueryParam
	}

	groups, err := s.GroupRepository.GetGroupsByReferencedUserID(ctx, userID)
	if err != nil {
		logger.Error("failed-to-get-groups-by-user-id", zap.Error(err), zap.String("user_id", userID))
		return nil, ErrDatabaseError
	}

	groupPointers := make([]*UniversalGroup, len(groups))
	for i := range groups {
		groups[i].SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)
		groupPointers[i] = &groups[i]
	}

	if req.PrefixName {
		prefixNameByRootID := make(map[string]string)
		for _, grp := range groupPointers {
			if grp == nil {
				continue
			}
			grp.Name = s.getPrefixName(ctx, grp, prefixNameByRootID)
		}
	}

	response := &GetGroupsByUserIDResponse{Groups: groupPointers, Descendants: map[string][][]GroupDescendantsNode{}}
	if !req.IncludeDescendants {
		return response, nil
	}

	// make sure there are no duplicate root groups in the list to avoid redundant descendant queries
	rootGroupIDs := map[string]struct{}{}
	for _, grp := range groupPointers {
		if grp == nil {
			continue
		}

		rootGroupID := grp.ID
		if len(grp.Lineage) > 0 && strings.TrimSpace(grp.Lineage[0]) != "" {
			rootGroupID = grp.Lineage[0]
		}

		if strings.TrimSpace(rootGroupID) == "" {
			continue
		}

		rootGroupIDs[rootGroupID] = struct{}{}
	}

	descendants := make(map[string][][]GroupDescendantsNode)
	for rootGroupID := range rootGroupIDs {
		descendantsResp, descendantsErr := s.GetGroupDescendants(ctx, &GetGroupDescendantsRequest{
			ID:          rootGroupID,
			IncludeSelf: true,
			AsUserID:    userID,
			PrefixName:  req.PrefixName,
		})
		if descendantsErr != nil || descendantsResp == nil {
			logger.Warn(
				"failed-to-get-root-group-descendants",
				zap.String("root_group_id", rootGroupID),
				zap.Error(descendantsErr),
			)
			continue
		}

		descendants[rootGroupID] = descendantsResp.Descendants
	}

	response.Descendants = descendants
	return response, nil
}

// getPrefixName prefixes a child group name with the root group's name.
// Root group is derived from the first lineage element, with parent ID as a fallback.
func (s *Service) getPrefixName(ctx context.Context, g *UniversalGroup, prefixNameByRootID map[string]string) string {
	logger := logger.AcquireOperationFrom(ctx, "external/group", "get-prefix-name")
	logger.Debug("handling-get-prefix-name-request")

	if g == nil {
		return ""
	}

	if strings.TrimSpace(g.ParentGroupID) == "" {
		return g.Name
	}

	rootID := ""
	if len(g.Lineage) > 0 {
		rootID = strings.TrimSpace(g.Lineage[0])
	}
	if rootID == "" {
		rootID = strings.TrimSpace(g.ParentGroupID)
	}

	if rootID == "" || rootID == g.ID {
		return g.Name
	}

	if prefixName, found := prefixNameByRootID[rootID]; found {
		if prefixName == "" {
			return g.Name
		}
		return prefixName + "/" + g.Name
	}

	rootGroup, err := s.GroupRepository.GetGroupByID(ctx, rootID)
	if err != nil || rootGroup == nil {
		prefixNameByRootID[rootID] = ""
		return g.Name
	}

	prefixName := strings.TrimSpace(rootGroup.Name)
	prefixNameByRootID[rootID] = prefixName
	if prefixName == "" {
		return g.Name
	}

	return prefixName + "/" + g.Name
}

// GetUserGroupAccessMap builds an effective group access map for the provided user.
//
// Admin privileges propagate from any directly-administered group down to all
// descendants of that group.
func (s *Service) GetUserGroupAccessMap(ctx context.Context, userID string) (map[string]UserGroupAccessSummary, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-user-group-access-map"))
	trimmedUserID := strings.TrimSpace(userID)
	logger.Debug("building-user-group-access-map", zap.String("user_id", trimmedUserID))

	if trimmedUserID == "" {
		return nil, ErrInvalidUserIDProvided
	}

	userGroupsResp, err := s.GetGroupsByUserID(ctx, &GetGroupsByUserIDRequest{
		UserID:             trimmedUserID,
		IncludeDescendants: false,
	})
	if err != nil {
		logger.Error("failed-to-get-user-groups", zap.Error(err), zap.String("user_id", trimmedUserID))
		return nil, err
	}

	if userGroupsResp == nil || len(userGroupsResp.Groups) == 0 {
		return map[string]UserGroupAccessSummary{}, nil
	}

	accessByGroupID := map[string]UserGroupAccessSummary{}
	adminSourceGroupIDs := map[string]struct{}{}

	for _, grp := range userGroupsResp.Groups {
		if grp == nil || strings.TrimSpace(grp.ID) == "" {
			continue
		}

		effectiveRole, isAdmin := s.resolveUserRoleForGroup(grp, trimmedUserID)
		accessByGroupID[grp.ID] = UserGroupAccessSummary{
			MaxRole:      effectiveRole,
			IsAccessible: true,
			IsAdmin:      isAdmin,
		}

		if isAdmin {
			adminSourceGroupIDs[grp.ID] = struct{}{}
		}
	}

	for adminGroupID := range adminSourceGroupIDs {
		descendants, descendantsErr := s.GroupRepository.GetGroupsByLineageAncestor(ctx, adminGroupID)
		if descendantsErr != nil {
			logger.Warn(
				"failed-to-get-admin-group-descendants",
				zap.String("admin_group_id", adminGroupID),
				zap.Error(descendantsErr),
			)
			continue
		}

		for i := range descendants {
			descendant := descendants[i]
			if strings.TrimSpace(descendant.ID) == "" {
				continue
			}

			existing := accessByGroupID[descendant.ID]
			existing.MaxRole = s.pickHigherRole(existing.MaxRole, MemberRoleAdmin)
			existing.IsAccessible = true
			existing.IsAdmin = true
			accessByGroupID[descendant.ID] = existing
		}
	}

	return accessByGroupID, nil
}

// AddMember adds a member to a group
func (s *Service) AddMember(ctx context.Context, req *AddMemberRequest) (*AddMemberResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "add-member"))
	logger.Debug("adding-member-to-group", zap.String("group_id", req.GroupID), zap.String("member_id", req.MemberID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		logger.Error("failed-to-get-group", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Validate role against group type configuration
	if err := s.isValidMemberRole(group.Type, req.Role); err != nil {
		logger.Error("invalid-member-role", zap.Error(err), zap.String("role", req.Role), zap.String("group_type", group.Type))
		return nil, err
	}

	if len(group.Lineage) > 0 {
		if err := s.ensureRootMembership(ctx, group, req.MemberID, req.Type); err != nil {
			logger.Error("failed-to-ensure-root-membership", zap.Error(err), zap.String("group_id", req.GroupID), zap.String("member_id", req.MemberID))
			return nil, err
		}
	}

	// Add member
	if _, err := group.AddMember(req.MemberID, req.Type, req.Role); err != nil {
		logger.Error("failed-to-add-member", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Get the added member
	addedMember, _ := group.GetMemberByID(req.MemberID)

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.member.added",
			TargetId:   req.GroupID,
			Details: map[string]interface{}{
				"member": addedMember,
			},
		})
	}

	return &AddMemberResponse{
		Group: updatedGroup,
	}, nil
}

// InviteUser adds a pending invitation for an email address to a top-level group.
func (s *Service) InviteUser(ctx context.Context, req *InviteUserRequest) (*InviteUserResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "invite-user"))
	logger.Debug("inviting-user-to-group", zap.String("group_id", req.GroupID), zap.String("invite_email", req.InviteEmail))

	inviteEmail, err := toolbox.NormaliseEmail(req.InviteEmail)
	if err != nil {
		return nil, err
	}

	targetGroup, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		logger.Error("failed-to-get-target-group-for-invite", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, err
	}
	targetGroup.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	if s.isNonTopLevelGroup(targetGroup) {
		return nil, ErrInviteRequiresTopLevelGroup
	}

	if req.Role != "" {
		if roleErr := s.isValidMemberRole(targetGroup.Type, req.Role); roleErr != nil {
			return nil, roleErr
		}
	}

	targetAdded, err := s.addPendingInviteMember(targetGroup, inviteEmail, req.Role, req.InvitedByID)
	if err != nil {
		logger.Error("failed-to-add-pending-invite-to-target-group", zap.Error(err), zap.String("group_id", req.GroupID), zap.String("invite_email", inviteEmail))
		return nil, err
	}
	if !targetAdded {
		return nil, ErrInvitationAlreadyExists
	}

	updatedTargetGroup, err := s.GroupRepository.UpdateGroup(ctx, targetGroup)
	if err != nil {
		logger.Error("failed-to-persist-target-group-invite", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, ErrDatabaseError
	}

	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.member.invited",
			TargetId:   req.GroupID,
			Details: map[string]interface{}{
				"invite_email":         inviteEmail,
				"invited_by_id":        req.InvitedByID,
				"target_group_id":      req.GroupID,
				"root_group_id":        req.GroupID,
				"target_member_role":   req.Role,
				"invitation_lifecycle": "sent",
			},
		})
	}

	return &InviteUserResponse{Group: updatedTargetGroup, InviteEmail: inviteEmail}, nil
}

// UninviteUser removes a pending invitation from a top-level group.
func (s *Service) UninviteUser(ctx context.Context, req *UninviteUserRequest) (*UninviteUserResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "uninvite-user"))
	logger.Debug("uninviting-user-from-group", zap.String("group_id", req.GroupID), zap.String("invite_email", req.InviteEmail))

	inviteEmail, err := toolbox.NormaliseEmail(req.InviteEmail)
	if err != nil {
		return nil, err
	}

	targetGroup, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		logger.Error("failed-to-get-target-group-for-uninvite", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, err
	}
	targetGroup.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	if s.isNonTopLevelGroup(targetGroup) {
		return nil, ErrInviteRequiresTopLevelGroup
	}

	if !removePendingInviteByEmail(targetGroup, inviteEmail) {
		return nil, ErrInvitationNotFound
	}

	updatedTargetGroup, err := s.GroupRepository.UpdateGroup(ctx, targetGroup)
	if err != nil {
		logger.Error("failed-to-persist-target-group-uninvite", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, ErrDatabaseError
	}

	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.member.uninvited",
			TargetId:   req.GroupID,
			Details: map[string]interface{}{
				"invite_email":         inviteEmail,
				"uninvited_by_id":      req.UninvitedByID,
				"invitation_lifecycle": "revoked",
			},
		})
	}

	return &UninviteUserResponse{Group: updatedTargetGroup, InviteEmail: inviteEmail}, nil
}

// AcceptInvite accepts a pending invitation for a user and materialises membership.
func (s *Service) AcceptInvite(ctx context.Context, req *AcceptInviteRequest) (*AcceptInviteResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "accept-invite"))
	logger.Debug("accepting-group-invite", zap.String("group_id", req.GroupID), zap.String("invite_email", req.InviteEmail), zap.String("user_id", req.UserID))

	inviteEmail, err := toolbox.NormaliseEmail(req.InviteEmail)
	if err != nil {
		return nil, err
	}

	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return nil, ErrInvalidUserIDProvided
	}

	targetGroup, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		logger.Error("failed-to-get-target-group-for-accept", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, err
	}
	targetGroup.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	if s.isNonTopLevelGroup(targetGroup) {
		return nil, ErrInviteRequiresTopLevelGroup
	}

	targetRole, targetChanged, acceptErr := s.acceptInviteInGroup(targetGroup, inviteEmail, userID)
	if acceptErr != nil {
		return nil, acceptErr
	}
	if !targetChanged {
		return nil, ErrInvitationNotFound
	}

	updatedTargetGroup, err := s.GroupRepository.UpdateGroup(ctx, targetGroup)
	if err != nil {
		logger.Error("failed-to-persist-target-group-accept", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, ErrDatabaseError
	}

	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.member.invite.accepted",
			TargetId:   req.GroupID,
			Details: map[string]interface{}{
				"invite_email":         inviteEmail,
				"user_id":              userID,
				"target_member_role":   targetRole,
				"invitation_lifecycle": "accepted",
			},
		})
	}

	return &AcceptInviteResponse{Group: updatedTargetGroup, InviteEmail: inviteEmail, UserID: userID}, nil
}

// RejectInvite rejects a pending invitation from a top-level group.
func (s *Service) RejectInvite(ctx context.Context, req *RejectInviteRequest) (*RejectInviteResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "reject-invite"))
	logger.Debug("rejecting-group-invite", zap.String("group_id", req.GroupID), zap.String("invite_email", req.InviteEmail))

	inviteEmail, err := toolbox.NormaliseEmail(req.InviteEmail)
	if err != nil {
		return nil, err
	}

	targetGroup, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		logger.Error("failed-to-get-target-group-for-reject", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, err
	}
	targetGroup.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	if s.isNonTopLevelGroup(targetGroup) {
		return nil, ErrInviteRequiresTopLevelGroup
	}

	if !removePendingInviteByEmail(targetGroup, inviteEmail) {
		return nil, ErrInvitationNotFound
	}

	updatedTargetGroup, err := s.GroupRepository.UpdateGroup(ctx, targetGroup)
	if err != nil {
		logger.Error("failed-to-persist-target-group-reject", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, ErrDatabaseError
	}

	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.member.invite.rejected",
			TargetId:   req.GroupID,
			Details: map[string]interface{}{
				"invite_email":         inviteEmail,
				"rejected_by_id":       req.RejectedByID,
				"invitation_lifecycle": "rejected",
			},
		})
	}

	return &RejectInviteResponse{Group: updatedTargetGroup, InviteEmail: inviteEmail}, nil
}

// RemoveUserFromAllGroups removes a user from all groups they are a member/owner of.
func (s *Service) RemoveUserFromAllGroups(ctx context.Context, req *RemoveUserFromAllGroupsRequest) (*RemoveUserFromAllGroupsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "remove-user-from-all-groups"))
	userID := strings.TrimSpace(req.UserID)
	logger.Debug("removing-user-from-all-groups", zap.String("user_id", userID))

	if userID == "" {
		return nil, ErrInvalidUserIDProvided
	}

	groupsResp, groupsErr := s.GetGroupsByUserID(ctx, &GetGroupsByUserIDRequest{
		UserID:             userID,
		IncludeDescendants: false,
	})
	if groupsErr != nil {
		return &RemoveUserFromAllGroupsResponse{
			Success:                 false,
			TotalRootGroupsAffected: 0,
			Message:                 "Failed to retrieve groups for user",
		}, groupsErr
	}

	rootGroupIDSet := map[string]struct{}{}
	if groupsResp != nil {
		for _, grp := range groupsResp.Groups {
			if grp == nil {
				continue
			}

			rootGroupID := strings.TrimSpace(grp.ID)
			if len(grp.Lineage) > 0 {
				candidateRootID := strings.TrimSpace(grp.Lineage[0])
				if candidateRootID != "" {
					rootGroupID = candidateRootID
				}
			}

			if rootGroupID == "" {
				continue
			}

			rootGroupIDSet[rootGroupID] = struct{}{}
		}
	}

	rootGroupIDs := make([]string, 0, len(rootGroupIDSet))
	for rootGroupID := range rootGroupIDSet {
		rootGroupIDs = append(rootGroupIDs, rootGroupID)
	}
	sort.Strings(rootGroupIDs)

	for _, rootGroupID := range rootGroupIDs {
		_, removeErr := s.RemoveMember(ctx, &RemoveMemberRequest{
			GroupID:             rootGroupID,
			MemberID:            userID,
			ConfirmOwnerRemoval: true,
		})
		if removeErr != nil {
			return &RemoveUserFromAllGroupsResponse{
				Success:                 false,
				TotalRootGroupsAffected: len(rootGroupIDSet),
				Message:                 "Failed to remove user from all groups",
			}, removeErr
		}
	}

	logger.Info(
		"completed-removing-user-from-all-groups",
		zap.String("user-id", userID),
		zap.Int("root-groups-count", len(rootGroupIDs)),
	)

	return &RemoveUserFromAllGroupsResponse{
		Success:                 true,
		TotalRootGroupsAffected: len(rootGroupIDSet),
		Message:                 "User removed from all groups successfully",
	}, nil

}

// RemoveMember removes a member from a group
func (s *Service) RemoveMember(ctx context.Context, req *RemoveMemberRequest) (*RemoveMemberResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "remove-member"))
	logger.Debug("removing-member-from-group", zap.String("group_id", req.GroupID), zap.String("member_id", req.MemberID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		logger.Error("failed-to-get-group", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	if group.OwnerID == req.MemberID && !req.ConfirmOwnerRemoval {
		logger.Warn(
			"owner-removal-requires-explicit-confirmation",
			zap.String("group_id", req.GroupID),
			zap.String("member_id", req.MemberID),
		)
		return nil, ErrOwnerRemovalRequiresConfirm
	}

	// Remove member
	if group, err = group.RemoveMember(req.MemberID); err != nil {
		logger.Error("failed-to-remove-member", zap.Error(err))
		return nil, err
	}

	if group.OwnerID == req.MemberID {
		group.OwnerID = ""

		err = s.GroupRepository.ClearOwnerFromGroup(ctx, req.GroupID, req.MemberID)
		if err != nil {
			logger.Error("failed-to-clear-owner-from-group-in-repository", zap.Error(err))
			return nil, ErrDatabaseError
		}
	}

	// Persist removal directly so member-array updates do not depend on full-document $set behavior.
	err = s.GroupRepository.RemoveMemberFromGroup(ctx, req.GroupID, req.MemberID)
	if err != nil {
		logger.Error("failed-to-remove-member-from-group-in-repository", zap.Error(err))
		return nil, ErrDatabaseError
	}

	updatedGroup, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		logger.Error("failed-to-get-updated-group-after-member-removal", zap.Error(err), zap.String("group_id", req.GroupID))
		return nil, err
	}

	removedCascadeGroupIDs := []string{}
	failedCascadeGroupIDs := []string{}
	if len(group.Lineage) == 0 {
		removedCascadeGroupIDs, failedCascadeGroupIDs = s.cascadeRemoveMemberFromDescendants(ctx, req.GroupID, req.MemberID)
		if len(failedCascadeGroupIDs) > 0 {
			logger.Warn(
				"failed-to-remove-member-from-some-descendants",
				zap.String("root_group_id", req.GroupID),
				zap.String("member_id", req.MemberID),
				zap.Strings("failed_descendant_group_ids", failedCascadeGroupIDs),
			)
		}
	}

	// Audit log
	if s.AuditService != nil {
		details := map[string]interface{}{
			"member": map[string]string{
				"id": req.MemberID,
			},
		}
		if len(group.Lineage) == 0 {
			details["cascade_removed_from_descendants"] = len(removedCascadeGroupIDs)
			details["removed_descendant_group_ids"] = removedCascadeGroupIDs
			if len(failedCascadeGroupIDs) > 0 {
				details["failed_descendant_group_ids"] = failedCascadeGroupIDs
			}

			s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
				TargetType: "group",
				Domain:     "group",
				Action:     "group.member.removed.cascade",
				TargetId:   req.GroupID,
				Details: map[string]interface{}{
					"member_id":                    req.MemberID,
					"removed_descendant_group_ids": removedCascadeGroupIDs,
					"failed_descendant_group_ids":  failedCascadeGroupIDs,
				},
			})
		}

		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.member.removed",
			TargetId:   req.GroupID,
			Details:    details,
		})
	}

	return &RemoveMemberResponse{
		Group: updatedGroup,
	}, nil
}

// pendingInviteEmail resolves the canonical invite email for a pending member entry.
// It prefers metadata and falls back to the member ID for backward compatibility.
func pendingInviteEmail(member Member) string {
	if member.Metadata != nil {
		if inviteEmailRaw, exists := member.Metadata[MemberMetadataKeyInviteEmail]; exists {
			if inviteEmail, ok := inviteEmailRaw.(string); ok {
				return strings.ToLower(strings.TrimSpace(inviteEmail))
			}
		}
	}

	return strings.ToLower(strings.TrimSpace(member.ID))
}

// isPendingInviteMember reports whether a member record represents an active invite.
// Legacy records with invited_at but missing invitation_state are also treated as pending.
func isPendingInviteMember(member Member) bool {
	state := strings.ToUpper(strings.TrimSpace(member.InvitationState))
	return state == MemberInvitationStateInvited || strings.TrimSpace(member.InvitedAt) != ""
}

// findPendingInviteIndexByEmail returns the index of a pending invite for the email.
// It returns -1 when the group is nil, the email is empty, or no invite exists.
func findPendingInviteIndexByEmail(group *UniversalGroup, inviteEmail string) int {
	if group == nil {
		return -1
	}

	normalisedInviteEmail := strings.ToLower(strings.TrimSpace(inviteEmail))
	if normalisedInviteEmail == "" {
		return -1
	}

	for i := range group.Members {
		member := group.Members[i]
		if member.Type != MemberTypeUser {
			continue
		}

		if !isPendingInviteMember(member) {
			continue
		}

		if pendingInviteEmail(member) == normalisedInviteEmail {
			return i
		}
	}

	return -1
}

// hasPendingInviteByEmail checks whether the group has a pending invite for the email.
func hasPendingInviteByEmail(group *UniversalGroup, inviteEmail string) bool {
	return findPendingInviteIndexByEmail(group, inviteEmail) >= 0
}

// removePendingInviteByEmail removes the pending invite matching the email.
// It returns true when a member was removed.
func removePendingInviteByEmail(group *UniversalGroup, inviteEmail string) bool {
	index := findPendingInviteIndexByEmail(group, inviteEmail)
	if index < 0 {
		return false
	}

	group.Members = append(group.Members[:index], group.Members[index+1:]...)
	group.SetUpdatedAtNow()
	return true
}

// addPendingInviteMember appends a pending invite member to the target top-level group.
// It returns (false, nil) when the invite already exists.
func (s *Service) addPendingInviteMember(group *UniversalGroup, inviteEmail, role, invitedByID string) (bool, error) {
	if group == nil {
		return false, ErrResourceNotFound
	}

	if hasPendingInviteByEmail(group, inviteEmail) {
		return false, nil
	}

	if group.HasMember(inviteEmail) {
		return false, ErrMemberAlreadyExists
	}

	member := Member{
		ID:              inviteEmail,
		Type:            MemberTypeUser,
		Role:            role,
		InvitedAt:       "",
		InvitationState: MemberInvitationStateInvited,
		Metadata: map[string]interface{}{
			MemberMetadataKeyInviteEmail: inviteEmail,
		},
	}

	if s.TimeProvider != nil {
		member.InvitedAt = s.TimeProvider.NowUTC()
	}

	if invitedByID != "" {
		member.SetInviter(invitedByID)
	}

	group.Members = append(group.Members, member)
	group.SetUpdatedAtNow()
	return true, nil
}

// acceptInviteInGroup converts a pending invite into an active user membership.
// If the user already exists, their role is elevated to the highest applicable role.
func (s *Service) acceptInviteInGroup(group *UniversalGroup, inviteEmail, userID string) (string, bool, error) {
	if group == nil {
		return "", false, ErrResourceNotFound
	}

	index := findPendingInviteIndexByEmail(group, inviteEmail)
	if index < 0 {
		return "", false, ErrInvitationNotFound
	}

	memberRole := strings.TrimSpace(group.Members[index].Role)
	if memberRole == "" {
		memberRole = MemberRoleMember
	}

	group.Members = append(group.Members[:index], group.Members[index+1:]...)

	if group.HasMember(userID) {
		for i := range group.Members {
			if group.Members[i].ID != userID || group.Members[i].Type != MemberTypeUser {
				continue
			}

			group.Members[i].Role = s.pickHigherRole(group.Members[i].Role, memberRole)
			group.Members[i].InvitedAt = ""
			group.Members[i].InvitationState = ""
			break
		}
		group.SetUpdatedAtNow()
		return memberRole, true, nil
	}

	acceptedMember := Member{ID: userID, Type: MemberTypeUser, Role: memberRole}
	if s.TimeProvider != nil {
		acceptedMember.JoinedAt = s.TimeProvider.NowUTC()
	}

	group.Members = append(group.Members, acceptedMember)
	group.SetUpdatedAtNow()
	return memberRole, true, nil
}

// isNonTopLevelGroup reports whether a group is nested under another group.
// A non-empty parent_group_id or lineage indicates a non-top-level group.
func (s *Service) isNonTopLevelGroup(group *UniversalGroup) bool {
	if group == nil {
		return true
	}

	return strings.TrimSpace(group.ParentGroupID) != "" || len(group.Lineage) > 0
}

// UpdateMemberRole updates a member's role in a group
func (s *Service) UpdateMemberRole(ctx context.Context, req *UpdateMemberRoleRequest) (*UpdateMemberRoleResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "update-member-role"))
	logger.Debug("updating-member-role", zap.String("group_id", req.GroupID), zap.String("member_id", req.MemberID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		logger.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	req.NewRole = strings.TrimSpace(req.NewRole)

	// Validate new role against group type configuration
	if err := s.isValidMemberRole(group.Type, req.NewRole); err != nil {
		logger.Error("invalid-member-role", zap.Error(err), zap.String("new_role", req.NewRole), zap.String("group_type", group.Type))
		return nil, err
	}

	// Update role
	if group, err = group.UpdateMemberRole(req.MemberID, req.NewRole); err != nil {
		logger.Error("failed-to-update-member-role", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, ErrDatabaseError
	}

	memberInfo, _ := group.GetMemberByID(req.MemberID)

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.member.role_updated",
			TargetId:   req.GroupID,
			Details: map[string]interface{}{
				"member": memberInfo,
			},
		})
	}

	return &UpdateMemberRoleResponse{Group: updatedGroup}, nil
}

// ensureOwnerMembershipAndAdminRole ensures the owner exists as a USER member of the
// target group and is assigned the ADMIN role. For nested groups it also ensures the
// owner exists in the root group to preserve hierarchy membership invariants.
func (s *Service) ensureOwnerMembershipAndAdminRole(ctx context.Context, group *UniversalGroup, ownerID string) error {
	logger := logger.AcquireOperationFrom(ctx, "external/group", "ensure-owner-membership-and-admin-role")
	logger.Debug("handling-ensure-owner-membership-and-admin-role-request")

	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil
	}

	if len(group.Lineage) > 0 {
		if err := s.ensureRootMembership(ctx, group, ownerID, MemberTypeUser); err != nil {
			return err
		}
	}

	if !group.HasMember(ownerID) {
		if _, err := group.AddMember(ownerID, MemberTypeUser, MemberRoleAdmin); err != nil {
			return err
		}
		return nil
	}

	memberInfo, err := group.GetMemberByID(ownerID)
	if err != nil {
		return err
	}

	if memberInfo.Role == MemberRoleAdmin {
		return nil
	}

	_, err = group.UpdateMemberRole(ownerID, MemberRoleAdmin)
	return err
}

// ensureRootMembership ensures the member exists in the root group of a nested hierarchy.
// If absent, it adds the member to the root with an empty role and writes an audit event.
func (s *Service) ensureRootMembership(ctx context.Context, group *UniversalGroup, memberID, memberType string) error {
	logger := logger.AcquireOperationFrom(ctx, "external/group", "ensure-root-membership")
	logger.Debug("handling-ensure-root-membership-request")

	rootGroupID, err := s.getRootGroupID(group.Lineage)
	if err != nil {
		return err
	}

	rootGroup, err := s.GroupRepository.GetGroupByID(ctx, rootGroupID)
	if err != nil {
		return err
	}

	rootGroup.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	if rootGroup.HasMember(memberID) {
		return nil
	}

	if _, err := rootGroup.AddMember(memberID, memberType, ""); err != nil {
		return err
	}

	if _, err := s.GroupRepository.UpdateGroup(ctx, rootGroup); err != nil {
		return ErrDatabaseError
	}

	if s.AuditService != nil {
		rootMember, _ := rootGroup.GetMemberByID(memberID)
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.member.added",
			TargetId:   rootGroupID,
			Details: map[string]interface{}{
				"member":     rootMember,
				"propagated": true,
			},
		})
	}

	return nil
}

// cascadeRemoveMemberFromDescendants removes a member from all descendant groups of a root group.
// It performs best-effort updates and returns removed descendant IDs and failed descendant IDs.
func (s *Service) cascadeRemoveMemberFromDescendants(ctx context.Context, rootGroupID, memberID string) ([]string, []string) {
	descendants, err := s.GroupRepository.GetGroupsByLineageAncestor(ctx, rootGroupID)
	if err != nil {
		logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "remove-member")).Warn(
			"failed-to-load-descendants-for-member-removal",
			zap.Error(err),
			zap.String("root_group_id", rootGroupID),
			zap.String("member_id", memberID),
		)
		return []string{}, []string{rootGroupID}
	}

	removedGroupIDs := []string{}
	failedGroupIDs := []string{}
	for i := range descendants {
		descendant := &descendants[i]

		if !descendant.HasMember(memberID) {
			continue
		}

		if err := s.GroupRepository.RemoveMemberFromGroup(ctx, descendant.ID, memberID); err != nil {
			failedGroupIDs = append(failedGroupIDs, descendant.ID)
			continue
		}

		if descendant.OwnerID == memberID {
			if err := s.GroupRepository.ClearOwnerFromGroup(ctx, descendant.ID, memberID); err != nil {
				failedGroupIDs = append(failedGroupIDs, descendant.ID)
				continue
			}
		}

		removedGroupIDs = append(removedGroupIDs, descendant.ID)
	}

	return removedGroupIDs, failedGroupIDs
}

// getRootGroupID extracts the root group ID from a root-first lineage slice.
func (s *Service) getRootGroupID(lineage []string) (string, error) {
	if len(lineage) == 0 || strings.TrimSpace(lineage[0]) == "" {
		return "", ErrInvalidGroupID
	}

	return lineage[0], nil
}

// resolveUserRoleForGroup resolves a user's strongest direct role for a group.
// Ownership implies OWNER role and admin privileges.
func (s *Service) resolveUserRoleForGroup(group *UniversalGroup, userID string) (string, bool) {
	if group == nil || strings.TrimSpace(userID) == "" {
		return "", false
	}

	maxRole := ""
	if strings.TrimSpace(group.OwnerID) == userID {
		maxRole = MemberRoleOwner
	}

	for i := range group.Members {
		member := group.Members[i]
		if member.ID != userID || member.Type != MemberTypeUser {
			continue
		}

		role := strings.ToUpper(strings.TrimSpace(member.Role))
		if role == "" {
			role = MemberRoleMember
		}

		maxRole = s.pickHigherRole(maxRole, role)
	}

	if maxRole == "" {
		return "", false
	}

	return maxRole, s.isAdminRole(maxRole)
}

// isAdminRole reports whether a role grants admin-level access.
func (s *Service) isAdminRole(role string) bool {
	normalisedRole := strings.ToUpper(strings.TrimSpace(role))
	return normalisedRole == MemberRoleOwner ||
		normalisedRole == MemberRoleAdmin ||
		normalisedRole == MemberRoleSuperUser
}

// pickHigherRole returns the higher-privilege role between current and candidate.
//
// This is a candidate from moving to the config so that when using ghatd, it can
// be costumised to meet different needs.
func (s *Service) pickHigherRole(currentRole, candidateRole string) string {
	normalisedCurrent := strings.ToUpper(strings.TrimSpace(currentRole))
	normalisedCandidate := strings.ToUpper(strings.TrimSpace(candidateRole))

	if normalisedCurrent == "" {
		return normalisedCandidate
	}

	if normalisedCandidate == "" {
		return normalisedCurrent
	}

	rolePriority := map[string]int{
		MemberRoleOwner:       100,
		MemberRoleAdmin:       90,
		MemberRoleSuperUser:   80,
		MemberRoleHead:        70,
		MemberRoleLead:        65,
		MemberRoleCoordinator: 60,
		MemberRoleModerator:   50,
		MemberRoleMember:      40,
		MemberRoleGuest:       10,
	}

	currentPriority := rolePriority[normalisedCurrent]
	candidatePriority := rolePriority[normalisedCandidate]

	if candidatePriority > currentPriority {
		return normalisedCandidate
	}

	return normalisedCurrent
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
		return ErrInvalidMemberRole
	}

	// If no override for group type, check DefaultRoles
	if len(cfg.DefaultRoles) > 0 {
		for _, defaultRole := range cfg.DefaultRoles {
			if defaultRole == role {
				return nil // Role is valid
			}
		}
		return ErrInvalidMemberRole
	}

	// If no default roles defined, allow any role (backward compatibility)
	return nil
}

// GetGroupMembers retrieves members of a group with optional filters
func (s *Service) GetGroupMembers(ctx context.Context, req *GetGroupMembersRequest) (*GetGroupMembersResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-group-members"))
	logger.Debug("getting-group-members", zap.String("group_id", req.GroupID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		logger.Error("failed-to-get-group", zap.Error(err))
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

// UpdateOwner updates group ownership
func (s *Service) UpdateOwner(ctx context.Context, req *UpdateOwnerRequest) (*UpdateOwnerResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "update-owner"))
	logger.Debug("updating-group-owner", zap.String("group_id", req.GroupID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		logger.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Update owner field
	hasChanges := false
	if req.OwnerID != nil && *req.OwnerID != group.OwnerID {
		nextOwnerID := strings.TrimSpace(*req.OwnerID)
		if nextOwnerID != "" {
			if len(group.Lineage) > 0 {
				if err := s.ensureRootMembership(ctx, group, nextOwnerID, MemberTypeUser); err != nil {
					logger.Error("failed-to-ensure-owner-root-membership", zap.Error(err), zap.String("group_id", req.GroupID), zap.String("owner_id", nextOwnerID))
					return nil, err
				}
			}

			// Owner must be a member of the target group and hold ADMIN role.
			if !group.HasMember(nextOwnerID) {
				if _, err := group.AddMember(nextOwnerID, MemberTypeUser, MemberRoleAdmin); err != nil {
					logger.Error("failed-to-add-owner-to-group", zap.Error(err), zap.String("group_id", req.GroupID), zap.String("owner_id", nextOwnerID))
					return nil, err
				}
			} else {
				memberInfo, memberErr := group.GetMemberByID(nextOwnerID)
				if memberErr != nil {
					logger.Error("failed-to-get-owner-member", zap.Error(memberErr), zap.String("group_id", req.GroupID), zap.String("owner_id", nextOwnerID))
					return nil, memberErr
				}

				if memberInfo.Role != MemberRoleAdmin {
					if _, err := group.UpdateMemberRole(nextOwnerID, MemberRoleAdmin); err != nil {
						logger.Error("failed-to-promote-owner-member-to-admin", zap.Error(err), zap.String("group_id", req.GroupID), zap.String("owner_id", nextOwnerID))
						return nil, err
					}
				}
			}
		}

		group.OwnerID = nextOwnerID
		hasChanges = true
	}

	if !hasChanges {
		return nil, ErrNoChangesDetected
	}

	// Update timestamps
	group.SetUpdatedAtNow()

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.owner.updated",
			TargetId:   req.GroupID,
		})
	}

	return &UpdateOwnerResponse{Group: updatedGroup}, nil
}

// ArchiveGroup archives a group
func (s *Service) ArchiveGroup(ctx context.Context, req *ArchiveGroupRequest) (*ArchiveGroupResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "archive-group"))
	logger.Debug("archiving-group", zap.String("id", req.ID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Update status to archived
	_, err = group.UpdateStatus(GroupStatusArchived)
	if err != nil {
		logger.Error("failed-to-archive-group", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.archived",
			TargetId:   req.ID,
		})
	}

	return &ArchiveGroupResponse{Group: updatedGroup}, nil
}

// RestoreGroup restores an archived group
func (s *Service) RestoreGroup(ctx context.Context, req *RestoreGroupRequest) (*RestoreGroupResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "restore-group"))
	logger.Debug("restoring-group", zap.String("id", req.ID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, req.ID)
	if err != nil {
		logger.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Update status to active
	_, err = group.UpdateStatus(GroupStatusActive)
	if err != nil {
		logger.Error("failed-to-restore-group", zap.Error(err))
		return nil, err
	}

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "group.restored",
			TargetId:   req.ID,
		})
	}

	return &RestoreGroupResponse{Group: updatedGroup}, nil
}

// GetGroupsAwaitingAnswerForInvitationsByMemberID retrieves all groups with
// pending invitations for the provided member ID.
func (s *Service) GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx context.Context, req *GetGroupsAwaitingAnswerForInvitationsByMemberIDRequest) (*GetGroupsAwaitingAnswerForInvitationsByMemberIDResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-groups-awaiting-answer-for-invitations-by-member-id"))
	memberID := strings.TrimSpace(req.MemberID)
	logger.Debug("getting-groups-awaiting-answer-for-invitations-by-member-id", zap.String("member_id", memberID))

	if memberID == "" {
		return nil, ErrInvalidMemberID
	}

	groups, err := s.GroupRepository.GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx, memberID)
	if err != nil {
		logger.Error("failed-to-get-groups-awaiting-answer-for-invitations-by-member-id", zap.Error(err), zap.String("member_id", memberID))
		return nil, ErrDatabaseError
	}

	filtered := make([]*UniversalGroup, 0, len(groups))
	for i := range groups {
		group := &groups[i]
		group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

		if !group.HasMember(memberID) {
			continue
		}

		member, err := group.GetMemberByID(memberID)
		if err != nil {
			continue
		}

		if !isPendingInviteMember(*member) {
			continue
		}

		if req.PrefixName {
			group.Name = s.getPrefixName(ctx, group, map[string]string{})
		}

		filtered = append(filtered, group)
	}

	return &GetGroupsAwaitingAnswerForInvitationsByMemberIDResponse{Groups: filtered}, nil
}

// GetLatestNotificationOverviews returns latest invite notifications for the request user.
func (s *Service) GetLatestNotificationOverviews(ctx context.Context, req *common.GetLatestNotificationOverviewsRequest) (*common.GetLatestNotificationOverviewsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-latest-notification-overviews"))
	inviteEmail := strings.TrimSpace(req.UserEmail)
	if inviteEmail == "" {
		return &common.GetLatestNotificationOverviewsResponse{Overviews: []common.NotificationOverview{}}, nil
	}

	kinds := toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.Kinds)
	if len(kinds) == 0 {
		kinds = []string{string(common.NotificationKindGroupInviteOutstanding)}
	}

	includePendingInvites := false
	for _, kind := range kinds {
		if common.NotificationKind(strings.ToLower(strings.TrimSpace(kind))) == common.NotificationKindGroupInviteOutstanding {
			includePendingInvites = true
			break
		}
	}

	if !includePendingInvites {
		return &common.GetLatestNotificationOverviewsResponse{Overviews: []common.NotificationOverview{}}, nil
	}

	groupsResponse, err := s.GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx, &GetGroupsAwaitingAnswerForInvitationsByMemberIDRequest{
		MemberID:   inviteEmail,
		PrefixName: true,
	})
	if err != nil {
		logger.Error("failed-to-get-groups-awaiting-invite-answer-for-notifications", zap.Error(err), zap.String("invite_email", inviteEmail))
		return nil, err
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	overviews := make([]common.NotificationOverview, 0, len(groupsResponse.Groups))
	for i := range groupsResponse.Groups {
		groupItem := groupsResponse.Groups[i]
		member, memberErr := groupItem.GetMemberByID(inviteEmail)
		if memberErr != nil || !isPendingInviteMember(*member) {
			continue
		}

		overviews = append(overviews, common.NotificationOverview{
			ID:                groupItem.ID,
			Source:            common.NotificationSourceGroup,
			Kind:              common.NotificationKindGroupInviteOutstanding,
			Title:             groupItem.Name,
			NotificationTitle: "Outstanding group invitation",
			OccurredAt:        member.InvitedAt,
			UpdatedAt:         member.InvitedAt,
			// We can hardcode the URL path for now
			Href:         "/settings#invitations",
			RevisionHash: fmt.Sprintf("group-invite:%s:%s", groupItem.ID, pendingInviteEmail(*member)),
			Metadata: map[string]interface{}{
				"group_id":         groupItem.ID,
				"group_name":       groupItem.Name,
				"group_type":       groupItem.Type,
				"invite_email":     pendingInviteEmail(*member),
				"invitation_state": member.InvitationState,
				"invited_at":       member.InvitedAt,
				"member_role":      member.Role,
			},
		})

		if len(overviews) >= limit {
			break
		}
	}

	return &common.GetLatestNotificationOverviewsResponse{Overviews: overviews}, nil
}

// GetGroupsByMemberID retrieves groups that contain a specific member
func (s *Service) GetGroupsByMemberID(ctx context.Context, req *GetGroupsRequest) (*GetGroupsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-groups-by-member-id"))
	logger.Debug("getting-groups-by-member-id", zap.String("member_id", req.MemberID))

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
		logger.Error("failed-to-get-total-groups-count-by-member-id", zap.Error(err))
		return nil, ErrDatabaseError
	}

	req.TotalCount = int(totalGroups)
	logger.Debug("handling-get-groups-by-member-id-total-groups-found", zap.Int64("total", totalGroups), zap.Any("request", safeLogValue(req)))

	groups, err := s.GroupRepository.GetGroupsByMemberID(ctx, req.MemberID, req.MemberType, req.Page, req.PageSize)
	if err != nil {
		logger.Error("failed-to-get-groups-by-member-id", zap.Error(err))
		return nil, ErrDatabaseError
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
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-groups-by-leader-id"))
	logger.Debug("getting-groups-by-leader-id", zap.String("owner_id", req.OwnerID))

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
		logger.Error("failed-to-get-total-groups-count-by-leader-id", zap.Error(err))
		return nil, ErrDatabaseError
	}

	req.TotalCount = int(totalGroups)
	logger.Debug("handling-get-groups-by-leader-id-total-groups-found", zap.Int64("total", totalGroups), zap.Any("request", safeLogValue(req)))

	leaderID := req.OwnerID

	groups, err := s.GroupRepository.GetGroupsByLeaderID(ctx, leaderID, req.Page, req.PageSize)
	if err != nil {
		logger.Error("failed-to-get-groups-by-leader-id", zap.Error(err))
		return nil, ErrDatabaseError
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
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-group-stats"))
	logger.Debug("getting-group-stats", zap.String("group_id", groupID))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, groupID)
	if err != nil {
		logger.Error("failed-to-get-group", zap.Error(err))
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
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-groups-stats"))

	stats, err := s.GroupRepository.GetGroupsStatsCounts(ctx)
	if err != nil {
		logger.Error("failed-to-get-groups-stats-counts", zap.Error(err))
		return nil, ErrDatabaseError
	}

	response := &GetGroupsStatsResponse{AllGroupsStats: stats}
	response.NormaliseGroupsStatsKeysToSnakeCase()

	return response, nil
}

// ValidateGroupName returns what RawName and Name would be for a given input
// without persisting anything.  It mirrors the name-resolution logic inside
// CreateGroup so that front-end forms can show live feedback to the user.
func (s *Service) ValidateGroupName(ctx context.Context, req *ValidateGroupNameRequest) (*ValidateGroupNameResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/group", "validate-group-name")
	logger.Debug("handling-validate-group-name-request")

	trimmedRawName := strings.TrimSpace(req.Name)
	trimmedParentGroupID := strings.TrimSpace(req.ParentGroupID)
	baseName, err := toolbox.StringConvertToKebabCase(trimmedRawName)
	if err != nil {
		return nil, ErrValidationFailed
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
		Config: cfg.toGroupConfigCapabilities(),
	}, nil
}

// RepairInvalidMembers repairs groups that contain members with empty or null IDs
// and returns affected group IDs before and after the repair.
func (s *Service) RepairInvalidMembers(ctx context.Context) (*RepairInvalidMembersResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "repair-invalid-members"))

	beforeAffectedGroupIDs, err := s.GroupRepository.GetGroupIDsWithInvalidMembers(ctx)
	if err != nil {
		logger.Error("failed-to-get-groups-with-invalid-members-before-repair", zap.Error(err))
		return nil, ErrDatabaseError
	}

	if err := s.GroupRepository.RepairInvalidMembers(ctx); err != nil {
		logger.Error("failed-to-repair-invalid-members", zap.Error(err))
		return nil, ErrDatabaseError
	}

	afterAffectedGroupIDs, err := s.GroupRepository.GetGroupIDsWithInvalidMembers(ctx)
	if err != nil {
		logger.Error("failed-to-get-groups-with-invalid-members-after-repair", zap.Error(err))
		return nil, ErrDatabaseError
	}

	afterSet := make(map[string]struct{}, len(afterAffectedGroupIDs))
	for _, groupID := range afterAffectedGroupIDs {
		afterSet[groupID] = struct{}{}
	}

	repairedGroupIDs := make([]string, 0, len(beforeAffectedGroupIDs))
	for _, groupID := range beforeAffectedGroupIDs {
		if _, stillAffected := afterSet[groupID]; !stillAffected {
			repairedGroupIDs = append(repairedGroupIDs, groupID)
		}
	}

	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "groups.members.repaired",
			Details: map[string]interface{}{
				"before_affected_group_ids": beforeAffectedGroupIDs,
				"after_affected_group_ids":  afterAffectedGroupIDs,
				"repaired_group_ids":        repairedGroupIDs,
			},
		})
	}

	return &RepairInvalidMembersResponse{
		BeforeAffectedGroupIDs: beforeAffectedGroupIDs,
		AfterAffectedGroupIDs:  afterAffectedGroupIDs,
		RepairedGroupIDs:       repairedGroupIDs,
		BeforeCount:            len(beforeAffectedGroupIDs),
		AfterCount:             len(afterAffectedGroupIDs),
		RepairedCount:          len(repairedGroupIDs),
	}, nil
}

// SetGroupExtension sets an extension field value
func (s *Service) SetGroupExtension(ctx context.Context, groupID, key string, value interface{}) (*UpdateGroupResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "set-group-extension"))
	logger.Debug("setting-group-extension", zap.String("group_id", groupID), zap.String("key", key))

	// Get group
	group, err := s.GroupRepository.GetGroupByID(ctx, groupID)
	if err != nil {
		logger.Error("failed-to-get-group", zap.Error(err))
		return nil, err
	}

	// Reinject dependencies
	group.SetDependencies(s.Config, s.IDGenerator, s.TimeProvider, s.StringUtils)

	// Set extension
	group.SetExtension(key, value)

	// Save to repository
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group-in-repository", zap.Error(err))
		return nil, ErrDatabaseError
	}

	return &UpdateGroupResponse{Group: updatedGroup}, nil
}

// SearchGroupsByExtension searches for groups by extension field value
func (s *Service) SearchGroupsByExtension(ctx context.Context, req *GetGroupsRequest) (*GetGroupsResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "search-groups-by-extension"))
	logger.Debug("searching-groups-by-extension", zap.Any("extension_filters", safeLogValue(req.ExtensionFilters)))

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
		logger.Error("failed-to-get-total-groups-count-by-extension", zap.Error(err))
		return nil, ErrDatabaseError
	}

	req.TotalCount = int(totalGroups)
	logger.Debug("handling-search-groups-by-extension-total-groups-found", zap.Int64("total", totalGroups), zap.Any("request", safeLogValue(req)))

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
		logger.Error("failed-to-search-groups-by-extension", zap.Error(err))
		return nil, ErrDatabaseError
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
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "bulk-update-groups-status"))
	logger.Debug("bulk-updating-group-status", zap.Int("count", len(groupIDs)), zap.String("status", status))

	err := s.GroupRepository.BulkUpdateGroupsStatus(ctx, groupIDs, status)
	if err != nil {
		logger.Error("failed-to-bulk-update-groups-status", zap.Error(err))
		return ErrDatabaseError
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetType: "group",
			Domain:     "group",
			Action:     "groups.bulk_status_updated",
			Details: map[string]interface{}{
				"group_ids": groupIDs,
				"status":    status,
			},
		})
	}

	return nil
}

// GetParentGroupsWithAutoJoinForEmail retrieves all parent groups that have
// auto-join or auto-invite enabled for the email domain matching the provided email.
func (s *Service) GetParentGroupsWithAutoJoinForEmail(ctx context.Context, email string) (*GetParentGroupsWithAutoJoinForEmailResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "get-parent-groups-with-auto-join-for-email"))

	trimmedEmail := strings.TrimSpace(email)
	if trimmedEmail == "" {
		logger.Error("email-is-required")
		return nil, ErrGroupEmailIsRequired
	}

	// Extract domain from email
	emailDomain := extractEmailDomain(trimmedEmail)
	if emailDomain == "" {
		logger.Error("invalid-email-format", zap.String("email", trimmedEmail))
		return nil, ErrGroupInvalidEmailFormat
	}

	logger.Debug("querying-groups-with-auto-join", zap.String("email_domain", emailDomain))

	// Get all groups (this is a broad query; in production you might optimize this)
	// by adding repository support for filtered queries on auto-join settings
	allGroups, err := s.GroupRepository.GetGroups(ctx, &GetGroupsRequest{
		Statuses: []string{GroupStatusActive},
	})
	if err != nil {
		logger.Error("failed-to-retrieve-groups", zap.Error(err))
		return nil, err
	}

	var matchedGroups []*UniversalGroup

	for i := range allGroups {
		group := &allGroups[i]

		// Skip if no settings
		if group.Settings == nil {
			continue
		}

		// Check if auto-join or auto-invite is enabled
		if !group.Settings.AutoJoinByEmailDomainEnabled && !group.Settings.AutoInviteByEmailDomainEnabled {
			continue
		}

		// Check if email domain matches any of the configured domains
		if matchesAnyDomain(emailDomain, group.Settings.AutoActionEmailDomains) {
			matchedGroups = append(matchedGroups, group)
		}
	}

	logger.Debug("found-matching-groups-with-auto-join", zap.Int("count", len(matchedGroups)))

	return &GetParentGroupsWithAutoJoinForEmailResponse{
		Groups: matchedGroups,
	}, nil
}

// EnableGroupAutoJoinByEmailDomain enables auto-join for a group with specified email domains.
func (s *Service) EnableGroupAutoJoinByEmailDomain(ctx context.Context, req *EnableGroupAutoJoinByEmailDomainRequest) (*EnableGroupAutoJoinByEmailDomainResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "enable-group-auto-join-by-email-domain"))

	if req == nil || strings.TrimSpace(req.GroupID) == "" {
		logger.Error("group-id-is-required")
		return nil, ErrInvalidGroupID
	}

	groupID := strings.TrimSpace(req.GroupID)

	// Retrieve the group
	group, err := s.GroupRepository.GetGroupByID(ctx, groupID)
	if err != nil {
		logger.Error("failed-to-retrieve-group", zap.String("group_id", groupID), zap.Error(err))
		return nil, err
	}

	if group == nil {
		logger.Error("group-not-found", zap.String("group_id", groupID))
		return nil, ErrResourceNotFound
	}

	// Ensure settings exist
	if group.Settings == nil {
		group.Settings = &GroupSettings{}
	}

	// Set the auto-join settings
	group.Settings.AutoActionEmailDomains = req.AutoActionEmailDomains
	group.Settings.AutoJoinByEmailDomainEnabled = true
	group.Settings.AutoInviteByEmailDomainEnabled = false // Enforce mutual exclusivity

	if req.AutoActionDefaultMemberRole != "" {
		group.Settings.AutoActionDefaultMemberRole = strings.TrimSpace(req.AutoActionDefaultMemberRole)
	} else {
		// Set to default role if not specified
		group.Settings.AutoActionDefaultMemberRole = MemberRoleMember
	}

	// Normalize settings
	normalizedSettings := group.Settings.NormalizeAutoActionConfig()
	if normalizedSettings != nil {
		group.Settings = normalizedSettings
	}

	// Validate settings
	if err := group.Settings.ValidateAutoActionConfig(); err != nil {
		logger.Error("validation-failed", zap.Error(err))
		return nil, err
	}

	// Update the group
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group", zap.String("group_id", groupID), zap.Error(err))
		return nil, err
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetId:   groupID,
			TargetType: "group",
			Domain:     "group",
			Action:     "group.auto_join_enabled",
			Details: map[string]interface{}{
				"auto_action_email_domains":       req.AutoActionEmailDomains,
				"auto_action_default_member_role": req.AutoActionDefaultMemberRole,
			},
		})
	}

	logger.Debug("successfully-enabled-auto-join", zap.String("group_id", groupID))

	return &EnableGroupAutoJoinByEmailDomainResponse{
		Group: updatedGroup,
	}, nil
}

// DisableGroupAutoJoinByEmailDomain disables auto-join for a group.
func (s *Service) DisableGroupAutoJoinByEmailDomain(ctx context.Context, req *DisableGroupAutoJoinByEmailDomainRequest) (*DisableGroupAutoJoinByEmailDomainResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "disable-group-auto-join-by-email-domain"))

	if req == nil || strings.TrimSpace(req.GroupID) == "" {
		logger.Error("group-id-is-required")
		return nil, ErrInvalidGroupID
	}

	groupID := strings.TrimSpace(req.GroupID)

	// Retrieve the group
	group, err := s.GroupRepository.GetGroupByID(ctx, groupID)
	if err != nil {
		logger.Error("failed-to-retrieve-group", zap.String("group_id", groupID), zap.Error(err))
		return nil, err
	}

	if group == nil {
		logger.Error("group-not-found", zap.String("group_id", groupID))
		return nil, ErrResourceNotFound
	}

	// Ensure settings exist
	if group.Settings == nil {
		group.Settings = &GroupSettings{}
	}

	// Disable auto-join
	group.Settings.AutoJoinByEmailDomainEnabled = false
	group.Settings.AutoActionEmailDomains = []string{}
	group.Settings.AutoActionDefaultMemberRole = ""

	// Update the group
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group", zap.String("group_id", groupID), zap.Error(err))
		return nil, err
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetId:   groupID,
			TargetType: "group",
			Domain:     "group",
			Action:     "group.auto_join_disabled",
		})
	}

	logger.Debug("successfully-disabled-auto-join", zap.String("group_id", groupID))

	return &DisableGroupAutoJoinByEmailDomainResponse{
		Group: updatedGroup,
	}, nil
}

// EnableGroupAutoInviteByEmailDomain enables auto-invite for a group with specified email domains.
func (s *Service) EnableGroupAutoInviteByEmailDomain(ctx context.Context, req *EnableGroupAutoInviteByEmailDomainRequest) (*EnableGroupAutoInviteByEmailDomainResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "enable-group-auto-invite-by-email-domain"))

	if req == nil || strings.TrimSpace(req.GroupID) == "" {
		logger.Error("group-id-is-required")
		return nil, ErrInvalidGroupID
	}

	groupID := strings.TrimSpace(req.GroupID)

	// Retrieve the group
	group, err := s.GroupRepository.GetGroupByID(ctx, groupID)
	if err != nil {
		logger.Error("failed-to-retrieve-group", zap.String("group_id", groupID), zap.Error(err))
		return nil, err
	}

	if group == nil {
		logger.Error("group-not-found", zap.String("group_id", groupID))
		return nil, ErrResourceNotFound
	}

	// Ensure settings exist
	if group.Settings == nil {
		group.Settings = &GroupSettings{}
	}

	// Set the auto-invite settings
	group.Settings.AutoInviteByEmailDomainEnabled = true
	group.Settings.AutoJoinByEmailDomainEnabled = false // Enforce mutual exclusivity
	group.Settings.AutoActionEmailDomains = req.AutoActionEmailDomains

	if req.AutoActionDefaultMemberRole != "" {
		group.Settings.AutoActionDefaultMemberRole = strings.TrimSpace(req.AutoActionDefaultMemberRole)
	} else {
		// Set to default role if not specified
		group.Settings.AutoActionDefaultMemberRole = MemberRoleMember
	}

	// Normalize settings
	normalizedSettings := group.Settings.NormalizeAutoActionConfig()
	if normalizedSettings != nil {
		group.Settings = normalizedSettings
	}

	// Validate settings
	if err := group.Settings.ValidateAutoActionConfig(); err != nil {
		logger.Error("validation-failed", zap.Error(err))
		return nil, err
	}

	// Update the group
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group", zap.String("group_id", groupID), zap.Error(err))
		return nil, err
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetId:   groupID,
			TargetType: "group",
			Domain:     "group",
			Action:     "group.auto_invite_enabled",
			Details: map[string]interface{}{
				"auto_action_email_domains":       req.AutoActionEmailDomains,
				"auto_action_default_member_role": req.AutoActionDefaultMemberRole,
			},
		})
	}

	logger.Debug("successfully-enabled-auto-invite", zap.String("group_id", groupID))

	return &EnableGroupAutoInviteByEmailDomainResponse{
		Group: updatedGroup,
	}, nil
}

// DisableGroupAutoInviteByEmailDomain disables auto-invite for a group.
func (s *Service) DisableGroupAutoInviteByEmailDomain(ctx context.Context, req *DisableGroupAutoInviteByEmailDomainRequest) (*DisableGroupAutoInviteByEmailDomainResponse, error) {
	logger := logger.AcquirePackageFrom(ctx, "external/group").With(zap.String("operation", "disable-group-auto-invite-by-email-domain"))

	if req == nil || strings.TrimSpace(req.GroupID) == "" {
		logger.Error("group-id-is-required")
		return nil, ErrInvalidGroupID
	}

	groupID := strings.TrimSpace(req.GroupID)

	// Retrieve the group
	group, err := s.GroupRepository.GetGroupByID(ctx, groupID)
	if err != nil {
		logger.Error("failed-to-retrieve-group", zap.String("group_id", groupID), zap.Error(err))
		return nil, err
	}

	if group == nil {
		logger.Error("group-not-found", zap.String("group_id", groupID))
		return nil, ErrResourceNotFound
	}

	// Ensure settings exist
	if group.Settings == nil {
		group.Settings = &GroupSettings{}
	}

	// Disable auto-invite
	group.Settings.AutoInviteByEmailDomainEnabled = false
	group.Settings.AutoActionEmailDomains = []string{}
	group.Settings.AutoActionDefaultMemberRole = ""

	// Update the group
	updatedGroup, err := s.GroupRepository.UpdateGroup(ctx, group)
	if err != nil {
		logger.Error("failed-to-update-group", zap.String("group_id", groupID), zap.Error(err))
		return nil, err
	}

	// Audit log
	if s.AuditService != nil {
		s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			TargetId:   groupID,
			TargetType: "group",
			Domain:     "group",
			Action:     "group.auto_invite_disabled",
		})
	}

	logger.Debug("successfully-disabled-auto-invite", zap.String("group_id", groupID))

	return &DisableGroupAutoInviteByEmailDomainResponse{
		Group: updatedGroup,
	}, nil
}

// extractEmailDomain extracts the domain from an email address.
// Returns empty string if email is invalid.
func extractEmailDomain(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}

	domain := strings.TrimSpace(parts[1])
	if domain == "" {
		return ""
	}

	return domain
}

// matchesAnyDomain checks if the given email domain matches any of the configured domains (exact match, case-insensitive).
func matchesAnyDomain(emailDomain string, configuredDomains []string) bool {
	if emailDomain == "" || len(configuredDomains) == 0 {
		return false
	}

	lowerEmailDomain := strings.ToLower(emailDomain)
	for _, configuredDomain := range configuredDomains {
		if strings.ToLower(strings.TrimSpace(configuredDomain)) == lowerEmailDomain {
			return true
		}
	}

	return false
}
