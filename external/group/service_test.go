package group_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/group"
)

func stringPtr(s string) *string {
	return &s
}

// mockGroupRepository implements group.GroupRepository for service tests
type mockGroupRepository struct {
	createGroupFunc                 func(ctx context.Context, grp *group.UniversalGroup) (*group.UniversalGroup, error)
	getGroupByIDFunc                func(ctx context.Context, id string) (*group.UniversalGroup, error)
	getGroupByNanoIDFunc            func(ctx context.Context, nanoID string) (*group.UniversalGroup, error)
	getGroupByNameFunc              func(ctx context.Context, name, groupType string, logError bool) (*group.UniversalGroup, error)
	getGroupByNameAndParentFunc     func(ctx context.Context, name, parentGroupID string, logError bool) (*group.UniversalGroup, error)
	getGroupsByLineageAncestorFunc  func(ctx context.Context, ancestorGroupID string) ([]group.UniversalGroup, error)
	updateGroupFunc                 func(ctx context.Context, group *group.UniversalGroup) (*group.UniversalGroup, error)
	deleteGroupByIDFunc             func(ctx context.Context, id string) error
	softDeleteGroupFunc             func(ctx context.Context, id, deletedByID string, deletedAt string) error
	getGroupsFunc                   func(ctx context.Context, req *group.GetGroupsRequest) ([]group.UniversalGroup, error)
	getTotalGroupsFunc              func(ctx context.Context, req *group.GetGroupsRequest) (int64, error)
	getGroupsByReferencedUserIDFunc func(ctx context.Context, userID string) ([]group.UniversalGroup, error)
	removeMemberFromGroupFunc       func(ctx context.Context, groupID, memberID string) error
	clearOwnerFromGroupFunc         func(ctx context.Context, groupID, ownerID string) error
}

func (m *mockGroupRepository) CreateGroup(ctx context.Context, grp *group.UniversalGroup) (*group.UniversalGroup, error) {
	if m.createGroupFunc != nil {
		return m.createGroupFunc(ctx, grp)
	}
	return grp, nil
}

func (m *mockGroupRepository) GetGroupByID(ctx context.Context, id string) (*group.UniversalGroup, error) {
	if m.getGroupByIDFunc != nil {
		return m.getGroupByIDFunc(ctx, id)
	}
	return nil, group.ErrResourceNotFound
}

func (m *mockGroupRepository) GetGroupByName(ctx context.Context, name, groupType string, logError bool) (*group.UniversalGroup, error) {
	if m.getGroupByNameFunc != nil {
		return m.getGroupByNameFunc(ctx, name, groupType, logError)
	}
	return nil, nil
}

func (m *mockGroupRepository) UpdateGroup(ctx context.Context, grp *group.UniversalGroup) (*group.UniversalGroup, error) {
	if m.updateGroupFunc != nil {
		return m.updateGroupFunc(ctx, grp)
	}
	return grp, nil
}

func (m *mockGroupRepository) DeleteGroupByID(ctx context.Context, id string) error {
	if m.deleteGroupByIDFunc != nil {
		return m.deleteGroupByIDFunc(ctx, id)
	}
	return nil
}

func (m *mockGroupRepository) GetGroups(ctx context.Context, req *group.GetGroupsRequest) ([]group.UniversalGroup, error) {
	if m.getGroupsFunc != nil {
		return m.getGroupsFunc(ctx, req)
	}
	return []group.UniversalGroup{}, nil
}

func (m *mockGroupRepository) GetTotalGroups(ctx context.Context, req *group.GetGroupsRequest) (int64, error) {
	if m.getTotalGroupsFunc != nil {
		return m.getTotalGroupsFunc(ctx, req)
	}
	return 0, nil
}

// Stub/implement methods
func (m *mockGroupRepository) GetGroupByNanoID(ctx context.Context, nanoID string) (*group.UniversalGroup, error) {
	if m.getGroupByNanoIDFunc != nil {
		return m.getGroupByNanoIDFunc(ctx, nanoID)
	}
	return nil, group.ErrResourceNotFound
}

func (m *mockGroupRepository) GetGroupByNameAndParent(ctx context.Context, name, parentGroupID string, logError bool) (*group.UniversalGroup, error) {
	if m.getGroupByNameAndParentFunc != nil {
		return m.getGroupByNameAndParentFunc(ctx, name, parentGroupID, logError)
	}
	return nil, nil
}

