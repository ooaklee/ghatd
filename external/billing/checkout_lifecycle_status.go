package billing

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// CheckoutLifecycleStatus is a fresh authenticated status of one original
// acknowledged checkout. It is not complete lifecycle or financial evidence.
// Only owning lookup output may enter a host disposition; never decode it from
// transport. Recorded expiry is immutable; new dispositions require fresh proof.
type CheckoutLifecycleStatus struct {
	Evidence   paymentprovider.CheckoutStatusEvidence `json:"-"`
	ObservedAt time.Time                              `json:"-"`
}

// ValidateForCheckout checks exact frozen terms and observation chronology.
// Freshness is checked separately at each conditional disposition write.
func (s CheckoutLifecycleStatus) ValidateForCheckout(i CheckoutIntent) error {
	if i.ValidateAcknowledgedSubscription() != nil || i.ValidateCheckoutStatusEvidence(s.Evidence) != nil || s.ObservedAt.IsZero() || s.ObservedAt.Before(i.CreatedAt) || s.ObservedAt.Before(s.Evidence.CreatedAt) {
		return ErrRevenueUnassessable
	}
	return nil
}

// ValidateFresh additionally bounds a new disposition to five minutes after
// its owning observation. Future proof and backward clocks fail closed.
func (s CheckoutLifecycleStatus) ValidateFresh(i CheckoutIntent, now time.Time) error {
	if s.ValidateForCheckout(i) != nil || now.IsZero() || now.Before(s.ObservedAt) || now.Sub(s.ObservedAt) > 5*time.Minute {
		return ErrRevenueUnassessable
	}
	return nil
}

// CheckoutLifecycleExpiry is an explicit nonpayment outcome, never an anchor,
// paid receipt, subscription status or permission. It shares the native intent
// receipt identity with captured lifecycle evidence so both cannot be accepted.
type CheckoutLifecycleExpiry struct {
	IntentID, IntentFingerprint, Fingerprint string                  `json:"-"`
	Status                                   CheckoutLifecycleStatus `json:"-"`
}

// NewCheckoutLifecycleExpiry binds authenticated owning status to an original.
// Shape validation is not authentication: callers must use owning lookup output.
func NewCheckoutLifecycleExpiry(i CheckoutIntent, status CheckoutLifecycleStatus) (CheckoutLifecycleExpiry, error) {
	out := CheckoutLifecycleExpiry{IntentID: i.ID, IntentFingerprint: i.Fingerprint, Status: status}
	out.Fingerprint = checkoutExpiryFingerprint(out)
	if err := out.ValidateForCheckout(i); err != nil {
		return CheckoutLifecycleExpiry{}, err
	}
	return out, nil
}

func checkoutExpiryFingerprint(e CheckoutLifecycleExpiry) string {
	return subscriptionDigest([]any{"checkout.expired.v1", e.IntentID, e.IntentFingerprint, e.Status.Evidence, e.Status.ObservedAt})
}

// ValidateForCheckout accepts only explicit expired/unpaid with known absent
// subscription and exact original terms. Historical reads do not require a new
// provider request or refresh an immutable observation timestamp.
func (e CheckoutLifecycleExpiry) ValidateForCheckout(i CheckoutIntent) error {
	status := e.Status.Evidence
	if e.IntentID != i.ID || e.IntentFingerprint != i.Fingerprint || e.Status.ValidateForCheckout(i) != nil || status.SessionStatus != "expired" || status.PaymentStatus != "unpaid" || !status.SubscriptionAbsent || e.Fingerprint != checkoutExpiryFingerprint(e) {
		return ErrRevenueUnassessable
	}
	return nil
}

// CheckoutExpiryTx is an optional native outcome extension. Reservation and
// host retirement must use the SAME active native transaction; implementations
// validate original joins and contend on the existing per-intent receipt key.
type CheckoutExpiryTx interface {
	ReserveCheckoutLifecycleExpiry(context.Context, CheckoutIntent, CheckoutLifecycleExpiry) error
	GetCheckoutLifecycleExpiry(context.Context, CheckoutIntent) (CheckoutLifecycleExpiry, error)
}

// SupportsCheckoutLifecycleStatus reports an optional provider capability;
// it grants no authority and performs no provider HTTP request.
func (s *CheckoutService) SupportsCheckoutLifecycleStatus(scope RevenueScope) bool {
	if s == nil || ValidateRevenueHistoryScopes([]RevenueScope{scope}) != nil {
		return false
	}
	p, ok := s.provider.(paymentprovider.CheckoutStatusProvider)
	if !ok || revenueNil(p) {
		return false
	}
	if capability, ok := p.(paymentprovider.CheckoutStatusCapability); ok {
		return !revenueNil(capability) && capability.SupportsCheckoutStatus(paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID, LiveMode: scope.LiveMode})
	}
	return true
}

// LookupCheckoutLifecycleStatus revalidates the original and both acknowledgements,
// then performs one authenticated GET-only status lookup outside any transaction.
// It creates no capture, expiry or financial record.
func (s *CheckoutService) LookupCheckoutLifecycleStatus(ctx context.Context, i CheckoutIntent) (CheckoutLifecycleStatus, error) {
	original, err := s.PrepareCheckoutLifecycle(ctx, i)
	if err != nil {
		return CheckoutLifecycleStatus{}, err
	}
	p, ok := s.provider.(paymentprovider.CheckoutStatusProvider)
	if !ok || revenueNil(p) || !s.SupportsCheckoutLifecycleStatus(i.Scope) {
		return CheckoutLifecycleStatus{}, ErrRevenueUnavailable
	}
	before := s.clock.Now().UTC()
	e, err := p.LookupCheckoutStatus(ctx, paymentprovider.RevenueScope{Provider: i.Scope.Provider, AccountID: i.Scope.AccountID, LiveMode: i.Scope.LiveMode}, i.SessionID)
	if canceled := ctx.Err(); canceled != nil {
		return CheckoutLifecycleStatus{}, canceled
	}
	if err != nil {
		return CheckoutLifecycleStatus{}, err
	}
	out := CheckoutLifecycleStatus{Evidence: e, ObservedAt: before}
	if err := out.ValidateFresh(original, s.clock.Now()); err != nil {
		return CheckoutLifecycleStatus{}, err
	}
	return out, nil
}

// FindCheckoutLifecycleExpiry rejoins an immutable explicit expiry with the
// current original acknowledgement. It never treats a captured anchor as expiry.
func (s *CheckoutService) FindCheckoutLifecycleExpiry(ctx context.Context, i CheckoutIntent) (CheckoutLifecycleExpiry, error) {
	if err := s.ready(ctx); err != nil {
		return CheckoutLifecycleExpiry{}, err
	}
	if err := i.ValidateAcknowledgedSubscription(); err != nil {
		return CheckoutLifecycleExpiry{}, err
	}
	var out CheckoutLifecycleExpiry
	err := s.repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		out = CheckoutLifecycleExpiry{}
		if _, err := readAcknowledgedLifecycleIntent(ctx, tx, i, true); err != nil {
			return err
		}
		expiry, ok := tx.(CheckoutExpiryTx)
		if !ok || revenueNil(expiry) {
			return ErrRevenueUnavailable
		}
		var err error
		out, err = expiry.GetCheckoutLifecycleExpiry(ctx, i)
		return err
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return CheckoutLifecycleExpiry{}, err
	}
	return out, nil
}
