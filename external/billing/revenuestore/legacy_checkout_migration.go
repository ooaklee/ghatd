package revenuestore

import (
	"context"
	"reflect"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const kindLegacyCheckoutMigration = "billing_legacy_checkout_migration"

// Private persistence retains fields deliberately excluded from public JSON.
type persistedLegacyCheckoutMigration struct {
	Receipt                                                                                                                      billing.LegacyCheckoutMigrationReceipt
	ActorID, Reason, SourceID, EnvelopeID, SourceObservationFingerprint, SourceFingerprint, RequestFingerprint, ProofFingerprint string
	Association                                                                                                                  billing.CheckoutAssociation
	Checkout                                                                                                                     paymentprovider.RevenueCheckoutEvidence
	Invoice                                                                                                                      paymentprovider.RevenueInvoiceEvidence
}

func persistLegacyCheckout(r billing.LegacyCheckoutMigrationReceipt) persistedLegacyCheckoutMigration {
	return persistedLegacyCheckoutMigration{r, r.ActorID, r.Reason, r.SourceID, r.EnvelopeID, r.SourceObservationFingerprint, r.SourceFingerprint, r.RequestFingerprint, r.ProofFingerprint, r.Association, r.Checkout, r.Invoice}
}
func (p persistedLegacyCheckoutMigration) receipt() billing.LegacyCheckoutMigrationReceipt {
	r := p.Receipt
	r.ActorID = p.ActorID
	r.Reason = p.Reason
	r.SourceID = p.SourceID
	r.EnvelopeID = p.EnvelopeID
	r.SourceObservationFingerprint = p.SourceObservationFingerprint
	r.SourceFingerprint = p.SourceFingerprint
	r.RequestFingerprint = p.RequestFingerprint
	r.ProofFingerprint = p.ProofFingerprint
	r.Association = p.Association
	r.Checkout = p.Checkout
	r.Invoice = p.Invoice
	return r
}
func (b *checkoutBound) legacySource(ctx context.Context, r billing.LegacyCheckoutMigrationReceipt) (billing.RevenueObservation, error) {
	source, err := (&bound{b.tx}).GetObservation(ctx, r.SourceID)
	if err != nil {
		return billing.RevenueObservation{}, err
	}
	if source.Scope != r.Association.Scope || source.EnvelopeID != r.EnvelopeID || source.Fingerprint != r.SourceObservationFingerprint || source.SourceFingerprint != r.SourceFingerprint || source.QuarantineReason != "historical_payer_plan_pending" || source.ResolutionOf != "" || len(source.FactIDs) != 0 {
		return billing.RevenueObservation{}, billing.ErrRevenueConflict
	}
	if r.MigratedAt.Before(source.AcceptedAt) {
		return billing.RevenueObservation{}, billing.ErrRevenueInvalid
	}
	return source, nil
}

// GetLegacyCheckoutMigration distinguishes a historical reservation from a
// modern acknowledgement. Conclusive absence requires a complete bounded scan
// of old associations because older encrypted rows have no session projection.
func (b *checkoutBound) GetLegacyCheckoutMigration(ctx context.Context, scope billing.RevenueScope, session string) (billing.LegacyCheckoutMigrationReceipt, error) {
	if err := b.checkScope(scope); err != nil {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	id := billing.LegacyCheckoutMigrationID(scope, session)
	p, row, err := get[persistedLegacyCheckoutMigration](ctx, b.tx, kindLegacyCheckoutMigration, id, checkoutPartition)
	if err == nil {
		r := p.receipt()
		if r.Validate() != nil || r.ID != id || r.Association.Scope != scope || r.Association.SessionID != session || row.Revision != 1 || row.Sequence != 0 || row.State != "" || row.ExpiresAt != nil {
			return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueUnavailable
		}
		reverse, _, e := get[checkoutAcknowledgement](ctx, b.tx, kindCheckoutSession, associationKey(scope, session, ""), checkoutPartition)
		if e != nil {
			return billing.LegacyCheckoutMigrationReceipt{}, discoveryJoinedError(e)
		}
		if reverse.IntentID != "" || reverse.SessionID != session || reverse.LegacyMigrationID != r.ID {
			return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueConflict
		}
		a, e := b.GetCheckoutAssociation(ctx, scope, r.Association.SubscriptionID, r.Association.ProviderPriceID)
		if e != nil {
			return billing.LegacyCheckoutMigrationReceipt{}, discoveryJoinedError(e)
		}
		if !reflect.DeepEqual(a, r.Association) {
			return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueConflict
		}
		principal, _, e := get[billing.CheckoutAssociation](ctx, b.tx, kindCheckoutPrincipal, associationKey(scope, a.SubscriptionID, ""), checkoutPartition)
		if e != nil {
			return billing.LegacyCheckoutMigrationReceipt{}, discoveryJoinedError(e)
		}
		if principal.Scope != scope || principal.SubscriptionID != a.SubscriptionID || principal.PrincipalID != a.PrincipalID || principal.CustomerID != a.CustomerID || principal.LinkedAt.IsZero() {
			return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueConflict
		}
		projection, _, e := get[lifecycleSubscriptionSource](ctx, b.tx, kindLifecycleSubscriptionSource, lifecycleSubscriptionKey(scope, a.SubscriptionID), lifecycleSourcePartition(scope))
		if e != nil {
			return billing.LegacyCheckoutMigrationReceipt{}, discoveryJoinedError(e)
		}
		if !validLifecycleOwner(projection) || projection.Scope != scope || projection.SubscriptionID != a.SubscriptionID || projection.PrincipalID != a.PrincipalID || projection.CustomerID != a.CustomerID {
			return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueConflict
		}
		if _, e = b.legacySource(ctx, r); e != nil {
			return billing.LegacyCheckoutMigrationReceipt{}, discoveryJoinedError(e)
		}
		return r, nil
	}
	if !singleCause(err, billing.ErrRevenueNotFound) {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	reverse, _, err := get[checkoutAcknowledgement](ctx, b.tx, kindCheckoutSession, associationKey(scope, session, ""), checkoutPartition)
	if err == nil {
		if reverse.LegacyMigrationID != "" || reverse.IntentID == "" || reverse.SessionID != session {
			return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueUnavailable
		}
		return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueConflict
	}
	if !singleCause(err, billing.ErrRevenueNotFound) {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	after := ""
	count := 0
	for {
		rows, err := b.tx.Find(ctx, recordstore.Query{Kind: kindCheckoutAssociation, Partition: checkoutPartition, AfterID: after, Limit: 200})
		if err != nil {
			return billing.LegacyCheckoutMigrationReceipt{}, mapped(err)
		}
		for _, row := range rows {
			count++
			if count > 10000 {
				return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueHistoryTooLarge
			}
			if row.ID <= after || row.Kind != kindCheckoutAssociation || row.Partition != checkoutPartition || row.Revision != 1 || row.ExpiresAt != nil {
				return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueUnavailable
			}
			var a billing.CheckoutAssociation
			if err := row.Decode(&a); err != nil {
				return billing.LegacyCheckoutMigrationReceipt{}, err
			}
			after = row.ID
			if a.Scope == scope && a.SessionID == session {
				return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueConflict
			}
		}
		if len(rows) < 200 {
			break
		}
	}
	return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueNotFound
}

// InstallLegacyCheckoutAssociation uses the EXISTING reverse session record as
// a discriminated historical reservation. Older acknowledgement writers also
// reject its empty IntentID, so no second kind can race the shared unique key.
func (b *checkoutBound) InstallLegacyCheckoutAssociation(ctx context.Context, r billing.LegacyCheckoutMigrationReceipt) (billing.LegacyCheckoutMigrationReceipt, error) {
	if err := b.checkScope(r.Association.Scope); err != nil {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	if r.Validate() != nil {
		return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueInvalid
	}
	old, err := b.GetLegacyCheckoutMigration(ctx, r.Association.Scope, r.Association.SessionID)
	if err == nil {
		if old.RequestFingerprint != r.RequestFingerprint || old.ProofFingerprint != r.ProofFingerprint {
			return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueConflict
		}
		return old, nil
	}
	if !singleCause(err, billing.ErrRevenueNotFound) {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	source, err := b.legacySource(ctx, r)
	if err != nil {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	_, err = (&bound{b.tx}).GetObservation(ctx, source.ResolutionID())
	if err == nil {
		return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueConflict
	}
	if !singleCause(err, billing.ErrRevenueNotFound) {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	_, err = b.GetCheckoutAssociation(ctx, r.Association.Scope, r.Association.SubscriptionID, r.Association.ProviderPriceID)
	if err == nil {
		return billing.LegacyCheckoutMigrationReceipt{}, billing.ErrRevenueConflict
	}
	if !singleCause(err, billing.ErrRevenueNotFound) {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	reservation := checkoutAcknowledgement{SessionID: r.Association.SessionID, LegacyMigrationID: r.ID}
	if err := insert(ctx, b.tx, kindCheckoutSession, associationKey(r.Association.Scope, r.Association.SessionID, ""), checkoutPartition, reservation); err != nil {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	if err := b.InsertCheckoutAssociation(ctx, r.Association); err != nil {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	if err := insert(ctx, b.tx, kindLegacyCheckoutMigration, r.ID, checkoutPartition, persistLegacyCheckout(r)); err != nil {
		return billing.LegacyCheckoutMigrationReceipt{}, err
	}
	return r, nil
}

var _ billing.LegacyCheckoutMigrationTx = (*checkoutBound)(nil)
