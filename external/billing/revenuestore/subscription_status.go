package revenuestore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const (
	kindSubscriptionStatusHead    = "billing_subscription_status_head"
	kindSubscriptionStatusCapture = "billing_subscription_status_capture"
)

// Explicit encrypted DTO: all domain fields are excluded from public JSON.
// Head and capture retain the same evidence; only the head storage revision
// advances. No raw provider response, customer email or TTL is retained.
type persistedSubscriptionStatus struct {
	CaptureID, FactID, FactFingerprint, ActorID     string
	Scope                                           billing.RevenueScope
	PrincipalID, ProviderCustomerID, SubscriptionID string
	ExpectedRevision                                int64
	ExpectedFingerprint                             string
	RequestedAt                                     time.Time
	Status                                          string
	CancellationScheduled                           bool
	ObservedAt                                      time.Time
	Revision                                        int64
	Fingerprint                                     string
}

func persistSubscriptionStatus(v billing.SubscriptionStatus) persistedSubscriptionStatus {
	p := v.Preparation
	return persistedSubscriptionStatus{p.CaptureID, p.FactID, p.FactFingerprint, p.ActorID, p.Scope, p.PrincipalID, p.ProviderCustomerID, p.SubscriptionID, p.ExpectedRevision, p.ExpectedFingerprint, p.RequestedAt, v.Status, v.CancellationScheduled, v.ObservedAt, v.Revision, v.Fingerprint}
}
func (p persistedSubscriptionStatus) domain() billing.SubscriptionStatus {
	return billing.SubscriptionStatus{Preparation: billing.SubscriptionStatusPreparation{CaptureID: p.CaptureID, FactID: p.FactID, FactFingerprint: p.FactFingerprint, ActorID: p.ActorID, Scope: p.Scope, PrincipalID: p.PrincipalID, ProviderCustomerID: p.ProviderCustomerID, SubscriptionID: p.SubscriptionID, ExpectedRevision: p.ExpectedRevision, ExpectedFingerprint: p.ExpectedFingerprint, RequestedAt: p.RequestedAt}, Status: p.Status, CancellationScheduled: p.CancellationScheduled, ObservedAt: p.ObservedAt, Revision: p.Revision, Fingerprint: p.Fingerprint}
}
func statusScopeValid(scope billing.RevenueScope, sub string) bool {
	for _, id := range []string{scope.Provider, scope.AccountID, sub} {
		if strings.TrimSpace(id) != id || id == "" || len(id) > 256 || strings.ContainsAny(id, "\r\n\x00") {
			return false
		}
	}
	return true
}

type subscriptionStatusBound struct {
	tx                     recordstore.Tx
	scope                  billing.RevenueScope
	subscription, identity string
}

