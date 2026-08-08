package paymentprovider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/common"
)

const (
	stripeProviderName            = "stripe"
	stripeDefaultAPIBaseURL       = "https://api.stripe.com"
	stripeSignatureHeader         = "Stripe-Signature"
	stripeDefaultBodySize   int64 = 2 << 20
)

var stripeDefaultSignatureTolerance = 5 * time.Minute

// StripeProvider implements webhook and subscription lookup plus optional
// browser-session capabilities without expanding the base Provider interface.
type StripeProvider struct {
	config             *Config
	name               string
	httpClient         *http.Client
	apiBaseURL         string
	apiVersion         string
	signatureTolerance time.Duration
	maxWebhookBodySize int64
}

// NewStripeProvider creates a Stripe payment provider.
func NewStripeProvider(config *Config) (*StripeProvider, error) {
	if config == nil || strings.TrimSpace(config.WebhookSecret) == "" {
		return nil, ErrPaymentProviderInvalidConfigWebhookSecret
	}

	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.APIBaseURL), "/")
	if baseURL == "" {
		baseURL = stripeDefaultAPIBaseURL
	}
	apiVersion := strings.TrimSpace(config.APIVersion)
	if apiVersion == "" {
		apiVersion = StripeDefaultAPIVersion
	}
	if !isValidStripeAPIVersion(apiVersion) {
		return nil, ErrPaymentProviderInvalidConfiguration
	}
	tolerance := config.SignatureTolerance
	if tolerance <= 0 {
		tolerance = stripeDefaultSignatureTolerance
	}
	maxBodySize := config.MaxWebhookBodySize
	if maxBodySize <= 0 {
		maxBodySize = stripeDefaultBodySize
	}

	return &StripeProvider{
		config:             config,
		name:               stripeProviderName,
		httpClient:         client,
		apiBaseURL:         baseURL,
		apiVersion:         apiVersion,
		signatureTolerance: tolerance,
		maxWebhookBodySize: maxBodySize,
	}, nil
}

func (s *StripeProvider) GetProviderName() string { return s.name }

// GetCheckoutReturnURL returns the provider-owned trusted checkout return
// destination. Billing Manager enriches this URL with catalogue correlation
// fields before passing it back to CreateCheckoutSession.
func (s *StripeProvider) GetCheckoutReturnURL() string {
	if s == nil || s.config == nil {
		return ""
	}
	return strings.TrimSpace(s.config.ReturnURL)
}

// ValidateCheckoutConfig validates Stripe checkout only when ReturnURL opts
// this provider into checkout. An empty ReturnURL remains valid for webhook-
// only and subscription-sync use cases.
func (s *StripeProvider) ValidateCheckoutConfig() error {
	if s == nil || s.config == nil {
		return ErrPaymentProviderInvalidConfiguration
	}
	returnURL := s.GetCheckoutReturnURL()
	if returnURL == "" {
		return nil
	}
	if strings.TrimSpace(s.config.APIKey) == "" || strings.TrimSpace(s.config.PublishableKey) == "" ||
		!isValidAbsoluteHTTPURL(returnURL, false) {
		return ErrPaymentProviderInvalidConfiguration
	}
	return nil
}

// VerifyWebhook validates the signed timestamp and preserves the request body.
func (s *StripeProvider) VerifyWebhook(_ context.Context, req *http.Request) error {
	body, err := readAndRestoreWebhookBody(req, s.maxWebhookBodySize)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPaymentProviderInvalidPayload, err)
	}

	timestamp, signatures, err := parseStripeSignatureHeader(req.Header.Get(stripeSignatureHeader))
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	toleranceSeconds := int64(s.signatureTolerance / time.Second)
	if timestamp < now-toleranceSeconds || timestamp > now+toleranceSeconds {
		return ErrPaymentProviderWebhookTimestampTooOld
	}

	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(s.config.WebhookSecret)))
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "." + string(body)))
	expected := mac.Sum(nil)
	for _, signature := range signatures {
		provided, decodeErr := hex.DecodeString(signature)
		if decodeErr == nil && hmac.Equal(expected, provided) {
			return nil
		}
	}
	return ErrPaymentProviderInvalidWebhookSignature
}