func (m *mockGroupRepository) GetGroupsByLineageAncestor(ctx context.Context, ancestorGroupID string) ([]group.UniversalGroup, error) {
	if m.getGroupsByLineageAncestorFunc != nil {
		return m.getGroupsByLineageAncestorFunc(ctx, ancestorGroupID)
	}
	return []group.UniversalGroup{}, nil
}

func (m *mockGroupRepository) SoftDeleteGroup(ctx context.Context, id, deletedByID string, deletedAt string) error {
	if m.softDeleteGroupFunc != nil {
		return m.softDeleteGroupFunc(ctx, id, deletedByID, deletedAt)
	}
	return nil
}

func (m *mockGroupRepository) GetGroupsByType(ctx context.Context, groupType string, page, pageSize int, order string) ([]group.UniversalGroup, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupRepository) GetGroupsByStatus(ctx context.Context, status string, page, pageSize int, order string) ([]group.UniversalGroup, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupRepository) GetGroupsByReferencedUserID(ctx context.Context, userID string) ([]group.UniversalGroup, error) {
	if m.getGroupsByReferencedUserIDFunc != nil {
		return m.getGroupsByReferencedUserIDFunc(ctx, userID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGroupRepository) GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx context.Context, memberID string) ([]group.UniversalGroup, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupRepository) GetGroupsByMemberID(ctx context.Context, memberID string, memberType string, page, pageSize int) ([]group.UniversalGroup, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupRepository) GetGroupsByLeaderID(ctx context.Context, leaderID string, page, pageSize int) ([]group.UniversalGroup, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupRepository) SearchGroupsByExtension(ctx context.Context, key string, value interface{}, page, pageSize int) ([]group.UniversalGroup, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupRepository) HasGroupDependents(ctx context.Context, groupID string) (bool, error) {
	return false, nil
}

func (m *mockGroupRepository) AddMemberToGroup(ctx context.Context, groupID string, member group.Member) error {
	return errors.New("not implemented")
}

func (m *mockGroupRepository) RemoveMemberFromGroup(ctx context.Context, groupID, memberID string) error {
	if m.removeMemberFromGroupFunc != nil {
		return m.removeMemberFromGroupFunc(ctx, groupID, memberID)
	}
	return errors.New("not implemented")
}

func (m *mockGroupRepository) ClearOwnerFromGroup(ctx context.Context, groupID, ownerID string) error {
	if m.clearOwnerFromGroupFunc != nil {
		return m.clearOwnerFromGroupFunc(ctx, groupID, ownerID)
	}
	return errors.New("not implemented")
}

func (m *mockGroupRepository) GetGroupIDsWithInvalidMembers(ctx context.Context) ([]string, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupRepository) RepairInvalidMembers(ctx context.Context) error {
	return errors.New("not implemented")
}

func (m *mockGroupRepository) BulkUpdateGroupsStatus(ctx context.Context, groupIDs []string, status string) error {
	return errors.New("not implemented")
}

func (m *mockGroupRepository) GetGroupsStatsCounts(ctx context.Context) (*group.AllGroupsStats, error) {
	return nil, errors.New("not implemented")
}

// mockAuditService implements group.AuditService
type mockAuditService struct {
	logAuditEventFunc func(ctx context.Context, r *audit.LogAuditEventRequest) error
}

func (m *mockAuditService) LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error {
	if m.logAuditEventFunc != nil {
		return m.logAuditEventFunc(ctx, r)
	}
	return nil
}

func newTestService(repo group.GroupRepository, audit group.AuditService) *group.Service {
	svc, err := group.NewService(
		repo,
		audit,
		group.DefaultGroupConfig(),
		group.NewDefaultIDGenerator(),
		group.NewDefaultTimeProvider(),
		group.NewDefaultStringUtils(),
	)
	if err != nil {
		panic(err)
	}
	return svc
}

func TestService_CreateGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		createReq         *group.CreateGroupRequest
		mockRepositoryErr error
		expectError       bool
		validateName      bool
	}{
		{
			name: "Success - creates new group with valid request",
			createReq: &group.CreateGroupRequest{
				Name:    "Engineering",
				Type:    group.GroupTypeTeam,
				OwnerID: testUserID,
			},
			validateName: true,
			expectError:  false,
		},
		{
			name: "Failure - repository error during creation",
			createReq: &group.CreateGroupRequest{
				Name:    "Engineering",
				Type:    group.GroupTypeTeam,
				OwnerID: testUserID,
			},
			mockRepositoryErr: group.ErrDatabaseError,
			expectError:       true,
		},
		{
			name: "Failure - invalid group type",
			createReq: &group.CreateGroupRequest{
				Name:    "Engineering",
				Type:    "INVALID_TYPE",
				OwnerID: testUserID,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockGroupRepository{
				createGroupFunc: func(ctx context.Context, grp *group.UniversalGroup) (*group.UniversalGroup, error) {
					return grp, tt.mockRepositoryErr
				},
				getGroupByNameFunc: func(ctx context.Context, name, groupType string, logError bool) (*group.UniversalGroup, error) {
					if tt.validateName {
						return nil, nil // Name is unique
					}
					return nil, nil
				},
			}

			svc := newTestService(repo, &mockAuditService{})

			response, err := svc.CreateGroup(context.Background(), tt.createReq)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			// Name is lowercased/kebab-cased, RawName preserves original input
			assert.Equal(t, tt.createReq.Name, response.Group.RawName)
			assert.Equal(t, tt.createReq.Type, response.Group.Type)
			assert.Equal(t, group.GroupStatusActive, response.Group.Status)
		})
	}
}

