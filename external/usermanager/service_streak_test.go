package usermanager_test

import (
	"context"
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

// TestStreakRecordActorScope proves that attribution and ownership are always
// derived from the trusted actor, even when the caller is an administrator.
func TestStreakRecordActorScope(t *testing.T) {
	for _, tc := range []struct {
		name, actor string
		admin       bool
	}{
		{"member", "caller", false},
		{"administrator", "admin", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			streaks := &mockStreakService{}
			actor := &userv2.UniversalUser{ID: tc.actor}
			if tc.admin {
				actor.Roles = []string{"ADMIN"}
			}
			service := &usermanager.Service{UserService: &mockReminderUserService{users: map[string]*userv2.UniversalUser{tc.actor: actor}}, StreakService: streaks}
			payload := &streaker.RecordStreakRequest{StreakType: "daily-check-in", OwnerId: "target", CreatedByUserId: "target", TargetType: "app", TargetId: "sample"}
			result, err := service.RecordStreak(context.Background(), &usermanager.RecordStreakRequest{ActorID: tc.actor, RecordStreakRequest: payload})
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, streaks.recordRequest)
			assert.Equal(t, tc.actor, streaks.recordRequest.OwnerId)
			assert.Equal(t, tc.actor, streaks.recordRequest.CreatedByUserId)
			assert.Equal(t, "target", payload.OwnerId, "must not mutate caller-owned payload")
		})
	}
}

// TestStreakQueryActorScope checks the same scope rules through all four read
// adapters, including missing-service failures before any lower-domain call.
func TestStreakQueryActorScope(t *testing.T) {
	for _, operation := range []string{"list", "current", "longest", "count"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range []struct {
				name, actor, target, want string
				admin, disabled           bool
			}{
				{"member cannot select another user", "caller", "target", "caller", false, false},
				{"admin can select another user", "admin", "target", "target", true, false},
				{"missing service", "caller", "target", "", false, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					streaks := &mockStreakService{}
					actor := &userv2.UniversalUser{ID: tc.actor}
					if tc.admin {
						actor.Roles = []string{"ADMIN"}
					}
					service := &usermanager.Service{UserService: &mockReminderUserService{users: map[string]*userv2.UniversalUser{tc.actor: actor}}}
					if !tc.disabled {
						service.StreakService = streaks
					}
					stats := streaker.StreakStatsRequest{StreakType: "daily-check-in", PeriodType: streaker.StreakPeriodTypeDaily}
					var err error
					var owner string
					switch operation {
					case "list":
						var result *usermanager.ListStreaksResponse
						result, err = service.ListStreaks(context.Background(), &usermanager.ListStreaksRequest{ActorID: tc.actor, FilterUserID: tc.target, ListStreaksRequest: &streaker.ListStreaksRequest{StreakStatsRequest: stats}})
						if err == nil {
							require.NotNil(t, result)
							require.NotNil(t, streaks.listRequest)
							owner = streaks.listRequest.OwnerId
						}
					case "current":
						var result *usermanager.GetCurrentStreakResponse
						result, err = service.GetCurrentStreak(context.Background(), &usermanager.GetCurrentStreakRequest{ActorID: tc.actor, FilterUserID: tc.target, GetCurrentCountRequest: &streaker.GetCurrentCountRequest{StreakStatsRequest: stats}})
						if err == nil {
							require.NotNil(t, result)
							require.NotNil(t, streaks.currentRequest)
							owner = streaks.currentRequest.OwnerId
							assert.Equal(t, 3, result.CurrentCount)
						}
					case "longest":
						var result *usermanager.GetLongestStreakResponse
						result, err = service.GetLongestStreak(context.Background(), &usermanager.GetLongestStreakRequest{ActorID: tc.actor, FilterUserID: tc.target, GetLongestStreakRequest: &streaker.GetLongestStreakRequest{StreakStatsRequest: stats}})
						if err == nil {
							require.NotNil(t, result)
							require.NotNil(t, streaks.longestRequest)
							owner = streaks.longestRequest.OwnerId
							assert.Equal(t, 8, result.LongestCount)
						}
					case "count":
						var result *usermanager.GetNumberOfStreaksResponse
						result, err = service.GetNumberOfStreaks(context.Background(), &usermanager.GetNumberOfStreaksRequest{ActorID: tc.actor, FilterUserID: tc.target, GetNumberOfStreaksRequest: &streaker.GetNumberOfStreaksRequest{StreakStatsRequest: stats}})
						if err == nil {
							require.NotNil(t, result)
							require.NotNil(t, streaks.countRequest)
							owner = streaks.countRequest.OwnerId
							assert.Equal(t, int64(12), result.Total)
						}
					}
					if tc.disabled {
						require.ErrorIs(t, err, usermanager.ErrStreakServiceNotEnabled)
					} else {
						require.NoError(t, err)
						assert.Equal(t, tc.want, owner)
					}
				})
			}
		})
	}
}
