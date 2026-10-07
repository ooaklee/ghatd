package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// CheckoutIntent retains the authenticated paying account and exact provider
// request before submission. Request contains personal information and is never
// public JSON; repositories must retain it in encrypted persistence envelopes.
// An acknowledgement is separate from this immutable authorization record.
type CheckoutIntent struct {
	ID          string
	Scope       RevenueScope
	Request     paymentprovider.CheckoutSessionRequest `json:"-"`
	CreatedAt   time.Time
	SessionID   string
	Fingerprint string `json:"-"`
}

// RevenueAssociationRequest selects an immutable historical subscription price
// binding. Invoice/payment identity has already been authenticated by billing.
type RevenueAssociationRequest struct {
	Scope                                                                       RevenueScope
	InvoiceID, PaymentID, CustomerID, SubscriptionID, ProviderPriceID, Currency string
	PaidAt                                                                      time.Time
}
type RevenueAssociation struct{ PrincipalID, PlanID, CostID string }

// CheckoutAssociation is immutable evidence of one server-authorized checkout.
// Later profile/catalogue/provider-metadata changes cannot rewrite it. A portal
// price change requires a separately reviewed historical price association.
type CheckoutAssociation struct {
	IntentID, SessionID                                   string
	Scope                                                 RevenueScope
	CustomerID, SubscriptionID, ProviderPriceID, Currency string
	PrincipalID, PlanID, CostID                           string
	CheckoutCreatedAt, LinkedAt                           time.Time
}

// CheckoutTx commits intent, acknowledgement and subscription association under
// the same scope guard. No provider requests may occur inside a callback.
type CheckoutTx interface {
	GetCheckoutIntent(context.Context, string) (CheckoutIntent, error)
	InsertCheckoutIntent(context.Context, CheckoutIntent) error
	GetCheckoutAcknowledgement(context.Context, string) (string, error)
	InsertCheckoutAcknowledgement(context.Context, string, string) error
	GetCheckoutAssociation(context.Context, RevenueScope, string, string) (CheckoutAssociation, error)
	InsertCheckoutAssociation(context.Context, CheckoutAssociation) error
}
type CheckoutRepository interface {
	WithCheckoutTransaction(context.Context, RevenueScope, func(CheckoutTx) error) error
	ReadCheckout(context.Context, func(CheckoutTx) error) error
}

// CheckoutEvidenceProvider returns authenticated session and complete line-item
// evidence, not an email/current catalogue match. IntentID is only a pointer to
// a previously persisted authorization; it does not establish payer or terms.
type CheckoutEvidenceProvider interface {
	LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error)
}
type CheckoutService struct {
	repo     CheckoutRepository
	clock    RevenueClock
	provider CheckoutEvidenceProvider
}

