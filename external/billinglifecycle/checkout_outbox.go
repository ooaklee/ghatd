// Package partnerlifecycle retains private lifecycle recovery inputs. Billing
// owns provenance and status; this package owns only the host's durable handoff.
package billinglifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const checkoutKind = "partners_lifecycle_checkout_input"

// CheckoutValidator must be the configured owning billing manager. Validation
// rechecks current selected refresh authority and original native joins; the
// original preparing actor and saved payload are never authority themselves.
type CheckoutValidator interface {
	ValidateCheckoutLifecycle(context.Context, string, billing.CheckoutIntent) error
}

// CheckoutInput is private recovery state, deliberately unavailable to JSON
// transport. Evidence is nil until an authenticated owning lookup was retained.
// A retained evidence input must be replayed exactly after an uncertain capture.
type CheckoutInput struct {
	Intent   billing.CheckoutIntent                   `json:"-"`
	Evidence *paymentprovider.RevenueCheckoutEvidence `json:"-"`
}

// CheckoutOutbox borrows the already prepared encrypted native store. It does
// not prepare indexes, authorize a worker lease, call a provider or start work.
type CheckoutOutbox struct {
	store     recordstore.Store
	validator CheckoutValidator
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

func NewCheckoutOutbox(store recordstore.Store, validator CheckoutValidator) (*CheckoutOutbox, error) {
	if nilPort(store) || nilPort(validator) {
		return nil, recordstore.ErrUnavailable
	}
	return &CheckoutOutbox{store: store, validator: validator}, nil
}

// Explicit fields preserve the request and fingerprint omitted by billing's
// public JSON tags. This codec is used only inside encrypted record payloads.
type checkoutPayload struct {
	Schema      int
	ID          string
	Scope       billing.RevenueScope
	Request     paymentprovider.CheckoutSessionRequest
	CreatedAt   time.Time
	SessionID   string
	Fingerprint string
	Evidence    *paymentprovider.RevenueCheckoutEvidence
}

func payload(i CheckoutInput) checkoutPayload {
	v := i.Intent
	return checkoutPayload{1, v.ID, v.Scope, v.Request, v.CreatedAt, v.SessionID, v.Fingerprint, i.Evidence}
}

func (p checkoutPayload) input() CheckoutInput {
	return CheckoutInput{Intent: billing.CheckoutIntent{ID: p.ID, Scope: p.Scope, Request: p.Request, CreatedAt: p.CreatedAt, SessionID: p.SessionID, Fingerprint: p.Fingerprint}, Evidence: p.Evidence}
}

func digest(v any) string {
	raw, _ := json.Marshal(v)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func checkoutIdentity(i billing.CheckoutIntent) (string, string) {
	return digest([]any{"partners.lifecycle.checkout.v1", i.Scope, i.ID}), digest([]any{"partners.lifecycle.scope.v1", i.Scope})
}

func encode(i CheckoutInput) (recordstore.Record, error) {
	if err := i.Intent.ValidateAcknowledgedSubscription(); err != nil {
		return recordstore.Record{}, err
	}
	revision, state := int64(1), "prepared"
	if i.Evidence != nil {
		if err := i.Intent.ValidateLifecycleEvidence(*i.Evidence); err != nil {
			return recordstore.Record{}, err
		}
		revision, state = 2, "evidence"
	}
	id, partition := checkoutIdentity(i.Intent)
	r, err := recordstore.NewRecord(checkoutKind, id, partition, revision, payload(i))
	r.State = state
	return r, err
}

func decode(r recordstore.Record, original billing.CheckoutIntent) (CheckoutInput, error) {
	id, partition := checkoutIdentity(original)
	if r.Kind != checkoutKind || r.ID != id || r.Partition != partition || r.Sequence != 0 || r.ExpiresAt != nil {
		return CheckoutInput{}, recordstore.ErrUnavailable
	}
	var p checkoutPayload
	d := json.NewDecoder(bytes.NewReader(r.Data))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Schema != 1 {
		return CheckoutInput{}, recordstore.ErrUnavailable
	}
	i := p.input()
	if i.Intent.ValidateAcknowledgedInput(original) != nil {
		return CheckoutInput{}, recordstore.ErrConflict
	}
	expected, err := encode(i)
	if err != nil || r.Revision != expected.Revision || r.State != expected.State {
		return CheckoutInput{}, recordstore.ErrUnavailable
	}
	return i, nil
}

// Only a genuine single wrapped store absence permits initial insertion.
// Joined dependency failures and Is-only aliases must never become absence.
func soleNotFound(err error) bool {
	for n := 0; n < 64 && err != nil; n++ {
		if err == recordstore.ErrNotFound {
			return true
		}
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return false
		}
		err = errors.Unwrap(err)
	}
	return false
}

func (o *CheckoutOutbox) begin(ctx context.Context, actor string, i billing.CheckoutIntent) error {
	if ctx == nil || i.ValidateAcknowledgedSubscription() != nil {
		return recordstore.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if o == nil || nilPort(o.store) || nilPort(o.validator) {
		return recordstore.ErrUnavailable
	}
	if err := o.validator.ValidateCheckoutLifecycle(ctx, actor, i); err != nil {
		return err
	}
	return ctx.Err()
}

func (o *CheckoutOutbox) finish(ctx context.Context, actor string, i billing.CheckoutIntent, result CheckoutInput, operationErr error) (CheckoutInput, error) {
	withhold := func(err error) (CheckoutInput, error) {
		// Current authority still withholds all data. A previously observed
		// unknown commit must also remain visible to private recovery callers.
		if errors.Is(operationErr, recordstore.ErrUncertain) {
			return CheckoutInput{}, errors.Join(err, operationErr)
		}
		return CheckoutInput{}, err
	}
	if err := ctx.Err(); err != nil {
		return withhold(err)
	}
	if err := o.validator.ValidateCheckoutLifecycle(ctx, actor, i); err != nil {
		return withhold(err)
	}
	if err := ctx.Err(); err != nil {
		return withhold(err)
	}
	if operationErr != nil {
		return CheckoutInput{}, operationErr
	}
	return result, nil
}

// Find reads durable originals under current authority, including on absence
// and errors. Call this before preparing a replacement provider lookup. Outage
// or corruption cannot justify treating the job as new.
func (o *CheckoutOutbox) Find(ctx context.Context, actor string, i billing.CheckoutIntent) (CheckoutInput, error) {
	if err := o.begin(ctx, actor, i); err != nil {
		return CheckoutInput{}, err
	}
	id, _ := checkoutIdentity(i)
	var out CheckoutInput
	err := o.store.Read(ctx, func(tx recordstore.Tx) error {
		out = CheckoutInput{}
		r, err := tx.Get(ctx, checkoutKind, id)
		if err != nil {
			return err
		}
		out, err = decode(r, i)
		return err
	})
	return o.finish(ctx, actor, i, out, err)
}

// RetainPreparation commits the native original before provider I/O. Replays
// recover any already-retained evidence rather than reset the stage. A post-
// commit denial or uncertain write withholds output without proving rollback.
func (o *CheckoutOutbox) RetainPreparation(ctx context.Context, actor string, i billing.CheckoutIntent) (CheckoutInput, error) {
	return o.retain(ctx, actor, CheckoutInput{Intent: i})
}

// RetainEvidence requires preparation already committed. The caller supplies
// authenticated owning Lookup output, not transport data. Replays compare the
// exact original evidence and refuse replacing it with a newer lookup result.
func (o *CheckoutOutbox) RetainEvidence(ctx context.Context, actor string, i billing.CheckoutIntent, evidence paymentprovider.RevenueCheckoutEvidence) (CheckoutInput, error) {
	return o.retain(ctx, actor, CheckoutInput{Intent: i, Evidence: &evidence})
}

func (o *CheckoutOutbox) retain(ctx context.Context, actor string, input CheckoutInput) (CheckoutInput, error) {
	i := input.Intent
	if err := o.begin(ctx, actor, i); err != nil {
		return CheckoutInput{}, err
	}
	wanted, err := encode(input)
	if err != nil {
		return o.finish(ctx, actor, i, CheckoutInput{}, err)
	}
	// Encode then decode detaches maps/pointers before a retryable callback.
	detached, err := decode(wanted, i)
	if err != nil {
		return o.finish(ctx, actor, i, CheckoutInput{}, err)
	}
	var out CheckoutInput
	err = o.store.Transact(ctx, checkoutKind+":"+wanted.ID, func(tx recordstore.Tx) error {
		out = CheckoutInput{}
		r, err := tx.Get(ctx, checkoutKind, wanted.ID)
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
		old, err := decode(r, i)
		if err != nil {
			return err
		}
		if input.Evidence == nil {
			out = old
			return nil
		}
		if old.Evidence != nil {
			if digest(old.Evidence) != digest(detached.Evidence) {
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
