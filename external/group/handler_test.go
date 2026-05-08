package group_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/toolbox"
)

const (
	testGroupName     = "Test Team"
	testGroupType     = group.GroupTypeTeam
	testGroupID       = "test-group-id-123"
	testNanoID        = "abc123xyz789"
	testUserID        = "test-user-id-456"
	testMemberID      = "test-member-id-789"
	testParentGroupID = "test-parent-group-id-101"
)

// mockGroupService implements group.GroupService for handler tests
type mockGroupService struct {
	createGroupFunc       func(ctx context.Context, r *group.CreateGroupRequest) (*group.CreateGroupResponse, error)
	getGroupByIDFunc      func(ctx context.Context, r *group.GetGroupByIDRequest) (*group.GetGroupByIDResponse, error)
	updateGroupFunc       func(ctx context.Context, r *group.UpdateGroupRequest) (*group.UpdateGroupResponse, error)
	getGroupsFunc         func(ctx context.Context, r *group.GetGroupsRequest) (*group.GetGroupsResponse, error)
	getGroupsConfigFunc   func(ctx context.Context, r *group.GetGroupsConfigRequest) (*group.GetGroupsConfigResponse, error)
	getGroupsStatsFunc    func(ctx context.Context, r *group.GetGroupsStatsRequest) (*group.GetGroupsStatsResponse, error)
	validateGroupNameFunc func(ctx context.Context, r *group.ValidateGroupNameRequest) (*group.ValidateGroupNameResponse, error)
	getGroupByNanoIDFunc  func(ctx context.Context, r *group.GetGroupByNanoIDRequest) (*group.GetGroupByNanoIDResponse, error)
	deleteGroupFunc       func(ctx context.Context, r *group.DeleteGroupRequest) (*group.DeleteGroupResponse, error)
}