func NewCheckoutService(repo CheckoutRepository, clock RevenueClock, provider CheckoutEvidenceProvider) (*CheckoutService, error) {
	if revenueNil(repo) || revenueNil(clock) || revenueNil(provider) {
		return nil, ErrRevenueUnavailable
	}
	return &CheckoutService{repo, clock, provider}, nil
}
func checkoutIntentID(scope RevenueScope, key string) string {
	b, _ := json.Marshal([]any{scope, key})
	sum := sha256.Sum256(b)
	return "checkout_" + hex.EncodeToString(sum[:])
}
func checkoutRequestFingerprint(scope RevenueScope, r paymentprovider.CheckoutSessionRequest) string {
	b, _ := json.Marshal([]any{scope, r})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func cloneCheckoutRequest(r paymentprovider.CheckoutSessionRequest) paymentprovider.CheckoutSessionRequest {
	copied := make(map[string]string, len(r.Metadata))
	for k, v := range r.Metadata {
		copied[k] = v
	}
	r.Metadata = copied
	return r
}
func validCheckoutText(s string, max int, required bool) bool {
	return (!required || s != "") && len(s) <= max && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\r\n\x00")
}
func validCheckoutRequest(r paymentprovider.CheckoutSessionRequest) bool {
	if !validCheckoutText(r.IdempotencyKey, 255, true) || !validCheckoutText(r.PriceID, 256, true) || !validCheckoutText(r.PlanID, 256, true) || !validCheckoutText(r.CostID, 256, true) || !validCheckoutText(r.UserID, 256, true) || r.UserReference != r.UserID || !validCheckoutText(r.CustomerEmail, 320, true) || !validCheckoutText(r.ReturnURL, 4096, true) || !validCheckoutText(r.PlanSlug, 256, false) || !validCheckoutText(r.PlanName, 1024, false) || !revenueCurrency.MatchString(strings.ToUpper(r.ExpectedCurrency)) || r.ExpectedAmount <= 0 || r.TrialPeriodDays < 0 || r.TrialPeriodDays > 730 || len(r.Metadata) > 32 {
		return false
	}
	if r.Mode != paymentprovider.CheckoutModePayment && r.Mode != paymentprovider.CheckoutModeSubscription {
		return false
	}
	if (r.Mode == paymentprovider.CheckoutModePayment && r.ExpectedBillingCadence != "one_time") || (r.Mode == paymentprovider.CheckoutModeSubscription && r.ExpectedBillingCadence != "week" && r.ExpectedBillingCadence != "month" && r.ExpectedBillingCadence != "year") {
		return false
	}
	if r.Mode == paymentprovider.CheckoutModePayment && r.TrialPeriodDays != 0 {
		return false
	}
	for k, v := range r.Metadata {
		if !validCheckoutText(k, 40, true) || !validCheckoutText(v, 500, false) {
			return false
		}
	}
	return true
}
func (s *CheckoutService) ready(ctx context.Context) error {
	if err := revenueContext(ctx); err != nil {
		return err
	}
	if s == nil || revenueNil(s.repo) || revenueNil(s.clock) || revenueNil(s.provider) {
		return ErrRevenueUnavailable
	}
	return nil
}
func validStoredCheckout(v CheckoutIntent) bool {
	return validRevenueScope(v.Scope) && !v.CreatedAt.IsZero() && validCheckoutRequest(v.Request) && v.ID == checkoutIntentID(v.Scope, v.Request.IdempotencyKey) && v.Request.Metadata["checkout_intent_id"] == v.ID && v.Fingerprint == checkoutRequestFingerprint(v.Scope, v.Request)
}
func readCheckoutIntent(ctx context.Context, tx CheckoutTx, id string) (CheckoutIntent, error) {
	v, err := tx.GetCheckoutIntent(ctx, id)
	if err != nil {
		return v, err
	}
	if !validStoredCheckout(v) {
		return CheckoutIntent{}, ErrRevenueUnavailable
	}
	session, err := tx.GetCheckoutAcknowledgement(ctx, id)
	if err == nil {
		if !validCheckoutText(session, 256, true) {
			return CheckoutIntent{}, ErrRevenueUnavailable
		}
		v.SessionID = session
	} else if !singleRevenueCause(err, ErrRevenueNotFound) {
		return CheckoutIntent{}, err
	}
	v.Request = cloneCheckoutRequest(v.Request)
	return v, nil
}

// FindCheckoutIntent supports an exact authorized retry before consulting a
// mutable catalogue/profile. The caller must still verify current authority.
func (s *CheckoutService) FindCheckoutIntent(ctx context.Context, scope RevenueScope, key string) (CheckoutIntent, error) {
	if err := s.ready(ctx); err != nil {
		return CheckoutIntent{}, err
	}
	if !validRevenueScope(scope) || !validCheckoutText(key, 255, true) {
		return CheckoutIntent{}, ErrRevenueInvalid
	}
	var result CheckoutIntent
	err := s.repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		var err error
		result, err = readCheckoutIntent(ctx, tx, checkoutIntentID(scope, key))
		return err
	})
	return result, err
}

