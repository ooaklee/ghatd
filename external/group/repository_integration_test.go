package group_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/benweissmann/memongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
)

func TestIntegration_GroupRepository_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start in-memory MongoDB
	mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "7.0.14"})
	if err != nil {
		t.Skipf("skipping integration test: unable to start memongo: %v", err)
	}
	t.Cleanup(func() {
		mongoServer.Stop()
	})

	dbName := memongo.RandomDatabase()
	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(mongoServer.URI(), dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = mongoHandler.Close(ctx)
	})

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	repo := group.NewRepository(store)

	t.Run("Create and retrieve group", func(t *testing.T) {
		newGroup := &group.UniversalGroup{
			ID:      group.NewDefaultIDGenerator().GenerateUUID(),
			Name:    "Engineering Team",
			RawName: "Engineering Team",
			Type:    group.GroupTypeTeam,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
		}

		createdGroup, err := repo.CreateGroup(ctx, newGroup)
		require.NoError(t, err)
		assert.Equal(t, newGroup.Name, createdGroup.Name)
		assert.NotEmpty(t, createdGroup.ID)

		retrieved, err := repo.GetGroupByID(ctx, createdGroup.ID)
		require.NoError(t, err)
		assert.Equal(t, createdGroup.ID, retrieved.ID)
		assert.Equal(t, createdGroup.Name, retrieved.Name)
		assert.Equal(t, group.GroupTypeTeam, retrieved.Type)
	})

	t.Run("Create multiple groups and filter by type", func(t *testing.T) {
		idGen := group.NewDefaultIDGenerator()

		team1 := &group.UniversalGroup{
			ID:      idGen.GenerateUUID(),
			Name:    "Product Team",
			RawName: "Product Team",
			Type:    group.GroupTypeTeam,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
		}

		team2 := &group.UniversalGroup{
			ID:      idGen.GenerateUUID(),
			Name:    "Backend Team",
			RawName: "Backend Team",
			Type:    group.GroupTypeTeam,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
		}

		dept := &group.UniversalGroup{
			ID:      idGen.GenerateUUID(),
			Name:    "Engineering Department",
			RawName: "Engineering Department",
			Type:    group.GroupTypeDepartment,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
		}

		_, err := repo.CreateGroup(ctx, team1)
		require.NoError(t, err)
		_, err = repo.CreateGroup(ctx, team2)
		require.NoError(t, err)
		_, err = repo.CreateGroup(ctx, dept)
		require.NoError(t, err)

		teams, err := repo.GetGroupsByType(ctx, group.GroupTypeTeam, 1, 10, "asc")
		require.NoError(t, err)
		assert.True(t, len(teams) >= 2, "should have at least 2 teams")

		depts, err := repo.GetGroupsByType(ctx, group.GroupTypeDepartment, 1, 10, "asc")
		require.NoError(t, err)
		assert.True(t, len(depts) >= 1, "should have at least 1 department")
	})

	t.Run("Update group status", func(t *testing.T) {
		idGen := group.NewDefaultIDGenerator()

		grp := &group.UniversalGroup{
			ID:      idGen.GenerateUUID(),
			Name:    "Status Update Test",
			RawName: "Status Update Test",
			Type:    group.GroupTypeTeam,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
		}

		created, err := repo.CreateGroup(ctx, grp)
		require.NoError(t, err)

		created.Status = group.GroupStatusSuspended
		updated, err := repo.UpdateGroup(ctx, created)
		require.NoError(t, err)
		assert.Equal(t, group.GroupStatusSuspended, updated.Status)

		retrieved, err := repo.GetGroupByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, group.GroupStatusSuspended, retrieved.Status)
	})

	t.Run("Add and retrieve members", func(t *testing.T) {
		idGen := group.NewDefaultIDGenerator()

		grp := &group.UniversalGroup{
			ID:      idGen.GenerateUUID(),
			Name:    "Members Test",
			RawName: "Members Test",
			Type:    group.GroupTypeTeam,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
			Members: []group.Member{},
		}

		created, err := repo.CreateGroup(ctx, grp)
		require.NoError(t, err)

		member := group.Member{
			ID:   group.NewDefaultIDGenerator().GenerateUUID(),
			Type: group.MemberTypeUser,
			Role: group.MemberRoleMember,
		}

		err = repo.AddMemberToGroup(ctx, created.ID, member)
		require.NoError(t, err)

		retrieved, err := repo.GetGroupByID(ctx, created.ID)
		require.NoError(t, err)
		assert.True(t, len(retrieved.Members) > 0, "group should have members")
	})

	t.Run("Get groups by status", func(t *testing.T) {
		idGen := group.NewDefaultIDGenerator()

		active := &group.UniversalGroup{
			ID:      idGen.GenerateUUID(),
			Name:    "Active Group",
			RawName: "Active Group",
			Type:    group.GroupTypeTeam,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
		}

		suspended := &group.UniversalGroup{
			ID:      idGen.GenerateUUID(),
			Name:    "Suspended Group",
			RawName: "Suspended Group",
			Type:    group.GroupTypeTeam,
			Status:  group.GroupStatusSuspended,
			OwnerID: testUserID,
		}

		_, err := repo.CreateGroup(ctx, active)
		require.NoError(t, err)
		_, err = repo.CreateGroup(ctx, suspended)
		require.NoError(t, err)

		activeGroups, err := repo.GetGroupsByStatus(ctx, group.GroupStatusActive, 1, 10, "asc")
		require.NoError(t, err)
		assert.True(t, len(activeGroups) > 0, "should have active groups")

		suspendedGroups, err := repo.GetGroupsByStatus(ctx, group.GroupStatusSuspended, 1, 10, "asc")
		require.NoError(t, err)
		assert.True(t, len(suspendedGroups) > 0, "should have suspended groups")
	})

	t.Run("Delete group", func(t *testing.T) {
		idGen := group.NewDefaultIDGenerator()

		grp := &group.UniversalGroup{
			ID:      idGen.GenerateUUID(),
			Name:    "Delete Test",
			RawName: "Delete Test",
			Type:    group.GroupTypeTeam,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
		}

		created, err := repo.CreateGroup(ctx, grp)
		require.NoError(t, err)

		err = repo.DeleteGroupByID(ctx, created.ID)
		require.NoError(t, err)

		_, err = repo.GetGroupByID(ctx, created.ID)
		assert.Error(t, err, "group should not be found after deletion")
	})

	t.Run("Validate group name uniqueness", func(t *testing.T) {
		idGen := group.NewDefaultIDGenerator()
		uniqueName := "Unique Group Name " + idGen.GenerateNanoID()

		grp := &group.UniversalGroup{
			ID:      idGen.GenerateUUID(),
			Name:    uniqueName,
			RawName: uniqueName,
			Type:    group.GroupTypeTeam,
			Status:  group.GroupStatusActive,
			OwnerID: testUserID,
		}

		created, err := repo.CreateGroup(ctx, grp)
		require.NoError(t, err)

		found, err := repo.GetGroupByName(ctx, uniqueName, group.GroupTypeTeam, false)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, created.ID, found.ID)

		notFound, err := repo.GetGroupByName(ctx, "Non Existent Group Name", group.GroupTypeTeam, false)
		require.NoError(t, err)
		assert.Nil(t, notFound)
	})

	t.Run("Bulk operations on multiple groups", func(t *testing.T) {
		idGen := group.NewDefaultIDGenerator()

		groupIDs := []string{}
		for i := 0; i < 3; i++ {
			grp := &group.UniversalGroup{
				ID:      idGen.GenerateUUID(),
				Name:    "Bulk Group " + idGen.GenerateNanoID(),
				RawName: "Bulk Group " + idGen.GenerateNanoID(),
				Type:    group.GroupTypeTeam,
				Status:  group.GroupStatusActive,
				OwnerID: testUserID,
			}
			created, err := repo.CreateGroup(ctx, grp)
			require.NoError(t, err)
			groupIDs = append(groupIDs, created.ID)
		}

		err := repo.BulkUpdateGroupsStatus(ctx, groupIDs, group.GroupStatusArchived)
		require.NoError(t, err)

		for _, id := range groupIDs {
			retrieved, err := repo.GetGroupByID(ctx, id)
			require.NoError(t, err)
			assert.Equal(t, group.GroupStatusArchived, retrieved.Status)
		}
	})
}

