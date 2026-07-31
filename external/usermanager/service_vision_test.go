package usermanager_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/vision"
)

type mockVisionUserService struct {
	users map[string]userv2.UniversalUser
}

func (*mockVisionUserService) GetUserMicroProfile(context.Context, *userv2.GetUserMicroProfileRequest) (*userv2.GetUserMicroProfileResponse, error) {
	return nil, errors.New("not implemented")
}
func (*mockVisionUserService) GetUserProfile(context.Context, *userv2.GetUserProfileRequest) (*userv2.GetUserProfileResponse, error) {
	return nil, errors.New("not implemented")
}
func (*mockVisionUserService) GetUserByID(context.Context, *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *mockVisionUserService) GetUsers(_ context.Context, req *userv2.GetUsersRequest) (*userv2.GetUsersResponse, error) {
	users := make([]userv2.UniversalUser, 0, len(req.IDsFilter))
	for _, id := range req.IDsFilter {
		if user, ok := m.users[id]; ok {
			users = append(users, user)
		}
	}
	return &userv2.GetUsersResponse{Users: users}, nil
}
func (*mockVisionUserService) GetUserByEmail(context.Context, *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error) {
	return nil, errors.New("not implemented")
}
func (*mockVisionUserService) UpdateUser(context.Context, *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error) {
	return nil, errors.New("not implemented")
}
func (*mockVisionUserService) DeleteUser(context.Context, *userv2.DeleteUserRequest) error {
	return errors.New("not implemented")
}

type mockVisionService struct {
	item           *vision.Vision
	config         *vision.VisionConfigCapabilities
	deleted        bool
	updatedRequest *vision.UpdateVisionRequest
}

func (m *mockVisionService) CreateVision(context.Context, *vision.CreateVisionRequest) (*vision.VisionResponse, error) {
	return &vision.VisionResponse{Vision: m.item}, nil
}
func (m *mockVisionService) GetVisionByNanoID(context.Context, *vision.GetVisionByNanoIDRequest) (*vision.VisionResponse, error) {
	return &vision.VisionResponse{Vision: m.item}, nil
}
func (m *mockVisionService) GetVisions(context.Context, *vision.GetVisionsRequest) (*vision.GetVisionsResponse, error) {
	return &vision.GetVisionsResponse{Visions: []vision.Vision{*m.item}, Total: 1}, nil
}
func (m *mockVisionService) GetVisionConfig(context.Context) (*vision.GetVisionConfigResponse, error) {
	return &vision.GetVisionConfigResponse{Config: m.config}, nil
}
func (m *mockVisionService) UpdateVision(_ context.Context, req *vision.UpdateVisionRequest) (*vision.VisionResponse, error) {
	m.updatedRequest = req
	if req.Title != nil {
		m.item.Title = *req.Title
	}
	if req.Description != nil {
		m.item.Description = *req.Description
	}
	return &vision.VisionResponse{Vision: m.item}, nil
}
func (m *mockVisionService) UpdateVisionStatus(_ context.Context, req *vision.UpdateVisionStatusRequest) (*vision.VisionResponse, error) {
	m.item.Status = req.Status
	return &vision.VisionResponse{Vision: m.item}, nil
}
func (m *mockVisionService) DeleteVision(context.Context, *vision.DeleteVisionRequest) (*vision.DeleteVisionResponse, error) {
	m.deleted = true
	return &vision.DeleteVisionResponse{Deleted: true}, nil
}
func (m *mockVisionService) SetVisionVote(context.Context, *vision.SetVisionVoteRequest) (*vision.VisionResponse, error) {
	return &vision.VisionResponse{Vision: m.item}, nil
}
func (m *mockVisionService) RemoveVisionVote(context.Context, *vision.RemoveVisionVoteRequest) (*vision.VisionResponse, error) {
	return &vision.VisionResponse{Vision: m.item}, nil
}
func (m *mockVisionService) AddVisionComment(context.Context, *vision.AddVisionCommentRequest) (*vision.VisionResponse, error) {
	return &vision.VisionResponse{Vision: m.item}, nil
}
func (m *mockVisionService) SetVisionCommentVote(context.Context, *vision.SetVisionCommentVoteRequest) (*vision.VisionResponse, error) {
	return &vision.VisionResponse{Vision: m.item}, nil
}
func (m *mockVisionService) RemoveVisionCommentVote(context.Context, *vision.RemoveVisionCommentVoteRequest) (*vision.VisionResponse, error) {
	return &vision.VisionResponse{Vision: m.item}, nil
}

