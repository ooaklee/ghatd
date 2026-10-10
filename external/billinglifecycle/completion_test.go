package billinglifecycle

import (
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/stretchr/testify/require"
)

func TestNextRefreshSlot(t *testing.T) {
	anchor := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		anchor, now time.Time
		cadence     time.Duration
		want        time.Time
		invalid     bool
	}{
		{"first_slot", anchor, anchor, 30 * time.Second, anchor.Add(30 * time.Second), false},
		{"exact_boundary_advances", anchor, anchor.Add(30 * time.Second), 30 * time.Second, anchor.Add(time.Minute), false},
		{"missed_slots_coalesce", anchor, anchor.Add(305 * time.Second), 30 * time.Second, anchor.Add(330 * time.Second), false},
		{"fractional_now_preserves_alignment", anchor, anchor.Add(31*time.Second + 500*time.Millisecond), 30 * time.Second, anchor.Add(time.Minute), false},
		{"minimum_cadence", anchor, anchor, time.Second, anchor.Add(time.Second), false},
		{"maximum_cadence", anchor, anchor.Add(time.Hour), 24 * time.Hour, anchor.Add(24 * time.Hour), false},
		{"zero_anchor", time.Time{}, anchor, time.Second, time.Time{}, true},
		{"zero_now", anchor, time.Time{}, time.Second, time.Time{}, true},
		{"clock_before_anchor", anchor, anchor.Add(-time.Nanosecond), time.Second, time.Time{}, true},
		{"too_short", anchor, anchor, time.Second - time.Nanosecond, time.Time{}, true},
		{"too_long", anchor, anchor, 24*time.Hour + time.Nanosecond, time.Time{}, true},
		{"duration_saturation", time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC), anchor, time.Second, time.Time{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NextRefreshSlot(tc.anchor, tc.now, tc.cadence)
			if tc.invalid {
				require.ErrorIs(t, err, billing.ErrRevenueInvalid)
				require.True(t, got.IsZero())
				return
			}
			require.NoError(t, err)
			require.True(t, got.Equal(tc.want))
			require.True(t, got.After(tc.now))
			require.Zero(t, got.Sub(tc.anchor)%tc.cadence)
		})
	}
}
