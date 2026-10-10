package billingmanager

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// RevenueProviderRegistry exposes optional authenticated economic evidence.
// Webhook-only registries retain their existing interface and behaviour.
type RevenueProviderRegistry interface {
	// GetRevenueProvider resolves the optional revenue provider registered under
	// the given provider name, returning an error when unavailable.
	GetRevenueProvider(string) (paymentprovider.RevenueProvider, error)
}

// RevenueFeedService is the owning persistence port for verified revenue:
// delivery, acceptance, fact lookup, observation reads, resolution history and
// quarantine resolution. It is the single configured feed behind revenue
// features.
type RevenueFeedService interface {
	// GetRevenueDelivery returns the stored delivery observation for the event
	// identified within the given revenue scope.
	GetRevenueDelivery(context.Context, billing.RevenueScope, string) (billing.RevenueObservation, error)
	// AcceptVerified persists a verified revenue request and returns the resulting
	// revenue observation from the owning feed.
	AcceptVerified(context.Context, billing.VerifiedRevenueRequest) (billing.RevenueObservation, error)
	// FindPaymentRevenueFacts returns the revenue facts recorded for the given
	// payment identifier within the revenue scope.
	FindPaymentRevenueFacts(context.Context, billing.RevenueScope, string) ([]billing.RevenueFact, error)
	// GetRevenueObservation returns the revenue observation persisted under the
	// given observation identifier.
	GetRevenueObservation(context.Context, string) (billing.RevenueObservation, error)
	// GetRevenueSourceResolution returns the stored resolution-history observation
	// for the given revenue source identifier.
	GetRevenueSourceResolution(context.Context, string) (billing.RevenueObservation, error)
	// ResolveQuarantinedRevenue resolves a quarantined revenue record per the
	// request and returns the resulting revenue observation.
	ResolveQuarantinedRevenue(context.Context, billing.ResolveRevenueRequest) (billing.RevenueObservation, error)
}

// RevenueAssociationRequest asks the owning billing/catalogue boundary for the
// historical paying principal and immutable price mapping. Current access,
// customer-email matches and organization seats cannot establish this result.
type RevenueAssociationRequest = billing.RevenueAssociationRequest

// RevenueAssociation aliases the billing result describing a historically
// verified paying principal and immutable price mapping.
type RevenueAssociation = billing.RevenueAssociation

// RevenueAssociationService resolves the historical paying principal and price
// mapping for a request; it cannot establish current access or entitlement.
type RevenueAssociationService interface {
	// ResolveRevenueAssociation returns the historical paying principal and price
	// mapping for the request; it cannot establish current access or entitlement.
	ResolveRevenueAssociation(context.Context, RevenueAssociationRequest) (RevenueAssociation, error)
}

// nilRevenueDependency reports whether v is nil, including nil values hidden
// inside interface, pointer, func, map, slice or channel wrappers.
func nilRevenueDependency(v any) bool {
	if v == nil {
		return true
	}
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}

// WithRevenueServices opts a manager into durable financial reception. Supply
// all owning ports before serving requests. No guessed historical association
// or nontransactional fallback is provided when a capability is unavailable.
func (s *Service) WithRevenueServices(registry RevenueProviderRegistry, feed RevenueFeedService, association RevenueAssociationService) (*Service, error) {
	if s == nil || nilRevenueDependency(registry) || nilRevenueDependency(feed) || nilRevenueDependency(association) {
		return nil, billing.ErrRevenueUnavailable
	}
	s.revenueRegistry = registry
	s.revenueFeed = feed
	s.revenueAssociation = association
	return s, nil
}

// billingRevenueScope converts a payment provider revenue scope into the
// equivalent billing scope, copying provider, account and live mode only.
func billingRevenueScope(s paymentprovider.RevenueScope) billing.RevenueScope {
	return billing.RevenueScope{Provider: s.Provider, AccountID: s.AccountID, LiveMode: s.LiveMode}
}

