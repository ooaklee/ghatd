package referral

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// PaymentAttribution is an immutable association of an economic paid allocation
// with the event-time referral revision. It survives later owner/policy changes
// and is reused for refunds and lost-response financial replay.
type PaymentAttribution struct {
	ID               string        `json:"id" bson:"id"`
	ProgramID        string        `json:"program_id" bson:"program_id"`
	PaymentID        string        `json:"payment_id" bson:"payment_id"`
	ReferredCustomer string        `json:"referred_customer" bson:"referred_customer"`
	ReferralID       string        `json:"referral_id" bson:"referral_id"`
	PartnerID        string        `json:"partner_id" bson:"partner_id"`
	EffectiveAt      time.Time     `json:"effective_at" bson:"effective_at"`
	BoundAt          time.Time     `json:"bound_at" bson:"bound_at"`
	Terms            TermsSnapshot `json:"terms" bson:"terms"`
}

// BindPayment selects the historical owner at the provider's paid timestamp,
// then atomically freezes that selection under the globally scoped economic
// payment ID. It performs no accrual and never overwrites a previous binding.
func (s *Service) BindPayment(ctx context.Context, customer, paymentID string, paidAt time.Time) (PaymentAttribution, error) {
	if err := s.ready(ctx); err != nil {
		return PaymentAttribution{}, err
	}
	if ctx == nil || customer == "" || paymentID == "" || len(paymentID) > 256 || paidAt.IsZero() {
		return PaymentAttribution{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return PaymentAttribution{}, err
	}
	var result PaymentAttribution
	err := s.repo.WithAttributionTransaction(ctx, ProgramID, customer, func(tx Repository) error {
		result = PaymentAttribution{}
		if nilReferralDependency(tx) {
			return ErrUnavailable
		}
		bound := *s
		bound.repo = tx
		var err error
		result, err = bound.bindPayment(ctx, customer, paymentID, paidAt)
		return err
	})
	if err != nil {
		return PaymentAttribution{}, err
	}
	return result, nil
}

// bindPayment freezes the owning referral revision at paidAt under the payment
// ID. An existing binding replays only when customer and effective time match;
// otherwise ErrStaleWrite. No eligible prior revision yields ErrNotFound, and a
// losing insert race is resolved by re-reading and replaying the stored
// binding.
func (s *Service) bindPayment(ctx context.Context, customer, paymentID string, paidAt time.Time) (PaymentAttribution, error) {
	replay := func(old PaymentAttribution) (PaymentAttribution, error) {
		if old.ReferredCustomer != customer || !old.EffectiveAt.Equal(paidAt) {
			return PaymentAttribution{}, ErrStaleWrite
		}
		return old, nil
	}
	if old, err := s.repo.GetPaymentAttribution(ctx, ProgramID, paymentID); err == nil {
		return replay(old)
	} else if !singleReferralCause(err, ErrNotFound) {
		return PaymentAttribution{}, err
	}
	history, err := s.repo.ListReferralHistory(ctx, ProgramID, customer)
	if err != nil {
		return PaymentAttribution{}, err
	}
	var chosen Referral
	for _, r := range history {
		if !r.LockedAt.After(paidAt) && r.Revision > chosen.Revision {
			chosen = r
		}
	}
	if chosen.ID == "" {
		return PaymentAttribution{}, ErrNotFound
	}
	sum := sha256.Sum256([]byte(ProgramID + ":" + paymentID))
	binding := PaymentAttribution{ID: "binding_" + hex.EncodeToString(sum[:]), ProgramID: ProgramID, PaymentID: paymentID, ReferredCustomer: customer, ReferralID: chosen.ID, PartnerID: chosen.PartnerID, EffectiveAt: paidAt.UTC(), BoundAt: s.clock.Now().UTC(), Terms: cloneTerms(chosen.TermsSnapshot)}
	if err := s.repo.InsertPaymentAttribution(ctx, binding); err != nil {
		if singleReferralCause(err, ErrAlreadyExists) {
			old, err := s.repo.GetPaymentAttribution(ctx, ProgramID, paymentID)
			if err != nil {
				return PaymentAttribution{}, err
			}
			return replay(old)
		}
		return PaymentAttribution{}, err
	}
	return binding, nil
}
