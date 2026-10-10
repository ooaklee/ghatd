package partnerstore

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// analyticsRecords bounds complete evidence across all queries in a snapshot.
// The extra row detects capacity before decoding or returning partial totals.
// Store.Find must return strictly ascending IDs, advancing AfterID on every
// page. Reject violated ordering before it can skip or duplicate evidence;
// sorting a returned page cannot repair an invalid pagination contract.
func analyticsRecords(ctx context.Context, tx recordstore.Tx, q recordstore.Query, budget *int) ([]recordstore.Record, error) {
	out := []recordstore.Record{}
	previous := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q.Limit = 200
		if *budget < q.Limit {
			q.Limit = *budget + 1
		}
		q.AfterID = previous
		page, err := tx.Find(ctx, q)
		if err != nil {
			return nil, err
		}
		if len(page) > *budget {
			return nil, referral.ErrCapacity
		}
		for _, row := range page {
			if row.Kind != q.Kind || row.Partition != q.Partition || row.ID <= previous || (q.State != "" && row.State != q.State) {
				return nil, recordstore.ErrUnavailable
			}
			previous = row.ID
		}
		*budget -= len(page)
		out = append(out, page...)
		if len(page) < q.Limit {
			return out, nil
		}
	}
}

// ReadAnalyticsSnapshot uses one owning read transaction, including full
// membership histories and retained links. Per-link pagination happens later
// in the service; a page cannot stand in for a global conversion denominator.
func (r *ReferralRepository) ReadAnalyticsSnapshot(ctx context.Context, program, partner string, q referral.AnalyticsQuery) (referral.AnalyticsSnapshot, error) {
	if err := validStoreContext(ctx); err != nil {
		return referral.AnalyticsSnapshot{}, referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.AnalyticsSnapshot{}, referral.ErrUnavailable
	}
	if program != referral.ProgramID || partner == "" || len(partner) > 256 || q.Validate() != nil {
		return referral.AnalyticsSnapshot{}, referral.ErrInvalid
	}
	var out referral.AnalyticsSnapshot
	completed := false
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out, completed = referral.AnalyticsSnapshot{}, false
		if nilStoreDependency(tx) {
			return recordstore.ErrUnavailable
		}
		budget := referral.AnalyticsCapacity
		var err error
		out, err = readAnalyticsEvidence(ctx, tx, program, partner, q, &budget, false)
		if err == nil {
			completed = true
		}
		return err
	})
	if err != nil {
		return referral.AnalyticsSnapshot{}, referralError(err)
	}
	if !completed {
		return referral.AnalyticsSnapshot{}, referral.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return referral.AnalyticsSnapshot{}, err
	}
	return out, nil
}

