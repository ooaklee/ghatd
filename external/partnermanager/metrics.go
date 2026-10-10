package partnermanager

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
)

// FinancialSummaryQuery uses one inclusive/exclusive date range for original
// accepted-payment cohorts and economic journal movements. Current balances,
// claim heads/maturity, remaining commission and processing exposure are unfiltered.
type FinancialSummaryQuery struct{ From, To *time.Time }

// FinancialSummary is a customer-safe commission aggregate. No billing amount,
// plan/provider/payment identity or payout reference is disclosed. Allocation
// rows are not unique invoices, subscriptions or paying referrals. No source or
// subscription completeness claim follows from an empty accepted journal.
type FinancialSummary struct {
	Currency                 string                              `json:"currency"`
	CurrencyExponent         int                                 `json:"currency_exponent"`
	From                     *time.Time                          `json:"from,omitempty"`
	To                       *time.Time                          `json:"to,omitempty"`
	FinancialAsOf            time.Time                           `json:"financial_as_of"`
	FinancialRevision        string                              `json:"financial_revision"`
	AcceptedAllocationRows   int                                 `json:"accepted_allocation_rows"`
	Acceptance               string                              `json:"acceptance"`
	CohortCommission         *ReferralCommission                 `json:"cohort_commission,omitempty"`
	PeriodMovements          partnerearnings.CommissionMovements `json:"period_movements"`
	CurrentPartnerBalances   partnerearnings.Balances            `json:"current_partner_balances"`
	CurrentClaims            partnerearnings.CurrentClaimMetrics `json:"current_claims"`
	CurrentMaturity          partnerearnings.CurrentMaturity     `json:"current_maturity"`
	RemainingCommissionMinor int64                               `json:"remaining_commission_minor"`
	SourceCoverage           string                              `json:"source_coverage"`
	SubscriptionCoverage     string                              `json:"subscription_coverage"`
}

// validFinancialQuery reports whether the owning query self-validates.
func validFinancialQuery(q partnerearnings.FinancialMetricsQuery) bool {
	return q.Validate() == nil
}

// financialMetrics fetches and strictly validates an owning financial report
// before projection: identity, currency, revision, pagination, non-negative
// amounts and counts, cohort/claim consistency, maturity bounded by balances,
// canonical ascending plan keys matching the filter, and cursor agreement with
// has_more. Any inconsistency yields ErrUnavailable rather than a distorted
// report.
func (m *Manager) financialMetrics(ctx context.Context, partner string, q partnerearnings.FinancialMetricsQuery) (partnerearnings.FinancialMetrics, error) {
	out, err := m.deps.Earnings.GetFinancialMetrics(ctx, partner, q)
	if err != nil {
		return partnerearnings.FinancialMetrics{}, err
	}
	if out.ProgramID != partnerprogram.ProgramID || out.PartnerID != partner || out.Currency != m.deps.Program.Config().Currency || out.Revision == "" || out.AsOf.IsZero() || out.LedgerSequence < 0 || out.AcceptedAllocationRows < 0 || out.RemainingCommissionMinor < 0 || len(out.Plans) > q.Limit || (out.HasMore && len(out.Plans) != q.Limit) || (!out.HasMore && out.NextAfterPlanKey != "") {
		return partnerearnings.FinancialMetrics{}, ErrUnavailable
	}
	if !validCohortAmounts(out.CohortAmounts) || !validMovementAmounts(out.PeriodMovements) || !validCurrentClaims(out.CurrentClaims) || (out.AcceptedAllocationRows == 0 && out.CohortAmounts != (partnerearnings.PaymentAmounts{})) {
		return partnerearnings.FinancialMetrics{}, ErrUnavailable
	}
	if out.CurrentMaturity.Validate(out.AsOf) != nil || out.CurrentMaturity.DuePendingMinor > out.CurrentPartnerBalances.PendingMinor || out.CurrentMaturity.DueDisputeHoldMinor > out.CurrentPartnerBalances.PendingDisputeHoldMinor {
		return partnerearnings.FinancialMetrics{}, ErrUnavailable
	}
	previous := q.AfterPlanKey
	for _, row := range out.Plans {
		key := "unspecified"
		if row.PlanID != "" {
			if !partnerearnings.IsCanonicalReportID(row.PlanID) {
				return partnerearnings.FinancialMetrics{}, ErrUnavailable
			}
			key = "plan:" + row.PlanID
		}
		if row.Key != key || row.Key <= previous || row.AcceptedAllocationRows < 0 || row.AcceptedAllocationRows > out.AcceptedAllocationRows || !validCohortAmounts(row.CohortAmounts) || !validMovementAmounts(row.PeriodMovements) || (row.AcceptedAllocationRows == 0 && row.CohortAmounts != (partnerearnings.PaymentAmounts{})) || (q.PlanID != "" && row.PlanID != q.PlanID) || (q.UnspecifiedPlanOnly && row.PlanID != "") {
			return partnerearnings.FinancialMetrics{}, ErrUnavailable
		}
		previous = row.Key
	}
	if out.HasMore && out.NextAfterPlanKey != previous {
		return partnerearnings.FinancialMetrics{}, ErrUnavailable
	}
	return out, nil
}

