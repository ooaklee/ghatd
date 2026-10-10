package examples

import (
	"context"
	"fmt"
	"strings"
	"time"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
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
		ActorID:          "user-123",
		IncludeAllGroups: true,
		PrefixName:       true,
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	profile := resp.Profile
	fmt.Printf("User: %s (%s)\n", profile.FullName, profile.Email)
	fmt.Printf("Groups: %d\n", len(profile.Groups))

	for _, g := range profile.Groups {
		fmt.Printf("  - %s (%s)\n", g.Name, g.Role)
	}
}

// Example5_GetUserGroups demonstrates fetching groups for a user with filtering
func Example5_GetUserGroups() {
	service := setupService()

	ctx := context.Background()
	resp, err := service.GetUserGroups(ctx, &usermanager.GetUserGroupsRequest{
		ActorID:    "user-123",
		GroupType:  group.GroupTypeTeam,
		Status:     group.GroupStatusActive,
		Page:       1,
		PerPage:    10,
		Meta:       true,
		PrefixName: true,
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

// Example9_UserProfileManagement demonstrates basic user profile operations
func Example9_UserProfileManagement() {
	service := setupService()

	ctx := context.Background()

	// Get user profile
	profileResp, err := service.GetUserProfile(ctx, &usermanager.GetUserProfileRequest{
		ActorID: "user-123",
	})
	if err != nil {
		fmt.Println("Error getting profile:", err)
		return
	}

	fmt.Printf("User profile: %s %s\n", profileResp.Profile.FirstName, profileResp.Profile.LastName)

	// Fixture-only verified context. Production must obtain this from credential
	// verification middleware, never by publishing caller-supplied IDs.
	ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, "user-123"), true)
	ctx = accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: "user-123", AccessUUID: "example-session"})
	// Update user profile through the narrow capability on MockUserService.
	updateResp, err := service.UpdateUserProfile(ctx, &usermanager.UpdateUserProfileRequest{
		ActorID: "user-123",
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
		ActorID: "user-123",
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

// Example12_AdminCreateGroup demonstrates admin creating a new group
func Example12_AdminCreateGroup() {
	service := setupService()

	ctx := context.Background()

	fmt.Println("=== Admin Create Group Example ===")

	// Create a new engineering team
	createResp, err := service.CreateGroup(ctx, &usermanager.CreateGroupRequest{
		ActorID: "admin-user-123",
		CreateGroupRequest: &group.CreateGroupRequest{
			Name:        "Machine Learning Team",
			Type:        group.GroupTypeTeam,
			Description: "Team focused on engineering projects and research",
			Visibility:  group.VisibilityPrivate,
			OwnerID:     "user-123",
		},
	})
	if err != nil {
		fmt.Printf("Error creating group: %v\n", err)
		return
	}

	fmt.Printf("Created group: %s (ID: %s)\n", createResp.Group.Name, createResp.Group.ID)
	fmt.Printf("  Type: %s\n", createResp.Group.Type)
	fmt.Printf("  Status: %s\n", createResp.Group.Status)
	fmt.Printf("  Created: %s\n", createResp.Group.Metadata.CreatedAt)

	fmt.Println("=== Group created successfully ===")
}

// Helper functions and types

// setupService assembles the example usermanager service from fixture-backed
// mock dependencies and attaches a group service; it configures the example
// only, not production wiring.
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

// MockUserService is the example's fixture user service returning static
// account data; production wiring supplies a real user service.
type MockUserService struct{}

// mockLookupRepository feeds the example's fixtures into the real domain
// lookup; batching and identity validation are not reimplemented by the host.
type mockLookupRepository struct {
	user.UserRepository
	service *MockUserService
}

// GetUsers supplies fixture records; unused repository methods are not invoked.
func (r *mockLookupRepository) GetUsers(ctx context.Context, req *user.GetUsersRequest) ([]user.UniversalUser, error) {
	response, err := r.service.GetUsers(ctx, req)
	if err != nil {
		return nil, err
	}
	return response.Users, nil
}

// GetUsersByIDs demonstrates delegation to the user domain. Production wiring
// supplies *user.Service directly instead of this fixture-backed mock.
func (m *MockUserService) GetUsersByIDs(ctx context.Context, req *user.GetUsersByIDsRequest) (*user.GetUsersByIDsResponse, error) {
	return user.NewService(&mockLookupRepository{service: m}, nil, nil, nil, nil, nil, "").GetUsersByIDs(ctx, req)
}

// GetUserMicroProfile echoes the requested ID with a static ACTIVE USER micro
// profile for the example.
func (m *MockUserService) GetUserMicroProfile(ctx context.Context, r *user.GetUserMicroProfileRequest) (*user.GetUserMicroProfileResponse, error) {
	return &user.GetUserMicroProfileResponse{
		MicroProfile: &user.UserMicroProfile{
			ID:     r.ID,
			Roles:  []string{"USER"},
			Status: "ACTIVE",
		},
	}, nil
}

// GetUserProfile returns a static verified example profile for the requested
// ID.
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

// GetUserByID returns a static ACTIVE example user for the requested ID with a
// creation date thirty days in the past.
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

// GetUsers returns a single static example user with pagination metadata
// echoing the requested page and per-page.
func (m *MockUserService) GetUsers(ctx context.Context, r *user.GetUsersRequest) (*user.GetUsersResponse, error) {
	return &user.GetUsersResponse{
		Users: []user.UniversalUser{
			{
				ID:    "user-123",
				Email: "user@example.com",
				PersonalInfo: &user.PersonalInfo{
					FullName:  "John Doe",
					FirstName: "John",
					LastName:  "Doe",
				},
				Status: "ACTIVE",
				Roles:  []string{"USER"},
			},
		},
		Meta: &user.PaginationMetadata{
			Page:           r.Page,
			PerPage:        r.PerPage,
			TotalResources: 1,
			TotalPages:     1,
		},
	}, nil
}

// GetUserByEmail returns a static ACTIVE example user whose email echoes the
// requested address.
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

// UpdateUser echoes the requested first and last names back as a fixture
// user-123 profile; no data is persisted.
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

// UpdateProfileNames illustrates the optional adapter contract without storage.
// A real adapter must compare the supplied snapshot and preserve unrelated data.
func (m *MockUserService) UpdateProfileNames(ctx context.Context, r *user.UpdateProfileNamesRequest) (*user.UniversalUser, error) {
	return &user.UniversalUser{ID: r.UserID, Email: r.ExpectedEmail, Status: r.ExpectedStatus, Type: r.ExpectedType, EmailRevision: r.ExpectedRevision,
		PersonalInfo: &user.PersonalInfo{FirstName: r.FirstName, LastName: r.LastName, FullName: r.FirstName + " " + r.LastName}}, nil
}

// DeleteUser accepts any delete request and reports success without touching
// storage.
func (m *MockUserService) DeleteUser(ctx context.Context, r *user.DeleteUserRequest) error {
	return nil
}

// MockApiTokenService is a stateless example stand-in for the API token domain,
// returning fixed counts and accepting deletions.
type MockApiTokenService struct{}

// DeleteApiTokensByOwnerId accepts any owner ID and reports success without
// deleting anything.
func (m *MockApiTokenService) DeleteApiTokensByOwnerId(ctx context.Context, ownerId string) error {
	return nil
}

// GetTotalApiTokens returns a fixed total of 5 tokens regardless of the request
// filters.
func (m *MockApiTokenService) GetTotalApiTokens(ctx context.Context, r *apitoken.GetTotalApiTokensRequest) (int64, error) {
	return 5, nil
}

// MockAuditService is a stateless example stand-in for audit logging with fixed
// event counts.
type MockAuditService struct{}

// LogAuditEvent accepts the event and reports success without recording it.
func (m *MockAuditService) LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error {
	return nil
}

// GetTotalAuditLogEvents returns a fixed total of 25 events regardless of the
// request filters.
func (m *MockAuditService) GetTotalAuditLogEvents(ctx context.Context, r *audit.GetTotalAuditLogEventsRequest) (int64, error) {
	return 25, nil
}

// MockContacterService backs example communication flows with canned contact
// records instead of persistent storage.
type MockContacterService struct{}

// CreateComms returns the submitted contact details as a stored-looking record
// with a fixed ID, current timestamp and logged-in flag.
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

// GetComms returns one hardcoded feedback record attributed to user-123 and a
// total of 1, ignoring pagination filters.
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

// UpdateComms builds a fresh record echoing the requested admin notes, reply
// and linked IDs, and stamps ReachedOutAt only when the reached-out flag is
// set.
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

// GetCommsStats returns an empty statistics response; the example does not
// aggregate any counters.
func (m *MockContacterService) GetCommsStats(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error) {
	return &contacter.GetCommsStatsResponse{}, nil
}

// GetAvailableCommsTypes returns the contacter package's default communication
// type map.
func (m *MockContacterService) GetAvailableCommsTypes(context.Context) (*contacter.GetAvailableCommsTypesResponse, error) {
	return &contacter.GetAvailableCommsTypesResponse{CommsTypes: contacter.DefaultCommsTypeMap()}, nil
}

// MockGroupService serves fixture teams and departments with common test users,
// backing group examples without persistent storage.
type MockGroupService struct{}

// GetGroups returns fixture teams and departments, honouring type, member and
// status filters; every group includes user-123, user-456 and user-new-hire as
// members.
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

// GetGroupByID returns a known fixture for recognised IDs, or a generic active
// team for any other ID, always seeded with the common test users.
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

// GetGroupByNanoID reuses the by-ID lookup, treating the NanoID as the group ID
// and attaching it to the returned group.
func (m *MockGroupService) GetGroupByNanoID(ctx context.Context, r *group.GetGroupByNanoIDRequest) (*group.GetGroupByNanoIDResponse, error) {
	// Reuse GetGroupByID behaviour and attach NanoID for testing
	groupResp, err := m.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.NanoID})
	if err != nil {
		return nil, err
	}

	groupResp.Group.NanoID = r.NanoID

	return &group.GetGroupByNanoIDResponse{Group: groupResp.Group}, nil
}

