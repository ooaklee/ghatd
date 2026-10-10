package partnermanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named customer/operator cases verify complete safe owning
// maturity aggregates, current authority and rejection of false monetary bounds.
func TestManagerCurrentMaturityBoundaries(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, tc := range []struct {
			name, mode string
			want       error
		}{
			{"owned_due_aggregate_despite_empty_date_cohort", "", nil},
			{"paused_admission_preserves_due_obligations", "paused", nil},
			{"zero_credit_due_work_remains_visible", "zero", nil},
			{"revocation_after_read_discards_due_report", "revoked", ErrDenied},
			{"negative_due_rows", "rows", ErrUnavailable},
			{"hold_exceeds_due_net", "hold", ErrUnavailable},
			{"due_net_exceeds_complete_pending", "net", ErrUnavailable},
			{"due_hold_exceeds_complete_pending_hold", "total_hold", ErrUnavailable},
			{"positive_due_rows_require_oldest_time", "time", ErrUnavailable},
			{"oldest_due_time_cannot_be_future", "future", ErrUnavailable},
			{"empty_due_count_cannot_have_credit", "false_zero", ErrUnavailable},
		} {
			prefix := "customer/"
			if admin {
				prefix = "operator/"
			}
			t.Run(prefix+tc.name, func(t *testing.T) {
				m, _, _, _, auth, _, _ := managerFixture(t)
				at := m.deps.Clock.Now()
				oldest := at.Add(-time.Hour)
				due := partnerearnings.CurrentMaturity{DueAllocationRows: 2, DuePendingMinor: 1000, DueDisputeHoldMinor: 600, OldestAvailableAt: &oldest}
				s := &financialMetricsStub{report: partnerearnings.FinancialMetrics{ProgramID: partnerprogram.ProgramID, PartnerID: "partner", Currency: "EUR", Revision: "owning-snapshot", AsOf: at, Plans: []partnerearnings.PlanFinancialMetrics{}, CurrentPartnerBalances: partnerearnings.Balances{PendingMinor: 2000, PendingDisputeHoldMinor: 800}, CurrentMaturity: due}}
				m.deps.Earnings = s
				switch tc.mode {
				case "paused":
					m.deps.Controls = Controls{}
				case "zero":
					s.report.CurrentMaturity.DuePendingMinor = 0
					s.report.CurrentMaturity.DueDisputeHoldMinor = 0
				case "revoked":
					s.read = func() { auth.deny = true }
				case "rows":
					s.report.CurrentMaturity.DueAllocationRows = -1
				case "hold":
					s.report.CurrentMaturity.DueDisputeHoldMinor = 1001
				case "net":
					s.report.CurrentMaturity.DuePendingMinor = 2001
				case "total_hold":
					s.report.CurrentMaturity.DueDisputeHoldMinor = 801
				case "time":
					s.report.CurrentMaturity.OldestAvailableAt = nil
				case "future":
					oldest = at.Add(time.Second)
				case "false_zero":
					s.report.CurrentMaturity.DueAllocationRows = 0
					s.report.CurrentMaturity.OldestAvailableAt = nil
				}
				from, to := at, at.Add(time.Hour)
				if admin {
					out, err := m.AdminFinancialMetrics(context.Background(), "operator", "partner", partnerearnings.FinancialMetricsQuery{Limit: 1, From: &from, To: &to})
					require.ErrorIs(t, err, tc.want)
					if tc.want != nil {
						require.Empty(t, out)
					} else {
						require.Equal(t, s.report.CurrentMaturity, out.CurrentMaturity)
					}
				} else {
					out, err := m.FinancialSummary(context.Background(), "owner", FinancialSummaryQuery{From: &from, To: &to})
					require.ErrorIs(t, err, tc.want)
					if tc.want != nil {
						require.Empty(t, out)
						return
					}
					require.Equal(t, s.report.CurrentMaturity, out.CurrentMaturity)
					require.Nil(t, out.CohortCommission)
					require.Equal(t, "no_accepted_accrual", out.Acceptance)
					body, err := json.Marshal(out)
					require.NoError(t, err)
					require.Contains(t, string(body), "current_maturity")
					require.Contains(t, string(body), "due_allocation_rows")
					for _, forbidden := range []string{"partner_id", "program_id", "payment_id", "provider", "PlanID", "receipt", "actor"} {
						require.NotContains(t, string(body), forbidden)
					}
				}
			})
		}
	}
}
