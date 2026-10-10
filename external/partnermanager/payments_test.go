package partnermanager

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

type paymentReportStub struct {
	EarningsService
	report    partnerearnings.PaymentReport
	err       error
	read      func()
	calls     int
	requested string
}

func (s *paymentReportStub) GetPaymentReport(_ context.Context, partner string, _ partnerearnings.PaymentQuery) (partnerearnings.PaymentReport, error) {
	s.calls++
	s.requested = partner
	if s.read != nil {
		s.read()
	}
	return s.report, s.err
}

// Audit disposition: named self/admin financial report scope, pagination and
// revoked-authority cases with fresh fixtures; this is a private domain view.
func TestManagerPaymentReports(t *testing.T) {
	cases := []struct {
		name, mode string
		want       error
	}{
		{name: "currently_authorized_owning_report"},
		{name: "paused_admission_preserves_read", mode: "paused"},
		{name: "denied_before_read", mode: "denied", want: ErrDenied},
		{name: "revoked_during_read", mode: "revoked", want: ErrDenied},
		{name: "joined_absence_and_outage", mode: "outage", want: ErrUnavailable},
		{name: "wrong_partner_result", mode: "partner", want: ErrUnavailable},
		{name: "wrong_program_result", mode: "program", want: ErrUnavailable},
		{name: "wrong_enrollment_program", mode: "enrollment", want: ErrUnavailable},
		{name: "wrong_currency", mode: "currency", want: ErrUnavailable},
		{name: "result_outside_referral_filter", mode: "filter", want: ErrUnavailable},
		{name: "result_outside_cursor", mode: "cursor", want: ErrUnavailable},
		{name: "duplicate_payment", mode: "duplicate", want: ErrUnavailable},
		{name: "false_more_cursor", mode: "more", want: ErrUnavailable},
		{name: "invalid_limit", mode: "limit", want: ErrInvalid},
	}
	for _, admin := range []bool{false, true} {
		prefix := "customer/"
		if admin {
			prefix = "operator/"
		}
		for _, tc := range cases {
			t.Run(prefix+tc.name, func(t *testing.T) {
				m, program, _, _, a, _, _ := managerFixture(t)
				at := m.deps.Clock.Now()
				s := &paymentReportStub{report: partnerearnings.PaymentReport{ProgramID: partnerprogram.ProgramID, PartnerID: "partner", Currency: "EUR", Revision: "revision", AsOf: at, LedgerSequence: 2, CohortPayments: 1, Items: []partnerearnings.PaymentEarning{{PaymentID: "payment", ReferralID: "referral", AccrualSequence: 1, OccurredAt: at, AvailableAt: at}}}}
				m.deps.Earnings = s
				q := partnerearnings.PaymentQuery{Limit: 1}
				switch tc.mode {
				case "paused":
					m.deps.Controls = Controls{}
				case "denied":
					a.deny = true
				case "revoked":
					s.read = func() { a.deny = true }
				case "outage":
					s.err = errors.Join(partnerearnings.ErrNotFound, ErrUnavailable)
				case "partner":
					s.report.PartnerID = "other"
				case "program":
					s.report.ProgramID = "other"
				case "enrollment":
					program.partner.ProgramID = "other"
				case "currency":
					s.report.Currency = "USD"
				case "filter":
					q.ReferralID = "other"
				case "cursor":
					q.BeforeAccrualSequence = 1
				case "duplicate":
					q.Limit, s.report.CohortPayments = 2, 2
					s.report.Items = append(s.report.Items, s.report.Items[0])
				case "more":
					s.report.HasMore, s.report.NextBeforeAccrualSequence = true, 2
					s.report.CohortPayments = 2
				case "limit":
					q.Limit = 101
				}
				var out partnerearnings.PaymentReport
				var err error
				if admin {
					out, err = m.AdminPayments(context.Background(), "operator", "partner", q)
				} else {
					out, err = m.Payments(context.Background(), "owner", q)
				}
				require.ErrorIs(t, err, tc.want)
				if tc.want != nil {
					require.Empty(t, out)
					if tc.mode == "denied" || tc.mode == "enrollment" || tc.mode == "limit" {
						require.Zero(t, s.calls)
					}
					return
				}
				require.Equal(t, s.report, out)
				require.Equal(t, "partner", s.requested)
				capability := CapabilitySelf
				if admin {
					capability = CapabilityReporting
				}
				require.Equal(t, []string{capability, capability}, a.calls)
			})
		}
	}
}
