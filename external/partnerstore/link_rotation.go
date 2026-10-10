package partnerstore

import (
	"context"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const kindLinkRotation = "partner_referral_link_rotation"

// The domain request excludes ActorID from transport JSON. Persist it explicitly
// in this private encrypted envelope so receipt validation retains its actor.
type storedLinkRotation struct {
	Receipt referral.LinkRotationReceipt
	ActorID string
}

// WithLinkTransaction uses the same partner guard as initial issuance and
// retirement. Every nested write remains on this bound storage transaction.
func (r *ReferralRepository) WithLinkTransaction(ctx context.Context, program, partner string, fn func(referral.LinkRotationTransaction) error) error {
	if err := validStoreContext(ctx); err != nil {
		return referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.ErrUnavailable
	}
	if program != referral.ProgramID || partner == "" || len(partner) > 256 || fn == nil {
		return referral.ErrInvalid
	}
	if _, nested := r.store.(partitionStore); nested {
		return referral.ErrInvalid
	}
	partition := linksPartition(partner)
	return referralError(r.store.Transact(ctx, partition, func(tx recordstore.Tx) error {
		return fn(&ReferralRepository{store: partitionStore{tx: tx, partition: partition}})
	}))
}

// GetLinkRotation reads an immutable actor/key receipt, validating the complete
// canonical envelope rather than treating a corrupt receipt as absence.
func (r *ReferralRepository) GetLinkRotation(ctx context.Context, partner, actor, key string) (referral.LinkRotationReceipt, error) {
	if err := validStoreContext(ctx); err != nil {
		return referral.LinkRotationReceipt{}, referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.LinkRotationReceipt{}, referral.ErrUnavailable
	}
	if partner == "" || actor == "" || key == "" || len(partner) > 256 || len(actor) > 256 || len(key) > 256 {
		return referral.LinkRotationReceipt{}, referral.ErrInvalid
	}
	var out referral.LinkRotationReceipt
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = referral.LinkRotationReceipt{}
		id := referral.LinkRotationID(partner, actor, key)
		stored, row, err := loadRecord[storedLinkRotation](ctx, tx, kindLinkRotation, id, linksPartition(partner))
		if err != nil {
			return err
		}
		v := stored.Receipt
		v.Request.ActorID = stored.ActorID
		if row.Revision != 1 || row.ExpiresAt != nil || v.ID != id || v.Validate() != nil ||
			v.Request.Partner.PartnerID != partner || v.Request.ActorID != actor || v.Request.IdempotencyKey != key {
			return recordstore.ErrUnavailable
		}
		out = v
		return nil
	})
	if err != nil {
		return referral.LinkRotationReceipt{}, referralError(err)
	}
	return out, nil
}

// InsertLinkRotation is transaction-bound: a receipt cannot be committed
// separately from retiring the original and inserting its replacement.
func (r *ReferralRepository) InsertLinkRotation(ctx context.Context, v referral.LinkRotationReceipt) error {
	if err := validStoreContext(ctx); err != nil {
		return referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.ErrUnavailable
	}
	if v.Validate() != nil {
		return referral.ErrInvalid
	}
	bound, ok := r.store.(partitionStore)
	partition := linksPartition(v.Request.Partner.PartnerID)
	if !ok || bound.partition != partition {
		return referral.ErrInvalid
	}
	return referralError(r.store.Transact(ctx, partition, func(tx recordstore.Tx) error {
		return insertRecord(ctx, tx, kindLinkRotation, v.ID, partition, 1, storedLinkRotation{Receipt: v, ActorID: v.Request.ActorID})
	}))
}
