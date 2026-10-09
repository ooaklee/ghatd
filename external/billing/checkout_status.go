package billing

import (
	"context"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// CheckoutSessionTx optionally resolves the existing reverse-session owner.
// This does not extend CheckoutTx or create a separate ownership catalogue.
type CheckoutSessionTx interface {
	FindCheckoutIntentIDBySession(context.Context, RevenueScope, string) (string, error)
}

// FindAcknowledgedCheckout validates the retained intent and both directions of
// its session acknowledgement in one owning snapshot. Callers must authorize
// the current payer before lookup and must not disclose another payer's input.
func (s *CheckoutService) FindAcknowledgedCheckout(ctx context.Context, scope RevenueScope, session string) (CheckoutIntent, error) {
	if err := s.ready(ctx); err != nil {
		return CheckoutIntent{}, err
	}
	if !validRevenueScope(scope) || !validCheckoutText(session, 256, true) {
		return CheckoutIntent{}, ErrRevenueInvalid
	}
	var out CheckoutIntent
	err := s.repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		out = CheckoutIntent{}
		reverse, ok := tx.(CheckoutSessionTx)
		if !ok || revenueNil(reverse) {
			return ErrRevenueUnavailable
		}
		id, err := reverse.FindCheckoutIntentIDBySession(ctx, scope, session)
		if err != nil {
			return err
		}
		original, err := readCheckoutIntent(ctx, tx, id)
		if err != nil {
			return lifecycleJoinedError(err)
		}
		if original.Scope != scope || original.SessionID != session {
			return ErrRevenueConflict
		}
		join, ok := tx.(CheckoutAcknowledgementTx)
		if !ok || revenueNil(join) {
			return ErrRevenueUnavailable
		}
		if err := join.ValidateCheckoutAcknowledgement(ctx, original); err != nil {
			return lifecycleJoinedError(err)
		}
		out = original
		return ctx.Err()
	})
	if err != nil {
		return CheckoutIntent{}, err
	}
	if err := ctx.Err(); err != nil {
		return CheckoutIntent{}, err
	}
	if !validStoredCheckout(out) || out.Scope != scope || out.SessionID != session {
		return CheckoutIntent{}, ErrRevenueUnavailable
	}
	return out, nil
}

// ValidateCheckoutStatusEvidence matches fresh evidence against the immutable
// original terms. This check never relaxes subscription lifecycle validators,
// creates an acknowledgement, or treats current access as proof of payment.
func (i CheckoutIntent) ValidateCheckoutStatusEvidence(e paymentprovider.CheckoutStatusEvidence) error {
	q := i.Request
	if !validStoredCheckout(i) || i.SessionID == "" || i.SessionID != e.SessionID || i.ID != e.IntentID || i.Scope != (RevenueScope{Provider: e.Scope.Provider, AccountID: e.Scope.AccountID, LiveMode: e.Scope.LiveMode}) || q.UserReference != e.ClientReferenceID || q.PriceID != e.PriceID || strings.ToUpper(q.ExpectedCurrency) != e.Currency || q.ExpectedAmount != e.UnitAmountMinor || q.Mode != e.Mode || string(q.ExpectedBillingCadence) != e.BillingCadence || e.CreatedAt.IsZero() || e.CreatedAt.Before(i.CreatedAt.Truncate(time.Second)) {
		return ErrRevenueUnassessable
	}
	if (q.Mode == paymentprovider.CheckoutModeSubscription && e.IntervalCount != 1) || (q.Mode == paymentprovider.CheckoutModePayment && e.IntervalCount != 0) {
		return ErrRevenueUnassessable
	}
	state, err := e.State()
	if err != nil {
		return ErrRevenueUnassessable
	}
	if state == "no_payment_required" && (q.Mode != paymentprovider.CheckoutModeSubscription || q.TrialPeriodDays <= 0) {
		return ErrRevenueUnassessable
	}
	return nil
}
