package revenuestore

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// historyRows bounds COMPLETE global source history, independently of report
// filtering. Existing source facts use a global acceptance partition. An indexed
// projection can replace this read only with equivalent completeness proof.
func historyRows(ctx context.Context, tx recordstore.Tx, kind string, budget int) ([]recordstore.Record, error) {
	out := []recordstore.Record{}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		limit := budget - len(out) + 1
		if limit > 200 {
			limit = 200
		}
		page, err := tx.Find(ctx, recordstore.Query{Kind: kind, Partition: partition, AfterID: after, Limit: limit})
		if err != nil {
			return nil, mapped(err)
		}
		if len(page) > limit {
			return nil, billing.ErrRevenueUnavailable
		}
		for _, row := range page {
			if row.ID <= after || row.Kind != kind || row.Partition != partition || row.Revision != 1 || row.ExpiresAt != nil {
				return nil, billing.ErrRevenueUnavailable
			}
			after = row.ID
			out = append(out, row)
		}
		if len(out) > budget {
			return nil, billing.ErrRevenueHistoryTooLarge
		}
		if len(page) < limit {
			return out, nil
		}
	}
}

// ReadRevenueHistory reads canonical facts, all source reception/resolution
// receipts and the sequence head in ONE owning native snapshot. It never
// returns a partial result on late failure, capacity or callback repetition.
func (r *Repository) ReadRevenueHistory(ctx context.Context) (billing.RevenueHistorySnapshot, error) {
	if ctx == nil {
		return billing.RevenueHistorySnapshot{}, billing.ErrRevenueInvalid
	}
	if r == nil || r.store == nil {
		return billing.RevenueHistorySnapshot{}, billing.ErrRevenueUnavailable
	}
	var out billing.RevenueHistorySnapshot
	readCompleted := false
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		readCompleted = false
		out = billing.RevenueHistorySnapshot{Facts: []billing.RevenueFact{}, Observations: []billing.RevenueObservation{}}
		head, row, err := get[sequenceHead](ctx, tx, kindHead, partition, partition)
		if err == nil {
			if row.Sequence != 0 || row.State != "" || row.ExpiresAt != nil || head.Sequence < 1 || row.Revision != head.Sequence {
				return billing.ErrRevenueUnavailable
			}
			out.Sequence = head.Sequence
			out.HeadRevision = row.Revision
		} else if !singleCause(err, billing.ErrRevenueNotFound) {
			return err
		}
		facts, err := historyRows(ctx, tx, kindFact, billing.RevenueHistoryCapacity)
		if err != nil {
			return err
		}
		for _, row := range facts {
			v, err := decodeFact(row)
			if err != nil {
				return err
			}
			if row.State != v.Kind {
				return billing.ErrRevenueUnavailable
			}
			out.Facts = append(out.Facts, v)
		}
		observations, err := historyRows(ctx, tx, kindObservation, billing.RevenueHistoryCapacity-len(facts))
		if err != nil {
			return err
		}
		for _, row := range observations {
			v, err := decodeObservation(row)
			if err != nil {
				return err
			}
			state := "accepted"
			if v.QuarantineReason != "" {
				state = "quarantined"
			}
			if row.Sequence != 0 || row.State != state {
				return billing.ErrRevenueUnavailable
			}
			out.Observations = append(out.Observations, v)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		readCompleted = true
		return nil
	})
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return billing.RevenueHistorySnapshot{}, canceled
		}
		if singleCause(err, billing.ErrRevenueHistoryTooLarge) {
			return billing.RevenueHistorySnapshot{}, err
		}
		// Crypto/driver failures belong to owning read availability. Retain the
		// diagnostic cause without letting internal errors escape unclassified.
		return billing.RevenueHistorySnapshot{}, errors.Join(billing.ErrRevenueUnavailable, mapped(err))
	}
	if err := ctx.Err(); err != nil {
		return billing.RevenueHistorySnapshot{}, err
	}
	if !readCompleted {
		return billing.RevenueHistorySnapshot{}, billing.ErrRevenueUnavailable
	}
	return out, nil
}

var _ billing.RevenueHistoryRepository = (*Repository)(nil)
