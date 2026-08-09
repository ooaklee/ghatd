package billingmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

// ProviderRegistry defines the expected methods of a payment provider registry
type ProviderRegistry interface {
	VerifyAndParseWebhookPayload(ctx context.Context, providerName string, req *http.Request) (*paymentprovider.WebhookPayload, error)
}

// CheckoutProviderRegistry resolves optional provider checkout capabilities.
// It remains separate from ProviderRegistry so webhook-only custom registries
// retain source compatibility.
type CheckoutProviderRegistry interface {
	GetCheckoutProvider(name string) (paymentprovider.CheckoutProvider, error)
}

// CustomerPortalProviderRegistry resolves optional hosted customer-portal
// capabilities without widening the webhook registry contract.
type CustomerPortalProviderRegistry interface {
	GetCustomerPortalProvider(name string) (paymentprovider.CustomerPortalProvider, error)
}

// UpcomingInvoicePreviewProviderRegistry resolves optional provider invoice
// preview capabilities without widening webhook-only custom registries.
type UpcomingInvoicePreviewProviderRegistry interface {
	GetUpcomingInvoicePreviewProvider(name string) (paymentprovider.UpcomingInvoicePreviewProvider, error)
}

// CheckoutProviderConfig contains legacy application-owned checkout settings.
//
// Deprecated: configure ReturnURL on paymentprovider.Config so the registered
// provider remains the single source of checkout capability and configuration.
type CheckoutProviderConfig struct {
	ReturnURL string
}

// AuditService interface for logging billing events (optional)
type AuditService interface {
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
}

// UserService interface for user operations (optional)
type UserService interface {
	GetUserByEmail(ctx context.Context, req *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error)
	GetUserByID(ctx context.Context, req *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error)
}

// userByEmailFinder is an optional capability implemented by user/v2 for
// association flows where no matching user is an expected outcome.
type userByEmailFinder interface {
	FindUserByEmail(ctx context.Context, req *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error)
}

// BillingService interface for valid billing service
type BillingService interface {
	GetSubscriptions(ctx context.Context, req *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error)
	GetBillingEvents(ctx context.Context, req *billing.GetBillingEventsRequest) (*billing.GetBillingEventsResponse, error)
	GetSubscriptionByIntegratorID(ctx context.Context, req *billing.GetSubscriptionByIntegratorIDRequest) (*billing.GetSubscriptionByIntegratorIDResponse, error)
	CreateSubscription(ctx context.Context, req *billing.CreateSubscriptionRequest) (*billing.CreateSubscriptionResponse, error)
	UpdateSubscription(ctx context.Context, req *billing.UpdateSubscriptionRequest) (*billing.UpdateSubscriptionResponse, error)
	CreateBillingEvent(ctx context.Context, req *billing.CreateBillingEventRequest) (*billing.CreateBillingEventResponse, error)
	GetSubscriptionsByEmail(ctx context.Context, req *billing.GetSubscriptionsByEmailRequest) (*billing.GetSubscriptionsByEmailResponse, error)
	AssociateSubscriptionsWithUser(ctx context.Context, req *billing.AssociateSubscriptionsWithUserRequest) (*billing.AssociateSubscriptionsWithUserResponse, error)
}

// PricerService defines the pricing operations exposed through billing manager.
type PricerService interface {
	GetPricePlans(ctx context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error)
	GetPricePlanBySlug(ctx context.Context, req *pricer.GetPricePlanBySlugRequest) (*pricer.GetPricePlanBySlugResponse, error)
	GetFeatures(ctx context.Context, req *pricer.GetFeaturesRequest) (*pricer.GetFeaturesResponse, error)
}

// Service orchestrates webhook processing and billing operations
// It uses paymentprovider for webhook verification and billingstore for persistence
type Service struct {
	ProviderRegistry                       ProviderRegistry
	CheckoutProviderRegistry               CheckoutProviderRegistry
	CustomerPortalProviderRegistry         CustomerPortalProviderRegistry
	UpcomingInvoicePreviewProviderRegistry UpcomingInvoicePreviewProviderRegistry
	// CheckoutProviderConfigs is retained as a compatibility fallback for
	// providers that do not yet implement paymentprovider.CheckoutReturnURLProvider.
	// New integrations should configure the registered provider instead.
	//
	// Deprecated: configure paymentprovider.Config.ReturnURL.
	CheckoutProviderConfigs map[string]CheckoutProviderConfig
	BillingService          BillingService
	AuditService            AuditService // Optional audit logging
	UserService             UserService  // Optional user service integration
	PricerService           PricerService
}

type webhookAccessAction uint8

const (
	webhookAccessLedgerOnly webhookAccessAction = iota
	webhookAccessUpsert
	webhookAccessRevokeOneTime
)

// webhookAccessAssociation records how strongly signed provider data can be
// tied to server-owned access. Exact associations may mutate access; a unique
// customer-only association is suitable for ledger attribution only.
type webhookAccessAssociation struct {
	subscription *billing.Subscription
	exact        bool
}

// NewService creates a new billing manager service
func NewService(registry ProviderRegistry, billingService BillingService) *Service {
	service := &Service{
		ProviderRegistry: registry,
		BillingService:   billingService,
	}
	if checkoutRegistry, ok := registry.(CheckoutProviderRegistry); ok {
		service.CheckoutProviderRegistry = checkoutRegistry
	}
	if portalRegistry, ok := registry.(CustomerPortalProviderRegistry); ok {
		service.CustomerPortalProviderRegistry = portalRegistry
	}
	if previewRegistry, ok := registry.(UpcomingInvoicePreviewProviderRegistry); ok {
		service.UpcomingInvoicePreviewProviderRegistry = previewRegistry
	}

	return service
}

// WithCustomerPortalProviderRegistry supplies portal lookup for custom webhook
// registries that do not expose the optional capability themselves.
func (s *Service) WithCustomerPortalProviderRegistry(registry CustomerPortalProviderRegistry) *Service {
	s.CustomerPortalProviderRegistry = registry
	return s
}

// WithUpcomingInvoicePreviewProviderRegistry supplies optional invoice-preview
// lookup for custom webhook registries that do not expose it themselves.
func (s *Service) WithUpcomingInvoicePreviewProviderRegistry(registry UpcomingInvoicePreviewProviderRegistry) *Service {
	s.UpcomingInvoicePreviewProviderRegistry = registry
	return s
}

// WithAuditService adds audit logging capability
func (s *Service) WithAuditService(audit AuditService) *Service {
	s.AuditService = audit
	return s
}

// WithUserService adds user service integration
func (s *Service) WithUserService(userSvc UserService) *Service {
	s.UserService = userSvc
	return s
}

// WithPricerService adds pricing catalog read capability.
func (s *Service) WithPricerService(pricerSvc PricerService) *Service {
	s.PricerService = pricerSvc
	return s
}

// WithCheckoutProviderRegistry supplies checkout lookup for custom webhook
// registries that do not expose the optional capability themselves.
func (s *Service) WithCheckoutProviderRegistry(registry CheckoutProviderRegistry) *Service {
	s.CheckoutProviderRegistry = registry
	return s
}

// WithCheckoutProviderConfig adds a legacy trusted return-URL fallback.
// Provider-owned configuration takes precedence when the registered checkout
// provider implements paymentprovider.CheckoutReturnURLProvider.
//
// Deprecated: configure paymentprovider.Config.ReturnURL on the registered
// provider and rely on registry capability discovery.
func (s *Service) WithCheckoutProviderConfig(providerName string, config *CheckoutProviderConfig) *Service {
	if s.CheckoutProviderConfigs == nil {
		s.CheckoutProviderConfigs = make(map[string]CheckoutProviderConfig)
	}
	if config == nil {
		s.CheckoutProviderConfigs[normaliseCheckoutProviderName(providerName)] = CheckoutProviderConfig{}
		return s
	}
	s.CheckoutProviderConfigs[normaliseCheckoutProviderName(providerName)] = *config
	return s
}

