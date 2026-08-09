package billingmanager

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"go.uber.org/zap"
)

const (
	portalSubscriptionPageSize = 100
	portalSubscriptionMaxPages = 100
)

// ProcessBillingProviderPortal validates an authenticated portal request,
// resolves the provider customer from server-owned recurring subscriptions,
// and delegates creation of a fresh hosted session to the named provider.
func (s *Service) ProcessBillingProviderPortal(ctx context.Context, req *ProcessBillingProviderPortalRequest) (*ProcessBillingProviderPortalResponse, error) {
	log := logger.AcquireOperationFrom(ctx, "external/billingmanager", "process-billing-provider-portal")
	if s == nil || req == nil {
		return nil, ErrInvalidBillingManagerRequestPayload
	}

	userID := strings.TrimSpace(req.UserID)
	providerName := normaliseCheckoutProviderName(req.ProviderName)
	if userID == "" || !checkoutProviderNamePattern.MatchString(providerName) ||
		len(strings.TrimSpace(req.Origin)) > checkoutMaxOriginLength || len(strings.TrimSpace(req.SecFetchSite)) > checkoutMaxFetchSiteLength {
		return nil, ErrInvalidBillingManagerRequestPayload
	}
	if isNilCheckoutCapability(s.CustomerPortalProviderRegistry) || isNilCheckoutCapability(s.BillingService) {
		log.Error("customer-portal-service-not-configured", zap.String("provider", providerName))
		return nil, ErrBillingManagerPortalConfigurationInvalid
	}

	portalProvider, err := s.CustomerPortalProviderRegistry.GetCustomerPortalProvider(providerName)
	if errors.Is(err, paymentprovider.ErrPaymentProviderInvalidConfiguration) {
		return nil, ErrBillingManagerPortalConfigurationInvalid
	}
	if err != nil || isNilCheckoutCapability(portalProvider) {
		log.Warn("customer-portal-provider-not-available", zap.String("provider", providerName), zap.Error(err))
		return nil, ErrBillingManagerPortalProviderUnavailable
	}
	if validator, ok := portalProvider.(paymentprovider.CustomerPortalConfigValidator); ok {
		if err := validator.ValidateCustomerPortalConfig(); err != nil {
			return nil, ErrBillingManagerPortalConfigurationInvalid
		}
	}
	configuredProvider, ok := portalProvider.(paymentprovider.CustomerPortalReturnURLProvider)
	if !ok || isNilCheckoutCapability(configuredProvider) {
		return nil, ErrBillingManagerPortalConfigurationInvalid
	}
	sessionURLValidator, ok := portalProvider.(paymentprovider.CustomerPortalSessionURLValidator)
	if !ok || isNilCheckoutCapability(sessionURLValidator) {
		return nil, ErrBillingManagerPortalConfigurationInvalid
	}
	returnURL := strings.TrimSpace(configuredProvider.GetCustomerPortalReturnURL())
	allowedOrigin, err := portalReturnURLOrigin(returnURL)
	if err != nil {
		return nil, ErrBillingManagerPortalConfigurationInvalid
	}
	if !checkoutRequestOriginAllowed(req.Origin, req.SecFetchSite, allowedOrigin) {
		log.Warn("customer-portal-request-origin-rejected", zap.String("provider", providerName))
		return nil, ErrBillingManagerPortalOriginRejected
	}

	customerID, err := s.resolvePortalCustomer(ctx, userID, providerName)
	if err != nil {
		log.Warn("customer-portal-billing-query-failed", zap.String("provider", providerName), zap.String("user-id", userID), zap.Error(err))
		if errors.Is(err, ErrBillingManagerPortalCustomerAmbiguous) {
			return nil, ErrBillingManagerPortalCustomerAmbiguous
		}
		return nil, ErrBillingManagerPortalBillingUnavailable
	}
	if customerID == "" {
		return nil, ErrBillingManagerPortalSubscriptionUnavailable
	}

	session, err := portalProvider.CreateCustomerPortalSession(ctx, &paymentprovider.CustomerPortalSessionRequest{
		CustomerID: customerID,
		ReturnURL:  returnURL,
	})
	if err != nil {
		log.Warn("customer-portal-session-creation-failed", zap.String("provider", providerName), zap.String("user-id", userID), zap.Error(err))
		return nil, ErrBillingManagerPortalProviderRequestFailed
	}
	if session == nil || strings.TrimSpace(session.ID) == "" || !isBrowserSafeHostedPortalURL(session.URL) ||
		sessionURLValidator.ValidateCustomerPortalSessionURL(session.URL) != nil {
		log.Warn("customer-portal-session-invalid", zap.String("provider", providerName), zap.String("user-id", userID))
		return nil, ErrBillingManagerPortalSessionInvalid
	}
	session.ID = strings.TrimSpace(session.ID)
	session.URL = strings.TrimSpace(session.URL)
	return &ProcessBillingProviderPortalResponse{Session: session}, nil
}

