// Package revenuestore implements the billing-owned verified revenue feed over
// encrypted recordstore transactions. It never parses provider payloads or
// chooses subscription allocations, commission rates or partner ownership.
package revenuestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const (
	kindFact        = "billing_revenue_fact"
	kindObservation = "billing_revenue_observation"
	kindHead        = "billing_revenue_head"
	kindAck         = "billing_revenue_acknowledgement"
	partition       = "billing-verified-revenue-v1"
)

type Repository struct{ store recordstore.Store }
type bound struct{ tx recordstore.Tx }
type sequenceHead struct {
	Sequence int64 `json:"sequence"`
}

// Private request/response fields remain excluded from public JSON, but the
// encrypted persistence envelope explicitly retains internal replay evidence.
type persistedFact struct {
	Fact        billing.RevenueFact `json:"fact"`
	Fingerprint string              `json:"fingerprint"`
}
type persistedObservation struct {
	Observation         billing.RevenueObservation `json:"observation"`
	ResolutionBy        string                     `json:"resolution_by,omitempty"`
	SourceFingerprint   string                     `json:"source_fingerprint,omitempty"`
	RecoveryFingerprint string                     `json:"recovery_fingerprint,omitempty"`
}

func decodeObservation(row recordstore.Record) (billing.RevenueObservation, error) {
	var stored persistedObservation
	if err := row.Decode(&stored); err != nil {
		return billing.RevenueObservation{}, err
	}
	v := stored.Observation
	v.ResolutionBy = stored.ResolutionBy
	v.SourceFingerprint = stored.SourceFingerprint
	v.RecoveryFingerprint = stored.RecoveryFingerprint
	if v.ID != row.ID || row.Kind != kindObservation || row.Partition != partition || v.Fingerprint == "" || v.AcceptedAt.IsZero() || (v.RecoveryFingerprint != "" && (len(v.RecoveryFingerprint) > 256 || strings.TrimSpace(v.RecoveryFingerprint) != v.RecoveryFingerprint || v.ResolutionOf == "")) {
		return billing.RevenueObservation{}, billing.ErrRevenueUnavailable
	}
	return v, nil
}

type persistedAcknowledgement struct {
	Acknowledgement billing.RevenueAcknowledgement `json:"acknowledgement"`
	ActorID         string                         `json:"actor_id"`
}

func decodeFact(row recordstore.Record) (billing.RevenueFact, error) {
	var stored persistedFact
	if err := row.Decode(&stored); err != nil {
		return billing.RevenueFact{}, err
	}
	v := stored.Fact
	v.Fingerprint = stored.Fingerprint
	if v.ID != row.ID || v.Sequence != row.Sequence || v.Sequence < 1 || v.Fingerprint == "" || v.AcceptedAt.IsZero() {
		return billing.RevenueFact{}, billing.ErrRevenueUnavailable
	}
	return v, nil
}

