package paymentprovider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// KofiWebhookPayload Parse the JSON payload
type KofiWebhookPayload struct {
	VerificationToken string `json:"verification_token"`
	MessageID         string `json:"message_id"`
	Timestamp         string `json:"timestamp"`
	// Type can be "Donation", "Subscription", "Commission", "Shop Order"
	Type                       string        `json:"type"`
	IsPublic                   bool          `json:"is_public"`
	FromName                   string        `json:"from_name"`
	Message                    string        `json:"message"`
	Amount                     string        `json:"amount"`
	URL                        string        `json:"url"`
	Email                      string        `json:"email"`
	Currency                   string        `json:"currency"`
	IsSubscriptionPayment      bool          `json:"is_subscription_payment"`
	IsFirstSubscriptionPayment bool          `json:"is_first_subscription_payment"`
	KofiTransactionID          string        `json:"kofi_transaction_id"`
	ShopItems                  []interface{} `json:"shop_items"`
	TierName                   string        `json:"tier_name"`
}

// KofiProvider represents a provider for  Ko-fi
type KofiProvider struct {
	config *Config
	name   string
}

// NewKofiProvider creates a new Ko-fi payment provider
func NewKofiProvider(config *Config) (*KofiProvider, error) {
	if config.WebhookSecret == "" {
		return nil, errors.New(ErrKeyPaymentProviderMissingConfiguration)
	}

	return &KofiProvider{
		config: config,
		name:   "kofi",
	}, nil
}

// GetProviderName returns the name of the provider, i.e "kofi"
func (k *KofiProvider) GetProviderName() string {
	return k.name
}

// VerifyWebhook verifies the Ko-fi webhook signature
// The webhook secret should match the verification token configured in Ko-fi
func (k *KofiProvider) VerifyWebhook(ctx context.Context, req *http.Request) error {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", k.name)).With(zap.String("method", "verify-webhook")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Debug("verifying-kofi-webhook")

	dataField, err := k.getFormData(req, log)
	if err != nil {
		return err
	}

	// For Ko-fi, we verify by checking if the verification_token in the payload matches
	var payload struct {
		VerificationToken string `json:"verification_token"`
	}

	if err := json.Unmarshal([]byte(dataField), &payload); err != nil {
		log.Error("failed-to-parse-json-payload", zap.Error(err))
		return errors.New(ErrKeyPaymentProviderInvalidPayload)
	}

	if payload.VerificationToken != k.config.WebhookSecret {
		log.Error("invalid-verification-token", zap.String("received", payload.VerificationToken))
		return errors.New(ErrKeyPaymentProviderInvalidWebhookSignature)
	}

	log.Debug("kofi-webhook-verified-successfully")

	return nil
}

