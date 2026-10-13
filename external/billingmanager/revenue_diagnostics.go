package billingmanager

import (
	"context"
	"github.com/ooaklee/ghatd/external/billing"
)

// Strict single-cause checks prevent a joined refusal/outage from being labelled
// solely as a refusal. Unknown failures retain an explicit unknown classification.
func revenueDiagnosticCause(err error) string {
	for _, rule := range []struct {
		err  error
		code string
	}{
		{context.Canceled, "cancelled"}, {context.DeadlineExceeded, "deadline"},
		{billing.ErrRevenueUncertain, "revenue_uncertain"}, {billing.ErrRevenueUnavailable, "revenue_unavailable"},
		{billing.ErrRevenueUnassessable, "revenue_unassessable"}, {billing.ErrRevenueConflict, "revenue_conflict"},
		{billing.ErrRevenueInvalid, "revenue_invalid"}, {billing.ErrRevenueNotFound, "revenue_not_found"},
	} {
		if singleRevenueError(err, rule.err) {
			return rule.code
		}
	}
	return "unclassified"
}

// QuarantineReason is stored/provider data, not a closed type. Never export an
// unregistered reason. Empty is distinct from evidence that has not been assessed.
func diagnosticQuarantineReason(reason string) string {
	switch reason {
	case "":
		return "none"
	case "payment_invoice_ambiguous", "original_payment_pending", "invoice_economics_unassessable", "adjustment_currency_mismatch", "partial_dispute_allocation_pending", "subscription_allocation_ambiguous", "original_economics_changed", "historical_payer_plan_pending", "refund_allocation_pending", "no_subscription_revenue", "payment_reference_missing":
		return reason
	default:
		return "unknown"
	}
}
