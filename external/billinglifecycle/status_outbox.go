package billinglifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const statusKind = "partners_lifecycle_status_input"

// StatusValidator is the configured billing manager, not its raw revenue owner.
// It validates native provenance under current global and selected refresh
// authority. Stored preparing authorship is never permission for recovery.
type StatusValidator interface {
	// ValidateSubscriptionStatusPreparation checks the preparation's native
	// provenance under current global and selected refresh authority, returning an
	// error when validation fails.
	ValidateSubscriptionStatusPreparation(context.Context, string, billing.SubscriptionStatusPreparation) error
}

// StatusInput retains an original observation, private to host recovery. Evidence
// is nil until authenticated owning lookup output has been durably retained.
// Exact original inputs must survive uncertain capture and later observations.
type StatusInput struct {
	Preparation billing.SubscriptionStatusPreparation       `json:"-"`
	Evidence    *billing.VerifiedSubscriptionStatusEvidence `json:"-"`
}

// StatusOutbox borrows the already prepared encrypted native store. It does
// not prepare indexes, authorize a worker lease, call a provider or start work.
type StatusOutbox struct {
	store     recordstore.Store
	validator StatusValidator
}

// NewStatusOutbox borrows prepared storage and the configured owning manager.
// It creates no indexes, service identity, grants or collector goroutines.
func NewStatusOutbox(store recordstore.Store, validator StatusValidator) (*StatusOutbox, error) {
	if nilPort(store) || nilPort(validator) {
		return nil, recordstore.ErrUnavailable
	}
	return &StatusOutbox{store: store, validator: validator}, nil
}

// Explicit private codecs preserve fields deliberately omitted from billing's
// public JSON. They describe durable inputs, never customer request payloads.
type statusPreparationPayload struct {
	Source, CheckoutIntentID, CheckoutFingerprint   string
	CaptureID, FactID, FactFingerprint, ActorID     string
	Scope                                           billing.RevenueScope
	PrincipalID, ProviderCustomerID, SubscriptionID string
	ExpectedRevision                                int64
	ExpectedFingerprint                             string
	RequestedAt                                     time.Time
}

// statusEvidencePayload is the private durable encoding of verified
// subscription status evidence.
type statusEvidencePayload struct {
	Scope                                      billing.RevenueScope
	SubscriptionID, ProviderCustomerID, Status string
	CancellationScheduled                      bool
}

// statusPayload is the private durable encoding of a retained status input and
// its optional evidence.
type statusPayload struct {
	Schema      int
	Preparation statusPreparationPayload
	Evidence    *statusEvidencePayload
}

// statusPreparation converts a preparation to its private durable payload.
func statusPreparation(p billing.SubscriptionStatusPreparation) statusPreparationPayload {
	return statusPreparationPayload{p.Source, p.CheckoutIntentID, p.CheckoutFingerprint, p.CaptureID, p.FactID, p.FactFingerprint, p.ActorID, p.Scope, p.PrincipalID, p.ProviderCustomerID, p.SubscriptionID, p.ExpectedRevision, p.ExpectedFingerprint, p.RequestedAt}
}

// input rebuilds the billing preparation from its private durable payload.
func (p statusPreparationPayload) input() billing.SubscriptionStatusPreparation {
	return billing.SubscriptionStatusPreparation{Source: p.Source, CheckoutIntentID: p.CheckoutIntentID, CheckoutFingerprint: p.CheckoutFingerprint, CaptureID: p.CaptureID, FactID: p.FactID, FactFingerprint: p.FactFingerprint, ActorID: p.ActorID, Scope: p.Scope, PrincipalID: p.PrincipalID, ProviderCustomerID: p.ProviderCustomerID, SubscriptionID: p.SubscriptionID, ExpectedRevision: p.ExpectedRevision, ExpectedFingerprint: p.ExpectedFingerprint, RequestedAt: p.RequestedAt}
}

