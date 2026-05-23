package streaker_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/streaker"
)

type mockStreakRepository struct {
	createStreakFunc              func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error)
	createRawStreakFunc           func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error)
	getStreakByScopeAndPeriodFunc func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error)
	getLatestStreakFunc           func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error)
	getLongestStreakFunc          func(ctx context.Context, req *streaker.GetLongestStreakRequest) (*streaker.Streak, error)
	getTotalStreaksFunc           func(ctx context.Context, req *streaker.GetNumberOfStreaksRequest) (int64, error)
	listStreaksFunc               func(ctx context.Context, req *streaker.ListStreaksRequest) ([]*streaker.Streak, error)
}

func (m *mockStreakRepository) CreateStreak(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
	if m.createStreakFunc != nil {
		return m.createStreakFunc(ctx, st)
	}
	return st, nil
}

func (m *mockStreakRepository) CreateRawStreak(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
	if m.createRawStreakFunc != nil {
		return m.createRawStreakFunc(ctx, st)
	}
	return st, nil
}

func (m *mockStreakRepository) GetStreakByScopeAndPeriod(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
	if m.getStreakByScopeAndPeriodFunc != nil {
		return m.getStreakByScopeAndPeriodFunc(ctx, req)
	}
	return nil, streaker.ErrResourceNotFound
}

func (m *mockStreakRepository) GetLatestStreak(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
	if m.getLatestStreakFunc != nil {
		return m.getLatestStreakFunc(ctx, req)
	}
	return nil, streaker.ErrResourceNotFound
}

func (m *mockStreakRepository) GetLongestStreak(ctx context.Context, req *streaker.GetLongestStreakRequest) (*streaker.Streak, error) {
	if m.getLongestStreakFunc != nil {
		return m.getLongestStreakFunc(ctx, req)
	}
	return nil, streaker.ErrResourceNotFound
}

func (m *mockStreakRepository) GetTotalStreaks(ctx context.Context, req *streaker.GetNumberOfStreaksRequest) (int64, error) {
	if m.getTotalStreaksFunc != nil {
		return m.getTotalStreaksFunc(ctx, req)
	}
	return 0, nil
}

func (m *mockStreakRepository) ListStreaks(ctx context.Context, req *streaker.ListStreaksRequest) ([]*streaker.Streak, error) {
	if m.listStreaksFunc != nil {
		return m.listStreaksFunc(ctx, req)
	}
	return []*streaker.Streak{}, nil
}

func TestService_RecordStreakCreatesFirstEntry(t *testing.T) {
	t.Parallel()

	var created *streaker.Streak
	repo := &mockStreakRepository{
		createStreakFunc: func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
			created = st
			st.Id = "streak-id"
			st.NanoId = "nano-id"
			return st, nil
		},
	}
	svc := streaker.NewService(repo)

	res, err := svc.RecordStreak(context.Background(), &streaker.RecordStreakRequest{
		StreakName:      "App Streak",
		StreakType:      "App Streak",
		OwnerId:         "user-1",
		TargetType:      "App",
		TargetId:        "platform",
		OccurredAt:      "2026-05-08T09:00:00",
		CreatedByUserId: "user-1",
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, created)
	assert.Equal(t, "app-streak", created.StreakType)
	assert.Equal(t, "app", created.TargetType)
	assert.Equal(t, streaker.StreakPeriodTypeDaily, created.PeriodType)
	assert.Equal(t, "2026-05-08", created.PeriodKey)
	assert.Equal(t, "UTC", created.PeriodTimezone)
	assert.Equal(t, 1, created.CurrentCount)
	assert.Nil(t, created.Previous)
}

func TestService_RecordStreakUsesPeriodTimezone(t *testing.T) {
	t.Parallel()

	var created *streaker.Streak
	repo := &mockStreakRepository{
		getStreakByScopeAndPeriodFunc: func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
			assert.Equal(t, "2026-05-23", req.PeriodKey)
			return nil, streaker.ErrResourceNotFound
		},
		createStreakFunc: func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
			created = st
			return st, nil
		},
	}
	svc := streaker.NewService(repo)

	res, err := svc.RecordStreak(context.Background(), &streaker.RecordStreakRequest{
		StreakType:      "app streak",
		OwnerId:         "user-1",
		TargetType:      "app",
		TargetId:        "platform",
		OccurredAt:      "2026-05-22T23:31:00",
		PeriodTimezone:  "Europe/London",
		CreatedByUserId: "user-1",
	})

	require.NoError(t, err)
	require.NotNil(t, res.Streak)
	require.NotNil(t, created)
	assert.Equal(t, "2026-05-23", created.PeriodKey)
	assert.Equal(t, "Europe/London", created.PeriodTimezone)
}