// acceptRevenueWebhook executes before legacy access/ledger projection. Its
// transaction may survive a later legacy failure; retries deduplicate economics.
func (s *Service) acceptRevenueWebhook(ctx context.Context, name string, req *http.Request) (bool, error) {
	if s.revenueFeed == nil {
		return false, nil
	}
	if ctx == nil || req == nil {
		return false, billing.ErrRevenueInvalid
	}
	provider, err := s.revenueRegistry.GetRevenueProvider(name)
	if errors.Is(err, paymentprovider.ErrPaymentProviderUnsupportedProvider) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var verified *paymentprovider.RevenueDeliveryIdentity
	if verifier, ok := provider.(paymentprovider.RevenueDeliveryVerifier); ok && !nilRevenueDependency(verifier) {
		identity, err := verifier.VerifyRevenueDelivery(ctx, req)
		if errors.Is(err, paymentprovider.ErrRevenueNotEnabled) || errors.Is(err, paymentprovider.ErrRevenueEventNotRelevant) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if identity.Scope.Provider != name || identity.EnvelopeID == "" || identity.SourceFingerprint == "" {
			return false, billing.ErrRevenueInvalid
		}
		verified = &identity
		original, err := s.revenueFeed.GetRevenueDelivery(ctx, billingRevenueScope(identity.Scope), identity.EnvelopeID)
		if err == nil {
			if original.Scope != billingRevenueScope(identity.Scope) || original.EnvelopeID != identity.EnvelopeID {
				return false, billing.ErrRevenueConflict
			}
			if original.SourceFingerprint == identity.SourceFingerprint || (identity.LegacySourceFingerprint != "" && original.SourceFingerprint == identity.LegacySourceFingerprint) {
				return true, nil
			}
			// A changed legacy representation can replay only after native recovery
			// retained a stable identity bound to this exact original quarantine.
			resolution, err := s.revenueFeed.GetRevenueSourceResolution(ctx, original.ID)
			if err != nil {
				if singleRevenueAbsence(err) {
					return false, billing.ErrRevenueConflict
				}
				return false, err
			}
			if !boundRevenueResolution(original, resolution) || resolution.RecoveryFingerprint == "" || resolution.RecoveryFingerprint != identity.SourceFingerprint {
				return false, billing.ErrRevenueConflict
			}
			return true, nil
		}
		if !singleRevenueAbsence(err) {
			return false, err
		}
	}
	evidence, err := provider.ResolveRevenueWebhook(ctx, req)
	if errors.Is(err, paymentprovider.ErrRevenueNotEnabled) || errors.Is(err, paymentprovider.ErrRevenueEventNotRelevant) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if evidence == nil || evidence.Scope.Provider != name || evidence.EnvelopeID == "" {
		return false, billing.ErrRevenueInvalid
	}
	if verified != nil && (evidence.Scope != verified.Scope || evidence.EnvelopeID != verified.EnvelopeID || evidence.SourceFingerprint != verified.SourceFingerprint) {
		return false, billing.ErrRevenueConflict
	}
	request, err := s.revenueRequest(ctx, provider, evidence)
	if err != nil {
		return false, err
	}
	if _, err := s.revenueFeed.AcceptVerified(ctx, request); err != nil {
		return false, err
	}
	return true, nil
}