// PrepareCheckout freezes all submitted parameters before any provider POST.
// A changed payload under an existing key conflicts instead of changing history.
// The 32-entry metadata budget includes the server-owned checkout_intent_id;
// callers must leave one slot for it when that key is absent.
func (s *CheckoutService) PrepareCheckout(ctx context.Context, scope RevenueScope, r paymentprovider.CheckoutSessionRequest) (CheckoutIntent, error) {
	if err := s.ready(ctx); err != nil {
		return CheckoutIntent{}, err
	}
	if !validRevenueScope(scope) || !validCheckoutRequest(r) {
		return CheckoutIntent{}, ErrRevenueInvalid
	}
	r = cloneCheckoutRequest(r)
	id := checkoutIntentID(scope, r.IdempotencyKey)
	r.Metadata["checkout_intent_id"] = id
	if !validCheckoutRequest(r) {
		return CheckoutIntent{}, ErrRevenueInvalid
	}
	proposed := CheckoutIntent{ID: id, Scope: scope, Request: r, CreatedAt: s.clock.Now().UTC(), Fingerprint: checkoutRequestFingerprint(scope, r)}
	if proposed.CreatedAt.IsZero() {
		return CheckoutIntent{}, ErrRevenueInvalid
	}
	var result CheckoutIntent
	err := s.repo.WithCheckoutTransaction(ctx, scope, func(tx CheckoutTx) error {
		result = CheckoutIntent{}
		old, err := readCheckoutIntent(ctx, tx, id)
		if err == nil {
			if old.Fingerprint != proposed.Fingerprint {
				return ErrRevenueConflict
			}
			result = old
			return nil
		}
		if !singleRevenueCause(err, ErrRevenueNotFound) {
			return err
		}
		if err := tx.InsertCheckoutIntent(ctx, proposed); err != nil {
			return err
		}
		result = proposed
		return nil
	})
	return result, err
}

// AcknowledgeCheckout binds a successful provider response. An ambiguous POST
// error does not remove or abandon the intent; authenticated evidence can link
// it later without resubmitting or inventing a successful payment.
func (s *CheckoutService) AcknowledgeCheckout(ctx context.Context, intent CheckoutIntent, session string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if !validStoredCheckout(intent) || !validCheckoutText(session, 256, true) {
		return ErrRevenueInvalid
	}
	return s.repo.WithCheckoutTransaction(ctx, intent.Scope, func(tx CheckoutTx) error {
		original, err := tx.GetCheckoutIntent(ctx, intent.ID)
		if err != nil {
			return err
		}
		if !validStoredCheckout(original) || original.Fingerprint != intent.Fingerprint {
			return ErrRevenueConflict
		}
		return acknowledgeCheckout(ctx, tx, intent.ID, session)
	})
}
func acknowledgeCheckout(ctx context.Context, tx CheckoutTx, id, session string) error {
	old, err := tx.GetCheckoutAcknowledgement(ctx, id)
	if err == nil {
		if old != session {
			return ErrRevenueConflict
		}
		return nil
	}
	if !singleRevenueCause(err, ErrRevenueNotFound) {
		return err
	}
	return tx.InsertCheckoutAcknowledgement(ctx, id, session)
}

// CanSubmitCheckout bounds provider POST retries below Stripe's minimum24-hour
// retention. An acknowledged session must be retrieved, never recreated. Old
// uncertain attempts require reconciliation instead of reusing an expired key.
func (s *CheckoutService) CanSubmitCheckout(ctx context.Context, intent CheckoutIntent) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if !validStoredCheckout(intent) {
		return ErrRevenueInvalid
	}
	age := s.clock.Now().Sub(intent.CreatedAt)
	if intent.SessionID != "" || age < 0 || age >= 23*time.Hour {
		return ErrRevenueUnassessable
	}
	return nil
}
func validAssociationRequest(r RevenueAssociationRequest) bool {
	return validRevenueScope(r.Scope) && validRevenueIdentity(r.InvoiceID) && validRevenueIdentity(r.PaymentID) && validRevenueIdentity(r.CustomerID) && validRevenueIdentity(r.SubscriptionID) && validRevenueIdentity(r.ProviderPriceID) && revenueCurrency.MatchString(r.Currency) && !r.PaidAt.IsZero()
}
func associationResult(a CheckoutAssociation, r RevenueAssociationRequest) (RevenueAssociation, error) {
	if a.Scope != r.Scope || a.SubscriptionID != r.SubscriptionID || a.CustomerID != r.CustomerID || a.ProviderPriceID != r.ProviderPriceID || a.Currency != r.Currency || a.CheckoutCreatedAt.IsZero() || r.PaidAt.Before(a.CheckoutCreatedAt) || !validRevenueIdentity(a.PrincipalID) || !validRevenueIdentity(a.PlanID) || !validRevenueIdentity(a.CostID) {
		return RevenueAssociation{}, ErrRevenueUnassessable
	}
	return RevenueAssociation{a.PrincipalID, a.PlanID, a.CostID}, nil
}

