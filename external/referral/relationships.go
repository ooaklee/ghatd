package referral

import (
	"context"
	"reflect"
	"sort"
	"time"
)

// RelationshipSnapshot is repository evidence, not a customer response. The
// immutable first-owned reference survives corrections away and reacquisition.
// Its current head and complete ownership history must share one read snapshot.
type RelationshipSnapshot struct {
	ID, ProgramID, PartnerID, ReferredCustomer, FirstReferralID string
	Head                                                        Referral
	History                                                     []Referral
}

// RelationshipReferenceID is the stable opaque membership/cursor identity.
// Its namespace includes the partner so different partners cannot correlate a
// referred customer from the public identifier alone.
func RelationshipReferenceID(program, partner, customer string) string {
	v, _ := correctionDigest([]string{"partner-referral-relationship", program, partner, customer})
	return v
}

// RelationshipQuery pages relationships. After is an opaque membership cursor
// from RelationshipPage.NextAfter; Limit controls page size and is otherwise
// interpreted by the repository.
type RelationshipQuery struct {
	Limit int
	After string
}

// OwnershipPeriod retains only the selected partner's attribution revision.
// Until is the next cutover, exclusive; Current means this was the head at the
// owning read snapshot. Already-frozen payments may still use an older period.
type OwnershipPeriod struct {
	ReferralID string
	From       time.Time
	Until      *time.Time
	Terms      TermsSnapshot
	Corrected  bool
}

// Relationship contains no other owner's identity, source code or raw reason.
// ReferredCustomer is internal owning-service input for privileged reporting;
// customer transports must use the partner-scoped opaque ID instead.
type Relationship struct {
	ID, ProgramID, PartnerID string
	ReferredCustomer         string `json:"-"`
	FirstOwnedAt             time.Time
	Current                  bool
	Periods                  []OwnershipPeriod
}

// RelationshipPage is one page of a partner's relationships plus continuation
// evidence. HasMore indicates further pages exist; NextAfter is the cursor for
// the next request. ProgramID and PartnerID echo the owning partition.
type RelationshipPage struct {
	ProgramID, PartnerID string
	Items                []Relationship
	HasMore              bool
	NextAfter            string
}

// ListRelationships retains former owners' referral visibility without
// exposing another owner's revisions. Pagination is by immutable membership
// ID, not the mutable head. Pages are separate snapshots; refresh to discover a
// newly inserted membership whose ID sorts before a previously used cursor.
func (s *Service) ListRelationships(ctx context.Context, partner string, q RelationshipQuery) (RelationshipPage, error) {
	if err := s.ready(ctx); err != nil {
		return RelationshipPage{}, err
	}
	if !correctionText(partner, 256) || q.Limit < 1 || q.Limit > 100 || (q.After != "" && !validCorrectionFingerprint(q.After)) {
		return RelationshipPage{}, ErrInvalid
	}
	rows, more, err := s.repo.ListRelationshipSnapshots(ctx, ProgramID, partner, q.Limit, q.After)
	if err != nil {
		return RelationshipPage{}, err
	}
	if len(rows) > q.Limit || (more && len(rows) != q.Limit) {
		return RelationshipPage{}, ErrUnavailable
	}
	out := RelationshipPage{ProgramID: ProgramID, PartnerID: partner, Items: []Relationship{}, HasMore: more}
	previous := q.After
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return RelationshipPage{}, err
		}
		if row.ProgramID != ProgramID || row.PartnerID != partner || !correctionText(row.ReferredCustomer, 256) || row.ID != RelationshipReferenceID(ProgramID, partner, row.ReferredCustomer) || row.ID <= previous {
			return RelationshipPage{}, ErrUnavailable
		}
		previous = row.ID
		item, err := projectRelationship(row, partner)
		if err != nil {
			return RelationshipPage{}, err
		}
		out.Items = append(out.Items, item)
	}
	if more {
		out.NextAfter = out.Items[len(out.Items)-1].ID
	}
	return out, nil
}

// validatedOwnershipHistory checks complete chain provenance before a service
// projects it. Copy before sorting to avoid mutating an adapter-owned slice.
func validatedOwnershipHistory(customer string, head Referral, rows []Referral) ([]Referral, error) {
	history := append([]Referral(nil), rows...)
	sort.Slice(history, func(i, j int) bool { return history[i].Revision < history[j].Revision })
	seen := map[string]bool{}
	for i, row := range history {
		if row.ID == "" || row.ProgramID != ProgramID || row.ReferredCustomer != customer || row.PartnerID == "" || row.Revision != int64(i)+1 || row.LockedAt.IsZero() || row.PostedAt.IsZero() || !validTerms(row.TermsSnapshot) || seen[row.ID] {
			return nil, ErrUnavailable
		}
		if i == 0 && (row.CorrectionOf != "" || row.PriorPartnerID != "") {
			return nil, ErrUnavailable
		}
		if i > 0 && (row.CorrectionOf != history[i-1].ID || row.PriorPartnerID != history[i-1].PartnerID || row.LockedAt.Before(history[i-1].LockedAt)) {
			return nil, ErrUnavailable
		}
		seen[row.ID] = true
	}
	if len(history) == 0 {
		if !reflect.DeepEqual(head, Referral{}) {
			return nil, ErrUnavailable
		}
	} else if !reflect.DeepEqual(head, history[len(history)-1]) {
		return nil, ErrUnavailable
	}
	return history, nil
}

// projectRelationship retains only the selected owner's frozen periods.
func projectRelationship(row RelationshipSnapshot, partner string) (Relationship, error) {
	history, err := validatedOwnershipHistory(row.ReferredCustomer, row.Head, row.History)
	if err != nil || len(history) == 0 {
		return Relationship{}, ErrUnavailable
	}
	item := Relationship{ID: row.ID, ProgramID: ProgramID, PartnerID: partner, ReferredCustomer: row.ReferredCustomer, Current: row.Head.PartnerID == partner, Periods: []OwnershipPeriod{}}
	for i, revision := range history {
		if revision.PartnerID != partner {
			continue
		}
		if len(item.Periods) == 0 {
			if row.FirstReferralID != revision.ID {
				return Relationship{}, ErrUnavailable
			}
			item.FirstOwnedAt = revision.LockedAt
		}
		period := OwnershipPeriod{ReferralID: revision.ID, From: revision.LockedAt, Terms: cloneTerms(revision.TermsSnapshot), Corrected: revision.CorrectionOf != ""}
		if i+1 < len(history) {
			until := history[i+1].LockedAt
			period.Until = &until
		}
		item.Periods = append(item.Periods, period)
	}
	if len(item.Periods) == 0 {
		return Relationship{}, ErrUnavailable
	}
	return item, nil
}