// GetGroupMembers returns the fixture group's members filtered by optional
// member type and role, with a count of the filtered result.
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

// AddMember returns a freshly created fixture group containing the requested
// member; the fixture set is not consulted.
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

// RemoveMember loads the fixture group by ID and applies its RemoveMember
// mutation, returning the updated group or the mutation error.
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

// UpdateMemberRole loads the fixture group by ID and applies its
// UpdateMemberRole mutation, returning the updated group or the mutation error.
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

// CreateGroup builds a new fixture group with a timestamp-derived ID, copying
// display, visibility, owner and extension fields and enrolling the requested
// initial members.
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

// UpdateGroup rejects a nil request or empty ID with ErrInvalidGroupID, then
// applies only the pointer fields that are set to the fixture group.
func (m *MockGroupService) UpdateGroup(ctx context.Context, req *group.UpdateGroupRequest) (*group.UpdateGroupResponse, error) {
	if req == nil || req.ID == "" {
		return nil, group.ErrInvalidGroupID
	}

	groupResp, err := m.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: req.ID})
	if err != nil {
		return nil, err
	}

	g := groupResp.Group
	if req.Name != nil {
		g.Name = *req.Name
	}
	if req.Description != nil {
		g.DisplayInfo.Description = *req.Description
	}
	if req.Email != nil {
		g.DisplayInfo.Email = *req.Email
	}
	if req.Visibility != nil {
		g.Settings.Visibility = *req.Visibility
	}
	if req.Status != nil {
		g.Status = *req.Status
	}

	return &group.UpdateGroupResponse{Group: g}, nil
}