// revenueRequest turns verified provider evidence into a durable request,
// fetching the invoice when absent, reconciling original facts by line, and
// resolving historical payer/plan association for new payment lines. Ambiguous
// or pending economics quarantine the request with a reason instead of failing;
// only unassessable association errors propagate.
func (s *Service) revenueRequest(ctx context.Context, provider paymentprovider.RevenueProvider, e *paymentprovider.RevenueEvidence) (billing.VerifiedRevenueRequest, error) {
	req := billing.VerifiedRevenueRequest{Scope: billingRevenueScope(e.Scope), EnvelopeID: e.EnvelopeID, QuarantineReason: e.QuarantineReason, SourceFingerprint: e.SourceFingerprint}
	if e.QuarantineReason != "" {
		return req, nil
	}
	original, err := s.revenueFeed.FindPaymentRevenueFacts(ctx, req.Scope, e.PaymentID)
	if err != nil {
		return req, err
	}
	invoice := e.Invoice
	if invoice == nil {
		invoiceID := e.InvoiceID
		if invoiceID == "" && len(original) > 0 {
			invoiceID = original[0].InvoiceID
			for _, f := range original {
				if f.InvoiceID != invoiceID {
					req.QuarantineReason = "payment_invoice_ambiguous"
					return req, nil
				}
			}
		}
		if invoiceID == "" {
			req.QuarantineReason = "original_payment_pending"
			return req, nil
		}
		invoice, err = provider.LookupRevenueInvoice(ctx, paymentprovider.RevenueInvoiceRequest{Scope: e.Scope, InvoiceID: invoiceID, PaymentID: e.PaymentID, IncludeRefunds: e.Kind == billing.RevenueRefund, ExpectedCumulativeRefundedGrossMinor: e.CumulativeRefundedGrossMinor})
		if errors.Is(err, paymentprovider.ErrRevenueUnassessable) {
			req.QuarantineReason = "invoice_economics_unassessable"
			return req, nil
		}
		if err != nil {
			return req, err
		}
	}
	if invoice == nil || invoice.Scope != e.Scope || invoice.PaymentID != e.PaymentID || invoice.InvoiceID == "" {
		return req, billing.ErrRevenueInvalid
	}
	if e.Kind != billing.RevenuePayment && e.Currency != invoice.Currency {
		req.QuarantineReason = "adjustment_currency_mismatch"
		return req, nil
	}
	if strings.HasPrefix(e.Kind, "dispute_") && e.AffectedMinor != invoice.GrossPaidMinor {
		req.QuarantineReason = "partial_dispute_allocation_pending"
		return req, nil
	}
	byLine := map[string]billing.RevenueFact{}
	for _, f := range original {
		if f.Scope != req.Scope || f.PaymentID != invoice.PaymentID || f.InvoiceID != invoice.InvoiceID || f.Kind != billing.RevenuePayment {
			return req, billing.ErrRevenueUnassessable
		}
		byLine[f.AllocationID] = f
	}
	for _, line := range invoice.Lines {
		if line.SubscriptionID == "" {
			continue
		} // Verified non-subscription revenue has no program entitlement.
		if line.SubscriptionID != invoice.SubscriptionID || line.PriceID == "" {
			req.Facts = nil
			req.QuarantineReason = "subscription_allocation_ambiguous"
			return req, nil
		}
		fact := billing.RevenueFact{Scope: req.Scope, Kind: e.Kind, PaymentID: invoice.PaymentID, InvoiceID: invoice.InvoiceID, AllocationID: line.ID, SubscriptionID: invoice.SubscriptionID, ProviderPriceID: line.PriceID, ProviderCustomerID: invoice.CustomerID, Currency: invoice.Currency, CurrencyExponent: invoice.CurrencyExponent, PaidMinor: line.NetPaidMinor, EffectiveAt: invoice.PaidAt}
		if old, ok := byLine[line.ID]; ok {
			if old.PaidMinor != line.NetPaidMinor || old.Currency != invoice.Currency || old.CurrencyExponent != invoice.CurrencyExponent || old.SubscriptionID != invoice.SubscriptionID || old.ProviderPriceID != line.PriceID || old.ProviderCustomerID != invoice.CustomerID || !old.EffectiveAt.Equal(invoice.PaidAt) {
				req.Facts = nil
				req.QuarantineReason = "original_economics_changed"
				return req, nil
			}
			fact.PrincipalID = old.PrincipalID
			fact.PlanID = old.PlanID
			fact.CostID = old.CostID
		} else if e.Kind == billing.RevenuePayment {
			association, err := s.revenueAssociation.ResolveRevenueAssociation(ctx, RevenueAssociationRequest{Scope: req.Scope, InvoiceID: invoice.InvoiceID, PaymentID: invoice.PaymentID, CustomerID: invoice.CustomerID, SubscriptionID: invoice.SubscriptionID, ProviderPriceID: line.PriceID, Currency: invoice.Currency, PaidAt: invoice.PaidAt})
			if singleRevenueAbsence(err) || singleRevenueError(err, billing.ErrRevenueUnassessable) {
				req.Facts = nil
				req.QuarantineReason = "historical_payer_plan_pending"
				return req, nil
			}
			if err != nil {
				return req, err
			}
			if strings.TrimSpace(association.PrincipalID) == "" || strings.TrimSpace(association.PlanID) == "" || strings.TrimSpace(association.CostID) == "" {
				return req, billing.ErrRevenueUnassessable
			}
			fact.PrincipalID = association.PrincipalID
			fact.PlanID = association.PlanID
			fact.CostID = association.CostID
		} else {
			req.Facts = nil
			req.QuarantineReason = "original_payment_pending"
			return req, nil
		}
		if e.Kind != billing.RevenuePayment {
			fact.AdjustmentID = e.AdjustmentID
			fact.EffectiveAt = e.EffectiveAt
			switch e.Kind {
			case billing.RevenueRefund:
				if line.CumulativeRefundedMinor == 0 {
					req.Facts = nil
					req.QuarantineReason = "refund_allocation_pending"
					return req, nil
				}
				fact.CumulativeRefundedMinor = line.CumulativeRefundedMinor
			case billing.RevenueDisputeHold, billing.RevenueDisputeLost, billing.RevenueDisputeWon:
			default:
				return req, billing.ErrRevenueInvalid
			}
		}
		req.Facts = append(req.Facts, fact)
	}
	if len(req.Facts) == 0 {
		req.QuarantineReason = "no_subscription_revenue"
	}
	return req, nil
}

