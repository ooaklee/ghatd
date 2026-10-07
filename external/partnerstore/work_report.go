package partnerstore

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

var _ partnermanager.WorkReportingRepository = (*WorkRepository)(nil)

// pendingWorkRecords uses indexed kind, partition and pending state. The shared
// budget bounds current backlog, never lifetime completed history. An extra row
// detects capacity without decoding or projecting a misleading first-N total.
func pendingWorkRecords(ctx context.Context, tx recordstore.Tx, q recordstore.Query, budget *int) ([]recordstore.Record, error) {
	var out []recordstore.Record
	previous := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q.Limit = 200
		if *budget < q.Limit {
			q.Limit = *budget + 1
		}
		q.AfterID = previous
		page, err := tx.Find(ctx, q)
		if err != nil {
			return nil, err
		}
		if len(page) > *budget {
			return nil, partnermanager.ErrReportCapacity
		}
		if len(page) > q.Limit {
			return nil, recordstore.ErrUnavailable
		}
		for _, row := range page {
			if row.Kind != q.Kind || row.Partition != q.Partition || row.State != partnermanager.WorkPending || row.ID <= previous || row.ExpiresAt != nil {
				return nil, recordstore.ErrUnavailable
			}
			previous = row.ID
		}
		*budget -= len(page)
		out = append(out, page...)
		if len(page) < q.Limit {
			return out, nil
		}
	}
}

// ReadWorkSnapshot reads all four pending partitions and their cursors in one
// owning read transaction. Repeated callbacks reset all output and budgets.
// Any failed page, corrupt scope or cursor discards the complete snapshot.
func (r *WorkRepository) ReadWorkSnapshot(ctx context.Context, program string) (partnermanager.WorkSnapshot, error) {
	if ctx == nil {
		return partnermanager.WorkSnapshot{}, partnermanager.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return partnermanager.WorkSnapshot{}, err
	}
	if r == nil || nilStoreDependency(r.store) {
		return partnermanager.WorkSnapshot{}, partnermanager.ErrUnavailable
	}
	if program == "" || len(program) > 128 || strings.TrimSpace(program) != program || strings.ContainsAny(program, "\r\n\x00") {
		return partnermanager.WorkSnapshot{}, partnermanager.ErrInvalid
	}
	var out partnermanager.WorkSnapshot
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = partnermanager.WorkSnapshot{ProgramID: program, Cursors: map[string]partnermanager.DiscoveryCursor{}}
		budget := partnermanager.WorkReportCapacity
		for _, kind := range []string{partnermanager.WorkSignup, partnermanager.WorkRevenue, partnermanager.WorkRevenueSource, partnermanager.WorkMaturity} {
			cursor, cursorRow, err := getCursor(ctx, tx, program, kind)
			if singleCauseIs(err, partnermanager.ErrNotFound) {
				cursor = partnermanager.DiscoveryCursor{}
			} else if err != nil {
				return err
			} else if cursorRow.ExpiresAt != nil || cursor.Revision < 1 {
				return partnermanager.ErrUnavailable
			}
			out.Cursors[kind] = cursor
			rows, err := pendingWorkRecords(ctx, tx, recordstore.Query{Kind: kindWorkItem, Partition: workPartition(program, kind), State: partnermanager.WorkPending}, &budget)
			if err != nil {
				return err
			}
			if len(rows) > 0 && cursor.Revision < 1 {
				return partnermanager.ErrUnavailable
			}
			for _, row := range rows {
				item, err := decodeWork(row)
				if err != nil {
					return err
				}
				if item.ProgramID != program || item.Kind != kind || item.State != partnermanager.WorkPending {
					return partnermanager.ErrUnavailable
				}
				out.Pending = append(out.Pending, item)
			}
		}
		return ctx.Err()
	})
	if err != nil {
		return partnermanager.WorkSnapshot{}, workMapped(err)
	}
	if err := ctx.Err(); err != nil {
		return partnermanager.WorkSnapshot{}, err
	}
	return out, nil
}
