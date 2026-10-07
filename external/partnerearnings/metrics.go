package partnerearnings

import (
	"context"
	"math/big"
	"sort"
	"strings"
	"time"
)

// FinancialMetricsQuery selects the original billing plan and one date range.
// The range has two explicit uses: original-payment cohort/current status and
// original economic journal movements. Neither filters current global balances
// or claim heads/maturity. Plan breakdown pagination does not narrow aggregate totals.
type FinancialMetricsQuery struct {
	Limit               int
	AfterPlanKey        string
	PlanID              string
	UnspecifiedPlanOnly bool
	From, To            *time.Time
}

// CommissionMovements sums original economic journal movements, not current
// cohort status or amended payment presentation dates. Payout/return amounts
// use exact original allocation portions, with the paid/return operation's
// economic date rather than the settlement row's later recording time.
type CommissionMovements struct {
	AccruedMinor         int64 `json:"accrued_minor"`
	MaturedGrossMinor    int64 `json:"matured_gross_minor"`
	RefundReversedMinor  int64 `json:"refund_reversed_minor"`
	DisputeLostMinor     int64 `json:"dispute_lost_minor"`
	DisputeHeldMinor     int64 `json:"dispute_held_minor"`
	DisputeReleasedMinor int64 `json:"dispute_released_minor"`
	PaidMinor            int64 `json:"paid_minor"`
	ReturnedMinor        int64 `json:"returned_minor"`
}

// CurrentClaimMetrics describes unfiltered current claim heads. State counts
// are not claims created, paid or returned during the selected date range.
type CurrentClaimMetrics struct {
	Requested   int `json:"requested"`
	Processing  int `json:"processing"`
	NeedsReview int `json:"needs_review"`
	Paid        int `json:"paid"`
	Cancelled   int `json:"cancelled"`
	Rejected    int `json:"rejected"`
	// ProcessingExposureMinor includes potentially sent processing/review
	// reservations, even when a later refund removes their original credit.
	ProcessingExposureMinor int64      `json:"processing_exposure_minor"`
	OldestOpenRequestedAt   *time.Time `json:"oldest_open_requested_at,omitempty"`
}

// PlanFinancialMetrics is a private domain breakdown. Empty PlanID means
// unspecified provenance; it is never inferred from current plans.
type PlanFinancialMetrics struct {
	Key, PlanID            string
	AcceptedAllocationRows int
	CohortAmounts          PaymentAmounts
	PeriodMovements        CommissionMovements
}

// FinancialMetrics keeps accepted-payment cohorts, period movements and
// unfiltered current obligations separate. Plans is a bounded private page;
// aggregate totals always cover the full selected plan/date scope.
type FinancialMetrics struct {
	ProgramID, PartnerID, Currency, Revision string
	AsOf                                     time.Time
	LedgerSequence                           int64
	AcceptedAllocationRows                   int
	CohortAmounts                            PaymentAmounts
	PeriodMovements                          CommissionMovements
	CurrentPartnerBalances                   Balances
	CurrentClaims                            CurrentClaimMetrics
	CurrentMaturity                          CurrentMaturity
	// RemainingCommissionMinor is pending plus positive net matured credit.
	// Debt and potentially sent exposure remain separate; reservations/holds
	// are parts of this obligation, not additional commission added to it.
	RemainingCommissionMinor int64
	Plans                    []PlanFinancialMetrics
	HasMore                  bool
	NextAfterPlanKey         string
}

// IsCanonicalReportID checks the exact nonempty report identifier contract:
// at most 256 UTF-8 bytes, no control characters or surrounding whitespace.
// Managers can reject malformed partner/plan filters before an owning read.
func IsCanonicalReportID(id string) bool {
	cleaned, ok := cleanPlain(id, maxIDLength)
	return ok && cleaned == id
}

func canonicalMetricID(id string) bool { return IsCanonicalReportID(id) }

