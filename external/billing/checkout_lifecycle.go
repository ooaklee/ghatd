package billing

import (
	"context"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// CheckoutLifecycleAnchor retains authenticated subscription ownership before
// the first payment. It is private lifecycle provenance, never a revenue fact,
// current status, trial entitlement or authorization to accrue commissions.
// Evidence and the original intent fingerprint must be encrypted at rest.
type CheckoutLifecycleAnchor struct {
	IntentID          string                                  `json:"-"`
	IntentFingerprint string                                  `json:"-"`
	PrincipalID       string                                  `json:"-"`
	PlanID            string                                  `json:"-"`
	CostID            string                                  `json:"-"`
	Evidence          paymentprovider.RevenueCheckoutEvidence `json:"-"`
	AnchoredAt        time.Time                               `json:"-"`
	Fingerprint       string                                  `json:"-"`
}

// CheckoutLifecycleTx is an optional extension to CheckoutTx. Its atomic
// insertion retains an immutable per-intent receipt and first subscription
// anchor, refusing disagreement with existing lifecycle or financial ownership.
// Later checkouts for the same payer may retain their own receipt but cannot
// replace the first anchor. All writes use the existing checkout scope guard.
type CheckoutLifecycleTx interface {
	// GetCheckoutLifecycleAnchor returns the first subscription anchor retained for
	// the payer within the supplied revenue scope.
	GetCheckoutLifecycleAnchor(context.Context, RevenueScope, string) (CheckoutLifecycleAnchor, error)
	// GetCheckoutLifecycleReceipt returns the immutable per-intent receipt anchor
	// retained for the supplied checkout intent.
	GetCheckoutLifecycleReceipt(context.Context, string) (CheckoutLifecycleAnchor, error)
	// InsertCheckoutLifecycleAnchor atomically inserts the supplied anchor,
	// refusing disagreement with existing lifecycle or financial ownership.
	InsertCheckoutLifecycleAnchor(context.Context, CheckoutLifecycleAnchor) error
}

// lifecycleScope copies the provider, account and live-mode triple from
// checkout evidence into a RevenueScope.
func lifecycleScope(e paymentprovider.RevenueCheckoutEvidence) RevenueScope {
	return RevenueScope{Provider: e.Scope.Provider, AccountID: e.Scope.AccountID, LiveMode: e.Scope.LiveMode}
}

// lifecycleFingerprint derives the anchor fingerprint from its intent identity,
// owner, plan/cost selection and evidence.
func lifecycleFingerprint(a CheckoutLifecycleAnchor) string {
	return subscriptionDigest([]any{a.IntentID, a.IntentFingerprint, a.PrincipalID, a.PlanID, a.CostID, a.Evidence})
}

// Validate checks internal receipt consistency. Owning intent validation and
// current caller authority remain separate requirements; this is not a token.
func (a CheckoutLifecycleAnchor) Validate() error {
	e := a.Evidence
	if !validRevenueScope(lifecycleScope(e)) || e.IntentID != a.IntentID || e.CreatedAt.IsZero() || a.AnchoredAt.IsZero() || a.AnchoredAt.Before(e.CreatedAt) || e.Status != "complete" || e.Mode != paymentprovider.CheckoutModeSubscription || e.UnitAmountMinor <= 0 || e.IntervalCount != 1 || !revenueCurrency.MatchString(e.Currency) || (e.BillingCadence != "week" && e.BillingCadence != "month" && e.BillingCadence != "year") {
		return ErrRevenueConflict
	}
	for _, value := range []string{a.IntentID, a.IntentFingerprint, a.PrincipalID, a.PlanID, a.CostID, e.SessionID, e.SubscriptionID, e.CustomerID, e.PriceID, e.ClientReferenceID} {
		if !cleanStatusID(value) {
			return ErrRevenueConflict
		}
	}
	if e.ClientReferenceID != a.PrincipalID || a.Fingerprint != lifecycleFingerprint(a) {
		return ErrRevenueConflict
	}
	return nil
}

// matchesLifecycleIntent reports whether stored intent and provider evidence
// agree exactly: same session, intent, scope, subscription mode, references,
// price, currency, amount, cadence, first interval, complete status, and an
// evidence time not before intent creation.
func matchesLifecycleIntent(intent CheckoutIntent, e paymentprovider.RevenueCheckoutEvidence) bool {
	q := intent.Request
	return validStoredCheckout(intent) && intent.SessionID != "" && intent.SessionID == e.SessionID && intent.ID == e.IntentID && intent.Scope == lifecycleScope(e) && q.Mode == paymentprovider.CheckoutModeSubscription && e.Mode == q.Mode && q.UserReference == e.ClientReferenceID && q.PriceID == e.PriceID && strings.ToUpper(q.ExpectedCurrency) == e.Currency && q.ExpectedAmount == e.UnitAmountMinor && string(q.ExpectedBillingCadence) == e.BillingCadence && e.IntervalCount == 1 && e.Status == "complete" && !e.CreatedAt.IsZero() && !e.CreatedAt.Before(intent.CreatedAt.Truncate(time.Second))
}

// A referenced owning record's absence is incomplete provenance, not absence of
// a checkout. Preserve outages and joined errors instead of fabricating recovery.
func lifecycleJoinedError(err error) error {
	if singleRevenueCause(err, ErrRevenueNotFound) {
		return ErrRevenueUnavailable
	}
	return err
}

// validateLifecycleStored re-reads and revalidates the anchor's owning intent
// within the transaction, confirming fingerprint, principal, plan and cost
// agreement plus full evidence matching; acknowledgement joins are validated
// when the optional interface is present. Structural or joined failures map to
// unavailable, disagreement to conflict.
func validateLifecycleStored(ctx context.Context, tx CheckoutTx, a CheckoutLifecycleAnchor) error {
	if a.Validate() != nil {
		return ErrRevenueUnavailable
	}
	intent, err := readCheckoutIntent(ctx, tx, a.IntentID)
	if err != nil {
		return lifecycleJoinedError(err)
	}
	if intent.Fingerprint != a.IntentFingerprint || intent.Request.UserID != a.PrincipalID || intent.Request.PlanID != a.PlanID || intent.Request.CostID != a.CostID || !matchesLifecycleIntent(intent, a.Evidence) {
		return ErrRevenueConflict
	}
	if join, ok := tx.(CheckoutAcknowledgementTx); ok && !revenueNil(join) {
		if err := join.ValidateCheckoutAcknowledgement(ctx, intent); err != nil {
			return lifecycleJoinedError(err)
		}
	}
	return nil
}

// LookupCheckoutLifecycleEvidence retrieves only the exact acknowledged
// session of a retained authorization. Provider I/O occurs outside transactions.
// Hosts must retain the returned original input before attempting capture and
// enforce current owning authority before lookup, capture and disclosure.
func (s *CheckoutService) LookupCheckoutLifecycleEvidence(ctx context.Context, intent CheckoutIntent) (paymentprovider.RevenueCheckoutEvidence, error) {
	if err := s.ready(ctx); err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	if !validStoredCheckout(intent) || intent.SessionID == "" || intent.Request.Mode != paymentprovider.CheckoutModeSubscription {
		return paymentprovider.RevenueCheckoutEvidence{}, ErrRevenueInvalid
	}
	var original CheckoutIntent
	err := s.repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		var err error
		original, err = readAcknowledgedLifecycleIntent(ctx, tx, intent, false)
		return err
	})
	if err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	if original.Fingerprint != intent.Fingerprint || original.SessionID != intent.SessionID {
		return paymentprovider.RevenueCheckoutEvidence{}, ErrRevenueConflict
	}
	if err := ctx.Err(); err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	provider, ok := s.provider.(paymentprovider.RevenueCheckoutSessionEvidenceProvider)
	if !ok || revenueNil(provider) {
		return paymentprovider.RevenueCheckoutEvidence{}, ErrRevenueUnavailable
	}
	e, err := provider.LookupRevenueCheckoutSessionEvidence(ctx, paymentprovider.RevenueScope{Provider: intent.Scope.Provider, AccountID: intent.Scope.AccountID, LiveMode: intent.Scope.LiveMode}, intent.SessionID)
	if canceled := ctx.Err(); canceled != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, canceled
	}
	if err != nil {
		if singleRevenueCause(err, paymentprovider.ErrRevenueUnassessable) {
			return paymentprovider.RevenueCheckoutEvidence{}, ErrRevenueUnassessable
		}
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	a := CheckoutLifecycleAnchor{IntentID: original.ID, IntentFingerprint: original.Fingerprint, PrincipalID: original.Request.UserID, PlanID: original.Request.PlanID, CostID: original.Request.CostID, Evidence: e, AnchoredAt: s.clock.Now().UTC()}
	a.Fingerprint = lifecycleFingerprint(a)
	if !matchesLifecycleIntent(original, e) || a.Validate() != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, ErrRevenueUnassessable
	}
	return e, nil
}