func TestService_RecordStreakIncrementsConsecutiveEntry(t *testing.T) {
	t.Parallel()

	previous := &streaker.Streak{
		Id:           "previous-id",
		NanoId:       "previous-nano",
		StreakType:   "app-streak",
		OwnerId:      "user-1",
		TargetType:   "app",
		TargetId:     "platform",
		PeriodType:   streaker.StreakPeriodTypeDaily,
		PeriodKey:    "2026-05-07",
		OccurredAt:   "2026-05-07T09:00:00",
		CurrentCount: 2,
		CreatedAt:    "2026-05-07T09:00:00",
	}

	repo := &mockStreakRepository{
		getLatestStreakFunc: func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
			return previous, nil
		},
		createStreakFunc: func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
			return st, nil
		},
	}
	svc := streaker.NewService(repo)

	res, err := svc.RecordStreak(context.Background(), &streaker.RecordStreakRequest{
		StreakType:      "app streak",
		OwnerId:         "user-1",
		TargetType:      "app",
		TargetId:        "platform",
		OccurredAt:      "2026-05-08T09:00:00",
		CreatedByUserId: "user-1",
	})

	require.NoError(t, err)
	require.NotNil(t, res.Streak)
	assert.Equal(t, 3, res.Streak.CurrentCount)
	require.NotNil(t, res.Streak.Previous)
	assert.Equal(t, "previous-id", res.Streak.Previous.Id)
	assert.Equal(t, 2, res.Streak.Previous.CurrentCount)
}

func TestService_RecordStreakResetsAfterGap(t *testing.T) {
	t.Parallel()

	previous := &streaker.Streak{
		Id:           "previous-id",
		NanoId:       "previous-nano",
		StreakType:   "app-streak",
		OwnerId:      "user-1",
		TargetType:   "app",
		TargetId:     "platform",
		PeriodType:   streaker.StreakPeriodTypeDaily,
		PeriodKey:    "2026-05-06",
		OccurredAt:   "2026-05-06T09:00:00",
		CurrentCount: 8,
		CreatedAt:    "2026-05-06T09:00:00",
	}

	repo := &mockStreakRepository{
		getLatestStreakFunc: func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
			return previous, nil
		},
		createStreakFunc: func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
			return st, nil
		},
	}
	svc := streaker.NewService(repo)

	res, err := svc.RecordStreak(context.Background(), &streaker.RecordStreakRequest{
		StreakType:      "app streak",
		OwnerId:         "user-1",
		TargetType:      "app",
		TargetId:        "platform",
		OccurredAt:      "2026-05-08T09:00:00",
		CreatedByUserId: "user-1",
	})

	require.NoError(t, err)
	assert.Equal(t, 1, res.Streak.CurrentCount)
	require.NotNil(t, res.Streak.Previous)
	assert.Equal(t, "previous-id", res.Streak.Previous.Id)
}

func TestService_RecordStreakReturnsExistingPeriodEntry(t *testing.T) {
	t.Parallel()

	existing := &streaker.Streak{
		Id:           "existing-id",
		StreakType:   "app-streak",
		OwnerId:      "user-1",
		TargetType:   "app",
		TargetId:     "platform",
		PeriodType:   streaker.StreakPeriodTypeDaily,
		PeriodKey:    "2026-05-08",
		CurrentCount: 4,
	}
	createCalled := false

	repo := &mockStreakRepository{
		getStreakByScopeAndPeriodFunc: func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
			return existing, nil
		},
		createStreakFunc: func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
			createCalled = true
			return st, nil
		},
	}
	svc := streaker.NewService(repo)

	res, err := svc.RecordStreak(context.Background(), &streaker.RecordStreakRequest{
		StreakType:      "app streak",
		OwnerId:         "user-1",
		TargetType:      "app",
		TargetId:        "platform",
		OccurredAt:      "2026-05-08T09:00:00",
		CreatedByUserId: "user-1",
	})

	require.NoError(t, err)
	assert.False(t, createCalled)
	assert.Equal(t, existing, res.Streak)
}