// RevenueReconciliationAuthority must resolve current scoped worker/operator
// permission, including revocation, for every attempt and receipt replay.
type RevenueReconciliationAuthority interface {
	// AuthorizeRevenueReconciliation resolves current scoped worker or operator
	// permission for the given actor, including revocation, per attempt.
	AuthorizeRevenueReconciliation(context.Context, string) error
}

// WithRevenueReconciliationAuthority installs the authority that must approve
// revenue reconciliation attempts; a nil authority returns
// ErrRevenueUnavailable without mutating the service.
func (s *Service) WithRevenueReconciliationAuthority(authority RevenueReconciliationAuthority) (*Service, error) {
	if s == nil || nilRevenueDependency(authority) {
		return nil, billing.ErrRevenueUnavailable
	}
	s.revenueAuthority = authority
	return s, nil
}

// ReconcileRevenueSourceRequest selects a quarantined observation for
// resolution. ActorID is bound from verified context and never decoded from
// transport; OriginalSnapshot is optional private recovery input verified
// against the retained original hash.
type ReconcileRevenueSourceRequest struct {
	ObservationID       string
	ExpectedFingerprint string
	Reason              string
	ActorID             string `json:"-"`
	// OriginalSnapshot is optional private recovery input for legacy source
	// hashes. It must reproduce the retained original hash; it is never stored
	// or accepted as a substitute for authenticated provider event retrieval.
	OriginalSnapshot []byte `json:"-"`
}

// ReconcileRevenueSource re-fetches authenticated provider evidence for a
// quarantined observation, verifies scope and source fingerprints, rebuilds
// facts, and resolves the quarantine under current authority. Fingerprint or
// ownership mismatches return ErrRevenueConflict; a matching existing
// resolution is returned idempotently when actor and reason agree.
func (s *Service) ReconcileRevenueSource(ctx context.Context, req ReconcileRevenueSourceRequest) (billing.RevenueObservation, error) {
	if ctx == nil || strings.TrimSpace(req.ActorID) == "" || strings.TrimSpace(req.Reason) == "" || req.ExpectedFingerprint == "" || req.ObservationID == "" {
		return billing.RevenueObservation{}, billing.ErrRevenueInvalid
	}
	if s == nil || nilRevenueDependency(s.revenueAuthority) || nilRevenueDependency(s.revenueFeed) || nilRevenueDependency(s.revenueRegistry) {
		return billing.RevenueObservation{}, billing.ErrRevenueUnavailable
	}
	if err := s.revenueAuthority.AuthorizeRevenueReconciliation(ctx, req.ActorID); err != nil {
		return billing.RevenueObservation{}, err
	}
	source, err := s.revenueFeed.GetRevenueObservation(ctx, req.ObservationID)
	if err != nil {
		return billing.RevenueObservation{}, err
	}
	if source.Fingerprint != req.ExpectedFingerprint || source.QuarantineReason == "" || source.ResolutionOf != "" {
		return billing.RevenueObservation{}, billing.ErrRevenueConflict
	}
	old, err := s.revenueFeed.GetRevenueSourceResolution(ctx, source.ID)
	if err == nil {
		if !boundRevenueResolution(source, old) || old.ResolutionBy != req.ActorID || old.ResolutionReason != req.Reason {
			return billing.RevenueObservation{}, billing.ErrRevenueConflict
		}
		if len(req.OriginalSnapshot) > 0 {
			provider, err := s.revenueRegistry.GetRevenueProvider(source.Scope.Provider)
			if err != nil {
				return billing.RevenueObservation{}, err
			}
			identity, err := verifyRevenueOriginalSnapshot(ctx, provider, source, req.OriginalSnapshot)
			if err != nil {
				return billing.RevenueObservation{}, err
			}
			if old.RecoveryFingerprint != identity {
				return billing.RevenueObservation{}, billing.ErrRevenueConflict
			}
		}
		return old, nil
	}
	if !singleRevenueAbsence(err) {
		return billing.RevenueObservation{}, err
	}
	provider, err := s.revenueRegistry.GetRevenueProvider(source.Scope.Provider)
	if err != nil {
		return billing.RevenueObservation{}, err
	}
	expectedSource := source.SourceFingerprint
	if len(req.OriginalSnapshot) > 0 {
		expectedSource, err = verifyRevenueOriginalSnapshot(ctx, provider, source, req.OriginalSnapshot)
		if err != nil {
			return billing.RevenueObservation{}, err
		}
	}
	reconciliation, ok := provider.(paymentprovider.RevenueReconciliationProvider)
	if !ok || nilRevenueDependency(reconciliation) {
		return billing.RevenueObservation{}, billing.ErrRevenueUnavailable
	}
	evidence, err := reconciliation.ReconcileRevenueEvent(ctx, paymentprovider.RevenueScope{Provider: source.Scope.Provider, AccountID: source.Scope.AccountID, LiveMode: source.Scope.LiveMode}, source.EnvelopeID)
	if err != nil {
		return billing.RevenueObservation{}, err
	}
	if evidence == nil || billingRevenueScope(evidence.Scope) != source.Scope || evidence.EnvelopeID != source.EnvelopeID {
		return billing.RevenueObservation{}, billing.ErrRevenueInvalid
	}
	if expectedSource != "" && evidence.SourceFingerprint != expectedSource && !(len(req.OriginalSnapshot) == 0 && evidence.LegacySourceFingerprint != "" && evidence.LegacySourceFingerprint == expectedSource) {
		return billing.RevenueObservation{}, billing.ErrRevenueInvalid
	}
	verified, err := s.revenueRequest(ctx, provider, evidence)
	if err != nil {
		return billing.RevenueObservation{}, err
	}
	if verified.QuarantineReason != "" && verified.QuarantineReason != "no_subscription_revenue" {
		return billing.RevenueObservation{}, billing.ErrRevenueUnassessable
	}
	return s.revenueFeed.ResolveQuarantinedRevenue(ctx, billing.ResolveRevenueRequest{ObservationID: source.ID, ExpectedFingerprint: source.Fingerprint, RecoveryFingerprint: evidence.SourceFingerprint, Facts: verified.Facts, Reason: req.Reason, ActorID: req.ActorID})
}

