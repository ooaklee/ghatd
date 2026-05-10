package reminder_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/reminder"
)

type mockReminderRepository struct {
	createReminderFunc                          func(ctx context.Context, r *reminder.Reminder) (*reminder.Reminder, error)
	getReminderByIDFunc                         func(ctx context.Context, id string) (*reminder.Reminder, error)
	listRemindersFunc                           func(ctx context.Context, userID string, status string, targetType string, targetId string, page, perPage int) ([]*reminder.Reminder, error)
	getRemindersForTargetTypeByUserIDFunc       func(ctx context.Context, userID string, targetType string, targetId string, page, perPage int) ([]*reminder.Reminder, error)
	getActiveRemindersForTargetTypeByUserIDFunc func(ctx context.Context, userID string, targetType string, targetId string, page, perPage int) ([]*reminder.Reminder, error)
	updateReminderFunc                          func(ctx context.Context, r *reminder.Reminder) (*reminder.Reminder, error)
	patchReminderFunc                           func(ctx context.Context, id string, update map[string]interface{}) error
	deleteReminderFunc                          func(ctx context.Context, id string) error
	countRemindersFunc                          func(ctx context.Context, filter *reminder.ReminderFilter) (int64, error)
	getDueRemindersFunc                         func(ctx context.Context, filter *reminder.ReminderFilter, limit int64) ([]*reminder.Reminder, error)
	createReminderExecutionFunc                 func(ctx context.Context, r *reminder.ReminderExecution) (*reminder.ReminderExecution, error)
	listReminderExecutionsFunc                  func(ctx context.Context, filter *reminder.ReminderExecutionFilter, page, perPage int) ([]*reminder.ReminderExecution, error)
}

func (m *mockReminderRepository) CreateReminder(ctx context.Context, r *reminder.Reminder) (*reminder.Reminder, error) {
	if m.createReminderFunc != nil {
		return m.createReminderFunc(ctx, r)
	}
	r.Id = "generated-id"
	r.NanoId = "generated-nano"
	return r, nil
}

func (m *mockReminderRepository) GetReminderByID(ctx context.Context, id string) (*reminder.Reminder, error) {
	if m.getReminderByIDFunc != nil {
		return m.getReminderByIDFunc(ctx, id)
	}
	return nil, reminder.ErrResourceNotFound
}

func (m *mockReminderRepository) ListReminders(ctx context.Context, userID string, status string, targetType string, targetId string, page, perPage int) ([]*reminder.Reminder, error) {
	if m.listRemindersFunc != nil {
		return m.listRemindersFunc(ctx, userID, status, targetType, targetId, page, perPage)
	}
	return []*reminder.Reminder{}, nil
}

func (m *mockReminderRepository) GetRemindersForTargetTypeByUserID(ctx context.Context, userID string, targetType string, targetId string, page, perPage int) ([]*reminder.Reminder, error) {
	if m.getRemindersForTargetTypeByUserIDFunc != nil {
		return m.getRemindersForTargetTypeByUserIDFunc(ctx, userID, targetType, targetId, page, perPage)
	}
	return []*reminder.Reminder{}, nil
}

func (m *mockReminderRepository) GetActiveRemindersForTargetTypeByUserID(ctx context.Context, userID string, targetType string, targetId string, page, perPage int) ([]*reminder.Reminder, error) {
	if m.getActiveRemindersForTargetTypeByUserIDFunc != nil {
		return m.getActiveRemindersForTargetTypeByUserIDFunc(ctx, userID, targetType, targetId, page, perPage)
	}
	return []*reminder.Reminder{}, nil
}

func (m *mockReminderRepository) UpdateReminderByID(ctx context.Context, r *reminder.Reminder) (*reminder.Reminder, error) {
	if m.updateReminderFunc != nil {
		return m.updateReminderFunc(ctx, r)
	}
	return r, nil
}

func (m *mockReminderRepository) PatchReminder(ctx context.Context, id string, update map[string]interface{}) error {
	if m.patchReminderFunc != nil {
		return m.patchReminderFunc(ctx, id, update)
	}
	return nil
}

func (m *mockReminderRepository) DeleteReminderByID(ctx context.Context, id string) error {
	if m.deleteReminderFunc != nil {
		return m.deleteReminderFunc(ctx, id)
	}
	return nil
}