// DeleteGroup validates the ID and reports success; no fixture state is
// actually removed.
func (m *MockGroupService) DeleteGroup(ctx context.Context, req *group.DeleteGroupRequest) (*group.DeleteGroupResponse, error) {
	if req == nil || req.ID == "" {
		return nil, group.ErrInvalidGroupID
	}

	return &group.DeleteGroupResponse{
		Success: true,
		Message: "Group deleted successfully",
	}, nil
}

// UpdateOwner loads the fixture group and replaces OwnerID only when the
// request supplies a non-nil value.
func (m *MockGroupService) UpdateOwner(ctx context.Context, req *group.UpdateOwnerRequest) (*group.UpdateOwnerResponse, error) {
	groupResp, err := m.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: req.GroupID})
	if err != nil {
		return nil, err
	}
	g := groupResp.Group
	if req.OwnerID != nil {
		g.OwnerID = *req.OwnerID
	}
	return &group.UpdateOwnerResponse{Group: g}, nil
}

// GetGroupDescendants returns an empty descendants list; hierarchy walking is
// not simulated.
func (m *MockGroupService) GetGroupDescendants(ctx context.Context, req *group.GetGroupDescendantsRequest) (*group.GetGroupDescendantsResponse, error) {
	// Mock implementation - return empty descendants for now
	return &group.GetGroupDescendantsResponse{
		Descendants: [][]group.GroupDescendantsNode{},
	}, nil
}