func statusBound(tx recordstore.Tx, scope billing.RevenueScope, sub string) *subscriptionStatusBound {
	return &subscriptionStatusBound{tx: tx, scope: scope, subscription: sub, identity: billing.SubscriptionStatusIdentity(scope, sub)}
}
func (b *subscriptionStatusBound) decode(row recordstore.Record, kind, id string) (billing.SubscriptionStatus, error) {
	var stored persistedSubscriptionStatus
	if row.Kind != kind || row.ID != id || row.Partition != b.identity || row.Sequence != 0 || row.ExpiresAt != nil || row.Decode(&stored) != nil {
		return billing.SubscriptionStatus{}, billing.ErrRevenueUnavailable
	}
	v := stored.domain()
	wantRevision := int64(1)
	if kind == kindSubscriptionStatusHead {
		wantRevision = v.Revision
	}
	if row.Revision != wantRevision || row.State != v.Status || v.Validate() != nil || v.Preparation.Scope != b.scope || v.Preparation.SubscriptionID != b.subscription || (kind == kindSubscriptionStatusCapture && v.Preparation.CaptureID != id) {
		return billing.SubscriptionStatus{}, billing.ErrRevenueUnavailable
	}
	return v, nil
}
func (b *subscriptionStatusBound) GetCapture(ctx context.Context, id string) (billing.SubscriptionStatus, error) {
	row, err := b.tx.Get(ctx, kindSubscriptionStatusCapture, id)
	if err != nil {
		if singleCause(err, recordstore.ErrNotFound) {
			return billing.SubscriptionStatus{}, billing.ErrRevenueNotFound
		}
		return billing.SubscriptionStatus{}, errors.Join(billing.ErrRevenueUnavailable, mapped(err))
	}
	return b.decode(row, kindSubscriptionStatusCapture, id)
}
func (b *subscriptionStatusBound) GetCurrent(ctx context.Context) (billing.SubscriptionStatusSnapshot, error) {
	row, err := b.tx.Get(ctx, kindSubscriptionStatusHead, b.identity)
	if err != nil {
		if singleCause(err, recordstore.ErrNotFound) {
			return billing.SubscriptionStatusSnapshot{}, billing.ErrRevenueNotFound
		}
		return billing.SubscriptionStatusSnapshot{}, errors.Join(billing.ErrRevenueUnavailable, mapped(err))
	}
	head, err := b.decode(row, kindSubscriptionStatusHead, b.identity)
	if err != nil {
		return billing.SubscriptionStatusSnapshot{}, err
	}
	receipt, err := b.GetCapture(ctx, head.Preparation.CaptureID)
	// A missing linked receipt is corruption/outage, never first absence.
	if err != nil {
		if singleCause(err, billing.ErrRevenueNotFound) {
			return billing.SubscriptionStatusSnapshot{}, billing.ErrRevenueUnavailable
		}
		return billing.SubscriptionStatusSnapshot{}, errors.Join(billing.ErrRevenueUnavailable, err)
	}
	if head.Fingerprint != receipt.Fingerprint || head.Preparation.CaptureID != receipt.Preparation.CaptureID || head.Revision != receipt.Revision || !head.ObservedAt.Equal(receipt.ObservedAt) {
		return billing.SubscriptionStatusSnapshot{}, billing.ErrRevenueUnavailable
	}
	return billing.SubscriptionStatusSnapshot{Current: head, Receipt: receipt}, nil
}
func (b *subscriptionStatusBound) record(v billing.SubscriptionStatus, kind string) (recordstore.Record, error) {
	if v.Validate() != nil || v.Preparation.Scope != b.scope || v.Preparation.SubscriptionID != b.subscription {
		return recordstore.Record{}, billing.ErrRevenueConflict
	}
	id, rev := v.Preparation.CaptureID, int64(1)
	if kind == kindSubscriptionStatusHead {
		id, rev = b.identity, v.Revision
	}
	r, err := recordstore.NewRecord(kind, id, b.identity, rev, persistSubscriptionStatus(v))
	r.State = v.Status
	return r, mapped(err)
}
func (b *subscriptionStatusBound) InsertCapture(ctx context.Context, v billing.SubscriptionStatus) error {
	r, err := b.record(v, kindSubscriptionStatusCapture)
	if err != nil {
		return err
	}
	return mapped(b.tx.Insert(ctx, r))
}
func (b *subscriptionStatusBound) PutCurrent(ctx context.Context, v billing.SubscriptionStatus, expected int64) error {
	if expected < 0 || v.Preparation.ExpectedRevision != expected {
		return billing.ErrRevenueConflict
	}
	r, err := b.record(v, kindSubscriptionStatusHead)
	if err != nil {
		return err
	}
	if expected == 0 {
		return mapped(b.tx.Insert(ctx, r))
	}
	return mapped(b.tx.Replace(ctx, r, expected))
}

// ReadSubscriptionStatus joins head and immutable receipt in one snapshot.
// A failed/repeated callback never returns partially observed evidence.
func (r *Repository) ReadSubscriptionStatus(ctx context.Context, scope billing.RevenueScope, sub string) (billing.SubscriptionStatusSnapshot, error) {
	if ctx == nil || !statusScopeValid(scope, sub) {
		return billing.SubscriptionStatusSnapshot{}, billing.ErrRevenueInvalid
	}
	if r == nil || r.store == nil {
		return billing.SubscriptionStatusSnapshot{}, billing.ErrRevenueUnavailable
	}
	var out billing.SubscriptionStatusSnapshot
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = billing.SubscriptionStatusSnapshot{}
		var err error
		out, err = statusBound(tx, scope, sub).GetCurrent(ctx)
		return err
	})
	if err != nil {
		return billing.SubscriptionStatusSnapshot{}, mapped(err)
	}
	if err := ctx.Err(); err != nil {
		return billing.SubscriptionStatusSnapshot{}, err
	}
	return out, nil
}

// WithSubscriptionStatusTransaction serializes only this provider/account/mode
// subscription. Status does not consume the economic feed's global sequence.
func (r *Repository) WithSubscriptionStatusTransaction(ctx context.Context, scope billing.RevenueScope, sub string, fn func(billing.SubscriptionStatusTx) error) error {
	if ctx == nil || fn == nil || !statusScopeValid(scope, sub) {
		return billing.ErrRevenueInvalid
	}
	if r == nil || r.store == nil {
		return billing.ErrRevenueUnavailable
	}
	return mapped(r.store.Transact(ctx, billing.SubscriptionStatusIdentity(scope, sub), func(tx recordstore.Tx) error { return fn(statusBound(tx, scope, sub)) }))
}

var _ billing.SubscriptionStatusRepository = (*Repository)(nil)
