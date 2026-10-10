package referral

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type analyticsRepo struct {
	Repository
	snapshot AnalyticsSnapshot
	err      error
	calls    int
}

func (r *analyticsRepo) ReadAnalyticsSnapshot(context.Context, string, string, AnalyticsQuery) (AnalyticsSnapshot, error) {
	r.calls++
	return r.snapshot, r.err
}

func analyticsFixture() (AnalyticsSnapshot, time.Time) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	links := []Link{{ID: "link-a", Code: "code-a", ProgramID: ProgramID, PartnerID: "partner", CreatedAt: at.Add(-time.Hour)}, {ID: "link-b", Code: "code-b", ProgramID: ProgramID, PartnerID: "partner", CreatedAt: at.Add(-time.Hour)}}
	first := Click{ID: "visit-a", LinkID: links[0].ID, Code: links[0].Code, OccurredAt: at, Classification: VisitEligible, MeasuredClickID: "visit-a", VisitDigest: strings.Repeat("a", 64)}
	dup := first
	dup.ID, dup.Classification, dup.OccurredAt = "duplicate", VisitDuplicate, at.Add(time.Minute)
	second := first
	second.ID, second.MeasuredClickID, second.LinkID, second.Code, second.VisitDigest, second.OccurredAt = "visit-b", "visit-b", links[1].ID, links[1].Code, strings.Repeat("b", 64), at.Add(10*time.Minute)
	snapshot := AnalyticsSnapshot{Links: links, Clicks: []Click{first, dup, {ID: "bot", LinkID: links[0].ID, Code: links[0].Code, OccurredAt: at.Add(2 * time.Minute), Classification: VisitKnownBot}, {ID: "legacy", LinkID: links[0].ID, Code: links[0].Code, OccurredAt: at.Add(3 * time.Minute)}, second}}
	for i, when := range []time.Time{at, at.Add(30 * time.Minute), at.Add(5 * time.Minute), at.Add(6 * time.Minute)} {
		customer := []string{"private-one", "private-two", "private-three", "private-four"}[i]
		r := Referral{ID: []string{"ref-one", "ref-two", "ref-three", "ref-four"}[i], Revision: 1, ProgramID: ProgramID, PartnerID: "partner", ReferredCustomer: customer, SignupID: "signup-" + customer, EvidenceDigest: "signed-digest", SourceKind: "click", SourceCode: links[0].Code, SourceMeasuredClickID: first.ID, SourceMeasuredOccurredAt: &at, LockedAt: when, PostedAt: when, TermsSnapshot: fixtureTerms()}
		if i == 2 {
			r.SourceMeasuredClickID = ""
			r.SourceMeasuredOccurredAt = nil
		}
		if i == 3 {
			r.SourceKind = "admin"
			r.SourceMeasuredClickID = ""
			r.SourceMeasuredOccurredAt = nil
			r.SourceCode = ""
		}
		member := RelationshipSnapshot{ID: RelationshipReferenceID(ProgramID, "partner", customer), ProgramID: ProgramID, PartnerID: "partner", ReferredCustomer: customer, FirstReferralID: r.ID, Head: r, History: []Referral{r}}
		if i == 0 {
			head := r
			head.ID, head.PartnerID, head.Revision, head.CorrectionOf, head.PriorPartnerID = "ref-away", "other", 2, r.ID, r.PartnerID
			head.SourceKind, head.SourceCode, head.SourceMeasuredClickID = "admin", "", ""
			head.SourceMeasuredOccurredAt = nil
			head.LockedAt, head.PostedAt = at.Add(time.Hour), at.Add(time.Hour)
			member.Head = head
			member.History = append(member.History, head)
		}
		snapshot.Relationships = append(snapshot.Relationships, member)
	}
	snapshot.Days = []VisitDay{{ProgramID: ProgramID, LinkID: links[0].ID, Day: utcDay(at), Revision: 4, Counts: VisitMetrics{Observations: 4, EligibleMeasuredVisits: 1, DuplicateObservations: 1, KnownBotObservations: 1, UnmeasuredObservations: 1}}, {ProgramID: ProgramID, LinkID: links[1].ID, Day: utcDay(at), Revision: 1, Counts: VisitMetrics{Observations: 1, EligibleMeasuredVisits: 1}}}
	return snapshot, at
}