// ParsePayload normalizes Checkout Session, PaymentIntent, refund, invoice,
// and subscription events without requiring extra API calls during webhook handling.
func (s *StripeProvider) ParsePayload(_ context.Context, req *http.Request) (*WebhookPayload, error) {
	body, err := readAndRestoreWebhookBody(req, s.maxWebhookBodySize)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderInvalidPayload, err)
	}

	var event struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Created int64  `json:"created"`
		Data    struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderPayloadParsing, err)
	}
	if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.Type) == "" || len(event.Data.Object) == 0 {
		return nil, ErrPaymentProviderMissingRequiredField
	}

	object := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(event.Data.Object))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderPayloadParsing, err)
	}

	payload := stripeBasePayload(event.ID, event.Type, event.Created, body, object)
	switch event.Type {
	case "checkout.session.completed":
		switch stripeString(object, "payment_status") {
		case "paid":
			markStripePaymentSucceeded(payload)
		case PaymentStatusNoPaymentRequired:
			if stripeString(object, "mode") == CheckoutModeSubscription {
				status := SubscriptionStatusActive
				trialDays, _ := strconv.Atoi(stripeString(stripeObject(object, "metadata"), "trial_period_days"))
				if trialDays > 0 {
					status = SubscriptionStatusTrialing
				}
				markStripeRecurring(payload, EventTypeSubscriptionCreated, status)
				payload.PaymentStatus = PaymentStatusNoPaymentRequired
			} else {
				markStripePaymentActionRequired(payload)
			}
		default:
			markStripePaymentActionRequired(payload)
		}
	case "checkout.session.async_payment_succeeded", "payment_intent.succeeded", "invoice.payment_succeeded", "invoice.paid":
		markStripePaymentSucceeded(payload)
	case "checkout.session.async_payment_failed", "payment_intent.payment_failed", "invoice.payment_failed":
		payload.EventType = EventTypePaymentFailed
		payload.PaymentStatus = PaymentStatusFailed
		payload.Status = SubscriptionStatusIncomplete
	case "payment_intent.requires_action", "invoice.payment_action_required":
		markStripePaymentActionRequired(payload)
	case "charge.refunded":
		markStripeChargeRefund(payload, object)
	case "refund.created", "refund.updated", "refund.failed":
		markStripeRefundLifecycle(payload, object)
	case "customer.subscription.created":
		markStripeRecurring(payload, EventTypeSubscriptionCreated, stripeStatusToStandard(stripeString(object, "status")))
	case "customer.subscription.updated":
		markStripeRecurring(payload, EventTypeSubscriptionUpdated, stripeStatusToStandard(stripeString(object, "status")))
	case "customer.subscription.deleted":
		markStripeRecurring(payload, EventTypeSubscriptionCancelled, SubscriptionStatusCancelled)
	case "customer.subscription.paused":
		markStripeRecurring(payload, EventTypeSubscriptionPaused, SubscriptionStatusPaused)
	case "customer.subscription.resumed":
		markStripeRecurring(payload, EventTypeSubscriptionResumed, SubscriptionStatusActive)
	default:
		return nil, fmt.Errorf("%w: %s", ErrPaymentProviderInvalidEventType, event.Type)
	}

	return payload, nil
}

func markStripePaymentSucceeded(payload *WebhookPayload) {
	payload.EventType = EventTypePaymentSucceeded
	payload.PaymentStatus = PaymentStatusSucceeded
	payload.Status = SubscriptionStatusActive
}

func markStripePaymentActionRequired(payload *WebhookPayload) {
	payload.EventType = EventTypePaymentActionRequired
	payload.PaymentStatus = PaymentStatusActionRequired
	payload.Status = SubscriptionStatusIncomplete
}