func TestService_GetStreakByScopeAndPeriodOrCreateReturnsExistingEntry(t *testing.T) {
	t.Parallel()

	existing := &streaker.Streak{
		Id:           "existing-id",
		StreakName:   "App Streak",
		StreakType:   "app-streak",
		OwnerId:      "user-1",
		TargetType:   "app",
		TargetId:     "platform",
		PeriodType:   streaker.StreakPeriodTypeDaily,
		PeriodKey:    "2026-05-08",
		CurrentCount: 4,
	}
	createCalled := false

	repo := &mockStreakRepository{
		getStreakByScopeAndPeriodFunc: func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
			assert.Equal(t, "app-streak", req.StreakType)
			assert.Equal(t, "user-1", req.OwnerId)
			assert.Equal(t, "app", req.TargetType)
			assert.Equal(t, "platform", req.TargetId)
			assert.Equal(t, streaker.StreakPeriodTypeDaily, req.PeriodType)
			assert.Equal(t, "2026-05-08", req.PeriodKey)
			return existing, nil
		},
		createStreakFunc: func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
			createCalled = true
			return st, nil
		},
	}
	svc := streaker.NewService(repo)

	res, err := svc.GetStreakByScopeAndPeriodOrCreate(context.Background(), &streaker.RecordStreakRequest{
		StreakName:      "App Streak",
		StreakType:      "App Streak",
		OwnerId:         "user-1",
		TargetType:      "App",
		TargetId:        "platform",
		OccurredAt:      "2026-05-08T09:00:00",
		CreatedByUserId: "user-1",
	})

	require.NoError(t, err)
	assert.False(t, createCalled)
	assert.Equal(t, existing, res.Streak)
}

func TestService_GetStreakByScopeAndPeriodOrCreateCreatesMissingEntry(t *testing.T) {
	t.Parallel()

	var created *streaker.Streak
	repo := &mockStreakRepository{
		createStreakFunc: func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
			created = st
			st.Id = "created-id"
			st.NanoId = "created-nano"
			return st, nil
		},
	}
	svc := streaker.NewService(repo)

	res, err := svc.GetStreakByScopeAndPeriodOrCreate(context.Background(), &streaker.RecordStreakRequest{
		StreakName:      "App Streak",
		StreakType:      "App Streak",
		OwnerId:         "user-1",
		TargetType:      "App",
		TargetId:        "platform",
		OccurredAt:      "2026-05-08T09:00:00",
		CreatedByUserId: "user-1",
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, created)
	assert.Equal(t, "created-id", res.Streak.Id)
	assert.Equal(t, "App Streak", created.StreakName)
	assert.Equal(t, "app-streak", created.StreakType)
	assert.Equal(t, "app", created.TargetType)
	assert.Equal(t, streaker.StreakPeriodTypeDaily, created.PeriodType)
	assert.Equal(t, "2026-05-08", created.PeriodKey)
	assert.Equal(t, 1, created.CurrentCount)
}

