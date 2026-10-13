package revenuestore

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// BindCheckoutTransaction borrows an already active native transaction. It
// opens no transaction, copies no codecs and supplies no caller authority.
// Hosts use it only to commit owning expiry and an exact fenced retirement in
// the same native transaction. The caller owns the transaction lifetime.
func BindCheckoutTransaction(tx recordstore.Tx, scope billing.RevenueScope) (billing.CheckoutTx, error) {
	if tx == nil || billing.ValidateRevenueHistoryScopes([]billing.RevenueScope{scope}) != nil {
		return nil, billing.ErrRevenueInvalid
	}
	v := reflect.ValueOf(tx)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Chan, reflect.Func, reflect.Map, reflect.Slice:
		if v.IsNil() {
			return nil, billing.ErrRevenueInvalid
		}
	}
	return &checkoutBound{tx: tx, scope: &scope}, nil
}

// An explicit expiry occupies the existing per-intent receipt key. It is not a
// persistedCheckoutLifecycle anchor: older readers reject its missing anchor
// fields, and older capture writers cannot insert a competing receipt.
type persistedCheckoutExpiry struct {
	Schema                                   int
	Outcome                                  string
	IntentID, IntentFingerprint, Fingerprint string
	Evidence                                 paymentprovider.CheckoutStatusEvidence
	ObservedAt                               time.Time
}

func expiryRecord(i billing.CheckoutIntent, e billing.CheckoutLifecycleExpiry) (recordstore.Record, error) {
	if e.ValidateForCheckout(i) != nil {
		return recordstore.Record{}, billing.ErrRevenueInvalid
	}
	p := persistedCheckoutExpiry{Schema: 2, Outcome: "expired_unpaid", IntentID: e.IntentID, IntentFingerprint: e.IntentFingerprint, Fingerprint: e.Fingerprint, Evidence: e.Status.Evidence, ObservedAt: e.Status.ObservedAt}
	row, err := recordstore.NewRecord(kindCheckoutLifecycleReceipt, i.ID, checkoutPartition, 1, p)
	row.State = "expired_unpaid"
	return row, err
}

func decodeCheckoutExpiry(row recordstore.Record, i billing.CheckoutIntent) (billing.CheckoutLifecycleExpiry, error) {
	if row.Kind != kindCheckoutLifecycleReceipt || row.ID != i.ID || row.Partition != checkoutPartition || row.Revision != 1 || row.State != "expired_unpaid" || row.Sequence != 0 || row.ExpiresAt != nil {
		return billing.CheckoutLifecycleExpiry{}, billing.ErrRevenueUnavailable
	}
	var p persistedCheckoutExpiry
	decoder := json.NewDecoder(bytes.NewReader(row.Data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF || p.Schema != 2 || p.Outcome != "expired_unpaid" {
		return billing.CheckoutLifecycleExpiry{}, billing.ErrRevenueUnavailable
	}
	out := billing.CheckoutLifecycleExpiry{IntentID: p.IntentID, IntentFingerprint: p.IntentFingerprint, Fingerprint: p.Fingerprint, Status: billing.CheckoutLifecycleStatus{Evidence: p.Evidence, ObservedAt: p.ObservedAt}}
	if out.ValidateForCheckout(i) != nil {
		return billing.CheckoutLifecycleExpiry{}, billing.ErrRevenueUnavailable
	}
	return out, nil
}

func (b *checkoutBound) validateExpiryOriginal(ctx context.Context, i billing.CheckoutIntent) error {
	if i.ValidateAcknowledgedSubscription() != nil {
		return billing.ErrRevenueInvalid
	}
	if err := b.checkScope(i.Scope); err != nil {
		return err
	}
	original, err := b.GetCheckoutIntent(ctx, i.ID)
	if err != nil {
		return discoveryJoinedError(err)
	}
	original.SessionID, err = b.GetCheckoutAcknowledgement(ctx, i.ID)
	if err != nil {
		return discoveryJoinedError(err)
	}
	if original.ValidateAcknowledgedInput(i) != nil {
		return billing.ErrRevenueConflict
	}
	return b.ValidateCheckoutAcknowledgement(ctx, original)
}

// ReserveCheckoutLifecycleExpiry validates actual native provenance and
// reserves the existing unique receipt identity. Its caller must also fence
// proof freshness, current host lease and the retained input in THIS transaction.
// Any captured or terminal receipt already present prevents a new reservation.
func (b *checkoutBound) ReserveCheckoutLifecycleExpiry(ctx context.Context, i billing.CheckoutIntent, e billing.CheckoutLifecycleExpiry) error {
	if err := b.validateExpiryOriginal(ctx, i); err != nil {
		return err
	}
	row, err := expiryRecord(i, e)
	if err != nil {
		return err
	}
	_, err = b.tx.Get(ctx, kindCheckoutLifecycleReceipt, i.ID)
	if err == nil {
		return billing.ErrRevenueConflict
	}
	if !singleCause(err, recordstore.ErrNotFound) {
		return mapped(err)
	}
	return mapped(b.tx.Insert(ctx, row))
}

func (b *checkoutBound) GetCheckoutLifecycleExpiry(ctx context.Context, i billing.CheckoutIntent) (billing.CheckoutLifecycleExpiry, error) {
	if err := b.validateExpiryOriginal(ctx, i); err != nil {
		return billing.CheckoutLifecycleExpiry{}, err
	}
	row, err := b.tx.Get(ctx, kindCheckoutLifecycleReceipt, i.ID)
	if err != nil {
		return billing.CheckoutLifecycleExpiry{}, mapped(err)
	}
	return decodeCheckoutExpiry(row, i)
}

var _ billing.CheckoutExpiryTx = (*checkoutBound)(nil)
