package paymentprovider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// NewStripeProvider creates a new Stripe payment provider
func NewStripeProvider(config *Config) (*StripeProvider, error) {
	if config.WebhookSecret == "" {
		return nil, ErrPaymentProviderInvalidConfigWebhookSecret
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
	logger := logger.AcquirePackageFrom(ctx, "external/paymentprovider").With(zap.String("provider", s.name)).With(zap.String("operation", "verify-webhook"))

	logger.Debug("verifying-stripe-webhook")

	signature := req.Header.Get("Stripe-Signature")
	if signature == "" {
		logger.Error("missing-signature-from-webhook")
		return ErrPaymentProviderMissingSignature
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		logger.Error("failed-to-read-webhook-body", zap.Error(err))
		return ErrPaymentProviderInvalidPayload
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
		logger.Error("missing-timestamp-or-signature-from-webhook")
		return ErrPaymentProviderInvalidWebhookSignature
	}

	// Verify the timestamp is recent (within 5 minutes)
	timestampInt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		logger.Error("invalid-timestamp-in-signature", zap.Error(err))
		return ErrPaymentProviderInvalidWebhookSignature
	}

	var maxAge int64 = 300 // 5 minutes
	if time.Now().Unix()-timestampInt > maxAge {
		logger.Error("timestamp-too-old", zap.Int64("timestamp", timestampInt), zap.Int64("allowed-age-in-seconds", maxAge))
		return ErrPaymentProviderWebhookTimestampTooOld
	}

	signedPayload := timestamp + "." + string(body)
	mac := hmac.New(sha256.New, []byte(s.config.WebhookSecret))
	mac.Write([]byte(signedPayload))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSignature), []byte(v1Signature)) {
		logger.Error("invalid-signature", zap.Int("received-signature-length", len(v1Signature)))
		return ErrPaymentProviderInvalidWebhookSignature
	}

	logger.Debug("stripe-webhook-verified-successfully")

	return nil
}

// ParsePayload extracts and normalises Stripe webhook data
func (s *StripeProvider) ParsePayload(ctx context.Context, req *http.Request) (*WebhookPayload, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/paymentprovider").With(zap.String("provider", s.name)).With(zap.String("operation", "parse-payload"))

	logger.Debug("parsing-stripe-webhook-payload")

	body, err := io.ReadAll(req.Body)
	if err != nil {
		logger.Error("failed-to-parse-webhook-payload", zap.Error(err))
		return nil, ErrPaymentProviderInvalidPayload
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
		logger.Error("failed-to-parse-webhook-payload", zap.Error(err))
		return nil, ErrPaymentProviderPayloadParsing
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

	logger.Debug("parsed-stripe-webhook-payload",
		zap.String("raw-event-type", event.Type),
		zap.String("event-type", stripeEventToStandard(event.Type)),
		zap.Bool("email-present", emailPresentForLog(email)),
		zap.String("email-domain", emailDomainForLog(email)),
	)

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

	logger := logger.AcquirePackageFrom(ctx, "external/paymentprovider").With(zap.String("provider", s.name)).With(zap.String("operation", "get-subscription-info"))

	logger.Info("handle-request-to-get-subscription-information")

	baseURL := s.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}

	apiURL := baseURL + "/v1/subscriptions/" + subscriptionID
	body, err := s.callStripeEndpoint(logger, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return nil, err
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		logger.Error("failed-to-parse-api-response", zap.Error(err))
		return nil, ErrPaymentProviderAPIResponseInvalid
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
				logger.Warn("unable-to-get-plan-name", zap.Error(err))
			} else {
				info.PlanName = planName
			}

			info.BillingInterval = getStringField(planInfo, "interval")
		}
	}

	logger.Info("retrieved-subscription-info")

	return info, nil
}