// ParsePayload extracts and normalizes Ko-fi webhook data
func (k *KofiProvider) ParsePayload(ctx context.Context, req *http.Request) (*WebhookPayload, error) {

	var (
		// NextBillingDate is when the next payment will be attempted (ISO 8601 format)
		nextBillingDate string = ""

		// AvailableUntilDate is when the subscription access expires (ISO 8601 format)
		availableUntilDate string = ""
	)

	log := logger.AcquireFrom(ctx).With(zap.String("provider", k.name)).With(zap.String("method", "parse-payload")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Debug("parsing-kofi-webhook-payload")

	dataField, err := k.getFormData(req, log)
	if err != nil {
		return nil, err
	}

	var payload KofiWebhookPayload

	if err := json.Unmarshal([]byte(dataField), &payload); err != nil {
		log.Warn("failed-to-parse-json-payload", zap.Error(err))
		return nil, errors.New(ErrKeyPaymentProviderPayloadParsing)
	}

	// Parse amount (Ko-fi sends it as a string like "3.00")
	amountFloat, err := strconv.ParseFloat(payload.Amount, 64)
	if err != nil {
		amountFloat = 0
	}

	// convert to pennies
	amount := int64(amountFloat * 100)

	// Determine payment type and event type based on Ko-fi's type field
	paymentType, eventType, isOneOff := kofiTypeToStandardPaymentType(payload.Type, payload.IsSubscriptionPayment, payload.IsFirstSubscriptionPayment)

	// Determine status (only relevant for subscriptions)
	status := ""
	subscriptionID := ""
	if payload.IsSubscriptionPayment {
		status = SubscriptionStatusActive
		subscriptionID = payload.KofiTransactionID
	}

	// Build plan name from tier
	planName := ""
	planName = getPayloadPlanName(payload, planName)

	// Estimate next billing date and available until date for subscriptions
	if payload.IsSubscriptionPayment {
		nextBillingDate = calculateNextBillingDate(ctx, payload.Timestamp)
		availableUntilDate = calculateAvailableUntilDate(ctx, payload.Timestamp)
	}

	log.Debug("parsed-kofi-webhook-payload",
		zap.String("event-type", eventType),
		zap.String("payment-type", paymentType),
		zap.String("email", payload.Email),
		zap.Int64("amount", amount),
		zap.String("currency", payload.Currency),
		zap.Bool("is-one-off", isOneOff))

	return &WebhookPayload{
		EventType:                  eventType,
		EventID:                    payload.MessageID,
		EventTime:                  payload.Timestamp,
		PaymentType:                paymentType,
		IsOneOff:                   isOneOff,
		SubscriptionID:             subscriptionID,
		TransactionID:              payload.KofiTransactionID,
		CustomerID:                 computeKofiCustomerID(payload.Email),
		CustomerEmail:              payload.Email,
		CustomerName:               payload.FromName,
		Status:                     status,
		PlanName:                   planName,
		Amount:                     amount,
		Currency:                   payload.Currency,
		IsFirstSubscriptionPayment: payload.IsFirstSubscriptionPayment,
		NextBillingDate:            nextBillingDate,
		AvailableUntilDate:         availableUntilDate,
		ReceiptURL:                 payload.URL,
		RawPayload:                 dataField,
	}, nil
}

// calculateNextBillingDate calculates the next billing date based on the passed timestamp
// Ko-fi does not provide this info, so we have to estimate it until their API improves
// Their timestamp is in RFC3339 format, e.g., "2023-10-05T14:48:00Z"
func calculateNextBillingDate(ctx context.Context, timestamp string) string {
	log := logger.AcquireFrom(ctx).With(zap.String("provider", "kofi")).With(zap.String("method", "calculate-next-billing-date")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// convert to time.Time
	providedTime, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		log.Warn("failed-to-parse-timestamp-defaulting-to-30-days-later", zap.Error(err))
		return time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	}

	// Add one month for monthly subscriptions
	return providedTime.Add(30 * 24 * time.Hour).Format(time.RFC3339)
}

// calculateAvailableUntilDate calculates the available until date based on the passed timestamp
// Ko-fi does not provide this info, so we have to estimate it until their API improves.
// We will give users an extra 48 hours grace period after the billing date to minimise disruption.
// Their timestamp is in RFC3339 format, e.g., "2023-10-05T14:48:00Z"
func calculateAvailableUntilDate(ctx context.Context, timestamp string) string {
	log := logger.AcquireFrom(ctx).With(zap.String("provider", "kofi")).With(zap.String("method", "calculate-available-until-date")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	gracePeriod := 48 * time.Hour

	// convert to time.Time
	providedTime, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		log.Warn("failed-to-parse-timestamp-defaulting-to-30-days-later", zap.Error(err))
		return time.Now().Add(30 * 24 * time.Hour).Add(gracePeriod).Format(time.RFC3339)
	}

	// Add one month for monthly subscriptions
	return providedTime.Add(30 * 24 * time.Hour).Add(gracePeriod).Format(time.RFC3339)
}

// getPayloadPlanName determines the plan name based on the Ko-fi payload
func getPayloadPlanName(payload KofiWebhookPayload, planName string) string {
	switch payload.Type {
	case "Donation":
		planName = "Ko-fi Supporter"
		if payload.IsSubscriptionPayment {
			planName += " (Monthly)"
		} else {
			planName += " (One-Time)"
		}
	case "Subscription":
		planName = "Ko-fi Subscription"
		if payload.TierName != "" {
			planName = payload.TierName
		}
	case "Commission":
		planName = "Ko-fi Commission"
	case "Shop Order":
		planName = "Ko-fi Shop Order"
	default:
		planName = "Ko-fi " + strings.Title(payload.Type)
	}
	return planName
}

// GetSubscriptionInfo retrieves subscription information
// Note: Ko-fi doesn't have a traditional subscription API, so this is limited.
// To get subscription info, we would need to track it in our own database.
func (k *KofiProvider) GetSubscriptionInfo(ctx context.Context, subscriptionID string) (*SubscriptionInfo, error) {

	log := logger.AcquireFrom(ctx).With(zap.String("provider", k.name)).With(zap.String("method", "get-subscription-info")).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	log.Warn("kofi-get-subscription-info-not-supported-there-is-no-subscription-api")

	return nil, errors.New(ErrKeyPaymentProviderKofiNoSubscriptionAPI)
}

// getFormData extracts the 'data' field from the form-encoded request
func (k *KofiProvider) getFormData(req *http.Request, log *zap.Logger) (string, error) {

	if err := req.ParseForm(); err != nil {
		log.Warn("failed-to-parse-form", zap.Error(err))
		return "", errors.New(ErrKeyPaymentProviderInvalidPayload)
	}

	// Ko-fi sends data as form-encoded with a 'data' field containing JSON
	dataField := req.FormValue("data")
	if dataField == "" {
		log.Warn("missing-data-field-in-form")
		return "", errors.New(ErrKeyPaymentProviderInvalidPayload)
	}

	return dataField, nil
}

// Helper functions

// kofiTypeToStandardPaymentType maps Ko-fi event types to standard payment types and event types
// Returns: (paymentType, eventType, isOneOff)
func kofiTypeToStandardPaymentType(kofiType string, isSubscription bool, isFirst bool) (string, string, bool) {
	switch kofiType {
	case "Subscription":
		// Monthly subscription
		if isFirst {
			return PaymentTypeSubscription, EventTypeSubscriptionCreated, true
		}
		return PaymentTypeSubscription, EventTypePaymentSucceeded, false

	case "Donation":
		// Check if it's actually a recurring monthly donation (subscription)
		if isSubscription {
			if isFirst {
				return PaymentTypeSubscription, EventTypeSubscriptionCreatedDonation, false
			}
			return PaymentTypeSubscription, EventTypePaymentSucceeded, false
		}
		// One-time donation
		return PaymentTypeDonation, EventTypePaymentSucceeded, true

	case "Commission":
		return PaymentTypeCommission, EventTypePaymentSucceeded, true

	case "Shop Order":
		return PaymentTypeShopOrder, EventTypePaymentSucceeded, true

	default:
		// Default to donation for unknown types
		return PaymentTypeDonation, EventTypePaymentSucceeded, true
	}
}

// computeKofiCustomerID creates a consistent customer ID from email
// Since Ko-fi doesn't provide customer IDs, we create one from the email
func computeKofiCustomerID(email string) string {
	if email == "" {
		return ""
	}

	// Create a hash of the email for consistency
	h := hmac.New(sha256.New, []byte("kofi-customer-id"))
	h.Write([]byte(email))
	return hex.EncodeToString(h.Sum(nil))[:16]
}