// NewRepository borrows an already prepared transaction-capable store. No
// separate client, nontransactional fallback or in-memory cursor is created.
func NewRepository(store recordstore.Store) (*Repository, error) {
	if store == nil {
		return nil, billing.ErrRevenueUnavailable
	}
	v := reflect.ValueOf(store)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return nil, billing.ErrRevenueUnavailable
	}
	return &Repository{store}, nil
}
func key(parts ...string) string {
	body, _ := json.Marshal(parts)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func ackPartition(consumer string) string { return "billing-revenue-consumer:" + key(consumer) }
func singleCause(err, target error) bool {
	for depth := 0; err != nil && depth < 32; depth++ {
		if err == target {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}
func mapped(err error) error {
	switch {
	case singleCause(err, recordstore.ErrNotFound):
		return billing.ErrRevenueNotFound
	case singleCause(err, recordstore.ErrConflict):
		return billing.ErrRevenueConflict
	case singleCause(err, recordstore.ErrInvalid):
		return fmt.Errorf("%w: %w", billing.ErrRevenueInvalid, err)
	case errors.Is(err, recordstore.ErrUncertain):
		return fmt.Errorf("%w: %w", billing.ErrRevenueUncertain, err)
	case errors.Is(err, recordstore.ErrUnavailable):
		return fmt.Errorf("%w: %w", billing.ErrRevenueUnavailable, err)
	default:
		return err
	}
}
func get[T any](ctx context.Context, tx recordstore.Tx, kind, id, part string) (T, recordstore.Record, error) {
	var value T
	row, err := tx.Get(ctx, kind, id)
	if err != nil {
		return value, row, mapped(err)
	}
	if row.ID != id || row.Kind != kind || row.Partition != part {
		return value, row, billing.ErrRevenueUnavailable
	}
	err = row.Decode(&value)
	return value, row, err
}
func insert(ctx context.Context, tx recordstore.Tx, kind, id, part string, value any) error {
	row, err := recordstore.NewRecord(kind, id, part, 1, value)
	if err != nil {
		return mapped(err)
	}
	return mapped(tx.Insert(ctx, row))
}
func (r *Repository) WithRevenueTransaction(ctx context.Context, fn func(billing.RevenueTx) error) error {
	if ctx == nil || fn == nil {
		return billing.ErrRevenueInvalid
	}
	return mapped(r.store.Transact(ctx, partition, func(tx recordstore.Tx) error { return fn(&bound{tx}) }))
}
func (b *bound) GetObservation(ctx context.Context, id string) (billing.RevenueObservation, error) {
	row, err := b.tx.Get(ctx, kindObservation, id)
	if err != nil {
		return billing.RevenueObservation{}, mapped(err)
	}
	if row.ID != id {
		return billing.RevenueObservation{}, billing.ErrRevenueUnavailable
	}
	return decodeObservation(row)
}
func (b *bound) InsertObservation(ctx context.Context, v billing.RevenueObservation) error {
	row, err := recordstore.NewRecord(kindObservation, v.ID, partition, 1, persistedObservation{Observation: v, ResolutionBy: v.ResolutionBy, SourceFingerprint: v.SourceFingerprint, RecoveryFingerprint: v.RecoveryFingerprint})
	if err != nil {
		return mapped(err)
	}
	if v.QuarantineReason != "" {
		row.State = "quarantined"
	} else {
		row.State = "accepted"
	}
	return mapped(b.tx.Insert(ctx, row))
}
func (b *bound) GetFact(ctx context.Context, id string) (billing.RevenueFact, error) {
	row, err := b.tx.Get(ctx, kindFact, id)
	if err != nil {
		return billing.RevenueFact{}, mapped(err)
	}
	if row.ID != id || row.Kind != kindFact || row.Partition != partition {
		return billing.RevenueFact{}, billing.ErrRevenueUnavailable
	}
	return decodeFact(row)
}
func (b *bound) AppendFact(ctx context.Context, v billing.RevenueFact) (billing.RevenueFact, error) {
	if v.ID == "" || v.Sequence != 0 || v.Fingerprint == "" || v.AcceptedAt.IsZero() {
		return billing.RevenueFact{}, billing.ErrRevenueInvalid
	}
	head, row, err := get[sequenceHead](ctx, b.tx, kindHead, partition, partition)
	revision := int64(1)
	if err == nil {
		if head.Sequence == math.MaxInt64 || row.Revision == math.MaxInt64 {
			return billing.RevenueFact{}, billing.ErrRevenueInvalid
		}
		revision = row.Revision + 1
	} else if !singleCause(err, billing.ErrRevenueNotFound) {
		return billing.RevenueFact{}, err
	}
	head.Sequence++
	v.Sequence = head.Sequence
	next, err := recordstore.NewRecord(kindHead, partition, partition, revision, head)
	if err != nil {
		return billing.RevenueFact{}, mapped(err)
	}
	if revision == 1 {
		err = b.tx.Insert(ctx, next)
	} else {
		err = b.tx.Replace(ctx, next, revision-1)
	}
	if err != nil {
		return billing.RevenueFact{}, mapped(err)
	}
	fact, err := recordstore.NewRecord(kindFact, v.ID, partition, 1, persistedFact{v, v.Fingerprint})
	if err != nil {
		return billing.RevenueFact{}, mapped(err)
	}
	fact.Sequence = v.Sequence
	fact.State = v.Kind
	if err := b.tx.Insert(ctx, fact); err != nil {
		return billing.RevenueFact{}, mapped(err)
	}
	// Legacy verified facts without provider customer evidence remain valid
	// financial records, but cannot become authenticated lifecycle candidates.
	if v.Kind == billing.RevenuePayment && v.ProviderCustomerID != "" {
		if err := retainLifecycleSubscription(ctx, b.tx, lifecycleSubscriptionSource{Scope: v.Scope, SubscriptionID: v.SubscriptionID, PrincipalID: v.PrincipalID, CustomerID: v.ProviderCustomerID, FactID: v.ID, FactFingerprint: v.Fingerprint}); err != nil {
			return billing.RevenueFact{}, err
		}
	}
	if v.Kind == billing.RevenuePayment {
		if err := touchLifecycleSourceEpoch(ctx, b.tx, v.Scope); err != nil {
			return billing.RevenueFact{}, err
		}
	}
	return v, nil
}
func (r *Repository) GetRevenueFact(ctx context.Context, id string) (billing.RevenueFact, error) {
	if ctx == nil || id == "" {
		return billing.RevenueFact{}, billing.ErrRevenueInvalid
	}
	var out billing.RevenueFact
	err := r.store.Read(ctx, func(tx recordstore.Tx) error { var err error; out, err = (&bound{tx}).GetFact(ctx, id); return err })
	return out, mapped(err)
}

// PendingRevenueFacts reads facts and this consumer's acknowledgements in one
// snapshot, then returns the earliest pending sequences. Reading the complete
// indexed history is explicit: a deadline/error returns no partial batch and
// never advances a global cursor past a late or unresolved fact.
func (r *Repository) PendingRevenueFacts(ctx context.Context, consumer string, limit int) ([]billing.RevenueFact, error) {
	return r.PendingRevenueFactsAfter(ctx, consumer, 0, limit)
}

func (r *Repository) PendingRevenueFactsAfter(ctx context.Context, consumer string, afterSequence int64, limit int) ([]billing.RevenueFact, error) {
	if ctx == nil || consumer == "" || afterSequence < 0 || limit < 1 || limit > 200 {
		return nil, billing.ErrRevenueInvalid
	}
	var out []billing.RevenueFact
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = nil
		acks, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindAck, Partition: ackPartition(consumer)})
		if err != nil {
			return err
		}
		consumed := make(map[string]bool, len(acks))
		for _, row := range acks {
			var stored persistedAcknowledgement
			if err := row.Decode(&stored); err != nil {
				return err
			}
			a := stored.Acknowledgement
			a.ActorID = stored.ActorID
			if a.ConsumerID != consumer || row.ID != key(a.ConsumerID, a.FactID) {
				return billing.ErrRevenueUnavailable
			}
			consumed[a.FactID] = true
		}
		facts, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindFact, Partition: partition})
		if err != nil {
			return err
		}
		for _, row := range facts {
			if consumed[row.ID] {
				continue
			}
			v, err := decodeFact(row)
			if err != nil {
				return err
			}
			if v.Sequence > afterSequence {
				out = append(out, v)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
		if len(out) > limit {
			out = out[:limit]
		}
		return nil
	})
	if err != nil {
		return nil, mapped(err)
	}
	return out, nil
}
func (r *Repository) AcknowledgeRevenueFact(ctx context.Context, a billing.RevenueAcknowledgement) error {
	if ctx == nil || a.ConsumerID == "" || a.FactID == "" || a.AcceptanceID == "" || a.ActorID == "" || a.AcceptedAt.IsZero() {
		return billing.ErrRevenueInvalid
	}
	id := key(a.ConsumerID, a.FactID)
	return mapped(r.store.Transact(ctx, ackPartition(a.ConsumerID), func(tx recordstore.Tx) error {
		if _, err := (&bound{tx}).GetFact(ctx, a.FactID); err != nil {
			return err
		}
		stored, _, err := get[persistedAcknowledgement](ctx, tx, kindAck, id, ackPartition(a.ConsumerID))
		old := stored.Acknowledgement
		old.ActorID = stored.ActorID
		if err == nil {
			if old.ConsumerID == a.ConsumerID && old.FactID == a.FactID && old.AcceptanceID == a.AcceptanceID && old.Outcome == a.Outcome && old.ActorID == a.ActorID {
				return nil
			}
			return billing.ErrRevenueConflict
		}
		if !singleCause(err, billing.ErrRevenueNotFound) {
			return err
		}
		return insert(ctx, tx, kindAck, id, ackPartition(a.ConsumerID), persistedAcknowledgement{a, a.ActorID})
	}))
}

