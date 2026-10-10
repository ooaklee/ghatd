package partnerhttp

import (
	"strconv"
	"strings"

	"github.com/ooaklee/ghatd/external/partnerearnings"
)

// The owning receipt verifies actor/key/payload before this projection is made.
// Its returned claim is the current head, so an overlaid payment or current
// state cannot confirm an earlier command. Accepted evidence comes only from
// the immutable original payment or the caller's append-only audit record.
func operatorReceipt(c partnerearnings.Claim, accepted map[string]any) (Response, error) {
	if accepted == nil {
		return Response{}, partnerearnings.ErrUncertain
	}
	return reply(200, map[string]any{"claim": claimView(c, false), "accepted": accepted}, "")
}

// acceptedPayment confirms a record/observe command against the claim's current
// state: revision must exceed the expected one and the recorded payment or
// observation must match actor, state, amount, currency, trimmed
// method/reference and paid-at. It returns the durable acceptance receipt
// fields, or nil when no match confirms the command.
func acceptedPayment(c partnerearnings.Claim, r partnerearnings.RecordPaymentRequest) map[string]any {
	if c.ID != r.ClaimID || c.Revision <= r.ExpectedRevision {
		return nil
	}
	method, reference := strings.TrimSpace(r.Method), strings.TrimSpace(r.Reference)
	if r.State == partnerearnings.PaymentStateFull {
		p := c.Payment
		if p == nil || p.RecordedBy != r.ActorID || p.RecordedAt.IsZero() || p.State != r.State || p.AmountMinor != r.AmountMinor || p.Currency != r.Currency || p.Method != method || p.Reference != reference || !p.PaidAt.Equal(r.PaidAt) {
			return nil
		}
		return map[string]any{"operation": "record", "key": r.IdempotencyKey, "method": p.Method, "reference": p.Reference, "paid_at": p.PaidAt, "amount_minor": p.AmountMinor, "currency": p.Currency, "state": p.State, "recorded_at": p.RecordedAt}
	}
	for _, p := range c.PaymentObservations {
		if p.RecordedBy == r.ActorID && !p.RecordedAt.IsZero() && p.State == r.State && p.AmountMinor == r.AmountMinor && p.Currency == r.Currency && p.Method == method && p.Reference == reference && p.PaidAt.Equal(r.PaidAt) {
			return map[string]any{"operation": "observe", "key": r.IdempotencyKey, "method": p.Method, "reference": p.Reference, "paid_at": p.PaidAt, "amount_minor": p.AmountMinor, "currency": p.Currency, "state": p.State, "recorded_at": p.RecordedAt}
		}
	}
	return nil
}

// acceptedAmendment confirms an amendment command by matching an entry in the
// claim's PaymentAmendments on actor, idempotency key, trimmed
// method/reference/reason and paid-at, with revision advanced past the expected
// one. Returns the receipt map or nil when no amendment matches.
func acceptedAmendment(c partnerearnings.Claim, r partnerearnings.AmendPaymentRequest) map[string]any {
	if c.ID != r.ClaimID || c.Revision <= r.ExpectedRevision {
		return nil
	}
	for _, a := range c.PaymentAmendments {
		if a.By == r.ActorID && a.IdempotencyKey == r.IdempotencyKey && !a.At.IsZero() && a.Method == strings.TrimSpace(r.Method) && a.Reference == strings.TrimSpace(r.Reference) && a.PaidAt.Equal(r.PaidAt) && a.Reason == strings.TrimSpace(r.Reason) {
			return map[string]any{"operation": "amend", "key": a.IdempotencyKey, "method": a.Method, "reference": a.Reference, "paid_at": a.PaidAt, "reason": a.Reason, "recorded_at": a.At}
		}
	}
	return nil
}

// acceptedReturn confirms a returned-transfer command by matching a
// ReturnedAdjustments entry on actor, idempotency key, amount, currency,
// reference, returned-at and reason, with revision advanced past the expected
// one. Returns the receipt map or nil when no adjustment matches.
func acceptedReturn(c partnerearnings.Claim, r partnerearnings.ReturnRequest) map[string]any {
	if c.ID != r.ClaimID || c.Revision <= r.ExpectedRevision {
		return nil
	}
	for _, a := range c.ReturnedAdjustments {
		if a.By == r.ActorID && a.IdempotencyKey == r.IdempotencyKey && !a.At.IsZero() && a.AmountMinor == r.AmountMinor && a.Currency == r.Currency && a.Reference == r.Reference && a.ReturnedAt.Equal(r.ReturnedAt) && a.Reason == r.Reason {
			return map[string]any{"operation": "return", "key": a.IdempotencyKey, "amount_minor": a.AmountMinor, "currency": a.Currency, "reference": a.Reference, "returned_at": a.ReturnedAt, "reason": a.Reason, "recorded_at": a.At}
		}
	}
	return nil
}

// acceptedOperatorClaim confirms an on-behalf claim creation by matching the
// returned claim's partner, requester, reason, amount, non-zero requested-at
// and destination snapshot version against the submitted command. Returns the
// acceptance receipt or nil on any mismatch.
func acceptedOperatorClaim(c partnerearnings.Claim, actor, partner, reason, key string, amount, version int64) map[string]any {
	if c.PartnerID != partner || c.RequestedBy != actor || c.RequestedReason != reason || c.AmountMinor != amount || c.RequestedAt.IsZero() || c.DestinationSnapshot["version"] != strconv.FormatInt(version, 10) {
		return nil
	}
	return map[string]any{"operation": "create", "key": key, "partner_id": c.PartnerID, "reason": c.RequestedReason, "requested_at": c.RequestedAt}
}
