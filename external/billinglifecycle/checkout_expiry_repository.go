package billinglifecycle

import (
	"context"
	"math"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// CheckoutExpiryObservation carries owning authenticated status and the exact
// retained original under a live host lease. It is never transport input.
type CheckoutExpiryObservation struct {
	Job    ScheduledJob                    `json:"-"`
	Input  CheckoutInput                   `json:"-"`
	Expiry billing.CheckoutLifecycleExpiry `json:"-"`
}

// CheckoutExpiryRepository is an optional native atomic disposition. Custom
// adapters must not implement it by separate transactions or a best-effort write.
type CheckoutExpiryRepository interface {
	ExpireCheckout(context.Context, CheckoutExpiryObservation) (CompletedExecution, error)
	FindExpiredCheckoutInput(context.Context, billing.CheckoutIntent) (billing.CheckoutLifecycleExpiry, error)
}

// Host private codecs preserve owning proof without reconstructing its
// fingerprint or interpreting the native receipt's persistence format.
type expiryProofPayload struct {
	IntentID, IntentFingerprint, Fingerprint string
	Evidence                                 paymentprovider.CheckoutStatusEvidence
	ObservedAt                               time.Time
}

func expiryProof(e billing.CheckoutLifecycleExpiry) expiryProofPayload {
	return expiryProofPayload{e.IntentID, e.IntentFingerprint, e.Fingerprint, e.Status.Evidence, e.Status.ObservedAt}
}

func (p expiryProofPayload) proof() billing.CheckoutLifecycleExpiry {
	return billing.CheckoutLifecycleExpiry{IntentID: p.IntentID, IntentFingerprint: p.IntentFingerprint, Fingerprint: p.Fingerprint, Status: billing.CheckoutLifecycleStatus{Evidence: p.Evidence, ObservedAt: p.ObservedAt}}
}

type expiredCheckoutPayload struct {
	Schema   int
	Outcome  string
	Original checkoutPayload
	Expiry   expiryProofPayload
}

func expiredCheckoutRecord(i billing.CheckoutIntent, e billing.CheckoutLifecycleExpiry) (recordstore.Record, error) {
	if e.ValidateForCheckout(i) != nil {
		return recordstore.Record{}, recordstore.ErrInvalid
	}
	id, part := checkoutIdentity(i)
	row, err := recordstore.NewRecord(checkoutKind, id, part, 2, expiredCheckoutPayload{2, "expired_unpaid", payload(CheckoutInput{Intent: i}), expiryProof(e)})
	row.State = "expired_unpaid"
	return row, err
}

func decodeExpiredCheckout(row recordstore.Record, i billing.CheckoutIntent) (billing.CheckoutLifecycleExpiry, error) {
	id, part := checkoutIdentity(i)
	if row.Kind != checkoutKind || row.ID != id || row.Partition != part || row.Revision != 2 || row.State != "expired_unpaid" || row.Sequence != 0 || row.ExpiresAt != nil {
		return billing.CheckoutLifecycleExpiry{}, recordstore.ErrUnavailable
	}
	var p expiredCheckoutPayload
	if strictScheduleDecode(row.Data, &p) != nil || p.Schema != 2 || p.Outcome != "expired_unpaid" || p.Original.Schema != 1 || p.Original.Evidence != nil || p.Original.input().Intent.ValidateAcknowledgedInput(i) != nil {
		return billing.CheckoutLifecycleExpiry{}, recordstore.ErrUnavailable
	}
	out := p.Expiry.proof()
	if out.ValidateForCheckout(i) != nil {
		return billing.CheckoutLifecycleExpiry{}, recordstore.ErrUnavailable
	}
	return out, nil
}

func expiryObservationShape(o CheckoutExpiryObservation) bool {
	return validateScheduledJob(o.Job) == nil && leaseShape(JobLease(o.Job)) && o.Job.CheckoutPrepared && o.Job.Source.Kind == billing.LifecycleCheckoutSources && checkoutMatchesSource(o.Input, o.Job.Source) && o.Input.Evidence == nil && o.Expiry.ValidateForCheckout(o.Input.Intent) == nil
}

// ExpireCheckout atomically reserves the native per-intent nonpayment outcome,
// replaces the original outbox with a terminal variant, inserts operational
// completion and retires the exact lease. Shared receipt/input writes close
// races with both native capture and standalone evidence retention.
func (r *RecordExecutionRepository) ExpireCheckout(ctx context.Context, o CheckoutExpiryObservation) (CompletedExecution, error) {
	if err := r.executionReady(ctx); err != nil {
		return CompletedExecution{}, err
	}
	if !expiryObservationShape(o) || o.Job.Revision == math.MaxInt64 {
		return CompletedExecution{}, recordstore.ErrInvalid
	}
	terminal, err := expiredCheckoutRecord(o.Input.Intent, o.Expiry)
	if err != nil {
		return CompletedExecution{}, err
	}
	// Detach all maps before the retryable callback.
	proof, err := decodeExpiredCheckout(terminal, o.Input.Intent)
	if err != nil {
		return CompletedExecution{}, err
	}
	var detached expiredCheckoutPayload
	if strictScheduleDecode(terminal.Data, &detached) != nil {
		return CompletedExecution{}, recordstore.ErrInvalid
	}
	o.Input = detached.Original.input()
	jobRow, err := jobRecord(o.Job)
	if err != nil {
		return CompletedExecution{}, err
	}
	o.Job, err = readJob(jobRow)
	if err != nil {
		return CompletedExecution{}, err
	}
	id, sourceDigest := scheduledIdentity(o.Job.Source), digest(scheduleSource(o.Job.Source))
	key := CompletionKey{o.Job.Source, o.Input.Intent.ID}
	completionID, _ := completionIdentity(key)
	var out CompletedExecution
	err = r.store.Transact(ctx, scheduleJobKind+":"+id, func(tx recordstore.Tx) error {
		out = CompletedExecution{}
		j, err := selectedJob(ctx, tx, id, sourceDigest)
		if err != nil {
			return err
		}
		before := r.clock.Now()
		if !exactLease(j, JobLease(o.Job), before) || !sameExecutionJob(j, o.Job) || j.LastCompletionID != "" || proof.Status.ValidateFresh(o.Input.Intent, before) != nil || before.Before(j.CreatedAt) {
			return recordstore.ErrConflict
		}
		retained, err := tx.Get(ctx, checkoutKind, terminal.ID)
		if err != nil {
			return boundInputError(err)
		}
		input, err := decode(retained, o.Input.Intent)
		if err != nil {
			return err
		}
		if input.Evidence != nil || retained.Revision != 1 {
			return recordstore.ErrConflict
		}
		native, err := revenuestore.BindCheckoutTransaction(tx, o.Input.Intent.Scope)
		if err != nil {
			return err
		}
		if err = native.(billing.CheckoutExpiryTx).ReserveCheckoutLifecycleExpiry(ctx, o.Input.Intent, proof); err != nil {
			return err
		}
		j.Revision++
		j.Lane, j.LastCompletionID = RetiredLane, completionID
		j.LeaseActor, j.LeaseToken, j.LeasedUntil = "", "", time.Time{}
		result := CompletedExecution{Job: j, Expiry: &proof, Receipt: CompletionReceipt{NativeCaptureID: o.Input.Intent.ID, NativeFingerprint: proof.Fingerprint, NativeRequestedAt: o.Input.Intent.CreatedAt, NativeObservedAt: proof.Status.ObservedAt, CompletedAt: before.UTC()}}
		completion, err := encodeCompletion(result)
		if err != nil {
			return err
		}
		result, err = decodeCompletion(completion, key)
		if err != nil {
			return err
		}
		successor, err := jobRecord(result.Job)
		if err != nil {
			return err
		}
		if err = tx.Insert(ctx, completion); err != nil {
			return err
		}
		if err = tx.Replace(ctx, terminal, retained.Revision); err != nil {
			return err
		}
		now := r.clock.Now()
		if now.IsZero() || now.Before(before) || !o.Job.LeasedUntil.After(now) || proof.Status.ValidateFresh(o.Input.Intent, now) != nil {
			return recordstore.ErrConflict
		}
		if err = tx.Replace(ctx, successor, o.Job.Revision); err != nil {
			return err
		}
		out = result
		return nil
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return CompletedExecution{}, err
	}
	return out, nil
}

func (r *RecordExecutionRepository) FindExpiredCheckoutInput(ctx context.Context, i billing.CheckoutIntent) (billing.CheckoutLifecycleExpiry, error) {
	if err := r.executionReady(ctx); err != nil {
		return billing.CheckoutLifecycleExpiry{}, err
	}
	if i.ValidateAcknowledgedSubscription() != nil {
		return billing.CheckoutLifecycleExpiry{}, recordstore.ErrInvalid
	}
	id, _ := checkoutIdentity(i)
	var out billing.CheckoutLifecycleExpiry
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = billing.CheckoutLifecycleExpiry{}
		row, err := tx.Get(ctx, checkoutKind, id)
		if err != nil {
			return boundInputError(err)
		}
		out, err = decodeExpiredCheckout(row, i)
		return err
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return billing.CheckoutLifecycleExpiry{}, err
	}
	return out, nil
}