func markStripeChargeRefund(payload *WebhookPayload, object map[string]any) {
	amount := stripeInteger(object, "amount")
	amountRefunded := stripeInteger(object, "amount_refunded")
	if stripeBoolean(object, "refunded") || (amount > 0 && amountRefunded >= amount) {
		payload.EventType = EventTypePaymentRefunded
		payload.PaymentStatus = PaymentStatusRefunded
		payload.Status = SubscriptionStatusCancelled
		return
	}
	payload.EventType = EventTypePaymentPartiallyRefunded
	payload.PaymentStatus = PaymentStatusPartiallyRefunded
	payload.Status = ""
}

func markStripeRefundLifecycle(payload *WebhookPayload, object map[string]any) {
	if status := strings.ToLower(stripeString(object, "status")); status == "failed" || status == "canceled" {
		payload.EventType = EventTypePaymentRefundFailed
		payload.PaymentStatus = PaymentStatusRefundFailed
	} else {
		// Refund objects describe the individual refund, not whether the original
		// charge is now fully refunded. charge.refunded is authoritative for access.
		payload.EventType = EventTypePaymentPartiallyRefunded
		payload.PaymentStatus = PaymentStatusPartiallyRefunded
	}
	payload.Status = ""
}

func markStripeRecurring(payload *WebhookPayload, eventType, status string) {
	payload.EventType = eventType
	payload.PaymentType = PaymentTypeSubscription
	payload.BillingKind = BillingKindRecurring
	payload.IsOneOff = false
	payload.Status = status
}

func stripeBasePayload(eventID, eventType string, created int64, raw []byte, object map[string]any) *WebhookPayload {
	metadata := stripeObject(object, "metadata")
	mode := stripeString(object, "mode")
	subscriptionID := stripeStringOrID(object, "subscription")
	subscriptionDetails := stripeObject(stripeObject(object, "parent"), "subscription_details")
	if subscriptionID == "" {
		subscriptionID = stripeStringOrID(subscriptionDetails, "subscription")
	}
	if len(metadata) == 0 {
		metadata = stripeObject(subscriptionDetails, "metadata")
	}
	objectID := stripeString(object, "id")
	invoicePaymentIntentID, invoiceChargeID := stripeInvoicePaymentReferences(object)
	paymentIntentID := firstNonEmpty(stripeStringOrID(object, "payment_intent"), invoicePaymentIntentID)
	transactionID := firstNonEmpty(paymentIntentID, stripeStringOrID(object, "charge"), invoiceChargeID, objectID)
	if strings.HasPrefix(eventType, "customer.subscription.") {
		transactionID = firstNonEmpty(paymentIntentID, stripeStringOrID(object, "latest_invoice"))
	}

	billingKind := billingKindFromStripeObject(mode, metadata, subscriptionID, eventType)
	paymentType := PaymentTypePurchase
	isOneOff := billingKind == BillingKindOneTime
	if billingKind == BillingKindRecurring {
		paymentType = PaymentTypeSubscription
	}
	if strings.HasPrefix(eventType, "customer.subscription.") && subscriptionID == "" {
		subscriptionID = objectID
	}

	priceID := firstNonEmpty(
		stripeString(metadata, "provider_price_id"),
		stripeString(metadata, "price_id"),
		stripeNestedPriceID(object),
	)
	planName := firstNonEmpty(
		stripeString(metadata, "plan_name"),
		stripeString(metadata, "plan"),
		stripeString(object, "description"),
	)

	amount := stripeInteger(object, "amount_total")
	if amount == 0 {
		amount = stripeInteger(object, "amount_received")
	}
	if amount == 0 {
		amount = stripeInteger(object, "amount_paid")
	}
	if amount == 0 {
		amount = stripeInteger(object, "amount_refunded")
	}
	if amount == 0 {
		amount = stripeInteger(object, "amount")
	}

	_, itemPeriodEnd := stripeSubscriptionItemPeriod(object)
	nextBillingDate := stripeUnixDate(firstNonZero(stripeInteger(object, "current_period_end"), itemPeriodEnd))
	if nextBillingDate == "" {
		nextBillingDate = stripeUnixDate(stripeInteger(object, "period_end"))
	}

	return &WebhookPayload{
		EventType:      stripeEventToStandard(eventType),
		EventID:        eventID,
		EventTime:      stripeUnixDate(created),
		PaymentType:    paymentType,
		BillingKind:    billingKind,
		IsOneOff:       isOneOff,
		SubscriptionID: subscriptionID,
		TransactionID:  transactionID,
		UserReference: firstNonEmpty(
			stripeString(object, "client_reference_id"),
			stripeString(metadata, "user_reference"),
			stripeString(metadata, "user_id"),
		),
		CustomerID: firstNonEmpty(
			stripeStringOrID(object, "customer"),
			stripeString(metadata, "customer_id"),
		),
		CustomerEmail: firstNonEmpty(
			stripeString(object, "customer_email"),
			stripeString(stripeObject(object, "customer_details"), "email"),
			stripeString(stripeObject(object, "billing_details"), "email"),
			stripeString(object, "receipt_email"),
			stripeString(metadata, "customer_email"),
		),
		CustomerName: firstNonEmpty(
			stripeString(stripeObject(object, "customer_details"), "name"),
			stripeString(stripeObject(object, "billing_details"), "name"),
		),
		Status:          stripeStatusToStandard(stripeString(object, "status")),
		PaymentStatus:   stripePaymentStatusToStandard(stripeString(object, "payment_status")),
		PlanName:        planName,
		PlanID:          stripeString(metadata, "plan_id"),
		PlanSlug:        stripeString(metadata, "plan_slug"),
		CostID:          stripeString(metadata, "cost_id"),
		ProviderPriceID: priceID,
		Amount:          amount,
		Currency:        strings.ToUpper(stripeString(object, "currency")),
		NextBillingDate: nextBillingDate,
		AvailableUntilDate: firstNonEmpty(
			stripeUnixDate(stripeInteger(object, "cancel_at")),
			nextBillingDate,
		),
		ReceiptURL: firstNonEmpty(
			stripeString(object, "receipt_url"),
			stripeString(object, "hosted_invoice_url"),
		),
		RawPayload: string(raw),
	}
}

