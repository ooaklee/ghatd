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

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// LemonSqueezyProvider represents a provider for Lemon Squeezy
type LemonSqueezyProvider struct {
	config *Config
	name   string
}

// NewLemonSqueezyProvider creates a new Lemon Squeezy payment provider
func NewLemonSqueezyProvider(config *Config) (*LemonSqueezyProvider, error) {
	if config.WebhookSecret == "" {
		return nil, errors.New(ErrKeyPaymentProviderMissingConfiguration)
	}

	return &LemonSqueezyProvider{
		config: config,
		name:   "lemonsqueezy",
	}, nil
}

// GetProviderName returns the name of the provider, i.e "lemonsqueezy"
func (l *LemonSqueezyProvider) GetProviderName() string {
	return l.name
}

// VerifyWebhook verifies the Lemon Squeezy webhook signature
func (l *LemonSqueezyProvider) VerifyWebhook(ctx context.Context, req *http.Request) error {
	log := logger.AcquireFrom(ctx).With(zap.String("provider", l.name)).With(zap.String("method", "verify-webhook")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Debug("verifying-lemonsqueezy-webhook")

	body, signature, err := l.getRequestBodyAndSignature(req, true, log)
	if err != nil {
		return err
	}

	mac := hmac.New(sha256.New, []byte(l.config.WebhookSecret))
	mac.Write(body)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	// Compare signatures
	if !hmac.Equal([]byte(expectedSignature), []byte(signature)) {
		log.Error("invalid-webhook-signature", zap.String("received", signature))
		return errors.New(ErrKeyPaymentProviderInvalidWebhookSignature)
	}

	log.Debug("lemonsqueezy-webhook-verified-successfully")

	return nil
}

// ParsePayload extracts and normalises Lemon Squeezy webhook data
func (l *LemonSqueezyProvider) ParsePayload(ctx context.Context, req *http.Request) (*WebhookPayload, error) {
	log := logger.AcquireFrom(ctx).With(zap.String("provider", l.name)).With(zap.String("method", "parse-payload")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Debug("parsing-lemonsqueezy-webhook-payload")

	body, _, err := l.getRequestBodyAndSignature(req, false, log)
	if err != nil {
		return nil, err
	}

	var webhook LemonSqueezyWebhookPayload

	if err := json.Unmarshal(body, &webhook); err != nil {
		log.Error("failed-to-parse-webhook-payload", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderPayloadParsing)
	}

	// Extract relevant fields
	attrs := webhook.Data.Attributes

	// Determine event type
	eventType := lemonSqueezyEventToStandard(webhook.Meta.EventName)

	// Determine status
	status := lemonSqueezyStatusToStandard(attrs.Status)

	// Build plan name from product and variant
	planName := attrs.ProductName
	if attrs.VariantName != "" {
		planName = fmt.Sprintf("%s - %s", attrs.ProductName, attrs.VariantName)
	}

	// Determine next billing date
	nextBillingDate := attrs.RenewsAt
	if attrs.EndsAt != "" {
		nextBillingDate = attrs.EndsAt
	}

	priceInfo, err := l.getPriceByOrderItemID(ctx, attrs.OrderItemID)
	if err != nil {
		log.Error("failed-to-get-price-info", zap.Int64("order-item-id", attrs.OrderItemID), zap.String("event-type", eventType), zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	}

	log.Debug("parsed-lemonsqueezy-webhook-payload", zap.String("event-type", eventType), zap.String("email", webhook.Data.Attributes.UserEmail))

	return &WebhookPayload{
		EventType:          eventType,
		EventID:            webhook.Data.ID,
		EventTime:          attrs.UpdatedAt,
		SubscriptionID:     webhook.Data.ID,
		CustomerID:         fmt.Sprintf("%d", attrs.CustomerID),
		CustomerEmail:      attrs.UserEmail,
		Status:             status,
		PlanName:           planName,
		Amount:             priceInfo.UnitPrice,
		Currency:           priceInfo.Currency,
		NextBillingDate:    nextBillingDate,
		AvailableUntilDate: nextBillingDate,
		CancelURL:          attrs.URLs.Cancel,
		UpdateURL:          attrs.URLs.Update,
		RawPayload:         string(body),
	}, nil
}

// GetSubscriptionInfo retrieves subscription information from Lemon Squeezy's API
func (l *LemonSqueezyProvider) GetSubscriptionInfo(ctx context.Context, subscriptionID string) (*SubscriptionInfo, error) {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", l.name)).With(zap.String("method", "get-subscription-info")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Info("handle-request-to-get-subscription-information")

	baseURL := l.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.lemonsqueezy.com"
	}

	apiURL := baseURL + "/v1/subscriptions/" + subscriptionID
	body, err := l.callLemonSqueezyEndpoint(log, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return nil, err
	}

	var apiResp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				CustomerID  int64  `json:"customer_id"`
				ProductID   int64  `json:"product_id"`
				VariantID   int64  `json:"variant_id"`
				ProductName string `json:"product_name"`
				VariantName string `json:"variant_name"`
				Status      string `json:"status"`
				RenewsAt    string `json:"renews_at"`
				EndsAt      string `json:"ends_at"`
				CreatedAt   string `json:"created_at"`
				UpdatedAt   string `json:"updated_at"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		log.Error("failed-to-parse-api-response", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	}

	attrs := apiResp.Data.Attributes
	planName := attrs.ProductName
	if attrs.VariantName != "" {
		planName = fmt.Sprintf("%s - %s", attrs.ProductName, attrs.VariantName)
	}

	log.Info("retrieved-subscription-info")

	return &SubscriptionInfo{
		SubscriptionID:  apiResp.Data.ID,
		CustomerID:      fmt.Sprintf("%d", attrs.CustomerID),
		Status:          lemonSqueezyStatusToStandard(attrs.Status),
		PlanName:        planName,
		PlanID:          fmt.Sprintf("%d", attrs.VariantID),
		NextBillingDate: attrs.RenewsAt,
	}, nil
}

// getPriceByPriceID retrieves price details by price ID from Lemon Squeezy's API
func (l *LemonSqueezyProvider) getPriceByPriceID(ctx context.Context, priceID string, quantity int64) (*PriceInfo, error) {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", l.name)).With(zap.String("method", "get-price-by-id")).With(zap.String("price_id", priceID)).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Info("handle-request-to-get-price-information-by-id")

	baseURL := l.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.lemonsqueezy.com"
	}

	apiURL := baseURL + "/v1/prices/" + priceID
	body, err := l.callLemonSqueezyEndpoint(log, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return nil, err
	}

	var apiResp LemonSqueezyPricePayload

	if err := json.Unmarshal(body, &apiResp); err != nil {
		log.Error("failed-to-parse-api-response-for-price-info", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	}

	unitPrice := calculateLemonSqueezyUnitPrice(&apiResp, quantity)
	log.Info("retrived-price-info-by-id", zap.Int64("unit_price", unitPrice), zap.String("scheme", apiResp.Data.Attributes.Scheme))

	return &PriceInfo{
		UnitPrice: unitPrice,
		Currency:  "USD", // Lemon Squeezy uses USD by default
	}, nil
}

// getPriceByOrderItemID retrieves price details by order item ID from Lemon Squeezy's API
func (l *LemonSqueezyProvider) getPriceByOrderItemID(ctx context.Context, orderItemID int64) (*PriceInfo, error) {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", l.name)).With(zap.String("method", "get-price-by-order-item-id")).With(zap.Int64("order-item-id", orderItemID)).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	log.Info("handle-request-to-get-price-information-by-order-item-id")

	baseURL := l.config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://api.lemonsqueezy.com"
	}

	apiURL := baseURL + "/v1/order-items/" + fmt.Sprintf("%d", orderItemID)
	body, err := l.callLemonSqueezyEndpoint(log, "GET", apiURL, nil, []int{http.StatusOK})
	if err != nil {
		return nil, err
	}

	var apiResp struct {
		Data struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				OrderID     int64  `json:"order_id"`
				ProductID   int64  `json:"product_id"`
				VariantID   int64  `json:"variant_id"`
				ProductName string `json:"product_name"`
				VariantName string `json:"variant_name"`
				Price       int64  `json:"price"`
				Quantity    int64  `json:"quantity"`
				CreatedAt   string `json:"created_at"`
				UpdatedAt   string `json:"updated_at"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		log.Error("failed-to-parse-api-response-for-order-item-info", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	}
	log.Info("retrived-order-item-info", zap.Int64("price", apiResp.Data.Attributes.Price), zap.Int64("quantity", apiResp.Data.Attributes.Quantity))
	return &PriceInfo{
		UnitPrice: apiResp.Data.Attributes.Price * apiResp.Data.Attributes.Quantity,
		Currency:  "USD", // Lemon Squeezy uses USD by default
	}, nil
}

// callLemonSqueezyEndpoint is a helper to call Lemon Squeezy API endpoints
func (l *LemonSqueezyProvider) callLemonSqueezyEndpoint(log *zap.Logger, method, endpoint string, body io.Reader, validHttpStatusCodes []int) ([]byte, error) {
	log.Info("calling-lemon-squeezy-endpoint", zap.String("method", method), zap.String("endpoint", endpoint))

	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		log.Error("failed-to-create-http-request", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderAPIRequestFailed)
	}

	req.Header.Set("Authorization", "Bearer "+l.config.APIKey)
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

	log.Info("successfully-called-lemon-squeezy-endpoint")
	return responseBody, nil
}

// getRequestBodyAndSignature reads the request body and retrieves the signature header if needed
func (l *LemonSqueezyProvider) getRequestBodyAndSignature(req *http.Request, getSignature bool, log *zap.Logger) ([]byte, string, error) {

	var signature string

	if getSignature {
		signature = req.Header.Get("X-Signature")
		if signature == "" {
			log.Error("missing-signature-header")
			return nil, "", errors.New(ErrKeyPaymentProviderMissingSignature)
		}
	}

	// Read the body
	body, err := io.ReadAll(req.Body)
	if err != nil {
		log.Error("failed-to-read-request-body", zap.Error(err))
		return nil, "", errors.New(ErrKeyPaymentProviderInvalidPayload)
	}

	return body, signature, nil
}

// Helper functions

// calculateLemonSqueezyUnitPrice calculates the unit price based on the pricing scheme and quantity
func calculateLemonSqueezyUnitPrice(pricePayloadResponse *LemonSqueezyPricePayload, quantity int64) int64 {

	var unitPrice int64
	attrs := pricePayloadResponse.Data.Attributes

	switch attrs.Scheme {
	case "standard", "package":
		// For standard and package pricing, use unit_price directly
		// If usage-based billing is enabled, unit_price will be 0 and we should use unit_price_decimal
		if attrs.UnitPrice > 0 {
			unitPrice = attrs.UnitPrice
		} else if attrs.UnitPriceDecimal != "" {
			// Parse decimal price if unit_price is not set (usage-based billing)
			// Note: UnitPriceDecimal is already in cents as a string
			// For now, we'll use UnitPrice which should be set for most cases
			unitPrice = 0
		}

		// For package pricing, multiply by package size and quantity
		if attrs.Scheme == "package" {
			unitPrice = unitPrice * attrs.PackageSize * quantity
		}

	case "graduated", "volume":
		// For graduated and volume pricing, calculate based on tiers
		if len(attrs.Tiers) > 0 {
			if attrs.Scheme == "graduated" {
				// Graduated pricing: charge different rates for different quantity ranges
				var totalPrice int64
				remainingQuantity := quantity

				for _, tier := range attrs.Tiers {
					if remainingQuantity <= 0 {
						break
					}

					var tierMax int64
					if lastUnit, ok := tier.LastUnit.(float64); ok {
						tierMax = int64(lastUnit)
					} else if tier.LastUnit == "inf" {
						tierMax = remainingQuantity // Use all remaining quantity for infinite tier
					}

					// Calculate how many units fall in this tier
					var unitsInTier int64
					if tierMax >= quantity {
						unitsInTier = remainingQuantity
					} else {
						unitsInTier = tierMax
					}

					// Add this tier's cost
					tierUnitPrice := tier.UnitPrice
					if tierUnitPrice > 0 {
						totalPrice += (unitsInTier * tierUnitPrice) + tier.FixedFee
					}

					remainingQuantity -= unitsInTier
				}

				unitPrice = totalPrice

			} else {
				// Volume pricing: all units charged at the rate of the tier they fall into
				for _, tier := range attrs.Tiers {
					var tierMax int64
					if lastUnit, ok := tier.LastUnit.(float64); ok {
						tierMax = int64(lastUnit)
					} else if tier.LastUnit == "inf" {
						tierMax = quantity // Infinite tier covers any quantity
					}

					if quantity <= tierMax {
						tierUnitPrice := tier.UnitPrice
						if tierUnitPrice > 0 {
							unitPrice = (quantity * tierUnitPrice) + tier.FixedFee
						}
						break
					}
				}
			}
		}

	default:
		// Unknown scheme, use unit_price if available
		unitPrice = attrs.UnitPrice
	}
	return unitPrice
}

// lemonSqueezyEventToStandard maps Lemon Squeezy event names to standard event types
func lemonSqueezyEventToStandard(eventName string) string {
	switch eventName {
	case "subscription_created":
		return EventTypeSubscriptionCreated
	case "subscription_updated":
		return EventTypeSubscriptionUpdated
	case "subscription_cancelled":
		return EventTypeSubscriptionCancelled
	case "subscription_resumed":
		return EventTypeSubscriptionResumed
	case "subscription_expired":
		return EventTypeSubscriptionCancelled
	case "subscription_paused":
		return EventTypeSubscriptionPaused
	case "subscription_unpaused":
		return EventTypeSubscriptionResumed
	case "subscription_payment_success":
		return EventTypePaymentSucceeded
	case "subscription_payment_failed":
		return EventTypePaymentFailed
	case "subscription_payment_recovered":
		return EventTypePaymentSucceeded
	case "subscription_payment_refunded":
		return EventTypePaymentRefunded
	default:
		return eventName
	}
}

// lemonSqueezyStatusToStandard maps Lemon Squeezy subscription statuses to standard statuses
func lemonSqueezyStatusToStandard(status string) string {
	switch status {
	case "active":
		return SubscriptionStatusActive
	case "on_trial":
		return SubscriptionStatusTrialing
	case "past_due":
		return SubscriptionStatusPastDue
	case "cancelled":
		return SubscriptionStatusCancelled
	case "expired":
		return SubscriptionStatusExpired
	case "paused":
		return SubscriptionStatusPaused
	case "unpaid":
		return SubscriptionStatusUnpaid
	default:
		return status
	}
}