// readAnalyticsEvidence reads traffic and retained ownership through the SAME
// bound native Tx. Conversion reads charge heads to their combined budget;
// existing traffic analytics retain their documented membership/history budget.
func readAnalyticsEvidence(ctx context.Context, tx recordstore.Tx, program, partner string, q referral.AnalyticsQuery, budget *int, chargeHeads bool) (referral.AnalyticsSnapshot, error) {
	out := referral.AnalyticsSnapshot{}
	dayBudget, rawBudget := referral.AnalyticsCapacity, referral.AnalyticsCapacity
	links, err := analyticsRecords(ctx, tx, recordstore.Query{Kind: kindLink, Partition: linksPartition(partner)}, budget)
	if err != nil {
		return referral.AnalyticsSnapshot{}, err
	}
	for _, row := range links {
		var link referral.Link
		if err := row.Decode(&link); err != nil {
			return referral.AnalyticsSnapshot{}, err
		}
		if link.ID != row.ID || row.Revision < 1 || row.ExpiresAt != nil || link.ProgramID != program || link.PartnerID != partner {
			return referral.AnalyticsSnapshot{}, recordstore.ErrUnavailable
		}
		out.Links = append(out.Links, link)
		days, err := analyticsRecords(ctx, tx, recordstore.Query{Kind: kindVisitDay, Partition: clicksPartition(link.ID)}, &dayBudget)
		if err != nil {
			return referral.AnalyticsSnapshot{}, err
		}
		for _, row := range days {
			var day referral.VisitDay
			if err := row.Decode(&day); err != nil {
				return referral.AnalyticsSnapshot{}, err
			}
			if day.Validate() != nil || day.LinkID != link.ID || row.ID != identity(program, link.ID, visitDayKey(day.Day)) || day.Revision != row.Revision || row.State != visitDayKey(day.Day) || row.ExpiresAt != nil {
				return referral.AnalyticsSnapshot{}, recordstore.ErrUnavailable
			}
			out.Days = append(out.Days, day)
		}
		for _, day := range q.PartialDays() {
			observations, err := analyticsRecords(ctx, tx, recordstore.Query{Kind: kindClick, Partition: clicksPartition(link.ID), State: visitDayKey(day)}, &rawBudget)
			if err != nil {
				return referral.AnalyticsSnapshot{}, err
			}
			for _, observation := range observations {
				var c referral.Click
				if err := observation.Decode(&c); err != nil {
					return referral.AnalyticsSnapshot{}, err
				}
				if c.ID != observation.ID || c.LinkID != link.ID || observation.Revision != 1 || observation.State != visitDayKey(c.OccurredAt) || observation.ExpiresAt == nil || observation.ExpiresAt.Before(c.OccurredAt.Add(24*time.Hour)) || observation.ExpiresAt.After(c.OccurredAt.Add(365*24*time.Hour+time.Millisecond)) {
					return referral.AnalyticsSnapshot{}, recordstore.ErrUnavailable
				}
				out.Clicks = append(out.Clicks, c)
			}
		}
	}
	members, err := analyticsRecords(ctx, tx, recordstore.Query{Kind: kindReferralRelationship, Partition: relationshipsPartition(partner)}, budget)
	if err != nil {
		return referral.AnalyticsSnapshot{}, err
	}
	for _, row := range members {
		var member referralRelationship
		if err := row.Decode(&member); err != nil {
			return referral.AnalyticsSnapshot{}, err
		}
		if row.Revision != 1 || row.State != "" || row.ExpiresAt != nil || member.ProgramID != program || member.PartnerID != partner || member.ReferredCustomer == "" || member.FirstReferralID == "" || row.ID != referral.RelationshipReferenceID(program, partner, member.ReferredCustomer) {
			return referral.AnalyticsSnapshot{}, recordstore.ErrUnavailable
		}
		if chargeHeads {
			if *budget == 0 {
				return referral.AnalyticsSnapshot{}, referral.ErrCapacity
			}
			*budget -= 1
		}
		head, headRow, err := loadRecord[storedReferral](ctx, tx, kindReferralHead, identity(program, member.ReferredCustomer), referralProgramPartition())
		if err != nil {
			return referral.AnalyticsSnapshot{}, err
		}
		if head.ProgramID != program || head.ReferredCustomer != member.ReferredCustomer || head.Revision != headRow.Revision || headRow.State != head.PartnerID || headRow.ExpiresAt != nil {
			return referral.AnalyticsSnapshot{}, recordstore.ErrUnavailable
		}
		relationship := referral.RelationshipSnapshot{ID: row.ID, ProgramID: program, PartnerID: partner, ReferredCustomer: member.ReferredCustomer, FirstReferralID: member.FirstReferralID, Head: head.value()}
		revisions, err := analyticsRecords(ctx, tx, recordstore.Query{Kind: kindReferralRevision, Partition: referralPartition(member.ReferredCustomer)}, budget)
		if err != nil {
			return referral.AnalyticsSnapshot{}, err
		}
		for _, revision := range revisions {
			var stored storedReferral
			if err := revision.Decode(&stored); err != nil {
				return referral.AnalyticsSnapshot{}, err
			}
			if stored.ID != revision.ID || revision.Revision != 1 || revision.State != "" || revision.ExpiresAt != nil || stored.ProgramID != program || stored.ReferredCustomer != member.ReferredCustomer {
				return referral.AnalyticsSnapshot{}, recordstore.ErrUnavailable
			}
			relationship.History = append(relationship.History, stored.value())
		}
		out.Relationships = append(out.Relationships, relationship)
	}
	return out, nil
}