// statusPayloadFor encodes a status input, including optional evidence, under
// schema 1.
func statusPayloadFor(i StatusInput) statusPayload {
	p := statusPayload{Schema: 1, Preparation: statusPreparation(i.Preparation)}
	if e := i.Evidence; e != nil {
		p.Evidence = &statusEvidencePayload{e.Scope, e.SubscriptionID, e.ProviderCustomerID, e.Status, e.CancellationScheduled}
	}
	return p
}

// input rebuilds the status input from its durable payload, copying optional
// evidence.
func (p statusPayload) input() StatusInput {
	i := StatusInput{Preparation: p.Preparation.input()}
	if e := p.Evidence; e != nil {
		i.Evidence = &billing.VerifiedSubscriptionStatusEvidence{Scope: e.Scope, SubscriptionID: e.SubscriptionID, ProviderCustomerID: e.ProviderCustomerID, Status: e.Status, CancellationScheduled: e.CancellationScheduled}
	}
	return i
}

// statusIdentity derives the status record identity and scope partition from
// preparation scope and capture ID.
func statusIdentity(p billing.SubscriptionStatusPreparation) (string, string) {
	return digest([]any{"partners.lifecycle.status.v1", p.Scope, p.CaptureID}), digest([]any{"partners.lifecycle.scope.v1", p.Scope})
}

// encodeStatus validates the input and encodes it as revision 1 "prepared" or
// revision 2 "evidence" when verified evidence is present.
func encodeStatus(i StatusInput) (recordstore.Record, error) {
	if err := i.Preparation.Validate(); err != nil {
		return recordstore.Record{}, err
	}
	revision, state := int64(1), "prepared"
	if i.Evidence != nil {
		if err := i.Evidence.Validate(i.Preparation); err != nil {
			return recordstore.Record{}, err
		}
		revision, state = 2, "evidence"
	}
	id, partition := statusIdentity(i.Preparation)
	r, err := recordstore.NewRecord(statusKind, id, partition, revision, statusPayloadFor(i))
	r.State = state
	return r, err
}

// decodeStatus strictly decodes a status record for the expected original
// preparation. A different retained preparation is ErrConflict; corruption or
// state drift is ErrUnavailable.
func decodeStatus(r recordstore.Record, original billing.SubscriptionStatusPreparation) (StatusInput, error) {
	id, partition := statusIdentity(original)
	if r.Kind != statusKind || r.ID != id || r.Partition != partition || r.Sequence != 0 || r.ExpiresAt != nil {
		return StatusInput{}, recordstore.ErrUnavailable
	}
	var p statusPayload
	d := json.NewDecoder(bytes.NewReader(r.Data))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Schema != 1 {
		return StatusInput{}, recordstore.ErrUnavailable
	}
	i := p.input()
	if i.Preparation.Validate() != nil || digest(statusPreparation(i.Preparation)) != digest(statusPreparation(original)) {
		return StatusInput{}, recordstore.ErrConflict
	}
	expected, err := encodeStatus(i)
	if err != nil || r.Revision != expected.Revision || r.State != expected.State {
		return StatusInput{}, recordstore.ErrUnavailable
	}
	return i, nil
}