func TestVisionProjectionUsesNanoIDsAndHidesRawUserIDs(t *testing.T) {
	item := &vision.Vision{
		ID:              "vision-1",
		NanoID:          "vision-nano",
		Title:           "Privacy-safe feedback",
		CreatedByUserID: "user-1",
		Voters: map[vision.VisionVote][]string{
			vision.VisionVoteUpvote: {"user-2"},
		},
		Comments: []vision.VisionComment{{
			ID: "comment-1", UserID: "user-3", Message: "Ask <@nano-2>",
			Voters: map[vision.VisionVote][]string{
				vision.VisionVoteUpvote: {"user-4"},
			},
		}},
		CommentCount: 1,
	}
	userService := &mockVisionUserService{users: map[string]userv2.UniversalUser{
		"user-1": {ID: "user-1", NanoID: "nano-1", PersonalInfo: &userv2.PersonalInfo{FullName: "Creator"}},
		"user-2": {ID: "user-2", NanoID: "nano-2", PersonalInfo: &userv2.PersonalInfo{FullName: "Voter"}},
		"user-3": {ID: "user-3", NanoID: "nano-3", PersonalInfo: &userv2.PersonalInfo{FullName: "Commenter"}},
		"user-4": {ID: "user-4", NanoID: "nano-4", PersonalInfo: &userv2.PersonalInfo{FullName: "Comment voter"}},
	}}
	service := usermanager.NewService(&usermanager.NewServiceRequest{UserService: userService}).
		WithVisionService(&mockVisionService{item: item})

	response, err := service.GetVisionByNanoID(context.Background(), &vision.GetVisionByNanoIDRequest{NanoID: item.NanoID})
	require.NoError(t, err)
	assert.Equal(t, "Creator", response.Users["nano-1"].FullName)
	assert.Equal(t, "nano-1", response.Vision.CreatedByUserNanoID)
	assert.Equal(t, "nano-3", response.Vision.Comments[0].UserNanoID)
	assert.Equal(t, "Ask <@nano-2>", response.Vision.Comments[0].Message)
	assert.Equal(t, 1, response.Vision.Votes.Upvotes)
	assert.Equal(t, 1, response.Vision.CommentCount)
	assert.Nil(t, response.Vision.ViewerVote)
	assert.False(t, response.Vision.CanDelete)
	assert.NotContains(t, response.Users, "nano-2", "voters are not returned as user records")
	assert.NotContains(t, response.Users, "nano-4", "comment voters are not returned as user records")

	body, err := json.Marshal(response)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "user-1")
	assert.NotContains(t, string(body), "user-2")
	assert.NotContains(t, string(body), "user-3")
	assert.NotContains(t, string(body), "user-4")
	assert.NotContains(t, string(body), `"voters"`)
	assert.NotContains(t, string(body), `"created_by_user_id"`)
}

func TestVisionProjectionIncludesOnlyAuthenticatedViewerVote(t *testing.T) {
	item := &vision.Vision{
		ID:     "vision-1",
		NanoID: "vision-nano",
		Voters: map[vision.VisionVote][]string{
			vision.VisionVoteDownvote: {},
			vision.VisionVoteUpvote:   {"viewer-user"},
		},
	}
	service := usermanager.NewService(&usermanager.NewServiceRequest{
		UserService: &mockVisionUserService{},
	}).WithVisionService(&mockVisionService{item: item})

	publicResponse, err := service.GetVisionByNanoID(context.Background(), &vision.GetVisionByNanoIDRequest{NanoID: item.NanoID})
	require.NoError(t, err)
	assert.Nil(t, publicResponse.Vision.ViewerVote)

	ctx := accessmanagerhelpers.TransitWith(context.Background(), "viewer-user")
	ctx = accessmanagerhelpers.TransitAuthenticatedWith(ctx, true)
	authenticatedResponse, err := service.GetVisionByNanoID(ctx, &vision.GetVisionByNanoIDRequest{NanoID: item.NanoID})
	require.NoError(t, err)
	require.NotNil(t, authenticatedResponse.Vision.ViewerVote)
	assert.Equal(t, vision.VisionVoteUpvote, *authenticatedResponse.Vision.ViewerVote)
}