func billingKindFromStripeObject(mode string, metadata map[string]any, subscriptionID, eventType string) BillingKind {
	kind := strings.ToLower(firstNonEmpty(stripeString(metadata, "billing_kind"), stripeString(metadata, "billing_cadence")))
	switch kind {
	case string(BillingKindRecurring), CheckoutModeSubscription, "monthly", "yearly", "annual":
		return BillingKindRecurring
	case string(BillingKindOneTime), CheckoutModePayment, "one-time", "one_off", "lifetime":
		return BillingKindOneTime
	}
	if mode == CheckoutModeSubscription || subscriptionID != "" || strings.HasPrefix(eventType, "customer.subscription.") {
		return BillingKindRecurring
	}
	return BillingKindOneTime
}

// CreateCheckoutSession creates a provider checkout session. Authentication
// and catalogue validation remain the responsibility of the calling service.
func (s *StripeProvider) CreateCheckoutSession(ctx context.Context, input *CheckoutSessionRequest) (*CheckoutSession, error) {
	if input == nil || strings.TrimSpace(input.PriceID) == "" || firstNonEmpty(input.UserReference, input.UserID) == "" || strings.TrimSpace(input.CustomerEmail) == "" {
		return nil, ErrPaymentProviderMissingRequiredField
	}
	if input.Mode != CheckoutModePayment && input.Mode != CheckoutModeSubscription {
		return nil, ErrPaymentProviderInvalidConfiguration
	}
	if input.TrialPeriodDays < 0 || input.TrialPeriodDays > 730 || (input.Mode != CheckoutModeSubscription && input.TrialPeriodDays > 0) {
		return nil, ErrPaymentProviderInvalidConfiguration
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if len(idempotencyKey) > 255 {
		return nil, ErrPaymentProviderInvalidConfiguration
	}
	if strings.TrimSpace(s.config.APIKey) == "" || strings.TrimSpace(s.config.PublishableKey) == "" {
		return nil, ErrPaymentProviderInvalidConfiguration
	}
	if err := s.validateCheckoutPrice(ctx, input); err != nil {
		return nil, err
	}
	returnURL := firstNonEmpty(input.ReturnURL, s.config.ReturnURL)
	if returnURL == "" {
		return nil, ErrPaymentProviderInvalidConfiguration
	}

	userReference := firstNonEmpty(input.UserReference, input.UserID)
	form := url.Values{}
	form.Set("mode", input.Mode)
	form.Set("ui_mode", "embedded_page")
	form.Set("redirect_on_completion", "if_required")
	form.Set("return_url", returnURL)
	form.Set("line_items[0][price]", input.PriceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("customer_email", input.CustomerEmail)
	form.Set("client_reference_id", userReference)
	metadata := map[string]string{
		"plan_id": input.PlanID, "plan_slug": input.PlanSlug, "plan_name": input.PlanName,
		"cost_id": input.CostID, "price_id": input.PriceID, "provider_price_id": input.PriceID,
		"user_id": userReference, "user_reference": userReference,
	}
	if input.Mode == CheckoutModePayment {
		metadata["billing_kind"] = string(BillingKindOneTime)
		form.Set("invoice_creation[enabled]", "true")
	} else {
		metadata["billing_kind"] = string(BillingKindRecurring)
		if input.TrialPeriodDays > 0 {
			metadata["trial_period_days"] = strconv.Itoa(input.TrialPeriodDays)
			form.Set("subscription_data[trial_period_days]", strconv.Itoa(input.TrialPeriodDays))
		}
	}
	for key, value := range input.Metadata {
		metadata[key] = value
	}
	for key, value := range metadata {
		if strings.TrimSpace(value) == "" {
			continue
		}
		form.Set("metadata["+key+"]", value)
		if input.Mode == CheckoutModePayment {
			form.Set("payment_intent_data[metadata]["+key+"]", value)
			form.Set("invoice_creation[invoice_data][metadata]["+key+"]", value)
		} else {
			form.Set("subscription_data[metadata]["+key+"]", value)
		}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBaseURL+"/v1/checkout/sessions", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderAPIRequestFailed, err)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(s.config.APIKey))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Stripe-Version", s.apiVersion)
	if idempotencyKey != "" {
		request.Header.Set(common.IdempotencyKeyHttpHeader, idempotencyKey)
	}

	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderAPIRequestFailed, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: status %d", ErrPaymentProviderAPIRequestFailed, response.StatusCode)
	}

	var session CheckoutSession
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&session); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderAPIResponseInvalid, err)
	}
	if strings.TrimSpace(session.ID) == "" || strings.TrimSpace(session.ClientSecret) == "" {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	session.PublishableKey = strings.TrimSpace(s.config.PublishableKey)
	return &session, nil
}

