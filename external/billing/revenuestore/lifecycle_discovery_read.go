package revenuestore

import (
	"context"
	"errors"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const kindLifecycleDiscoveryPreparation = "billing_lifecycle_discovery_preparation"

// Only completed owning preparation installs this immutable record after all
// history/projection phases and the source epoch CAS. Reads never infer readiness
// from emptiness. Older writers must be drained before preparation.
type lifecycleDiscoveryPreparation struct {
	Scope      billing.RevenueScope
	Schema     int
	PreparedAt time.Time
}

func discoveryJoinedError(err error) error {
	if singleCause(err, billing.ErrRevenueNotFound) || singleCause(err, recordstore.ErrNotFound) {
		return billing.ErrRevenueUnavailable
	}
	return err
}
func discoveryMetadata(row recordstore.Record, kind, id, part string, immutable bool) bool {
	return row.Kind == kind && row.ID == id && row.Partition == part && row.Revision >= 1 && (!immutable || row.Revision == 1) && row.Sequence == 0 && row.State == "" && row.ExpiresAt == nil
}
func discoveryIntent(ctx context.Context, tx recordstore.Tx, id string) (billing.CheckoutIntent, error) {
	b := &checkoutBound{tx: tx}
	i, err := b.GetCheckoutIntent(ctx, id)
	if err != nil {
		return billing.CheckoutIntent{}, discoveryJoinedError(err)
	}
	// Validate both acknowledgement and its immutable reverse session reservation.
	row, err := tx.Get(ctx, kindCheckoutIntent, id)
	if err != nil {
		return billing.CheckoutIntent{}, discoveryJoinedError(err)
	}
	if !discoveryMetadata(row, kindCheckoutIntent, id, checkoutPartition, true) {
		return billing.CheckoutIntent{}, billing.ErrRevenueUnavailable
	}
	ack, ackRow, err := get[checkoutAcknowledgement](ctx, tx, kindCheckoutAck, id, checkoutPartition)
	if err != nil {
		return billing.CheckoutIntent{}, discoveryJoinedError(err)
	}
	if !discoveryMetadata(ackRow, kindCheckoutAck, id, checkoutPartition, true) || ack.IntentID != id || ack.SessionID == "" {
		return billing.CheckoutIntent{}, billing.ErrRevenueUnavailable
	}
	sessionID := associationKey(i.Scope, ack.SessionID, "")
	reverse, reverseRow, err := get[checkoutAcknowledgement](ctx, tx, kindCheckoutSession, sessionID, checkoutPartition)
	if err != nil {
		return billing.CheckoutIntent{}, discoveryJoinedError(err)
	}
	if !discoveryMetadata(reverseRow, kindCheckoutSession, sessionID, checkoutPartition, true) || reverse != ack {
		return billing.CheckoutIntent{}, billing.ErrRevenueUnavailable
	}
	i.SessionID = ack.SessionID
	return i, nil
}
func joinDiscoveryCheckout(ctx context.Context, tx recordstore.Tx, row recordstore.Record, scope billing.RevenueScope) (billing.LifecycleDiscoveryCandidate, error) {
	var source lifecycleCheckoutSource
	part := lifecycleSourcePartition(scope)
	if row.Decode(&source) != nil || source.Scope != scope || !discoveryMetadata(row, kindLifecycleCheckoutSource, billing.LifecycleDiscoverySourceID(scope, billing.LifecycleCheckoutSources, source.IntentID), part, true) {
		return billing.LifecycleDiscoveryCandidate{}, billing.ErrRevenueUnavailable
	}
	i, err := discoveryIntent(ctx, tx, source.IntentID)
	if err != nil {
		return billing.LifecycleDiscoveryCandidate{}, err
	}
	if source.SessionID != i.SessionID || source.IntentFingerprint != i.Fingerprint {
		return billing.LifecycleDiscoveryCandidate{}, billing.ErrRevenueUnavailable
	}
	return billing.LifecycleDiscoveryCandidate{ID: row.ID, Revision: 1, Scope: scope, PrincipalID: i.Request.UserID, Intent: i}, nil
}

func joinDiscoverySubscription(ctx context.Context, tx recordstore.Tx, row recordstore.Record, scope billing.RevenueScope) (billing.LifecycleDiscoveryCandidate, error) {
	var source lifecycleSubscriptionSource
	if row.Decode(&source) != nil || !validLifecycleOwner(source) || source.Scope != scope || !discoveryMetadata(row, kindLifecycleSubscriptionSource, lifecycleSubscriptionKey(scope, source.SubscriptionID), lifecycleSourcePartition(scope), false) {
		return billing.LifecycleDiscoveryCandidate{}, billing.ErrRevenueUnavailable
	}
	c := billing.LifecycleDiscoveryCandidate{ID: row.ID, Revision: row.Revision, Scope: scope, PrincipalID: source.PrincipalID, CustomerID: source.CustomerID, SubscriptionID: source.SubscriptionID}
	if source.FactID != "" {
		factRow, err := tx.Get(ctx, kindFact, source.FactID)
		if err != nil {
			return billing.LifecycleDiscoveryCandidate{}, discoveryJoinedError(err)
		}
		fact, err := decodeFact(factRow)
		if err != nil {
			return billing.LifecycleDiscoveryCandidate{}, err
		}
		if fact.ID != source.FactID || factRow.Kind != kindFact || factRow.Partition != partition || factRow.Revision != 1 || factRow.State != billing.RevenuePayment || factRow.ExpiresAt != nil || fact.Fingerprint != source.FactFingerprint {
			return billing.LifecycleDiscoveryCandidate{}, billing.ErrRevenueUnavailable
		}
		c.Fact = fact
	}
	b := &checkoutBound{tx: tx}
	anchor, err := b.GetCheckoutLifecycleAnchor(ctx, scope, source.SubscriptionID)
	if err == nil {
		if source.AnchorIntentID == "" || source.AnchorIntentID != anchor.IntentID || source.AnchorFingerprint != anchor.Fingerprint {
			return billing.LifecycleDiscoveryCandidate{}, billing.ErrRevenueUnavailable
		}
		receipt, err := b.GetCheckoutLifecycleReceipt(ctx, anchor.IntentID)
		if err != nil {
			return billing.LifecycleDiscoveryCandidate{}, discoveryJoinedError(err)
		}
		for _, ref := range []struct{ kind, id string }{{kindCheckoutLifecycleAnchor, associationKey(scope, source.SubscriptionID, "")}, {kindCheckoutLifecycleReceipt, anchor.IntentID}} {
			native, err := tx.Get(ctx, ref.kind, ref.id)
			if err != nil {
				return billing.LifecycleDiscoveryCandidate{}, discoveryJoinedError(err)
			}
			if !discoveryMetadata(native, ref.kind, ref.id, checkoutPartition, true) {
				return billing.LifecycleDiscoveryCandidate{}, billing.ErrRevenueUnavailable
			}
		}
		intent, err := discoveryIntent(ctx, tx, anchor.IntentID)
		if err != nil {
			return billing.LifecycleDiscoveryCandidate{}, err
		}
		c.Anchor, c.AnchorReceipt, c.AnchorIntent = anchor, receipt, intent
	} else if !singleCause(err, billing.ErrRevenueNotFound) {
		return billing.LifecycleDiscoveryCandidate{}, err
	} else if source.AnchorIntentID != "" {
		return billing.LifecycleDiscoveryCandidate{}, billing.ErrRevenueUnavailable
	}
	paidID := associationKey(scope, source.SubscriptionID, "")
	paid, paidRow, err := get[billing.CheckoutAssociation](ctx, tx, kindCheckoutPrincipal, paidID, checkoutPartition)
	if err == nil {
		if !discoveryMetadata(paidRow, kindCheckoutPrincipal, paidID, checkoutPartition, true) {
			return billing.LifecycleDiscoveryCandidate{}, billing.ErrRevenueUnavailable
		}
		original, err := discoveryIntent(ctx, tx, paid.IntentID)
		if err != nil {
			return billing.LifecycleDiscoveryCandidate{}, err
		}
		c.PaidOwner, c.PaidOwnerIntent, c.HasPaidOwner = paid, original, true
	} else if !singleCause(err, billing.ErrRevenueNotFound) {
		return billing.LifecycleDiscoveryCandidate{}, err
	}
	return c, nil
}

// ReadLifecycleDiscovery performs ONE bounded indexed projection query followed
// by bounded owning joins in the same snapshot. Neither FindAll nor global
// history reporting is used. No partial page survives failure or cancellation.
func (r *Repository) ReadLifecycleDiscovery(ctx context.Context, q billing.LifecycleDiscoveryQuery) (billing.LifecycleDiscoverySnapshot, error) {
	if ctx == nil {
		return billing.LifecycleDiscoverySnapshot{}, billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return billing.LifecycleDiscoverySnapshot{}, err
	}
	after, err := q.AfterID()
	if err != nil {
		return billing.LifecycleDiscoverySnapshot{}, err
	}
	if r == nil || r.store == nil {
		return billing.LifecycleDiscoverySnapshot{}, billing.ErrRevenueUnavailable
	}
	var out billing.LifecycleDiscoverySnapshot
	completed := false
	err = r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = billing.LifecycleDiscoverySnapshot{Items: []billing.LifecycleDiscoveryCandidate{}}
		position := after
		completed = false
		part := lifecycleSourcePartition(q.Scope)
		id := checkoutScopeKey(q.Scope)
		ready, row, err := get[lifecycleDiscoveryPreparation](ctx, tx, kindLifecycleDiscoveryPreparation, id, part)
		if singleCause(err, billing.ErrRevenueNotFound) {
			return billing.ErrLifecycleDiscoveryUnprepared
		}
		if err != nil {
			return err
		}
		if !discoveryMetadata(row, kindLifecycleDiscoveryPreparation, id, part, true) || ready.Scope != q.Scope || ready.Schema != 1 || ready.PreparedAt.IsZero() {
			return billing.ErrRevenueUnavailable
		}
		kind := kindLifecycleCheckoutSource
		if q.Kind == billing.LifecycleSubscriptionSources {
			kind = kindLifecycleSubscriptionSource
		}
		rows, err := tx.Find(ctx, recordstore.Query{Kind: kind, Partition: part, AfterID: position, Limit: q.Limit})
		if err != nil {
			return err
		}
		if len(rows) > q.Limit {
			return billing.ErrRevenueUnavailable
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			if row.ID <= position {
				return billing.ErrRevenueUnavailable
			}
			position = row.ID
			var c billing.LifecycleDiscoveryCandidate
			if q.Kind == billing.LifecycleCheckoutSources {
				c, err = joinDiscoveryCheckout(ctx, tx, row, q.Scope)
				if err != nil {
					return err
				}
			} else {
				c, err = joinDiscoverySubscription(ctx, tx, row, q.Scope)
				if err != nil {
					return err
				}
			}
			out.Items = append(out.Items, c)
		}
		out.Prepared = true
		completed = true
		return ctx.Err()
	})
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return billing.LifecycleDiscoverySnapshot{}, errors.Join(canceled, err)
		}
		if singleCause(err, billing.ErrLifecycleDiscoveryUnprepared) {
			return billing.LifecycleDiscoverySnapshot{}, err
		}
		return billing.LifecycleDiscoverySnapshot{}, errors.Join(billing.ErrRevenueUnavailable, mapped(err))
	}
	if err := ctx.Err(); err != nil {
		return billing.LifecycleDiscoverySnapshot{}, err
	}
	if !completed {
		return billing.LifecycleDiscoverySnapshot{}, billing.ErrRevenueUnavailable
	}
	return out, nil
}

var _ billing.LifecycleDiscoveryRepository = (*Repository)(nil)
