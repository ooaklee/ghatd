package partnermanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
)

func (m *Manager) paymentReport(ctx context.Context, partner string, q partnerearnings.PaymentQuery) (partnerearnings.PaymentReport, error) {
	report, err := m.deps.Earnings.GetPaymentReport(ctx, partner, q)
	if err != nil {
		return partnerearnings.PaymentReport{}, err
	}
	if report.ProgramID != partnerprogram.ProgramID || report.PartnerID != partner || report.Currency != m.deps.Program.Config().Currency || report.Revision == "" || report.AsOf.IsZero() || report.LedgerSequence < 0 || report.CohortPayments < len(report.Items) || len(report.Items) > q.Limit || (report.HasMore && (len(report.Items) != q.Limit || report.CohortPayments <= len(report.Items))) || (!report.HasMore && report.NextBeforeAccrualSequence != 0) {
		return partnerearnings.PaymentReport{}, ErrUnavailable
	}
	seen := map[string]bool{}
	var previous int64
	first := true
	for _, lot := range report.Items {
		if lot.PaymentID == "" || seen[lot.PaymentID] || lot.AccrualSequence < 1 || lot.AccrualSequence > report.LedgerSequence || (!first && lot.AccrualSequence >= previous) || (q.BeforeAccrualSequence != 0 && lot.AccrualSequence >= q.BeforeAccrualSequence) || lot.OccurredAt.IsZero() || lot.AvailableAt.Before(lot.OccurredAt) || (q.ReferralID != "" && lot.ReferralID != q.ReferralID) || (q.From != nil && lot.OccurredAt.Before(*q.From)) || (q.To != nil && !lot.OccurredAt.Before(*q.To)) {
			return partnerearnings.PaymentReport{}, ErrUnavailable
		}
		first, previous, seen[lot.PaymentID] = false, lot.AccrualSequence, true
	}
	if report.HasMore && report.NextBeforeAccrualSequence != previous {
		return partnerearnings.PaymentReport{}, ErrUnavailable
	}
	return report, nil
}

// Payments is an authorized financial domain report for the verified owner.
// The host must aggregate/project customer-safe referral amounts rather than
// serialize internal payment, policy or invoice provenance directly.
func (m *Manager) Payments(ctx context.Context, actor string, q partnerearnings.PaymentQuery) (partnerearnings.PaymentReport, error) {
	if q.Limit < 1 || q.Limit > 100 {
		return partnerearnings.PaymentReport{}, ErrInvalid
	}
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return partnerearnings.PaymentReport{}, err
	}
	if p.ProgramID != partnerprogram.ProgramID {
		return partnerearnings.PaymentReport{}, ErrUnavailable
	}
	report, err := m.paymentReport(ctx, p.ID, q)
	if err != nil {
		return partnerearnings.PaymentReport{}, err
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return partnerearnings.PaymentReport{}, err
	}
	return report, nil
}

// AdminPayments requires reporting permission scoped to the selected partner,
// independently of claim-processing/payment-recording authority.
func (m *Manager) AdminPayments(ctx context.Context, actor, partner string, q partnerearnings.PaymentQuery) (partnerearnings.PaymentReport, error) {
	if !validWorkText(partner, 256) || q.Limit < 1 || q.Limit > 100 {
		return partnerearnings.PaymentReport{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return partnerearnings.PaymentReport{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return partnerearnings.PaymentReport{}, err
	}
	if p.ID != partner || p.ProgramID != partnerprogram.ProgramID {
		return partnerearnings.PaymentReport{}, ErrUnavailable
	}
	report, err := m.paymentReport(ctx, partner, q)
	if err != nil {
		return partnerearnings.PaymentReport{}, err
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return partnerearnings.PaymentReport{}, err
	}
	return report, nil
}