// CreateCustomerPortalSession creates a hosted customer billing-management
// session. Customer ownership and access policy remain with the caller.
func (s *StripeProvider) CreateCustomerPortalSession(ctx context.Context, input *CustomerPortalSessionRequest) (*CustomerPortalSession, error) {
	if s == nil || s.config == nil || input == nil || strings.TrimSpace(input.CustomerID) == "" || !isValidAbsoluteHTTPURL(input.ReturnURL, false) {
		return nil, ErrPaymentProviderInvalidConfiguration
	}
	if strings.TrimSpace(s.config.APIKey) == "" {
		return nil, ErrPaymentProviderInvalidConfiguration
	}

	form := url.Values{}
	form.Set("customer", strings.TrimSpace(input.CustomerID))
	form.Set("return_url", strings.TrimSpace(input.ReturnURL))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBaseURL+"/v1/billing_portal/sessions", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderAPIRequestFailed, err)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(s.config.APIKey))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Stripe-Version", s.apiVersion)

	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderAPIRequestFailed, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: status %d", ErrPaymentProviderAPIRequestFailed, response.StatusCode)
	}

	var session CustomerPortalSession
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&session); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderAPIResponseInvalid, err)
	}
	if strings.TrimSpace(session.ID) == "" || !isValidAbsoluteHTTPURL(session.URL, true) {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	session.ID = strings.TrimSpace(session.ID)
	session.URL = strings.TrimSpace(session.URL)
	return &session, nil
}

