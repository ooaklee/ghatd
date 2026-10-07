package referral

import (
	"context"
	"math"
	"time"
)

// AnalyticsConfig is an explicit host-approved raw observation lifetime. It
// does not expire signup ownership, financial records or anonymous day counts.
type AnalyticsConfig struct {
	ObservationRetention time.Duration
}

// WithAnalytics returns a separately configured service without mutating a
// shared instance. Collection has no implicit retention default. Twenty-four
// hours covers every supported visit nonce; hosts approve the actual lifetime.
func (s *Service) WithAnalytics(config AnalyticsConfig) (*Service, error) {
	if s == nil || nilReferralDependency(s.repo) || nilReferralDependency(s.clock) || nilReferralDependency(s.ids) {
		return nil, ErrUnavailable
	}
	if config.ObservationRetention < 24*time.Hour || config.ObservationRetention > 365*24*time.Hour {
		return nil, ErrInvalid
	}
	copy := *s
	copy.analytics = &config
	return &copy, nil
}

// VisitDay contains anonymous counts only: no customer, observation identity,
// nonce, digest, IP or user-agent data. The UTC day/link count changes atomically
// with its raw observation and visit receipt under the owning link guard.
type VisitDay struct {
	ProgramID string       `json:"program_id"`
	LinkID    string       `json:"link_id"`
	Day       time.Time    `json:"day"`
	Revision  int64        `json:"revision"`
	Counts    VisitMetrics `json:"counts"`
}

func utcDay(at time.Time) time.Time { return at.UTC().Truncate(24 * time.Hour) }

func (d VisitDay) Validate() error {
	if d.ProgramID != ProgramID || !analyticsID(d.LinkID, 128) || d.Day.IsZero() || !d.Day.Equal(utcDay(d.Day)) || d.Revision < 1 || !validVisitMetrics(d.Counts) || d.Counts.Observations < 1 || d.Counts.ConvertedMeasuredVisits != 0 || d.Counts.SignupsFromVisitCohort != 0 {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) recordObservation(ctx context.Context, tx Repository, c Click) error {
	if s.analytics == nil {
		return ErrUnavailable
	}
	day := utcDay(c.OccurredAt)
	d, err := tx.GetVisitDay(ctx, c.LinkID, day)
	expected := int64(0)
	if singleReferralCause(err, ErrNotFound) {
		d = VisitDay{ProgramID: ProgramID, LinkID: c.LinkID, Day: day}
	} else {
		if err != nil {
			return err
		}
		if d.Validate() != nil || d.LinkID != c.LinkID || !d.Day.Equal(day) {
			return ErrUnavailable
		}
		expected = d.Revision
	}
	if d.Revision == math.MaxInt64 {
		return ErrCapacity
	}
	d.Revision++
	increment := VisitMetrics{Observations: 1}
	switch c.Classification {
	case VisitEligible:
		increment.EligibleMeasuredVisits = 1
	case VisitDuplicate:
		increment.DuplicateObservations = 1
	case VisitKnownBot:
		increment.KnownBotObservations = 1
	case "":
		increment.UnmeasuredObservations = 1
	default:
		return ErrInvalid
	}
	if !addVisitMetrics(&d.Counts, increment) {
		return ErrCapacity
	}
	if err := tx.PutVisitDay(ctx, d, expected); err != nil {
		return err
	}
	return tx.RecordClick(ctx, c, c.OccurredAt.Add(s.analytics.ObservationRetention))
}

// PartialDays lists the complete UTC boundary days required for an exact
// sub-day query. Whole UTC-day ranges and all-time reports need no raw rows.
func (q AnalyticsQuery) PartialDays() []time.Time {
	var days []time.Time
	for _, at := range []*time.Time{q.From, q.To} {
		if at == nil || at.Equal(utcDay(*at)) {
			continue
		}
		day := utcDay(*at)
		if len(days) == 0 || !days[0].Equal(day) {
			days = append(days, day)
		}
	}
	return days
}