// GetGroupsByUserID returns the fixture groups containing the user as member,
// with an empty descendants map.
func (m *MockGroupService) GetGroupsByUserID(ctx context.Context, req *group.GetGroupsByUserIDRequest) (*group.GetGroupsByUserIDResponse, error) {
	groupsResp, err := m.GetGroups(ctx, &group.GetGroupsRequest{MemberID: req.UserID})
	if err != nil {
		return nil, err
	}

	return &group.GetGroupsByUserIDResponse{
		Groups:      groupsResp.Groups,
		Descendants: map[string][][]group.GroupDescendantsNode{},
	}, nil
}

// RemoveUserFromAllGroups reports success and counts the distinct root groups
// (first lineage entry, else group ID) the user belongs to; nothing is removed.
func (m *MockGroupService) RemoveUserFromAllGroups(ctx context.Context, req *group.RemoveUserFromAllGroupsRequest) (*group.RemoveUserFromAllGroupsResponse, error) {
	groupsResp, err := m.GetGroupsByUserID(ctx, &group.GetGroupsByUserIDRequest{UserID: req.UserID})
	if err != nil {
		return nil, err
	}

	rootSet := map[string]struct{}{}
	for _, grp := range groupsResp.Groups {
		if grp == nil {
			continue
		}

		rootID := strings.TrimSpace(grp.ID)
		if len(grp.Lineage) > 0 && strings.TrimSpace(grp.Lineage[0]) != "" {
			rootID = strings.TrimSpace(grp.Lineage[0])
		}

		if rootID == "" {
			continue
		}

		rootSet[rootID] = struct{}{}
	}

	return &group.RemoveUserFromAllGroupsResponse{
		Success:                 true,
		TotalRootGroupsAffected: len(rootSet),
		Message:                 "User removed from all groups successfully",
	}, nil
}

// GetGroupsAwaitingAnswerForInvitationsByMemberID returns all fixture groups
// containing the member; invitation state is not actually filtered.
func (m *MockGroupService) GetGroupsAwaitingAnswerForInvitationsByMemberID(
	ctx context.Context,
	req *group.GetGroupsAwaitingAnswerForInvitationsByMemberIDRequest,
) (*group.GetGroupsAwaitingAnswerForInvitationsByMemberIDResponse, error) {
	groupsResp, err := m.GetGroups(ctx, &group.GetGroupsRequest{MemberID: req.MemberID})
	if err != nil {
		return nil, err
	}

	return &group.GetGroupsAwaitingAnswerForInvitationsByMemberIDResponse{Groups: groupsResp.Groups}, nil
}

// GetUserGroupAccessMap derives per-group access summaries from fixture
// membership: accessible with member role, upgraded to the stored role, owner,
// or admin-classified roles as applicable.
func (m *MockGroupService) GetUserGroupAccessMap(ctx context.Context, userID string) (map[string]group.UserGroupAccessSummary, error) {
	groupsResp, err := m.GetGroups(ctx, &group.GetGroupsRequest{MemberID: userID})
	if err != nil {
		return nil, err
	}

	accessMap := make(map[string]group.UserGroupAccessSummary, len(groupsResp.Groups))
	for _, grp := range groupsResp.Groups {
		if grp == nil || grp.ID == "" {
			continue
		}

		summary := group.UserGroupAccessSummary{
			IsAccessible: true,
			MaxRole:      group.MemberRoleMember,
		}

		if grp.OwnerID == userID {
			summary.MaxRole = group.MemberRoleOwner
			summary.IsAdmin = true
		}

		member, memberErr := grp.GetMemberByID(userID)
		if memberErr == nil {
			if member.Role != "" {
				summary.MaxRole = strings.ToUpper(member.Role)
			}

			if strings.EqualFold(member.Role, group.MemberRoleAdmin) || strings.EqualFold(member.Role, group.MemberRoleSuperUser) {
				summary.IsAdmin = true
			}
		}

		accessMap[grp.ID] = summary
	}

	return accessMap, nil
}

