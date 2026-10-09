package partnerhttp

import (
	"context"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
)

func partnersPublishPolicy(ctx context.Context, svc *partnermanager.Manager, p Principal, req Request) (Response, error) {
	var input struct {
		Scope            string   `json:"scope"`
		GroupID          string   `json:"group_id"`
		PartnerCustomer  string   `json:"partner_customer"`
		Priority         int      `json:"priority"`
		RateBasisPoints  *int     `json:"rate_basis_points"`
		HoldDays         *int     `json:"hold_days"`
		Currency         string   `json:"currency"`
		EligiblePlanIDs  []string `json:"eligible_plan_ids"`
		EffectiveFrom    string   `json:"effective_from"`
		EffectiveTo      string   `json:"effective_to"`
		TermsVersion     string   `json:"terms_version"`
		RecurringMonths  *int     `json:"recurring_months"`
		ExpectedRevision *int64   `json:"expected_revision"`
	}
	if err := decode(req.Body, &input); err != nil {
		return Response{}, err
	}
	if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || input.RateBasisPoints == nil || input.HoldDays == nil {
		return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	at, err := partnerDate(input.EffectiveFrom)
	if err != nil {
		return Response{}, err
	}
	draft := partnerprogram.PolicyDraft{Scope: input.Scope, GroupID: input.GroupID, PartnerCustomer: input.PartnerCustomer, Priority: input.Priority, RateBasisPoints: *input.RateBasisPoints, HoldDays: *input.HoldDays, Currency: input.Currency, EligiblePlanIDs: input.EligiblePlanIDs, EffectiveFrom: at, TermsVersion: input.TermsVersion, RecurringMonths: input.RecurringMonths}
	if input.EffectiveTo != "" {
		to, err := partnerDate(input.EffectiveTo)
		if err != nil {
			return Response{}, err
		}
		draft.EffectiveTo = &to
	}
	version, err := svc.AdminPublishPolicy(ctx, partnerprogram.PublishPolicyRequest{ActorID: p.ActorID, Draft: draft, ExpectedRevision: *input.ExpectedRevision})
	if err != nil {
		return Response{}, err
	}
	return reply(201, policyView(version), "")
}

func partnersRecordPayment(ctx context.Context, svc *partnermanager.Manager, p Principal, req Request) (Response, error) {
	// V1 records a completed external transfer. The browser chooses neither the
	// actor, immutable claim amount/currency nor the recording timestamp. PaidAt
	// is the explicitly supplied actual transfer date; no wall-clock fallback.
	var input struct {
		ClaimID          string `json:"claim_id"`
		Method           string `json:"method"`
		Reference        string `json:"reference"`
		PaidAt           string `json:"paid_at"`
		ExpectedRevision int64  `json:"expected_revision"`
	}
	if err := decode(req.Body, &input); err != nil {
		return Response{}, err
	}
	at, err := partnerDate(input.PaidAt)
	if err != nil {
		return Response{}, err
	}
	claim, err := svc.AdminPaymentClaim(ctx, p.ActorID, input.ClaimID)
	if err != nil {
		return Response{}, err
	}
	command := partnerearnings.RecordPaymentRequest{ActorID: p.ActorID, ClaimID: claim.ID, Method: input.Method, Reference: input.Reference, PaidAt: at, AmountMinor: claim.AmountMinor, Currency: claim.Currency, State: partnerearnings.PaymentStateFull, ExpectedRevision: input.ExpectedRevision, IdempotencyKey: req.Key}
	result, err := svc.AdminRecordPayment(ctx, command)
	if err != nil {
		return Response{}, err
	}
	return operatorReceipt(result, acceptedPayment(result, command))
}

func partnersAttribution(ctx context.Context, svc *partnermanager.Manager, p Principal, req Request) (Response, error) {
	// Preview is a read model; Apply binds the identical prospective request and
	// original owning revisions/fingerprints. Neither rewrites past allocations.
	var input struct {
		PartnerID           string `json:"partner_id"`
		ReferredCustomer    string `json:"referred_customer"`
		SignupID            string `json:"signup_id"`
		Reason              string `json:"reason"`
		Mode                string `json:"mode"`
		ExpectedRevision    *int64 `json:"expected_revision"`
		ExpectedReferralID  string `json:"expected_referral_id"`
		SnapshotFingerprint string `json:"snapshot_fingerprint"`
		PreviewFingerprint  string `json:"preview_fingerprint"`
	}
	if err := decode(req.Body, &input); err != nil {
		return Response{}, err
	}
	change := partnermanager.AttributionChange{ActorID: p.ActorID, PartnerID: input.PartnerID, ReferredCustomer: input.ReferredCustomer, SignupID: input.SignupID, Reason: input.Reason, Mode: input.Mode}
	if req.Operation == "admin.partners.attribution.preview" {
		if input.ExpectedRevision != nil || input.ExpectedReferralID != "" || input.SnapshotFingerprint != "" || input.PreviewFingerprint != "" {
			return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
		}
		preview, err := svc.PreviewAttribution(ctx, change)
		if err != nil {
			return Response{}, err
		}
		return reply(200, map[string]any{"mode": preview.Mode, "partner_id": preview.PartnerID, "referred_customer": preview.ReferredCustomer, "signup_id": preview.SignupID, "reason": preview.Reason, "expected_revision": preview.Original.Revision, "expected_referral_id": preview.Original.ID, "snapshot_fingerprint": preview.SnapshotFingerprint, "preview_fingerprint": preview.Fingerprint, "proposed_terms": frozenTermsView(preview.ProposedTerms), "retained_binding_count": len(preview.UnchangedBindings), "historical_commission_delta_minor": preview.HistoricalCommissionDeltaMinor, "historical_payout_delta_minor": preview.HistoricalPayoutDeltaMinor, "as_of": preview.AsOf}, "")
	}
	if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
		return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	result, err := svc.ApplyAttribution(ctx, partnermanager.ApplyAttributionRequest{AttributionChange: change, ExpectedRevision: *input.ExpectedRevision, ExpectedReferralID: input.ExpectedReferralID, SnapshotFingerprint: input.SnapshotFingerprint, PreviewFingerprint: input.PreviewFingerprint, IdempotencyKey: req.Key})
	if err != nil {
		return Response{}, err
	}
	c := result.Correction
	if c == nil || result.CorrectionBy != p.ActorID || result.PartnerID != input.PartnerID || result.ReferredCustomer != input.ReferredCustomer || result.CorrectionReason != input.Reason || c.SignupID != input.SignupID || c.Mode != input.Mode || c.ExpectedRevision != *input.ExpectedRevision || c.ExpectedReferralID != input.ExpectedReferralID || c.SnapshotFingerprint != input.SnapshotFingerprint || c.PreviewFingerprint != input.PreviewFingerprint {
		return Response{}, partnerearnings.ErrUncertain
	}
	accepted := map[string]any{"operation": "attribution", "key": req.Key, "signup_id": c.SignupID, "reason": result.CorrectionReason, "mode": c.Mode, "expected_revision": c.ExpectedRevision, "expected_referral_id": c.ExpectedReferralID, "snapshot_fingerprint": c.SnapshotFingerprint, "preview_fingerprint": c.PreviewFingerprint}
	return reply(200, map[string]any{"referral": map[string]any{"id": result.ID, "partner_id": result.PartnerID, "referred_customer": result.ReferredCustomer, "revision": result.Revision, "locked_at": result.LockedAt, "terms": frozenTermsView(result.TermsSnapshot)}, "accepted": accepted}, "")
}

// Observations retain an unresolved or incomplete external attempt for review.
// This operation cannot settle: the full-payment state is not accepted here.
func partnersObservePayment(ctx context.Context, svc *partnermanager.Manager, p Principal, req Request) (Response, error) {
	var input struct {
		ClaimID             string `json:"claim_id"`
		Method              string `json:"method"`
		Reference           string `json:"reference"`
		PaidAt              string `json:"paid_at"`
		ObservedAmountMinor int64  `json:"observed_amount_minor"`
		ObservedCurrency    string `json:"observed_currency"`
		ObservedState       string `json:"observed_state"`
		ExpectedRevision    int64  `json:"expected_revision"`
	}
	if err := decode(req.Body, &input); err != nil {
		return Response{}, err
	}
	switch input.ObservedState {
	case partnerearnings.PaymentStateUnknown, partnerearnings.PaymentStatePartial, partnerearnings.PaymentStateMismatched:
	default:
		return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	at, err := partnerDate(input.PaidAt)
	if err != nil {
		return Response{}, err
	}
	claim, err := svc.AdminPaymentClaim(ctx, p.ActorID, input.ClaimID)
	if err != nil {
		return Response{}, err
	}
	command := partnerearnings.RecordPaymentRequest{ActorID: p.ActorID, ClaimID: claim.ID, Method: input.Method, Reference: input.Reference, PaidAt: at, AmountMinor: input.ObservedAmountMinor, Currency: input.ObservedCurrency, State: input.ObservedState, ExpectedRevision: input.ExpectedRevision, IdempotencyKey: req.Key}
	result, err := svc.AdminRecordPayment(ctx, command)
	if err != nil {
		return Response{}, err
	}
	return operatorReceipt(result, acceptedPayment(result, command))
}