// Validate checks range, plan selection and cursor shape without reading data.
// Cursor membership still requires the selected owning snapshot.
func (q FinancialMetricsQuery) Validate() error {
	if !validFinancialMetricsQuery(q) {
		return ErrInvalid
	}
	return nil
}
func financialPlanKey(plan string) string {
	if plan == "" {
		return "unspecified"
	}
	return "plan:" + plan
}
func validPlanKey(key string) bool {
	return key == "unspecified" || (strings.HasPrefix(key, "plan:") && canonicalMetricID(strings.TrimPrefix(key, "plan:")))
}
func validFinancialMetricsQuery(q FinancialMetricsQuery) bool {
	return q.Limit >= 1 && q.Limit <= 100 && (q.PlanID == "" || canonicalMetricID(q.PlanID)) && !(q.PlanID != "" && q.UnspecifiedPlanOnly) && (q.AfterPlanKey == "" || validPlanKey(q.AfterPlanKey)) && (q.From == nil || !q.From.IsZero()) && (q.To == nil || !q.To.IsZero()) && (q.From == nil || q.To == nil || q.To.After(*q.From))
}
func metricDateMatches(at time.Time, q FinancialMetricsQuery) bool {
	return (q.From == nil || !at.Before(*q.From)) && (q.To == nil || at.Before(*q.To))
}
func metricPlanMatches(plan string, q FinancialMetricsQuery) bool {
	return (q.PlanID == "" || plan == q.PlanID) && (!q.UnspecifiedPlanOnly || plan == "")
}

// GetFinancialMetrics reads one complete owning snapshot. AcceptedAllocationRows
// counts commission accrual allocations, not invoices, subscribers or paying
// referrals. It makes no claim of billing-source/worker/subscription completeness.
// A host must label those separate owning measures and project customer fields.
func (s *Service) GetFinancialMetrics(ctx context.Context, partner string, q FinancialMetricsQuery) (FinancialMetrics, error) {
	if err := s.checkContext(ctx); err != nil {
		return FinancialMetrics{}, err
	}
	if !canonicalMetricID(partner) || !validFinancialMetricsQuery(q) {
		return FinancialMetrics{}, ErrInvalid
	}
	var out FinancialMetrics
	err := s.withPaymentSnapshot(ctx, partner, func(snapshot paymentSnapshot) error {
		out = FinancialMetrics{ProgramID: s.programID, PartnerID: partner, Currency: s.currency, Revision: snapshot.revision, AsOf: snapshot.asOf, LedgerSequence: snapshot.sequence, CurrentPartnerBalances: snapshot.balances, Plans: []PlanFinancialMetrics{}}
		remaining := big.NewInt(snapshot.balances.PendingMinor)
		if snapshot.balances.MatchedMinor > 0 {
			remaining.Add(remaining, big.NewInt(snapshot.balances.MatchedMinor))
		}
		var err error
		out.RemainingCommissionMinor, err = bigToInt64(remaining)
		if err != nil {
			return err
		}
		out.CurrentClaims, err = currentClaimMetrics(ctx, snapshot.claims)
		if err != nil {
			return err
		}
		out.CurrentMaturity, err = currentMaturityMetrics(ctx, snapshot)
		if err != nil {
			return err
		}
		plans := map[string]*PlanFinancialMetrics{}
		lots := map[string]PaymentEarning{}
		planRow := func(plan string) *PlanFinancialMetrics {
			key := financialPlanKey(plan)
			if plans[key] == nil {
				plans[key] = &PlanFinancialMetrics{Key: key, PlanID: plan}
			}
			return plans[key]
		}
		for _, lot := range snapshot.lots {
			if err := ctx.Err(); err != nil {
				return err
			}
			lots[lot.PaymentID] = lot
			if !metricPlanMatches(lot.PlanID, q) || !metricDateMatches(lot.OccurredAt, q) {
				continue
			}
			out.AcceptedAllocationRows++
			row := planRow(lot.PlanID)
			row.AcceptedAllocationRows++
			if err := addPaymentAmounts(&row.CohortAmounts, lot.Amounts); err != nil {
				return err
			}
			if err := addPaymentAmounts(&out.CohortAmounts, lot.Amounts); err != nil {
				return err
			}
		}
		paidAt, returnedAt := map[string]time.Time{}, map[string]time.Time{}
		for _, e := range snapshot.entries {
			if e.Kind == EntryPaid {
				paidAt[e.SourceEventID] = e.OccurredAt
			}
			if e.Kind == EntryReturned {
				returnedAt[e.SourceEventID] = e.OccurredAt
			}
		}
		for _, e := range snapshot.entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			payment, at := e.SourceRef, e.OccurredAt
			switch e.Kind {
			case EntryAccrued, EntryMatured:
				payment = e.SourceEventID
			case EntryReversed, EntryDisputeLost, EntryDisputeHold, EntryDisputeReleased, EntryDisputeWon:
			case EntryAllocationSettled:
				at = paidAt[e.SourceEventID]
			case EntryAllocationReleased:
				var returned bool
				at, returned = returnedAt[e.SourceEventID]
				if !returned {
					continue
				} // cancelled/rejected reservations are not returns
			default:
				continue
			}
			lot, exists := lots[payment]
			if !exists || at.IsZero() {
				return ErrConflict
			}
			if !metricPlanMatches(lot.PlanID, q) || !metricDateMatches(at, q) {
				continue
			}
			row := planRow(lot.PlanID)
			if err := addCommissionMovement(&row.PeriodMovements, e); err != nil {
				return err
			}
			if err := addCommissionMovement(&out.PeriodMovements, e); err != nil {
				return err
			}
		}
		keys := make([]string, 0, len(plans))
		for key := range plans {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if q.AfterPlanKey != "" && plans[q.AfterPlanKey] == nil {
			return ErrInvalid
		}
		for _, key := range keys {
			if q.AfterPlanKey != "" && key <= q.AfterPlanKey {
				continue
			}
			if len(out.Plans) == q.Limit {
				out.HasMore = true
				break
			}
			out.Plans = append(out.Plans, *plans[key])
		}
		if out.HasMore {
			out.NextAfterPlanKey = out.Plans[len(out.Plans)-1].Key
		}
		return nil
	})
	if err != nil {
		return FinancialMetrics{}, err
	}
	return out, nil
}