// begin validates the preparation, context and dependencies, then has the
// configured validator authorize the actor before any storage attempt.
func (o *StatusOutbox) begin(ctx context.Context, actor string, i billing.SubscriptionStatusPreparation) error {
	if ctx == nil || i.Validate() != nil {
		return recordstore.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if o == nil || nilPort(o.store) || nilPort(o.validator) {
		return recordstore.ErrUnavailable
	}
	if err := o.validator.ValidateSubscriptionStatusPreparation(ctx, actor, i); err != nil {
		return err
	}
	return ctx.Err()
}

// finish re-validates the preparation under current authority after an
// operation and withholds all data on failure. An uncertain operation error is
// joined so a previously unknown commit stays visible to recovery.
func (o *StatusOutbox) finish(ctx context.Context, actor string, i billing.SubscriptionStatusPreparation, result StatusInput, operationErr error) (StatusInput, error) {
	withhold := func(err error) (StatusInput, error) {
		// Current authority still withholds all data. A previously observed
		// unknown commit must also remain visible to private recovery callers.
		if errors.Is(operationErr, recordstore.ErrUncertain) {
			return StatusInput{}, errors.Join(err, operationErr)
		}
		return StatusInput{}, err
	}
	if err := ctx.Err(); err != nil {
		return withhold(err)
	}
	if err := o.validator.ValidateSubscriptionStatusPreparation(ctx, actor, i); err != nil {
		return withhold(err)
	}
	if err := ctx.Err(); err != nil {
		return withhold(err)
	}
	if operationErr != nil {
		return StatusInput{}, operationErr
	}
	return result, nil
}

// Find reads durable originals under current authority, including on absence
// and errors. Call this before preparing a replacement provider lookup. Outage
// or corruption cannot justify treating the job as new.
func (o *StatusOutbox) Find(ctx context.Context, actor string, i billing.SubscriptionStatusPreparation) (StatusInput, error) {
	if err := o.begin(ctx, actor, i); err != nil {
		return StatusInput{}, err
	}
	id, _ := statusIdentity(i)
	var out StatusInput
	err := o.store.Read(ctx, func(tx recordstore.Tx) error {
		out = StatusInput{}
		r, err := tx.Get(ctx, statusKind, id)
		if err != nil {
			return err
		}
		out, err = decodeStatus(r, i)
		return err
	})
	return o.finish(ctx, actor, i, out, err)
}

// RetainPreparation commits the native original before provider I/O. Replays
// recover any already-retained evidence rather than reset the stage. A post-
// commit denial or uncertain write withholds output without proving rollback.
func (o *StatusOutbox) RetainPreparation(ctx context.Context, actor string, i billing.SubscriptionStatusPreparation) (StatusInput, error) {
	return o.retain(ctx, actor, StatusInput{Preparation: i})
}

// RetainEvidence requires preparation already committed. The caller supplies
// authenticated owning Lookup output, not transport data. Replays compare the
// exact original evidence and refuse replacing it with a newer lookup result.
func (o *StatusOutbox) RetainEvidence(ctx context.Context, actor string, i billing.SubscriptionStatusPreparation, evidence billing.VerifiedSubscriptionStatusEvidence) (StatusInput, error) {
	return o.retain(ctx, actor, StatusInput{Preparation: i, Evidence: &evidence})
}

// retain durably retains the input, detaching the evidence pointer before the
// retryable callback. Absent records accept a first preparation only; existing
// evidence is replayed when identical and conflicts otherwise.
func (o *StatusOutbox) retain(ctx context.Context, actor string, input StatusInput) (StatusInput, error) {
	i := input.Preparation
	if err := o.begin(ctx, actor, i); err != nil {
		return StatusInput{}, err
	}
	wanted, err := encodeStatus(input)
	if err != nil {
		return o.finish(ctx, actor, i, StatusInput{}, err)
	}
	// Encode then decode detaches the evidence pointer before a retryable callback.
	detached, err := decodeStatus(wanted, i)
	if err != nil {
		return o.finish(ctx, actor, i, StatusInput{}, err)
	}
	var out StatusInput
	err = o.store.Transact(ctx, statusKind+":"+wanted.ID, func(tx recordstore.Tx) error {
		out = StatusInput{}
		r, err := tx.Get(ctx, statusKind, wanted.ID)
		if err != nil {
			if !soleNotFound(err) {
				return err
			}
			if input.Evidence != nil {
				return errors.Join(recordstore.ErrUnavailable, err)
			}
			if err := tx.Insert(ctx, wanted); err != nil {
				return err
			}
			out = detached
			return nil
		}
		old, err := decodeStatus(r, i)
		if err != nil {
			return err
		}
		if input.Evidence == nil {
			out = old
			return nil
		}
		if old.Evidence != nil {
			if digest(statusPayloadFor(old).Evidence) != digest(statusPayloadFor(detached).Evidence) {
				return recordstore.ErrConflict
			}
			out = old
			return nil
		}
		if err := tx.Replace(ctx, wanted, r.Revision); err != nil {
			return err
		}
		out = detached
		return nil
	})
	return o.finish(ctx, actor, i, out, err)
}
