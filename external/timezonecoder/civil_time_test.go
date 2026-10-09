package timezonecoder

import (
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/stretchr/testify/require"
)

func TestResolveLocalTime(t *testing.T) {
	for _, tc := range []struct {
		name, date, zone, clock, want string
	}{
		{"DST gap", "2026-03-29", "Europe/London", "01:30", ""},
		{"earliest DST fold", "2026-10-25", "Europe/London", "01:30", "2026-10-25T00:30:00Z"},
		{"half-hour offset", "2026-10-02", "Asia/Kolkata", "09:00", "2026-10-02T03:30:00Z"},
		{"quarter-hour offset", "2026-10-02", "Asia/Kathmandu", "09:00", "2026-10-02T03:15:00Z"},
		{"positive date boundary", "2026-10-02", "Pacific/Kiritimati", "00:00", "2026-10-01T10:00:00Z"},
		{"negative date boundary", "2026-10-02", "Etc/GMT+12", "23:00", "2026-10-03T11:00:00Z"},
		{"UTC", "2026-10-02", "UTC", "09:00", "2026-10-02T09:00:00Z"},
		{"impossible date", "2026-02-30", "Europe/London", "09:00", ""},
		{"unpadded date", "2026-2-3", "UTC", "09:00", ""},
		{"unpadded clock", "2026-10-02", "UTC", "9:5", ""},
		{"invalid hour", "2026-10-02", "UTC", "24:00", ""},
		{"process local zone rejected", "2026-10-02", "Local", "09:00", ""},
		{"unknown zone", "2026-10-02", "Invalid/Zone", "09:00", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instant, err := ResolveLocalTime(tc.date, tc.zone, tc.clock)
			if tc.want == "" {
				require.ErrorIs(t, err, catalogue.ErrInvalidPayload)
				require.True(t, instant.IsZero())
				return
			}
			require.NoError(t, err)
			want, err := time.Parse(time.RFC3339, tc.want)
			require.NoError(t, err)
			require.Equal(t, want, instant)
			require.Equal(t, time.UTC, instant.Location())
		})
	}
}
