package referral

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Named cases distinguish persistent day reports from exact sub-day evidence;
// deleting optional raw analytics must never manufacture zero or move ownership.
func TestAnalyticsRetainedDaysAndBoundaryGranularity(t *testing.T) {
	for _, tc := range []struct {
		name, mode   string
		observations int64
		want         error
	}{
		{"all_time_after_raw_cleanup", "expired", 5, nil},
		{"whole_UTC_day_after_cleanup", "whole", 5, nil},
		{"retained_partial_day_is_exact", "partial", 4, nil},
		{"same_day_from_inclusive_to_exclusive", "same", 2, nil},
		{"expired_boundary_is_not_a_zero_report", "missing", 0, ErrGranularity},
		{"partially_expired_boundary_is_not_prorated", "incomplete", 0, ErrGranularity},
		{"aggregate_counts_exceed_record_budget", "large", 100002, nil},
		{"anonymous_counter_sum_overflow", "overflow", 0, ErrCapacity},
		{"category_sum_overflow_is_invalid", "category_overflow", 0, ErrUnavailable},
		{"duplicate_day_is_invalid", "duplicate", 0, ErrUnavailable},
		{"foreign_day_is_invalid", "foreign", 0, ErrUnavailable},
		{"future_day_is_invalid", "future", 0, ErrUnavailable},
		{"origin_time_without_identifier_is_invalid", "origin_pair", 0, ErrUnavailable},
		{"origin_before_link_is_invalid", "origin_floor", 0, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, at := analyticsFixture()
			q := AnalyticsQuery{Limit: 100}
			switch tc.mode {
			case "expired":
				snapshot.Clicks = nil
			case "whole":
				from, to := utcDay(at), utcDay(at).Add(24*time.Hour)
				q.From, q.To = &from, &to
				snapshot.Clicks = nil
			case "partial", "missing", "incomplete":
				to := at.Add(10 * time.Minute)
				q.From, q.To = &at, &to
				if tc.mode == "missing" {
					snapshot.Clicks = nil
				}
				if tc.mode == "incomplete" {
					snapshot.Clicks = snapshot.Clicks[1:]
				}
			case "same":
				from, to := at.Add(time.Minute), at.Add(3*time.Minute)
				q.From, q.To = &from, &to
			case "large":
				snapshot.Clicks = nil
				snapshot.Days[0].Counts = VisitMetrics{Observations: 100001, EligibleMeasuredVisits: 100000, DuplicateObservations: 1}
			case "overflow":
				snapshot.Clicks = nil
				snapshot.Relationships = nil
				snapshot.Days[0].Counts = VisitMetrics{Observations: math.MaxInt64, UnmeasuredObservations: math.MaxInt64}
			case "category_overflow":
				snapshot.Days[0].Counts = VisitMetrics{Observations: math.MaxInt64, EligibleMeasuredVisits: math.MaxInt64, DuplicateObservations: 1}
			case "duplicate":
				snapshot.Days = append(snapshot.Days, snapshot.Days[0])
			case "foreign":
				snapshot.Days[0].LinkID = "foreign"
			case "future":
				snapshot.Days[0].Day = utcDay(at).Add(24 * time.Hour)
			case "origin_pair":
				snapshot.Relationships[2].History[0].SourceMeasuredOccurredAt = &at
				snapshot.Relationships[2].Head = snapshot.Relationships[2].History[0]
			case "origin_floor":
				before := at.Add(-2 * time.Hour)
				snapshot.Relationships[0].History[0].SourceMeasuredOccurredAt = &before
			}
			repo := &analyticsRepo{snapshot: snapshot}
			s, err := NewService(repo, fakeClock{at.Add(3 * time.Hour)}, &seqIDs{}, 24*time.Hour)
			require.NoError(t, err)
			out, err := s.GetAnalytics(context.Background(), "partner", q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				return
			}
			require.Equal(t, tc.observations, out.Visits.Observations)
			if tc.mode != "same" {
				require.EqualValues(t, 1, out.Visits.ConvertedMeasuredVisits)
				require.EqualValues(t, 2, out.Visits.SignupsFromVisitCohort)
			}
			require.Equal(t, "anonymous_daily_observations", out.Coverage.Scope)
		})
	}
}

func TestAnalyticsPersistentRevisionAndOverflowProjection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		overflow bool
	}{{"raw_expiry_and_query_do_not_change_persistent_revision", false}, {"page_sum_overflow_cannot_wrap_under_global_total", true}} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, at := analyticsFixture()
			repo := &analyticsRepo{snapshot: snapshot}
			s, err := NewService(repo, fakeClock{at.Add(3 * time.Hour)}, &seqIDs{}, 24*time.Hour)
			require.NoError(t, err)
			q := AnalyticsQuery{Limit: 100}
			whole, err := s.GetAnalytics(context.Background(), "partner", q)
			require.NoError(t, err)
			if tc.overflow {
				counts := VisitMetrics{Observations: math.MaxInt64, EligibleMeasuredVisits: math.MaxInt64}
				whole.Visits = counts
				for i := range whole.Links {
					whole.Links[i].Visits = counts
				}
				require.ErrorIs(t, whole.Validate(q), ErrUnavailable)
				return
			}
			to := at.Add(10 * time.Minute)
			q.To = &to
			partial, err := s.GetAnalytics(context.Background(), "partner", q)
			require.NoError(t, err)
			require.Equal(t, whole.Revision, partial.Revision)
			repo.snapshot.Clicks = nil
			q.To = nil
			cleaned, err := s.GetAnalytics(context.Background(), "partner", q)
			require.NoError(t, err)
			require.Equal(t, whole, cleaned)
		})
	}
}