func TestService_RecordStreakPeriodExamples(t *testing.T) {
	t.Parallel()

	t.Run("same daily period returns the existing streak entry", func(t *testing.T) {
		t.Parallel()

		existing := &streaker.Streak{
			Id:           "existing-period-entry",
			StreakType:   "app-streak",
			OwnerId:      "user-1",
			TargetType:   "app",
			TargetId:     "platform",
			PeriodType:   streaker.StreakPeriodTypeDaily,
			PeriodKey:    "2026-05-08",
			CurrentCount: 2,
		}
		createCalled := false

		repo := &mockStreakRepository{
			getStreakByScopeAndPeriodFunc: func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
				assert.Equal(t, "2026-05-08", req.PeriodKey)
				return existing, nil
			},
			createStreakFunc: func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
				createCalled = true
				return st, nil
			},
		}
		svc := streaker.NewService(repo)

		res, err := svc.RecordStreak(context.Background(), &streaker.RecordStreakRequest{
			StreakType:      "app-streak",
			OwnerId:         "user-1",
			TargetType:      "app",
			TargetId:        "platform",
			OccurredAt:      "2026-05-08T18:00:00",
			CreatedByUserId: "user-1",
		})

		require.NoError(t, err)
		assert.False(t, createCalled)
		assert.Equal(t, existing.Id, res.Streak.Id)
		assert.Equal(t, 2, res.Streak.CurrentCount)
	})

	t.Run("next daily period increments from previous current count", func(t *testing.T) {
		t.Parallel()

		previous := &streaker.Streak{
			Id:           "entry-from-2026-05-07",
			NanoId:       "nano-from-2026-05-07",
			StreakType:   "app-streak",
			OwnerId:      "user-1",
			TargetType:   "app",
			TargetId:     "platform",
			PeriodType:   streaker.StreakPeriodTypeDaily,
			PeriodKey:    "2026-05-07",
			OccurredAt:   "2026-05-07T18:00:00",
			CurrentCount: 2,
			CreatedAt:    "2026-05-07T18:00:00",
		}

		repo := &mockStreakRepository{
			getLatestStreakFunc: func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
				return previous, nil
			},
			createStreakFunc: func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
				return st, nil
			},
		}
		svc := streaker.NewService(repo)

		res, err := svc.RecordStreak(context.Background(), &streaker.RecordStreakRequest{
			StreakType:      "app-streak",
			OwnerId:         "user-1",
			TargetType:      "app",
			TargetId:        "platform",
			OccurredAt:      "2026-05-08T18:00:00",
			CreatedByUserId: "user-1",
		})

		require.NoError(t, err)
		assert.Equal(t, "2026-05-08", res.Streak.PeriodKey)
		assert.Equal(t, 3, res.Streak.CurrentCount)
		require.NotNil(t, res.Streak.Previous)
		assert.Equal(t, previous.Id, res.Streak.Previous.Id)
	})

	t.Run("gap in daily period resets count while retaining previous entry metadata", func(t *testing.T) {
		t.Parallel()

		previous := &streaker.Streak{
			Id:           "entry-from-2026-05-06",
			NanoId:       "nano-from-2026-05-06",
			StreakType:   "app-streak",
			OwnerId:      "user-1",
			TargetType:   "app",
			TargetId:     "platform",
			PeriodType:   streaker.StreakPeriodTypeDaily,
			PeriodKey:    "2026-05-06",
			OccurredAt:   "2026-05-06T18:00:00",
			CurrentCount: 8,
			CreatedAt:    "2026-05-06T18:00:00",
		}

		repo := &mockStreakRepository{
			getLatestStreakFunc: func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
				return previous, nil
			},
			createStreakFunc: func(ctx context.Context, st *streaker.Streak) (*streaker.Streak, error) {
				return st, nil
			},
		}
		svc := streaker.NewService(repo)

		res, err := svc.RecordStreak(context.Background(), &streaker.RecordStreakRequest{
			StreakType:      "app-streak",
			OwnerId:         "user-1",
			TargetType:      "app",
			TargetId:        "platform",
			OccurredAt:      "2026-05-08T18:00:00",
			CreatedByUserId: "user-1",
		})

		require.NoError(t, err)
		assert.Equal(t, "2026-05-08", res.Streak.PeriodKey)
		assert.Equal(t, 1, res.Streak.CurrentCount)
		require.NotNil(t, res.Streak.Previous)
		assert.Equal(t, previous.Id, res.Streak.Previous.Id)
		assert.Equal(t, 8, res.Streak.Previous.CurrentCount)
	})
}

func TestIsConsecutivePeriod(t *testing.T) {
	t.Parallel()

	assert.False(t, streaker.IsConsecutivePeriod("2026-05-08", "2026-05-08", streaker.StreakPeriodTypeDaily))
	assert.True(t, streaker.IsConsecutivePeriod("2026-05-07", "2026-05-08", streaker.StreakPeriodTypeDaily))
	assert.False(t, streaker.IsConsecutivePeriod("2026-05-06", "2026-05-08", streaker.StreakPeriodTypeDaily))
}

