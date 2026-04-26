package examples

import (
	"context"
	"fmt"
	"time"

	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
)

// Example1_GetEnrichedUserProfile demonstrates fetching a user profile with group memberships
func Example1_GetEnrichedUserProfile() {
	service := setupService()

	ctx := context.Background()
	resp, err := service.GetEnrichedUserProfile(ctx, &usermanager.GetEnrichedUserProfileRequest{
		UserId:             "user-123",
		IncludeTeams:       true,
		IncludeDepartments: true,
		IncludeAllGroups:   true,
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	profile := resp.Profile
	fmt.Printf("User: %s (%s)\n", profile.FullName, profile.Email)
	fmt.Printf("Teams: %d\n", len(profile.Teams))
	fmt.Printf("Departments: %d\n", len(profile.Departments))

	for _, team := range profile.Teams {
		fmt.Printf("  - %s (%s)\n", team.Group.Name, team.Role)
	}
}

// Example2_GetUserTeamMemberships demonstrates getting detailed team membership information
func Example2_GetUserTeamMemberships() {
	service := setupService()

	ctx := context.Background()
	resp, err := service.GetUserTeamMemberships(ctx, &usermanager.GetUserTeamMembershipsRequest{
		UserId:          "user-123",
		IncludeInactive: false,
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("User %s team memberships:\n", resp.UserID)
	for teamName, membership := range resp.Memberships {
		if membership.IsMember {
			fmt.Printf("  - %s: %s", teamName, membership.Role)
			if membership.IsOwner {
				fmt.Print(" (Owner)")
			}
			if membership.IsLead {
				fmt.Print(" (Lead)")
			}
			fmt.Println()
		}
	}
}

// Example3_UpdateUserTeamMembership demonstrates adding a user to a team
func Example3_UpdateUserTeamMembership() {
	service := setupService()

	ctx := context.Background()
	resp, err := service.UpdateUserTeamMembership(ctx, &usermanager.UpdateUserTeamMembershipRequest{
		UserId:               "user-456",
		GroupID:              "team-frontend",
		Role:                 group.MemberRoleMember,
		RemoveFromOtherTeams: false,
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if resp.Success {
		fmt.Printf("Successfully added user to team: %s\n", resp.Membership.Group.Name)
		fmt.Printf("Role: %s\n", resp.Membership.Role)
		if len(resp.PreviousMemberships) > 0 {
			fmt.Printf("Removed from %d previous teams\n", len(resp.PreviousMemberships))
		}
	}
}

// Example4_RemoveUserFromGroup demonstrates removing a user from a group
func Example4_RemoveUserFromGroup() {
	service := setupService()

	ctx := context.Background()
	resp, err := service.RemoveUserFromGroup(ctx, &usermanager.RemoveUserFromGroupRequest{
		UserId:  "user-456",
		GroupID: "team-frontend",
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if resp.Success {
		fmt.Printf("Successfully removed user from group: %s\n", resp.GroupID)
	}
}

// Example5_GetUserGroups demonstrates fetching groups for a user with filtering
func Example5_GetUserGroups() {
	service := setupService()

	ctx := context.Background()
	resp, err := service.GetUserGroups(ctx, &usermanager.GetUserGroupsRequest{
		UserId:    "user-123",
		GroupType: group.GroupTypeTeam,
		Status:    group.GroupStatusActive,
		Page:      1,
		PerPage:   10,
		Meta:      true,
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Found %d teams:\n", len(resp.Groups))
	for _, g := range resp.Groups {
		fmt.Printf("  - %s (%s) - %d members\n", g.Name, g.Type, g.MemberCount)
	}

	if resp.Meta != nil {
		fmt.Printf("Total: %d, Page: %d\n", resp.Total, resp.Meta["page"])
	}
}

// Example6_GetGroupsByType demonstrates fetching groups filtered by type
func Example6_GetGroupsByType() {
	service := setupService()

	ctx := context.Background()
	resp, err := service.GetGroupsByType(ctx, &usermanager.GetGroupsByTypeRequest{
		UserId:              "user-123",
		GroupType:           group.GroupTypeDepartment,
		Status:              group.GroupStatusActive,
		OnlyUserMemberships: true,
		Page:                1,
		PerPage:             20,
		Meta:                true,
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Found %d departments:\n", len(resp.Groups))
	for _, g := range resp.Groups {
		fmt.Printf("  - %s (%d members)\n", g.Name, g.MemberCount)
	}
}

// Example7_FindUserInfo demonstrates finding user information with group memberships
func Example7_FindUserInfo() {
	service := setupService()

	ctx := context.Background()
	resp, err := service.FindUserInfo(ctx, &usermanager.FindUserInfoRequest{
		Email:                   "john.doe@company.com",
		IncludeTeamMemberships:  true,
		IncludeGroupMemberships: true,
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Found user: %s (%s)\n", resp.User.FirstName+" "+resp.User.LastName, resp.User.Email)
	if resp.MembershipDataFetched {
		fmt.Printf("Team memberships: %d\n", len(resp.TeamMemberships))
		fmt.Printf("All group memberships: %d\n", len(resp.GroupMemberships))
	}
}

// Example8_BulkUpdateUserGroupMemberships demonstrates bulk membership operations
func Example8_BulkUpdateUserGroupMemberships() {
	service := setupService()

	ctx := context.Background()
	resp, err := service.BulkUpdateUserGroupMemberships(ctx, &usermanager.BulkUpdateUserGroupMembershipsRequest{
		UserId:       "admin-user",
		TargetUserId: "user-789",
		Actions: []usermanager.GroupMembershipAction{
			{
				Action:  "ADD",
				GroupID: "team-backend",
				Role:    group.MemberRoleMember,
			},
			{
				Action:  "ADD",
				GroupID: "dept-engineering",
				Role:    group.MemberRoleMember,
			},
			{
				Action:  "REMOVE",
				GroupID: "team-frontend",
			},
		},
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Bulk update completed: %d success, %d failures\n",
		resp.SuccessCount, resp.FailureCount)

	for _, result := range resp.Results {
		status := "SUCCESS"
		if !result.Success {
			status = "FAILED: " + result.Error
		}
		fmt.Printf("  %s %s: %s\n", result.Action, result.GroupID, status)
	}
}

// Example9_UserProfileManagement demonstrates basic user profile operations
func Example9_UserProfileManagement() {
	service := setupService()

	ctx := context.Background()

	// Get user profile
	profileResp, err := service.GetUserProfile(ctx, &usermanager.GetUserProfileRequest{
		UserId: "user-123",
	})
	if err != nil {
		fmt.Println("Error getting profile:", err)
		return
	}

	fmt.Printf("User profile: %s %s\n", profileResp.Profile.FirstName, profileResp.Profile.LastName)

	// Update user profile
	updateResp, err := service.UpdateUserProfile(ctx, &usermanager.UpdateUserProfileRequest{
		UserId: "user-123",
		UpdateUserRequest: &user.UpdateUserRequest{
			FirstName: "John",
			LastName:  "Smith",
		},
	})
	if err != nil {
		fmt.Println("Error updating profile:", err)
		return
	}

	fmt.Printf("Updated user: %s %s\n",
		updateResp.UpdateUserResponse.User.PersonalInfo.FirstName,
		updateResp.UpdateUserResponse.User.PersonalInfo.LastName)
}

// Example10_CommunicationManagement demonstrates creating and retrieving communications
func Example10_CommunicationManagement() {
	service := setupService()

	ctx := context.Background()

	// Create a communication
	createResp, err := service.CreateComms(ctx, &usermanager.CreateCommsRequest{
		CreateCommsRequest: &contacter.CreateCommsRequest{
			UserId:   "user-123",
			FullName: "John Doe",
			Email:    "john.doe@company.com",
			Type:     contacter.CommsTypeFeedback,
			Message:  "Welcome to our engineering team...",
			Meta: map[string]interface{}{
				"subject": "Welcome to the team!",
			},
		},
	})
	if err != nil {
		fmt.Println("Error creating comms:", err)
		return
	}

	fmt.Printf("Created communication: %s\n", createResp.Comms.Id)

	// Get communications
	getResp, err := service.GetComms(ctx, &usermanager.GetCommsRequest{
		UserId: "user-123",
		GetCommsRequest: &contacter.GetCommsRequest{
			Page:    1,
			PerPage: 10,
		},
	})
	if err != nil {
		fmt.Println("Error getting comms:", err)
		return
	}

	fmt.Printf("Found %d communications\n", len(getResp.Comms))
}

// Example11_GroupManagementWorkflow demonstrates a complete group management workflow
func Example11_GroupManagementWorkflow() {
	service := setupService()

	ctx := context.Background()
	userID := "user-new-hire"

	fmt.Println("=== Group Management Workflow ===")

	// 1. Get user's current memberships
	fmt.Println("1. Checking current memberships...")
	currentResp, err := service.GetUserGroups(ctx, &usermanager.GetUserGroupsRequest{
		UserId:  userID,
		Page:    1,
		PerPage: 50,
	})
	if err != nil {
		fmt.Printf("Error getting current memberships: %v\n", err)
		return
	}
	fmt.Printf("   Current groups: %d\n", len(currentResp.Groups))

	// 2. Add to engineering department
	fmt.Println("2. Adding to Engineering department...")
	deptResp, err := service.UpdateUserTeamMembership(ctx, &usermanager.UpdateUserTeamMembershipRequest{
		UserId:  userID,
		GroupID: "dept-engineering",
		Role:    group.MemberRoleMember,
	})
	if err != nil {
		fmt.Printf("Error adding to department: %v\n", err)
		return
	}
	fmt.Printf("   Added to: %s\n", deptResp.Membership.Group.Name)

	// 3. Add to frontend team
	fmt.Println("3. Adding to Frontend team...")
	teamResp, err := service.UpdateUserTeamMembership(ctx, &usermanager.UpdateUserTeamMembershipRequest{
		UserId:               userID,
		GroupID:              "team-frontend",
		Role:                 group.MemberRoleMember,
		RemoveFromOtherTeams: true, // Remove from other teams
	})
	if err != nil {
		fmt.Printf("Error adding to team: %v\n", err)
		return
	}
	fmt.Printf("   Added to: %s\n", teamResp.Membership.Group.Name)
	if len(teamResp.PreviousMemberships) > 0 {
		fmt.Printf("   Removed from %d other teams\n", len(teamResp.PreviousMemberships))
	}

	// 4. Verify final memberships
	fmt.Println("4. Verifying final memberships...")
	finalResp, err := service.GetEnrichedUserProfile(ctx, &usermanager.GetEnrichedUserProfileRequest{
		UserId:           userID,
		IncludeAllGroups: true,
	})
	if err != nil {
		fmt.Printf("Error verifying memberships: %v\n", err)
		return
	}

	fmt.Printf("   Final teams: %d\n", len(finalResp.Profile.Teams))
	fmt.Printf("   Final departments: %d\n", len(finalResp.Profile.Departments))
	fmt.Printf("   Total groups: %d\n", len(finalResp.Profile.Groups))

	fmt.Println("=== Workflow completed successfully ===")
}

// Example12_AdminCreateGroup demonstrates admin creating a new group
func Example12_AdminCreateGroup() {
	service := setupService()

	ctx := context.Background()

	fmt.Println("=== Admin Create Group Example ===")

	// Create a new engineering team
	createResp, err := service.CreateGroup(ctx, &usermanager.CreateGroupRequest{
		AdminUserId:       "admin-user-123",
		Name:              "Machine Learning Team",
		Type:              group.GroupTypeTeam,
		Description:       "Team focused on ML/AI projects and research",
		Email:             "ml-team@company.com",
		Icon:              "🤖",
		Visibility:        group.VisibilityPrivate,
		InitialMemberIDs:  []string{"user-123", "user-456", "user-789"},
		InitialMemberRole: group.MemberRoleMember,
		OwnerID:           "user-123",
		Extensions: map[string]interface{}{
			"slack_channel": "#ml-team",
			"cost_center":   "ENG-ML-001",
			"budget":        150000,
		},
	})
	if err != nil {
		fmt.Printf("Error creating group: %v\n", err)
		return
	}

	fmt.Printf("Created group: %s (ID: %s)\n", createResp.Group.Name, createResp.Group.ID)
	fmt.Printf("  Type: %s\n", createResp.Group.Type)
	fmt.Printf("  Members: %d\n", createResp.Group.MemberCount)
	fmt.Printf("  Status: %s\n", createResp.Group.Status)
	fmt.Printf("  Created: %s\n", createResp.Group.CreatedAt)

	fmt.Println("=== Group created successfully ===")
}

// Helper functions and types

func setupService() *usermanager.Service {
	// Create mock services
	userSvc := &MockUserService{}
	apiTokenSvc := &MockApiTokenService{}
	auditSvc := &MockAuditService{}
	contacterSvc := &MockContacterService{}
	groupSvc := &MockGroupService{}

	// Create usermanager service
	service := usermanager.NewService(&usermanager.NewServiceRequest{
		UserService:      userSvc,
		ApiTokenService:  apiTokenSvc,
		AuditService:     auditSvc,
		ContacterService: contacterSvc,
	})

	// Add group service
	service.WithGroupService(groupSvc)

	return service
}

// Mock services for examples

type MockUserService struct{}

func (m *MockUserService) GetUserMicroProfile(ctx context.Context, r *user.GetUserMicroProfileRequest) (*user.GetUserMicroProfileResponse, error) {
	return &user.GetUserMicroProfileResponse{
		MicroProfile: &user.UserMicroProfile{
			ID:     r.ID,
			Roles:  []string{"USER"},
			Status: "ACTIVE",
		},
	}, nil
}

func (m *MockUserService) GetUserProfile(ctx context.Context, r *user.GetUserProfileRequest) (*user.GetUserProfileResponse, error) {
	return &user.GetUserProfileResponse{
		Profile: &user.UserProfile{
			ID:            r.ID,
			Email:         "user@example.com",
			FirstName:     "John",
			LastName:      "Doe",
			Status:        "ACTIVE",
			Roles:         []string{"USER"},
			EmailVerified: true,
			UpdatedAt:     time.Now().Format(time.RFC3339),
		},
	}, nil
}

func (m *MockUserService) GetUserByID(ctx context.Context, r *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	return &user.GetUserByIDResponse{
		User: &user.UniversalUser{
			ID:    r.ID,
			Email: "user@example.com",
			PersonalInfo: &user.PersonalInfo{
				FullName:  "John Doe",
				FirstName: "John",
				LastName:  "Doe",
			},
			Status: "ACTIVE",
			Roles:  []string{"USER"},
			Metadata: &user.UserMetadata{
				CreatedAt: time.Now().Add(-30 * 24 * time.Hour).Format(common.RFC3339NanoUTC),
			},
		},
	}, nil
}

func (m *MockUserService) GetUserByEmail(ctx context.Context, r *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error) {
	return &user.GetUserByEmailResponse{
		User: &user.UniversalUser{
			ID:    "user-123",
			Email: r.Email,
			PersonalInfo: &user.PersonalInfo{
				FullName:  "John Doe",
				FirstName: "John",
				LastName:  "Doe",
			},
			Status: "ACTIVE",
			Roles:  []string{"USER"},
		},
	}, nil
}

func (m *MockUserService) UpdateUser(ctx context.Context, r *user.UpdateUserRequest) (*user.UpdateUserResponse, error) {
	return &user.UpdateUserResponse{
		User: &user.UniversalUser{
			ID:    "user-123",
			Email: "user@example.com",
			PersonalInfo: &user.PersonalInfo{
				FullName:  r.FirstName + " " + r.LastName,
				FirstName: r.FirstName,
				LastName:  r.LastName,
			},
		},
	}, nil
}

func (m *MockUserService) DeleteUser(ctx context.Context, r *user.DeleteUserRequest) error {
	return nil
}

type MockApiTokenService struct{}

func (m *MockApiTokenService) DeleteApiTokensByOwnerId(ctx context.Context, ownerId string) error {
	return nil
}

func (m *MockApiTokenService) GetTotalApiTokens(ctx context.Context, r *apitoken.GetTotalApiTokensRequest) (int64, error) {
	return 5, nil
}

type MockAuditService struct{}

func (m *MockAuditService) GetTotalAuditLogEvents(ctx context.Context, r *audit.GetTotalAuditLogEventsRequest) (int64, error) {
	return 25, nil
}

type MockContacterService struct{}

func (m *MockContacterService) CreateComms(ctx context.Context, req *contacter.CreateCommsRequest) (*contacter.CreateCommsResponse, error) {
	return &contacter.CreateCommsResponse{
		Comms: &contacter.Comms{
			Id:           "comms-123",
			FullName:     req.FullName,
			Email:        req.Email,
			Type:         req.Type,
			Message:      req.Message,
			Meta:         req.Meta,
			UserId:       req.UserId,
			UserLoggedIn: true,
			CreatedAt:    time.Now().Format(time.RFC3339),
		},
	}, nil
}

func (m *MockContacterService) GetComms(ctx context.Context, req *contacter.GetCommsRequest) (*contacter.GetCommsResponse, error) {
	return &contacter.GetCommsResponse{
		Comms: []contacter.Comms{
			{
				Id:           "comms-123",
				FullName:     "John Doe",
				Email:        "john.doe@company.com",
				Type:         contacter.CommsTypeFeedback,
				Message:      "Welcome!",
				UserId:       "user-123",
				UserLoggedIn: true,
				CreatedAt:    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
		},
		Total: 1,
	}, nil
}

func (m *MockContacterService) UpdateComms(ctx context.Context, req *contacter.UpdateCommsRequest) (*contacter.UpdateCommsResponse, error) {
	// Simulate fetching existing comms and updating admin fields
	comms := &contacter.Comms{
		Id:           req.CommsId,
		UserLoggedIn: true,
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}

	if req.AdminNotes != nil {
		comms.AdminNotes = *req.AdminNotes
	}

	if req.AdminReply != nil {
		comms.AdminReply = *req.AdminReply
	}

	if req.LinkedCommsIds != nil {
		comms.LinkedCommsIds = *req.LinkedCommsIds
	}

	// Set reached out timestamp if flag is true
	if req.ReachedOut != nil && *req.ReachedOut {
		comms.ReachedOutAt = time.Now().Format(time.RFC3339)
	}

	return &contacter.UpdateCommsResponse{
		Comms: comms,
	}, nil
}

func (m *MockContacterService) GetCommsStats(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error) {
	return &contacter.GetCommsStatsResponse{}, nil
}

type MockGroupService struct{}

func (m *MockGroupService) GetGroups(ctx context.Context, r *group.GetGroupsRequest) (*group.GetGroupsResponse, error) {
	var groups []*group.UniversalGroup

	// Mock some teams
	if r.Types == nil || contains(r.Types, group.GroupTypeTeam) {
		teams := []*group.UniversalGroup{
			createMockGroup("team-frontend", "Frontend Team", group.GroupTypeTeam, 5),
			createMockGroup("team-backend", "Backend Team", group.GroupTypeTeam, 8),
			createMockGroup("team-devops", "DevOps Team", group.GroupTypeTeam, 4),
		}

		// Add common test users to teams for consistent testing
		for _, team := range teams {
			team.AddMember("user-123", group.MemberTypeUser, group.MemberRoleMember)
			team.AddMember("user-456", group.MemberTypeUser, group.MemberRoleMember)
			team.AddMember("user-new-hire", group.MemberTypeUser, group.MemberRoleMember)
		}

		// Filter by member if specified
		if r.MemberID != "" {
			for _, team := range teams {
				if team.HasMember(r.MemberID) {
					groups = append(groups, team)
				}
			}
		} else {
			groups = append(groups, teams...)
		}
	}

	// Mock departments
	if r.Types == nil || contains(r.Types, group.GroupTypeDepartment) {
		depts := []*group.UniversalGroup{
			createMockGroup("dept-engineering", "Engineering", group.GroupTypeDepartment, 25),
			createMockGroup("dept-product", "Product", group.GroupTypeDepartment, 15),
		}

		// Add common test users to departments
		for _, dept := range depts {
			dept.AddMember("user-123", group.MemberTypeUser, group.MemberRoleMember)
			dept.AddMember("user-456", group.MemberTypeUser, group.MemberRoleMember)
			dept.AddMember("user-new-hire", group.MemberTypeUser, group.MemberRoleMember)
		}

		if r.MemberID != "" {
			for _, dept := range depts {
				if dept.HasMember(r.MemberID) {
					groups = append(groups, dept)
				}
			}
		} else {
			groups = append(groups, depts...)
		}
	}

	// Apply status filter if specified
	if len(r.Statuses) > 0 {
		filteredGroups := []*group.UniversalGroup{}
		for _, g := range groups {
			for _, status := range r.Statuses {
				if g.Status == status {
					filteredGroups = append(filteredGroups, g)
					break
				}
			}
		}
		groups = filteredGroups
	}

	return &group.GetGroupsResponse{
		Groups:     groups,
		Total:      len(groups),
		TotalPages: 1,
		Page:       r.Page,
		PerPage:    r.PerPage,
	}, nil
}

func (m *MockGroupService) GetGroupByID(ctx context.Context, r *group.GetGroupByIDRequest) (*group.GetGroupByIDResponse, error) {
	// Return specific groups based on ID for better example testing
	var mockGroup *group.UniversalGroup

	switch r.ID {
	case "team-frontend":
		mockGroup = createMockGroup("team-frontend", "Frontend Team", group.GroupTypeTeam, 5)
	case "team-backend":
		mockGroup = createMockGroup("team-backend", "Backend Team", group.GroupTypeTeam, 8)
	case "team-devops":
		mockGroup = createMockGroup("team-devops", "DevOps Team", group.GroupTypeTeam, 4)
	case "dept-engineering":
		mockGroup = createMockGroup("dept-engineering", "Engineering", group.GroupTypeDepartment, 25)
	case "dept-product":
		mockGroup = createMockGroup("dept-product", "Product", group.GroupTypeDepartment, 15)
	default:
		mockGroup = createMockGroup(r.ID, "Mock Group", group.GroupTypeTeam, 3)
	}

	// Add common test users
	mockGroup.AddMember("user-123", group.MemberTypeUser, group.MemberRoleMember)
	mockGroup.AddMember("user-456", group.MemberTypeUser, group.MemberRoleMember)
	mockGroup.AddMember("user-new-hire", group.MemberTypeUser, group.MemberRoleMember)

	return &group.GetGroupByIDResponse{
		Group: mockGroup,
	}, nil
}

func (m *MockGroupService) GetGroupByNanoID(ctx context.Context, r *group.GetGroupByNanoIDRequest) (*group.GetGroupByNanoIDResponse, error) {
	// Reuse GetGroupByID behaviour and attach NanoID for testing
	groupResp, err := m.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.NanoID})
	if err != nil {
		return nil, err
	}

	groupResp.Group.NanoID = r.NanoID

	return &group.GetGroupByNanoIDResponse{Group: groupResp.Group}, nil
}

func (m *MockGroupService) GetGroupMembers(ctx context.Context, r *group.GetGroupMembersRequest) (*group.GetGroupMembersResponse, error) {
	groupResp, err := m.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID})
	if err != nil {
		return nil, err
	}

	filtered := make([]group.Member, 0, len(groupResp.Group.Members))
	for _, member := range groupResp.Group.Members {
		if r.MemberType != "" && member.Type != r.MemberType {
			continue
		}
		if r.Role != "" && member.Role != r.Role {
			continue
		}
		filtered = append(filtered, member)
	}

	return &group.GetGroupMembersResponse{
		Members: filtered,
		Count:   len(filtered),
	}, nil
}

func (m *MockGroupService) AddMember(ctx context.Context, r *group.AddMemberRequest) (*group.AddMemberResponse, error) {
	// Return a properly populated group
	mockGroup := createMockGroup(r.GroupID, "Updated Group", group.GroupTypeTeam, 5)
	if _, err := mockGroup.AddMember(r.MemberID, r.Type, r.Role); err != nil {
		return nil, err
	}

	return &group.AddMemberResponse{
		Group: mockGroup,
	}, nil
}

func (m *MockGroupService) RemoveMember(ctx context.Context, r *group.RemoveMemberRequest) (*group.RemoveMemberResponse, error) {
	groupResp, err := m.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID})
	if err != nil {
		return nil, err
	}

	updatedGroup, err := groupResp.Group.RemoveMember(r.MemberID)
	if err != nil {
		return nil, err
	}

	return &group.RemoveMemberResponse{Group: updatedGroup}, nil
}

func (m *MockGroupService) UpdateMemberRole(ctx context.Context, r *group.UpdateMemberRoleRequest) (*group.UpdateMemberRoleResponse, error) {
	groupResp, err := m.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID})
	if err != nil {
		return nil, err
	}

	updatedGroup, err := groupResp.Group.UpdateMemberRole(r.MemberID, r.NewRole)
	if err != nil {
		return nil, err
	}

	return &group.UpdateMemberRoleResponse{Group: updatedGroup}, nil
}

func (m *MockGroupService) CreateGroup(ctx context.Context, req *group.CreateGroupRequest) (*group.CreateGroupResponse, error) {
	// Create a new mock group based on the request
	newGroup := createMockGroup(
		fmt.Sprintf("group-%s", time.Now().Format("20060102150405")),
		req.Name,
		req.Type,
		len(req.InitialMembers),
	)
	newGroup.DisplayInfo.Description = req.Description
	newGroup.DisplayInfo.Email = req.Email
	newGroup.DisplayInfo.Icon = req.Icon

	if req.Visibility != "" {
		newGroup.Settings.Visibility = req.Visibility
	}

	if len(req.Extensions) > 0 {
		newGroup.Extensions = req.Extensions
	}

	// Set owner
	if req.OwnerID != "" {
		newGroup.OwnerID = req.OwnerID
	}

	// Add initial members
	for _, member := range req.InitialMembers {
		newGroup.AddMember(member.ID, member.Type, member.Role)
	}

	return &group.CreateGroupResponse{
		Group: newGroup,
	}, nil
}

func (m *MockGroupService) UpdateLeadership(ctx context.Context, req *group.UpdateLeadershipRequest) (*group.UpdateLeadershipResponse, error) {
	groupResp, err := m.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: req.GroupID})
	if err != nil {
		return nil, err
	}
	g := groupResp.Group
	if req.OwnerID != nil {
		g.OwnerID = *req.OwnerID
	}
	return &group.UpdateLeadershipResponse{Group: g}, nil
}

func createMockGroup(id, name, groupType string, memberCount int) *group.UniversalGroup {
	config := group.DefaultGroupConfig()
	idGen := group.NewDefaultIDGenerator()
	timeProvider := group.NewDefaultTimeProvider()
	stringUtils := group.NewDefaultStringUtils()

	g := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
	g.ID = id
	g.Name = name
	g.Type = groupType
	g.Status = group.GroupStatusActive
	g.DisplayInfo.Description = fmt.Sprintf("%s - A collaborative group", name)
	g.Metadata.CreatedAt = time.Now().Add(-7 * 24 * time.Hour).Format(time.RFC3339)

	// Add some mock members with varied roles
	for i := 0; i < memberCount; i++ {
		role := group.MemberRoleMember
		if i == 0 {
			role = group.MemberRoleOwner
			g.OwnerID = fmt.Sprintf("user-%d", i+1)
		} else if i == 1 {
			role = group.MemberRoleAdmin
		}
		g.AddMember(fmt.Sprintf("user-%d", i+1), group.MemberTypeUser, role)
	}

	return g
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
