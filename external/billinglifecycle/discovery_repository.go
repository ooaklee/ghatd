package billinglifecycle

import (
	"context"
	"math"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const discoveryCheckpointKind = "partners_lifecycle_discovery"

// DiscoveryCheckpoint is the owning billing projection's continuation, distinct
// from the host's fair job scan. An empty cursor starts another full sweep.
type DiscoveryCheckpoint struct {
	Revision int64  `json:"-"`
	Cursor   string `json:"-"`
}

// DiscoveryCommit admits a canonical page obtained under current authority.
// Shape validation here is not a substitute for the owning manager's read or
// current worker authority before and after the repository operation.
type DiscoveryCommit struct {
	Query    billing.LifecycleDiscoveryQuery `json:"-"`
	Expected DiscoveryCheckpoint             `json:"-"`
	Page     billing.LifecycleDiscoveryPage  `json:"-"`
	Now      time.Time                       `json:"-"`
}

// DiscoveryRepository commits all sources before advancing their checkpoint in
// one transaction. Repeated admission never resets execution or recovery state.
type DiscoveryRepository interface {
	// ReadDiscovery returns the durable discovery checkpoint for a validated query;
	// absence yields an empty checkpoint while storage or context errors return a
	// zero value.
	ReadDiscovery(context.Context, billing.LifecycleDiscoveryQuery) (DiscoveryCheckpoint, error)
	// CommitDiscovery atomically admits a validated discovery page and advances the
	// checkpoint only when it still matches the expected state; repeated admission
	// never resets execution or recovery state.
	CommitDiscovery(context.Context, DiscoveryCommit) error
}

// discoveryCheckpointPayload persists one scope/kind continuation cursor;
// Schema pins the recognized encoding used by checkpoint records.
type discoveryCheckpointPayload struct {
	Schema int
	Scope  billing.RevenueScope
	Kind   string
	Cursor string
}

// discoveryCheckpointID derives the stable checkpoint record identity for one
// scope and source kind.
func discoveryCheckpointID(q billing.LifecycleDiscoveryQuery) string {
	return digest([]any{"partners.lifecycle.discovery.v1", q.Scope, q.Kind})
}

// readDiscoveryCheckpoint loads the discovery continuation inside a
// transaction. A missing record is an empty checkpoint, not an error; any
// corrupted or inconsistent stored row is reported as ErrUnavailable.
func readDiscoveryCheckpoint(ctx context.Context, tx recordstore.Tx, q billing.LifecycleDiscoveryQuery) (DiscoveryCheckpoint, error) {
	id := discoveryCheckpointID(q)
	r, err := tx.Get(ctx, discoveryCheckpointKind, id)
	if soleNotFound(err) {
		return DiscoveryCheckpoint{}, nil
	}
	if err != nil {
		return DiscoveryCheckpoint{}, err
	}
	var p discoveryCheckpointPayload
	if strictScheduleDecode(r.Data, &p) != nil || p.Schema != 1 || p.Scope != q.Scope || p.Kind != q.Kind || r.Kind != discoveryCheckpointKind || r.ID != id || r.Partition != id || r.Revision < 1 || r.State != "" || r.Sequence != 0 || r.ExpiresAt != nil {
		return DiscoveryCheckpoint{}, recordstore.ErrUnavailable
	}
	bound := q
	bound.Cursor = p.Cursor
	if _, err := bound.AfterID(); err != nil {
		return DiscoveryCheckpoint{}, recordstore.ErrUnavailable
	}
	return DiscoveryCheckpoint{r.Revision, p.Cursor}, nil
}

// ReadDiscovery returns the durable discovery checkpoint for a validated query.
// Absence yields an empty checkpoint; storage or context errors return a zero
// value.
func (r *RecordScheduleRepository) ReadDiscovery(ctx context.Context, q billing.LifecycleDiscoveryQuery) (DiscoveryCheckpoint, error) {
	if err := r.ready(ctx); err != nil {
		return DiscoveryCheckpoint{}, err
	}
	if _, err := q.AfterID(); err != nil {
		return DiscoveryCheckpoint{}, err
	}
	var out DiscoveryCheckpoint
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		var err error
		out, err = readDiscoveryCheckpoint(ctx, tx, q)
		return err
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return DiscoveryCheckpoint{}, err
	}
	return out, nil
}