func (m *mockGroupService) CreateGroup(ctx context.Context, r *group.CreateGroupRequest) (*group.CreateGroupResponse, error) {
	if m.createGroupFunc != nil {
		return m.createGroupFunc(ctx, r)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupByID(ctx context.Context, r *group.GetGroupByIDRequest) (*group.GetGroupByIDResponse, error) {
	if m.getGroupByIDFunc != nil {
		return m.getGroupByIDFunc(ctx, r)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) UpdateGroup(ctx context.Context, r *group.UpdateGroupRequest) (*group.UpdateGroupResponse, error) {
	if m.updateGroupFunc != nil {
		return m.updateGroupFunc(ctx, r)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroups(ctx context.Context, r *group.GetGroupsRequest) (*group.GetGroupsResponse, error) {
	if m.getGroupsFunc != nil {
		return m.getGroupsFunc(ctx, r)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupsConfig(ctx context.Context, r *group.GetGroupsConfigRequest) (*group.GetGroupsConfigResponse, error) {
	if m.getGroupsConfigFunc != nil {
		return m.getGroupsConfigFunc(ctx, r)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupsStats(ctx context.Context, r *group.GetGroupsStatsRequest) (*group.GetGroupsStatsResponse, error) {
	if m.getGroupsStatsFunc != nil {
		return m.getGroupsStatsFunc(ctx, r)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) ValidateGroupName(ctx context.Context, r *group.ValidateGroupNameRequest) (*group.ValidateGroupNameResponse, error) {
	if m.validateGroupNameFunc != nil {
		return m.validateGroupNameFunc(ctx, r)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupByNanoID(ctx context.Context, r *group.GetGroupByNanoIDRequest) (*group.GetGroupByNanoIDResponse, error) {
	if m.getGroupByNanoIDFunc != nil {
		return m.getGroupByNanoIDFunc(ctx, r)
	}
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) DeleteGroup(ctx context.Context, r *group.DeleteGroupRequest) (*group.DeleteGroupResponse, error) {
	if m.deleteGroupFunc != nil {
		return m.deleteGroupFunc(ctx, r)
	}
	return nil, errors.New("not implemented")
}

// Stub all other methods to satisfy the interface
func (m *mockGroupService) GetGroupLineage(ctx context.Context, r *group.GetGroupLineageRequest) (*group.GetGroupLineageResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupDescendants(ctx context.Context, r *group.GetGroupDescendantsRequest) (*group.GetGroupDescendantsResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupsByUserID(ctx context.Context, r *group.GetGroupsByUserIDRequest) (*group.GetGroupsByUserIDResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx context.Context, r *group.GetGroupsAwaitingAnswerForInvitationsByMemberIDRequest) (*group.GetGroupsAwaitingAnswerForInvitationsByMemberIDResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupsByMemberID(ctx context.Context, r *group.GetGroupsRequest) (*group.GetGroupsResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupsByLeaderID(ctx context.Context, r *group.GetGroupsRequest) (*group.GetGroupsResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) SearchGroupsByExtension(ctx context.Context, r *group.GetGroupsRequest) (*group.GetGroupsResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) AddMember(ctx context.Context, r *group.AddMemberRequest) (*group.AddMemberResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) InviteUser(ctx context.Context, r *group.InviteUserRequest) (*group.InviteUserResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) UninviteUser(ctx context.Context, r *group.UninviteUserRequest) (*group.UninviteUserResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) AcceptInvite(ctx context.Context, r *group.AcceptInviteRequest) (*group.AcceptInviteResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) RejectInvite(ctx context.Context, r *group.RejectInviteRequest) (*group.RejectInviteResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) RemoveMember(ctx context.Context, r *group.RemoveMemberRequest) (*group.RemoveMemberResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) UpdateMemberRole(ctx context.Context, r *group.UpdateMemberRoleRequest) (*group.UpdateMemberRoleResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupMembers(ctx context.Context, r *group.GetGroupMembersRequest) (*group.GetGroupMembersResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) UpdateOwner(ctx context.Context, r *group.UpdateOwnerRequest) (*group.UpdateOwnerResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) RepairInvalidMembers(ctx context.Context) (*group.RepairInvalidMembersResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) ArchiveGroup(ctx context.Context, r *group.ArchiveGroupRequest) (*group.ArchiveGroupResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) RestoreGroup(ctx context.Context, r *group.RestoreGroupRequest) (*group.RestoreGroupResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetGroupStats(ctx context.Context, groupID string) (*group.GetGroupStatsResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) GetParentGroupsWithAutoJoinForEmail(ctx context.Context, email string) (*group.GetParentGroupsWithAutoJoinForEmailResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) EnableGroupAutoJoinByEmailDomain(ctx context.Context, r *group.EnableGroupAutoJoinByEmailDomainRequest) (*group.EnableGroupAutoJoinByEmailDomainResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) DisableGroupAutoJoinByEmailDomain(ctx context.Context, r *group.DisableGroupAutoJoinByEmailDomainRequest) (*group.DisableGroupAutoJoinByEmailDomainResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) EnableGroupAutoInviteByEmailDomain(ctx context.Context, r *group.EnableGroupAutoInviteByEmailDomainRequest) (*group.EnableGroupAutoInviteByEmailDomainResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockGroupService) DisableGroupAutoInviteByEmailDomain(ctx context.Context, r *group.DisableGroupAutoInviteByEmailDomainRequest) (*group.DisableGroupAutoInviteByEmailDomainResponse, error) {
	return nil, errors.New("not implemented")
}

// mockValidator implements group.GroupValidator for handler tests
type mockValidator struct {
	validateFunc func(s interface{}) error
}

func (m *mockValidator) Validate(s interface{}) error {
	if m.validateFunc != nil {
		return m.validateFunc(s)
	}

	return validator.New().Struct(s)
}

func newTestHandler(svc group.GroupService) *group.Handler {
	return group.NewHandler(svc, &mockValidator{}, group.GroupErrorMap)
}

func TestHandler_CreateGroup(t *testing.T) {
	t.Parallel()

	successResponse := &group.CreateGroupResponse{
		Group: &group.UniversalGroup{
			ID:            testGroupID,
			Name:          strings.ToLower(strings.ReplaceAll(testGroupName, " ", "-")),
			RawName:       testGroupName, // Request "name" field becomes "raw_name" in response
			Type:          testGroupType,
			Status:        group.GroupStatusActive,
			OwnerID:       testUserID,
			NanoID:        testNanoID,
			ParentGroupID: testParentGroupID,
			Lineage:       []string{testParentGroupID},
			Members: []group.Member{
				{
					ID:       testUserID,
					Type:     group.MemberTypeUser,
					Role:     "ADMIN",
					JoinedAt: "2026-05-03T20:05:30Z",
				},
			},
			DisplayInfo: &group.DisplayInfo{
				Description: "Test description",
			},
			Settings: &group.GroupSettings{
				Visibility: "PRIVATE",
			},
			Integrations: &group.Integrations{},
			Metadata: &group.GroupMetadata{
				CreatedAt: "2026-05-03T20:05:30Z",
				UpdatedAt: "2026-05-03T20:05:30Z",
			},
		},
	}

	tests := []struct {
		name         string
		body         string
		mockResponse *group.CreateGroupResponse
		mockErr      error
		expectStatus int
	}{
		{
			name:         "Success - returns 201 with created group",
			body:         `{"name":"` + testGroupName + `","type":"` + testGroupType + `","parent_group_id":"` + testParentGroupID + `","description":"Test description","visibility":"PRIVATE"}`,
			mockResponse: successResponse,
			expectStatus: http.StatusCreated,
		},
		{
			name:         "Failure - bad body returns 400",
			body:         `{"name":""}`,
			expectStatus: http.StatusBadRequest,
		},
		{
			name:         "Failure - service returns error",
			body:         `{"name":"` + testGroupName + `","type":"` + testGroupType + `","parent_group_id":"` + testParentGroupID + `"}`,
			mockErr:      group.ErrValidationFailed,
			expectStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &mockGroupService{
				createGroupFunc: func(ctx context.Context, r *group.CreateGroupRequest) (*group.CreateGroupResponse, error) {
					return tt.mockResponse, tt.mockErr
				},
			}

			h := newTestHandler(svc)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				h.CreateGroup(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)
		})
	}
}

func TestHandler_GetGroupByID(t *testing.T) {
	t.Parallel()

	successResponse := &group.GetGroupByIDResponse{
		Group: &group.UniversalGroup{
			ID:      testGroupID,
			Name:    strings.ToLower(strings.ReplaceAll(testGroupName, " ", "-")),
			RawName: testGroupName,
			Type:    testGroupType,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
			NanoID:  testNanoID,
			Members: []group.Member{
				{
					ID:       testUserID,
					Type:     group.MemberTypeUser,
					Role:     "ADMIN",
					JoinedAt: "2026-05-03T20:05:30Z",
				},
			},
			DisplayInfo: &group.DisplayInfo{
				Description: "Test group description",
			},
			Settings: &group.GroupSettings{
				Visibility: "INTERNAL",
			},
			Integrations: &group.Integrations{},
			Metadata: &group.GroupMetadata{
				CreatedAt: "2026-05-03T20:05:30Z",
				UpdatedAt: "2026-05-03T20:05:30Z",
			},
		},
	}

	tests := []struct {
		name         string
		groupID      string
		mockResponse *group.GetGroupByIDResponse
		mockErr      error
		expectStatus int
	}{
		{
			name:         "Success - returns group",
			groupID:      testGroupID,
			mockResponse: successResponse,
			expectStatus: http.StatusOK,
		},
		{
			name:         "Failure - group not found",
			groupID:      testGroupID,
			mockErr:      group.ErrResourceNotFound,
			expectStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &mockGroupService{
				getGroupByIDFunc: func(ctx context.Context, r *group.GetGroupByIDRequest) (*group.GetGroupByIDResponse, error) {
					return tt.mockResponse, tt.mockErr
				},
			}

			h := newTestHandler(svc)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/"+tt.groupID, nil)
			req = mux.SetURLVars(req, map[string]string{"groupID": tt.groupID})
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				h.GetGroupByID(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)
		})
	}
}

func TestHandler_GetGroupsConfig(t *testing.T) {
	t.Parallel()

	successResponse := &group.GetGroupsConfigResponse{
		Config: &group.GroupConfigCapabilities{
			RequiredFields:      []string{"name", "type"},
			MultipleIdentifiers: true,
		},
	}

	tests := []struct {
		name         string
		mockResponse *group.GetGroupsConfigResponse
		mockErr      error
		expectStatus int
	}{
		{
			name:         "Success - returns config",
			mockResponse: successResponse,
			expectStatus: http.StatusOK,
		},
		{
			name:         "Failure - config error",
			mockErr:      group.ErrGroupConfigNotSet,
			expectStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &mockGroupService{
				getGroupsConfigFunc: func(ctx context.Context, r *group.GetGroupsConfigRequest) (*group.GetGroupsConfigResponse, error) {
					return tt.mockResponse, tt.mockErr
				},
			}

			h := newTestHandler(svc)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/configs", nil)
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				h.GetGroupsConfig(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)
		})
	}
}

func TestHandler_ValidateGroupName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		queryParams  url.Values
		mockResponse *group.ValidateGroupNameResponse
		mockErr      error
		expectStatus int
	}{
		{
			name:         "Success - name is valid",
			queryParams:  url.Values{"name": []string{testGroupName}, "type": []string{testGroupType}, "parent_group_id": []string{testParentGroupID}},
			mockResponse: &group.ValidateGroupNameResponse{RawName: testGroupName, Name: strings.ReplaceAll(toolbox.StringStandardisedToLower(testGroupName), " ", "-"), Adjusted: false},
			expectStatus: http.StatusOK,
		},
		{
			name:         "Success - name was adjusted to be unique",
			queryParams:  url.Values{"name": []string{testGroupName}, "type": []string{testGroupType}, "parent_group_id": []string{testParentGroupID}},
			mockResponse: &group.ValidateGroupNameResponse{RawName: testGroupName, Name: strings.ReplaceAll(toolbox.StringStandardisedToLower(testGroupName), " ", "-") + "-2", Adjusted: true},
			expectStatus: http.StatusOK,
		},
		{
			name:         "Failure - validation error",
			queryParams:  url.Values{"name": []string{""}, "type": []string{testGroupType}, "parent_group_id": []string{testParentGroupID}},
			expectStatus: http.StatusBadRequest,
		},
		{
			name:         "Failure - missing query parameters",
			queryParams:  url.Values{},
			expectStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &mockGroupService{
				validateGroupNameFunc: func(ctx context.Context, r *group.ValidateGroupNameRequest) (*group.ValidateGroupNameResponse, error) {
					return tt.mockResponse, tt.mockErr
				},
			}

			h := newTestHandler(svc)

			queryString := tt.queryParams.Encode()
			if queryString != "" {
				queryString = "?" + queryString
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/validate-name"+queryString, nil)
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				h.ValidateGroupName(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)
		})
	}
}

func TestHandler_GetGroupByNanoID(t *testing.T) {
	t.Parallel()

	successResponse := &group.GetGroupByNanoIDResponse{
		Group: &group.UniversalGroup{
			ID:      testGroupID,
			NanoID:  testNanoID,
			Name:    strings.ToLower(strings.ReplaceAll(testGroupName, " ", "-")),
			RawName: testGroupName,
			Type:    testGroupType,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
			Members: []group.Member{
				{
					ID:       testUserID,
					Type:     group.MemberTypeUser,
					Role:     "ADMIN",
					JoinedAt: "2026-05-03T20:05:30Z",
				},
			},
			DisplayInfo: &group.DisplayInfo{
				Description: "Test group description",
			},
			Settings: &group.GroupSettings{
				Visibility: "INTERNAL",
			},
			Integrations: &group.Integrations{},
			Metadata: &group.GroupMetadata{
				CreatedAt: "2026-05-03T20:05:30Z",
				UpdatedAt: "2026-05-03T20:05:30Z",
			},
		},
	}

	tests := []struct {
		name         string
		nanoID       string
		mockResponse *group.GetGroupByNanoIDResponse
		mockErr      error
		expectStatus int
	}{
		{
			name:         "Success - returns group by nano ID",
			nanoID:       testNanoID,
			mockResponse: successResponse,
			expectStatus: http.StatusOK,
		},
		{
			name:         "Failure - group not found",
			nanoID:       testNanoID,
			mockErr:      group.ErrResourceNotFound,
			expectStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &mockGroupService{
				getGroupByNanoIDFunc: func(ctx context.Context, r *group.GetGroupByNanoIDRequest) (*group.GetGroupByNanoIDResponse, error) {
					return tt.mockResponse, tt.mockErr
				},
			}

			h := newTestHandler(svc)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups/by-nano/"+tt.nanoID, nil)
			req = mux.SetURLVars(req, map[string]string{"groupNanoID": tt.nanoID})
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				h.GetGroupByNanoID(rec, req)
			})

			assert.Equal(t, tt.expectStatus, rec.Code)
		})
	}
}
