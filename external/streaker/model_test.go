package streaker_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/streaker"
)

func TestBuildPeriodKeyForTimezoneDailyUsesLocalDate(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 5, 22, 23, 31, 0, 0, time.UTC)

	londonKey, err := streaker.BuildPeriodKeyForTimezone(occurredAt, streaker.StreakPeriodTypeDaily, "", "Europe/London")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-23", londonKey)

	newYorkKey, err := streaker.BuildPeriodKeyForTimezone(occurredAt, streaker.StreakPeriodTypeDaily, "", "America/New_York")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-22", newYorkKey)
}

func TestBuildPeriodKeyForTimezoneValidatesTimezone(t *testing.T) {
	t.Parallel()

	_, err := streaker.BuildPeriodKeyForTimezone(time.Now(), streaker.StreakPeriodTypeDaily, "", "not-a-zone")
	require.Error(t, err)
	assert.ErrorIs(t, err, streaker.ErrInvalidPeriodTimezone)
}

func TestBuildPeriodKeyKeepsUTCSemantics(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 5, 22, 23, 31, 0, 0, time.UTC)

	key, err := streaker.BuildPeriodKey(occurredAt, streaker.StreakPeriodTypeDaily, "")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-22", key)
}
