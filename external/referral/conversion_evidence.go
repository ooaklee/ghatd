package referral

import (
	"context"
	"sort"
)

// ConversionSnapshot keeps the existing analytics revision input unchanged.
// Bindings are complete per-customer evidence from the SAME native read;
// absent customer keys are not proof of no bindings.
type ConversionSnapshot struct {
	Analytics AnalyticsSnapshot               `json:"-"`
	Bindings  map[string][]PaymentAttribution `json:"-"`
}

// ConversionSnapshotRepository is an optional capability of the SAME referral
// repository. Combined links+members+heads+history+bindings fit 10,000 rows;
// day buckets and partial-day raw evidence each have independent 10,000 budgets.
type ConversionSnapshotRepository interface {
	// ReadConversionSnapshot reads the complete conversion evidence snapshot for
	// the partner and scope identified by the two string arguments under the
	// analytics query. The owning repository treats it as one complete snapshot
	// within its documented row budgets, not a page or partial result.
	ReadConversionSnapshot(context.Context, string, string, AnalyticsQuery) (ConversionSnapshot, error)
}

// ConversionEvidence is private input to authorized manager reporting. Traffic
// and attribution share one owning read; billing remains an independent owner.
// Revision combines their canonical revisions without changing either contract.
type ConversionEvidence struct {
	Analytics     Analytics            `json:"-"`
	Relationships RelationshipEvidence `json:"-"`
	Links         []Link               `json:"-"` // complete validated set before pagination
	Revision      string               `json:"-"`
}

// GetConversionEvidence validates complete traffic/ownership/binding evidence.
// It performs no correction guards, billing/provider calls or business writes.
// Partial-day raw loss remains ErrGranularity; no rate is guessed from totals.
func (s *Service) GetConversionEvidence(ctx context.Context, partner string, q AnalyticsQuery) (ConversionEvidence, error) {
	if err := s.ready(ctx); err != nil {
		return ConversionEvidence{}, err
	}
	if !analyticsID(partner, 256) || q.Validate() != nil {
		return ConversionEvidence{}, ErrInvalid
	}
	repo, ok := s.repo.(ConversionSnapshotRepository)
	if !ok || nilReferralDependency(repo) {
		return ConversionEvidence{}, ErrUnavailable
	}
	snapshot, err := repo.ReadConversionSnapshot(ctx, ProgramID, partner, q)
	if err != nil {
		return ConversionEvidence{}, err
	}
	if snapshot.Bindings == nil || len(snapshot.Bindings) != len(snapshot.Analytics.Relationships) {
		return ConversionEvidence{}, ErrUnavailable
	}
	budget := RelationshipEvidenceCapacity - len(snapshot.Analytics.Links)
	rows := []RelationshipEvidenceRow{}
	for _, member := range snapshot.Analytics.Relationships {
		bindings, exists := snapshot.Bindings[member.ReferredCustomer]
		if !exists || bindings == nil {
			return ConversionEvidence{}, ErrUnavailable
		}
		cost := 2 + len(member.History) + len(bindings)
		if cost > budget {
			return ConversionEvidence{}, ErrCapacity
		}
		budget -= cost
		rows = append(rows, RelationshipEvidenceRow{Relationship: member, Bindings: bindings})
	}
	if budget < 0 {
		return ConversionEvidence{}, ErrCapacity
	}
	at := s.clock.Now().UTC()
	analytics, err := analyticsFromSnapshot(ctx, partner, q, snapshot.Analytics, at)
	if err != nil {
		return ConversionEvidence{}, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Relationship.ID < rows[j].Relationship.ID })
	relationships, err := relationshipEvidenceFromRows(ctx, partner, rows, at)
	if err != nil {
		return ConversionEvidence{}, err
	}
	if analytics.LifetimeRelationships != len(relationships.Items) {
		return ConversionEvidence{}, ErrUnavailable
	}
	revision, err := correctionDigest([]string{ProgramID, partner, analytics.Revision, relationships.Revision})
	if err != nil {
		return ConversionEvidence{}, err
	}
	return ConversionEvidence{Analytics: analytics, Relationships: relationships, Links: append([]Link{}, snapshot.Analytics.Links...), Revision: revision}, nil
}
