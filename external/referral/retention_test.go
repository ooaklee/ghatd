package referral

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExplicitAnalyticsRetentionConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		duration time.Duration
		want     error
	}{
		{"no_implicit_default", 0, ErrInvalid}, {"under_nonce_maximum", 24*time.Hour - time.Nanosecond, ErrInvalid},
		{"maximum_nonce_lifetime", 24 * time.Hour, nil}, {"host_selected_longer_lifetime", 30 * 24 * time.Hour, nil},
		{"supported_cap", 365 * 24 * time.Hour, nil}, {"over_cap", 365*24*time.Hour + time.Nanosecond, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, p, link, e := fixture(t)
			s.analytics = nil
			configured, err := s.WithAnalytics(AnalyticsConfig{ObservationRetention: tc.duration})
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, s.analytics, "builder must not mutate an already shared service")
			if tc.want != nil {
				require.Nil(t, configured)
			} else {
				require.Equal(t, tc.duration, configured.analytics.ObservationRetention)
			}
			_, err = s.ObserveClick(context.Background(), link.Code, "private-agent")
			require.ErrorIs(t, err, ErrUnavailable)
			_, err = s.ObserveVisit(context.Background(), VisitRequest{Code: link.Code, Consented: true, KnownBot: true})
			require.ErrorIs(t, err, ErrUnavailable)
			v, err := s.ObserveVisit(context.Background(), VisitRequest{Code: "missing", Consented: false})
			require.NoError(t, err)
			require.Empty(t, v)
			_, err = s.LockAttribution(context.Background(), p, link, e)
			require.NoError(t, err, "attribution does not depend on analytics configuration")
			require.Empty(t, repo.clicks)
			require.Empty(t, repo.days)
			require.Empty(t, repo.visits)
		})
	}
}

func TestAnonymousObservationCounterAndRawPrivacy(t *testing.T) {
	for _, tc := range []struct {
		name string
		bot  bool
	}{{"unmeasured_API_does_not_store_user_agent", false}, {"known_bot_uses_same_anonymous_transaction", true}} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, _, link, _ := fixture(t)
			var err error
			if tc.bot {
				_, err = s.ObserveVisit(context.Background(), VisitRequest{Code: link.Code, Consented: true, KnownBot: true})
			} else {
				_, err = s.ObserveClick(context.Background(), link.Code, "private-user-agent")
			}
			require.NoError(t, err)
			require.Len(t, repo.clicks, 1)
			require.Empty(t, repo.clicks[0].UserAgent)
			require.Len(t, repo.days, 1)
			for _, day := range repo.days {
				require.NoError(t, day.Validate())
				require.EqualValues(t, 1, day.Counts.Observations)
				if tc.bot {
					require.EqualValues(t, 1, day.Counts.KnownBotObservations)
				} else {
					require.EqualValues(t, 1, day.Counts.UnmeasuredObservations)
				}
			}
			require.Empty(t, repo.visits)
		})
	}
}