func isValidAbsoluteHTTPURL(value string, requireHTTPS bool) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" {
		return false
	}
	if requireHTTPS {
		return parsed.Scheme == "https"
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func (s *StripeProvider) validateCheckoutPrice(ctx context.Context, input *CheckoutSessionRequest) error {
	hasExpectation := input.ExpectedAmount != 0 || strings.TrimSpace(input.ExpectedCurrency) != "" || strings.TrimSpace(input.ExpectedBillingCadence) != ""
	if !hasExpectation {
		return nil
	}
	if input.ExpectedAmount <= 0 || strings.TrimSpace(input.ExpectedCurrency) == "" || strings.TrimSpace(input.ExpectedBillingCadence) == "" {
		return ErrPaymentProviderInvalidConfiguration
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBaseURL+"/v1/prices/"+url.PathEscape(input.PriceID), nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPaymentProviderAPIRequestFailed, err)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(s.config.APIKey))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Stripe-Version", s.apiVersion)
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPaymentProviderAPIRequestFailed, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%w: status %d", ErrPaymentProviderAPIRequestFailed, response.StatusCode)
	}

	var price struct {
		ID         string `json:"id"`
		Active     bool   `json:"active"`
		Currency   string `json:"currency"`
		UnitAmount *int64 `json:"unit_amount"`
		Type       string `json:"type"`
		Recurring  *struct {
			Interval      string `json:"interval"`
			IntervalCount int64  `json:"interval_count"`
		} `json:"recurring"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&price); err != nil {
		return fmt.Errorf("%w: %v", ErrPaymentProviderAPIResponseInvalid, err)
	}

	expectedCurrency := strings.ToLower(strings.TrimSpace(input.ExpectedCurrency))
	expectedCadence := strings.ToLower(strings.TrimSpace(input.ExpectedBillingCadence))
	if price.ID != input.PriceID || !price.Active || price.UnitAmount == nil || *price.UnitAmount != input.ExpectedAmount ||
		!strings.EqualFold(price.Currency, expectedCurrency) {
		return ErrPaymentProviderPriceMismatch
	}
	if input.Mode == CheckoutModePayment {
		if expectedCadence != string(BillingKindOneTime) || price.Type != "one_time" || price.Recurring != nil {
			return ErrPaymentProviderPriceMismatch
		}
		return nil
	}
	if expectedCadence != "week" && expectedCadence != "month" && expectedCadence != "year" {
		return ErrPaymentProviderInvalidConfiguration
	}
	if price.Type != "recurring" || price.Recurring == nil || price.Recurring.Interval != expectedCadence || price.Recurring.IntervalCount != 1 {
		return ErrPaymentProviderPriceMismatch
	}
	return nil
}

// GetSubscriptionInfo retrieves subscription information from the provider API.
func (s *StripeProvider) GetSubscriptionInfo(ctx context.Context, subscriptionID string) (*SubscriptionInfo, error) {
	if strings.TrimSpace(s.config.APIKey) == "" || strings.TrimSpace(subscriptionID) == "" {
		return nil, ErrPaymentProviderSubscriptionNotFound
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBaseURL+"/v1/subscriptions/"+url.PathEscape(subscriptionID), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderAPIRequestFailed, err)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(s.config.APIKey))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Stripe-Version", s.apiVersion)
	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderAPIRequestFailed, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, ErrPaymentProviderSubscriptionNotFound
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: status %d", ErrPaymentProviderAPIRequestFailed, response.StatusCode)
	}

	var result struct {
		ID                 string `json:"id"`
		Customer           string `json:"customer"`
		Status             string `json:"status"`
		CurrentPeriodStart int64  `json:"current_period_start"`
		CurrentPeriodEnd   int64  `json:"current_period_end"`
		CancelAt           int64  `json:"cancel_at"`
		Items              struct {
			Data []struct {
				CurrentPeriodStart int64 `json:"current_period_start"`
				CurrentPeriodEnd   int64 `json:"current_period_end"`
				Price              struct {
					ID         string `json:"id"`
					UnitAmount int64  `json:"unit_amount"`
					Currency   string `json:"currency"`
					Recurring  struct {
						Interval string `json:"interval"`
					} `json:"recurring"`
				} `json:"price"`
			} `json:"data"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPaymentProviderAPIResponseInvalid, err)
	}
	if result.ID == "" {
		return nil, ErrPaymentProviderSubscriptionNotFound
	}

	currentPeriodStart := result.CurrentPeriodStart
	currentPeriodEnd := result.CurrentPeriodEnd
	if len(result.Items.Data) > 0 {
		currentPeriodStart = firstNonZero(currentPeriodStart, result.Items.Data[0].CurrentPeriodStart)
		currentPeriodEnd = firstNonZero(currentPeriodEnd, result.Items.Data[0].CurrentPeriodEnd)
	}
	info := &SubscriptionInfo{
		SubscriptionID:     result.ID,
		CustomerID:         result.Customer,
		Status:             stripeStatusToStandard(result.Status),
		NextBillingDate:    stripeUnixDate(currentPeriodEnd),
		CurrentPeriodStart: stripeUnixDate(currentPeriodStart),
		CurrentPeriodEnd:   stripeUnixDate(currentPeriodEnd),
		CancelledAt:        stripeUnixDate(result.CancelAt),
	}
	if len(result.Items.Data) > 0 {
		price := result.Items.Data[0].Price
		info.PlanID = price.ID
		info.Amount = float64(price.UnitAmount)
		info.Currency = strings.ToUpper(price.Currency)
		info.BillingInterval = price.Recurring.Interval
	}
	return info, nil
}

