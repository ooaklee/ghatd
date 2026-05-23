package reminder_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/reminder"
)

func TestBuildNextDueAtUsesLocalTimezone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 22, 7, 0, 0, 0, time.UTC)

	nextDueAt, err := reminder.BuildNextDueAt("09:00", "Europe/London", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-22T08:00:00", nextDueAt)
}

func TestBuildNextDueAtMovesPastWallClockToNextDay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 22, 9, 30, 0, 0, time.UTC)

	nextDueAt, err := reminder.BuildNextDueAt("09:00", "Europe/London", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-05-23T08:00:00", nextDueAt)
}

func TestBuildNextDueAtHandlesDSTStart(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 28, 23, 30, 0, 0, time.UTC)

	nextDueAt, err := reminder.BuildNextDueAt("09:00", "Europe/London", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-03-29T08:00:00", nextDueAt)
}

func TestBuildNextDueAtSupportsLegacyAbsoluteTargetTime(t *testing.T) {
	t.Parallel()

	nextDueAt, err := reminder.BuildNextDueAt("2026-05-15T10:00:00.000000000", "", time.Now())
	require.NoError(t, err)
	assert.Equal(t, "2026-05-15T10:00:00", nextDueAt)
}

func TestBuildNextDueAtValidatesTimezone(t *testing.T) {
	t.Parallel()

	_, err := reminder.BuildNextDueAt("09:00", "not-a-zone", time.Now())
	require.Error(t, err)
	assert.ErrorIs(t, err, reminder.ErrInvalidTimezone)
}
