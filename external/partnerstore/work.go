package partnerstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const (
	kindWorkItem   = "partner_work_item"
	kindWorkCursor = "partner_work_discovery"
	kindWorkPage   = "partner_work_page_receipt"
)

type WorkRepository struct{ store recordstore.Store }
type persistedWork struct {
	Item                                                                          partnermanager.WorkItem
	SourceID, SourceFingerprint, LeaseToken, DecisionActorID, DecisionFingerprint string
	InitialDueAt                                                                  time.Time
}

func NewWorkRepository(store recordstore.Store) (*WorkRepository, error) {
	if nilStoreDependency(store) {
		return nil, partnermanager.ErrUnavailable
	}
	return &WorkRepository{store}, nil
}
func workDigest(parts ...string) string {
	body, _ := json.Marshal(parts)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func workPartition(program, kind string) string { return "partner-work:" + workDigest(program, kind) }
func workMapped(err error) error {
	switch {
	case singleCauseIs(err, recordstore.ErrNotFound):
		return partnermanager.ErrNotFound
	case singleCauseIs(err, recordstore.ErrConflict):
		return partnermanager.ErrWorkConflict
	case singleCauseIs(err, recordstore.ErrInvalid):
		return partnermanager.ErrInvalid
	case errors.Is(err, recordstore.ErrUncertain):
		return fmt.Errorf("%w: %w", partnermanager.ErrWorkUncertain, err)
	case errors.Is(err, recordstore.ErrUnavailable):
		return fmt.Errorf("%w: %w", partnermanager.ErrUnavailable, err)
	default:
		return err
	}
}
func decodeWork(row recordstore.Record) (partnermanager.WorkItem, error) {
	var stored persistedWork
	if err := row.Decode(&stored); err != nil {
		return partnermanager.WorkItem{}, err
	}
	v := stored.Item
	v.SourceID = stored.SourceID
	v.SourceFingerprint = stored.SourceFingerprint
	v.InitialDueAt = stored.InitialDueAt
	v.LeaseToken = stored.LeaseToken
	if v.Decision != nil {
		v.Decision.ActorID = stored.DecisionActorID
		v.Decision.Fingerprint = stored.DecisionFingerprint
		if v.Decision.ID == "" || v.Decision.Fingerprint == "" || v.Decision.ActorID == "" || v.Decision.RecordedAt.IsZero() {
			return partnermanager.WorkItem{}, partnermanager.ErrUnavailable
		}
	}
	if row.Kind != kindWorkItem || row.ID != v.ID || row.Partition != workPartition(v.ProgramID, v.Kind) || row.State != v.State || v.SourceID == "" || v.SourceFingerprint == "" || v.CreatedAt.IsZero() || v.NextAttemptAt.IsZero() || v.NextAttemptAt.Before(v.CreatedAt) || !validStoredWorkDeadline(v) || v.Attempts < 0 || row.Revision < 1 || row.ExpiresAt != nil {
		return partnermanager.WorkItem{}, partnermanager.ErrUnavailable
	}
	return v, nil
}
func workEnvelope(v partnermanager.WorkItem) persistedWork {
	result := persistedWork{Item: v, SourceID: v.SourceID, SourceFingerprint: v.SourceFingerprint, LeaseToken: v.LeaseToken, InitialDueAt: v.InitialDueAt}
	if v.Decision != nil {
		result.DecisionActorID = v.Decision.ActorID
		result.DecisionFingerprint = v.Decision.Fingerprint
	}
	return result
}
func getWork(ctx context.Context, tx recordstore.Tx, program, id string) (partnermanager.WorkItem, recordstore.Record, error) {
	row, err := tx.Get(ctx, kindWorkItem, id)
	if err != nil {
		return partnermanager.WorkItem{}, row, workMapped(err)
	}
	v, err := decodeWork(row)
	if err != nil {
		return v, row, err
	}
	if v.ProgramID != program {
		return partnermanager.WorkItem{}, row, partnermanager.ErrWorkConflict
	}
	return v, row, nil
}
func sameWorkSource(a, b partnermanager.WorkItem) bool {
	return a.ID == b.ID && a.ProgramID == b.ProgramID && a.Kind == b.Kind && a.SourceID == b.SourceID && a.SourceFingerprint == b.SourceFingerprint && a.InitialDueAt.Equal(b.InitialDueAt)
}

func validStoredWorkDeadline(v partnermanager.WorkItem) bool {
	switch v.Kind {
	case partnermanager.WorkMaturity:
		return !v.InitialDueAt.IsZero() && !v.NextAttemptAt.Before(v.InitialDueAt)
	case partnermanager.WorkSignup, partnermanager.WorkRevenue, partnermanager.WorkRevenueSource:
		return v.InitialDueAt.IsZero()
	default:
		return false
	}
}
func writeWork(ctx context.Context, tx recordstore.Tx, v partnermanager.WorkItem, old *recordstore.Record) error {
	revision := int64(1)
	if old != nil {
		if old.Revision == math.MaxInt64 {
			return partnermanager.ErrUnavailable
		}
		revision = old.Revision + 1
	}
	row, err := recordstore.NewRecord(kindWorkItem, v.ID, workPartition(v.ProgramID, v.Kind), revision, workEnvelope(v))
	if err != nil {
		return workMapped(err)
	}
	row.State = v.State
	if old == nil {
		return workMapped(tx.Insert(ctx, row))
	}
	return workMapped(tx.Replace(ctx, row, old.Revision))
}
func getCursor(ctx context.Context, tx recordstore.Tx, program, kind string) (partnermanager.DiscoveryCursor, recordstore.Record, error) {
	row, err := tx.Get(ctx, kindWorkCursor, workDigest(program, kind))
	if err != nil {
		return partnermanager.DiscoveryCursor{}, row, workMapped(err)
	}
	var v partnermanager.DiscoveryCursor
	if err := row.Decode(&v); err != nil {
		return v, row, err
	}
	if row.ID != workDigest(program, kind) || row.Kind != kindWorkCursor || row.Partition != workPartition(program, kind) || row.Revision != v.Revision {
		return v, row, partnermanager.ErrUnavailable
	}
	return v, row, nil
}
func (r *WorkRepository) GetDiscoveryCursor(ctx context.Context, program, kind string) (partnermanager.DiscoveryCursor, error) {
	var result partnermanager.DiscoveryCursor
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		var err error
		result, _, err = getCursor(ctx, tx, program, kind)
		return err
	})
	return result, workMapped(err)
}

