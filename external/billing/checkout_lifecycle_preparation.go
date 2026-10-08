package billing

import (
	"context"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// CheckoutAcknowledgementTx is an optional owning snapshot join. New lifecycle
// preparation requires it to validate the acknowledgement's reverse-session
// owner. Legacy checkout adapters remain compatible with existing methods.
type CheckoutAcknowledgementTx interface {
	ValidateCheckoutAcknowledgement(context.Context, CheckoutIntent) error
}

// ValidateAcknowledgedSubscription checks frozen in-process shape, not current
// authority or stored provenance. It does not authorize provider lookup.
func (i CheckoutIntent) ValidateAcknowledgedSubscription() error {
	if !validStoredCheckout(i) || !cleanStatusID(i.SessionID) || i.Request.Mode != paymentprovider.CheckoutModeSubscription {
		return ErrRevenueInvalid
	}
	return nil
}

// ValidateAcknowledgedInput checks equality with a retained original input.
// Both shapes must be canonical; original creation time is not request-hashed.
func (i CheckoutIntent) ValidateAcknowledgedInput(expected CheckoutIntent) error {
	if i.ValidateAcknowledgedSubscription() != nil || expected.ValidateAcknowledgedSubscription() != nil || i.ID != expected.ID || i.Scope != expected.Scope || i.Fingerprint != expected.Fingerprint || i.SessionID != expected.SessionID || !i.CreatedAt.Equal(expected.CreatedAt) {
		return ErrRevenueConflict
	}
	return nil
}

// ValidateLifecycleEvidence checks frozen intent and complete provider evidence
// shape. It does not authenticate a provider, establish ownership or create a
// receipt; those remain owning lookup/capture responsibilities.
func (i CheckoutIntent) ValidateLifecycleEvidence(e paymentprovider.RevenueCheckoutEvidence) error {
	a := CheckoutLifecycleAnchor{IntentID: i.ID, IntentFingerprint: i.Fingerprint, PrincipalID: i.Request.UserID, PlanID: i.Request.PlanID, CostID: i.Request.CostID, Evidence: e, AnchoredAt: e.CreatedAt}
	a.Fingerprint = lifecycleFingerprint(a)
	if i.ValidateAcknowledgedSubscription() != nil || !matchesLifecycleIntent(i, e) || a.Validate() != nil {
		return ErrRevenueUnassessable
	}
	return nil
}

// ValidateForCheckout binds a receipt to one original acknowledged intent.
// A valid receipt alone is neither current authority nor fresh status evidence.
func (a CheckoutLifecycleAnchor) ValidateForCheckout(i CheckoutIntent) error {
	if a.Validate() != nil || i.ValidateLifecycleEvidence(a.Evidence) != nil || a.IntentID != i.ID || a.IntentFingerprint != i.Fingerprint || a.PrincipalID != i.Request.UserID || a.PlanID != i.Request.PlanID || a.CostID != i.Request.CostID {
		return ErrRevenueConflict
	}
	return nil
}

// ValidateCapturedEvidence additionally conserves the exact original provider
// input. It preserves existing receipt fingerprints and ignores retry clocks.
func (a CheckoutLifecycleAnchor) ValidateCapturedEvidence(i CheckoutIntent, e paymentprovider.RevenueCheckoutEvidence) error {
	if a.ValidateForCheckout(i) != nil || i.ValidateLifecycleEvidence(e) != nil {
		return ErrRevenueConflict
	}
	expected := a
	expected.Evidence = e
	if lifecycleFingerprint(expected) != a.Fingerprint {
		return ErrRevenueConflict
	}
	return nil
}

func readAcknowledgedLifecycleIntent(ctx context.Context, tx CheckoutTx, supplied CheckoutIntent, requireReverse bool) (CheckoutIntent, error) {
	original, err := readCheckoutIntent(ctx, tx, supplied.ID)
	if err != nil {
		return CheckoutIntent{}, lifecycleJoinedError(err)
	}
	if original.ValidateAcknowledgedSubscription() != nil {
		return CheckoutIntent{}, ErrRevenueUnavailable
	}
	if original.Scope != supplied.Scope || original.Fingerprint != supplied.Fingerprint || original.SessionID != supplied.SessionID || !original.CreatedAt.Equal(supplied.CreatedAt) {
		return CheckoutIntent{}, ErrRevenueConflict
	}
	join, ok := tx.(CheckoutAcknowledgementTx)
	if !ok || revenueNil(join) {
		if requireReverse {
			return CheckoutIntent{}, ErrRevenueUnavailable
		}
	} else if err := join.ValidateCheckoutAcknowledgement(ctx, original); err != nil {
		return CheckoutIntent{}, lifecycleJoinedError(err)
	}
	if err := ctx.Err(); err != nil {
		return CheckoutIntent{}, err
	}
	return original, nil
}

// PrepareCheckoutLifecycle validates the retained original intent, forward
// acknowledgement and reverse-session ownership in ONE owning snapshot. The
// returned detached input must be durably retained before provider lookup.
// It performs no provider request, financial/status write or receipt creation.
func (s *CheckoutService) PrepareCheckoutLifecycle(ctx context.Context, intent CheckoutIntent) (CheckoutIntent, error) {
	if err := s.ready(ctx); err != nil {
		return CheckoutIntent{}, err
	}
	if err := intent.ValidateAcknowledgedSubscription(); err != nil {
		return CheckoutIntent{}, err
	}
	var out CheckoutIntent
	err := s.repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		out = CheckoutIntent{}
		var err error
		out, err = readAcknowledgedLifecycleIntent(ctx, tx, intent, true)
		return err
	})
	if err != nil {
		return CheckoutIntent{}, err
	}
	if err := ctx.Err(); err != nil {
		return CheckoutIntent{}, err
	}
	return out, nil
}

// FindCheckoutLifecycleReceipt reads an original per-intent receipt and its
// immutable first anchor. Only conclusive receipt absence is NotFound; missing
// joined intent/ack/session/anchor is unavailable. This is recovery before a
// fresh lookup, not permission to overwrite retained uncertain evidence.
func (s *CheckoutService) FindCheckoutLifecycleReceipt(ctx context.Context, intent CheckoutIntent) (CheckoutLifecycleAnchor, error) {
	if err := s.ready(ctx); err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	if err := intent.ValidateAcknowledgedSubscription(); err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	var out CheckoutLifecycleAnchor
	err := s.repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		out = CheckoutLifecycleAnchor{}
		if _, err := readAcknowledgedLifecycleIntent(ctx, tx, intent, true); err != nil {
			return err
		}
		lifecycle, ok := tx.(CheckoutLifecycleTx)
		if !ok || revenueNil(lifecycle) {
			return ErrRevenueUnavailable
		}
		receipt, err := lifecycle.GetCheckoutLifecycleReceipt(ctx, intent.ID)
		if err != nil {
			return err
		}
		if err := validateLifecycleStored(ctx, tx, receipt); err != nil {
			return err
		}
		if receipt.IntentID != intent.ID || receipt.IntentFingerprint != intent.Fingerprint {
			return ErrRevenueConflict
		}
		head, err := readCheckoutLifecycleAnchor(ctx, tx, intent.Scope, receipt.Evidence.SubscriptionID)
		if err != nil {
			return lifecycleJoinedError(err)
		}
		if head.PrincipalID != receipt.PrincipalID || head.Evidence.CustomerID != receipt.Evidence.CustomerID {
			return ErrRevenueConflict
		}
		out = receipt
		return ctx.Err()
	})
	if err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	if err := ctx.Err(); err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	return out, nil
}
