package partnermanager

import (
	"context"
	"errors"
)

// ErrIneligible means current commercial admission was assessed and refused.
// Dependency failures must never be converted into this definitive decision.
var ErrIneligible = errors.New("partnermanager/acquisition-ineligible")

// AcquisitionEligibility supplies current host-owned commercial eligibility
// for an existing customer's enrollment and new referrals. It is actor-free so
// signup workers do not borrow a member session or an operator's authority.
// Implementations may verify paid subscription evidence, but must never infer
// payment from a role, browser flag or mere trial-inclusive access status.
type AcquisitionEligibility interface {
	// CanAcquirePartnerReferrals assesses the selected customer's current
	// eligibility. False with nil error is a known refusal; an outage is an error.
	CanAcquirePartnerReferrals(context.Context, string) (bool, error)
}

// AcquisitionEligible returns the verified member's current commercial
// admission without requiring existing enrollment. Retained balances and claims
// do not use this gate. Authority is checked again after eligibility I/O.
func (m *Manager) AcquisitionEligible(ctx context.Context, actor string) (bool, error) {
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return false, err
	}
	principal, err := m.deps.Identity.GetPartnerPrincipal(ctx, actor)
	if err != nil {
		return false, err
	}
	if principal.ID != actor || !principal.Active || !principal.Individual || !principal.EmailVerified {
		return false, ErrDenied
	}
	eligible, err := m.acquisitionEligible(ctx, actor)
	if err != nil {
		return false, err
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return false, err
	}
	return eligible, nil
}

// MaximumHoldDays exposes the owning programme's limit for new policy drafts.
// It does not bound retained financial history or expose policy versions.
func (m *Manager) MaximumHoldDays() int { return m.deps.Program.Config().MaximumHoldDays() }

// acquisitionEligible centralizes optional admission wiring for both human and
// worker facades. Nil preserves hosts that deliberately have no extra policy;
// RequireAcquisitionEligibility makes missing wiring a startup failure.
func (m *Manager) acquisitionEligible(ctx context.Context, customer string) (bool, error) {
	if ctx == nil || m == nil || customer == "" {
		return false, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if m.deps.AcquisitionEligibility == nil {
		return !m.deps.RequireAcquisitionEligibility, nil
	}
	eligible, err := m.deps.AcquisitionEligibility.CanAcquirePartnerReferrals(ctx, customer)
	if canceled := ctx.Err(); canceled != nil {
		return false, canceled
	}
	if err != nil {
		return false, err
	}
	return eligible, nil
}

// requireAcquisition maps only a successfully assessed refusal to ErrIneligible.
func (m *Manager) requireAcquisition(ctx context.Context, customer string) error {
	eligible, err := m.acquisitionEligible(ctx, customer)
	if err != nil {
		return err
	}
	if !eligible {
		return ErrIneligible
	}
	return nil
}