// EnqueueWorkPage commits a bounded page and its read position together. Its
// immutable receipt makes lost-ack replay safe even after a later page advances.
func (r *WorkRepository) EnqueueWorkPage(ctx context.Context, program, kind string, expected, next partnermanager.DiscoveryCursor, items []partnermanager.WorkItem) error {
	type source struct {
		ID, Fingerprint string
		DueAt           *time.Time `json:",omitempty"`
	}
	sources := make([]source, 0, len(items))
	for _, item := range items {
		v := source{ID: item.SourceID, Fingerprint: item.SourceFingerprint}
		if !item.InitialDueAt.IsZero() {
			due := item.InitialDueAt.UTC()
			v.DueAt = &due
		}
		sources = append(sources, v)
	}
	body, _ := json.Marshal([]any{program, kind, expected.Revision, expected.AfterID, expected.AfterSequence, next.AfterID, next.AfterSequence, sources})
	sum := sha256.Sum256(body)
	pageID := hex.EncodeToString(sum[:])
	part := workPartition(program, kind)
	return workMapped(r.store.Transact(ctx, part, func(tx recordstore.Tx) error {
		receipt, err := tx.Get(ctx, kindWorkPage, pageID)
		if err == nil {
			if receipt.Partition != part || string(receipt.Data) != string(body) {
				return partnermanager.ErrWorkConflict
			}
			return nil
		}
		if !singleCauseIs(err, recordstore.ErrNotFound) {
			return err
		}
		cursor, old, err := getCursor(ctx, tx, program, kind)
		absent := singleCauseIs(err, partnermanager.ErrNotFound)
		if err != nil && !absent {
			return err
		}
		if cursor != expected || cursor.Revision == math.MaxInt64 {
			return partnermanager.ErrWorkConflict
		}
		for _, item := range items {
			if item.ProgramID != program || item.Kind != kind || item.State != partnermanager.WorkPending || !validStoredWorkDeadline(item) || item.CreatedAt.IsZero() || item.NextAttemptAt.Before(item.CreatedAt) || item.Attempts != 0 || item.Decision != nil || item.LeaseToken != "" {
				return partnermanager.ErrInvalid
			}
			original, _, err := getWork(ctx, tx, program, item.ID)
			if err == nil {
				if !sameWorkSource(original, item) {
					return partnermanager.ErrWorkConflict
				}
				continue
			}
			if !singleCauseIs(err, partnermanager.ErrNotFound) {
				return err
			}
			if err := writeWork(ctx, tx, item, nil); err != nil {
				return err
			}
		}
		next.Revision = expected.Revision + 1
		row, err := recordstore.NewRecord(kindWorkCursor, workDigest(program, kind), part, next.Revision, next)
		if err != nil {
			return err
		}
		if absent {
			err = tx.Insert(ctx, row)
		} else {
			err = tx.Replace(ctx, row, old.Revision)
		}
		if err != nil {
			return err
		}
		row = recordstore.Record{ID: pageID, Kind: kindWorkPage, Partition: part, Revision: 1, Data: body}
		return tx.Insert(ctx, row)
	}))
}
func (r *WorkRepository) GetWorkItem(ctx context.Context, program, id string) (partnermanager.WorkItem, error) {
	var result partnermanager.WorkItem
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		var err error
		result, _, err = getWork(ctx, tx, program, id)
		return err
	})
	return result, workMapped(err)
}
func (r *WorkRepository) LeaseWork(ctx context.Context, program, kind string, now time.Time, duration time.Duration, token string, limit int) ([]partnermanager.WorkItem, error) {
	part := workPartition(program, kind)
	var result []partnermanager.WorkItem
	err := r.store.Transact(ctx, part, func(tx recordstore.Tx) error {
		result = nil
		rows, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindWorkItem, Partition: part, State: partnermanager.WorkPending})
		if err != nil {
			return err
		}
		type due struct {
			item partnermanager.WorkItem
			row  recordstore.Record
		}
		selected := []due{}
		for _, row := range rows {
			item, err := decodeWork(row)
			if err != nil {
				return err
			}
			if item.NextAttemptAt.After(now) || item.LeasedUntil.After(now) {
				continue
			}
			selected = append(selected, due{item, row})
		}
		sort.Slice(selected, func(i, j int) bool {
			a, b := selected[i].item, selected[j].item
			if !a.NextAttemptAt.Equal(b.NextAttemptAt) {
				return a.NextAttemptAt.Before(b.NextAttemptAt)
			}
			return a.ID < b.ID
		})
		if len(selected) > limit {
			selected = selected[:limit]
		}
		for _, candidate := range selected {
			item := candidate.item
			if item.Attempts == math.MaxInt64 {
				return partnermanager.ErrUnavailable
			}
			item.Attempts++
			item.LeaseToken = token + ":" + workDigest(item.ID)[:16]
			item.LeasedUntil = now.Add(duration)
			if err := writeWork(ctx, tx, item, &candidate.row); err != nil {
				return err
			}
			result = append(result, item)
		}
		return nil
	})
	if err != nil {
		return nil, workMapped(err)
	}
	return result, nil
}
func liveWorkLease(original, lease partnermanager.WorkItem, now time.Time) bool {
	return sameWorkSource(original, lease) && original.State == partnermanager.WorkPending && original.LeaseToken == lease.LeaseToken && original.LeaseToken != "" && original.LeasedUntil.After(now)
}
func (r *WorkRepository) RecordWorkDecision(ctx context.Context, lease partnermanager.WorkItem, decision partnermanager.WorkDecision, now time.Time) (partnermanager.WorkDecision, error) {
	var result partnermanager.WorkDecision
	err := r.store.Transact(ctx, workPartition(lease.ProgramID, lease.Kind), func(tx recordstore.Tx) error {
		result = partnermanager.WorkDecision{}
		item, row, err := getWork(ctx, tx, lease.ProgramID, lease.ID)
		if err != nil {
			return err
		}
		if !sameWorkSource(item, lease) {
			return partnermanager.ErrWorkConflict
		}
		if item.Decision != nil {
			if item.Decision.Fingerprint != decision.Fingerprint {
				return partnermanager.ErrWorkConflict
			}
			result = *item.Decision
			return nil
		}
		if !liveWorkLease(item, lease, now) {
			return partnermanager.ErrWorkLeaseLost
		}
		item.Decision = &decision
		if err := writeWork(ctx, tx, item, &row); err != nil {
			return err
		}
		result = decision
		return nil
	})
	return result, workMapped(err)
}
func (r *WorkRepository) RetryWork(ctx context.Context, lease partnermanager.WorkItem, code string, now, next time.Time) error {
	return workMapped(r.store.Transact(ctx, workPartition(lease.ProgramID, lease.Kind), func(tx recordstore.Tx) error {
		item, row, err := getWork(ctx, tx, lease.ProgramID, lease.ID)
		if err != nil {
			return err
		}
		if !liveWorkLease(item, lease, now) {
			return partnermanager.ErrWorkLeaseLost
		}
		item.NextAttemptAt = next
		item.LastErrorCode = code
		item.LeaseToken = ""
		item.LeasedUntil = time.Time{}
		return writeWork(ctx, tx, item, &row)
	}))
}
func (r *WorkRepository) CompleteWork(ctx context.Context, lease partnermanager.WorkItem, decisionID string, now time.Time) error {
	return workMapped(r.store.Transact(ctx, workPartition(lease.ProgramID, lease.Kind), func(tx recordstore.Tx) error {
		item, row, err := getWork(ctx, tx, lease.ProgramID, lease.ID)
		if err != nil {
			return err
		}
		if !sameWorkSource(item, lease) {
			return partnermanager.ErrWorkConflict
		}
		if item.Decision == nil || item.Decision.ID != decisionID {
			return partnermanager.ErrWorkConflict
		}
		if item.State == partnermanager.WorkComplete {
			return nil
		}
		if !liveWorkLease(item, lease, now) {
			return partnermanager.ErrWorkLeaseLost
		}
		item.State = partnermanager.WorkComplete
		item.LeaseToken = ""
		item.LeasedUntil = time.Time{}
		item.LastErrorCode = ""
		return writeWork(ctx, tx, item, &row)
	}))
}

var _ partnermanager.WorkRepository = (*WorkRepository)(nil)
