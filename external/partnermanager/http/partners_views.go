package partnerhttp

import (
	"net/url"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

// ProgramView contains disclosure and admission switches, not effective
// commercial terms. Effective terms require an approved, published policy.
type ProgramView struct {
	TermsVersion      string `json:"terms_version"`
	EnrollmentEnabled bool   `json:"enrollment_enabled"`
	ApprovalRequired  bool   `json:"approval_required"`
	Currency          string `json:"currency"`
	CurrencyExponent  int    `json:"currency_exponent"`
	MinimumMinor      int64  `json:"minimum_minor"`
	ClaimsEnabled     bool   `json:"claims_enabled"`
}

// Enumerate transport fields rather than serializing private owning records.
// Balances are already the owner's aggregate-only public projection.
func partnerView(p partnerprogram.Partner) map[string]any {
	return map[string]any{"id": p.ID, "status": p.Status, "enrolled_at": p.EnrolledAt, "accepted_terms_version": p.AcceptedTermsVersion, "can_acquire_referrals": p.CanAcquireReferrals, "can_accrue": p.CanAccrue, "can_request_payouts": p.CanRequestPayouts, "revision": p.Revision}
}

// destinationView projects the payout destination's public fields: ID, method,
// email, version and creation time.
func destinationView(d partnerprogram.Destination) map[string]any {
	return map[string]any{"id": d.ID, "method": d.Method, "email": d.Email, "version": d.Version, "created_at": d.CreatedAt}
}

// termsView projects effective terms, converting hold duration to whole days
// and exposing which policy sources resolved the rate and hold.
func termsView(t partnerprogram.EffectiveTerms) map[string]any {
	return map[string]any{"rate_basis_points": t.RateBasisPoints, "hold_duration_days": int(t.HoldDuration / (24 * time.Hour)), "currency": t.Currency, "currency_exponent": t.CurrencyExponent, "terms_version": t.TermsVersion, "recurrence_ends_at": t.RecurrenceEndsAt, "resolved_at": t.ResolvedAt, "rate_source": t.RateSource.Kind, "hold_source": t.HoldSource.Kind}
}

// frozenTermsView projects a locked terms snapshot: rate, hold days, currency
// with exponent, terms version and recurrence end. It carries no policy version
// identifiers.
func frozenTermsView(t referral.TermsSnapshot) map[string]any {
	return map[string]any{"rate_basis_points": t.RateBasisPoints, "hold_duration_days": t.HoldDurationDays, "currency": t.Currency, "currency_exponent": t.CurrencyExponent, "terms_version": t.TermsVersion, "recurrence_ends_at": t.RecurrenceEndsAt}
}

// linkView projects a referral link as its code, a share URL built from the
// configured referral base with the code path-escaped, creation time and
// retired flag.
func (m *Service) linkView(l referral.Link) map[string]any {
	return map[string]any{"code": l.Code, "url": m.referralBase + "/" + url.PathEscape(l.Code), "created_at": l.CreatedAt, "retired": l.RetiredAt != nil}
}

// paymentView projects a manual payment with its claim-relative version,
// returning nil for an absent payment so the field is omitted from views.
func paymentView(p *partnerearnings.ManualPayment, version int64) any {
	if p == nil {
		return nil
	}
	return map[string]any{"method": p.Method, "reference": p.Reference, "paid_at": p.PaidAt, "recorded_at": p.RecordedAt, "amount_minor": p.AmountMinor, "currency": p.Currency, "state": p.State, "version": version}
}

// claimView projects a claim for member or operator audiences. Payment details
// are overlaid with non-empty amendment fields and versioned by amendment
// count; destination comes from the claim's snapshot. Only operator projections
// include partner, requester, processing actor and reason fields.
func claimView(c partnerearnings.Claim, operator bool) map[string]any {
	var payment *partnerearnings.ManualPayment
	version := int64(0)
	if c.Payment != nil {
		copy := *c.Payment
		payment = &copy
		version = 1
		for _, a := range c.PaymentAmendments {
			if a.Method != "" {
				payment.Method = a.Method
			}
			if a.Reference != "" {
				payment.Reference = a.Reference
			}
			if !a.PaidAt.IsZero() {
				payment.PaidAt = a.PaidAt
			}
			version++
		}
	}
	amendments := make([]map[string]any, 0, len(c.PaymentAmendments))
	for _, a := range c.PaymentAmendments {
		amendments = append(amendments, map[string]any{"method": a.Method, "reference": a.Reference, "paid_at": a.PaidAt, "recorded_at": a.At})
	}
	observations := make([]map[string]any, 0, len(c.PaymentObservations))
	for _, o := range c.PaymentObservations {
		observations = append(observations, map[string]any{"state": o.State, "amount_minor": o.AmountMinor, "currency": o.Currency, "method": o.Method, "reference": o.Reference, "paid_at": o.PaidAt, "recorded_at": o.RecordedAt})
	}
	returns := make([]map[string]any, 0, len(c.ReturnedAdjustments))
	for _, r := range c.ReturnedAdjustments {
		returns = append(returns, map[string]any{"amount_minor": r.AmountMinor, "currency": r.Currency, "reference": r.Reference, "returned_at": r.ReturnedAt, "recorded_at": r.At})
	}
	out := map[string]any{"id": c.ID, "amount_minor": c.AmountMinor, "currency": c.Currency, "state": c.State, "destination": map[string]any{"id": c.DestinationID, "method": c.DestinationSnapshot["method"], "email": c.DestinationSnapshot["email"], "version": c.DestinationSnapshot["version"]}, "requested_at": c.RequestedAt, "updated_at": c.UpdatedAt, "revision": c.Revision, "payment": paymentView(payment, version), "payment_amendments": amendments, "payment_observations": observations, "returned_adjustments": returns}
	if operator {
		out["partner_id"] = c.PartnerID
		out["requested_by"] = c.RequestedBy
		out["requested_reason"] = c.RequestedReason
		out["processing_actor"] = c.ProcessingActor
		out["updated_by"] = c.UpdatedBy
		out["reason"] = c.Reason
		out["review_reason"] = c.ReviewReason
	}
	return out
}

// claimsView projects one bounded page of claims. A full page sets
// may_have_more and exposes the last row's ID as next_after; this permits
// another read but is not evidence that more rows exist.
func claimsView(rows []partnerearnings.Claim, limit int, operator bool) map[string]any {
	items := make([]map[string]any, 0, len(rows))
	for _, c := range rows {
		items = append(items, claimView(c, operator))
	}
	after := ""
	mayHaveMore := len(rows) == limit
	if mayHaveMore {
		after = rows[len(rows)-1].ID
	}
	// The owner exposes a bounded list, not an over-fetch count. A full page
	// permits a next read; it is not evidence that another row exists.
	return map[string]any{"claims": items, "limit": limit, "may_have_more": mayHaveMore, "next_after": after}
}

// entryView projects one ledger entry, including sequence, amounts, commission,
// applied rate/hold and terms version.
func entryView(e partnerearnings.Entry) map[string]any {
	return map[string]any{"id": e.ID, "sequence": e.Sequence, "kind": e.Kind, "amount_minor": e.AmountMinor, "currency": e.Currency, "created_at": e.CreatedAt, "occurred_at": e.OccurredAt, "available_at": e.AvailableAt, "commission_minor": e.CommissionMinor, "rate_basis_points": e.RateBasisPoints, "hold_duration_days": int(e.HoldDuration / (24 * time.Hour)), "terms_version": e.TermsVersion}
}

// statementView projects a statement, decorating each entry with its running
// matured balance and versioned payment, plus page cursor fields.
func statementView(s partnerearnings.Statement) map[string]any {
	entries := make([]map[string]any, 0, len(s.Lines))
	for _, l := range s.Lines {
		row := entryView(l.Entry)
		row["running_matured_minor"] = l.RunningMaturedMinor
		row["payment"] = paymentView(l.Payment, l.PaymentVersion)
		entries = append(entries, row)
	}
	return map[string]any{"currency": s.Currency, "revision": s.Revision, "as_of": s.AsOf, "ledger_sequence": s.LedgerSequence, "balances": s.Balances, "entries": entries, "has_more": s.HasMore, "next_before_sequence": s.NextBeforeSequence}
}

// policyView projects a policy version including group, publisher and audit
// identity; it copies the eligible plan list so callers cannot mutate the
// source. Unlike the individual selected view, it is for operator/policy
// audiences.
func policyView(v partnerprogram.PolicyVersion) map[string]any {
	var plans []string
	if v.EligiblePlanIDs != nil {
		plans = append([]string{}, v.EligiblePlanIDs...)
	}
	return map[string]any{"id": v.ID, "revision": v.Revision, "scope": v.Scope, "group_id": v.GroupID, "partner_customer": v.PartnerCustomer, "priority": v.Priority, "rate_basis_points": v.RateBasisPoints, "hold_days": v.HoldDays, "currency": v.Currency, "eligible_plan_ids": plans, "effective_from": v.EffectiveFrom, "effective_to": v.EffectiveTo, "published_at": v.PublishedAt, "published_by": v.PublishedBy, "terms_version": v.TermsVersion, "recurring_months": v.RecurringMonths}
}
