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

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// KofiWebhookPayload Parse the JSON payload
type KofiWebhookPayload struct {
	VerificationToken          string        `json:"verification_token"`
	MessageID                  string        `json:"message_id"`
	Timestamp                  string        `json:"timestamp"`
	Type                       string        `json:"type"` // "Donation" or "Subscription"
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

	// Determine event type
	eventType := kofiTypeToStandard(payload.Type, payload.IsSubscriptionPayment, payload.IsFirstSubscriptionPayment)

	// Parse amount (Ko-fi sends it as a string like "3.00")
	amountFloat, err := strconv.ParseFloat(payload.Amount, 64)
	if err != nil {
		amountFloat = 0
	}

	// convert to pennies
	amount := int64(amountFloat * 100)

	// Determine status
	status := SubscriptionStatusActive
	if payload.IsFirstSubscriptionPayment {
		status = SubscriptionStatusTrialing // First payment could be considered trial start
	}

	// Build plan name from tier
	planName := "Ko-fi Support"
	if payload.TierName != "" {
		planName = payload.TierName
	}

	log.Debug("parsed-kofi-webhook-payload", zap.String("event-type", eventType), zap.String("email", payload.Email), zap.Int64("amount", amount), zap.String("currency", payload.Currency))

	return &WebhookPayload{
		EventType:      eventType,
		EventID:        payload.MessageID,
		EventTime:      payload.Timestamp,
		SubscriptionID: payload.KofiTransactionID, // Ko-fi doesn't have traditional subscription IDs
		CustomerID:     computeKofiCustomerID(payload.Email),
		CustomerEmail:  payload.Email,
		Status:         status,
		PlanName:       planName,
		Amount:         amount,
		Currency:       payload.Currency,
		ReceiptURL:     payload.URL,
		RawPayload:     dataField,
	}, nil
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

// kofiTypeToStandard maps Ko-fi event types to standard event types
func kofiTypeToStandard(paymentType string, isSubscription bool, isFirst bool) string {
	if paymentType == "Subscription" || isSubscription {
		if isFirst {
			return EventTypeSubscriptionCreated
		}
		return EventTypePaymentSucceeded
	}
	return EventTypePaymentSucceeded // One-time donation
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