func TestService_GetStats(t *testing.T) {
	t.Parallel()

	longest := &streaker.Streak{CurrentCount: 12}
	latest := &streaker.Streak{CurrentCount: 3}
	repo := &mockStreakRepository{
		getLongestStreakFunc: func(ctx context.Context, req *streaker.GetLongestStreakRequest) (*streaker.Streak, error) {
			assert.Equal(t, "app-streak", req.StreakType)
			assert.Equal(t, "user-1", req.OwnerId)
			assert.Equal(t, streaker.StreakPeriodTypeDaily, req.PeriodType)
			return longest, nil
		},
		getLatestStreakFunc: func(ctx context.Context, req *streaker.GetLatestStreakRequest) (*streaker.Streak, error) {
			return latest, nil
		},
		getTotalStreaksFunc: func(ctx context.Context, req *streaker.GetNumberOfStreaksRequest) (int64, error) {
			return 44, nil
		},
	}
	svc := streaker.NewService(repo)

	longestRes, err := svc.GetLongestStreakByStreakTypeAndUserID(context.Background(), &streaker.GetLongestStreakRequest{
		StreakStatsRequest: streaker.StreakStatsRequest{StreakType: "App Streak", OwnerId: "user-1", PeriodType: streaker.StreakPeriodTypeDaily},
	})
	require.NoError(t, err)
	assert.Equal(t, 12, longestRes.LongestCount)

	currentRes, err := svc.GetCurrentCountByStreakTypeAndUserID(context.Background(), &streaker.GetCurrentCountRequest{
		StreakStatsRequest: streaker.StreakStatsRequest{StreakType: "App Streak", OwnerId: "user-1", PeriodType: streaker.StreakPeriodTypeDaily},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, currentRes.CurrentCount)

	totalRes, err := svc.GetNumberOfStreaksByStreakTypeAndUserID(context.Background(), &streaker.GetNumberOfStreaksRequest{
		StreakStatsRequest: streaker.StreakStatsRequest{StreakType: "App Streak", OwnerId: "user-1", PeriodType: streaker.StreakPeriodTypeDaily},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(44), totalRes.Total)
}

func TestService_GetStatsRequiresPeriodType(t *testing.T) {
	t.Parallel()

	svc := streaker.NewService(&mockStreakRepository{})

	_, err := svc.GetCurrentCountByStreakTypeAndUserID(context.Background(), &streaker.GetCurrentCountRequest{
		StreakStatsRequest: streaker.StreakStatsRequest{StreakType: "App Streak", OwnerId: "user-1"},
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, streaker.ErrPeriodTypeIsRequired)
}

func TestService_ListStreaksNormalisesFilters(t *testing.T) {
	t.Parallel()

	expected := []*streaker.Streak{
		{Id: "streak-1", PeriodKey: "2026-05-20"},
		{Id: "streak-2", PeriodKey: "2026-05-21"},
	}
	repo := &mockStreakRepository{
		listStreaksFunc: func(ctx context.Context, req *streaker.ListStreaksRequest) ([]*streaker.Streak, error) {
			assert.Equal(t, "saved-words", req.StreakType)
			assert.Equal(t, "user-1", req.OwnerId)
			assert.Equal(t, "affirmation", req.TargetType)
			assert.Equal(t, "saved-words", req.TargetId)
			assert.Equal(t, streaker.StreakPeriodTypeDaily, req.PeriodType)
			assert.Equal(t, "2026-05-20", req.PeriodKeyFrom)
			assert.Equal(t, "2026-05-22", req.PeriodKeyTo)
			assert.Equal(t, 1, req.Page)
			assert.Equal(t, 100, req.PerPage)
			return expected, nil
		},
	}
	svc := streaker.NewService(repo)

	res, err := svc.ListStreaks(context.Background(), &streaker.ListStreaksRequest{
		StreakStatsRequest: streaker.StreakStatsRequest{
			StreakType: "Saved Words",
			OwnerId:    "user-1",
			TargetType: "Affirmation",
			TargetId:   "saved-words",
			PeriodType: streaker.StreakPeriodTypeDaily,
		},
		PeriodKeyFrom: "2026-05-20",
		PeriodKeyTo:   "2026-05-22",
	})

	require.NoError(t, err)
	assert.Equal(t, expected, res.Streaks)
}