func (m *mockReminderRepository) CountReminders(ctx context.Context, filter *reminder.ReminderFilter) (int64, error) {
	if m.countRemindersFunc != nil {
		return m.countRemindersFunc(ctx, filter)
	}
	return 0, nil
}

func (m *mockReminderRepository) GetDueReminders(ctx context.Context, filter *reminder.ReminderFilter, limit int64) ([]*reminder.Reminder, error) {
	if m.getDueRemindersFunc != nil {
		return m.getDueRemindersFunc(ctx, filter, limit)
	}
	return []*reminder.Reminder{}, nil
}

func (m *mockReminderRepository) CreateReminderExecution(ctx context.Context, r *reminder.ReminderExecution) (*reminder.ReminderExecution, error) {
	if m.createReminderExecutionFunc != nil {
		return m.createReminderExecutionFunc(ctx, r)
	}
	r.Id = "execution-id"
	r.NanoId = "execution-nano"
	return r, nil
}

func (m *mockReminderRepository) ListReminderExecutions(ctx context.Context, filter *reminder.ReminderExecutionFilter, page, perPage int) ([]*reminder.ReminderExecution, error) {
	if m.listReminderExecutionsFunc != nil {
		return m.listReminderExecutionsFunc(ctx, filter, page, perPage)
	}
	return []*reminder.ReminderExecution{}, nil
}

func TestService_CreateReminderSuccess(t *testing.T) {
	t.Parallel()

	svc := reminder.NewService(&mockReminderRepository{})

	res, err := svc.CreateReminder(context.Background(), &reminder.CreateReminderRequest{
		UserID:     "user-1",
		TargetType: "lesson",
		TargetId:   "lesson-1",
		Title:      "Test Reminder",
		TargetTime: "2026-05-15T10:00:00.000000000",
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.Reminder)
	assert.Equal(t, "user-1", res.Reminder.UserID)
	assert.Equal(t, "lesson", res.Reminder.TargetType)
	assert.Equal(t, "lesson-1", res.Reminder.TargetId)
	assert.Equal(t, "Test Reminder", res.Reminder.Title)
	assert.Equal(t, "2026-05-15T10:00:00.000000000", res.Reminder.TargetTime)
	assert.Equal(t, reminder.ReminderStatusActive, res.Reminder.Status)
	assert.NotEmpty(t, res.Reminder.Id)
	assert.NotEmpty(t, res.Reminder.NanoId)
}

func TestService_CreateReminderValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	svc := reminder.NewService(&mockReminderRepository{})

	t.Run("nil request", func(t *testing.T) {
		t.Parallel()
		_, err := svc.CreateReminder(context.Background(), nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrUserIDIsRequired)
	})

	t.Run("missing user id", func(t *testing.T) {
		t.Parallel()
		_, err := svc.CreateReminder(context.Background(), &reminder.CreateReminderRequest{
			Title:      "Test",
			TargetTime: "2026-05-15T10:00:00.000000000",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrUserIDIsRequired)
	})

	t.Run("missing title", func(t *testing.T) {
		t.Parallel()
		_, err := svc.CreateReminder(context.Background(), &reminder.CreateReminderRequest{
			UserID:     "user-1",
			TargetTime: "2026-05-15T10:00:00.000000000",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrTitleIsRequired)
	})

	t.Run("missing target time", func(t *testing.T) {
		t.Parallel()
		_, err := svc.CreateReminder(context.Background(), &reminder.CreateReminderRequest{
			UserID: "user-1",
			Title:  "Test",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrTargetTimeIsRequired)
	})
}

func TestService_GetReminderByID(t *testing.T) {
	t.Parallel()

	svc := reminder.NewService(&mockReminderRepository{})

	t.Run("missing id", func(t *testing.T) {
		t.Parallel()
		_, err := svc.GetReminderByID(context.Background(), &reminder.GetReminderByIDRequest{})
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrIdIsRequired)
	})

	t.Run("nil request", func(t *testing.T) {
		t.Parallel()
		_, err := svc.GetReminderByID(context.Background(), nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrIdIsRequired)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		_, err := svc.GetReminderByID(context.Background(), &reminder.GetReminderByIDRequest{Id: "nonexistent"})
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrResourceNotFound)
	})
}

func TestService_ListReminders(t *testing.T) {
	t.Parallel()

	svc := reminder.NewService(&mockReminderRepository{})

	t.Run("nil request returns empty list", func(t *testing.T) {
		t.Parallel()
		res, err := svc.ListReminders(context.Background(), nil)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Empty(t, res.Reminders)
	})

	t.Run("invalid status", func(t *testing.T) {
		t.Parallel()
		_, err := svc.ListReminders(context.Background(), &reminder.ListRemindersRequest{Status: "missing"})
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrInvalidStatus)
	})

	t.Run("valid request returns empty list", func(t *testing.T) {
		t.Parallel()
		res, err := svc.ListReminders(context.Background(), &reminder.ListRemindersRequest{
			UserID: "user-1",
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Empty(t, res.Reminders)
	})
}

func TestService_DeleteReminderByID(t *testing.T) {
	t.Parallel()

	t.Run("missing id", func(t *testing.T) {
		t.Parallel()
		svc := reminder.NewService(&mockReminderRepository{})
		err := svc.DeleteReminderByID(context.Background(), &reminder.DeleteReminderByIDRequest{UserID: "user-1"})
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrIdIsRequired)
	})

	t.Run("missing user id", func(t *testing.T) {
		t.Parallel()
		svc := reminder.NewService(&mockReminderRepository{})
		err := svc.DeleteReminderByID(context.Background(), &reminder.DeleteReminderByIDRequest{Id: "reminder-1"})
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrUserIDIsRequired)
	})
}

