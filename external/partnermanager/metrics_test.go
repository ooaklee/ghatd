package partnermanager

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

type financialMetricsStub struct {
	EarningsService
	report  partnerearnings.FinancialMetrics
	err     error
	read    func()
	calls   int
	partner string
	query   partnerearnings.FinancialMetricsQuery
}

func (s *financialMetricsStub) GetFinancialMetrics(_ context.Context, partner string, q partnerearnings.FinancialMetricsQuery) (partnerearnings.FinancialMetrics, error) {
	s.calls++
	s.partner, s.query = partner, q
	if s.read != nil {
		s.read()
	}
	return s.report, s.err
}

// Audit disposition: named verified-owner and scoped-operator cases cover safe
// projections, current authorization/enrollment, range semantics and failures.
func TestManagerFinancialMetricsBoundaries(t *testing.T) {
	cases := []struct {
		name, mode string
		want       error
	}{
		{name: "authorized_snapshot_and_private_plan_projection"},
		{name: "paused_commercial_admission_preserves_reporting", mode: "paused"},
		{name: "no_accepted_cohort_omits_commission_but_keeps_period_payment", mode: "empty"},
		{name: "accepted_zero_commission_is_explicit", mode: "zero"},
		{name: "denied_before_financial_read", mode: "denied", want: ErrDenied},
		{name: "revoked_during_financial_read_discards_result", mode: "revoked", want: ErrDenied},
		{name: "changed_enrollment_discards_result", mode: "changed", want: ErrDenied},
		{name: "wrong_partner_returned", mode: "partner", want: ErrUnavailable},
		{name: "wrong_program_returned", mode: "program", want: ErrUnavailable},
		{name: "wrong_currency_returned", mode: "currency", want: ErrUnavailable},
		{name: "wrong_enrollment_program_fails_before_read", mode: "enrollment", want: ErrUnavailable},
		{name: "joined_absence_and_outage_is_not_zero", mode: "outage", want: ErrUnavailable},
		{name: "malformed_plan_cursor_fails_closed", mode: "cursor", want: ErrUnavailable},
		{name: "false_more_cursor_fails_closed", mode: "more", want: ErrUnavailable},
		{name: "invalid_date_range_fails_before_read", mode: "date", want: ErrInvalid},
		{name: "negative_cohort_amount_is_not_customer_credit", mode: "negative_cohort", want: ErrUnavailable},
		{name: "negative_period_movement_is_malformed", mode: "negative_movement", want: ErrUnavailable},
		{name: "missing_acceptance_cannot_have_cohort_amounts", mode: "false_zero", want: ErrUnavailable},
		{name: "processing_claim_cannot_hide_potentially_sent_exposure", mode: "exposure", want: ErrUnavailable},
	}
	for _, admin := range []bool{false, true} {
		prefix := "customer/"
		if admin {
			prefix = "operator/"
		}
		for _, tc := range cases {
			t.Run(prefix+tc.name, func(t *testing.T) {
				m, p, _, _, auth, _, _ := managerFixture(t)
				p.cfg.CurrencyExponent = 2
				at := m.deps.Clock.Now()
				from, to := at.Add(-24*time.Hour), at
				s := &financialMetricsStub{report: partnerearnings.FinancialMetrics{ProgramID: partnerprogram.ProgramID, PartnerID: "partner", Currency: "EUR", Revision: "snapshot", AsOf: at, LedgerSequence: 3, AcceptedAllocationRows: 1, CohortAmounts: partnerearnings.PaymentAmounts{OriginalRevenueMinor: 424242, AccruedMinor: 1000}, PeriodMovements: partnerearnings.CommissionMovements{PaidMinor: 600}, CurrentClaims: partnerearnings.CurrentClaimMetrics{Paid: 1}, CurrentPartnerBalances: partnerearnings.Balances{AvailableMinor: 400}, RemainingCommissionMinor: 400, Plans: []partnerearnings.PlanFinancialMetrics{{Key: "plan:private-plan", PlanID: "private-plan", AcceptedAllocationRows: 1}}}}
				m.deps.Earnings = s
				switch tc.mode {
				case "paused":
					m.deps.Controls = Controls{}
				case "empty":
					s.report.AcceptedAllocationRows, s.report.CohortAmounts, s.report.Plans[0].AcceptedAllocationRows = 0, partnerearnings.PaymentAmounts{}, 0
				case "zero":
					s.report.CohortAmounts.AccruedMinor = 0
				case "denied":
					auth.deny = true
				case "revoked":
					s.read = func() { auth.deny = true }
				case "changed":
					s.read = func() { p.partner.CustomerID = "other-owner" }
				case "partner":
					s.report.PartnerID = "other-partner"
				case "program":
					s.report.ProgramID = "other-program"
				case "currency":
					s.report.Currency = "USD"
				case "enrollment":
					p.partner.ProgramID = "other-program"
				case "outage":
					s.err = errors.Join(partnerearnings.ErrNotFound, ErrUnavailable)
				case "cursor":
					s.report.Plans[0].Key = "foreign-key"
				case "more":
					s.report.HasMore, s.report.NextAfterPlanKey = true, "plan:other"
				case "date":
					to = from
				case "negative_cohort":
					s.report.CohortAmounts.AccruedMinor = -1
				case "negative_movement":
					s.report.PeriodMovements.ReturnedMinor = -1
				case "false_zero":
					s.report.AcceptedAllocationRows = 0
				case "exposure":
					s.report.CurrentClaims.Processing = 1
				}
				if admin {
					out, err := m.AdminFinancialMetrics(context.Background(), "operator", "partner", partnerearnings.FinancialMetricsQuery{Limit: 1, From: &from, To: &to})
					require.ErrorIs(t, err, tc.want)
					if tc.want != nil {
						require.Empty(t, out)
					} else {
						require.Equal(t, s.report, out)
					}
				} else {
					out, err := m.FinancialSummary(context.Background(), "owner", FinancialSummaryQuery{From: &from, To: &to})
					require.ErrorIs(t, err, tc.want)
					if tc.want != nil {
						require.Empty(t, out)
					} else {
						require.Equal(t, s.report.AcceptedAllocationRows, out.AcceptedAllocationRows)
						require.EqualValues(t, 600, out.PeriodMovements.PaidMinor)
						require.EqualValues(t, 400, out.RemainingCommissionMinor)
						require.EqualValues(t, 400, out.CurrentPartnerBalances.AvailableMinor)
						require.Equal(t, "not_evaluated", out.SourceCoverage)
						require.Equal(t, "not_evaluated", out.SubscriptionCoverage)
						if tc.mode == "empty" {
							require.Nil(t, out.CohortCommission)
							require.Equal(t, "no_accepted_accrual", out.Acceptance)
						} else {
							require.NotNil(t, out.CohortCommission)
							require.Equal(t, s.report.CohortAmounts.AccruedMinor, out.CohortCommission.AccruedMinor)
							require.Equal(t, "accepted_accruals", out.Acceptance)
						}
						body, err := json.Marshal(out)
						require.NoError(t, err)
						for _, forbidden := range []string{"private-plan", "424242", "partner_id", "program_id", "original_revenue", "PlanID"} {
							require.NotContains(t, string(body), forbidden)
						}
					}
				}
				if tc.mode == "denied" || tc.mode == "enrollment" || tc.mode == "date" {
					require.Zero(t, s.calls)
				}
				if tc.want == nil {
					require.Equal(t, "partner", s.partner)
					require.Equal(t, from, *s.query.From)
					require.Equal(t, to, *s.query.To)
					if !admin {
						require.Empty(t, s.query.PlanID)
						require.Empty(t, s.query.AfterPlanKey)
					}
				}
			})
		}
	}
}

