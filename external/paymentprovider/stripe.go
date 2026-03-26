package paymentprovider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// StripeProvider implements the Provider interface for Stripe
type StripeProvider struct {
	config *Config
	name   string
}

// StripeAllowedSyncEvents contains the normalised event types that should trigger
// a re-sync of subscription state from Stripe's API. These are Stripe events where
// the subscription state may have changed and we want authoritative data rather than
// trusting the webhook payload. Based on Theo's recommended event list.
var StripeAllowedSyncEvents = []string{
	EventTypeCheckoutCompleted,
	EventTypeSubscriptionCreated,
	EventTypeSubscriptionUpdated,
	EventTypeSubscriptionCancelled,
	EventTypeSubscriptionPaused,
	EventTypeSubscriptionResumed,
	EventTypeSubscriptionPendingUpdateApplied,
	EventTypeSubscriptionPendingUpdateExpired,
	EventTypeTrialWillEnd,
	EventTypeInvoicePaid,
	EventTypePaymentFailed,
	EventTypePaymentActionRequired,
	EventTypeInvoiceUpcoming,
	EventTypeInvoiceMarkedUncollectible,
	EventTypePaymentSucceeded,
	EventTypePaymentIntentSucceeded,
	EventTypePaymentIntentFailed,
	EventTypePaymentIntentCancelled,
}

// NewStripeProvider creates a new Stripe payment provider
func NewStripeProvider(config *Config) (*StripeProvider, error) {
	if config.WebhookSecret == "" {
		return nil, errors.New(ErrKeyPaymentProviderInvalidConfigWebhookSecret)
	}

	return &StripeProvider{
		config: config,
		name:   "stripe",
	}, nil
}

// GetProviderName returns the name of the provider, i.e "stripe"
func (s *StripeProvider) GetProviderName() string {
	return s.name
}

// VerifyWebhook verifies the Stripe webhook signature
func (s *StripeProvider) VerifyWebhook(ctx context.Context, req *http.Request) error {
	log := logger.AcquireFrom(ctx).With(zap.String("provider", s.name)).With(zap.String("method", "verify-webhook")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Debug("verifying-stripe-webhook")

	signature := req.Header.Get("Stripe-Signature")
	if signature == "" {
		log.Error("missing-signature-from-webhook")
		return errors.New(ErrKeyPaymentProviderMissingSignature)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		log.Error("failed-to-read-webhook-body", zap.Error(err))
		return errors.New(ErrKeyPaymentProviderInvalidPayload)
	}

	// Parse the signature header
	var timestamp string
	var v1Signature string

	parts := strings.Split(signature, ",")
	for _, part := range parts {
		keyValue := strings.SplitN(part, "=", 2)
		if len(keyValue) != 2 {
			continue
		}

		switch keyValue[0] {
		case "t":
			timestamp = keyValue[1]
		case "v1":
			v1Signature = keyValue[1]
		}
	}

	if timestamp == "" || v1Signature == "" {
		log.Error("missing-timestamp-or-signature-from-webhook")
		return errors.New(ErrKeyPaymentProviderInvalidWebhookSignature)
	}

	// Verify the timestamp is recent (within 5 minutes)
	timestampInt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		log.Error("invalid-timestamp-in-signature", zap.Error(err))
		return errors.New(ErrKeyPaymentProviderInvalidWebhookSignature)
	}

	var maxAge int64 = 300 // 5 minutes
	if time.Now().Unix()-timestampInt > maxAge {
		log.Error("timestamp-too-old", zap.Int64("timestamp", timestampInt), zap.Int64("allowed-age-in-seconds", maxAge))
		return errors.New(ErrKeyPaymentProviderWebhookTimestampTooOld)
	}

	signedPayload := timestamp + "." + string(body)
	mac := hmac.New(sha256.New, []byte(s.config.WebhookSecret))
	mac.Write([]byte(signedPayload))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSignature), []byte(v1Signature)) {
		log.Error("invalid-signature", zap.String("expected", expectedSignature), zap.String("received", v1Signature))
		return errors.New(ErrKeyPaymentProviderInvalidWebhookSignature)
	}

	log.Debug("stripe-webhook-verified-successfully")

	return nil
}