func TestService_GetGroupByID(t *testing.T) {
	t.Parallel()

	mockGroup := &group.UniversalGroup{
		ID:      testGroupID,
		Name:    testGroupName,
		Type:    testGroupType,
		Status:  group.GroupStatusActive,
		OwnerID: testUserID,
	}

	tests := []struct {
		name              string
		groupID           string
		mockGroup         *group.UniversalGroup
		mockRepositoryErr error
		expectError       bool
	}{
		{
			name:      "Success - retrieves group by ID",
			groupID:   testGroupID,
			mockGroup: mockGroup,
		},
		{
			name:              "Failure - group not found",
			groupID:           testGroupID,
			mockRepositoryErr: group.ErrResourceNotFound,
			expectError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockGroupRepository{
				getGroupByIDFunc: func(ctx context.Context, id string) (*group.UniversalGroup, error) {
					if tt.mockRepositoryErr != nil {
						return nil, tt.mockRepositoryErr
					}
					return tt.mockGroup, nil
				},
			}

			svc := newTestService(repo, &mockAuditService{})

			response, err := svc.GetGroupByID(context.Background(), &group.GetGroupByIDRequest{
				ID: tt.groupID,
			})

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			assert.Equal(t, tt.mockGroup.ID, response.Group.ID)
			assert.Equal(t, tt.mockGroup.Name, response.Group.Name)
		})
	}
}

func TestService_UpdateGroup(t *testing.T) {
	t.Parallel()

	existingGroup := &group.UniversalGroup{
		ID:      testGroupID,
		Name:    testGroupName,
		Type:    testGroupType,
		Status:  group.GroupStatusActive,
		OwnerID: testUserID,
	}

	updatedGroup := &group.UniversalGroup{
		ID:      testGroupID,
		Name:    "Updated Team Name",
		Type:    testGroupType,
		Status:  group.GroupStatusActive,
		OwnerID: testUserID,
	}

	tests := []struct {
		name              string
		updateReq         *group.UpdateGroupRequest
		mockGroup         *group.UniversalGroup
		mockRepositoryErr error
		expectError       bool
	}{
		{
			name: "Success - updates group name",
			updateReq: &group.UpdateGroupRequest{
				ID:   testGroupID,
				Name: stringPtr("Updated Team Name"),
			},
			mockGroup: updatedGroup,
		},
		{
			name: "Failure - group not found",
			updateReq: &group.UpdateGroupRequest{
				ID:   testGroupID,
				Name: stringPtr("Updated Team Name"),
			},
			mockRepositoryErr: group.ErrResourceNotFound,
			expectError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockGroupRepository{
				getGroupByIDFunc: func(ctx context.Context, id string) (*group.UniversalGroup, error) {
					if tt.mockRepositoryErr != nil {
						return nil, tt.mockRepositoryErr
					}
					return existingGroup, nil
				},
				updateGroupFunc: func(ctx context.Context, grp *group.UniversalGroup) (*group.UniversalGroup, error) {
					if tt.mockRepositoryErr != nil {
						return nil, tt.mockRepositoryErr
					}
					return tt.mockGroup, nil
				},
				getGroupByNameFunc: func(ctx context.Context, name, groupType string, logError bool) (*group.UniversalGroup, error) {
					return nil, nil // Assume name is unique
				},
			}

			svc := newTestService(repo, &mockAuditService{})

			response, err := svc.UpdateGroup(context.Background(), &group.UpdateGroupRequest{
				ID:   tt.updateReq.ID,
				Name: tt.updateReq.Name,
			})

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, response)
			if tt.mockGroup != nil {
				assert.Equal(t, tt.mockGroup.Name, response.Group.Name)
			}
		})
	}
}