// Audit disposition: named malformed caller filters demonstrate one owning
// canonical contract, rejected before the financial service is read.
func TestFinancialMetricsCanonicalQueryBeforeRead(t *testing.T) {
	for _, tc := range []struct{ name, plan, after, partner string }{
		{name: "embedded_control_character_plan", plan: "pl\tan"},
		{name: "invalid_utf8_plan", plan: "pl\xffan"},
		{name: "padded_plan", plan: " padded "},
		{name: "overlong_plan", plan: strings.Repeat("p", 257)},
		{name: "control_character_cursor_suffix", after: "plan:pl\tan"},
		{name: "empty_cursor_suffix", after: "plan:"},
		{name: "noncanonical_selected_partner", partner: "part\tner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, _, _, _ := managerFixture(t)
			s := &financialMetricsStub{}
			m.deps.Earnings = s
			partner := "partner"
			if tc.partner != "" {
				partner = tc.partner
			}
			out, err := m.AdminFinancialMetrics(context.Background(), "operator", partner, partnerearnings.FinancialMetricsQuery{Limit: 1, PlanID: tc.plan, AfterPlanKey: tc.after})
			require.ErrorIs(t, err, ErrInvalid)
			require.Empty(t, out)
			require.Zero(t, s.calls)
		})
	}
}
