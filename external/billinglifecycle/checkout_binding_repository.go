package billinglifecycle

import (
	"context"
	"errors"
	"math"

	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// CheckoutBinding is one acknowledged transaction, not permission for provider I/O.
// Use its new job handle for the next stage; the consumed handle is stale.
type CheckoutBinding struct {
	Job   ScheduledJob  `json:"-"`
	Input CheckoutInput `json:"-"`
}

// CheckoutBindingRepository atomically retains an acknowledged checkout/evidence and its
// active job binding marker. Native provenance and current authority belong to services.
type CheckoutBindingRepository interface {
	BindCheckout(context.Context, LeaseHandle, CheckoutInput) (CheckoutBinding, error)
}

func checkoutMatchesSource(input CheckoutInput, source ScheduledSource) bool {
	return source.Checkout != nil && input.Intent.ValidateAcknowledgedInput(*source.Checkout) == nil
}

// BindCheckout checks the exact execution in the same transaction as both writes.
// No authority or provider call occurs in the retryable callback. Even an exact
// replay consumes the current revision; uncertainty requires reading the job and
// retained input under current authority, not repeating an old handle.
func (r *RecordExecutionRepository) BindCheckout(ctx context.Context, h LeaseHandle, input CheckoutInput) (CheckoutBinding, error) {
	if err := r.executionReady(ctx); err != nil {
		return CheckoutBinding{}, err
	}
	if !leaseShape(h) || h.Revision == math.MaxInt64 || !checkoutMatchesSource(input, h.Source) {
		return CheckoutBinding{}, recordstore.ErrInvalid
	}
	wanted, err := encode(input)
	if err != nil {
		return CheckoutBinding{}, err
	}
	detached, err := decode(wanted, input.Intent)
	if err != nil {
		return CheckoutBinding{}, err
	}
	id, sourceDigest := scheduledIdentity(h.Source), digest(scheduleSource(h.Source))
	var out CheckoutBinding
	err = r.store.Transact(ctx, scheduleJobKind+":"+id, func(tx recordstore.Tx) error {
		out = CheckoutBinding{}
		j, err := selectedJob(ctx, tx, id, sourceDigest)
		if err != nil {
			return err
		}
		before := r.clock.Now()
		if !exactLease(j, h, before) {
			return recordstore.ErrConflict
		}
		if detached.Evidence != nil && !j.CheckoutPrepared {
			return recordstore.ErrConflict
		}
		row, readErr := tx.Get(ctx, checkoutKind, wanted.ID)
		retained := detached
		if readErr != nil {
			if !soleNotFound(readErr) {
				return readErr
			}
			// An attached pointer must never repair a missing retained original, and
			// evidence cannot bootstrap a preparation even with a valid lease.
			if j.CheckoutPrepared || detached.Evidence != nil {
				return errors.Join(recordstore.ErrUnavailable, readErr)
			}
			if err := tx.Insert(ctx, wanted); err != nil {
				return err
			}
		} else {
			old, err := decode(row, detached.Intent)
			if err != nil {
				return err
			}
			retained = old
			if detached.Evidence != nil {
				if old.Evidence != nil {
					if digest(old.Evidence) != digest(detached.Evidence) {
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
		j.CheckoutPrepared = true
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
		out = CheckoutBinding{Job: j, Input: retained}
		return nil
	})
	if err != nil {
		return CheckoutBinding{}, err
	}
	return out, nil
}
