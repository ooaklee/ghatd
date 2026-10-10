package partnermanager

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

type summaryEarningsStub struct {
	EarningsService
	report  partnerearnings.ReferralAmountReport
	err     error
	read    func()
	calls   int
	partner string
	query   partnerearnings.ReferralAmountQuery
}

func (s *summaryEarningsStub) GetReferralAmounts(_ context.Context, partner string, q partnerearnings.ReferralAmountQuery) (partnerearnings.ReferralAmountReport, error) {
	s.calls++
	s.partner, s.query = partner, q
	if s.read != nil {
		s.read()
	}
	return s.report, s.err
}

// Audit disposition: named self/operator join, current authority, incomplete
// acceptance and privacy cases with independent fixtures and no public raw IDs.
func TestReferralSummaryBoundaries(t *testing.T) {
	cases := []struct {
		name, mode string
		want       error
	}{
		{name: "retained_owner_receives_safe_accepted_summary"},
		{name: "reacquired_periods_use_full_internal_revision_ids", mode: "reacquired"},
		{name: "no_acceptance_omits_commission_instead_of_entitlement_zero", mode: "empty"},
		{name: "accepted_zero_commission_remains_visible", mode: "zero"},
		{name: "paused_acquisition_keeps_retained_earnings", mode: "paused"},
		{name: "page_is_not_whole_program_total", mode: "page"},
		{name: "denied_before_read", mode: "denied", want: ErrDenied},
		{name: "current_capability_revoked_during_financial_read", mode: "revoked", want: ErrDenied},
		{name: "enrollment_changes_during_read", mode: "enrollment_changed", want: ErrDenied},
		{name: "on_page_correction_requires_refresh", mode: "changed", want: referral.ErrStaleWrite},
		{name: "second_attribution_read_failure_discards_financial_result", mode: "confirm_error", want: ErrUnavailable},
		{name: "financial_outage_never_becomes_zero", mode: "outage", want: ErrUnavailable},
		{name: "history_capacity_error_is_not_an_invalid_customer_request", mode: "capacity", want: partnerearnings.ErrReportTooLarge},
		{name: "wrong_financial_partner", mode: "partner", want: ErrUnavailable},
		{name: "wrong_financial_program", mode: "program", want: ErrUnavailable},
		{name: "wrong_financial_currency", mode: "currency", want: ErrUnavailable},
		{name: "wrong_group_identity", mode: "identity", want: ErrUnavailable},
		{name: "missing_group", mode: "missing", want: ErrUnavailable},
		{name: "negative_amount", mode: "negative", want: ErrUnavailable},
		{name: "zero_count_with_amount_is_inconsistent", mode: "inconsistent", want: ErrUnavailable},
		{name: "missing_payment_time", mode: "time", want: ErrUnavailable},
		{name: "payment_outside_selected_cohort", mode: "cohort", want: ErrUnavailable},
		{name: "unserializable_attribution_time_fails_closed", mode: "marshal", want: ErrUnavailable},
		{name: "invalid_limit", mode: "limit", want: ErrInvalid},
		{name: "invalid_date_order", mode: "dates", want: ErrInvalid},
	}
	for _, admin := range []bool{false, true} {
		prefix := "customer/"
		if admin {
			prefix = "operator/"
		}
		for _, tc := range cases {
			t.Run(prefix+tc.name, func(t *testing.T) {
				m, program, _, _, authority, _, _ := managerFixture(t)
				program.cfg.CurrencyExponent = 2
				at := m.deps.Clock.Now()
				r := &relationshipReadStub{page: managerRelationshipPage(at)}
				e := &summaryEarningsStub{report: partnerearnings.ReferralAmountReport{ProgramID: program.partner.ProgramID, PartnerID: "partner", Currency: "EUR", Revision: "financial-revision", AsOf: at.Add(time.Minute), LedgerSequence: 2, Balances: partnerearnings.Balances{AvailableMinor: 400}, Items: []partnerearnings.ReferralAmounts{{ID: r.page.Items[0].ID, AcceptedPayments: 1, FirstPaymentAt: &at, LastPaymentAt: &at, Amounts: partnerearnings.PaymentAmounts{OriginalRevenueMinor: 5000, AccruedMinor: 1000, MaturedEarnedMinor: 1000, ReservedBackingMinor: 600}}}}}
				m.deps.Referral, m.deps.Earnings = r, e
				q := ReferralSummaryQuery{Limit: 1}
				switch tc.mode {
				case "reacquired":
					period := r.page.Items[0].Periods[0]
					period.ReferralID = "private-reacquired-revision"
					period.From = at.Add(2 * time.Hour)
					period.Until = nil
					period.Corrected = true
					r.page.Items[0].Periods = append(r.page.Items[0].Periods, period)
					r.page.Items[0].Current = true
				case "empty":
					e.report.Items[0] = partnerearnings.ReferralAmounts{ID: r.page.Items[0].ID}
				case "zero":
					e.report.Items[0].Amounts = partnerearnings.PaymentAmounts{OriginalRevenueMinor: 1}
				case "paused":
					m.deps.Controls = Controls{}
					program.partner.CanAcquireReferrals = false
				case "page":
					r.page.HasMore, r.page.NextAfter = true, r.page.Items[0].ID
				case "denied":
					authority.deny = true
				case "revoked":
					e.read = func() { authority.deny = true }
				case "enrollment_changed":
					e.read = func() { program.partner.ProgramID = "other-program" }
				case "changed":
					e.read = func() { until := at.Add(2 * time.Hour); r.page.Items[0].Periods[0].Until = &until }
				case "confirm_error":
					r.read = func() {
						if r.calls == 2 {
							r.err = ErrUnavailable
						}
					}
				case "outage":
					e.err = errors.Join(partnerearnings.ErrNotFound, ErrUnavailable)
				case "capacity":
					e.err = partnerearnings.ErrReportTooLarge
				case "partner":
					e.report.PartnerID = "other"
				case "program":
					e.report.ProgramID = "other"
				case "currency":
					e.report.Currency = "USD"
				case "identity":
					e.report.Items[0].ID = "other"
				case "missing":
					e.report.Items = nil
				case "negative":
					e.report.Items[0].Amounts.AccruedMinor = -1
				case "inconsistent":
					e.report.Items[0].AcceptedPayments = 0
				case "time":
					e.report.Items[0].FirstPaymentAt = nil
				case "cohort":
					from := at.Add(time.Hour)
					q.From = &from
				case "marshal":
					future := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
					until := future.Add(time.Hour)
					r.page.Items[0].FirstOwnedAt = future
					r.page.Items[0].Periods[0].From = future
					r.page.Items[0].Periods[0].Until = &until
				case "limit":
					q.Limit = 101
				case "dates":
					q.From, q.To = &at, &at
				}
				var out ReferralSummaryPage
				var err error
				if admin {
					out, err = m.AdminReferralSummaries(context.Background(), "operator", "partner", q)
				} else {
					out, err = m.ReferralSummaries(context.Background(), "owner", q)
				}
				require.ErrorIs(t, err, tc.want)
				if tc.want != nil {
					require.Empty(t, out)
					if tc.mode == "denied" || tc.mode == "limit" || tc.mode == "dates" || tc.mode == "marshal" {
						require.Zero(t, e.calls)
					}
					return
				}
				require.Equal(t, "partner", e.partner)
				require.Equal(t, r.page.Items[0].ID, e.query.Groups[0].ID)
				require.Equal(t, "private-revision", e.query.Groups[0].ReferralIDs[0], "join uses owning service evidence, never public period digest")
				if tc.mode == "reacquired" {
					require.Equal(t, []string{"private-revision", "private-reacquired-revision"}, e.query.Groups[0].ReferralIDs)
				}
				require.Equal(t, 2, r.calls)
				require.Len(t, authority.calls, 2)
				require.Equal(t, "visible_relationships", out.Scope)
				require.Equal(t, "not_evaluated", out.SourceCoverage)
				require.Equal(t, "not_evaluated", out.SubscriptionCoverage)
				require.Equal(t, at, out.AttributionObservedAt)
				require.Equal(t, at.Add(time.Minute), out.FinancialAsOf)
				require.NotEmpty(t, out.AttributionRevision)
				require.Equal(t, "financial-revision", out.FinancialRevision)
				require.EqualValues(t, 400, out.CurrentPartnerBalances.AvailableMinor)
				row := out.Items[0]
				wire, err := json.Marshal(out)
				require.NoError(t, err)
				for _, private := range []string{"private-customer", "private-revision", "private-reacquired-revision", "private-policy", "private-plan", "partner_id", "payment_id", "original_revenue_minor", "referred_customer", "first_payment_at", "last_payment_at", "correction_reason"} {
					require.NotContains(t, string(wire), private)
				}
				if tc.mode == "empty" {
					require.Equal(t, "no_accepted_accrual", row.Acceptance)
					require.Nil(t, row.Commission)
					require.NotContains(t, string(wire), "\"commission\"")
				} else {
					require.Equal(t, "accepted_accruals", row.Acceptance)
					require.NotNil(t, row.Commission)
					if tc.mode == "zero" {
						require.Zero(t, row.Commission.AccruedMinor)
					} else {
						require.EqualValues(t, 1000, row.Commission.AccruedMinor)
						require.EqualValues(t, 600, row.Commission.ReservedBackingMinor)
					}
				}
			})
		}
	}
}