func TestService_DisableReminderByID(t *testing.T) {
	t.Parallel()

	t.Run("missing id", func(t *testing.T) {
		t.Parallel()
		svc := reminder.NewService(&mockReminderRepository{})
		_, err := svc.DisableReminderByID(context.Background(), &reminder.DisableReminderByIDRequest{UserID: "user-1"})
		require.Error(t, err)
		assert.ErrorIs(t, err, reminder.ErrIdIsRequired)
	})
}

func TestService_GetReminderStats(t *testing.T) {
	t.Parallel()

	repo := &mockReminderRepository{
		countRemindersFunc: func(ctx context.Context, filter *reminder.ReminderFilter) (int64, error) {
			if filter == nil || filter.Status == "" {
				return 10, nil
			}
			switch filter.Status {
			case "active":
				return 5, nil
			case "completed":
				return 3, nil
			case "disabled":
				return 2, nil
			}
			return 0, nil
		},
		getDueRemindersFunc: func(ctx context.Context, filter *reminder.ReminderFilter, limit int64) ([]*reminder.Reminder, error) {
			assert.Equal(t, string(reminder.ReminderStatusActive), filter.Status)
			return []*reminder.Reminder{{Id: "due-1"}}, nil
		},
	}

	svc := reminder.NewService(repo)
	res, err := svc.GetReminderStats(context.Background(), nil)

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.Stats)
	assert.EqualValues(t, 10, res.Stats.TotalReminders)
	assert.EqualValues(t, 5, res.Stats.ActiveReminders)
	assert.EqualValues(t, 3, res.Stats.CompletedCount)
	assert.EqualValues(t, 2, res.Stats.DisabledCount)
	assert.EqualValues(t, 1, res.Stats.DueReminders)
}

func TestService_GetDueReminders(t *testing.T) {
	t.Parallel()

	svc := reminder.NewService(&mockReminderRepository{})

	res, err := svc.GetDueReminders(context.Background(), &reminder.GetDueRemindersRequest{})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Empty(t, res.Reminders)
}

