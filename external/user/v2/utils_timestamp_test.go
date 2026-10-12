package user

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseTimestampUTC(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        time.Time
		invalid     bool
	}{
		{"UTC nanoseconds", "2026-10-01T11:00:00.123456789Z", time.Date(2026, 10, 1, 11, 0, 0, 123456789, time.UTC), false},
		{"explicit offset", "2026-10-01T12:00:00.123456789+01:00", time.Date(2026, 10, 1, 11, 0, 0, 123456789, time.UTC), false},
		{"legacy nanoseconds", "2026-10-01T11:00:00.123456789", time.Date(2026, 10, 1, 11, 0, 0, 123456789, time.UTC), false},
		{"legacy seconds", "2026-10-01T11:00:00", time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC), false},
		{"empty", "", time.Time{}, true},
		{"malformed", "invalid", time.Time{}, true},
		{"date only", "2026-10-01", time.Time{}, true},
		{"invalid offset", "2026-10-01T11:00:00+bad", time.Time{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stamp, err := ParseTimestampUTC(tc.value)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, stamp)
		})
	}
}

// This single real-clock observation verifies the emitted instant lies between
// its surrounding reads; duplicating it into table rows adds no distinct case.
func TestDefaultTimeProviderEmitsExplicitUTC(t *testing.T) {
	before := time.Now().UTC()
	value := (&DefaultTimeProvider{}).NowUTC()
	after := time.Now().UTC()
	require.True(t, strings.HasSuffix(value, "Z"))
	stamp, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	require.False(t, stamp.Before(before))
	require.False(t, stamp.After(after))
}