// Audit disposition: named count/cohort/cutover/pagination cases exercise full
// owning projection. Distinct conversions never use signups-per-click ratios.
func TestAnalyticsCohortsAndRetainedRelationships(t *testing.T) {
	for _, tc := range []struct {
		name, mode                                                                               string
		observations, eligible, measured, manual, current, corrections, converted, cohortSignups int
		scope                                                                                    string
	}{
		{name: "unfiltered_retains_original_owner", observations: 5, eligible: 2, measured: 2, manual: 1, current: 3, converted: 1, cohortSignups: 2, scope: "anonymous_daily_observations"},
		{name: "from_inclusive_to_exclusive_and_later_signup_conversion", mode: "period", observations: 4, eligible: 1, measured: 1, manual: 1, current: 3, converted: 1, cohortSignups: 2, scope: "anonymous_daily_observations"},
		{name: "second_link_page_retains_global_totals", mode: "page", observations: 5, eligible: 2, measured: 2, manual: 1, current: 3, converted: 1, cohortSignups: 2, scope: "anonymous_daily_observations"},
		{name: "out_of_period_link_page_keeps_global_cohort", mode: "period_page", observations: 4, eligible: 1, measured: 1, manual: 1, current: 3, converted: 1, cohortSignups: 2, scope: "anonymous_daily_observations"},
		{name: "reacquisition_does_not_duplicate_signup_or_membership", mode: "reacquired", observations: 5, eligible: 2, measured: 2, manual: 1, current: 4, corrections: 1, converted: 1, cohortSignups: 2, scope: "anonymous_daily_observations"},
		{name: "missing_optional_origin_is_explicit_not_reconstructed", mode: "missing", observations: 5, eligible: 2, measured: 2, manual: 1, current: 3, scope: "incomplete_measured_origins"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, at := analyticsFixture()
			repo := &analyticsRepo{snapshot: raw}
			q := AnalyticsQuery{Limit: 1}
			switch tc.mode {
			case "period", "period_page":
				to := at.Add(10 * time.Minute)
				q.From, q.To = &at, &to
				if tc.mode == "period_page" {
					q.AfterLinkID = "link-a"
				}
			case "page":
				q.AfterLinkID = "link-a"
			case "missing":
				repo.snapshot.Clicks = nil
				for i := 0; i < 2; i++ {
					repo.snapshot.Relationships[i].History[0].SourceMeasuredOccurredAt = nil
					if len(repo.snapshot.Relationships[i].History) == 1 {
						repo.snapshot.Relationships[i].Head = repo.snapshot.Relationships[i].History[0]
					}
				}
			case "reacquired":
				member := &repo.snapshot.Relationships[0]
				r := member.History[0]
				r.ID, r.Revision, r.CorrectionOf, r.PriorPartnerID = "ref-back", 3, member.Head.ID, member.Head.PartnerID
				r.SourceKind, r.SourceCode, r.SourceMeasuredClickID = "admin", "", ""
				r.SourceMeasuredOccurredAt = nil
				r.LockedAt, r.PostedAt = at.Add(2*time.Hour), at.Add(2*time.Hour)
				member.History = append(member.History, r)
				member.Head = r
			}
			s, err := NewService(repo, fakeClock{at.Add(3 * time.Hour)}, &seqIDs{}, 24*time.Hour)
			require.NoError(t, err)
			out, err := s.GetAnalytics(context.Background(), "partner", q)
			require.NoError(t, err)
			require.EqualValues(t, tc.observations, out.Visits.Observations)
			require.EqualValues(t, tc.eligible, out.Visits.EligibleMeasuredVisits)
			require.EqualValues(t, tc.converted, out.Visits.ConvertedMeasuredVisits)
			require.EqualValues(t, tc.cohortSignups, out.Visits.SignupsFromVisitCohort)
			require.EqualValues(t, 1, out.Visits.DuplicateObservations)
			require.EqualValues(t, 1, out.Visits.KnownBotObservations)
			require.EqualValues(t, 1, out.Visits.UnmeasuredObservations)
			require.EqualValues(t, tc.measured, out.Signups.MeasuredSignups)
			require.EqualValues(t, 1, out.Signups.UnmeasuredSignups)
			require.EqualValues(t, tc.manual, out.Signups.ManualInitialAcquisitions)
			require.EqualValues(t, tc.corrections, out.Signups.CorrectionAcquisitionEvents)
			require.Equal(t, 4, out.LifetimeRelationships)
			require.Equal(t, tc.current, out.CurrentRelationships)
			require.Equal(t, 4-tc.current, out.RetainedRelationships)
			require.Equal(t, tc.scope, out.Coverage.Scope)
			require.EqualValues(t, 1, out.Coverage.UnmeasuredSignupOrigins)
			require.EqualValues(t, 1, out.Coverage.LegacyUnmeasuredObservations)
			if tc.mode == "missing" {
				require.EqualValues(t, 2, out.Coverage.MissingMeasuredSignupOrigins)
			}
			require.Len(t, out.Links, 1)
			if tc.mode == "page" || tc.mode == "period_page" {
				require.Equal(t, "link-b", out.Links[0].LinkID)
				require.False(t, out.HasMore)
				if tc.mode == "period_page" {
					require.Empty(t, out.Links[0].Visits)
				}
			} else {
				require.True(t, out.HasMore)
				require.Equal(t, "link-a", out.NextAfterLinkID)
			}
			whole, err := s.GetAnalytics(context.Background(), "partner", AnalyticsQuery{Limit: 100, From: q.From, To: q.To})
			require.NoError(t, err)
			require.Equal(t, out.Visits, whole.Visits)
			require.Equal(t, out.Signups, whole.Signups)
			require.Equal(t, out.Revision, whole.Revision)
			body, err := json.Marshal(out)
			require.NoError(t, err)
			for _, private := range []string{"private-one", "ref-one", "signup-", "signed-digest", "old-optional-agent", strings.Repeat("a", 64)} {
				require.NotContains(t, string(body), private)
			}
		})
	}
}

func TestAnalyticsInvalidEvidenceAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"foreign_link_scope", "link", ErrUnavailable}, {"duplicate_observation_ID", "duplicate", ErrUnavailable}, {"unknown_classification", "class", ErrUnavailable}, {"unmeasured_user_agent_is_rejected", "user_agent", ErrUnavailable}, {"bot_with_measured_origin", "bot", ErrUnavailable}, {"foreign_measured_origin", "foreign", ErrUnavailable}, {"signup_before_eligible_visit", "before", ErrUnavailable}, {"foreign_click_origin_second_revision_is_also_malformed", "foreign_second_click", ErrUnavailable},
		{"click_origin_second_revision_is_not_another_signup", "second_click", ErrUnavailable},
		{"duplicate_origin_is_from_another_link", "duplicate_link", ErrUnavailable},
		{"duplicate_before_first_origin", "duplicate_before", ErrUnavailable},
		{"zero_report_clock", "clock", ErrUnavailable},
		{"ownership_history_gap", "gap", ErrUnavailable}, {"head_history_mismatch", "head", ErrUnavailable}, {"duplicate_lifetime_membership", "member", ErrUnavailable}, {"wrong_first_owned_reference", "first", ErrUnavailable}, {"capacity_discards_full_report", "capacity", ErrCapacity}, {"joined_absence_and_outage", "outage", ErrUnavailable}, {"invalid_limit_before_read", "limit", ErrInvalid}, {"foreign_cursor_cannot_change_scope", "cursor", ErrInvalid}, {"reversed_period_before_read", "dates", ErrInvalid}, {"cancelled_before_read", "cancelled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, at := analyticsFixture()
			r := &analyticsRepo{snapshot: raw}
			q := AnalyticsQuery{Limit: 100}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			switch tc.mode {
			case "link":
				r.snapshot.Links[0].PartnerID = "other"
			case "duplicate":
				r.snapshot.Clicks = append(r.snapshot.Clicks, r.snapshot.Clicks[0])
			case "user_agent":
				r.snapshot.Clicks[3].UserAgent = "must-not-be-collected"
			case "class":
				r.snapshot.Clicks[0].Classification = "unknown"
			case "bot":
				r.snapshot.Clicks[2].MeasuredClickID = "visit-a"
			case "foreign":
				r.snapshot.Relationships[0].History[0].SourceMeasuredClickID = "visit-b"
			case "before":
				r.snapshot.Clicks[0].OccurredAt = at.Add(time.Second)
			case "foreign_second_click":
				r.snapshot.Relationships[0].History[1].SourceKind = "click"
				r.snapshot.Relationships[0].Head = r.snapshot.Relationships[0].History[1]
			case "second_click":
				r.snapshot.Relationships[0].History[1].PartnerID = "partner"
				r.snapshot.Relationships[0].History[1].SourceKind = "click"
				r.snapshot.Relationships[0].History[1].SourceCode = "code-a"
				r.snapshot.Relationships[0].History[1].SourceMeasuredClickID = "visit-a"
				r.snapshot.Relationships[0].Head = r.snapshot.Relationships[0].History[1]
			case "duplicate_link":
				r.snapshot.Clicks[1].MeasuredClickID = "visit-b"
				r.snapshot.Clicks[1].VisitDigest = r.snapshot.Clicks[4].VisitDigest
				r.snapshot.Clicks[4].OccurredAt = at
			case "duplicate_before":
				r.snapshot.Clicks[1].OccurredAt = at.Add(-time.Second)
			case "gap":
				r.snapshot.Relationships[0].History = r.snapshot.Relationships[0].History[1:]
			case "head":
				r.snapshot.Relationships[0].Head = r.snapshot.Relationships[0].History[0]
			case "member":
				r.snapshot.Relationships = append(r.snapshot.Relationships, r.snapshot.Relationships[0])
			case "first":
				r.snapshot.Relationships[0].FirstReferralID = "other"
			case "capacity":
				r.snapshot.Clicks = make([]Click, AnalyticsCapacity+1)
			case "outage":
				r.err = errors.Join(ErrNotFound, ErrUnavailable)
			case "limit":
				q.Limit = 101
			case "cursor":
				q.AfterLinkID = "other-link"
			case "dates":
				before := at.Add(-time.Hour)
				q.From, q.To = &at, &before
			case "cancelled":
				cancel()
			}
			s, err := NewService(r, fakeClock{at.Add(3 * time.Hour)}, &seqIDs{}, 24*time.Hour)
			require.NoError(t, err)
			if tc.mode == "clock" {
				s.clock = fakeClock{}
			}
			out, err := s.GetAnalytics(ctx, "partner", q)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out)
			if tc.mode == "limit" || tc.mode == "dates" || tc.mode == "cancelled" {
				require.Zero(t, r.calls)
			}
		})
	}
}