func TestVisionProjectionIdentifiesViewerAndEditCapability(t *testing.T) {
	item := &vision.Vision{
		ID:              "vision-1",
		NanoID:          "vision-nano",
		CreatedByUserID: "owner-user",
		Comments: []vision.VisionComment{{
			ID:     "comment-1",
			UserID: "commenter-user",
		}},
	}
	users := map[string]userv2.UniversalUser{
		"owner-user": {
			ID:     "owner-user",
			NanoID: "owner-nano",
			Roles:  []string{userv2.UserRoleUser},
		},
		"commenter-user": {
			ID:     "commenter-user",
			NanoID: "commenter-nano",
			Roles:  []string{userv2.UserRoleUser},
		},
	}
	service := usermanager.NewService(&usermanager.NewServiceRequest{
		UserService: &mockVisionUserService{users: users},
	}).WithVisionService(&mockVisionService{item: item})

	owner := users["owner-user"]
	ownerContext := authenticatedVisionContext(context.Background(), &owner)
	ownerResponse, err := service.GetVisionByNanoID(
		ownerContext,
		&vision.GetVisionByNanoIDRequest{NanoID: item.NanoID},
	)
	require.NoError(t, err)
	assert.Equal(t, "owner-nano", ownerResponse.ViewerUserNanoID)
	assert.True(t, ownerResponse.Vision.CanEdit)
	assert.True(t, ownerResponse.Vision.CanDelete)

	commenter := users["commenter-user"]
	commenterContext := authenticatedVisionContext(context.Background(), &commenter)
	commenterResponse, err := service.GetVisionByNanoID(
		commenterContext,
		&vision.GetVisionByNanoIDRequest{NanoID: item.NanoID},
	)
	require.NoError(t, err)
	assert.Equal(t, "commenter-nano", commenterResponse.ViewerUserNanoID)
	assert.False(t, commenterResponse.Vision.CanEdit)
	assert.False(t, commenterResponse.Vision.CanDelete)

	admin := userv2.UniversalUser{
		ID:     "admin-user",
		NanoID: "admin-nano",
		Roles:  []string{userv2.UserRoleAdmin},
	}
	adminResponse, err := service.GetVisionByNanoID(
		authenticatedVisionContext(context.Background(), &admin),
		&vision.GetVisionByNanoIDRequest{NanoID: item.NanoID},
	)
	require.NoError(t, err)
	assert.Empty(t, adminResponse.ViewerUserNanoID, "non-participant admin is not added to the public user map")
	assert.True(t, adminResponse.Vision.CanEdit)
	assert.True(t, adminResponse.Vision.CanDelete)
}