// ProcessBillingProviderWebhooks handles incoming webhooks from payment providers
// This is the main entry point for webhook processing
func (s *Service) ProcessBillingProviderWebhooks(ctx context.Context, req *ProcessBillingProviderWebhooksRequest) error {

	logger := logger.AcquirePackageFrom(ctx, "external/billingmanager")
	if s == nil || s.ProviderRegistry == nil || s.BillingService == nil || req == nil {
		return ErrInvalidBillingManagerRequestPayload
	}
	var subscriptionID string

	payload, err := s.ProviderRegistry.VerifyAndParseWebhookPayload(ctx, req.ProviderName, req.Request)
	if err != nil {
		logger.Error("failed-to-verify-and-parse-webhook-payload", zap.String("provider", req.ProviderName), zap.Error(err))
		return err
	}
	stripeRecurringPaymentStateWasEmpty := strings.EqualFold(strings.TrimSpace(req.ProviderName), "stripe") &&
		payload.IsRecurring() && payload.Status == "" && isPaymentLifecycleEvent(payload.EventType)
	normaliseLegacyWebhookPayload(payload)
	if stripeRecurringPaymentStateWasEmpty {
		// Stripe invoices report payment state; customer.subscription.* owns
		// recurring access status and period. Preserve that distinction across
		// the legacy normalizer.
		payload.Status = ""
	}

	association, err := s.resolveWebhookAccessAssociation(ctx, req.ProviderName, payload)
	if err != nil {
		logger.Error("failed-to-resolve-webhook-access-association", append(webhookPayloadFieldsForLog(req.ProviderName, "", payload), zap.Error(err))...)
		return err
	}

	action := determineWebhookAccessAction(req.ProviderName, payload, association)
	var userID string
	if association != nil && association.subscription != nil {
		userID = strings.TrimSpace(association.subscription.UserID)
		if association.exact {
			inheritServerOwnedAccessIdentity(payload, association.subscription)
			subscriptionID = association.subscription.ID
		}
	}

	switch action {
	case webhookAccessUpsert:
		if association == nil || association.subscription == nil || !association.exact {
			userID, err = s.resolveUserID(ctx, req.ProviderName, payload)
			if err != nil {
				logger.Error("failed-to-resolve-user-id", zap.String("provider", req.ProviderName), zap.Error(err))
				return err
			}
		}
		subscription, findErr := s.findOrCreateSubscription(ctx, req.ProviderName, payload, userID)
		if findErr != nil {
			logger.Error("failed-to-find-or-create-subscription", append(webhookPayloadFieldsForLog(req.ProviderName, userID, payload), zap.Error(findErr))...)
			return findErr
		}

		if updateErr := s.updateSubscriptionFromPayload(ctx, subscription, payload, userID); updateErr != nil {
			logger.Error("failed-to-update-subscription-from-payload", append(webhookPayloadFieldsForLog(req.ProviderName, userID, payload), zap.String("subscription-id", subscription.ID), zap.Error(updateErr))...)
			return updateErr
		}

		subscriptionID = subscription.ID
	case webhookAccessRevokeOneTime:
		if association == nil || association.subscription == nil || !association.exact {
			return paymentprovider.ErrPaymentProviderMissingRequiredField
		}
		if updateErr := s.updateSubscriptionFromPayload(ctx, association.subscription, payload, userID); updateErr != nil {
			logger.Error("failed-to-revoke-one-time-access-from-payload", append(webhookPayloadFieldsForLog(req.ProviderName, userID, payload), zap.String("subscription-id", association.subscription.ID), zap.Error(updateErr))...)
			return updateErr
		}
		subscriptionID = association.subscription.ID
	case webhookAccessLedgerOnly:
		if userID == "" {
			userID, err = s.resolveLedgerUserID(ctx, req.ProviderName, payload)
			if err != nil {
				return err
			}
		}
	}

	if err := s.createBillingEvent(ctx, subscriptionID, userID, req.ProviderName, payload); err != nil {
		logger.Error("failed-to-create-billing-event", append(webhookPayloadFieldsForLog(req.ProviderName, userID, payload), zap.String("subscription-id", subscriptionID), zap.Error(err))...)
		return err
	}
	billingEventSuccessfullyCreated := true

	// Optional audit logging
	if s.AuditService != nil {

		eventMessageDetails := ""
		if payload.IsRecurring() {
			eventMessageDetails = fmt.Sprintf("Processed %s webhook for subscription %s", req.ProviderName, payload.SubscriptionID)
		} else {
			eventMessageDetails = fmt.Sprintf("Processed %s webhook for non-subscription event", req.ProviderName)
		}

		event := &AuditEvent{
			EventType:                       payload.EventType,
			UserID:                          userID,
			Details:                         eventMessageDetails,
			OccurredAt:                      time.Now(),
			BillingSubscriptionId:           subscriptionID,
			Provider:                        req.ProviderName,
			BillingEventSuccessfullyCreated: billingEventSuccessfullyCreated,
		}

		// Only include full payload if billing event creation failed
		// This avoids logging sensitive data unnecessarily
		if !billingEventSuccessfullyCreated {
			event.ProviderPayload = payload
		}

		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			ActorId:    audit.AuditActorIdSystem,
			Action:     AuditActionBillingWebhookProcessed,
			TargetId:   payload.EventID,
			TargetType: TargetTypeWebhook,
			Domain:     "billingmanager",
			Details:    event,
		})
	}

	return nil
}

// resolveWebhookAccessAssociation finds server-owned access without making a
// provider API call. Provider references and the billing ledger are exact;
// a unique customer match is used only to attribute a ledger entry.
func (s *Service) resolveWebhookAccessAssociation(ctx context.Context, providerName string, payload *paymentprovider.WebhookPayload) (*webhookAccessAssociation, error) {
	if payload == nil {
		return nil, nil
	}

	if providerAccessReference(payload) != "" {
		subscription, err := s.findPlanAccess(ctx, providerName, payload)
		if err == nil {
			return &webhookAccessAssociation{subscription: subscription, exact: true}, nil
		}
		if !errors.Is(err, billing.ErrBillingSubscriptionNotFound) {
			return nil, err
		}
	}

	if strings.TrimSpace(payload.TransactionID) != "" {
		subscription, err := s.findPlanAccessFromLedger(ctx, providerName, payload.TransactionID)
		if err != nil {
			return nil, err
		}
		if subscription != nil {
			return &webhookAccessAssociation{subscription: subscription, exact: true}, nil
		}
	}

	customerID := strings.TrimSpace(payload.CustomerID)
	if customerID == "" {
		return nil, nil
	}
	response, err := s.BillingService.GetSubscriptions(ctx, &billing.GetSubscriptionsRequest{
		IntegratorName:       providerName,
		IntegratorCustomerID: customerID,
		Order:                "created_at_desc",
		PerPage:              2,
		Page:                 1,
	})
	if err != nil {
		return nil, err
	}
	if response == nil || response.Total != 1 || len(response.Subscriptions) != 1 {
		return nil, nil
	}
	subscription := &response.Subscriptions[0]
	if !strings.EqualFold(strings.TrimSpace(subscription.Integrator), strings.TrimSpace(providerName)) ||
		strings.TrimSpace(subscription.IntegratorCustomerID) != customerID {
		return nil, nil
	}
	return &webhookAccessAssociation{subscription: subscription, exact: false}, nil
}

// findPlanAccessFromLedger correlates transaction-only events through prior
// signed events. Ambiguous or incomplete ledger history fails closed.
func (s *Service) findPlanAccessFromLedger(ctx context.Context, providerName, transactionID string) (*billing.Subscription, error) {
	response, err := s.BillingService.GetBillingEvents(ctx, &billing.GetBillingEventsRequest{
		IntegratorName:          providerName,
		IntegratorTransactionID: strings.TrimSpace(transactionID),
		Order:                   "created_at_desc",
		PerPage:                 100,
		Page:                    1,
	})
	if err != nil {
		return nil, err
	}
	if response == nil || response.Total == 0 || len(response.BillingEvents) == 0 || response.Total > len(response.BillingEvents) {
		return nil, nil
	}

	var candidate *billing.Subscription
	for index := range response.BillingEvents {
		event := &response.BillingEvents[index]
		if !strings.EqualFold(strings.TrimSpace(event.Integrator), strings.TrimSpace(providerName)) ||
			strings.TrimSpace(event.IntegratorTransactionID) != strings.TrimSpace(transactionID) {
			continue
		}
		reference := strings.TrimSpace(event.IntegratorSubscriptionID)
		if reference == "" && (event.IsOneOff || event.BillingKind == string(paymentprovider.BillingKindOneTime)) {
			reference = strings.TrimSpace(transactionID)
		}
		if reference == "" {
			continue
		}
		lookup, lookupErr := s.BillingService.GetSubscriptionByIntegratorID(ctx, &billing.GetSubscriptionByIntegratorIDRequest{
			IntegratorName:           providerName,
			IntegratorSubscriptionID: reference,
		})
		if errors.Is(lookupErr, billing.ErrBillingSubscriptionNotFound) {
			continue
		}
		if lookupErr != nil {
			return nil, lookupErr
		}
		if lookup == nil || lookup.Subscription == nil {
			continue
		}
		if candidate != nil && candidate.ID != lookup.Subscription.ID {
			return nil, nil
		}
		candidate = lookup.Subscription
	}
	return candidate, nil
}

