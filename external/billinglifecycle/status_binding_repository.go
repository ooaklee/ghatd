package billinglifecycle

import (
	"context"
	"errors"
	"math"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// StatusBinding is one acknowledged transaction, not permission for provider I/O.
// Use its new job handle for the next stage; the consumed handle is stale.
type StatusBinding struct {
	Job   ScheduledJob `json:"-"`
	Input StatusInput  `json:"-"`
}

// StatusBindingRepository atomically retains a native original/evidence and its
// active job pointer. Native provenance and current authority belong to services.
type StatusBindingRepository interface {
	BindStatus(context.Context, LeaseHandle, StatusInput) (StatusBinding, error)
}

func statusMatchesSource(p billing.SubscriptionStatusPreparation, source ScheduledSource) bool {
	return source.Kind == billing.LifecycleSubscriptionSources && p.Scope == source.Scope && p.PrincipalID == source.PrincipalID && p.SubscriptionID == source.SubscriptionID
}

// BindStatus checks the exact execution in the same transaction as both writes.
// No authority or provider call occurs in the retryable callback. Even an exact
// replay consumes the current revision; uncertainty requires reading the job and
// retained input under current authority, not repeating an old handle.
func (r *RecordExecutionRepository) BindStatus(ctx context.Context, h LeaseHandle, input StatusInput) (StatusBinding, error) {
	if err := r.executionReady(ctx); err != nil {
		return StatusBinding{}, err
	}
	if !leaseShape(h) || h.Revision == math.MaxInt64 || !statusMatchesSource(input.Preparation, h.Source) {
		return StatusBinding{}, recordstore.ErrInvalid
	}
	wanted, err := encodeStatus(input)
	if err != nil {
		return StatusBinding{}, err
	}
	detached, err := decodeStatus(wanted, input.Preparation)
	if err != nil {
		return StatusBinding{}, err
	}
	id, sourceDigest := scheduledIdentity(h.Source), digest(scheduleSource(h.Source))
	var out StatusBinding
	err = r.store.Transact(ctx, scheduleJobKind+":"+id, func(tx recordstore.Tx) error {
		out = StatusBinding{}
		j, err := selectedJob(ctx, tx, id, sourceDigest)
		if err != nil {
			return err
		}
		before := r.clock.Now()
		if !exactLease(j, h, before) {
			return recordstore.ErrConflict
		}
		if j.OriginalStatus != nil && digest(statusPreparation(*j.OriginalStatus)) != digest(statusPreparation(detached.Preparation)) {
			return recordstore.ErrConflict
		}
		if detached.Evidence != nil && j.OriginalStatus == nil {
			return recordstore.ErrConflict
		}
		row, readErr := tx.Get(ctx, statusKind, wanted.ID)
		retained := detached
		if readErr != nil {
			if !soleNotFound(readErr) {
				return readErr
			}
			// An attached pointer must never repair a missing retained original, and
			// evidence cannot bootstrap a preparation even with a valid lease.
			if j.OriginalStatus != nil || detached.Evidence != nil {
				return errors.Join(recordstore.ErrUnavailable, readErr)
			}
			if err := tx.Insert(ctx, wanted); err != nil {
				return err
			}
		} else {
			old, err := decodeStatus(row, detached.Preparation)
			if err != nil {
				return err
			}
			retained = old
			if detached.Evidence != nil {
				if old.Evidence != nil {
					if digest(statusPayloadFor(old).Evidence) != digest(statusPayloadFor(detached).Evidence) {
						return recordstore.ErrConflict
					}
				} else {
					if err := tx.Replace(ctx, wanted, row.Revision); err != nil {
						return err
					}
					retained = detached
				}
			}
		}
		p := retained.Preparation
		j.OriginalStatus = &p
		j.Revision++
		jobRow, err := jobRecord(j)
		if err != nil {
			return err
		}
		// If the callback was delayed after its input write, failure here rolls
		// back both kinds. Local time is a conditional-write boundary, not a claim
		// about the network commit instant or another domain's capture transaction.
		now := r.clock.Now()
		if now.IsZero() || now.Before(before) || !h.Until.After(now) {
			return recordstore.ErrConflict
		}
		if err := tx.Replace(ctx, jobRow, h.Revision); err != nil {
			return err
		}
		out = StatusBinding{Job: j, Input: retained}
		return nil
	})
	if err != nil {
		return StatusBinding{}, err
	}
	return out, nil
}