// CaptureCheckoutLifecycleEvidence commits validated original provider evidence
// under its acknowledged intent. Exact retries recover the retained receipt
// without another provider call; changed evidence conflicts. It writes neither
// paid history nor current status. Never decode this input from a browser.
func (s *CheckoutService) CaptureCheckoutLifecycleEvidence(ctx context.Context, intent CheckoutIntent, e paymentprovider.RevenueCheckoutEvidence) (CheckoutLifecycleAnchor, error) {
	if err := s.ready(ctx); err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	if !matchesLifecycleIntent(intent, e) {
		return CheckoutLifecycleAnchor{}, ErrRevenueUnassessable
	}
	a := CheckoutLifecycleAnchor{IntentID: intent.ID, IntentFingerprint: intent.Fingerprint, PrincipalID: intent.Request.UserID, PlanID: intent.Request.PlanID, CostID: intent.Request.CostID, Evidence: e, AnchoredAt: s.clock.Now().UTC()}
	a.Fingerprint = lifecycleFingerprint(a)
	var result CheckoutLifecycleAnchor
	err := s.repo.WithCheckoutTransaction(ctx, intent.Scope, func(tx CheckoutTx) error {
		result = CheckoutLifecycleAnchor{}
		lifecycle, ok := tx.(CheckoutLifecycleTx)
		if !ok || revenueNil(lifecycle) {
			return ErrRevenueUnavailable
		}
		original, err := readAcknowledgedLifecycleIntent(ctx, tx, intent, false)
		if err != nil {
			return lifecycleJoinedError(err)
		}
		if original.Fingerprint != intent.Fingerprint || !matchesLifecycleIntent(original, e) {
			return ErrRevenueConflict
		}
		old, err := lifecycle.GetCheckoutLifecycleReceipt(ctx, intent.ID)
		if err == nil {
			if err := validateLifecycleStored(ctx, tx, old); err != nil {
				return err
			}
			if old.Fingerprint != a.Fingerprint {
				return ErrRevenueConflict
			}
			head, err := lifecycle.GetCheckoutLifecycleAnchor(ctx, intent.Scope, e.SubscriptionID)
			if err != nil {
				return lifecycleJoinedError(err)
			}
			if err := validateLifecycleStored(ctx, tx, head); err != nil {
				return err
			}
			headReceipt, err := lifecycle.GetCheckoutLifecycleReceipt(ctx, head.IntentID)
			if err != nil {
				return lifecycleJoinedError(err)
			}
			if headReceipt.Validate() != nil || headReceipt.Fingerprint != head.Fingerprint || !headReceipt.AnchoredAt.Equal(head.AnchoredAt) {
				return ErrRevenueConflict
			}
			if head.PrincipalID != old.PrincipalID || head.Evidence.CustomerID != old.Evidence.CustomerID {
				return ErrRevenueConflict
			}
			result = old
			return nil
		}
		if !singleRevenueCause(err, ErrRevenueNotFound) {
			return err
		}
		if a.Validate() != nil {
			return ErrRevenueUnassessable
		}
		if err := lifecycle.InsertCheckoutLifecycleAnchor(ctx, a); err != nil {
			return err
		}
		result = a
		return nil
	})
	if err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	return result, nil
}

