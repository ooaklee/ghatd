package partnerearnings

import (
	"context"
	"math/big"
	"time"
)

// CurrentMaturity describes unjournaled due accrual allocations in the complete
// current financial snapshot. It is unfiltered by plan, date or report page.
// Rows include zero/reversed credit; they count ledger work, never people.
type CurrentMaturity struct {
	DueAllocationRows int `json:"due_allocation_rows"`
	// DuePendingMinor is current net pending commission in the due rows, not
	// the original gross amount that Mature journals for each allocation.
	DuePendingMinor int64 `json:"due_pending_minor"`
	// DueDisputeHoldMinor is a subset of DuePendingMinor. Maturity does not release
	// this hold or establish claim availability; never add it to pending.
	DueDisputeHoldMinor int64      `json:"due_dispute_hold_minor"`
	OldestAvailableAt   *time.Time `json:"oldest_available_at,omitempty"`
}

// Validate checks a projection against its owning snapshot classification
// clock. Positive due rows may have zero net credit after refund or rounding.
func (m CurrentMaturity) Validate(asOf time.Time) error {
	if asOf.IsZero() || m.DueAllocationRows < 0 || m.DuePendingMinor < 0 || m.DueDisputeHoldMinor < 0 || m.DueDisputeHoldMinor > m.DuePendingMinor {
		return ErrConflict
	}
	if m.DueAllocationRows == 0 {
		if m.DuePendingMinor != 0 || m.DueDisputeHoldMinor != 0 || m.OldestAvailableAt != nil {
			return ErrConflict
		}
	} else if m.OldestAvailableAt == nil || m.OldestAvailableAt.IsZero() || m.OldestAvailableAt.After(asOf) {
		return ErrConflict
	}
	return nil
}

// currentMaturityMetrics consumes only already-validated owning payment lots.
// Missing horizons are corruption rejected by paymentEarnings, not a legacy
// cohort to guess. Dispute holds do not exclude a due maturity journal movement.
func currentMaturityMetrics(ctx context.Context, snapshot paymentSnapshot) (CurrentMaturity, error) {
	var out CurrentMaturity
	for _, lot := range snapshot.lots {
		if err := ctx.Err(); err != nil {
			return CurrentMaturity{}, err
		}
		if lot.Matured || lot.AvailableAt.After(snapshot.asOf) {
			continue
		}
		out.DueAllocationRows++
		if err := addSignedAmount(&out.DuePendingMinor, big.NewInt(lot.Amounts.PendingEarnedMinor)); err != nil {
			return CurrentMaturity{}, err
		}
		if err := addSignedAmount(&out.DueDisputeHoldMinor, big.NewInt(lot.Amounts.DisputeHoldMinor)); err != nil {
			return CurrentMaturity{}, err
		}
		if out.OldestAvailableAt == nil || lot.AvailableAt.Before(*out.OldestAvailableAt) {
			at := lot.AvailableAt.UTC()
			out.OldestAvailableAt = &at
		}
	}
	if out.Validate(snapshot.asOf) != nil || out.DuePendingMinor > snapshot.balances.PendingMinor || out.DueDisputeHoldMinor > snapshot.balances.PendingDisputeHoldMinor {
		return CurrentMaturity{}, ErrConflict
	}
	return out, nil
}