func determineWebhookAccessAction(providerName string, payload *paymentprovider.WebhookPayload, association *webhookAccessAssociation) webhookAccessAction {
	if payload == nil {
		return webhookAccessLedgerOnly
	}
	if association != nil && association.subscription != nil && association.exact {
		subscription := association.subscription
		if payload.EventType == paymentprovider.EventTypePaymentRefunded {
			if subscription.IsRecurring() {
				return webhookAccessLedgerOnly
			}
			return webhookAccessRevokeOneTime
		}
		if subscription.IsRecurring() {
			if payload.IsRecurring() && strings.TrimSpace(payload.SubscriptionID) == strings.TrimSpace(subscription.IntegratorSubscriptionID) && isSubscriptionLifecycleEvent(payload.EventType) {
				return webhookAccessUpsert
			}
			// Checkout and invoice payment events are evidence for the ledger;
			// subscription lifecycle events remain authoritative for state/period.
			return webhookAccessLedgerOnly
		}
		if isSuccessfulOneOffAccess(payload) {
			return webhookAccessUpsert
		}
		return webhookAccessLedgerOnly
	}

	if payload.IsRecurring() {
		if !strings.EqualFold(strings.TrimSpace(providerName), "stripe") {
			return webhookAccessUpsert
		}
		if isStripeOwnedRecurringBootstrap(payload) {
			return webhookAccessUpsert
		}
		return webhookAccessLedgerOnly
	}
	if isSuccessfulOneOffAccess(payload) {
		return webhookAccessUpsert
	}
	return webhookAccessLedgerOnly
}

func isStripeOwnedRecurringBootstrap(payload *paymentprovider.WebhookPayload) bool {
	if payload == nil || !payload.IsRecurring() || strings.TrimSpace(payload.SubscriptionID) == "" ||
		strings.TrimSpace(payload.UserReference) == "" || strings.TrimSpace(payload.PlanID) == "" ||
		strings.TrimSpace(payload.CostID) == "" || strings.TrimSpace(payload.ProviderPriceID) == "" {
		return false
	}
	if isSubscriptionLifecycleEvent(payload.EventType) {
		return true
	}
	// A completed Checkout Session can bootstrap identity before Stripe emits
	// customer.subscription.created. Recurring invoice events deliberately
	// have no access Status and remain ledger-only.
	return payload.EventType == paymentprovider.EventTypePaymentSucceeded && strings.TrimSpace(payload.Status) != ""
}

func isSubscriptionLifecycleEvent(eventType string) bool {
	switch eventType {
	case paymentprovider.EventTypeSubscriptionCreated,
		paymentprovider.EventTypeSubscriptionUpdated,
		paymentprovider.EventTypeSubscriptionCancelled,
		paymentprovider.EventTypeSubscriptionPaused,
		paymentprovider.EventTypeSubscriptionResumed,
		paymentprovider.EventTypeSubscriptionCreatedDonation,
		paymentprovider.EventTypeSubscriptionUpdatedDonation,
		paymentprovider.EventTypeSubscriptionCancelledDonation,
		paymentprovider.EventTypeSubscriptionPausedDonation,
		paymentprovider.EventTypeSubscriptionResumedDonation:
		return true
	default:
		return false
	}
}

func isPaymentLifecycleEvent(eventType string) bool {
	switch eventType {
	case paymentprovider.EventTypePaymentSucceeded,
		paymentprovider.EventTypePaymentFailed,
		paymentprovider.EventTypePaymentRefunded,
		paymentprovider.EventTypePaymentPartiallyRefunded,
		paymentprovider.EventTypePaymentRefundFailed,
		paymentprovider.EventTypePaymentActionRequired:
		return true
	default:
		return false
	}
}

// inheritServerOwnedAccessIdentity enriches an exact ledger event while
// preventing mutable provider metadata from rebinding an existing record.
func inheritServerOwnedAccessIdentity(payload *paymentprovider.WebhookPayload, subscription *billing.Subscription) {
	if payload == nil || subscription == nil {
		return
	}
	payload.UserReference = firstNonEmptyString(subscription.UserReference, subscription.UserID)
	if strings.TrimSpace(subscription.IntegratorCustomerID) != "" {
		payload.CustomerID = strings.TrimSpace(subscription.IntegratorCustomerID)
	}
	if subscription.IsRecurring() {
		payload.BillingKind = paymentprovider.BillingKindRecurring
		payload.PaymentType = paymentprovider.PaymentTypeSubscription
		payload.IsOneOff = false
		if strings.TrimSpace(payload.SubscriptionID) == "" {
			payload.SubscriptionID = strings.TrimSpace(subscription.IntegratorSubscriptionID)
		}
	} else {
		payload.BillingKind = paymentprovider.BillingKindOneTime
		payload.PaymentType = firstNonEmptyString(subscription.PaymentType, paymentprovider.PaymentTypePurchase)
		payload.IsOneOff = true
	}
	if payload.PlanName == "" {
		payload.PlanName = subscription.PlanName
	}
	if payload.PlanID == "" {
		payload.PlanID = subscription.PlanID
	}
	if payload.PlanSlug == "" {
		payload.PlanSlug = subscription.PlanSlug
	}
	if payload.CostID == "" {
		payload.CostID = subscription.CostID
	}
	if payload.ProviderPriceID == "" {
		payload.ProviderPriceID = subscription.ProviderPriceID
	}
}

// resolveLedgerUserID performs best-effort attribution for events that cannot
// mutate access. Missing identity is valid and must not cause provider retries.
func (s *Service) resolveLedgerUserID(ctx context.Context, providerName string, payload *paymentprovider.WebhookPayload) (string, error) {
	if payload == nil {
		return "", nil
	}
	if userReference := strings.TrimSpace(payload.UserReference); userReference != "" {
		return userReference, nil
	}
	if strings.TrimSpace(payload.CustomerEmail) == "" || s.UserService == nil {
		return "", nil
	}
	userID, err := s.resolveUserID(ctx, providerName, payload)
	if errors.Is(err, ErrBillingManagerNoUserIdentifyingInformationInPayload) {
		return "", nil
	}
	return userID, err
}

// GetPricingPlans retrieves pricing plans for external BMS clients.
func (s *Service) GetPricingPlans(ctx context.Context, req *GetPricingPlansRequest) (*GetPricingPlansResponse, error) {

	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/billingmanager")

	if s.PricerService == nil {
		logger.Error("pricer-service-not-enabled", zap.String("user-id", req.UserID))
		return nil, ErrBillingManagerPricerServiceNotSet
	}

	isAdmin := s.isRequesterAdmin(ctx, req.UserID, logger)
	if !isAdmin {
		// Non-admin users are not allowed to access pricing in certain states, i.e draft, archieved, etc
		// we should override any queries to ensure they can only see active pricing plans
		if req.GetPricePlansRequest == nil {
			req.GetPricePlansRequest = &pricer.GetPricePlansRequest{}
		}
		req.GetPricePlansRequest.IsNotDeleted = true
		req.GetPricePlansRequest.IsPublished = true
		req.GetPricePlansRequest.WithStatus = string(pricer.PricePlanStatusPublished)
		logger.Debug("non-admin-user-requesting-pricing-plans-only-returning-plans-in-valid-state", zap.String("user-id", req.UserID))
	}

	response, err := s.PricerService.GetPricePlans(ctx, req.GetPricePlansRequest)
	if err != nil {
		logger.Error("failed-to-get-pricing-plans", zap.String("user-id", req.UserID), zap.Error(err))
		return nil, err
	}

	return &GetPricingPlansResponse{GetPricePlansResponse: response}, nil
}

// GetPricePlanBySlug retrieves a pricing plan by slug for external BMS clients.
func (s *Service) GetPricePlanBySlug(ctx context.Context, req *GetPricePlanBySlugRequest) (*GetPricePlanBySlugResponse, error) {
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/billingmanager")

	if s.PricerService == nil {
		logger.Error("pricer-service-not-enabled", zap.String("user-id", req.UserID))
		return nil, ErrBillingManagerPricerServiceNotSet
	}

	response, err := s.PricerService.GetPricePlanBySlug(ctx, req.GetPricePlanBySlugRequest)
	if err != nil {
		return nil, err
	}

	isAdmin := s.isRequesterAdmin(ctx, req.UserID, logger)
	if !isAdmin {
		// Non-admin users are not allowed to access pricing in certain states, i.e draft, archieved, etc
		// we should override any queries to ensure they can only see active pricing plans
		if response.PricePlan.Status != pricer.PricePlanStatusPublished || response.PricePlan.DeletedAt != "" || !isPricePlanPubliclyVisible(response.PricePlan.PublishedAt) {
			logger.Debug("non-admin-user-requesting-pricing-plans-only-returning-plans-in-valid-state", zap.String("user-id", req.UserID))
			return nil, pricer.ErrPricePlanNotFound
		}
	}

	return &GetPricePlanBySlugResponse{GetPricePlanBySlugResponse: response}, nil
}

// GetPricingFeatures retrieves pricing feature catalog items for external BMS clients.
func (s *Service) GetPricingFeatures(ctx context.Context, req *GetPriceFeaturesRequest) (*GetPriceFeaturesResponse, error) {
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/billingmanager")

	if s.PricerService == nil {
		logger.Error("pricer-service-not-enabled", zap.String("user-id", req.UserID))
		return nil, ErrBillingManagerPricerServiceNotSet
	}

	isAdmin := s.isRequesterAdmin(ctx, req.UserID, logger)
	if !isAdmin {
		// Non-admin users are not allowed to access price features in certain states, i.e draft, archieved, etc
		// we should override any queries to ensure they can only see active features
		if req.GetFeaturesRequest == nil {
			req.GetFeaturesRequest = &pricer.GetFeaturesRequest{}
		}

		req.GetFeaturesRequest.IsNotDeleted = true
		req.GetFeaturesRequest.IsPublished = true
		logger.Debug("non-admin-user-requesting-pricing-features-only-returning-features-in-valid-state", zap.String("user-id", req.UserID))
	}

	response, err := s.PricerService.GetFeatures(ctx, req.GetFeaturesRequest)
	if err != nil {
		return nil, err
	}

	return &GetPriceFeaturesResponse{GetFeaturesResponse: response}, nil
}