func TestIntegration_GroupService_GetGroupsByUserID_WithSampleDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	dbName := memongo.RandomDatabase()
	mongoURI := ""

	mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "7.0.14"})
	if err == nil {
		mongoURI = mongoServer.URI()
		t.Cleanup(func() {
			mongoServer.Stop()
		})
	} else {
		mongoURI = strings.TrimSpace(os.Getenv("GROUP_IT_MONGO_URI"))
		if mongoURI == "" {
			t.Skipf("skipping integration test: memongo unavailable (%v). set GROUP_IT_MONGO_URI to run against a real MongoDB", err)
		}
	}

	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(mongoURI, dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = mongoHandler.Close(ctx)
	})

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	repo := group.NewRepository(store)

	svc, err := group.NewService(
		repo,
		nil,
		group.DefaultGroupConfig(),
		group.NewDefaultIDGenerator(),
		group.NewDefaultTimeProvider(),
		group.NewDefaultStringUtils(),
	)
	require.NoError(t, err)

	const (
		primaryUserID   = "c530f049-73ff-499b-9802-1765de1469f8"
		secondaryUserID = "997d491d-c2ff-4e67-b91f-b4ea58231317"
	)

	seedGroups := []*group.UniversalGroup{
		{
			ID:      "e4cd2ebe-c979-4814-a526-19d33cf47811",
			Name:    "school-for-boys",
			RawName: "School for Boys",
			Type:    group.GroupTypeOrganisation,
			Status:  group.GroupStatusActive,
			OwnerID: primaryUserID,
			Version: 1,
			NanoID:  "OkqJxaZBmdRx0h0gIRtbF",
			DisplayInfo: &group.DisplayInfo{
				Description: "Test 2 me ,mm",
			},
			Members: []group.Member{
				{ID: primaryUserID, Type: group.MemberTypeUser, Role: group.MemberRoleAdmin, JoinedAt: "2026-04-30T11:07:52Z"},
				{ID: secondaryUserID, Type: group.MemberTypeUser, Role: group.MemberRoleAdmin, JoinedAt: "2026-04-30T11:08:13Z"},
			},
			Settings:     &group.GroupSettings{Visibility: group.VisibilityPublic},
			Integrations: &group.Integrations{},
			Metadata:     &group.GroupMetadata{CreatedAt: "2026-04-30T11:07:52Z", UpdatedAt: "2026-05-03T05:53:42Z"},
		},
		{
			ID:            "560a256d-c70c-4a8a-97c2-291a3c35a1cb",
			Name:          "testing-group-1",
			RawName:       "Testing group",
			Type:          group.GroupTypeDepartment,
			Status:        group.GroupStatusActive,
			ParentGroupID: "e4cd2ebe-c979-4814-a526-19d33cf47811",
			OwnerID:       secondaryUserID,
			Lineage:       []string{"e4cd2ebe-c979-4814-a526-19d33cf47811"},
			Version:       1,
			NanoID:        "BpWa9rFChENvA8P5pMN1l",
			Members: []group.Member{
				{ID: secondaryUserID, Type: group.MemberTypeUser, Role: group.MemberRoleAdmin, JoinedAt: "2026-04-30T19:30:21Z"},
			},
			Settings:     &group.GroupSettings{Visibility: group.VisibilityPrivate},
			Integrations: &group.Integrations{},
			Metadata:     &group.GroupMetadata{CreatedAt: "2026-04-30T19:09:28Z", UpdatedAt: "2026-05-03T13:43:35Z"},
		},
		{
			ID:            "9abea3aa-0b80-42f9-8cb4-33d6da2e1304",
			Name:          "another-group",
			RawName:       "another group",
			Type:          group.GroupTypeTeam,
			Status:        group.GroupStatusActive,
			ParentGroupID: "560a256d-c70c-4a8a-97c2-291a3c35a1cb",
			OwnerID:       secondaryUserID,
			Lineage:       []string{"e4cd2ebe-c979-4814-a526-19d33cf47811", "560a256d-c70c-4a8a-97c2-291a3c35a1cb"},
			Version:       1,
			NanoID:        "rMchHC8qoD3a-zCoc4JNR",
			Members: []group.Member{
				{ID: secondaryUserID, Type: group.MemberTypeUser, Role: group.MemberRoleAdmin, JoinedAt: "2026-04-30T22:32:38Z"},
			},
			Settings:     &group.GroupSettings{Visibility: group.VisibilityPrivate},
			Integrations: &group.Integrations{},
			Metadata:     &group.GroupMetadata{CreatedAt: "2026-04-30T22:32:38Z", UpdatedAt: "2026-05-02T14:09:10Z"},
		},
		{
			ID:            "0e8e0a88-62ad-423e-b400-e9fa4d227cc9",
			Name:          "can-you-dig-it",
			RawName:       "Can you dig it",
			Type:          group.GroupTypeTribe,
			Status:        group.GroupStatusActive,
			ParentGroupID: "560a256d-c70c-4a8a-97c2-291a3c35a1cb",
			OwnerID:       "",
			Lineage:       []string{"e4cd2ebe-c979-4814-a526-19d33cf47811", "560a256d-c70c-4a8a-97c2-291a3c35a1cb"},
			Version:       1,
			NanoID:        "EcUGvWQtU_bc-V4ddfj40",
			Members:       []group.Member{},
			Settings:      &group.GroupSettings{Visibility: group.VisibilityPrivate},
			Integrations:  &group.Integrations{},
			Metadata:      &group.GroupMetadata{CreatedAt: "2026-04-30T22:33:39Z", UpdatedAt: "2026-04-30T22:33:39Z"},
		},
		{
			ID:            "0b697f3f-879d-490a-b232-e2de43a9c097",
			Name:          "how-deep-can-this-go-",
			RawName:       "How deep can this go?",
			Type:          group.GroupTypeSquad,
			Status:        group.GroupStatusActive,
			ParentGroupID: "0e8e0a88-62ad-423e-b400-e9fa4d227cc9",
			OwnerID:       secondaryUserID,
			Lineage:       []string{"e4cd2ebe-c979-4814-a526-19d33cf47811", "560a256d-c70c-4a8a-97c2-291a3c35a1cb", "0e8e0a88-62ad-423e-b400-e9fa4d227cc9"},
			Version:       1,
			NanoID:        "DisqXba3PzIiUmTscnbxe",
			Members: []group.Member{
				{ID: secondaryUserID, Type: group.MemberTypeUser, Role: group.MemberRoleAdmin, JoinedAt: "2026-04-30T22:34:30Z"},
			},
			Settings:     &group.GroupSettings{Visibility: group.VisibilityPrivate},
			Integrations: &group.Integrations{},
			Metadata:     &group.GroupMetadata{CreatedAt: "2026-04-30T22:34:30Z", UpdatedAt: "2026-04-30T22:34:30Z"},
		},
	}

	for i := range seedGroups {
		_, createErr := repo.CreateGroup(ctx, seedGroups[i])
		require.NoError(t, createErr)
	}

	t.Run("Primary user exact request returns direct reference and descendants tree", func(t *testing.T) {
		resp, getErr := svc.GetGroupsByUserID(ctx, &group.GetGroupsByUserIDRequest{
			UserID:             primaryUserID,
			IncludeDescendants: true,
			PrefixName:         true,
		})
		require.NoError(t, getErr)
		require.NotNil(t, resp)

		// Primary user is directly referenced by ownership/member on root group.
		assert.Len(t, resp.Groups, 1)
		assert.Equal(t, "e4cd2ebe-c979-4814-a526-19d33cf47811", resp.Groups[0].ID)

		require.Contains(t, resp.Descendants, "e4cd2ebe-c979-4814-a526-19d33cf47811")
		levels := resp.Descendants["e4cd2ebe-c979-4814-a526-19d33cf47811"]
		assert.NotEmpty(t, levels)

		descendantIDs := map[string]bool{}
		for _, depthGroups := range levels {
			for _, node := range depthGroups {
				descendantIDs[node.ID] = true
			}
		}

		assert.True(t, descendantIDs["e4cd2ebe-c979-4814-a526-19d33cf47811"])
		assert.True(t, descendantIDs["560a256d-c70c-4a8a-97c2-291a3c35a1cb"])
		assert.True(t, descendantIDs["9abea3aa-0b80-42f9-8cb4-33d6da2e1304"])
		assert.True(t, descendantIDs["0e8e0a88-62ad-423e-b400-e9fa4d227cc9"])
		assert.True(t, descendantIDs["0b697f3f-879d-490a-b232-e2de43a9c097"])
	})

	t.Run("Secondary user appears across direct references and descendant propagation", func(t *testing.T) {
		resp, getErr := svc.GetGroupsByUserID(ctx, &group.GetGroupsByUserIDRequest{
			UserID:             secondaryUserID,
			IncludeDescendants: true,
			PrefixName:         true,
		})
		require.NoError(t, getErr)
		require.NotNil(t, resp)

		// Secondary user is directly referenced in 4 groups from seed data.
		directIDs := map[string]bool{}
		for _, grp := range resp.Groups {
			directIDs[grp.ID] = true
		}

		assert.True(t, directIDs["e4cd2ebe-c979-4814-a526-19d33cf47811"])
		assert.True(t, directIDs["560a256d-c70c-4a8a-97c2-291a3c35a1cb"])
		assert.True(t, directIDs["9abea3aa-0b80-42f9-8cb4-33d6da2e1304"])
		assert.True(t, directIDs["0b697f3f-879d-490a-b232-e2de43a9c097"])

		require.Contains(t, resp.Descendants, "e4cd2ebe-c979-4814-a526-19d33cf47811")
	})

	t.Run("RemoveUserFromAllGroups removes persisted memberships and clears descendant ownership", func(t *testing.T) {
		resp, removeErr := svc.RemoveUserFromAllGroups(ctx, &group.RemoveUserFromAllGroupsRequest{
			UserID: secondaryUserID,
		})
		require.NoError(t, removeErr)
		require.NotNil(t, resp)
		assert.True(t, resp.Success)
		assert.Equal(t, 1, resp.TotalRootGroupsAffected)

		rootGroup, err := repo.GetGroupByID(ctx, "e4cd2ebe-c979-4814-a526-19d33cf47811")
		require.NoError(t, err)
		assert.True(t, rootGroup.HasMember(primaryUserID))
		assert.False(t, rootGroup.HasMember(secondaryUserID))
		assert.Equal(t, primaryUserID, rootGroup.OwnerID)

		departmentGroup, err := repo.GetGroupByID(ctx, "560a256d-c70c-4a8a-97c2-291a3c35a1cb")
		require.NoError(t, err)
		assert.False(t, departmentGroup.HasMember(secondaryUserID))
		assert.Empty(t, departmentGroup.OwnerID)

		teamGroup, err := repo.GetGroupByID(ctx, "9abea3aa-0b80-42f9-8cb4-33d6da2e1304")
		require.NoError(t, err)
		assert.False(t, teamGroup.HasMember(secondaryUserID))
		assert.Empty(t, teamGroup.OwnerID)

		squadGroup, err := repo.GetGroupByID(ctx, "0b697f3f-879d-490a-b232-e2de43a9c097")
		require.NoError(t, err)
		assert.False(t, squadGroup.HasMember(secondaryUserID))
		assert.Empty(t, squadGroup.OwnerID)

		postRemovalResp, err := svc.GetGroupsByUserID(ctx, &group.GetGroupsByUserIDRequest{
			UserID:             secondaryUserID,
			IncludeDescendants: true,
		})
		require.NoError(t, err)
		require.NotNil(t, postRemovalResp)
		assert.Empty(t, postRemovalResp.Groups)
		assert.Empty(t, postRemovalResp.Descendants)
	})
}
