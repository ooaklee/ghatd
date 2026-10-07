package partnermanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

// ReferralSummaryQuery pages retained relationships. From/To select original
// accepted-payment cohorts, not signup dates or period journal movements. No
// client-supplied ownership revision or financial grouping is accepted.
type ReferralSummaryQuery struct {
	Limit    int
	After    string
	From, To *time.Time
}

// ReferralCommission exposes aggregate commission provenance, never invoice
// amounts or provider/payment IDs. Backing amounts are not available funds.
type ReferralCommission struct {
	AccruedMinor          int64 `json:"accrued_minor"`
	PendingEarnedMinor    int64 `json:"pending_earned_minor"`
	MaturedEarnedMinor    int64 `json:"matured_earned_minor"`
	ReversedMinor         int64 `json:"reversed_minor"`
	DisputeLostMinor      int64 `json:"dispute_lost_minor"`
	DisputeHoldMinor      int64 `json:"dispute_hold_minor"`
	ReservedBackingMinor  int64 `json:"reserved_backing_minor"`
	ReviewBackingMinor    int64 `json:"review_backing_minor"`
	GrossPaidBackingMinor int64 `json:"gross_paid_backing_minor"`
	ReturnedBackingMinor  int64 `json:"returned_backing_minor"`
	NetPaidBackingMinor   int64 `json:"net_paid_backing_minor"`
}

type CustomerReferralSummary struct {
	CustomerReferral
	AcceptedPaymentRows int                   `json:"accepted_payment_rows"`
	Acceptance          string                `json:"acceptance"`
	ReviewRequired      bool                  `json:"review_required"`
	Commission          *ReferralCommission   `json:"commission,omitempty"`
	PaidEvidence        *ReferralPaidEvidence `json:"paid_evidence,omitempty"`
}

// ReferralSummaryPage reports only its visible relationships. Attribution
// Revision fingerprints this page, not the global relationship set. The reads
// are independent owning snapshots; no globally atomic view is claimed.
// Source/subscription coverage remains not_evaluated without explicit trusted
// WithRevenueReporting configuration on the same revenue owner.
type ReferralSummaryPage struct {
	Items                  []CustomerReferralSummary `json:"items"`
	HasMore                bool                      `json:"has_more"`
	NextAfter              string                    `json:"next_after,omitempty"`
	Scope                  string                    `json:"scope"`
	Currency               string                    `json:"currency"`
	CurrencyExponent       int                       `json:"currency_exponent"`
	From                   *time.Time                `json:"from,omitempty"`
	To                     *time.Time                `json:"to,omitempty"`
	AttributionObservedAt  time.Time                 `json:"attribution_observed_at"`
	AttributionRevision    string                    `json:"attribution_revision"`
	FinancialAsOf          time.Time                 `json:"financial_as_of"`
	FinancialRevision      string                    `json:"financial_revision"`
	CurrentPartnerBalances partnerearnings.Balances  `json:"current_partner_balances"`
	SourceCoverage         string                    `json:"source_coverage"`
	SubscriptionCoverage   string                    `json:"subscription_coverage"`
	PaidCoverage           *ReferralPaidTotals       `json:"paid_coverage,omitempty"`
}

func validReferralSummaryQuery(q ReferralSummaryQuery) bool {
	return q.Limit >= 1 && q.Limit <= 100 && (q.From == nil || !q.From.IsZero()) && (q.To == nil || !q.To.IsZero()) && (q.From == nil || q.To == nil || q.To.After(*q.From))
}

