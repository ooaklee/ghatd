package partnerhttp

import (
	"context"

	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
)

// These preparation reads never borrow reporting/list permission. They bind
// the verified actor and selected path target; commands separately authorize
// their current action and retain independent original-intent recovery.
func partnersSelectedRead(ctx context.Context, svc *partnermanager.Manager, p Principal, req Request) (Response, error) {
	invalid := fail("PARTNERS_INVALID_REQUEST", 400)
	if req.ID == "" || len(req.Body) != 0 {
		return Response{}, invalid
	}
	capability := partnermanager.CapabilityPolicy
	switch req.Operation {
	case "admin.partners.claim.read":
		if len(req.Query) != 1 || len(req.Query["capability"]) != 1 {
			return Response{}, invalid
		}
		capability = req.Query["capability"][0]
		switch capability {
		case partnermanager.CapabilityProcessing, partnermanager.CapabilityRecordPayment, partnermanager.CapabilityAmendPayment, partnermanager.CapabilityReturnPayment:
		default:
			return Response{}, invalid
		}
	case "admin.partners.claim-preparation.read":
		capability = partnermanager.CapabilityCreateClaimOnBehalf
		if len(req.Query) != 0 {
			return Response{}, invalid
		}
	case "admin.partners.status.read", "admin.partners.policy.individual.read":
		if len(req.Query) != 0 {
			return Response{}, invalid
		}
	default:
		return Response{}, invalid
	}
	if !partneraccess.OperatorTargetValid(capability, req.ID) {
		return Response{}, invalid
	}
	switch req.Operation {
	case "admin.partners.claim.read":
		claim, err := svc.AdminClaimForAction(ctx, p.ActorID, capability, req.ID)
		if err != nil {
			return Response{}, err
		}
		// All four selected actions need the obligation and its safe payment
		// history. Actor/audit identities and unrelated partner data stay private.
		return reply(200, map[string]any{"capability": capability, "claim": claimView(claim, false), "assigned_to_you": claim.ProcessingActor == p.ActorID, "new_handling_paused": !svc.ManualHandlingAdmitted()}, "")
	case "admin.partners.status.read":
		partner, err := svc.AdminPartnerStatus(ctx, p.ActorID, req.ID)
		if err != nil {
			return Response{}, err
		}
		return reply(200, map[string]any{"id": partner.ID, "status": partner.Status, "revision": partner.Revision, "status_reason": partner.StatusReason, "enrolled_at": partner.EnrolledAt, "status_changed_at": partner.StatusChangedAt, "can_acquire_referrals": partner.CanAcquireReferrals, "can_accrue": partner.CanAccrue, "can_request_payouts": partner.CanRequestPayouts}, "")
	case "admin.partners.claim-preparation.read":
		view, err := svc.AdminClaimPreparation(ctx, p.ActorID, req.ID)
		if err != nil {
			return Response{}, err
		}
		var destination any
		if d := view.Destination; d != nil {
			destination = map[string]any{"id": d.ID, "method": d.Method, "email": d.Email, "version": d.Version}
		}
		return reply(200, map[string]any{"partner_id": req.ID, "currency": view.Currency, "currency_exponent": view.CurrencyExponent, "minimum_minor": view.MinimumMinor, "claims_enabled": view.ClaimsEnabled, "can_request_payouts": view.CanRequestPayouts, "identity_eligible": view.IdentityEligible, "destination": destination, "available_minor": view.AvailableMinor}, "")
	case "admin.partners.policy.individual.read":
		history, err := svc.AdminIndividualPolicyVersions(ctx, p.ActorID, req.ID)
		if err != nil {
			return Response{}, err
		}
		versions := make([]map[string]any, 0, len(history.Versions))
		for _, version := range history.Versions {
			versions = append(versions, selectedIndividualPolicyView(version))
		}
		return reply(200, map[string]any{"customer_id": req.ID, "revision": history.Revision, "versions": versions}, "")
	}
	return Response{}, invalid
}

// selectedIndividualPolicyView projects one individual policy version for a
// selected read. It preserves the nil-versus-empty distinction of eligible plan
// IDs (inherit versus exclude all) while omitting publisher identity, audit
// fields and group data.
func selectedIndividualPolicyView(v partnerprogram.PolicyVersion) map[string]any {
	// Nil plans mean inherit; an explicit empty list means exclude all. Preserve
	// that distinction without exposing publisher/audit identity or group data.
	var plans []string
	if v.EligiblePlanIDs != nil {
		plans = append([]string{}, v.EligiblePlanIDs...)
	}
	return map[string]any{"id": v.ID, "revision": v.Revision, "scope": v.Scope, "partner_customer": v.PartnerCustomer, "priority": v.Priority, "rate_basis_points": v.RateBasisPoints, "hold_days": v.HoldDays, "currency": v.Currency, "eligible_plan_ids": plans, "effective_from": v.EffectiveFrom, "effective_to": v.EffectiveTo, "published_at": v.PublishedAt, "terms_version": v.TermsVersion, "recurring_months": v.RecurringMonths}
}