// isPricePlanPubliclyVisible checks if a price plan is publicly visible based on its published_at timestamp.
func isPricePlanPubliclyVisible(publishedAt string) bool {
	publishedAt = strings.TrimSpace(publishedAt)
	if publishedAt == "" {
		return false
	}

	var publishedAtTime time.Time
	for _, layout := range []string{common.RFC3339NanoUTC, time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, publishedAt)
		if err == nil {
			publishedAtTime = parsed
			break
		}
	}
	if publishedAtTime.IsZero() {
		return false
	}

	return !publishedAtTime.After(time.Now().UTC())
}

// GetUserSubscriptionStatus retrieves a user's subscription status
// This can be called from anywhere in the application
func (s *Service) GetUserSubscriptionStatus(ctx context.Context, req *GetUserSubscriptionStatusRequest) (*GetUserSubscriptionStatusResponse, error) {

	var (
		logger                = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(req.UserID, req.RequestingUserID)
	)

	logger.Info("getting-subscription-status-for-user")

	err := s.isUserAuthorisedToProceedWithUserOperation(ctx, req.UserID, req.RequestingUserID)
	if err != nil {
		logger.Error("failed-to-access-subscription-status-for-user", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Get subscriptions for the user
	subscriptionsResp, err := s.BillingService.GetSubscriptions(ctx, &billing.GetSubscriptionsRequest{
		ForUserIDs: []string{req.UserID},
		PerPage:    100,
		Page:       1,
		Order:      "created_at_desc",
	})
	if err != nil {
		logger.Error("unexpected-error-while-attempting-to-get-user-subscription-status", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Check if user has any subscriptions
	if (subscriptionsResp == nil || subscriptionsResp.Total == 0 || len(subscriptionsResp.Subscriptions) == 0) && s.UserService != nil {
		logger.Info("no-active-subscription-with-user-id-falling-back-to-user-email", logFields...)
		userResp, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: req.UserID})
		if err == nil {
			emailSubsResp, _ := s.BillingService.GetSubscriptionsByEmail(ctx, &billing.GetSubscriptionsByEmailRequest{Email: userResp.User.Email})
			if emailSubsResp != nil && len(emailSubsResp.Subscriptions) > 0 {
				logger.Info("found-email-based-subscription-associating-with-user", append(logFields,
					zap.Bool("email-present", emailPresentForLog(userResp.User.Email)),
					zap.String("email-domain", emailDomainForLog(userResp.User.Email)),
					zap.Int("found-subscriptions", len(emailSubsResp.Subscriptions)),
				)...)
				// Associate found subscriptions with user
				if _, associateErr := s.BillingService.AssociateSubscriptionsWithUser(ctx, &billing.AssociateSubscriptionsWithUserRequest{
					UserID: req.UserID,
					Email:  userResp.User.Email,
				}); associateErr != nil {
					return nil, associateErr
				}

				// Re-query to get updated results
				subscriptionsResp, err = s.BillingService.GetSubscriptions(ctx, &billing.GetSubscriptionsRequest{
					ForUserIDs: []string{req.UserID},
					PerPage:    100,
					Page:       1,
					Order:      "created_at_desc",
				})
			}
		}
	}

	if subscriptionsResp == nil || subscriptionsResp.Total == 0 || len(subscriptionsResp.Subscriptions) == 0 {
		logger.Info("no-active-subscription-found", logFields...)
		return &GetUserSubscriptionStatusResponse{
			SubscriptionStatus: &SubscriptionStatus{
				HasAccess:       false,
				HasSubscription: false,
				Status:          "none",
			},
		}, nil
	}

	subscription := selectPlanAccessRecord(subscriptionsResp.Subscriptions)
	if subscription == nil {
		return &GetUserSubscriptionStatusResponse{SubscriptionStatus: &SubscriptionStatus{Status: "none"}}, nil
	}
	logger.Info("subscription-status-retrieved", append(logFields, zap.String("subscription-id", subscription.ID))...)

	return &GetUserSubscriptionStatusResponse{
		SubscriptionStatus: &SubscriptionStatus{
			HasAccess:            subscription.HasAccess(),
			HasSubscription:      subscription.IsRecurring(),
			BillingKind:          subscriptionBillingKind(subscription),
			PaymentType:          subscriptionPaymentType(subscription),
			IsOneOff:             !subscription.IsRecurring(),
			PaymentStatus:        subscription.PaymentStatus,
			Status:               subscription.Status,
			PlanName:             subscription.PlanName,
			PlanID:               subscription.PlanID,
			PlanSlug:             subscription.PlanSlug,
			CostID:               subscription.CostID,
			ProviderPriceID:      subscription.ProviderPriceID,
			Provider:             subscription.Integrator,
			TransactionID:        subscription.IntegratorTransactionID,
			CustomerID:           subscription.IntegratorCustomerID,
			UserReference:        subscription.UserReference,
			Amount:               subscription.Amount,
			AmountKnown:          subscriptionCommercialAmountKnown(subscription),
			Currency:             subscription.Currency,
			BillingInterval:      subscription.BillingInterval,
			BillingIntervalCount: subscription.BillingIntervalCount,
			Quantity:             subscription.Quantity,
			NextBillingDate:      subscription.NextBillingDate,
			TrialEndsAt:          subscription.ProviderTrialEndsAt,
			AvailableUntilDate:   subscription.AvailableUntilDate,
			CancelURL:            subscription.CancelURL,
			UpdateURL:            subscription.UpdateURL,
			IsActive:             subscription.IsActive(),
			IsInGoodStanding:     subscription.IsInGoodStanding(),
		},
	}, nil
}

// GetUserBillingEvents retrieves billing events for a user
func (s *Service) GetUserBillingEvents(ctx context.Context, req *GetUserBillingEventsRequest) (*GetUserBillingEventsResponse, error) {

	var (
		logger                = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(req.UserID, req.RequestingUserID)
	)

	logger.Info("getting-billing-events-for-user")

	err := s.isUserAuthorisedToProceedWithUserOperation(ctx, req.UserID, req.RequestingUserID)
	if err != nil {
		logger.Error("failed-to-access-billing-events-for-user", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Get billing events for the user
	eventsResp, err := s.BillingService.GetBillingEvents(ctx, &billing.GetBillingEventsRequest{
		ForUserIDs: []string{req.UserID},
		PerPage:    req.PerPage,
		Page:       req.Page,
		Order:      req.Order,
	})
	if err != nil {
		logger.Error("failed-to-retrieve-billing-events-for-user", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Convert to summary format
	events := make([]EventSummary, len(eventsResp.BillingEvents))
	for i, e := range eventsResp.BillingEvents {
		events[i] = EventSummary{
			EventID:         e.ID,
			ProviderEventID: e.IntegratorEventID,
			EventType:       e.EventType,
			EventTime:       e.ProviderEventTime,
			BillingKind:     e.BillingKind,
			PaymentType:     e.PaymentType,
			IsOneOff:        e.IsOneOff,
			PaymentStatus:   e.PaymentStatus,
			TransactionID:   e.IntegratorTransactionID,
			CustomerID:      e.IntegratorCustomerID,
			UserReference:   e.UserReference,
			Amount:          e.Amount,
			Currency:        e.Currency,
			PlanName:        e.PlanName,
			PlanID:          e.PlanID,
			PlanSlug:        e.PlanSlug,
			CostID:          e.CostID,
			ProviderPriceID: e.ProviderPriceID,
			Status:          e.Status,
			ReceiptURL:      e.ReceiptURL,
			Description:     formatEventDescription(e.EventType, e.PlanName, e.Status),
		}
	}

	logger.Info("billing-events-retrieved-for-user", append(logFields, zap.Int("total-events", eventsResp.Total), zap.Int("returned-events", len(events)))...)

	return &GetUserBillingEventsResponse{
		Events:     events,
		Total:      eventsResp.Total,
		TotalPages: eventsResp.TotalPages,
		PerPage:    eventsResp.PerPage,
		Page:       eventsResp.Page,
	}, nil
}

// GetUserBillingDetail retrieves detailed billing information for a user
func (s *Service) GetUserBillingDetail(ctx context.Context, req *GetUserBillingDetailRequest) (*GetUserBillingDetailResponse, error) {

	var (
		logger                = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(req.UserID, req.RequestingUserID)
	)

	logger.Info("getting-billing-detail-for-user")

	err := s.isUserAuthorisedToProceedWithUserOperation(ctx, req.UserID, req.RequestingUserID)
	if err != nil {
		logger.Error("failed-to-access-billing-detail-for-user", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Get subscriptions for the user
	subscriptionsResp, err := s.BillingService.GetSubscriptions(ctx, &billing.GetSubscriptionsRequest{
		ForUserIDs: []string{req.UserID},
		PerPage:    100,
		Page:       1,
		Order:      "created_at_desc",
	})
	if err != nil {
		logger.Error("unexpected-error-while-attempting-to-get-user-billing-detail", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Check if user has any subscriptions
	if subscriptionsResp == nil || subscriptionsResp.Total == 0 || len(subscriptionsResp.Subscriptions) == 0 {
		logger.Info("no-active-subscription-found", logFields...)
		return &GetUserBillingDetailResponse{
			BillingDetail: &BillingDetail{
				HasAccess:       false,
				HasSubscription: false,
				Summary:         "No plan access found",
			},
		}, nil
	}

	subscription := selectPlanAccessRecord(subscriptionsResp.Subscriptions)
	if subscription == nil {
		return &GetUserBillingDetailResponse{BillingDetail: &BillingDetail{Summary: "No plan access found"}}, nil
	}

	detail := &BillingDetail{
		HasAccess:            subscription.HasAccess(),
		HasSubscription:      subscription.IsRecurring(),
		BillingKind:          subscriptionBillingKind(subscription),
		PaymentType:          subscriptionPaymentType(subscription),
		IsOneOff:             !subscription.IsRecurring(),
		PaymentStatus:        subscription.PaymentStatus,
		Provider:             subscription.Integrator,
		Plan:                 subscription.PlanName,
		PlanName:             subscription.PlanName,
		PlanID:               subscription.PlanID,
		PlanSlug:             subscription.PlanSlug,
		CostID:               subscription.CostID,
		ProviderPriceID:      subscription.ProviderPriceID,
		TransactionID:        subscription.IntegratorTransactionID,
		CustomerID:           subscription.IntegratorCustomerID,
		UserReference:        subscription.UserReference,
		Status:               subscription.Status,
		Amount:               subscription.Amount,
		AmountKnown:          subscriptionCommercialAmountKnown(subscription),
		Currency:             subscription.Currency,
		BillingInterval:      subscription.BillingInterval,
		BillingIntervalCount: subscription.BillingIntervalCount,
		Quantity:             subscription.Quantity,
		NextBillingDate:      subscription.NextBillingDate,
		TrialEndsAt:          subscription.ProviderTrialEndsAt,
		AvailableUntilDate:   subscription.AvailableUntilDate,
		CancelURL:            subscription.CancelURL,
		UpdateURL:            subscription.UpdateURL,
	}

	// Generate human-readable summary
	detail.Summary = s.generateSubscriptionSummary(subscription)
	s.enrichUpcomingInvoiceEstimate(ctx, detail, subscription)

	logger.Info("billing-detail-retrieved", logFields...)
	return &GetUserBillingDetailResponse{
		BillingDetail: detail,
	}, nil
}

// enrichUpcomingInvoiceEstimate is deliberately best effort. A provider
// outage or unsupported capability must not make the authenticated billing
// details endpoint unavailable.
func (s *Service) enrichUpcomingInvoiceEstimate(ctx context.Context, detail *BillingDetail, subscription *billing.Subscription) {
	if s == nil || detail == nil || subscription == nil || !subscription.IsRecurring() ||
		(subscription.Status != billing.StatusActive && subscription.Status != billing.StatusTrialing) ||
		strings.TrimSpace(subscription.IntegratorSubscriptionID) == "" ||
		isNilCheckoutCapability(s.UpcomingInvoicePreviewProviderRegistry) {
		return
	}
	provider, err := s.UpcomingInvoicePreviewProviderRegistry.GetUpcomingInvoicePreviewProvider(strings.TrimSpace(subscription.Integrator))
	if err != nil || isNilCheckoutCapability(provider) {
		return
	}
	previewCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	preview, err := provider.CreateUpcomingInvoicePreview(previewCtx, &paymentprovider.UpcomingInvoicePreviewRequest{
		SubscriptionID: strings.TrimSpace(subscription.IntegratorSubscriptionID),
	})
	if err != nil || !validUpcomingInvoicePreview(preview) {
		logger.AcquirePackageFrom(ctx, "external/billingmanager").Warn("upcoming-invoice-preview-unavailable",
			zap.String("provider", subscription.Integrator), zap.Error(err))
		return
	}
	dueDate := parseTimeOrNil(preview.DueDate)
	detail.UpcomingInvoiceEstimate = &UpcomingInvoiceEstimate{
		Estimated: true,
		Subtotal:  preview.Subtotal,
		TaxAmount: preview.TaxAmount,
		Total:     preview.Total,
		AmountDue: preview.AmountDue,
		Currency:  strings.ToUpper(strings.TrimSpace(preview.Currency)),
		DueDate:   dueDate,
	}
}

func validUpcomingInvoicePreview(preview *paymentprovider.UpcomingInvoicePreview) bool {
	if preview == nil || preview.Subtotal < 0 || preview.TaxAmount < 0 || preview.Total < 0 || preview.AmountDue < 0 {
		return false
	}
	currency := strings.ToUpper(strings.TrimSpace(preview.Currency))
	if len(currency) != 3 {
		return false
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	if strings.TrimSpace(preview.DueDate) != "" && parseTimeOrNil(preview.DueDate) == nil {
		return false
	}
	return true
}

// isRequesterAdmin safely checks if the requester has admin privileges
func (s *Service) isRequesterAdmin(ctx context.Context, userID string, logger *zap.Logger) bool {
	if s.UserService == nil {
		return false
	}

	userResp, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: userID})
	if err != nil || userResp == nil || userResp.User == nil {
		logger.Warn("unable-to-resolve-requester-for-admin-check", zap.String("user-id", userID), zap.Error(err))
		return false
	}

	return userResp.User.IsAdmin()
}

// isUserAuthorisedToProceedWithUserOperation checks if the requesting user is authorised to perform operations on behalf of the target user.
// Returns an error if not authorised or prerequisites are not met.
func (s *Service) isUserAuthorisedToProceedWithUserOperation(ctx context.Context, targetUserId, requestingUserId string) error {
	var (
		logger                = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(targetUserId, requestingUserId)
	)

	if targetUserId == "" {
		logger.Warn("failed-to-get-billing-detail-user-id-is-missing", logFields...)
		return ErrBillingManagerRequiresUserIdIsMissing
	}

	if requestingUserId != "" && requestingUserId != targetUserId {

		userResp, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: requestingUserId})
		if err != nil {
			logger.Warn("failed-to-get-billing-detail-requesting-user-not-found", append(logFields, zap.Error(err))...)
			return ErrBillingManagerRequiresUserIdIsMissing
		}
		if !userResp.User.IsAdmin() {
			logger.Warn("failed-to-get-billing-detail-requesting-user-not-admin", logFields...)
			return ErrBillingManagerUserUnauthorisedToCarryOutOperation
		}

		logger.Info("admin-user-requesting-billing-detail-for-another-user", logFields...)
	}
	return nil
}

// resolveUserID attempts to resolve the user ID associated with a payment provider webhook payload.
// note that this may return an empty user ID if only an email is available in the payload but no user
// is found with that email
func (s *Service) resolveUserID(ctx context.Context, providerName string, payload *paymentprovider.WebhookPayload) (string, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/billingmanager")
	if payload == nil {
		return "", ErrBillingManagerNoUserIdentifyingInformationInPayload
	}

	// The stable reference supplied by the application at checkout is the most
	// reliable identity and does not depend on a mutable email address.
	if strings.TrimSpace(payload.UserReference) != "" {
		return strings.TrimSpace(payload.UserReference), nil
	}

	reference := providerAccessReference(payload)
	if reference != "" {
		subResp, lookupErr := s.BillingService.GetSubscriptionByIntegratorID(ctx, &billing.GetSubscriptionByIntegratorIDRequest{
			IntegratorName:           providerName,
			IntegratorSubscriptionID: reference,
		})
		if lookupErr == nil && subResp != nil && subResp.Subscription != nil {
			logger.Info("found-existing-plan-access-using-provider-reference", zap.String("user-id", subResp.Subscription.UserID), zap.String("subscription-id", subResp.Subscription.ID), zap.String("provider", providerName))
			return subResp.Subscription.UserID, nil
		}
		if lookupErr != nil && !errors.Is(lookupErr, billing.ErrBillingSubscriptionNotFound) {
			return "", lookupErr
		}
	}

	if s.UserService != nil && payload.CustomerEmail != "" {
		userResp, userErr := findUserByEmail(ctx, s.UserService, &user.GetUserByEmailRequest{Email: payload.CustomerEmail})
		if userErr == nil {
			logger.Info("found-user-id-falling-back-to-payload-email",
				zap.String("user-id", userResp.User.GetUserId()),
				zap.Bool("payload-email-present", emailPresentForLog(payload.CustomerEmail)),
				zap.String("payload-email-domain", emailDomainForLog(payload.CustomerEmail)),
			)
			return userResp.User.GetUserId(), nil
		}
		if !errors.Is(userErr, user.ErrUserNotFound) {
			return "", userErr
		}
	}

	// if email is missing we need to error out as we have no way to identify the user
	if payload.CustomerEmail == "" {
		logger.Warn("unable-to-identify-user-no-email-in-payload", zap.String("provider-reference", reference), zap.String("customer-id", payload.CustomerID))
		return "", ErrBillingManagerNoUserIdentifyingInformationInPayload
	}

	logger.Info("no-user-found-will-store-subscription-with-email-only",
		zap.Bool("email-present", emailPresentForLog(payload.CustomerEmail)),
		zap.String("email-domain", emailDomainForLog(payload.CustomerEmail)),
	)

	return "", nil
}

// findUserByEmail prefers the optional userByEmailFinder capability when the
// underlying user service implements it, so callers receive expected-absence
// semantics. Otherwise it falls back to the strict GetUserByEmail lookup for
// backward compatibility.
func findUserByEmail(ctx context.Context, userService UserService, req *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error) {
	if finder, ok := userService.(userByEmailFinder); ok {
		return finder.FindUserByEmail(ctx, req)
	}
	return userService.GetUserByEmail(ctx, req)
}

// findOrCreateSubscription finds an existing subscription or creates a new one
func (s *Service) findOrCreateSubscription(ctx context.Context, providerName string, payload *paymentprovider.WebhookPayload, userID string) (*billing.Subscription, error) {

	var (
		logger = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		err    error
	)

	reference := providerAccessReference(payload)
	if reference == "" {
		return nil, paymentprovider.ErrPaymentProviderMissingRequiredField
	}

	if existing, lookupErr := s.findPlanAccess(ctx, providerName, payload); lookupErr == nil {
		logger.Info("found-existing-plan-access-using-integrator-and-reference", zap.String("user-id", existing.UserID), zap.String("subscription-id", existing.ID), zap.String("provider", providerName))
		return existing, nil
	} else if !errors.Is(lookupErr, billing.ErrBillingSubscriptionNotFound) {
		return nil, lookupErr
	}

	// Create new subscription
	nextBillingDate := parseTimeOrNil(payload.NextBillingDate)
	availableUntilDate := parseTimeOrNil(payload.AvailableUntilDate)
	trialEndsAt := parseTimeOrNil(payload.TrialEndsAt)
	providerEventTime := authoritativeProviderEventTime(payload)
	status := payload.Status
	if status == "" && payload.PaymentStatus == paymentprovider.PaymentStatusSucceeded {
		status = billing.StatusActive
	}

	createReq := &billing.CreateSubscriptionRequest{
		UserID:                   userID,
		Email:                    payload.CustomerEmail,
		Status:                   status,
		Integrator:               providerName,
		IntegratorSubscriptionID: reference,
		IntegratorCustomerID:     payload.CustomerID,
		IntegratorTransactionID:  payload.TransactionID,
		UserReference:            payload.UserReference,
		BillingKind:              string(payload.BillingKind),
		PaymentType:              payload.PaymentType,
		IsOneOff:                 payload.IsOneOff,
		PaymentStatus:            payload.PaymentStatus,
		PlanName:                 payload.PlanName,
		PlanID:                   payload.PlanID,
		PlanSlug:                 payload.PlanSlug,
		CostID:                   payload.CostID,
		ProviderPriceID:          payload.ProviderPriceID,
		NextBillingDate:          nextBillingDate,
		AvailableUntilDate:       availableUntilDate,
		TrialEndsAt:              trialEndsAt,
		CancelURL:                payload.CancelURL,
		UpdateURL:                payload.UpdateURL,
		ProviderEventTime:        providerEventTime,
	}
	applyCommercialTermsToCreateRequest(createReq, payload)

	logFields := []zap.Field{
		zap.String("provider", providerName),
		zap.String("provider-reference", reference),
		zap.Bool("email-present", emailPresentForLog(payload.CustomerEmail)),
		zap.String("email-domain", emailDomainForLog(payload.CustomerEmail)),
	}

	// Add user-id to logs if present, otherwise note it's email-only
	if userID != "" {
		logFields = append(logFields, zap.String("user-id", userID))
	} else {
		logFields = append(logFields, zap.String("user-id", "email-only-subscription"))
	}

	if nextBillingDate != nil {
		logFields = append(logFields, zap.String("next-billing-date", nextBillingDate.Format(time.RFC3339)))
	} else {
		logFields = append(logFields, zap.String("next-billing-date", "not-set"))
	}
	if availableUntilDate != nil {
		logFields = append(logFields, zap.String("available-until-date", availableUntilDate.Format(time.RFC3339)))
	} else {
		logFields = append(logFields, zap.String("available-until-date", "not-set"))
	}

	logger.Info("attempting-to-create-new-subscription", logFields...)

	createResp, err := s.BillingService.CreateSubscription(ctx, createReq)
	if err != nil {
		return nil, err
	}
	return createResp.Subscription, nil
}

func applyCommercialTermsToCreateRequest(req *billing.CreateSubscriptionRequest, payload *paymentprovider.WebhookPayload) {
	if req == nil || payload == nil {
		return
	}
	if !payload.IsRecurring() {
		req.Amount = payload.Amount
		req.Currency = payload.Currency
		req.AmountKnown = payload.Amount != 0 || strings.TrimSpace(payload.Currency) != ""
		return
	}
	terms := payload.SubscriptionTerms
	if !terms.Observed && payload.Amount != 0 {
		// Backward compatibility for recurring providers that still expose only
		// the legacy non-zero payment amount. Zero remains unknown because it
		// cannot distinguish an absent value from a free price.
		req.Amount = payload.Amount
		req.AmountKnown = true
		req.Currency = payload.Currency
	}
	if terms.Amount != nil {
		req.Amount = *terms.Amount
		req.AmountKnown = true
	}
	if terms.Currency != nil {
		req.Currency = strings.ToUpper(strings.TrimSpace(*terms.Currency))
	}
	if terms.BillingInterval != nil {
		req.BillingInterval = strings.ToLower(strings.TrimSpace(*terms.BillingInterval))
	}
	if terms.BillingIntervalCount != nil {
		req.BillingIntervalCount = *terms.BillingIntervalCount
	}
	if terms.Quantity != nil {
		req.Quantity = *terms.Quantity
	}
}

func applyCommercialTermsToUpdateRequest(req *billing.UpdateSubscriptionRequest, payload *paymentprovider.WebhookPayload) {
	if req == nil || payload == nil {
		return
	}
	if !payload.IsRecurring() {
		if payload.Amount != 0 || strings.TrimSpace(payload.Currency) != "" {
			amountKnown := true
			req.Amount = &payload.Amount
			req.AmountKnown = &amountKnown
		}
		if payload.Currency != "" {
			req.Currency = &payload.Currency
		}
		return
	}
	terms := payload.SubscriptionTerms
	if !terms.Observed && payload.Amount != 0 {
		amountKnown := true
		req.Amount = &payload.Amount
		req.AmountKnown = &amountKnown
		if payload.Currency != "" {
			req.Currency = &payload.Currency
		}
	}
	if terms.Observed && terms.Amount == nil {
		amount := int64(0)
		amountKnown := false
		req.Amount = &amount
		req.AmountKnown = &amountKnown
	}
	if terms.Amount != nil {
		amountKnown := true
		req.Amount = terms.Amount
		req.AmountKnown = &amountKnown
	}
	if terms.Currency != nil {
		currency := strings.ToUpper(strings.TrimSpace(*terms.Currency))
		req.Currency = &currency
	}
	if terms.BillingInterval != nil {
		interval := strings.ToLower(strings.TrimSpace(*terms.BillingInterval))
		req.BillingInterval = &interval
	}
	if terms.BillingIntervalCount != nil {
		req.BillingIntervalCount = terms.BillingIntervalCount
	}
	if terms.Quantity != nil {
		req.Quantity = terms.Quantity
	}
}

func authoritativeProviderEventTime(payload *paymentprovider.WebhookPayload) *time.Time {
	if payload == nil || !payload.SubscriptionStateAuthoritative {
		return nil
	}
	return parseTimeOrNil(payload.EventTime)
}

// findPlanAccess locates the access record identified by a normalized webhook reference.
func (s *Service) findPlanAccess(ctx context.Context, providerName string, payload *paymentprovider.WebhookPayload) (*billing.Subscription, error) {
	reference := providerAccessReference(payload)
	if reference == "" {
		return nil, billing.ErrBillingSubscriptionNotFound
	}
	response, err := s.BillingService.GetSubscriptionByIntegratorID(ctx, &billing.GetSubscriptionByIntegratorIDRequest{
		IntegratorName:           providerName,
		IntegratorSubscriptionID: reference,
	})
	if err != nil {
		return nil, err
	}
	if response == nil || response.Subscription == nil {
		return nil, billing.ErrBillingSubscriptionNotFound
	}
	return response.Subscription, nil
}

// updateSubscriptionFromPayload updates a subscription based on webhook data
func (s *Service) updateSubscriptionFromPayload(ctx context.Context, subscription *billing.Subscription, payload *paymentprovider.WebhookPayload, userID string) error {

	var (
		logger    = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		logFields = []zap.Field{
			zap.String("subscription-id", subscription.ID),
		}
	)
	providerEventTime := authoritativeProviderEventTime(payload)
	if providerEventTime != nil && subscription.ProviderUpdatedAt.IsZero() {
		latestEventTime, err := s.latestSubscriptionLifecycleEventTime(ctx, subscription)
		if err != nil {
			return err
		}
		if latestEventTime != nil && providerEventTime.Before(*latestEventTime) {
			logger.Info("ignoring-older-subscription-lifecycle-event", append(logFields,
				zap.Time("provider-event-time", *providerEventTime),
				zap.Time("latest-ledger-event-time", *latestEventTime),
			)...)
			return nil
		}
	}
	if payload.SubscriptionStateAuthoritative && providerEventTime == nil && !subscription.ProviderUpdatedAt.IsZero() {
		logger.Info("ignoring-undated-subscription-lifecycle-event", logFields...)
		return nil
	}
	if providerEventTime != nil && !subscription.ProviderUpdatedAt.IsZero() && providerEventTime.Before(subscription.ProviderUpdatedAt) {
		logger.Info("ignoring-older-subscription-lifecycle-event", append(logFields,
			zap.Time("provider-event-time", *providerEventTime),
			zap.Time("stored-provider-event-time", subscription.ProviderUpdatedAt),
		)...)
		return nil
	}

	updateReq := &billing.UpdateSubscriptionRequest{ID: subscription.ID}
	if userID != "" {
		updateReq.UserID = &userID
	}
	status := payload.Status
	if payload.EventType == paymentprovider.EventTypePaymentRefunded {
		status = billing.StatusCancelled
	}
	if status != "" {
		updateReq.Status = &status
	}
	if payload.BillingKind != "" {
		billingKind := string(payload.BillingKind)
		updateReq.BillingKind = &billingKind
	}
	if payload.PaymentType != "" {
		updateReq.PaymentType = &payload.PaymentType
	}
	if payload.BillingKind != "" || payload.PaymentType != "" {
		updateReq.IsOneOff = &payload.IsOneOff
	}
	if payload.PaymentStatus != "" {
		updateReq.PaymentStatus = &payload.PaymentStatus
	}
	if payload.TransactionID != "" {
		updateReq.IntegratorTransactionID = &payload.TransactionID
	}
	if payload.CustomerID != "" {
		updateReq.IntegratorCustomerID = &payload.CustomerID
	}
	if payload.UserReference != "" && (subscription.UserID == "" || strings.TrimSpace(subscription.UserID) == strings.TrimSpace(payload.UserReference)) {
		updateReq.UserReference = &payload.UserReference
		updateReq.UserID = &payload.UserReference
	}
	if payload.CustomerEmail != "" {
		updateReq.Email = &payload.CustomerEmail
	}

	// Update dates if present
	if payload.NextBillingDate != "" {
		logger.Debug("updating-next-billing-date", append(logFields, zap.String("next-billing-date", payload.NextBillingDate))...)
		nextBillingDate := parseTimeOrNil(payload.NextBillingDate)
		updateReq.NextBillingDate = nextBillingDate
	}

	if payload.AvailableUntilDate != "" {
		logger.Debug("updating-available-until-date", append(logFields, zap.String("available-until-date", payload.AvailableUntilDate))...)
		availableUntilDate := parseTimeOrNil(payload.AvailableUntilDate)
		updateReq.AvailableUntilDate = availableUntilDate
	}

	// Update plan name if present
	if payload.PlanName != "" {
		logger.Debug("updating-plan-name", append(logFields, zap.String("plan-name", payload.PlanName))...)
		updateReq.PlanName = &payload.PlanName
	}
	if payload.PlanID != "" {
		updateReq.PlanID = &payload.PlanID
	}
	if payload.PlanSlug != "" {
		updateReq.PlanSlug = &payload.PlanSlug
	}
	if payload.CostID != "" {
		updateReq.CostID = &payload.CostID
	}
	if payload.ProviderPriceID != "" {
		updateReq.ProviderPriceID = &payload.ProviderPriceID
	}
	applyCommercialTermsToUpdateRequest(updateReq, payload)
	if payload.TrialEndsAt != "" {
		updateReq.TrialEndsAt = parseTimeOrNil(payload.TrialEndsAt)
	}
	if providerEventTime != nil {
		updateReq.ProviderUpdatedAt = providerEventTime
	}

	// Update URLs if present
	if payload.CancelURL != "" {
		logger.Debug("updating-cancel-url", append(logFields, zap.String("cancel-url", payload.CancelURL))...)
		updateReq.CancelURL = &payload.CancelURL
	}
	if payload.UpdateURL != "" {
		logger.Debug("updating-update-url", append(logFields, zap.String("update-url", payload.UpdateURL))...)
		updateReq.UpdateURL = &payload.UpdateURL
	}

	// Handle cancellation
	if payload.EventType == paymentprovider.EventTypeSubscriptionCancelled || payload.EventType == paymentprovider.EventTypePaymentRefunded {
		logger.Info("marking-subscription-as-cancelled", append(logFields, zap.String("user-id", payload.CustomerID))...)
		now := time.Now()
		updateReq.CancelledAt = &now
	}

	_, err := s.BillingService.UpdateSubscription(ctx, updateReq)
	return err
}

func (s *Service) latestSubscriptionLifecycleEventTime(ctx context.Context, subscription *billing.Subscription) (*time.Time, error) {
	if s == nil || subscription == nil || strings.TrimSpace(subscription.IntegratorSubscriptionID) == "" {
		return nil, nil
	}
	response, err := s.BillingService.GetBillingEvents(ctx, &billing.GetBillingEventsRequest{
		IntegratorName:           subscription.Integrator,
		IntegratorSubscriptionID: subscription.IntegratorSubscriptionID,
		EventTypes: []string{
			paymentprovider.EventTypeSubscriptionCreated,
			paymentprovider.EventTypeSubscriptionUpdated,
			paymentprovider.EventTypeSubscriptionCancelled,
			paymentprovider.EventTypeSubscriptionPaused,
			paymentprovider.EventTypeSubscriptionResumed,
		},
		Order:   "event_time_desc",
		PerPage: 1,
		Page:    1,
	})
	if err != nil {
		return nil, err
	}
	if response == nil || len(response.BillingEvents) == 0 || response.BillingEvents[0].ProviderEventTime.IsZero() {
		return nil, nil
	}
	latest := response.BillingEvents[0].ProviderEventTime.UTC()
	return &latest, nil
}

// createBillingEvent creates an audit trail event
func (s *Service) createBillingEvent(ctx context.Context, subscriptionID, userID, providerName string, payload *paymentprovider.WebhookPayload) error {

	logger := logger.AcquirePackageFrom(ctx, "external/billingmanager")
	logFields := []zap.Field{
		zap.String("subscription-id", subscriptionID), zap.String("user-id", userID), zap.String("provider", providerName), zap.String("event-type", payload.EventType), zap.String("event-id", payload.EventID),
	}

	logger.Info("creating-billing-event", logFields...)
	eventTime := parseTimeOrNil(payload.EventTime)
	if eventTime == nil {
		now := time.Now()
		eventTime = &now
	}

	createReq := &billing.CreateBillingEventRequest{
		SubscriptionID:           subscriptionID,
		UserID:                   userID,
		EventType:                payload.EventType,
		Integrator:               providerName,
		IntegratorEventID:        payload.EventID,
		IntegratorSubscriptionID: payload.SubscriptionID,
		IntegratorTransactionID:  payload.TransactionID,
		IntegratorCustomerID:     payload.CustomerID,
		UserReference:            payload.UserReference,
		BillingKind:              string(payload.BillingKind),
		PaymentType:              payload.PaymentType,
		IsOneOff:                 payload.IsOneOff,
		PaymentStatus:            payload.PaymentStatus,
		Status:                   payload.Status,
		Amount:                   payload.Amount,
		Currency:                 payload.Currency,
		PlanName:                 payload.PlanName,
		PlanID:                   payload.PlanID,
		PlanSlug:                 payload.PlanSlug,
		CostID:                   payload.CostID,
		ProviderPriceID:          payload.ProviderPriceID,
		Email:                    payload.CustomerEmail,
		ReceiptURL:               payload.ReceiptURL,
		RawPayload:               payload.RawPayload,
		EventTime:                *eventTime,
	}

	_, err := s.BillingService.CreateBillingEvent(ctx, createReq)

	if errors.Is(err, billing.ErrBillingEventAlreadyProcessed) {
		logger.Info("billing-event-already-processed", logFields...)
		return nil
	}
	if err != nil {
		logger.Error("failed-to-create-billing-event", append(logFields, zap.Error(err))...)
		return err
	}

	logger.Info("billing-event-created", logFields...)
	return nil
}

// normaliseLegacyWebhookPayload fills canonical one-time and payment fields for older payloads.
func normaliseLegacyWebhookPayload(payload *paymentprovider.WebhookPayload) {
	if payload == nil {
		return
	}
	if payload.BillingKind == "" {
		switch {
		case payload.IsOneOff:
			payload.BillingKind = paymentprovider.BillingKindOneTime
		case payload.PaymentType == paymentprovider.PaymentTypeSubscription || payload.SubscriptionID != "":
			payload.BillingKind = paymentprovider.BillingKindRecurring
		}
	}
	if payload.BillingKind == paymentprovider.BillingKindOneTime {
		payload.IsOneOff = true
		if payload.PaymentType == paymentprovider.PaymentTypeSubscription {
			payload.PaymentType = paymentprovider.PaymentTypePurchase
		}
		if payload.TransactionID == "" {
			payload.TransactionID = payload.SubscriptionID
		}
	}
	if payload.PaymentType == "" {
		if payload.BillingKind == paymentprovider.BillingKindRecurring {
			payload.PaymentType = paymentprovider.PaymentTypeSubscription
		} else if payload.BillingKind == paymentprovider.BillingKindOneTime &&
			(payload.PlanID != "" || payload.PlanSlug != "" || payload.CostID != "" || payload.ProviderPriceID != "" || payload.PlanName != "") {
			payload.PaymentType = paymentprovider.PaymentTypePurchase
		}
	}
	if payload.PaymentStatus == "" {
		switch payload.EventType {
		case paymentprovider.EventTypePaymentSucceeded, paymentprovider.EventTypeSubscriptionCreated:
			payload.PaymentStatus = paymentprovider.PaymentStatusSucceeded
		case paymentprovider.EventTypePaymentFailed:
			payload.PaymentStatus = paymentprovider.PaymentStatusFailed
		case paymentprovider.EventTypePaymentRefunded:
			payload.PaymentStatus = paymentprovider.PaymentStatusRefunded
		case paymentprovider.EventTypePaymentActionRequired:
			payload.PaymentStatus = paymentprovider.PaymentStatusActionRequired
		}
	}
	if payload.Status == "" {
		switch payload.PaymentStatus {
		case paymentprovider.PaymentStatusSucceeded:
			payload.Status = billing.StatusActive
		case paymentprovider.PaymentStatusRefunded:
			payload.Status = billing.StatusCancelled
		case paymentprovider.PaymentStatusFailed, paymentprovider.PaymentStatusActionRequired:
			payload.Status = billing.StatusIncomplete
		}
	}
}

// providerAccessReference chooses the stable provider identifier for access persistence.
func providerAccessReference(payload *paymentprovider.WebhookPayload) string {
	if payload == nil {
		return ""
	}
	if payload.IsRecurring() {
		return strings.TrimSpace(payload.SubscriptionID)
	}
	return strings.TrimSpace(firstNonEmptyString(payload.TransactionID, payload.SubscriptionID, payload.EventID))
}

// isSuccessfulOneOffAccess reports whether a payment webhook can grant one-time plan access.
func isSuccessfulOneOffAccess(payload *paymentprovider.WebhookPayload) bool {
	return payload != nil && payload.GrantsPlanAccess() && !payload.IsRecurring() &&
		(payload.PaymentStatus == paymentprovider.PaymentStatusSucceeded || payload.EventType == paymentprovider.EventTypePaymentSucceeded) &&
		hasPlanAccessMetadata(payload)
}

// hasPlanAccessMetadata reports whether a payment contains trusted plan correlation metadata.
func hasPlanAccessMetadata(payload *paymentprovider.WebhookPayload) bool {
	return payload != nil && strings.TrimSpace(firstNonEmptyString(
		payload.PlanID,
		payload.PlanSlug,
		payload.CostID,
		payload.ProviderPriceID,
		payload.PlanName,
	)) != ""
}

// selectPlanAccessRecord prefers an access-bearing record when several records are returned.
func selectPlanAccessRecord(records []billing.Subscription) *billing.Subscription {
	for i := range records {
		if records[i].HasAccess() {
			return &records[i]
		}
	}
	if len(records) == 0 {
		return nil
	}
	return &records[0]
}

// subscriptionBillingKind returns the canonical billing kind with legacy inference as fallback.
func subscriptionBillingKind(record *billing.Subscription) string {
	if record == nil {
		return ""
	}
	if record.BillingKind != "" {
		return record.BillingKind
	}
	if record.IsRecurring() {
		return string(paymentprovider.BillingKindRecurring)
	}
	return string(paymentprovider.BillingKindOneTime)
}

// subscriptionPaymentType returns the canonical payment type with legacy inference as fallback.
func subscriptionPaymentType(record *billing.Subscription) string {
	if record == nil {
		return ""
	}
	if record.PaymentType != "" {
		return record.PaymentType
	}
	if record.IsRecurring() {
		return paymentprovider.PaymentTypeSubscription
	}
	return paymentprovider.PaymentTypePurchase
}

// firstNonEmptyString returns the first non-blank, trimmed value.
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// generateSubscriptionSummary creates a human-readable summary of the subscription
func (s *Service) generateSubscriptionSummary(sub *billing.Subscription) string {
	if sub == nil {
		return "No plan access found"
	}
	if !sub.IsRecurring() {
		switch sub.Status {
		case billing.StatusActive, billing.StatusTrialing:
			return fmt.Sprintf("Your one-time access to %s is active and does not renew", sub.PlanName)
		case billing.StatusCancelled, billing.StatusExpired:
			if sub.PaymentStatus == paymentprovider.PaymentStatusRefunded {
				return fmt.Sprintf("Your %s purchase was refunded and access is inactive", sub.PlanName)
			}
			return fmt.Sprintf("Your one-time access to %s is inactive", sub.PlanName)
		default:
			return fmt.Sprintf("One-time access status: %s", sub.Status)
		}
	}
	switch sub.Status {
	case billing.StatusActive:
		if sub.AmountKnown && sub.Amount == 0 {
			return fmt.Sprintf("Your %s plan is active with no recurring charge", sub.PlanName)
		}
		if sub.NextBillingDate != nil {
			return fmt.Sprintf("Your %s plan will automatically renew on %s. Review your billing details for the estimated amount due",
				sub.PlanName, sub.NextBillingDate.Format("02 January, 2006"))
		}
		return fmt.Sprintf("Your %s plan is active", sub.PlanName)

	case billing.StatusTrialing:
		if sub.NextBillingDate != nil {
			if sub.AmountKnown && sub.Amount == 0 {
				return fmt.Sprintf("Your trial of %s will end on %s and continue with no recurring charge",
					sub.PlanName, sub.NextBillingDate.Format("02 January, 2006"))
			}
			return fmt.Sprintf("Your trial of %s will end on %s. Your subscription will then begin",
				sub.PlanName, sub.NextBillingDate.Format("02 January, 2006"))
		}
		return fmt.Sprintf("You're on a trial of %s", sub.PlanName)

	case billing.StatusPastDue:
		return "Your subscription payment is past due. Please update your payment method."

	case billing.StatusCancelled:
		if sub.AvailableUntilDate != nil {
			return fmt.Sprintf("Your subscription was cancelled and will expire on %s",
				sub.AvailableUntilDate.Format("02 January, 2006"))
		}
		return "Your subscription has been cancelled"

	default:
		return fmt.Sprintf("Subscription status: %s", sub.Status)
	}
}

func subscriptionCommercialAmountKnown(sub *billing.Subscription) bool {
	return sub != nil && (sub.AmountKnown || sub.Amount != 0)
}

// initLogFieldsWithUserIdAndRequestingUserId initialises log fields with user ID and requesting user ID
func initLogFieldsWithUserIdAndRequestingUserId(userId, requestingUserId string) []zap.Field {
	var logFields []zap.Field
	if userId != "" {
		logFields = append(logFields, zap.String("user-id", userId))
	}
	if requestingUserId != "" {
		logFields = append(logFields, zap.String("requesting-user-id", requestingUserId))
	}
	return logFields
}

// webhookPayloadFieldsForLog builds privacy-safe webhook fields for structured logging.
func webhookPayloadFieldsForLog(providerName string, userID string, payload *paymentprovider.WebhookPayload) []zap.Field {
	fields := []zap.Field{
		zap.String("provider", providerName),
		zap.String("user-id", userID),
	}
	if payload == nil {
		return fields
	}

	return append(fields,
		zap.String("event-type", payload.EventType),
		zap.String("event-id", payload.EventID),
		zap.String("subscription-id", payload.SubscriptionID),
		zap.String("customer-id", payload.CustomerID),
		zap.Bool("is-subscription", payload.IsSubscription()),
	)
}