func TestVisionUpdateAllowsOwnerOrAdminAndRejectsOtherUsers(t *testing.T) {
	title := "Updated title"
	description := ""
	item := &vision.Vision{
		ID:              "vision-1",
		NanoID:          "vision-nano",
		Title:           "Original title",
		Description:     "Original description",
		CreatedByUserID: "owner-user",
	}
	rawService := &mockVisionService{item: item}
	users := map[string]userv2.UniversalUser{
		"owner-user": {
			ID:     "owner-user",
			NanoID: "owner-nano",
			Roles:  []string{userv2.UserRoleUser},
		},
	}
	service := usermanager.NewService(&usermanager.NewServiceRequest{
		UserService: &mockVisionUserService{users: users},
	}).WithVisionService(rawService)

	owner := users["owner-user"]
	response, err := service.UpdateVision(
		authenticatedVisionContext(context.Background(), &owner),
		&vision.UpdateVisionRequest{
			NanoID:          item.NanoID,
			Title:           &title,
			Description:     &description,
			Metadata:        map[string]interface{}{"not": "owner editable"},
			UpdatedByUserID: "owner-user",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, title, response.Vision.Title)
	assert.Empty(t, response.Vision.Description)
	assert.Nil(t, rawService.updatedRequest.Metadata)

	otherUser := userv2.UniversalUser{
		ID:    "other-user",
		Roles: []string{userv2.UserRoleUser},
	}
	_, err = service.UpdateVision(
		authenticatedVisionContext(context.Background(), &otherUser),
		&vision.UpdateVisionRequest{
			NanoID:          item.NanoID,
			Title:           &title,
			UpdatedByUserID: "owner-user",
		},
	)
	assert.ErrorIs(t, err, usermanager.ErrVisionEditForbidden)

	admin := userv2.UniversalUser{
		ID:    "admin-user",
		Roles: []string{userv2.UserRoleAdmin},
	}
	_, err = service.UpdateVision(
		authenticatedVisionContext(context.Background(), &admin),
		&vision.UpdateVisionRequest{
			NanoID:          item.NanoID,
			Title:           &title,
			UpdatedByUserID: admin.ID,
		},
	)
	require.NoError(t, err)
}

func TestVisionDeleteAllowsOwnerOrAdminAndRejectsOtherUsers(t *testing.T) {
	item := &vision.Vision{
		ID:              "vision-1",
		NanoID:          "vision-nano",
		CreatedByUserID: "owner-user",
	}
	rawService := &mockVisionService{item: item}
	service := usermanager.NewService(&usermanager.NewServiceRequest{
		UserService: &mockVisionUserService{},
	}).WithVisionService(rawService)
	request := &vision.DeleteVisionRequest{NanoID: item.NanoID}

	otherUser := &userv2.UniversalUser{
		ID:    "other-user",
		Roles: []string{userv2.UserRoleUser},
	}
	_, err := service.DeleteVision(
		authenticatedVisionContext(context.Background(), otherUser),
		request,
	)
	assert.ErrorIs(t, err, usermanager.ErrVisionDeleteForbidden)
	assert.False(t, rawService.deleted)

	owner := &userv2.UniversalUser{
		ID:    "owner-user",
		Roles: []string{userv2.UserRoleUser},
	}
	response, err := service.DeleteVision(
		authenticatedVisionContext(context.Background(), owner),
		request,
	)
	require.NoError(t, err)
	assert.True(t, response.Deleted)
	assert.True(t, rawService.deleted)

	rawService.deleted = false
	admin := &userv2.UniversalUser{
		ID:    "admin-user",
		Roles: []string{userv2.UserRoleAdmin},
	}
	response, err = service.DeleteVision(
		authenticatedVisionContext(context.Background(), admin),
		request,
	)
	require.NoError(t, err)
	assert.True(t, response.Deleted)
	assert.True(t, rawService.deleted)
}

func authenticatedVisionContext(ctx context.Context, user *userv2.UniversalUser) context.Context {
	ctx = accessmanagerhelpers.TransitWith(ctx, user.ID)
	ctx = accessmanagerhelpers.TransitUserWith(ctx, user)
	return accessmanagerhelpers.TransitAuthenticatedWith(ctx, true)
}

func TestVisionIntegrationRequiresService(t *testing.T) {
	service := usermanager.NewService(&usermanager.NewServiceRequest{UserService: &mockVisionUserService{}})
	_, err := service.GetVisions(context.Background(), &vision.GetVisionsRequest{})
	assert.ErrorIs(t, err, usermanager.ErrVisionServiceNotEnabled)
}

func TestVisionAdminOperationsReturnSafeConfigAndEnrichedProjection(t *testing.T) {
	item := &vision.Vision{
		ID:              "vision-1",
		NanoID:          "vision-nano",
		CreatedByUserID: "user-1",
	}
	capabilities := &vision.VisionConfigCapabilities{
		StatusTransitions: map[vision.VisionStatus][]vision.VisionStatus{
			vision.VisionStatusUnderReview: {""},
		},
		ValidTypes:        []vision.VisionType{vision.VisionTypeFeedback},
		DownvotingEnabled: true,
	}
	rawService := &mockVisionService{item: item, config: capabilities}
	service := usermanager.NewService(&usermanager.NewServiceRequest{
		UserService: &mockVisionUserService{users: map[string]userv2.UniversalUser{
			"user-1": {
				ID:           "user-1",
				NanoID:       "nano-1",
				PersonalInfo: &userv2.PersonalInfo{FullName: "Creator"},
			},
		}},
	}).WithVisionService(rawService)

	configResponse, err := service.GetVisionConfig(context.Background())
	require.NoError(t, err)
	assert.Same(t, capabilities, configResponse.Config)

	updateResponse, err := service.UpdateVisionStatus(
		context.Background(),
		&vision.UpdateVisionStatusRequest{
			NanoID:          item.NanoID,
			Status:          vision.VisionStatusUnderReview,
			UpdatedByUserID: "admin-user",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, vision.VisionStatusUnderReview, updateResponse.Vision.Status)
	assert.Equal(t, "nano-1", updateResponse.Vision.CreatedByUserNanoID)

	admin := &userv2.UniversalUser{
		ID:    "admin-user",
		Roles: []string{userv2.UserRoleAdmin},
	}
	deleteResponse, err := service.DeleteVision(
		authenticatedVisionContext(context.Background(), admin),
		&vision.DeleteVisionRequest{NanoID: item.NanoID},
	)
	require.NoError(t, err)
	assert.True(t, deleteResponse.Deleted)
	assert.True(t, rawService.deleted)
}
