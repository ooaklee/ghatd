package usermanager_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/reminder"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
)

type mockReminderUserService struct {
	users map[string]*userv2.UniversalUser
}

func (m *mockReminderUserService) GetUserMicroProfile(ctx context.Context, r *userv2.GetUserMicroProfileRequest) (*userv2.GetUserMicroProfileResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderUserService) GetUserProfile(ctx context.Context, r *userv2.GetUserProfileRequest) (*userv2.GetUserProfileResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderUserService) GetUserByID(ctx context.Context, r *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	user, ok := m.users[r.ID]
	if !ok {
		return nil, errors.New("user not found")
	}
	return &userv2.GetUserByIDResponse{User: user}, nil
}

func (m *mockReminderUserService) GetUsers(ctx context.Context, r *userv2.GetUsersRequest) (*userv2.GetUsersResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderUserService) GetUserByEmail(ctx context.Context, r *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderUserService) UpdateUser(ctx context.Context, r *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderUserService) DeleteUser(ctx context.Context, r *userv2.DeleteUserRequest) error {
	return errors.New("not implemented")
}

type mockReminderService struct {
	listRemindersRequest *reminder.ListRemindersRequest
	getStatsRequest      *reminder.GetReminderStatsRequest
	getDueRequest        *reminder.GetDueRemindersRequest
}

func (m *mockReminderService) CreateReminder(ctx context.Context, r *reminder.CreateReminderRequest) (*reminder.CreateReminderResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderService) GetReminderByID(ctx context.Context, r *reminder.GetReminderByIDRequest) (*reminder.GetReminderByIDResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderService) ListReminders(ctx context.Context, r *reminder.ListRemindersRequest) (*reminder.ListRemindersResponse, error) {
	m.listRemindersRequest = r
	return &reminder.ListRemindersResponse{}, nil
}

func (m *mockReminderService) GetRemindersForTargetTypeByUserID(ctx context.Context, r *reminder.GetRemindersForTargetTypeByUserIDRequest) (*reminder.ListRemindersResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderService) GetActiveRemindersForTargetTypeByUserID(ctx context.Context, r *reminder.GetActiveRemindersForTargetTypeByUserIDRequest) (*reminder.ListRemindersResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderService) UpdateReminderByID(ctx context.Context, r *reminder.UpdateReminderByIDRequest) (*reminder.UpdateReminderByIDResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderService) DeleteReminderByID(ctx context.Context, r *reminder.DeleteReminderByIDRequest) error {
	return errors.New("not implemented")
}

func (m *mockReminderService) DisableReminderByID(ctx context.Context, r *reminder.DisableReminderByIDRequest) (*reminder.UpdateReminderByIDResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockReminderService) GetReminderStats(ctx context.Context, r *reminder.GetReminderStatsRequest) (*reminder.GetReminderStatsResponse, error) {
	m.getStatsRequest = r
	return &reminder.GetReminderStatsResponse{}, nil
}

func (m *mockReminderService) GetDueReminders(ctx context.Context, r *reminder.GetDueRemindersRequest) (*reminder.GetDueRemindersResponse, error) {
	m.getDueRequest = r
	return &reminder.GetDueRemindersResponse{}, nil
}

func TestServiceListRemindersLocksNonAdminToOwnUserID(t *testing.T) {
	t.Parallel()

	reminderSvc := &mockReminderService{}
	svc := &usermanager.Service{
		UserService: &mockReminderUserService{
			users: map[string]*userv2.UniversalUser{
				"user-1": {ID: "user-1"},
			},
		},
		ReminderService: reminderSvc,
	}

	res, err := svc.ListReminders(context.Background(), &usermanager.ListRemindersRequest{
		UserID:       "user-1",
		FilterUserID: "user-2",
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, reminderSvc.listRemindersRequest)
	assert.Equal(t, "user-1", reminderSvc.listRemindersRequest.UserID)
}

func TestServiceListRemindersAllowsAdminAllUserScope(t *testing.T) {
	t.Parallel()

	reminderSvc := &mockReminderService{}
	svc := &usermanager.Service{
		UserService: &mockReminderUserService{
			users: map[string]*userv2.UniversalUser{
				"admin-1": {ID: "admin-1", Roles: []string{"ADMIN"}},
			},
		},
		ReminderService: reminderSvc,
	}

	res, err := svc.ListReminders(context.Background(), &usermanager.ListRemindersRequest{
		UserID: "admin-1",
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, reminderSvc.listRemindersRequest)
	assert.Empty(t, reminderSvc.listRemindersRequest.UserID)
}

func TestServiceGetDueRemindersPassesOptionalUserFilters(t *testing.T) {
	t.Parallel()

	reminderSvc := &mockReminderService{}
	svc := &usermanager.Service{
		UserService: &mockReminderUserService{
			users: map[string]*userv2.UniversalUser{
				"admin-1": {ID: "admin-1", Roles: []string{"ADMIN"}},
			},
		},
		ReminderService: reminderSvc,
	}

	res, err := svc.GetDueReminders(context.Background(), &usermanager.GetDueRemindersRequest{
		UserID:        "admin-1",
		FilterUserID:  "user-1",
		FilterUserIDs: []string{"user-2", "user-3"},
		DueBefore:     "2026-05-15T10:00:00Z",
		Limit:         20,
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, reminderSvc.getDueRequest)
	assert.Empty(t, reminderSvc.getDueRequest.UserID)
	assert.Equal(t, []string{"user-1", "user-2", "user-3"}, reminderSvc.getDueRequest.UserIDs)
	assert.Equal(t, "2026-05-15T10:00:00Z", reminderSvc.getDueRequest.DueBefore)
	assert.EqualValues(t, 20, reminderSvc.getDueRequest.Limit)
}

func TestServiceGetDueRemindersLocksNonAdminToOwnUserID(t *testing.T) {
	t.Parallel()

	reminderSvc := &mockReminderService{}
	svc := &usermanager.Service{
		UserService: &mockReminderUserService{
			users: map[string]*userv2.UniversalUser{
				"user-1": {ID: "user-1"},
			},
		},
		ReminderService: reminderSvc,
	}

	res, err := svc.GetDueReminders(context.Background(), &usermanager.GetDueRemindersRequest{
		UserID:        "user-1",
		FilterUserID:  "user-2",
		FilterUserIDs: []string{"user-3"},
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, reminderSvc.getDueRequest)
	assert.Equal(t, "user-1", reminderSvc.getDueRequest.UserID)
	assert.Empty(t, reminderSvc.getDueRequest.UserIDs)
}

func TestServiceGetReminderStatsLocksNonAdminToOwnUserID(t *testing.T) {
	t.Parallel()

	reminderSvc := &mockReminderService{}
	svc := &usermanager.Service{
		UserService: &mockReminderUserService{
			users: map[string]*userv2.UniversalUser{
				"user-1": {ID: "user-1"},
			},
		},
		ReminderService: reminderSvc,
	}

	res, err := svc.GetReminderStats(context.Background(), &usermanager.GetReminderStatsRequest{
		UserID:        "user-1",
		FilterUserIDs: []string{"user-2"},
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, reminderSvc.getStatsRequest)
	assert.Equal(t, "user-1", reminderSvc.getStatsRequest.UserID)
	assert.Empty(t, reminderSvc.getStatsRequest.UserIDs)
}
