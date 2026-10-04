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

func (m *mockReminderUserService) GetUsersByIDs(context.Context, *userv2.GetUsersByIDsRequest) (*userv2.GetUsersByIDsResponse, error) {
	return nil, errors.New("not implemented")
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

// TestReminderListActorScope keeps an admin's identity separate from the target
// filter while ordinary users remain restricted to their own reminders.
func TestReminderListActorScope(t *testing.T) {
	for _, tc := range []struct {
		name, actor, target, want string
		admin                     bool
	}{
		{"member cannot select another user", "caller", "target", "caller", false},
		{"admin can list all users", "admin", "", "", true},
		{"admin can select a different user", "admin", "target", "target", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reminders := &mockReminderService{}
			actor := &userv2.UniversalUser{ID: tc.actor}
			if tc.admin {
				actor.Roles = []string{"ADMIN"}
			}
			service := &usermanager.Service{UserService: &mockReminderUserService{users: map[string]*userv2.UniversalUser{tc.actor: actor}}, ReminderService: reminders}
			result, err := service.ListReminders(context.Background(), &usermanager.ListRemindersRequest{ActorID: tc.actor, FilterUserID: tc.target})
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, reminders.listRemindersRequest)
			assert.Equal(t, tc.want, reminders.listRemindersRequest.UserID)
		})
	}
}

// TestReminderAggregateActorScope applies the same actor-versus-target contract
// to scheduler queries and statistics, preserving filters, due time and limit.
func TestReminderAggregateActorScope(t *testing.T) {
	for _, operation := range []string{"due", "stats"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range []struct {
				name, actor, target, wantID string
				admin                       bool
				targets, wantIDs            []string
			}{
				{"member filters cannot widen scope", "caller", "target", "caller", false, []string{"other"}, nil},
				{"admin can combine target filters", "admin", "target", "", true, []string{"other", "third"}, []string{"target", "other", "third"}},
				{"admin can query all users", "admin", "", "", true, nil, nil},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					reminders := &mockReminderService{}
					actor := &userv2.UniversalUser{ID: tc.actor}
					if tc.admin {
						actor.Roles = []string{"ADMIN"}
					}
					service := &usermanager.Service{UserService: &mockReminderUserService{users: map[string]*userv2.UniversalUser{tc.actor: actor}}, ReminderService: reminders}
					var target string
					var targets []string
					switch operation {
					case "due":
						result, err := service.GetDueReminders(context.Background(), &usermanager.GetDueRemindersRequest{ActorID: tc.actor, FilterUserID: tc.target, FilterUserIDs: tc.targets, DueBefore: "2026-05-15T10:00:00Z", Limit: 20})
						require.NoError(t, err)
						require.NotNil(t, result)
						require.NotNil(t, reminders.getDueRequest)
						target, targets = reminders.getDueRequest.UserID, reminders.getDueRequest.UserIDs
						assert.Equal(t, "2026-05-15T10:00:00Z", reminders.getDueRequest.DueBefore)
						assert.EqualValues(t, 20, reminders.getDueRequest.Limit)
					case "stats":
						result, err := service.GetReminderStats(context.Background(), &usermanager.GetReminderStatsRequest{ActorID: tc.actor, FilterUserID: tc.target, FilterUserIDs: tc.targets})
						require.NoError(t, err)
						require.NotNil(t, result)
						require.NotNil(t, reminders.getStatsRequest)
						target, targets = reminders.getStatsRequest.UserID, reminders.getStatsRequest.UserIDs
					}
					assert.Equal(t, tc.wantID, target)
					assert.Equal(t, tc.wantIDs, targets)
				})
			}
		})
	}
}