// A recovery receipt must belong to the immutable original source. Provider
// replay cannot use a receipt from another scope, envelope or quarantine.
func boundRevenueResolution(source, resolution billing.RevenueObservation) bool {
	return source.ID != "" && source.QuarantineReason != "" && source.ResolutionOf == "" && resolution.ID != "" && resolution.Fingerprint != "" && !resolution.AcceptedAt.IsZero() && resolution.ResolutionOf == source.ID && resolution.QuarantineReason == "" && resolution.Scope == source.Scope && resolution.EnvelopeID == source.EnvelopeID && resolution.SourceFingerprint == source.SourceFingerprint
}

// verifyRevenueOriginalSnapshot asks the provider to verify a retained original
// snapshot and returns its canonical fingerprint, rejecting identities whose
// scope, envelope or original fingerprint differ from the stored source.
func verifyRevenueOriginalSnapshot(ctx context.Context, provider paymentprovider.RevenueProvider, source billing.RevenueObservation, snapshot []byte) (string, error) {
	verifier, ok := provider.(paymentprovider.RevenueSnapshotVerifier)
	if !ok || nilRevenueDependency(verifier) {
		return "", billing.ErrRevenueUnavailable
	}
	scope := paymentprovider.RevenueScope{Provider: source.Scope.Provider, AccountID: source.Scope.AccountID, LiveMode: source.Scope.LiveMode}
	identity, err := verifier.VerifyRetainedRevenueSnapshot(ctx, paymentprovider.RevenueSnapshotRequest{Scope: scope, EnvelopeID: source.EnvelopeID, OriginalFingerprint: source.SourceFingerprint, OriginalSnapshot: snapshot})
	if err != nil {
		return "", err
	}
	if identity.Scope != scope || identity.EnvelopeID != source.EnvelopeID || identity.OriginalFingerprint != source.SourceFingerprint || identity.CanonicalFingerprint == "" {
		return "", billing.ErrRevenueInvalid
	}
	return identity.CanonicalFingerprint, nil
}

// singleRevenueAbsence reports whether the error chain contains
// billing.ErrRevenueNotFound within 32 unwraps, treating it as conclusive
// absence.
func singleRevenueAbsence(err error) bool {
	for i := 0; err != nil && i < 32; i++ {
		if err == billing.ErrRevenueNotFound {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// A joined evidence refusal and transport outage remains retryable; only a
// single cause establishes a conclusive unassessable historical association.
func singleRevenueError(err, target error) bool {
	for n := 0; err != nil && n < 32; n++ {
		if err == target {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}
