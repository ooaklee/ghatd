package partnermanager

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

// relationshipEvidenceService is the narrow capability paid-referral metrics
// need: relationship evidence for one partner from the referral owner.
type relationshipEvidenceService interface {
	// GetRelationshipEvidence returns the referral RelationshipEvidence for one
	// partner, the narrow read paid-referral metrics need from the referral owner.
	GetRelationshipEvidence(context.Context, string) (referral.RelationshipEvidence, error)
}

// PaidReferralQuery selects the original-payment cohort [From,To). Signup and
// visit cohorts use separate analytics and must not share this denominator.
type PaidReferralQuery struct{ From, To *time.Time }

// validate rejects a cohort range whose bounds are zero or whose end is not
// strictly after the start with ErrInvalid; absent bounds are allowed.
func (q PaidReferralQuery) validate() error {
	if (q.From != nil && q.From.IsZero()) || (q.To != nil && q.To.IsZero()) || (q.From != nil && q.To != nil && !q.To.After(*q.From)) {
		return ErrInvalid
	}
	return nil
}

// PaidReferralReport covers the complete selected partner's lifetime retained
// relationship set, independently of list pagination. Renewals are allocations,
// never additional people. Original paid history and current net-positive
// revenue differ; current activity is known only for fresh scoped candidates.
// Raw payer/provider/payment identities and other owners are never disclosed.
type PaidReferralReport struct {
	Scope                 string             `json:"scope"`
	From                  *time.Time         `json:"from,omitempty"`
	To                    *time.Time         `json:"to,omitempty"`
	LifetimeRelationships int                `json:"lifetime_relationships"`
	CurrentRelationships  int                `json:"current_relationships"`
	RetainedRelationships int                `json:"retained_relationships"`
	AttributionAsOf       time.Time          `json:"attribution_as_of"`
	AttributionRevision   string             `json:"attribution_revision"`
	SourceCoverage        string             `json:"source_coverage"`
	SubscriptionCoverage  string             `json:"subscription_coverage"`
	Paid                  ReferralPaidTotals `json:"paid"`
}

// paidReferralReport builds the partner's complete lifetime paid report from
// relationship evidence, requiring revenue reporting configuration. It
// validates strict item ordering and capacity, computes paid evidence over the
// [From,To) cohort, then rereads the evidence and fails with ErrStaleWrite if
// the revision changed during billing/status reads; it claims no multi-owner
// transaction.
func (m *Manager) paidReferralReport(ctx context.Context, partner string, q PaidReferralQuery) (PaidReferralReport, error) {
	owner, ok := m.deps.Referral.(relationshipEvidenceService)
	if !ok || nilManagerDependency(owner) || m.revenueReporting == nil {
		return PaidReferralReport{}, ErrUnavailable
	}
	evidence, err := owner.GetRelationshipEvidence(ctx, partner)
	if err != nil {
		return PaidReferralReport{}, err
	}
	if evidence.ProgramID != referral.ProgramID || evidence.PartnerID != partner || evidence.Revision == "" || evidence.AsOf.IsZero() || evidence.Items == nil || len(evidence.Items) > referral.RelationshipEvidenceCapacity {
		return PaidReferralReport{}, ErrUnavailable
	}
	page := referral.RelationshipPage{ProgramID: referral.ProgramID, PartnerID: partner, Items: []referral.Relationship{}}
	joined := ReferralSummaryPage{Items: []CustomerReferralSummary{}}
	snapshots := []referral.AttributionSnapshot{}
	out := PaidReferralReport{Scope: "complete_partner_relationships", From: q.From, To: q.To, AttributionAsOf: evidence.AsOf, AttributionRevision: evidence.Revision, LifetimeRelationships: len(evidence.Items)}
	previous := ""
	for _, item := range evidence.Items {
		r := item.Relationship
		if r.ProgramID != referral.ProgramID || r.PartnerID != partner || r.ID != referral.RelationshipReferenceID(referral.ProgramID, partner, r.ReferredCustomer) || r.ID <= previous || len(r.Periods) == 0 {
			return PaidReferralReport{}, ErrUnavailable
		}
		previous = r.ID
		if r.Current {
			out.CurrentRelationships++
		} else {
			out.RetainedRelationships++
		}
		page.Items = append(page.Items, r)
		snapshots = append(snapshots, item.Attribution)
		joined.Items = append(joined.Items, CustomerReferralSummary{})
	}
	if err := m.calculateReferralPaidEvidence(ctx, partner, page, ReferralSummaryQuery{From: q.From, To: q.To}, snapshots, &joined); err != nil {
		return PaidReferralReport{}, err
	}
	// One COMPLETE recheck detects inserted memberships, new bindings and
	// corrections observed during independent billing/status reads. It does
	// not claim a multi-owner transaction or repeat per-customer guard writes.
	confirm, err := owner.GetRelationshipEvidence(ctx, partner)
	if err != nil {
		return PaidReferralReport{}, err
	}
	if confirm.ProgramID != evidence.ProgramID || confirm.PartnerID != evidence.PartnerID || confirm.Revision != evidence.Revision {
		return PaidReferralReport{}, referral.ErrStaleWrite
	}
	out.SourceCoverage, out.SubscriptionCoverage = joined.SourceCoverage, joined.SubscriptionCoverage
	if len(page.Items) == 0 {
		out.SourceCoverage = "no_partner_relationships"
	}
	out.Paid = *joined.PaidCoverage
	return out, ctx.Err()
}

// PaidReferralMetrics returns global paid-source aggregates for the verified
// current partner. Paused admission preserves authorized history reads.
func (m *Manager) PaidReferralMetrics(ctx context.Context, actor string, q PaidReferralQuery) (PaidReferralReport, error) {
	if err := q.validate(); err != nil {
		return PaidReferralReport{}, err
	}
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return PaidReferralReport{}, err
	}
	if p.ProgramID != partnerprogram.ProgramID {
		return PaidReferralReport{}, ErrUnavailable
	}
	out, err := m.paidReferralReport(ctx, p.ID, q)
	if err != nil {
		return PaidReferralReport{}, err
	}
	current, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return PaidReferralReport{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID || current.CustomerID != p.CustomerID {
		return PaidReferralReport{}, ErrDenied
	}
	return out, nil
}

// AdminPaidReferralMetrics requires selected-partner reporting authority before
// and after the complete report. It grants no identity disclosure or provider I/O.
func (m *Manager) AdminPaidReferralMetrics(ctx context.Context, actor, partner string, q PaidReferralQuery) (PaidReferralReport, error) {
	if !referral.IsCanonicalAnalyticsPartnerID(partner) || q.validate() != nil {
		return PaidReferralReport{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return PaidReferralReport{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return PaidReferralReport{}, err
	}
	if p.ID != partner || p.ProgramID != partnerprogram.ProgramID {
		return PaidReferralReport{}, ErrUnavailable
	}
	out, err := m.paidReferralReport(ctx, partner, q)
	if err != nil {
		return PaidReferralReport{}, err
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return PaidReferralReport{}, err
	}
	current, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return PaidReferralReport{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID || current.CustomerID != p.CustomerID {
		return PaidReferralReport{}, ErrDenied
	}
	return out, nil
}
