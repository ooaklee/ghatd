package partnerstore

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// ReadRelationshipEvidence reads all selected lifetime memberships, heads,
// complete histories and immutable customer bindings in ONE native snapshot.
// The combined budget is independent of traffic retention and is applied
// before report filters. Callback reentry resets every accumulated result.
func (r *ReferralRepository) ReadRelationshipEvidence(ctx context.Context, program, partner string) ([]referral.RelationshipEvidenceRow, error) {
	if err := validStoreContext(ctx); err != nil {
		return nil, referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return nil, referral.ErrUnavailable
	}
	if program != referral.ProgramID || partner == "" || len(partner) > 256 {
		return nil, referral.ErrInvalid
	}
	var out []referral.RelationshipEvidenceRow
	completed := false
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out, completed = []referral.RelationshipEvidenceRow{}, false
		if nilStoreDependency(tx) {
			return recordstore.ErrUnavailable
		}
		budget := referral.RelationshipEvidenceCapacity
		partition := relationshipsPartition(partner)
		members, err := analyticsRecords(ctx, tx, recordstore.Query{Kind: kindReferralRelationship, Partition: partition}, &budget)
		if err != nil {
			return err
		}
		for _, row := range members {
			var member referralRelationship
			if err := row.Decode(&member); err != nil {
				return err
			}
			if row.Revision != 1 || row.State != "" || row.ExpiresAt != nil || member.ProgramID != program || member.PartnerID != partner || member.ReferredCustomer == "" || member.FirstReferralID == "" || row.ID != referral.RelationshipReferenceID(program, partner, member.ReferredCustomer) {
				return recordstore.ErrUnavailable
			}
			if budget == 0 {
				return referral.ErrCapacity
			}
			budget-- // The current head is evidence too, not a free per-member read.
			head, headRow, err := loadRecord[storedReferral](ctx, tx, kindReferralHead, identity(program, member.ReferredCustomer), referralProgramPartition())
			if err != nil {
				return err
			}
			if head.ProgramID != program || head.ReferredCustomer != member.ReferredCustomer || head.Revision != headRow.Revision || headRow.State != head.PartnerID || headRow.ExpiresAt != nil {
				return recordstore.ErrUnavailable
			}
			item := referral.RelationshipEvidenceRow{Relationship: referral.RelationshipSnapshot{ID: row.ID, ProgramID: program, PartnerID: partner, ReferredCustomer: member.ReferredCustomer, FirstReferralID: member.FirstReferralID, Head: head.value(), History: []referral.Referral{}}, Bindings: []referral.PaymentAttribution{}}
			revisions, err := analyticsRecords(ctx, tx, recordstore.Query{Kind: kindReferralRevision, Partition: referralPartition(member.ReferredCustomer)}, &budget)
			if err != nil {
				return err
			}
			for _, revision := range revisions {
				var stored storedReferral
				if err := revision.Decode(&stored); err != nil {
					return err
				}
				if stored.ID != revision.ID || revision.Revision != 1 || revision.State != "" || revision.ExpiresAt != nil || stored.ProgramID != program || stored.ReferredCustomer != member.ReferredCustomer {
					return recordstore.ErrUnavailable
				}
				item.Relationship.History = append(item.Relationship.History, stored.value())
			}
			item.Bindings, err = readCustomerBindingEvidence(ctx, tx, program, member.ReferredCustomer, &budget)
			if err != nil {
				return err
			}

			out = append(out, item)
		}
		completed = true
		return ctx.Err()
	})
	if err != nil {
		if errors.Is(err, referral.ErrCapacity) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		// Retain the underlying driver/encryption cause while classifying a
		// failed complete read. Absence of an expected retained head is corrupt.
		return nil, errors.Join(referral.ErrUnavailable, referralError(err))
	}
	if !completed {
		return nil, referral.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// readCustomerBindingEvidence is shared by complete relationship and conversion
// reads inside their one owning native snapshot, with the same metadata checks.
func readCustomerBindingEvidence(ctx context.Context, tx recordstore.Tx, program, customer string, budget *int) ([]referral.PaymentAttribution, error) {
	out := []referral.PaymentAttribution{}
	bindings, err := analyticsRecords(ctx, tx, recordstore.Query{Kind: kindPaymentBinding, Partition: referralProgramPartition(), State: customer}, budget)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		var value referral.PaymentAttribution
		if err := binding.Decode(&value); err != nil {
			return nil, err
		}
		if binding.Revision != 1 || binding.ExpiresAt != nil || value.ProgramID != program || value.ReferredCustomer != customer || binding.ID != identity(program, value.PaymentID) {
			return nil, recordstore.ErrUnavailable
		}
		out = append(out, value)
	}
	return out, nil
}