func isValidStripeAPIVersion(version string) bool {
	if version == "" || len(version) > 64 {
		return false
	}
	for _, character := range version {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func readAndRestoreWebhookBody(req *http.Request, maxBodySize int64) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, errors.New("request body is required")
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, maxBodySize+1))
	req.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBodySize {
		return nil, errors.New("request body exceeds the maximum supported size")
	}
	return body, nil
}

func parseStripeSignatureHeader(value string) (int64, []string, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil, ErrPaymentProviderMissingSignature
	}
	var timestamp int64
	var signatures []string
	for _, item := range strings.Split(value, ",") {
		keyValue := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(keyValue) != 2 {
			continue
		}
		switch keyValue[0] {
		case "t":
			parsed, err := strconv.ParseInt(keyValue[1], 10, 64)
			if err != nil {
				return 0, nil, ErrPaymentProviderInvalidWebhookSignature
			}
			timestamp = parsed
		case "v1":
			signatures = append(signatures, keyValue[1])
		}
	}
	if timestamp == 0 || len(signatures) == 0 {
		return 0, nil, ErrPaymentProviderInvalidWebhookSignature
	}
	return timestamp, signatures, nil
}

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
	case "checkout.session.completed", "checkout.session.async_payment_succeeded", "payment_intent.succeeded", "invoice.payment_succeeded", "invoice.paid":
		return EventTypePaymentSucceeded
	case "checkout.session.async_payment_failed", "payment_intent.payment_failed", "invoice.payment_failed":
		return EventTypePaymentFailed
	case "charge.refunded":
		return EventTypePaymentRefunded
	case "refund.created", "refund.updated":
		return EventTypePaymentPartiallyRefunded
	case "refund.failed":
		return EventTypePaymentRefundFailed
	case "payment_intent.requires_action", "invoice.payment_action_required":
		return EventTypePaymentActionRequired
	default:
		return eventType
	}
}