func TestService_DeleteGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		groupID           string
		mockRepositoryErr error
		expectError       bool
	}{
		{
			name:    "Success - deletes group",
			groupID: testGroupID,
		},
		{
			name:              "Failure - database error during deletion",
			groupID:           testGroupID,
			mockRepositoryErr: group.ErrDatabaseError,
			expectError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockGroupRepository{
				getGroupByIDFunc: func(ctx context.Context, id string) (*group.UniversalGroup, error) {
					if tt.mockRepositoryErr != nil {
						return nil, tt.mockRepositoryErr
					}
					return &group.UniversalGroup{ID: id}, nil
				},
				getGroupsByLineageAncestorFunc: func(ctx context.Context, ancestorGroupID string) ([]group.UniversalGroup, error) {
					return []group.UniversalGroup{}, nil // No descendants
				},
				deleteGroupByIDFunc: func(ctx context.Context, id string) error {
					return tt.mockRepositoryErr
				},
			}

			svc := newTestService(repo, &mockAuditService{})

			_, err := svc.DeleteGroup(context.Background(), &group.DeleteGroupRequest{
				ID: tt.groupID,
			})

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestService_ValidateGroupName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		validateReq       *group.ValidateGroupNameRequest
		mockGroup         *group.UniversalGroup
		mockRepositoryErr error
		expectAdjusted    bool
		expectAvailable   bool
	}{
		{
			name: "Success - name is unique and valid",
			validateReq: &group.ValidateGroupNameRequest{
				Name: "NewTeamName",
				Type: group.GroupTypeTeam,
			},
			mockGroup:       nil, // No group found, so name is unique
			expectAdjusted:  false,
			expectAvailable: true,
		},
		{
			name: "Failure - name already exists",
			validateReq: &group.ValidateGroupNameRequest{
				Name: testGroupName,
				Type: group.GroupTypeTeam,
			},
			mockGroup: &group.UniversalGroup{
				ID:   testGroupID,
				Name: testGroupName,
				Type: group.GroupTypeTeam,
			},
			expectAdjusted:  true, // Will be auto-adjusted with -1 suffix for non-root
			expectAvailable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockGroupRepository{
				getGroupByNameFunc: func(ctx context.Context, name, groupType string, logError bool) (*group.UniversalGroup, error) {
					// Only return mockGroup if checking for the expected kebab-case name
					// The service converts "Test Team" to "test-team"
					if tt.mockGroup != nil && name == "test-team" {
						return tt.mockGroup, tt.mockRepositoryErr
					}
					// For other names or suffixed versions like "test-team-1", they should not exist
					return nil, nil
				},
			}

			svc := newTestService(repo, &mockAuditService{})

			response, err := svc.ValidateGroupName(context.Background(), tt.validateReq)

			require.NoError(t, err)
			require.NotNil(t, response)
			assert.Equal(t, tt.expectAdjusted, response.Adjusted)
			assert.Equal(t, tt.expectAvailable, response.Available)
			if tt.expectAdjusted {
				// When adjusted, name should have suffix like "-1"
				assert.Contains(t, response.Name, "-")
			}
		})
	}
}

