package billingmanager

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/pricer"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

const (
	checkoutCataloguePageSize  = 100
	checkoutCatalogueMaxPages  = 100
	checkoutMaxPriceIDLength   = 512
	checkoutMaxClientKeyLength = 512
	checkoutMaxOriginLength    = 2048
	checkoutMaxFetchSiteLength = 32
)

var checkoutBracePlaceholderPattern = regexp.MustCompile(`\{[^{}\s]+\}`)
var checkoutProviderNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type checkoutSelection struct {
	plan pricer.PricePlan
	cost pricer.PriceCost
	mode string
}

// ProcessBillingProviderCheckout validates an authenticated checkout against
// the published catalogue and delegates session creation to the named payment
// provider. Provider webhooks remain authoritative for access fulfilment.
func (s *Service) ProcessBillingProviderCheckout(ctx context.Context, req *ProcessBillingProviderCheckoutRequest) (*ProcessBillingProviderCheckoutResponse, error) {
	log := logger.AcquireOperationFrom(ctx, "external/billingmanager", "process-billing-provider-checkout")
	if s == nil || req == nil {
		return nil, ErrInvalidBillingManagerRequestPayload
	}

	userID := strings.TrimSpace(req.UserID)
	providerName := normaliseCheckoutProviderName(req.ProviderName)
	priceID := strings.TrimSpace(req.PriceID)
	if userID == "" || !checkoutProviderNamePattern.MatchString(providerName) || priceID == "" ||
		len(priceID) > checkoutMaxPriceIDLength || len(strings.TrimSpace(req.IdempotencyKey)) > checkoutMaxClientKeyLength ||
		len(strings.TrimSpace(req.Origin)) > checkoutMaxOriginLength || len(strings.TrimSpace(req.SecFetchSite)) > checkoutMaxFetchSiteLength {
		return nil, ErrInvalidBillingManagerRequestPayload
	}
	if s.CheckoutProviderRegistry == nil || s.PricerService == nil || s.UserService == nil {
		log.Error("checkout-service-not-configured", zap.String("provider", providerName))
		return nil, ErrBillingManagerCheckoutConfigurationInvalid
	}

	checkoutProvider, err := s.CheckoutProviderRegistry.GetCheckoutProvider(providerName)
	if errors.Is(err, paymentprovider.ErrPaymentProviderInvalidConfiguration) {
		log.Error("checkout-provider-configuration-invalid", zap.String("provider", providerName))
		return nil, ErrBillingManagerCheckoutConfigurationInvalid
	}
	if err != nil || isNilCheckoutCapability(checkoutProvider) {
		log.Warn("checkout-provider-not-available", zap.String("provider", providerName), zap.Error(err))
		return nil, ErrBillingManagerCheckoutProviderUnavailable
	}
	if validator, ok := checkoutProvider.(paymentprovider.CheckoutConfigValidator); ok {
		if err := validator.ValidateCheckoutConfig(); err != nil {
			log.Error("checkout-provider-configuration-invalid", zap.String("provider", providerName))
			return nil, ErrBillingManagerCheckoutConfigurationInvalid
		}
	}

	returnURL, returnURLConflict := s.checkoutProviderReturnURL(providerName, checkoutProvider)
	if returnURLConflict {
		log.Warn("checkout-provider-return-url-overrides-deprecated-config", zap.String("provider", providerName))
	}
	allowedOrigin, err := checkoutReturnURLOrigin(returnURL)
	if err != nil {
		log.Error("checkout-provider-return-url-invalid", zap.String("provider", providerName), zap.Error(err))
		return nil, ErrBillingManagerCheckoutConfigurationInvalid
	}
	if !checkoutRequestOriginAllowed(req.Origin, req.SecFetchSite, allowedOrigin) {
		log.Warn("checkout-request-origin-rejected", zap.String("provider", providerName))
		return nil, ErrBillingManagerCheckoutOriginRejected
	}

	selection, err := s.resolvePublishedCheckoutPrice(ctx, providerName, priceID)
	if err != nil {
		return nil, err
	}

	userResponse, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: userID})
	if err != nil || userResponse == nil || userResponse.User == nil ||
		strings.TrimSpace(userResponse.User.ID) != userID || strings.TrimSpace(userResponse.User.GetUserEmail()) == "" {
		log.Warn("checkout-user-billing-identity-unavailable", zap.String("provider", providerName), zap.String("user-id", userID), zap.Error(err))
		return nil, ErrBillingManagerCheckoutUserUnavailable
	}

	idempotencyKey, err := checkoutIdempotencyKey(userID, providerName, priceID, req.IdempotencyKey)
	if err != nil {
		log.Error("checkout-idempotency-key-generation-failed", zap.String("provider", providerName), zap.String("user-id", userID), zap.Error(err))
		return nil, ErrBillingManagerCheckoutIdempotencyFailed
	}
	returnURL, err = enrichCheckoutReturnURL(returnURL, selection)
	if err != nil {
		log.Error("checkout-return-url-enrichment-failed", zap.String("provider", providerName), zap.Error(err))
		return nil, ErrBillingManagerCheckoutConfigurationInvalid
	}

	session, err := checkoutProvider.CreateCheckoutSession(ctx, &paymentprovider.CheckoutSessionRequest{
		PriceID:                priceID,
		PlanID:                 selection.plan.ID,
		PlanSlug:               selection.plan.Slug,
		PlanName:               selection.plan.Name,
		CostID:                 selection.cost.ID,
		UserID:                 userID,
		UserReference:          userID,
		CustomerEmail:          strings.TrimSpace(userResponse.User.GetUserEmail()),
		Mode:                   selection.mode,
		ReturnURL:              returnURL,
		ExpectedAmount:         selection.cost.Amount,
		ExpectedCurrency:       selection.cost.Currency,
		ExpectedBillingCadence: string(selection.cost.BillingCadence),
		TrialPeriodDays:        selection.cost.TrialPeriodDays,
		IdempotencyKey:         idempotencyKey,
		Metadata: map[string]string{
			"plan_id":           selection.plan.ID,
			"plan_slug":         selection.plan.Slug,
			"plan_name":         selection.plan.Name,
			"cost_id":           selection.cost.ID,
			"provider_price_id": priceID,
			"user_reference":    userID,
		},
	})
	if err != nil {
		log.Warn("checkout-provider-session-creation-failed", zap.String("provider", providerName), zap.String("user-id", userID), zap.Error(err))
		if errors.Is(err, paymentprovider.ErrPaymentProviderPriceMismatch) {
			return nil, paymentprovider.ErrPaymentProviderPriceMismatch
		}
		return nil, ErrBillingManagerCheckoutProviderRequestFailed
	}
	if session == nil || strings.TrimSpace(session.ID) == "" ||
		(strings.TrimSpace(session.ClientSecret) == "" && strings.TrimSpace(session.URL) == "") {
		log.Warn("checkout-provider-session-invalid", zap.String("provider", providerName), zap.String("user-id", userID))
		return nil, ErrBillingManagerCheckoutSessionInvalid
	}

	return &ProcessBillingProviderCheckoutResponse{Session: session}, nil
}