// FindCheckoutLifecycleAnchor returns the first immutable owning anchor and
// joined per-intent receipt. It does not certify fresh subscription status.
func (s *CheckoutService) FindCheckoutLifecycleAnchor(ctx context.Context, scope RevenueScope, subscription string) (CheckoutLifecycleAnchor, error) {
	if err := s.ready(ctx); err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	if !validRevenueScope(scope) || !cleanStatusID(subscription) {
		return CheckoutLifecycleAnchor{}, ErrRevenueInvalid
	}
	var result CheckoutLifecycleAnchor
	err := s.repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		var err error
		result, err = readCheckoutLifecycleAnchor(ctx, tx, scope, subscription)
		return err
	})
	if err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	return result, nil
}

// readCheckoutLifecycleAnchor joins the immutable first anchor, receipt and
// original acknowledged intent in a single owning checkout snapshot.
func readCheckoutLifecycleAnchor(ctx context.Context, tx CheckoutTx, scope RevenueScope, subscription string) (CheckoutLifecycleAnchor, error) {
	lifecycle, ok := tx.(CheckoutLifecycleTx)
	if !ok || revenueNil(lifecycle) {
		return CheckoutLifecycleAnchor{}, ErrRevenueUnavailable
	}
	a, err := lifecycle.GetCheckoutLifecycleAnchor(ctx, scope, subscription)
	if err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	if lifecycleScope(a.Evidence) != scope || a.Evidence.SubscriptionID != subscription {
		return CheckoutLifecycleAnchor{}, ErrRevenueConflict
	}
	if err := validateLifecycleStored(ctx, tx, a); err != nil {
		return CheckoutLifecycleAnchor{}, err
	}
	receipt, err := lifecycle.GetCheckoutLifecycleReceipt(ctx, a.IntentID)
	if err != nil {
		return CheckoutLifecycleAnchor{}, lifecycleJoinedError(err)
	}
	if receipt.Fingerprint != a.Fingerprint || !receipt.AnchoredAt.Equal(a.AnchoredAt) || receipt.Validate() != nil {
		return CheckoutLifecycleAnchor{}, ErrRevenueConflict
	}
	return a, nil
}
