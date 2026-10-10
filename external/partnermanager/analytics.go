package partnermanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

// referralAnalytics forwards the owning analytics read and fails as unavailable
// when the returned scope or validation does not match the requested partner
// and query.
func (m *Manager) referralAnalytics(ctx context.Context, partner string, q referral.AnalyticsQuery) (referral.Analytics, error) {
	out, err := m.deps.Referral.GetAnalytics(ctx, partner, q)
	if err != nil {
		return referral.Analytics{}, err
	}
	if out.ProgramID != partnerprogram.ProgramID || out.PartnerID != partner || out.Validate(q) != nil {
		return referral.Analytics{}, ErrUnavailable
	}
	return out, nil
}

// ReferralAnalytics returns only owning aggregate counts and the current
// partner's share links. It discloses no customer, signup, cookie or provider
// identity. Admission pauses preserve authorized earned-history/report reads.
func (m *Manager) ReferralAnalytics(ctx context.Context, actor string, q referral.AnalyticsQuery) (referral.Analytics, error) {
	if q.Validate() != nil {
		return referral.Analytics{}, ErrInvalid
	}
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return referral.Analytics{}, err
	}
	if p.ProgramID != partnerprogram.ProgramID {
		return referral.Analytics{}, ErrUnavailable
	}
	out, err := m.referralAnalytics(ctx, p.ID, q)
	if err != nil {
		return referral.Analytics{}, err
	}
	current, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return referral.Analytics{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID {
		return referral.Analytics{}, ErrDenied
	}
	return out, nil
}

// AdminReferralAnalytics requires current selected-partner reporting authority
// before and after the owning read. It does not confer cross-partner identity
// disclosure or financial/subscription source completeness.
func (m *Manager) AdminReferralAnalytics(ctx context.Context, actor, partner string, q referral.AnalyticsQuery) (referral.Analytics, error) {
	if !referral.IsCanonicalAnalyticsPartnerID(partner) || q.Validate() != nil {
		return referral.Analytics{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return referral.Analytics{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return referral.Analytics{}, err
	}
	if p.ID != partner || p.ProgramID != partnerprogram.ProgramID {
		return referral.Analytics{}, ErrUnavailable
	}
	out, err := m.referralAnalytics(ctx, partner, q)
	if err != nil {
		return referral.Analytics{}, err
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return referral.Analytics{}, err
	}
	current, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return referral.Analytics{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID || current.CustomerID != p.CustomerID {
		return referral.Analytics{}, ErrDenied
	}
	return out, nil
}