// discoverySource projects a discovery candidate into an immutable private
// ScheduledSource; checkout sources additionally copy the candidate's intent.
func discoverySource(q billing.LifecycleDiscoveryQuery, c billing.LifecycleDiscoveryCandidate) ScheduledSource {
	s := ScheduledSource{Scope: q.Scope, Kind: q.Kind, SourceID: c.ID, PrincipalID: c.PrincipalID, SubscriptionID: c.SubscriptionID, FactID: c.Fact.ID}
	if q.Kind == billing.LifecycleCheckoutSources {
		i := c.Intent
		s.Checkout = &i
	}
	return s
}

// sameDiscoveryOwner permits a subscription projection to gain a first paid
// fact without retargeting the original job. A different nonempty fact or owner
// conflicts. An acknowledged checkout must remain the exact original input.
func sameDiscoveryOwner(prior, next ScheduledSource) bool {
	if prior.FactID != "" && next.FactID != "" && prior.FactID != next.FactID {
		return false
	}
	a, b := scheduleSource(prior), scheduleSource(next)
	a.FactID, b.FactID = "", ""
	return digest(a) == digest(b)
}

// CommitDiscovery atomically admits a validated page and advances the
// checkpoint only when it still matches Expected. Existing jobs are left
// untouched unless sameDiscoveryOwner permits the replay; mismatched cursor,
// revision or owner returns ErrConflict without advancing.
func (r *RecordScheduleRepository) CommitDiscovery(ctx context.Context, c DiscoveryCommit) error {
	if err := r.ready(ctx); err != nil {
		return err
	}
	if c.Now.IsZero() || c.Expected.Revision < 0 || c.Expected.Revision == math.MaxInt64 || c.Query.Cursor != c.Expected.Cursor || (c.Expected.Revision == 0 && c.Expected.Cursor != "") {
		return recordstore.ErrInvalid
	}
	if err := c.Page.Validate(c.Query); err != nil {
		return err
	}
	// Pre-encode private inputs before retryable callbacks; caller-owned maps or
	// page slices must never change the transaction's proposed jobs on retry.
	rows := make([]recordstore.Record, 0, len(c.Page.Items))
	for _, item := range c.Page.Items {
		job := ScheduledJob{Source: discoverySource(c.Query, item), Revision: 1, Lane: ColdLane, CreatedAt: c.Now, NextAttemptAt: c.Now}
		row, err := jobRecord(job)
		if err != nil {
			return err
		}
		rows = append(rows, row)
	}
	q, expected := c.Query, c.Expected
	id := discoveryCheckpointID(q)
	checkpoint, err := recordstore.NewRecord(discoveryCheckpointKind, id, id, expected.Revision+1, discoveryCheckpointPayload{1, q.Scope, q.Kind, c.Page.NextCursor})
	if err != nil {
		return err
	}
	return r.store.Transact(ctx, discoveryCheckpointKind+":"+id, func(tx recordstore.Tx) error {
		old, err := readDiscoveryCheckpoint(ctx, tx, q)
		if err != nil {
			return err
		}
		if old != expected {
			return recordstore.ErrConflict
		}
		for _, row := range rows {
			stored, err := tx.Get(ctx, scheduleJobKind, row.ID)
			if soleNotFound(err) {
				if err := tx.Insert(ctx, row); err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			prior, err := readJob(stored)
			if err != nil {
				return err
			}
			next, err := readJob(row)
			if err != nil {
				return err
			}
			if !sameDiscoveryOwner(prior.Source, next.Source) {
				return recordstore.ErrConflict
			}
			// Existing job is deliberately untouched, including active originals,
			// revisions, leases and retired lane. Discovery is not rescheduling.
		}
		if old.Revision == 0 {
			return tx.Insert(ctx, checkpoint)
		}
		return tx.Replace(ctx, checkpoint, old.Revision)
	})
}