func (s *Service) checkoutProviderReturnURL(providerName string, provider paymentprovider.CheckoutProvider) (string, bool) {
	legacyReturnURL := ""
	if s != nil {
		legacyReturnURL = strings.TrimSpace(s.CheckoutProviderConfigs[providerName].ReturnURL)
	}

	configuredProvider, ok := provider.(paymentprovider.CheckoutReturnURLProvider)
	if !ok || isNilCheckoutCapability(configuredProvider) {
		return legacyReturnURL, false
	}
	providerReturnURL := strings.TrimSpace(configuredProvider.GetCheckoutReturnURL())
	if providerReturnURL == "" {
		return legacyReturnURL, false
	}
	return providerReturnURL, legacyReturnURL != "" && legacyReturnURL != providerReturnURL
}

func isNilCheckoutCapability(capability interface{}) bool {
	if capability == nil {
		return true
	}
	value := reflect.ValueOf(capability)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (s *Service) resolvePublishedCheckoutPrice(ctx context.Context, providerName, priceID string) (*checkoutSelection, error) {
	var (
		found              *checkoutSelection
		expectedTotal      = -1
		expectedTotalPages = -1
	)
	for page := 1; page <= checkoutCatalogueMaxPages; page++ {
		response, err := s.PricerService.GetPricePlans(ctx, &pricer.GetPricePlansRequest{
			PerPage:          checkoutCataloguePageSize,
			Page:             page,
			WithStatus:       string(pricer.PricePlanStatusPublished),
			IsPublished:      true,
			IsNotDeleted:     true,
			IncludeCosts:     true,
			IncludeProviders: true,
		})
		if err != nil || response == nil {
			return nil, ErrBillingManagerCheckoutCatalogueUnavailable
		}
		if !checkoutCataloguePageMetadataValid(response, page) {
			return nil, ErrBillingManagerCheckoutCatalogueUnavailable
		}
		if page == 1 {
			expectedTotal = response.Total
			expectedTotalPages = response.TotalPages
		} else if response.Total != expectedTotal || response.TotalPages != expectedTotalPages {
			return nil, ErrBillingManagerCheckoutCatalogueUnavailable
		}

		for planIndex := range response.PricePlans {
			plan := response.PricePlans[planIndex]
			if plan.Status != pricer.PricePlanStatusPublished || strings.TrimSpace(plan.DeletedAt) != "" || !isPricePlanPubliclyVisible(plan.PublishedAt) {
				continue
			}
			for costIndex := range plan.Costs {
				cost := plan.Costs[costIndex]
				for refIndex := range cost.ProviderRefs {
					providerRef := cost.ProviderRefs[refIndex]
					if !strings.EqualFold(strings.TrimSpace(string(providerRef.Provider)), providerName) ||
						strings.TrimSpace(providerRef.ProviderPriceID) != priceID {
						continue
					}
					if found != nil {
						return nil, ErrBillingManagerCheckoutPriceAmbiguous
					}
					found = &checkoutSelection{plan: plan, cost: cost}
				}
			}
		}

		if expectedTotalPages <= page {
			break
		}
	}

	if found == nil {
		return nil, ErrBillingManagerCheckoutPriceUnavailable
	}
	mode, err := validateCheckoutSelection(found)
	if err != nil {
		return nil, err
	}
	found.mode = mode
	return found, nil
}

func checkoutCataloguePageMetadataValid(response *pricer.GetPricePlansResponse, page int) bool {
	if response == nil || page < 1 || response.Page != page || response.PerPage != checkoutCataloguePageSize ||
		response.Total < 0 || response.TotalPages < 0 || response.TotalPages > checkoutCatalogueMaxPages ||
		len(response.PricePlans) > checkoutCataloguePageSize {
		return false
	}

	expectedTotalPages := response.Total / checkoutCataloguePageSize
	if response.Total%checkoutCataloguePageSize != 0 {
		expectedTotalPages++
	}
	if response.TotalPages != expectedTotalPages {
		return false
	}
	if response.TotalPages == 0 {
		return page == 1 && len(response.PricePlans) == 0
	}
	if page > response.TotalPages {
		return false
	}

	remaining := response.Total - ((page - 1) * checkoutCataloguePageSize)
	expectedPageSize := checkoutCataloguePageSize
	if remaining < expectedPageSize {
		expectedPageSize = remaining
	}
	return expectedPageSize >= 0 && len(response.PricePlans) == expectedPageSize
}

func validateCheckoutSelection(selection *checkoutSelection) (string, error) {
	if selection == nil || strings.TrimSpace(selection.plan.ID) == "" || strings.TrimSpace(selection.plan.Slug) == "" ||
		strings.TrimSpace(selection.cost.ID) == "" || strings.TrimSpace(selection.cost.Currency) == "" ||
		selection.cost.Amount <= 0 || selection.cost.SetupFeeAmount != 0 || len(selection.plan.Discounts) != 0 ||
		selection.plan.PaymentTerms != nil || selection.cost.TrialPeriodDays < 0 {
		return "", ErrBillingManagerCheckoutTermsUnsupported
	}

	switch selection.cost.BillingCadence {
	case pricer.PriceBillingCadenceOneTime:
		if selection.cost.TrialPeriodDays != 0 {
			return "", ErrBillingManagerCheckoutTermsUnsupported
		}
		return paymentprovider.CheckoutModePayment, nil
	case pricer.PriceBillingCadenceWeekly, pricer.PriceBillingCadenceMonthly, pricer.PriceBillingCadenceYearly:
		return paymentprovider.CheckoutModeSubscription, nil
	default:
		return "", ErrBillingManagerCheckoutTermsUnsupported
	}
}

func normaliseCheckoutProviderName(providerName string) string {
	return strings.ToLower(strings.TrimSpace(providerName))
}

func checkoutIdempotencyKey(userID, providerName, priceID, clientKey string) (string, error) {
	clientKey = strings.TrimSpace(clientKey)
	if clientKey == "" {
		var randomKey [32]byte
		if _, err := rand.Read(randomKey[:]); err != nil {
			return "", err
		}
		clientKey = hex.EncodeToString(randomKey[:])
	}
	digest := sha256.Sum256([]byte(userID + "\x00" + providerName + "\x00" + priceID + "\x00" + clientKey))
	return "bms-checkout-" + hex.EncodeToString(digest[:]), nil
}

func checkoutReturnURLOrigin(rawURL string) (string, error) {
	parsed, _, err := parseCheckoutReturnURL(rawURL)
	if err != nil {
		return "", err
	}
	return canonicalHTTPOrigin(parsed), nil
}

func enrichCheckoutReturnURL(rawURL string, selection *checkoutSelection) (string, error) {
	if selection == nil {
		return "", ErrBillingManagerCheckoutConfigurationInvalid
	}
	parsed, placeholders, err := parseCheckoutReturnURL(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("plan_id", selection.plan.ID)
	query.Set("plan_slug", selection.plan.Slug)
	query.Set("cost_id", selection.cost.ID)
	parsed.RawQuery = query.Encode()

	result := parsed.String()
	for _, placeholder := range placeholders {
		result = strings.ReplaceAll(result, placeholder.token, placeholder.value)
	}
	return result, nil
}

type checkoutURLPlaceholder struct {
	token string
	value string
}

func parseCheckoutReturnURL(rawURL string) (*url.URL, []checkoutURLPlaceholder, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil, ErrBillingManagerCheckoutConfigurationInvalid
	}

	protectedURL := rawURL
	placeholderValues := checkoutBracePlaceholderPattern.FindAllString(rawURL, -1)
	placeholders := make([]checkoutURLPlaceholder, 0, len(placeholderValues))
	for index, value := range placeholderValues {
		token := fmt.Sprintf("__ghatd_checkout_placeholder_%d__", index)
		for strings.Contains(rawURL, token) {
			token = "_" + token
		}
		protectedURL = strings.Replace(protectedURL, value, token, 1)
		placeholders = append(placeholders, checkoutURLPlaceholder{token: token, value: value})
	}
	if strings.ContainsAny(protectedURL, "{}") {
		return nil, nil, ErrBillingManagerCheckoutConfigurationInvalid
	}

	parsed, err := url.Parse(protectedURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || !hasValidHTTPPort(parsed) {
		return nil, nil, ErrBillingManagerCheckoutConfigurationInvalid
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, nil, ErrBillingManagerCheckoutConfigurationInvalid
	}
	for _, placeholder := range placeholders {
		if strings.Contains(parsed.Scheme, placeholder.token) || strings.Contains(parsed.Host, placeholder.token) {
			return nil, nil, ErrBillingManagerCheckoutConfigurationInvalid
		}
	}

	return parsed, placeholders, nil
}

func checkoutRequestOriginAllowed(origin, secFetchSite, allowedOrigin string) bool {
	if strings.EqualFold(strings.TrimSpace(secFetchSite), "cross-site") {
		return false
	}
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || !hasValidHTTPPort(parsed) ||
		parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return canonicalHTTPOrigin(parsed) == allowedOrigin
}

func canonicalHTTPOrigin(parsed *url.URL) string {
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port != "" {
		portNumber, _ := strconv.Atoi(port)
		port = strconv.Itoa(portNumber)
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	return scheme + "://" + host
}

func hasValidHTTPPort(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	if parsed.Port() == "" {
		return true
	}
	port, err := strconv.Atoi(parsed.Port())
	return err == nil && port >= 1 && port <= 65535
}