func TestService_GetDueRemindersFiltersUserIDs(t *testing.T) {
	t.Parallel()

	repo := &mockReminderRepository{
		getDueRemindersFunc: func(ctx context.Context, filter *reminder.ReminderFilter, limit int64) ([]*reminder.Reminder, error) {
			assert.Equal(t, []string{"user-1", "user-2", "user-3"}, filter.UserIDs)
			assert.Equal(t, string(reminder.ReminderStatusActive), filter.Status)
			assert.EqualValues(t, 50, limit)
			return []*reminder.Reminder{}, nil
		},
	}
	svc := reminder.NewService(repo)

	res, err := svc.GetDueReminders(context.Background(), &reminder.GetDueRemindersRequest{
		UserID:  "user-1",
		UserIDs: []string{"user-2,user-3", "user-1"},
		Limit:   50,
	})

	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestService_GetRemindersForTargetTypeByUserID(t *testing.T) {
	t.Parallel()

	repo := &mockReminderRepository{
		getRemindersForTargetTypeByUserIDFunc: func(ctx context.Context, userID string, targetType string, targetId string, page, perPage int) ([]*reminder.Reminder, error) {
			assert.Equal(t, "user-1", userID)
			assert.Equal(t, "lesson", targetType)
			assert.Equal(t, "lesson-1", targetId)
			return []*reminder.Reminder{{Id: "reminder-1", UserID: userID, TargetType: targetType, TargetId: targetId}}, nil
		},
	}

	svc := reminder.NewService(repo)
	res, err := svc.GetRemindersForTargetTypeByUserID(context.Background(), &reminder.GetRemindersForTargetTypeByUserIDRequest{
		UserID:     "user-1",
		TargetType: "lesson",
		TargetId:   "lesson-1",
	})

	require.NoError(t, err)
	require.Len(t, res.Reminders, 1)
	assert.Equal(t, "reminder-1", res.Reminders[0].Id)
}

func TestService_GetActiveRemindersForTargetTypeByUserID(t *testing.T) {
	t.Parallel()

	repo := &mockReminderRepository{
		getActiveRemindersForTargetTypeByUserIDFunc: func(ctx context.Context, userID string, targetType string, targetId string, page, perPage int) ([]*reminder.Reminder, error) {
			assert.Equal(t, "user-1", userID)
			assert.Equal(t, "task", targetType)
			return []*reminder.Reminder{{Id: "reminder-1", Status: reminder.ReminderStatusActive}}, nil
		},
	}

	svc := reminder.NewService(repo)
	res, err := svc.GetActiveRemindersForTargetTypeByUserID(context.Background(), &reminder.GetActiveRemindersForTargetTypeByUserIDRequest{
		UserID:     "user-1",
		TargetType: "task",
	})

	require.NoError(t, err)
	require.Len(t, res.Reminders, 1)
	assert.Equal(t, reminder.ReminderStatusActive, res.Reminders[0].Status)
}

func TestService_RecordReminderExecution(t *testing.T) {
	t.Parallel()

	repo := &mockReminderRepository{
		createReminderExecutionFunc: func(ctx context.Context, execution *reminder.ReminderExecution) (*reminder.ReminderExecution, error) {
			assert.Equal(t, "reminder-1", execution.ReminderId)
			assert.Equal(t, "user-1", execution.UserID)
			assert.Equal(t, reminder.ReminderExecutionStatusSent, execution.Status)
			assert.Equal(t, 2, execution.Attempt)
			execution.Id = "execution-1"
			return execution, nil
		},
	}

	svc := reminder.NewService(repo)
	res, err := svc.RecordReminderExecution(context.Background(), &reminder.RecordReminderExecutionRequest{
		ReminderId:   "reminder-1",
		UserID:       "user-1",
		TargetType:   "lesson",
		TargetId:     "lesson-1",
		ScheduledFor: "2026-05-15T10:00:00.000000000",
		Status:       reminder.ReminderExecutionStatusSent,
		Attempt:      2,
	})

	require.NoError(t, err)
	require.NotNil(t, res.Execution)
	assert.Equal(t, "execution-1", res.Execution.Id)
	assert.NotEmpty(t, res.Execution.ExecutedAt)
}

func TestService_ListReminderExecutions(t *testing.T) {
	t.Parallel()

	repo := &mockReminderRepository{
		listReminderExecutionsFunc: func(ctx context.Context, filter *reminder.ReminderExecutionFilter, page, perPage int) ([]*reminder.ReminderExecution, error) {
			assert.Equal(t, "reminder-1", filter.ReminderId)
			assert.Equal(t, reminder.ReminderExecutionStatusFailed, filter.Status)
			assert.Equal(t, 1, page)
			assert.Equal(t, 25, perPage)
			return []*reminder.ReminderExecution{{Id: "execution-1"}}, nil
		},
	}

	svc := reminder.NewService(repo)
	res, err := svc.ListReminderExecutions(context.Background(), &reminder.ListReminderExecutionsRequest{
		ReminderId: "reminder-1",
		Status:     reminder.ReminderExecutionStatusFailed,
	})

	require.NoError(t, err)
	require.Len(t, res.Executions, 1)
	assert.Equal(t, "execution-1", res.Executions[0].Id)
}
