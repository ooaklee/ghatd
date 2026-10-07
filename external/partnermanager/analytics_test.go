package partnermanager

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

type analyticsServiceStub struct {
	ReferralService
	report  referral.Analytics
	err     error
	read    func()
	calls   int
	partner string
}

func (s *analyticsServiceStub) GetAnalytics(_ context.Context, partner string, _ referral.AnalyticsQuery) (referral.Analytics, error) {
	s.calls++
	s.partner = partner
	if s.read != nil {
		s.read()
	}
	return s.report, s.err
}

// Audit disposition: named self/scoped-operator cases cover current authority,
// enrollment, malformed results, optional failure and privacy of safe reports.
func TestManagerReferralAnalyticsBoundaries(t *testing.T) {
	for _, admin := range []bool{false, true} {
		prefix := "customer/"
		if admin {
			prefix = "operator/"
		}
		for _, tc := range []struct {
			name, mode string
			want       error
		}{
			{"authorized_owning_aggregate", "", nil}, {"admission_paused_retains_reports", "paused", nil}, {"denied_before_read", "denied", ErrDenied}, {"revoked_during_read_discards_result", "revoked", ErrDenied}, {"changed_enrollment_discards_result", "changed", ErrDenied}, {"foreign_partner_result", "partner", ErrUnavailable}, {"foreign_program_result", "program", ErrUnavailable}, {"false_zero_from_outage", "outage", referral.ErrUnavailable}, {"capacity_is_not_zero", "capacity", referral.ErrCapacity}, {"negative_count_is_not_a_report", "negative", ErrUnavailable}, {"link_conversion_cannot_exceed_global_conversion", "link_total", ErrUnavailable},
			{"page_sum_cannot_exceed_global_totals", "page_total", ErrUnavailable},
			{"invalid_membership_accounting", "membership", ErrUnavailable}, {"false_pagination_cursor", "cursor", ErrUnavailable}, {"invalid_query_before_read", "query", ErrInvalid},
		} {
			t.Run(prefix+tc.name, func(t *testing.T) {
				m, p, _, _, auth, _, _ := managerFixture(t)
				q := referral.AnalyticsQuery{Limit: 1}
				metrics := referral.VisitMetrics{Observations: 1, EligibleMeasuredVisits: 1, ConvertedMeasuredVisits: 1, SignupsFromVisitCohort: 1}
				s := &analyticsServiceStub{report: referral.Analytics{ProgramID: referral.ProgramID, PartnerID: "partner", AsOf: m.deps.Clock.Now(), Revision: strings.Repeat("a", 64), Visits: metrics, Signups: referral.SignupMetrics{MeasuredSignups: 1}, LifetimeRelationships: 1, CurrentRelationships: 1, Coverage: referral.AnalyticsCoverage{Scope: "anonymous_daily_observations"}, Links: []referral.LinkAnalytics{{LinkID: "own-link", Code: "own-code", Visits: metrics, Signups: referral.SignupMetrics{MeasuredSignups: 1}}}}}
				m.deps.Referral = s
				switch tc.mode {
				case "paused":
					m.deps.Controls = Controls{}
				case "denied":
					auth.deny = true
				case "revoked":
					s.read = func() { auth.deny = true }
				case "changed":
					s.read = func() { p.partner.CustomerID = "another-owner" }
				case "partner":
					s.report.PartnerID = "foreign"
				case "program":
					s.report.ProgramID = "foreign"
				case "outage":
					s.err = errors.Join(referral.ErrNotFound, referral.ErrUnavailable)
				case "capacity":
					s.err = referral.ErrCapacity
				case "negative":
					s.report.Visits.Observations = -1
				case "link_total":
					s.report.Visits.ConvertedMeasuredVisits = 0
				case "page_total":
					q.Limit = 2
					second := s.report.Links[0]
					second.LinkID = "own-link-2"
					second.Code = "own-code-2"
					s.report.Links = append(s.report.Links, second)
				case "membership":
					s.report.RetainedRelationships = 1
				case "cursor":
					s.report.HasMore = true
					s.report.NextAfterLinkID = "foreign-link"
				case "query":
					q.Limit = 101
				}
				var out referral.Analytics
				var err error
				if admin {
					out, err = m.AdminReferralAnalytics(context.Background(), "operator", "partner", q)
				} else {
					out, err = m.ReferralAnalytics(context.Background(), "owner", q)
				}
				require.ErrorIs(t, err, tc.want)
				if tc.want != nil {
					require.Empty(t, out)
				} else {
					require.Equal(t, s.report, out)
					require.Equal(t, "partner", s.partner)
					body, err := json.Marshal(out)
					require.NoError(t, err)
					for _, private := range []string{"referred_customer", "signup_id", "evidence_digest", "visit_cookie", "provider_id", "referral_id"} {
						require.NotContains(t, string(body), private)
					}
				}
				if tc.mode == "denied" || tc.mode == "query" {
					require.Zero(t, s.calls)
				}
			})
		}
	}
}