func addCommissionMovement(target *CommissionMovements, e Entry) error {
	var field *int64
	amount := big.NewInt(e.AmountMinor)
	switch e.Kind {
	case EntryAccrued:
		field = &target.AccruedMinor
	case EntryMatured:
		field = &target.MaturedGrossMinor
	case EntryReversed:
		field = &target.RefundReversedMinor
		amount.Neg(amount)
	case EntryDisputeLost:
		field = &target.DisputeLostMinor
		amount.Neg(amount)
	case EntryDisputeHold:
		field = &target.DisputeHeldMinor
	case EntryDisputeReleased, EntryDisputeWon:
		field = &target.DisputeReleasedMinor
	case EntryAllocationSettled:
		field = &target.PaidMinor
		amount.Neg(amount)
	case EntryAllocationReleased:
		field = &target.ReturnedMinor
		amount.Neg(amount)
	default:
		return ErrConflict
	}
	if amount.Sign() < 0 {
		return ErrConflict
	}
	return addSignedAmount(field, amount)
}

func currentClaimMetrics(ctx context.Context, claims []Claim) (CurrentClaimMetrics, error) {
	out := CurrentClaimMetrics{}
	for _, c := range claims {
		if err := ctx.Err(); err != nil {
			return CurrentClaimMetrics{}, err
		}
		open := false
		switch c.State {
		case ClaimRequested:
			out.Requested++
			open = true
		case ClaimProcessing:
			out.Processing++
			open = true
		case ClaimNeedsReview:
			out.NeedsReview++
			open = true
		case ClaimPaid:
			out.Paid++
		case ClaimCancelled:
			out.Cancelled++
		case ClaimRejected:
			out.Rejected++
		default:
			return CurrentClaimMetrics{}, ErrConflict
		}
		if c.State == ClaimProcessing || c.State == ClaimNeedsReview {
			if err := addSignedAmount(&out.ProcessingExposureMinor, big.NewInt(c.AmountMinor)); err != nil {
				return CurrentClaimMetrics{}, err
			}
		}
		if open {
			if c.RequestedAt.IsZero() {
				return CurrentClaimMetrics{}, ErrConflict
			}
			if out.OldestOpenRequestedAt == nil || c.RequestedAt.Before(*out.OldestOpenRequestedAt) {
				at := c.RequestedAt
				out.OldestOpenRequestedAt = &at
			}
		}
	}
	return out, nil
}