func stripeStatusToStandard(status string) string {
	switch status {
	case "active", "paid", "complete", "succeeded":
		return SubscriptionStatusActive
	case "trialing":
		return SubscriptionStatusTrialing
	case "past_due":
		return SubscriptionStatusPastDue
	case "canceled", "cancelled", "refunded":
		return SubscriptionStatusCancelled
	case "paused":
		return SubscriptionStatusPaused
	case "incomplete", "requires_action", "requires_payment_method":
		return SubscriptionStatusIncomplete
	case "unpaid":
		return SubscriptionStatusUnpaid
	default:
		return status
	}
}

func stripePaymentStatusToStandard(status string) string {
	switch status {
	case "paid", "succeeded":
		return PaymentStatusSucceeded
	case "failed":
		return PaymentStatusFailed
	case "refunded":
		return PaymentStatusRefunded
	case "requires_action", "unpaid":
		return PaymentStatusActionRequired
	case PaymentStatusNoPaymentRequired:
		return PaymentStatusNoPaymentRequired
	default:
		return status
	}
}

func stripeString(object map[string]any, key string) string {
	if value, ok := object[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func stripeStringOrID(object map[string]any, key string) string {
	if value := stripeString(object, key); value != "" {
		return value
	}
	return stripeString(stripeObject(object, key), "id")
}

func stripeObject(object map[string]any, key string) map[string]any {
	if object == nil {
		return nil
	}
	value, _ := object[key].(map[string]any)
	return value
}

func stripeInteger(object map[string]any, key string) int64 {
	value, ok := object[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case json.Number:
		result, _ := typed.Int64()
		return result
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	}
	return 0
}

func stripeBoolean(object map[string]any, key string) bool {
	value, _ := object[key].(bool)
	return value
}

func stripeNestedPriceID(object map[string]any) string {
	items := stripeObject(object, "items")
	if len(items) == 0 {
		items = stripeObject(object, "lines")
	}
	data, _ := items["data"].([]any)
	if len(data) == 0 {
		return ""
	}
	item, _ := data[0].(map[string]any)
	return firstNonEmpty(
		stripeString(stripeObject(item, "price"), "id"),
		stripeString(stripeObject(item, "plan"), "id"),
	)
}

func stripeInvoicePaymentReferences(object map[string]any) (paymentIntentID, chargeID string) {
	payments := stripeObject(object, "payments")
	data, _ := payments["data"].([]any)
	for _, rawItem := range data {
		item, _ := rawItem.(map[string]any)
		payment := stripeObject(item, "payment")
		paymentIntentID = firstNonEmpty(paymentIntentID, stripeStringOrID(payment, "payment_intent"))
		chargeID = firstNonEmpty(chargeID, stripeStringOrID(payment, "charge"))
	}
	return paymentIntentID, chargeID
}

func stripeSubscriptionItemPeriod(object map[string]any) (periodStart, periodEnd int64) {
	items := stripeObject(object, "items")
	data, _ := items["data"].([]any)
	for _, rawItem := range data {
		item, _ := rawItem.(map[string]any)
		itemStart := stripeInteger(item, "current_period_start")
		itemEnd := stripeInteger(item, "current_period_end")
		if itemStart > 0 && (periodStart == 0 || itemStart < periodStart) {
			periodStart = itemStart
		}
		if itemEnd > 0 && (periodEnd == 0 || itemEnd < periodEnd) {
			periodEnd = itemEnd
		}
	}
	return periodStart, periodEnd
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func stripeUnixDate(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// These helpers remain for package compatibility with provider-specific parsers.
func getStringField(object map[string]interface{}, key string) string {
	return stripeString(object, key)
}

func getFloatField(object map[string]interface{}, key string) float64 {
	if value, ok := object[key].(float64); ok {
		return value
	}
	return 0
}