// getProductDetailsByProductID retrieves product details from Stripe's API
func (s *StripeProvider) getProductDetailsByProductID(ctx context.Context, productID string) (string, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/paymentprovider").With(zap.String("provider", s.name)).With(zap.String("operation", "get-product-details-by-product-id")).With(zap.String("stripe-product-id", productID))

	logger.Info("handle-request-to-get-product-details-by-product-id")

	baseURL := s.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}

	apiURL := baseURL + "/v1/products/" + productID
	body, err := s.callStripeEndpoint(logger, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return "", err
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		logger.Error("failed-to-parse-api-response", zap.Error(err))
		return "", ErrPaymentProviderAPIResponseInvalid
	}

	logger.Info("successfully-retrieved-product-details")

	return getStringField(obj, "name"), nil
}

// getPlanDetailsByPlanID retrieves plan details from Stripe's API
func (s *StripeProvider) getPlanDetailsByPlanID(ctx context.Context, planID string) (string, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/paymentprovider").With(zap.String("provider", s.name)).With(zap.String("operation", "get-plan-details-by-plan-id")).With(zap.String("stripe-plan-id", planID))

	logger.Info("handle-request-to-get-plan-details-by-plan-id")

	baseURL := s.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}

	apiURL := baseURL + "/v1/plans/" + planID
	body, err := s.callStripeEndpoint(logger, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return "", err
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		logger.Error("failed-to-parse-api-response", zap.Error(err))
		return "", ErrPaymentProviderAPIResponseInvalid
	}

	logger.Info("successfully-retrieved-plan-details")

	return getStringField(obj, "usage_type"), nil
}

// getCustomerDetailsByCustomerID retrieves customer details from Stripe's API
func (s *StripeProvider) getCustomerDetailsByCustomerID(ctx context.Context, customerID string) (string, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/paymentprovider").With(zap.String("provider", s.name)).With(zap.String("operation", "get-customer-details-by-customer-id")).With(zap.String("stripe-customer-id", customerID))

	logger.Info("handle-request-to-get-customer-details-by-customer-id")

	baseURL := s.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.stripe.com"
	}

	apiURL := baseURL + "/v1/customers/" + customerID
	body, err := s.callStripeEndpoint(logger, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return "", err
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		logger.Error("failed-to-parse-api-response", zap.Error(err))
		return "", ErrPaymentProviderAPIResponseInvalid
	}

	logger.Info("successfully-retrieved-customer-details")

	var customerEmail = getStringField(obj, "email")
	if customerEmail == "" {
		return "", ErrPaymentProviderMissingPayloadCustomerEmail
	}

	return customerEmail, nil
}

// callStripeEndpoint is a helper to call Stripe API endpoints
func (s *StripeProvider) callStripeEndpoint(logger *zap.Logger, method, endpoint string, body io.Reader, validHttpStatusCodes []int) ([]byte, error) {
	logger.Info("calling-stripe-endpoint", zap.String("http-method", method), zap.String("endpoint-host", endpointHostForLog(endpoint)))

	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		logger.Error("failed-to-create-http-request", zap.Error(err))
		return nil, ErrPaymentProviderAPIRequestFailed
	}

	req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("Content-Type", "application/vnd.api+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Error("http-request-failed", zap.Error(err))
		return nil, ErrPaymentProviderAPIRequestFailed
	}
	defer resp.Body.Close()

	if !slices.Contains(validHttpStatusCodes, resp.StatusCode) {
		logger.Error("http-request-returned-invalid-status", zap.Int("status-code", resp.StatusCode), zap.Ints("valid-status-codes", validHttpStatusCodes))
		return nil, ErrPaymentProviderSubscriptionNotFound
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed-to-read-http-response-body", zap.Error(err))
		return nil, ErrPaymentProviderAPIResponseInvalid
	}

	logger.Info("successfully-called-lemon-squeezy-endpoint")
	return responseBody, nil
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
	case "invoice.payment_succeeded":
		return EventTypePaymentSucceeded
	case "invoice.payment_failed":
		return EventTypePaymentFailed
	case "charge.refunded":
		return EventTypePaymentRefunded
	case "invoice.payment_action_required":
		return EventTypePaymentActionRequired
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