func TestService_RemoveUserFromAllGroups(t *testing.T) {
	t.Parallel()

	const (
		rootGroupAlphaID  = "root-alpha"
		childGroupAlphaID = "child-alpha"
		rootGroupBetaID   = "root-beta"
		childGroupBetaID  = "child-beta"
	)

	cloneGroup := func(src *group.UniversalGroup) *group.UniversalGroup {
		if src == nil {
			return nil
		}

		clone := *src
		clone.Lineage = append([]string(nil), src.Lineage...)
		clone.Members = append([]group.Member(nil), src.Members...)
		return &clone
	}

	groupsByID := map[string]*group.UniversalGroup{
		rootGroupAlphaID: {
			ID:      rootGroupAlphaID,
			Name:    "alpha",
			Type:    group.GroupTypeTeam,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
			Members: []group.Member{{
				ID:   testUserID,
				Type: group.MemberTypeUser,
				Role: group.MemberRoleAdmin,
			}},
		},
		childGroupAlphaID: {
			ID:            childGroupAlphaID,
			Name:          "alpha-child",
			Type:          group.GroupTypeTeam,
			Status:        group.GroupStatusActive,
			ParentGroupID: rootGroupAlphaID,
			Lineage:       []string{rootGroupAlphaID},
			Members: []group.Member{{
				ID:   testUserID,
				Type: group.MemberTypeUser,
				Role: group.MemberRoleMember,
			}},
		},
		rootGroupBetaID: {
			ID:     rootGroupBetaID,
			Name:   "beta",
			Type:   group.GroupTypeTeam,
			Status: group.GroupStatusActive,
			Members: []group.Member{{
				ID:   testUserID,
				Type: group.MemberTypeUser,
				Role: group.MemberRoleMember,
			}},
		},
		childGroupBetaID: {
			ID:            childGroupBetaID,
			Name:          "beta-child",
			Type:          group.GroupTypeTeam,
			Status:        group.GroupStatusActive,
			ParentGroupID: rootGroupBetaID,
			Lineage:       []string{rootGroupBetaID},
			Members: []group.Member{{
				ID:   testUserID,
				Type: group.MemberTypeUser,
				Role: group.MemberRoleMember,
			}},
		},
	}

	cascadeQueryRootIDs := make([]string, 0, 2)
	removedMemberships := make([]string, 0, 4)
	clearedOwners := make([]string, 0, 1)

	repo := &mockGroupRepository{
		getGroupsByReferencedUserIDFunc: func(ctx context.Context, userID string) ([]group.UniversalGroup, error) {
			require.Equal(t, testUserID, userID)

			return []group.UniversalGroup{
				*cloneGroup(groupsByID[rootGroupAlphaID]),
				*cloneGroup(groupsByID[childGroupAlphaID]),
				*cloneGroup(groupsByID[childGroupBetaID]),
			}, nil
		},
		getGroupByIDFunc: func(ctx context.Context, id string) (*group.UniversalGroup, error) {
			grp, ok := groupsByID[id]
			if !ok {
				return nil, group.ErrResourceNotFound
			}
			return cloneGroup(grp), nil
		},
		getGroupsByLineageAncestorFunc: func(ctx context.Context, ancestorGroupID string) ([]group.UniversalGroup, error) {
			cascadeQueryRootIDs = append(cascadeQueryRootIDs, ancestorGroupID)

			switch ancestorGroupID {
			case rootGroupAlphaID:
				return []group.UniversalGroup{*cloneGroup(groupsByID[childGroupAlphaID])}, nil
			case rootGroupBetaID:
				return []group.UniversalGroup{*cloneGroup(groupsByID[childGroupBetaID])}, nil
			default:
				return []group.UniversalGroup{}, nil
			}
		},
		removeMemberFromGroupFunc: func(ctx context.Context, groupID, memberID string) error {
			require.Equal(t, testUserID, memberID)
			grp := groupsByID[groupID]
			require.NotNil(t, grp)

			filtered := grp.Members[:0]
			for _, member := range grp.Members {
				if member.ID != memberID {
					filtered = append(filtered, member)
				}
			}
			grp.Members = append([]group.Member(nil), filtered...)
			removedMemberships = append(removedMemberships, groupID)
			return nil
		},
		clearOwnerFromGroupFunc: func(ctx context.Context, groupID, ownerID string) error {
			require.Equal(t, testUserID, ownerID)
			grp := groupsByID[groupID]
			require.NotNil(t, grp)
			grp.OwnerID = ""
			clearedOwners = append(clearedOwners, groupID)
			return nil
		},
	}

	svc := newTestService(repo, &mockAuditService{})

	response, err := svc.RemoveUserFromAllGroups(context.Background(), &group.RemoveUserFromAllGroupsRequest{
		UserID: testUserID,
	})

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.True(t, response.Success)
	assert.Equal(t, 2, response.TotalRootGroupsAffected)
	assert.Equal(t, "User removed from all groups successfully", response.Message)
	assert.ElementsMatch(t, []string{rootGroupAlphaID, rootGroupBetaID}, cascadeQueryRootIDs)
	assert.Contains(t, clearedOwners, rootGroupAlphaID)
	assert.Empty(t, groupsByID[rootGroupAlphaID].OwnerID)
	assert.False(t, groupsByID[rootGroupAlphaID].HasMember(testUserID))
	assert.False(t, groupsByID[childGroupAlphaID].HasMember(testUserID))
	assert.False(t, groupsByID[rootGroupBetaID].HasMember(testUserID))
	assert.False(t, groupsByID[childGroupBetaID].HasMember(testUserID))
	assert.ElementsMatch(t, []string{rootGroupAlphaID, childGroupAlphaID, rootGroupBetaID, childGroupBetaID}, removedMemberships)
}