// resolvePortalCustomer scans a bounded, internally filtered subscription set.
// Active and trialing customers win; otherwise a single recurring lifecycle
// customer remains eligible for payment recovery, invoices, or cancellation
// state. Multiple customer identities in the selected tier fail closed.
func (s *Service) resolvePortalCustomer(ctx context.Context, userID, providerName string) (string, error) {
	var (
		preferredCustomerIDs = make(map[string]struct{})
		fallbackCustomerIDs  = make(map[string]struct{})
		expectedTotal        = -1
		expectedTotalPages   = -1
	)
	for page := 1; page <= portalSubscriptionMaxPages; page++ {
		response, err := s.BillingService.GetSubscriptions(ctx, &billing.GetSubscriptionsRequest{
			Order:          "created_at_desc",
			PerPage:        portalSubscriptionPageSize,
			Page:           page,
			IntegratorName: providerName,
			ForUserIDs:     []string{userID},
		})
		if err != nil || !portalSubscriptionPageMetadataValid(response, page) {
			return "", ErrBillingManagerPortalBillingUnavailable
		}
		if page == 1 {
			expectedTotal = response.Total
			expectedTotalPages = response.TotalPages
		} else if response.Total != expectedTotal || response.TotalPages != expectedTotalPages {
			return "", ErrBillingManagerPortalBillingUnavailable
		}

		for index := range response.Subscriptions {
			subscription := &response.Subscriptions[index]
			customerID := strings.TrimSpace(subscription.IntegratorCustomerID)
			if strings.TrimSpace(subscription.UserID) != userID ||
				!strings.EqualFold(strings.TrimSpace(subscription.Integrator), providerName) ||
				!subscription.IsRecurring() || customerID == "" {
				continue
			}
			if isPreferredPortalSubscriptionStatus(subscription.Status) {
				preferredCustomerIDs[customerID] = struct{}{}
			} else {
				fallbackCustomerIDs[customerID] = struct{}{}
			}
		}
		if expectedTotalPages <= page {
			break
		}
	}
	if len(preferredCustomerIDs) > 1 {
		return "", ErrBillingManagerPortalCustomerAmbiguous
	}
	for customerID := range preferredCustomerIDs {
		return customerID, nil
	}
	if len(fallbackCustomerIDs) > 1 {
		return "", ErrBillingManagerPortalCustomerAmbiguous
	}
	for customerID := range fallbackCustomerIDs {
		return customerID, nil
	}
	return "", nil
}

func isPreferredPortalSubscriptionStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case billing.StatusActive, billing.StatusTrialing:
		return true
	default:
		return false
	}
}

func portalSubscriptionPageMetadataValid(response *billing.GetSubscriptionsResponse, page int) bool {
	if response == nil || page < 1 || response.Page != page || response.PerPage != portalSubscriptionPageSize ||
		response.Total < 0 || response.TotalPages < 0 || response.TotalPages > portalSubscriptionMaxPages ||
		len(response.Subscriptions) > portalSubscriptionPageSize {
		return false
	}
	expectedTotalPages := response.Total / portalSubscriptionPageSize
	if response.Total%portalSubscriptionPageSize != 0 {
		expectedTotalPages++
	}
	if response.TotalPages != expectedTotalPages {
		return false
	}
	if response.TotalPages == 0 {
		return page == 1 && len(response.Subscriptions) == 0
	}
	if page > response.TotalPages {
		return false
	}
	remaining := response.Total - ((page - 1) * portalSubscriptionPageSize)
	expectedPageSize := portalSubscriptionPageSize
	if remaining < expectedPageSize {
		expectedPageSize = remaining
	}
	return expectedPageSize >= 0 && len(response.Subscriptions) == expectedPageSize
}

func portalReturnURLOrigin(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.ContainsAny(rawURL, "{}") {
		return "", ErrBillingManagerPortalConfigurationInvalid
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" ||
		!hasValidHTTPPort(parsed) || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return "", ErrBillingManagerPortalConfigurationInvalid
	}
	return canonicalHTTPOrigin(parsed), nil
}

// isBrowserSafeHostedPortalURL enforces a generic HTTPS baseline. Providers
// remain responsible for validating the URL against their own hosted allowlist.
func isBrowserSafeHostedPortalURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.IsAbs() && strings.EqualFold(parsed.Scheme, "https") &&
		parsed.Host != "" && parsed.Hostname() != "" && parsed.User == nil && parsed.Opaque == "" && hasValidHTTPPort(parsed)
}