func relationshipPageRevision(page referral.RelationshipPage) (string, error) {
	data, err := json.Marshal(page)
	if err != nil {
		return "", ErrUnavailable
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (m *Manager) referralSummaries(ctx context.Context, partner string, q ReferralSummaryQuery) (ReferralSummaryPage, error) {
	rq := referral.RelationshipQuery{Limit: q.Limit, After: q.After}
	page, err := m.relationshipPage(ctx, partner, rq)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	revision, err := relationshipPageRevision(page)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	observedAt := m.deps.Clock.Now().UTC()
	fq := partnerearnings.ReferralAmountQuery{From: q.From, To: q.To}
	for _, item := range page.Items {
		group := partnerearnings.ReferralAmountGroup{ID: item.ID}
		for _, period := range item.Periods {
			group.ReferralIDs = append(group.ReferralIDs, period.ReferralID)
		}
		fq.Groups = append(fq.Groups, group)
	}
	financial, err := m.deps.Earnings.GetReferralAmounts(ctx, partner, fq)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	if financial.ProgramID != partnerprogram.ProgramID || financial.PartnerID != partner || financial.Currency != m.deps.Program.Config().Currency || financial.Revision == "" || financial.AsOf.IsZero() || financial.LedgerSequence < 0 || len(financial.Items) != len(page.Items) {
		return ReferralSummaryPage{}, ErrUnavailable
	}
	out := ReferralSummaryPage{Items: []CustomerReferralSummary{}, HasMore: page.HasMore, NextAfter: page.NextAfter, Scope: "visible_relationships", Currency: financial.Currency, CurrencyExponent: m.deps.Program.Config().CurrencyExponent, From: q.From, To: q.To, AttributionObservedAt: observedAt, AttributionRevision: revision, FinancialAsOf: financial.AsOf, FinancialRevision: financial.Revision, CurrentPartnerBalances: financial.Balances, SourceCoverage: "not_evaluated", SubscriptionCoverage: "not_evaluated"}
	for i, row := range financial.Items {
		if row.ID != page.Items[i].ID || row.AcceptedPayments < 0 || !validReferralAmounts(row, q) {
			return ReferralSummaryPage{}, ErrUnavailable
		}
		item := CustomerReferralSummary{CustomerReferral: projectCustomerReferral(page.Items[i]), AcceptedPaymentRows: row.AcceptedPayments, Acceptance: "no_accepted_accrual", ReviewRequired: row.ReviewRequired}
		if row.AcceptedPayments > 0 {
			item.Acceptance = "accepted_accruals"
			item.Commission = commissionProjection(row.Amounts)
		}
		out.Items = append(out.Items, item)
	}
	if m.revenueReporting != nil {
		if err := m.joinReferralPaidEvidence(ctx, partner, page, q, &out); err != nil {
			return ReferralSummaryPage{}, err
		}
	}
	// Append-only owning revisions make on-page correction/reacquisition
	// detectable. Return one bounded conflict rather than retry indefinitely.
	confirm, err := m.relationshipPage(ctx, partner, rq)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	confirmedRevision, err := relationshipPageRevision(confirm)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	if confirmedRevision != revision {
		return ReferralSummaryPage{}, referral.ErrStaleWrite
	}
	return out, nil
}

func validReferralAmounts(row partnerearnings.ReferralAmounts, q ReferralSummaryQuery) bool {
	a := row.Amounts
	if !validCohortAmounts(a) {
		return false
	}
	if row.AcceptedPayments == 0 {
		return a == (partnerearnings.PaymentAmounts{}) && row.FirstPaymentAt == nil && row.LastPaymentAt == nil && !row.ReviewRequired
	}
	return row.FirstPaymentAt != nil && row.LastPaymentAt != nil && !row.FirstPaymentAt.IsZero() && !row.LastPaymentAt.Before(*row.FirstPaymentAt) && (q.From == nil || !row.FirstPaymentAt.Before(*q.From)) && (q.To == nil || row.LastPaymentAt.Before(*q.To))
}

func validCohortAmounts(a partnerearnings.PaymentAmounts) bool {
	for _, amount := range []int64{a.OriginalRevenueMinor, a.RefundedRevenueMinor, a.AccruedMinor, a.PendingEarnedMinor, a.MaturedEarnedMinor, a.ReversedMinor, a.DisputeLostMinor, a.DisputeHoldMinor, a.ReservedBackingMinor, a.ReviewBackingMinor, a.GrossPaidBackingMinor, a.ReturnedBackingMinor, a.NetPaidBackingMinor} {
		if amount < 0 {
			return false
		}
	}
	return true
}

// ReferralSummaries joins only owning-service periods of the verified partner
// to one financial snapshot and rechecks current identity and enrollment after
// reads. Paused commercial admission does not hide retained earned history.
func (m *Manager) ReferralSummaries(ctx context.Context, actor string, q ReferralSummaryQuery) (ReferralSummaryPage, error) {
	if !validReferralSummaryQuery(q) {
		return ReferralSummaryPage{}, ErrInvalid
	}
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	if p.ProgramID != partnerprogram.ProgramID {
		return ReferralSummaryPage{}, ErrUnavailable
	}
	out, err := m.referralSummaries(ctx, p.ID, q)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	current, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID {
		return ReferralSummaryPage{}, ErrDenied
	}
	return out, nil
}

// AdminReferralSummaries uses explicit partner-scoped reporting authority and
// the same safe aggregate projection; broader identity disclosure is separate.
func (m *Manager) AdminReferralSummaries(ctx context.Context, actor, partner string, q ReferralSummaryQuery) (ReferralSummaryPage, error) {
	if !validWorkText(partner, 256) || !validReferralSummaryQuery(q) {
		return ReferralSummaryPage{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return ReferralSummaryPage{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	if p.ID != partner || p.ProgramID != partnerprogram.ProgramID {
		return ReferralSummaryPage{}, ErrUnavailable
	}
	out, err := m.referralSummaries(ctx, partner, q)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return ReferralSummaryPage{}, err
	}
	current, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return ReferralSummaryPage{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID || current.CustomerID != p.CustomerID {
		return ReferralSummaryPage{}, ErrDenied
	}
	return out, nil
}
