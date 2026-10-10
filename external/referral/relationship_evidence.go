package referral

import (
	"context"
	"time"
)

// RelationshipEvidenceCapacity bounds one complete partner read before any
// report filter. Memberships, heads, ownership revisions and bindings all count.
// Both service and adapter must count 2 + revisions + bindings per membership.
const RelationshipEvidenceCapacity = 10000

// RelationshipEvidenceRow is private repository evidence. Every selected
// lifetime membership, complete history and customer binding shares ONE read.
type RelationshipEvidenceRow struct {
	Relationship RelationshipSnapshot `json:"-"`
	Bindings     []PaymentAttribution `json:"-"`
}

// RelationshipEvidenceRepository is an optional capability of the SAME referral
// repository. A page, partial result or separately assembled customer reads
// cannot satisfy its complete owning snapshot contract.
type RelationshipEvidenceRepository interface {
	// ReadRelationshipEvidence reads the complete set of relationship evidence rows
	// for the partner and scope given by its two string arguments. The owning
	// repository requires a complete snapshot; pages or separately assembled reads
	// cannot satisfy this contract.
	ReadRelationshipEvidence(context.Context, string, string) ([]RelationshipEvidenceRow, error)
}

// RelationshipEvidenceItem retains only the selected owner's public periods;
// its complete attribution is private input to authorized manager reports.
type RelationshipEvidenceItem struct {
	Relationship Relationship        `json:"-"`
	Attribution  AttributionSnapshot `json:"-"`
}

// RelationshipEvidence is a complete bounded partner set, never a transport.
// AsOf is classification time after the read; Revision identifies retained
// ownership and binding evidence, not a billing/status/financial watermark.
type RelationshipEvidence struct {
	ProgramID, PartnerID string                     `json:"-"`
	Items                []RelationshipEvidenceItem `json:"-"`
	AsOf                 time.Time                  `json:"-"`
	Revision             string                     `json:"-"`
}

// GetRelationshipEvidence validates complete retained ownership and immutable
// payment bindings without correction guards, provider calls or business writes.
// Capacity, corruption, cancellation and read failure discard the entire set.
func (s *Service) GetRelationshipEvidence(ctx context.Context, partner string) (RelationshipEvidence, error) {
	if err := s.ready(ctx); err != nil {
		return RelationshipEvidence{}, err
	}
	if !correctionText(partner, 256) {
		return RelationshipEvidence{}, ErrInvalid
	}
	repo, ok := s.repo.(RelationshipEvidenceRepository)
	if !ok || nilReferralDependency(repo) {
		return RelationshipEvidence{}, ErrUnavailable
	}
	rows, err := repo.ReadRelationshipEvidence(ctx, ProgramID, partner)
	if err != nil {
		return RelationshipEvidence{}, err
	}
	return relationshipEvidenceFromRows(ctx, partner, rows, s.clock.Now().UTC())
}

// relationshipEvidenceFromRows validates a complete canonical owning set.
// A combined traffic/binding read uses this same fingerprint contract.
func relationshipEvidenceFromRows(ctx context.Context, partner string, rows []RelationshipEvidenceRow, at time.Time) (RelationshipEvidence, error) {
	if rows == nil {
		return RelationshipEvidence{}, ErrUnavailable
	}
	budget := RelationshipEvidenceCapacity
	for _, row := range rows {
		cost := 2 + len(row.Relationship.History) + len(row.Bindings)
		if cost > budget {
			return RelationshipEvidence{}, ErrCapacity
		}
		budget -= cost
	}
	if at.IsZero() {
		return RelationshipEvidence{}, ErrUnavailable
	}
	out := RelationshipEvidence{ProgramID: ProgramID, PartnerID: partner, AsOf: at, Items: []RelationshipEvidenceItem{}}
	proof := []string{ProgramID, partner}
	previous := ""
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return RelationshipEvidence{}, err
		}
		r := row.Relationship
		if r.ProgramID != ProgramID || r.PartnerID != partner || !correctionText(r.ReferredCustomer, 256) || r.ID != RelationshipReferenceID(ProgramID, partner, r.ReferredCustomer) || r.ID <= previous {
			return RelationshipEvidence{}, ErrUnavailable
		}
		previous = r.ID
		relationship, err := projectRelationship(r, partner)
		if err != nil {
			return RelationshipEvidence{}, err
		}
		attribution, err := validatedAttributionSnapshot(r.ReferredCustomer, r.Head, r.History, row.Bindings)
		if err != nil {
			return RelationshipEvidence{}, err
		}
		for _, revision := range attribution.History {
			if revision.LockedAt.After(at) || revision.PostedAt.After(at) {
				return RelationshipEvidence{}, ErrUnavailable
			}
		}
		for _, binding := range attribution.Bindings {
			if binding.BoundAt.After(at) {
				return RelationshipEvidence{}, ErrUnavailable
			}
		}
		out.Items = append(out.Items, RelationshipEvidenceItem{Relationship: relationship, Attribution: attribution})
		proof = append(proof, r.ID, r.FirstReferralID, attribution.Fingerprint)
	}
	var err error
	out.Revision, err = correctionDigest(proof)
	if err != nil {
		return RelationshipEvidence{}, err
	}
	if err := ctx.Err(); err != nil {
		return RelationshipEvidence{}, err
	}
	return out, nil
}
