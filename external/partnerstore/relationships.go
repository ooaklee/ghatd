package partnerstore

import (
	"context"
	"sort"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const kindReferralRelationship = "partner_referral_relationship"

type referralRelationship struct {
	ProgramID, PartnerID, ReferredCustomer, FirstReferralID string
}

func relationshipsPartition(partner string) string {
	return "partner-referral-relationships:" + identity(referral.ProgramID, partner)
}

// retainReferralRelationship runs inside the same customer guard/transaction
// as the immutable revision and head. Unique kind/ID enforces one lifetime
// reference; returning ownership never overwrites its first owned revision.
func retainReferralRelationship(ctx context.Context, tx recordstore.Tx, v referral.Referral) error {
	id := referral.RelationshipReferenceID(v.ProgramID, v.PartnerID, v.ReferredCustomer)
	partition := relationshipsPartition(v.PartnerID)
	member, row, err := loadRecord[referralRelationship](ctx, tx, kindReferralRelationship, id, partition)
	if err == nil {
		if row.Revision != 1 || member.ProgramID != v.ProgramID || member.PartnerID != v.PartnerID || member.ReferredCustomer != v.ReferredCustomer || member.FirstReferralID == "" {
			return recordstore.ErrUnavailable
		}
		first, _, err := loadRecord[storedReferral](ctx, tx, kindReferralRevision, member.FirstReferralID, referralPartition(v.ReferredCustomer))
		if err != nil {
			return err
		}
		if first.ProgramID != v.ProgramID || first.PartnerID != v.PartnerID || first.ReferredCustomer != v.ReferredCustomer || first.Revision >= v.Revision {
			return recordstore.ErrUnavailable
		}
		return nil
	}
	if !singleCauseIs(err, recordstore.ErrNotFound) {
		return err
	}
	return insertRecord(ctx, tx, kindReferralRelationship, id, partition, 1, referralRelationship{v.ProgramID, v.PartnerID, v.ReferredCustomer, v.ID})
}

// ListRelationshipSnapshots reads the page, current heads and all selected
// customers' revisions in one owning read transaction. Failure discards the
// complete result, including any preceding successfully decoded customers.
func (r *ReferralRepository) ListRelationshipSnapshots(ctx context.Context, program, partner string, limit int, after string) ([]referral.RelationshipSnapshot, bool, error) {
	if err := validStoreContext(ctx); err != nil {
		return nil, false, referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return nil, false, referral.ErrUnavailable
	}
	if program != referral.ProgramID || partner == "" || len(partner) > 256 || limit < 1 || limit > 100 || len(after) > 256 {
		return nil, false, referral.ErrInvalid
	}
	var out []referral.RelationshipSnapshot
	var more bool
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out, more = nil, false
		partition := relationshipsPartition(partner)
		if after != "" {
			cursor, row, err := loadRecord[referralRelationship](ctx, tx, kindReferralRelationship, after, partition)
			if err != nil {
				return err
			}
			if row.Revision != 1 || cursor.ProgramID != program || cursor.PartnerID != partner || after != referral.RelationshipReferenceID(program, partner, cursor.ReferredCustomer) {
				return referral.ErrInvalid
			}
		}
		rows, err := tx.Find(ctx, recordstore.Query{Kind: kindReferralRelationship, Partition: partition, Limit: limit + 1, AfterID: after})
		if err != nil {
			return err
		}
		if len(rows) > limit {
			more = true
			rows = rows[:limit]
		}
		for _, row := range rows {
			var member referralRelationship
			if err := row.Decode(&member); err != nil {
				return err
			}
			if row.Kind != kindReferralRelationship || row.Partition != partition || row.Revision != 1 || member.ProgramID != program || member.PartnerID != partner || member.ReferredCustomer == "" || member.FirstReferralID == "" || row.ID != referral.RelationshipReferenceID(program, partner, member.ReferredCustomer) {
				return recordstore.ErrUnavailable
			}
			head, headRow, err := loadRecord[storedReferral](ctx, tx, kindReferralHead, identity(program, member.ReferredCustomer), referralProgramPartition())
			if err != nil {
				return err
			}
			if head.ProgramID != program || head.ReferredCustomer != member.ReferredCustomer || head.Revision != headRow.Revision || headRow.State != head.PartnerID {
				return recordstore.ErrUnavailable
			}
			snapshot := referral.RelationshipSnapshot{ID: row.ID, ProgramID: program, PartnerID: partner, ReferredCustomer: member.ReferredCustomer, FirstReferralID: member.FirstReferralID, Head: head.value()}
			revisions, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindReferralRevision, Partition: referralPartition(member.ReferredCustomer)})
			if err != nil {
				return err
			}
			for _, revision := range revisions {
				var stored storedReferral
				if err := revision.Decode(&stored); err != nil {
					return err
				}
				if stored.ID != revision.ID || revision.Kind != kindReferralRevision || stored.ProgramID != program || stored.ReferredCustomer != member.ReferredCustomer {
					return recordstore.ErrUnavailable
				}
				snapshot.History = append(snapshot.History, stored.value())
			}
			sort.Slice(snapshot.History, func(i, j int) bool { return snapshot.History[i].Revision < snapshot.History[j].Revision })
			out = append(out, snapshot)
		}
		return nil
	})
	if err != nil {
		return nil, false, referralError(err)
	}
	return out, more, nil
}