func (r *Repository) GetRevenueAcknowledgement(ctx context.Context, consumer, fact string) (billing.RevenueAcknowledgement, error) {
	if ctx == nil || consumer == "" || fact == "" {
		return billing.RevenueAcknowledgement{}, billing.ErrRevenueInvalid
	}
	var result billing.RevenueAcknowledgement
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		result = billing.RevenueAcknowledgement{}
		id, part := key(consumer, fact), ackPartition(consumer)
		stored, row, err := get[persistedAcknowledgement](ctx, tx, kindAck, id, part)
		if err != nil {
			return err
		}
		ack := stored.Acknowledgement
		ack.ActorID = stored.ActorID
		if row.ID != id || row.Kind != kindAck || row.Partition != part || ack.ConsumerID != consumer || ack.FactID != fact || ack.AcceptanceID == "" || ack.ActorID == "" || ack.AcceptedAt.IsZero() {
			return billing.ErrRevenueUnavailable
		}
		switch ack.Outcome {
		case "accepted", "no_entitlement", "quarantined":
		default:
			return billing.ErrRevenueUnavailable
		}
		result = ack
		return nil
	})
	if err != nil {
		return billing.RevenueAcknowledgement{}, mapped(err)
	}
	return result, nil
}

var _ billing.RevenueRepository = (*Repository)(nil)
var _ billing.RevenueAcknowledgementRepository = (*Repository)(nil)
