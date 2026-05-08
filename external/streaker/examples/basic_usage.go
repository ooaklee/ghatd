package examples

import (
	"context"

	"github.com/ooaklee/ghatd/external/streaker"
)

func RecordAppStreak(ctx context.Context, service *streaker.Service, userID string) (*streaker.Streak, error) {
	res, err := service.RecordStreak(ctx, &streaker.RecordStreakRequest{
		StreakName:      "App Streak",
		StreakType:      "app-streak",
		OwnerId:         userID,
		TargetType:      "app",
		TargetId:        "platform",
		CreatedByUserId: userID,
	})
	if err != nil {
		return nil, err
	}

	return res.Streak, nil
}

func GetAppStreakStats(ctx context.Context, service *streaker.Service, userID string) (*streaker.GetCurrentCountResponse, *streaker.GetLongestStreakResponse, *streaker.GetNumberOfStreaksResponse, error) {
	current, err := service.GetCurrentCountByStreakTypeAndUserID(ctx, &streaker.GetCurrentCountRequest{
		StreakStatsRequest: streaker.StreakStatsRequest{
			StreakType: "app-streak",
			OwnerId:    userID,
			PeriodType: streaker.StreakPeriodTypeDaily,
		},
	})
	if err != nil {
		return nil, nil, nil, err
	}

	longest, err := service.GetLongestStreakByStreakTypeAndUserID(ctx, &streaker.GetLongestStreakRequest{
		StreakStatsRequest: streaker.StreakStatsRequest{
			StreakType: "app-streak",
			OwnerId:    userID,
			PeriodType: streaker.StreakPeriodTypeDaily,
		},
	})
	if err != nil {
		return nil, nil, nil, err
	}

	total, err := service.GetNumberOfStreaksByStreakTypeAndUserID(ctx, &streaker.GetNumberOfStreaksRequest{
		StreakStatsRequest: streaker.StreakStatsRequest{
			StreakType: "app-streak",
			OwnerId:    userID,
			PeriodType: streaker.StreakPeriodTypeDaily,
		},
	})
	if err != nil {
		return nil, nil, nil, err
	}

	return current, longest, total, nil
}