// validMovementAmounts requires every commission movement component to be non-
// negative.
func validMovementAmounts(a partnerearnings.CommissionMovements) bool {
	return a.AccruedMinor >= 0 && a.MaturedGrossMinor >= 0 && a.RefundReversedMinor >= 0 && a.DisputeLostMinor >= 0 && a.DisputeHeldMinor >= 0 && a.DisputeReleasedMinor >= 0 && a.PaidMinor >= 0 && a.ReturnedMinor >= 0
}

// validCurrentClaims requires non-negative claim counts and exposure, exposure
// exactly when claims are potentially sent, and a non-zero oldest-open
// timestamp exactly when open claims exist.
func validCurrentClaims(c partnerearnings.CurrentClaimMetrics) bool {
	if c.Requested < 0 || c.Processing < 0 || c.NeedsReview < 0 || c.Paid < 0 || c.Cancelled < 0 || c.Rejected < 0 || c.ProcessingExposureMinor < 0 {
		return false
	}
	potentiallySent := c.Processing > 0 || c.NeedsReview > 0
	open := c.Requested > 0 || potentiallySent
	return potentiallySent == (c.ProcessingExposureMinor > 0) && ((open && c.OldestOpenRequestedAt != nil && !c.OldestOpenRequestedAt.IsZero()) || (!open && c.OldestOpenRequestedAt == nil))
}

// FinancialSummary returns one owning financial snapshot to the verified
// current partner. Admission pauses preserve authorized earned-history reads.
func (m *Manager) FinancialSummary(ctx context.Context, actor string, q FinancialSummaryQuery) (FinancialSummary, error) {
	fq := partnerearnings.FinancialMetricsQuery{Limit: 1, From: q.From, To: q.To}
	if !validFinancialQuery(fq) {
		return FinancialSummary{}, ErrInvalid
	}
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return FinancialSummary{}, err
	}
	if p.ProgramID != partnerprogram.ProgramID {
		return FinancialSummary{}, ErrUnavailable
	}
	financial, err := m.financialMetrics(ctx, p.ID, fq)
	if err != nil {
		return FinancialSummary{}, err
	}
	current, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return FinancialSummary{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID {
		return FinancialSummary{}, ErrDenied
	}
	out := FinancialSummary{Currency: financial.Currency, CurrencyExponent: m.deps.Program.Config().CurrencyExponent, From: q.From, To: q.To, FinancialAsOf: financial.AsOf, FinancialRevision: financial.Revision, AcceptedAllocationRows: financial.AcceptedAllocationRows, Acceptance: "no_accepted_accrual", PeriodMovements: financial.PeriodMovements, CurrentPartnerBalances: financial.CurrentPartnerBalances, CurrentClaims: financial.CurrentClaims, CurrentMaturity: financial.CurrentMaturity, RemainingCommissionMinor: financial.RemainingCommissionMinor, SourceCoverage: "not_evaluated", SubscriptionCoverage: "not_evaluated"}
	if financial.AcceptedAllocationRows > 0 {
		out.Acceptance = "accepted_accruals"
		out.CohortCommission = commissionProjection(financial.CohortAmounts)
	}
	return out, nil
}

// commissionProjection converts owning PaymentAmounts into the public
// ReferralCommission aggregate, copying backing fields without exposing payer
// or transaction identity.
func commissionProjection(a partnerearnings.PaymentAmounts) *ReferralCommission {
	return &ReferralCommission{AccruedMinor: a.AccruedMinor, PendingEarnedMinor: a.PendingEarnedMinor, MaturedEarnedMinor: a.MaturedEarnedMinor, ReversedMinor: a.ReversedMinor, DisputeLostMinor: a.DisputeLostMinor, DisputeHoldMinor: a.DisputeHoldMinor, ReservedBackingMinor: a.ReservedBackingMinor, ReviewBackingMinor: a.ReviewBackingMinor, GrossPaidBackingMinor: a.GrossPaidBackingMinor, ReturnedBackingMinor: a.ReturnedBackingMinor, NetPaidBackingMinor: a.NetPaidBackingMinor}
}

// AdminFinancialMetrics returns a private domain report under selected-partner
// reporting permission. The host projects permitted plan/catalogue labels and
// financial fields; this is not a customer transport or export contract.
func (m *Manager) AdminFinancialMetrics(ctx context.Context, actor, partner string, q partnerearnings.FinancialMetricsQuery) (partnerearnings.FinancialMetrics, error) {
	if !partnerearnings.IsCanonicalReportID(partner) || !validFinancialQuery(q) {
		return partnerearnings.FinancialMetrics{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return partnerearnings.FinancialMetrics{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return partnerearnings.FinancialMetrics{}, err
	}
	if p.ID != partner || p.ProgramID != partnerprogram.ProgramID {
		return partnerearnings.FinancialMetrics{}, ErrUnavailable
	}
	out, err := m.financialMetrics(ctx, partner, q)
	if err != nil {
		return partnerearnings.FinancialMetrics{}, err
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return partnerearnings.FinancialMetrics{}, err
	}
	current, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return partnerearnings.FinancialMetrics{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID || current.CustomerID != p.CustomerID {
		return partnerearnings.FinancialMetrics{}, ErrDenied
	}
	return out, nil
}