// ParsePayload extracts and normalises Stripe webhook data
func (s *StripeProvider) ParsePayload(ctx context.Context, req *http.Request) (*WebhookPayload, error) {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", s.name)).With(zap.String("method", "parse-payload")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Debug("parsing-stripe-webhook-payload")

	body, err := io.ReadAll(req.Body)
	if err != nil {
		log.Error("failed-to-parse-webhook-payload", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderInvalidPayload)
	}

	// Parse the JSON
	var event struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Created int64  `json:"created"`
		Data    struct {
			Object map[string]interface{} `json:"object"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		log.Error("failed-to-parse-webhook-payload", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderPayloadParsing)
	}

	obj := event.Data.Object

	subscriptionID := getStringField(obj, "id")
	customerID := getStringField(obj, "customer")
	status := getStringField(obj, "status")

	// Get customer email
	email, err := s.getCustomerDetailsByCustomerID(ctx, customerID)
	if err != nil {
		return nil, err
	}

	// Get plan information
	var productId string
	if items, ok := obj["items"].(map[string]interface{}); ok {
		if data, ok := items["data"].([]interface{}); ok && len(data) > 0 {
			if item, ok := data[0].(map[string]interface{}); ok {
				if plan, ok := item["plan"].(map[string]interface{}); ok {
					productId = getStringField(plan, "product")
				}
			}
		}
	}

	planName, err := s.getProductDetailsByProductID(ctx, productId)
	if err != nil {
		return nil, err
	}

	// Get amount and currency
	var amount int64
	var currency string
	var quantity int64
	if items, ok := obj["items"].(map[string]interface{}); ok {
		if data, ok := items["data"].([]interface{}); ok && len(data) > 0 {
			if item, ok := data[0].(map[string]interface{}); ok {
				if price, ok := item["price"].(map[string]interface{}); ok {
					amount = int64(getFloatField(price, "unit_amount"))
					currency = getStringField(price, "currency")
				}
				quantity = int64(getFloatField(item, "quantity"))
			}
		}
	}

	// Get billing dates
	var nextBillingDate string
	if currentPeriodEnd, ok := obj["current_period_end"].(float64); ok {
		nextBillingDate = time.Unix(int64(currentPeriodEnd), 0).Format(time.RFC3339)
	}

	log.Debug("parsed-stripe-webhook-payload", zap.String("raw-event-type", event.Type), zap.String("event-type", stripeEventToStandard(event.Type)), zap.String("email", email))

	return &WebhookPayload{
		EventType:          stripeEventToStandard(event.Type),
		EventID:            event.ID,
		EventTime:          time.Unix(event.Created, 0).Format(time.RFC3339),
		SubscriptionID:     subscriptionID,
		CustomerID:         customerID,
		CustomerEmail:      email,
		Status:             stripeStatusToStandard(status),
		PlanName:           planName,
		Amount:             amount * quantity,
		Currency:           strings.ToUpper(currency),
		NextBillingDate:    nextBillingDate,
		AvailableUntilDate: nextBillingDate,
		RawPayload:         string(body),
	}, nil
}

// GetSubscriptionInfo retrieves subscription information from Stripe's API
func (s *StripeProvider) GetSubscriptionInfo(ctx context.Context, subscriptionID string) (*SubscriptionInfo, error) {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", s.name)).With(zap.String("method", "get-subscription-info")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Info("handle-request-to-get-subscription-information")

	baseURL := s.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}

	apiURL := baseURL + "/v1/subscriptions/" + subscriptionID
	body, err := s.callStripeEndpoint(log, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return nil, err
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		log.Error("failed-to-parse-api-response", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	}

	// Extract subscription info
	info := &SubscriptionInfo{
		SubscriptionID: getStringField(obj, "id"),
		CustomerID:     getStringField(obj, "customer"),
		Status:         stripeStatusToStandard(getStringField(obj, "status")),
	}

	if currentPeriodEnd, ok := obj["current_period_end"].(float64); ok {
		info.NextBillingDate = time.Unix(int64(currentPeriodEnd), 0).Format(time.RFC3339)
	}

	if currentPeriodStart, ok := obj["current_period_start"].(float64); ok {
		info.CurrentPeriodStart = time.Unix(int64(currentPeriodStart), 0).Format(time.RFC3339)
	}

	if currency, ok := obj["currency"].(string); ok {
		info.Currency = strings.ToUpper(currency)
	}

	// Get plan information
	if planInfo, ok := obj["plan"].(map[string]interface{}); ok {
		info.PlanID = getStringField(planInfo, "id")

		if info.PlanID != "" {
			planName, err := s.getPlanDetailsByPlanID(ctx, info.PlanID)
			if err != nil {
				log.Warn("unable-to-get-plan-name", zap.Error(err))
			} else {
				info.PlanName = planName
			}

			info.BillingInterval = getStringField(planInfo, "interval")
		}
	}

	log.Info("retrieved-subscription-info")

	return info, nil
}

// getProductDetailsByProductID retrieves product details from Stripe's API
func (s *StripeProvider) getProductDetailsByProductID(ctx context.Context, productID string) (string, error) {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", s.name)).With(zap.String("method", "get-product-details-by-product-id")).With(zap.String("stripe-product-id", productID)).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Info("handle-request-to-get-product-details-by-product-id")

	baseURL := s.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}

	apiURL := baseURL + "/v1/products/" + productID
	body, err := s.callStripeEndpoint(log, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return "", err
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		log.Error("failed-to-parse-api-response", zap.Error(err))
		return "", errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	}

	log.Info("successfully-retrieved-product-details")

	return getStringField(obj, "name"), nil
}

// getPlanDetailsByPlanID retrieves plan details from Stripe's API
func (s *StripeProvider) getPlanDetailsByPlanID(ctx context.Context, planID string) (string, error) {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", s.name)).With(zap.String("method", "get-plan-details-by-plan-id")).With(zap.String("stripe-plan-id", planID)).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Info("handle-request-to-get-plan-details-by-plan-id")

	baseURL := s.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}

	apiURL := baseURL + "/v1/plans/" + planID
	body, err := s.callStripeEndpoint(log, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return "", err
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		log.Error("failed-to-parse-api-response", zap.Error(err))
		return "", errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	}

	log.Info("successfully-retrieved-plan-details")

	return getStringField(obj, "usage_type"), nil
}

// getCustomerDetailsByCustomerID retrieves customer details from Stripe's API
func (s *StripeProvider) getCustomerDetailsByCustomerID(ctx context.Context, customerID string) (string, error) {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", s.name)).With(zap.String("method", "get-customer-details-by-customer-id")).With(zap.String("stripe-customer-id", customerID)).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Info("handle-request-to-get-customer-details-by-customer-id")

	baseURL := s.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}

	apiURL := baseURL + "/v1/customers/" + customerID
	body, err := s.callStripeEndpoint(log, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return "", err
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		log.Error("failed-to-parse-api-response", zap.Error(err))
		return "", errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	}

	log.Info("successfully-retrieved-customer-details")

	var customerEmail = getStringField(obj, "email")
	if customerEmail == "" {
		return "", errors.New(ErrKeyPaymentProviderMissingPayloadCustomerEmail)
	}

	return customerEmail, nil
}

// callStripeEndpoint is a helper to call Stripe API endpoints
func (s *StripeProvider) callStripeEndpoint(log *zap.Logger, method, endpoint string, body io.Reader, validHttpStatusCodes []int) ([]byte, error) {
	log.Info("calling-stripe-endpoint", zap.String("method", method), zap.String("endpoint", endpoint))

	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		log.Error("failed-to-create-http-request", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderAPIRequestFailed)
	}

	req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("Content-Type", "application/vnd.api+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error("http-request-failed", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderAPIRequestFailed)
	}
	defer resp.Body.Close()

	if !slices.Contains(validHttpStatusCodes, resp.StatusCode) {
		log.Error("http-request-returned-invalid-status", zap.Int("status-code", resp.StatusCode), zap.Ints("valid-status-codes", validHttpStatusCodes))
		return nil, errors.New(ErrKeyPaymentProviderSubscriptionNotFound)
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("failed-to-read-http-response-body", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	}

	log.Info("successfully-called-stripe-endpoint")
	return responseBody, nil
}

// GetActiveSubscriptionByCustomerID fetches the latest subscription state directly
// from Stripe's API using the customer ID. This implements Theo's "re-fetch from API"
// pattern: instead of trusting webhook event data (which can be out of order, partial,
// or stale), we use the event as a trigger and pull the authoritative subscription
// state from Stripe.
//
// Calls: GET /v1/subscriptions?customer={id}&limit=1&status=all
// Returns nil, nil if the customer has no subscriptions.
func (s *StripeProvider) GetActiveSubscriptionByCustomerID(ctx context.Context, customerID string) (*SubscriptionInfo, error) {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", s.name)).With(zap.String("method", "get-active-subscription-by-customer-id")).With(zap.String("stripe-customer-id", customerID)).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Info("handle-request-to-sync-subscription-by-customer-id")

	baseURL := s.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}

	// List subscriptions for this customer, latest first, expand payment method
	apiURL := fmt.Sprintf("%s/v1/subscriptions?customer=%s&limit=1&status=all", baseURL, customerID)
	body, err := s.callStripeEndpoint(log, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return nil, err
	}

	var listResp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		log.Error("failed-to-parse-subscriptions-list-response", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	}

	// No subscriptions found for this customer
	if len(listResp.Data) == 0 {
		log.Info("no-subscriptions-found-for-customer")
		return nil, nil
	}

	obj := listResp.Data[0]

	info := &SubscriptionInfo{
		SubscriptionID: getStringField(obj, "id"),
		CustomerID:     getStringField(obj, "customer"),
		Status:         stripeStatusToStandard(getStringField(obj, "status")),
	}

	// Period dates
	if currentPeriodEnd, ok := obj["current_period_end"].(float64); ok {
		info.NextBillingDate = time.Unix(int64(currentPeriodEnd), 0).Format(time.RFC3339)
		info.CurrentPeriodEnd = info.NextBillingDate
	}

	if currentPeriodStart, ok := obj["current_period_start"].(float64); ok {
		info.CurrentPeriodStart = time.Unix(int64(currentPeriodStart), 0).Format(time.RFC3339)
	}

	if currency, ok := obj["currency"].(string); ok {
		info.Currency = strings.ToUpper(currency)
	}

	// CancelAtPeriodEnd — critical for knowing if user will lose access at period end
	if cancelAtPeriodEnd, ok := obj["cancel_at_period_end"].(bool); ok {
		info.CancelAtPeriodEnd = cancelAtPeriodEnd
	}

	// Extract price ID, plan name, amount, and billing interval from items
	if items, ok := obj["items"].(map[string]interface{}); ok {
		if data, ok := items["data"].([]interface{}); ok && len(data) > 0 {
			if item, ok := data[0].(map[string]interface{}); ok {
				if price, ok := item["price"].(map[string]interface{}); ok {
					info.PriceID = getStringField(price, "id")
					info.Amount = getFloatField(price, "unit_amount")
					info.BillingInterval = getStringField(price, "recurring.interval")

					// Try to get interval from nested recurring object
					if info.BillingInterval == "" {
						if recurring, ok := price["recurring"].(map[string]interface{}); ok {
							info.BillingInterval = getStringField(recurring, "interval")
						}
					}
				}

				// Get plan product name
				if plan, ok := item["plan"].(map[string]interface{}); ok {
					if productId := getStringField(plan, "product"); productId != "" {
						planName, err := s.getProductDetailsByProductID(ctx, productId)
						if err != nil {
							log.Warn("unable-to-get-product-name-during-sync", zap.Error(err))
						} else {
							info.PlanName = planName
						}
					}
					info.PlanID = getStringField(plan, "id")
				}
			}
		}
	}

	// Extract payment method if expanded
	if pm, ok := obj["default_payment_method"].(map[string]interface{}); ok {
		if card, ok := pm["card"].(map[string]interface{}); ok {
			info.PaymentMethod = &PaymentMethodInfo{
				Brand: getStringField(card, "brand"),
				Last4: getStringField(card, "last4"),
			}
		}
	}

	// CancelledAt
	if cancelledAt, ok := obj["canceled_at"].(float64); ok && cancelledAt > 0 {
		info.CancelledAt = time.Unix(int64(cancelledAt), 0).Format(time.RFC3339)
	}

	log.Info("successfully-synced-subscription-by-customer-id", zap.String("subscription-id", info.SubscriptionID), zap.String("status", info.Status))

	return info, nil
}

// Helper functions

func stripeEventToStandard(eventType string) string {
	switch eventType {
	case "customer.subscription.created":
		return EventTypeSubscriptionCreated
	case "customer.subscription.updated":
		return EventTypeSubscriptionUpdated
	case "customer.subscription.deleted":
		return EventTypeSubscriptionCancelled
	case "customer.subscription.paused":
		return EventTypeSubscriptionPaused
	case "customer.subscription.resumed":
		return EventTypeSubscriptionResumed
	case "customer.subscription.pending_update_applied":
		return EventTypeSubscriptionPendingUpdateApplied
	case "customer.subscription.pending_update_expired":
		return EventTypeSubscriptionPendingUpdateExpired
	case "customer.subscription.trial_will_end":
		return EventTypeTrialWillEnd
	case "checkout.session.completed":
		return EventTypeCheckoutCompleted
	case "invoice.paid":
		return EventTypeInvoicePaid
	case "invoice.payment_succeeded":
		return EventTypePaymentSucceeded
	case "invoice.payment_failed":
		return EventTypePaymentFailed
	case "invoice.payment_action_required":
		return EventTypePaymentActionRequired
	case "invoice.upcoming":
		return EventTypeInvoiceUpcoming
	case "invoice.marked_uncollectible":
		return EventTypeInvoiceMarkedUncollectible
	case "charge.refunded":
		return EventTypePaymentRefunded
	case "payment_intent.succeeded":
		return EventTypePaymentIntentSucceeded
	case "payment_intent.payment_failed":
		return EventTypePaymentIntentFailed
	case "payment_intent.canceled":
		return EventTypePaymentIntentCancelled
	default:
		return eventType
	}
}

func stripeStatusToStandard(status string) string {
	switch status {
	case "active":
		return SubscriptionStatusActive
	case "trialing":
		return SubscriptionStatusTrialing
	case "past_due":
		return SubscriptionStatusPastDue
	case "canceled":
		return SubscriptionStatusCancelled
	case "paused":
		return SubscriptionStatusPaused
	case "incomplete":
		return SubscriptionStatusIncomplete
	case "unpaid":
		return SubscriptionStatusUnpaid
	default:
		return status
	}
}

func getStringField(obj map[string]interface{}, key string) string {
	if val, ok := obj[key]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return ""
}

func getFloatField(obj map[string]interface{}, key string) float64 {
	if val, ok := obj[key]; ok {
		if floatVal, ok := val.(float64); ok {
			return floatVal
		}
	}
	return 0
}