// ResolveRevenueAssociation first recovers immutable owning history. Unknown
// subscriptions require authenticated complete checkout evidence and a matching
// pre-existing intent. Current access, emails and catalogue prices are unused.
func (s *CheckoutService) ResolveRevenueAssociation(ctx context.Context, r RevenueAssociationRequest) (RevenueAssociation, error) {
	if err := s.ready(ctx); err != nil {
		return RevenueAssociation{}, err
	}
	if !validAssociationRequest(r) {
		return RevenueAssociation{}, ErrRevenueInvalid
	}
	var found CheckoutAssociation
	err := s.repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		var err error
		found, err = tx.GetCheckoutAssociation(ctx, r.Scope, r.SubscriptionID, r.ProviderPriceID)
		return err
	})
	if err == nil {
		return associationResult(found, r)
	}
	if !singleRevenueCause(err, ErrRevenueNotFound) {
		return RevenueAssociation{}, err
	}
	evidence, err := s.provider.LookupRevenueCheckout(ctx, paymentprovider.RevenueScope{Provider: r.Scope.Provider, AccountID: r.Scope.AccountID, LiveMode: r.Scope.LiveMode}, r.SubscriptionID)
	if err != nil {
		if singleRevenueCause(err, paymentprovider.ErrRevenueUnassessable) {
			return RevenueAssociation{}, ErrRevenueUnassessable
		}
		return RevenueAssociation{}, err
	}
	if evidence.Scope.Provider != r.Scope.Provider || evidence.Scope.AccountID != r.Scope.AccountID || evidence.Scope.LiveMode != r.Scope.LiveMode || evidence.SubscriptionID != r.SubscriptionID || evidence.CustomerID != r.CustomerID || evidence.PriceID != r.ProviderPriceID || evidence.Currency != r.Currency || evidence.Mode != paymentprovider.CheckoutModeSubscription || evidence.Status != "complete" || evidence.CreatedAt.IsZero() || r.PaidAt.Before(evidence.CreatedAt) || !validCheckoutText(evidence.SessionID, 256, true) || !validCheckoutText(evidence.IntentID, 256, true) {
		return RevenueAssociation{}, ErrRevenueUnassessable
	}
	var result RevenueAssociation
	err = s.repo.WithCheckoutTransaction(ctx, r.Scope, func(tx CheckoutTx) error {
		result = RevenueAssociation{}
		old, err := tx.GetCheckoutAssociation(ctx, r.Scope, r.SubscriptionID, r.ProviderPriceID)
		if err == nil {
			result, err = associationResult(old, r)
			return err
		}
		if !singleRevenueCause(err, ErrRevenueNotFound) {
			return err
		}
		intent, err := readCheckoutIntent(ctx, tx, evidence.IntentID)
		if singleRevenueCause(err, ErrRevenueNotFound) {
			return ErrRevenueUnassessable
		}
		if err != nil {
			return err
		}
		q := intent.Request
		if intent.Scope != r.Scope || q.UserReference != evidence.ClientReferenceID || q.PriceID != evidence.PriceID || strings.ToUpper(q.ExpectedCurrency) != evidence.Currency || q.Mode != evidence.Mode || q.ExpectedAmount != evidence.UnitAmountMinor || string(q.ExpectedBillingCadence) != evidence.BillingCadence || evidence.IntervalCount != 1 || evidence.CreatedAt.Before(intent.CreatedAt.Truncate(time.Second)) || (intent.SessionID != "" && intent.SessionID != evidence.SessionID) {
			return ErrRevenueUnassessable
		}
		a := CheckoutAssociation{IntentID: intent.ID, SessionID: evidence.SessionID, Scope: r.Scope, CustomerID: evidence.CustomerID, SubscriptionID: evidence.SubscriptionID, ProviderPriceID: evidence.PriceID, Currency: evidence.Currency, PrincipalID: q.UserID, PlanID: q.PlanID, CostID: q.CostID, CheckoutCreatedAt: evidence.CreatedAt, LinkedAt: s.clock.Now().UTC()}
		if err := acknowledgeCheckout(ctx, tx, intent.ID, evidence.SessionID); err != nil {
			return err
		}
		if err := tx.InsertCheckoutAssociation(ctx, a); err != nil {
			return err
		}
		result, err = associationResult(a, r)
		return err
	})
	return result, err
}