// GetGroupsConfig returns an empty capabilities response for the example.
func (m *MockGroupService) GetGroupsConfig(_ context.Context, _ *group.GetGroupsConfigRequest) (*group.GetGroupsConfigResponse, error) {
	return &group.GetGroupsConfigResponse{}, nil
}

// GetGroupLineage returns an empty lineage list; ancestry is not simulated.
func (m *MockGroupService) GetGroupLineage(_ context.Context, req *group.GetGroupLineageRequest) (*group.GetGroupLineageResponse, error) {
	return &group.GetGroupLineageResponse{Lineage: []group.GroupLineageNode{}}, nil
}

// ValidateGroupName rejects a blank name with ErrValidationFailed and otherwise
// reports the trimmed name plus a lowercased hyphenated slug as available.
func (m *MockGroupService) ValidateGroupName(_ context.Context, req *group.ValidateGroupNameRequest) (*group.ValidateGroupNameResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, group.ErrValidationFailed
	}

	return &group.ValidateGroupNameResponse{
		RawName:    name,
		Name:       strings.ToLower(strings.ReplaceAll(name, " ", "-")),
		Adjusted:   false,
		Available:  true,
		IsRootType: false,
	}, nil
}

// GetLatestNotificationOverviews returns a single outstanding group-invite
// overview, defaulting the user to user-123 when the request omits one.
func (m *MockGroupService) GetLatestNotificationOverviews(
	_ context.Context,
	req *common.GetLatestNotificationOverviewsRequest,
) (*common.GetLatestNotificationOverviewsResponse, error) {
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = "user-123"
	}

	return &common.GetLatestNotificationOverviewsResponse{
		Overviews: []common.NotificationOverview{
			{
				ID:                "mock-group-invite-1",
				Source:            common.NotificationSourceGroup,
				Kind:              common.NotificationKindGroupInviteOutstanding,
				Title:             "Workspace invite",
				NotificationTitle: "Outstanding invitation",
				OccurredAt:        time.Now().Add(-30 * time.Minute).Format(time.RFC3339),
				Href:              "/settings?section=invitations",
				RevisionHash:      "mock-group-invite-1",
				Metadata: map[string]interface{}{
					"user_id": userID,
				},
			},
		},
	}, nil
}

// AcceptInvite echoes the fixture group, invite email and user ID; membership
// is not actually changed.
func (m *MockGroupService) AcceptInvite(ctx context.Context, req *group.AcceptInviteRequest) (*group.AcceptInviteResponse, error) {
	groupResp, err := m.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: req.GroupID})
	if err != nil {
		return nil, err
	}

	return &group.AcceptInviteResponse{
		Group:       groupResp.Group,
		InviteEmail: req.InviteEmail,
		UserID:      req.UserID,
	}, nil
}

// RejectInvite echoes the fixture group and invite email; no invitation state
// is changed.
func (m *MockGroupService) RejectInvite(ctx context.Context, req *group.RejectInviteRequest) (*group.RejectInviteResponse, error) {
	groupResp, err := m.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: req.GroupID})
	if err != nil {
		return nil, err
	}

	return &group.RejectInviteResponse{
		Group:       groupResp.Group,
		InviteEmail: req.InviteEmail,
	}, nil
}

// createMockGroup builds an active UniversalGroup with default domain
// collaborators, a week-old creation time, and generated members whose first
// member becomes the owner and second becomes an admin.
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

// contains reports whether the string slice includes an exactly equal item.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
