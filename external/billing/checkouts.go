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

// RevenueAssociation carries the principal, plan and cost identifiers resolved
// from an immutable checkout association.
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
	// GetCheckoutIntent returns the stored checkout intent identified by the
	// supplied ID, without performing provider requests.
	GetCheckoutIntent(context.Context, string) (CheckoutIntent, error)
	// InsertCheckoutIntent persists a checkout intent within the CheckoutTx
	// transaction, which commits intent, acknowledgement and association under one
	// scope guard. The intent is committed with other checkout records when the
	// transaction succeeds.
	InsertCheckoutIntent(context.Context, CheckoutIntent) error
	// GetCheckoutAcknowledgement reads the acknowledgement token stored for the
	// identified checkout within the CheckoutTx transaction, returning it from the
	// owning checkout persistence.
	GetCheckoutAcknowledgement(context.Context, string) (string, error)
	// InsertCheckoutAcknowledgement stores an acknowledgement token for the
	// identified checkout inside the CheckoutTx transaction so it commits
	// atomically with intent and association records.
	InsertCheckoutAcknowledgement(context.Context, string, string) error
	// GetCheckoutAssociation reads a checkout association for the given revenue
	// scope and identifiers within the CheckoutTx transaction, returning the
	// persisted association record.
	GetCheckoutAssociation(context.Context, RevenueScope, string, string) (CheckoutAssociation, error)
	// InsertCheckoutAssociation persists a checkout subscription association inside
	// the CheckoutTx transaction so it commits atomically with intent and
	// acknowledgement records.
	InsertCheckoutAssociation(context.Context, CheckoutAssociation) error
}

// CheckoutRepository opens checkout-scoped transactions and scope-free reads
// over the owning checkout persistence.
type CheckoutRepository interface {
	// WithCheckoutTransaction runs the callback with a CheckoutTx scoped to the
	// given revenue scope, committing intent, acknowledgement and association
	// writes under one scope guard; provider requests must not occur in the
	// callback.
	WithCheckoutTransaction(context.Context, RevenueScope, func(CheckoutTx) error) error
	// ReadCheckout runs the callback with a CheckoutTx for scope-free reads over
	// the owning checkout persistence, without opening a checkout-scoped
	// transaction.
	ReadCheckout(context.Context, func(CheckoutTx) error) error
}

// CheckoutEvidenceProvider returns authenticated session and complete line-item
// evidence, not an email/current catalogue match. IntentID is only a pointer to
// a previously persisted authorization; it does not establish payer or terms.
type CheckoutEvidenceProvider interface {
	// LookupRevenueCheckout returns authenticated session and complete line-item
	// evidence for a checkout within the given revenue scope. IntentID only points
	// to a previously persisted authorization; it does not establish payer or
	// terms.
	LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error)
}

// CheckoutService composes the checkout repository, clock and evidence
// provider; it owns freezing intents and binding acknowledgements, not provider
// verification.
type CheckoutService struct {
	repo     CheckoutRepository
	clock    RevenueClock
	provider CheckoutEvidenceProvider
}

// NewCheckoutService rejects nil repository, clock or provider with
// ErrRevenueUnavailable; otherwise it returns the wired service.
func NewCheckoutService(repo CheckoutRepository, clock RevenueClock, provider CheckoutEvidenceProvider) (*CheckoutService, error) {
	if revenueNil(repo) || revenueNil(clock) || revenueNil(provider) {
		return nil, ErrRevenueUnavailable
	}
	return &CheckoutService{repo, clock, provider}, nil
}

// checkoutIntentID derives the deterministic "checkout_"-prefixed intent key
// from scope and idempotency key.
func checkoutIntentID(scope RevenueScope, key string) string {
	b, _ := json.Marshal([]any{scope, key})
	sum := sha256.Sum256(b)
	return "checkout_" + hex.EncodeToString(sum[:])
}

// checkoutRequestFingerprint hashes scope plus the full checkout request into a
// stable identity used to detect changed retries.
func checkoutRequestFingerprint(scope RevenueScope, r paymentprovider.CheckoutSessionRequest) string {
	b, _ := json.Marshal([]any{scope, r})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// cloneCheckoutRequest returns the request with a copied metadata map so
// callers cannot mutate shared stored metadata.
func cloneCheckoutRequest(r paymentprovider.CheckoutSessionRequest) paymentprovider.CheckoutSessionRequest {
	copied := make(map[string]string, len(r.Metadata))
	for k, v := range r.Metadata {
		copied[k] = v
	}
	r.Metadata = copied
	return r
}

// validCheckoutText accepts strings within max length that carry no surrounding
// whitespace or control/NUL characters; empty values pass only when not
// required.
func validCheckoutText(s string, max int, required bool) bool {
	return (!required || s != "") && len(s) <= max && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\r\n\x00")
}

// validCheckoutRequest enforces the frozen request contract: bounded well-
// formed identities and URLs, matching user reference and user ID, supported
// mode/cadence pairing, positive amount, valid currency, trials only for
// subscriptions capped at 730 days, and at most 32 metadata entries with
// bounded keys/values.
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

// ready verifies a non-cancelled context and that the service and its
// repository, clock and provider are populated, returning ErrRevenueUnavailable
// otherwise.
func (s *CheckoutService) ready(ctx context.Context) error {
	if err := revenueContext(ctx); err != nil {
		return err
	}
	if s == nil || revenueNil(s.repo) || revenueNil(s.clock) || revenueNil(s.provider) {
		return ErrRevenueUnavailable
	}
	return nil
}

// validStoredCheckout recomputes the intent's ID, server-owned metadata slot
// and request fingerprint, accepting only structurally valid, self-consistent
// stored intents.
func validStoredCheckout(v CheckoutIntent) bool {
	return validRevenueScope(v.Scope) && !v.CreatedAt.IsZero() && validCheckoutRequest(v.Request) && v.ID == checkoutIntentID(v.Scope, v.Request.IdempotencyKey) && v.Request.Metadata["checkout_intent_id"] == v.ID && v.Fingerprint == checkoutRequestFingerprint(v.Scope, v.Request)
}

// readCheckoutIntent loads a stored intent, rejects any record failing self-
// consistency as unavailable, joins a well-formed session acknowledgement when
// present, and returns the request with cloned metadata. Only a conclusive
// acknowledgement absence is tolerated.
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

// acknowledgeCheckout is idempotent for the identical session and conflicts on
// a different one; any non-absence read error is returned unchanged before
// insertion.
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

// validAssociationRequest checks scope plus identity-shaped invoice, payment,
// customer, subscription and price IDs, a supported currency and a nonzero paid
// time.
func validAssociationRequest(r RevenueAssociationRequest) bool {
	return validRevenueScope(r.Scope) && validRevenueIdentity(r.InvoiceID) && validRevenueIdentity(r.PaymentID) && validRevenueIdentity(r.CustomerID) && validRevenueIdentity(r.SubscriptionID) && validRevenueIdentity(r.ProviderPriceID) && revenueCurrency.MatchString(r.Currency) && !r.PaidAt.IsZero()
}

// associationResult accepts a stored association only when it matches the
// request's scope, subscription, customer, price and currency with a paid time
// at or after checkout creation and valid owner identifiers; otherwise it
// returns ErrRevenueUnassessable.
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
