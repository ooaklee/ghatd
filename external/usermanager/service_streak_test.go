package usermanager_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/streaker"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
)

type mockStreakService struct {
	recordRequest  *streaker.RecordStreakRequest
	listRequest    *streaker.ListStreaksRequest
	currentRequest *streaker.GetCurrentCountRequest
	longestRequest *streaker.GetLongestStreakRequest
	countRequest   *streaker.GetNumberOfStreaksRequest
}

func (m *mockStreakService) RecordStreak(ctx context.Context, r *streaker.RecordStreakRequest) (*streaker.RecordStreakResponse, error) {
	m.recordRequest = r
	return &streaker.RecordStreakResponse{Streak: &streaker.Streak{Id: "streak-1", CurrentCount: 3}}, nil
}

func (m *mockStreakService) GetCurrentCount(ctx context.Context, r *streaker.GetCurrentCountRequest) (*streaker.GetCurrentCountResponse, error) {
	m.currentRequest = r
	return &streaker.GetCurrentCountResponse{CurrentCount: 3}, nil
}

func (m *mockStreakService) GetLongestStreak(ctx context.Context, r *streaker.GetLongestStreakRequest) (*streaker.GetLongestStreakResponse, error) {
	m.longestRequest = r
	return &streaker.GetLongestStreakResponse{LongestCount: 8}, nil
}

func (m *mockStreakService) GetNumberOfStreaks(ctx context.Context, r *streaker.GetNumberOfStreaksRequest) (*streaker.GetNumberOfStreaksResponse, error) {
	m.countRequest = r
	return &streaker.GetNumberOfStreaksResponse{Total: 12}, nil
}

func (m *mockStreakService) ListStreaks(ctx context.Context, r *streaker.ListStreaksRequest) (*streaker.ListStreaksResponse, error) {
	m.listRequest = r
	return &streaker.ListStreaksResponse{Streaks: []*streaker.Streak{{Id: "streak-1"}}}, nil
}

func TestServiceRecordStreakScopesToRequester(t *testing.T) {
	t.Parallel()

	streakSvc := &mockStreakService{}
	svc := &usermanager.Service{
		UserService: &mockReminderUserService{
			users: map[string]*userv2.UniversalUser{
				"user-1": {ID: "user-1"},
			},
		},
		StreakService: streakSvc,
	}

	res, err := svc.RecordStreak(context.Background(), &usermanager.RecordStreakRequest{
		UserID: "user-1",
		RecordStreakRequest: &streaker.RecordStreakRequest{
			StreakType:      "wms-app-check-in",
			OwnerId:         "user-2",
			TargetType:      "app",
			TargetId:        "astr",
			CreatedByUserId: "user-2",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, streakSvc.recordRequest)
	assert.Equal(t, "user-1", streakSvc.recordRequest.OwnerId)
	assert.Equal(t, "user-1", streakSvc.recordRequest.CreatedByUserId)
}

func TestServiceListStreaksLocksNonAdminToOwnUserID(t *testing.T) {
	t.Parallel()

	streakSvc := &mockStreakService{}
	svc := &usermanager.Service{
		UserService: &mockReminderUserService{
			users: map[string]*userv2.UniversalUser{
				"user-1": {ID: "user-1"},
			},
		},
		StreakService: streakSvc,
	}

	res, err := svc.ListStreaks(context.Background(), &usermanager.ListStreaksRequest{
		UserID:       "user-1",
		FilterUserID: "user-2",
		ListStreaksRequest: &streaker.ListStreaksRequest{
			StreakStatsRequest: streaker.StreakStatsRequest{PeriodType: streaker.StreakPeriodTypeDaily},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, streakSvc.listRequest)
	assert.Equal(t, "user-1", streakSvc.listRequest.OwnerId)
}

func TestServiceGetCurrentStreakAllowsAdminTargetUserScope(t *testing.T) {
	t.Parallel()

	streakSvc := &mockStreakService{}
	svc := &usermanager.Service{
		UserService: &mockReminderUserService{
			users: map[string]*userv2.UniversalUser{
				"admin-1": {ID: "admin-1", Roles: []string{"ADMIN"}},
			},
		},
		StreakService: streakSvc,
	}

	res, err := svc.GetCurrentStreak(context.Background(), &usermanager.GetCurrentStreakRequest{
		UserID:       "admin-1",
		FilterUserID: "user-2",
		GetCurrentCountRequest: &streaker.GetCurrentCountRequest{
			StreakStatsRequest: streaker.StreakStatsRequest{
				StreakType: "wms-app-check-in",
				PeriodType: streaker.StreakPeriodTypeDaily,
			},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, streakSvc.currentRequest)
	assert.Equal(t, "user-2", streakSvc.currentRequest.OwnerId)
	assert.Equal(t, 3, res.CurrentCount)
}

func TestServiceStreakReturnsServiceDisabledWhenMissing(t *testing.T) {
	t.Parallel()

	svc := &usermanager.Service{
		UserService: &mockReminderUserService{
			users: map[string]*userv2.UniversalUser{
				"user-1": {ID: "user-1"},
			},
		},
	}

	_, err := svc.GetNumberOfStreaks(context.Background(), &usermanager.GetNumberOfStreaksRequest{
		UserID:                    "user-1",
		GetNumberOfStreaksRequest: &streaker.GetNumberOfStreaksRequest{},
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, usermanager.ErrStreakServiceNotEnabled))
}
