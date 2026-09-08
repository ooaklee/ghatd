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
	"math"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/observability"
)

const (
	stripeProviderName          = "stripe"
	stripeSignatureHeader       = "Stripe-Signature"
	stripeDefaultBodySize int64 = 2 << 20
)

var stripeDefaultSignatureTolerance = 5 * time.Minute
var stripeCustomerPortalConfigurationIDPattern = regexp.MustCompile(`^bpc_[A-Za-z0-9_]{1,251}$`)

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
		client = observability.NewHTTPClient(http.DefaultTransport, 10*time.Second)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.APIBaseURL), "/")
	if baseURL == "" {
		baseURL = StripeDefaultAPIBaseURL
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

// GetProviderName returns Stripe's stable provider registry key.
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

// GetCustomerPortalReturnURL returns the provider-owned trusted portal return
// destination. It is deliberately independent from the checkout return URL.
func (s *StripeProvider) GetCustomerPortalReturnURL() string {
	if s == nil || s.config == nil {
		return ""
	}
	return strings.TrimSpace(s.config.CustomerPortalReturnURL)
}

// ValidateCustomerPortalConfig validates Stripe portal configuration only
// when CustomerPortalReturnURL opts this provider into the generic route.
func (s *StripeProvider) ValidateCustomerPortalConfig() error {
	if s == nil || s.config == nil {
		return ErrPaymentProviderInvalidConfiguration
	}
	returnURL := s.GetCustomerPortalReturnURL()
	configurationID := strings.TrimSpace(s.config.CustomerPortalConfigurationID)
	if returnURL == "" {
		if configurationID != "" {
			return ErrPaymentProviderInvalidConfiguration
		}
		return nil
	}
	if strings.TrimSpace(s.config.APIKey) == "" || !isValidStripeCustomerPortalReturnURL(returnURL) {
		return ErrPaymentProviderInvalidConfiguration
	}
	if configurationID != "" && !stripeCustomerPortalConfigurationIDPattern.MatchString(configurationID) {
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
			if stripeCheckoutHasTrial(object) {
				markStripeRecurring(payload, EventTypeSubscriptionCreated, SubscriptionStatusTrialing)
				payload.PaymentStatus = PaymentStatusSucceeded
			} else {
				markStripePaymentSucceeded(payload)
			}
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
	case "checkout.session.async_payment_succeeded", "payment_intent.succeeded":
		markStripePaymentSucceeded(payload)
	case "invoice.payment_succeeded", "invoice.paid":
		payload.EventType = EventTypePaymentSucceeded
		payload.PaymentStatus = PaymentStatusSucceeded
		if !payload.IsRecurring() {
			payload.Status = SubscriptionStatusActive
		} else {
			// Subscription lifecycle events own recurring access state. Invoice
			// events update payment state without reviving a cancelled or paused
			// subscription when deliveries arrive out of order.
			payload.Status = ""
		}
	case "checkout.session.async_payment_failed", "payment_intent.payment_failed":
		payload.EventType = EventTypePaymentFailed
		payload.PaymentStatus = PaymentStatusFailed
		payload.Status = SubscriptionStatusIncomplete
	case "invoice.payment_failed":
		payload.EventType = EventTypePaymentFailed
		payload.PaymentStatus = PaymentStatusFailed
		if !payload.IsRecurring() {
			payload.Status = SubscriptionStatusIncomplete
		} else {
			payload.Status = ""
		}
	case "payment_intent.requires_action", "invoice.payment_action_required":
		markStripePaymentActionRequired(payload)
		if event.Type == "invoice.payment_action_required" && payload.IsRecurring() {
			payload.Status = ""
		}
	case "charge.refunded":
		markStripeChargeRefund(payload, object)
	case "refund.created", "refund.updated", "refund.failed":
		markStripeRefundLifecycle(payload, object)
	case "customer.subscription.created":
		payload.SubscriptionStateAuthoritative = true
		markStripeRecurring(payload, EventTypeSubscriptionCreated, stripeStatusToStandard(stripeString(object, "status")))
	case "customer.subscription.updated":
		payload.SubscriptionStateAuthoritative = true
		markStripeRecurring(payload, EventTypeSubscriptionUpdated, stripeStatusToStandard(stripeString(object, "status")))
	case "customer.subscription.deleted":
		payload.SubscriptionStateAuthoritative = true
		markStripeRecurring(payload, EventTypeSubscriptionCancelled, SubscriptionStatusCancelled)
	case "customer.subscription.paused":
		payload.SubscriptionStateAuthoritative = true
		markStripeRecurring(payload, EventTypeSubscriptionPaused, SubscriptionStatusPaused)
	case "customer.subscription.resumed":
		payload.SubscriptionStateAuthoritative = true
		markStripeRecurring(payload, EventTypeSubscriptionResumed, SubscriptionStatusActive)
	default:
		return nil, fmt.Errorf("%w: %s", ErrPaymentProviderInvalidEventType, event.Type)
	}

	return payload, nil
}

// stripeCheckoutHasTrial reports whether subscription metadata requests a trial.
func stripeCheckoutHasTrial(object map[string]any) bool {
	if stripeString(object, "mode") != CheckoutModeSubscription {
		return false
	}
	trialDays, err := strconv.Atoi(stripeString(stripeObject(object, "metadata"), "trial_period_days"))
	return err == nil && trialDays > 0
}

// markStripePaymentSucceeded marks a Stripe payment event as successful access.
func markStripePaymentSucceeded(payload *WebhookPayload) {
	payload.EventType = EventTypePaymentSucceeded
	payload.PaymentStatus = PaymentStatusSucceeded
	payload.Status = SubscriptionStatusActive
}

// markStripePaymentActionRequired marks a Stripe payment that needs customer action.
func markStripePaymentActionRequired(payload *WebhookPayload) {
	payload.EventType = EventTypePaymentActionRequired
	payload.PaymentStatus = PaymentStatusActionRequired
	payload.Status = SubscriptionStatusIncomplete
}

// markStripeChargeRefund maps a charge refund to full or partial refund semantics.
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

// markStripeRefundLifecycle maps an individual Stripe refund object's lifecycle.
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

// markStripeRecurring maps a Stripe subscription event to canonical recurring fields.
func markStripeRecurring(payload *WebhookPayload, eventType, status string) {
	payload.EventType = eventType
	payload.PaymentType = PaymentTypeSubscription
	payload.BillingKind = BillingKindRecurring
	payload.IsOneOff = false
	payload.Status = status
}

// stripeBasePayload extracts shared identity, catalogue, payment, and period fields.
func stripeBasePayload(eventID, eventType string, created int64, raw []byte, object map[string]any) *WebhookPayload {
	subscriptionDetails := stripeObject(stripeObject(object, "parent"), "subscription_details")
	// Subscription invoice metadata is an immutable snapshot under
	// parent.subscription_details. Merge it with object metadata so an
	// invoice-specific key does not hide the application-owned subscription
	// correlation written when Checkout created the subscription.
	metadata := mergeStripeMetadata(
		stripeObject(subscriptionDetails, "metadata"),
		stripeObject(object, "metadata"),
	)
	mode := stripeString(object, "mode")
	subscriptionID := stripeStringOrID(object, "subscription")
	if subscriptionID == "" {
		subscriptionID = stripeStringOrID(subscriptionDetails, "subscription")
	}
	objectID := stripeString(object, "id")
	invoicePaymentIntentID, invoiceChargeID := stripeInvoicePaymentReferences(object)
	paymentIntentID := firstNonEmpty(stripeStringOrID(object, "payment_intent"), invoicePaymentIntentID)
	transactionID := firstNonEmpty(paymentIntentID, stripeStringOrID(object, "charge"), invoiceChargeID, objectID)
	if strings.HasPrefix(eventType, "customer.subscription.") {
		transactionID = firstNonEmpty(paymentIntentID, stripeStringOrID(object, "latest_invoice"))
	}

	billingKind := billingKindFromStripeObject(mode, metadata, subscriptionID, eventType)
	paymentType := ""
	isOneOff := billingKind == BillingKindOneTime
	if billingKind == BillingKindOneTime {
		paymentType = PaymentTypePurchase
	} else if billingKind == BillingKindRecurring {
		paymentType = PaymentTypeSubscription
	}
	if strings.HasPrefix(eventType, "customer.subscription.") && subscriptionID == "" {
		subscriptionID = objectID
	}

	subscriptionTerms := SubscriptionTerms{}
	if strings.HasPrefix(eventType, "customer.subscription.") {
		subscriptionTerms = stripeSubscriptionTerms(object, metadata)
	}
	priceID := firstNonEmpty(
		stripeOptionalString(subscriptionTerms.ProviderPriceID),
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
		Status:            stripeStatusToStandard(stripeString(object, "status")),
		PaymentStatus:     stripePaymentStatusToStandard(stripeString(object, "payment_status")),
		PlanName:          planName,
		PlanID:            stripeString(metadata, "plan_id"),
		PlanSlug:          stripeString(metadata, "plan_slug"),
		CostID:            stripeString(metadata, "cost_id"),
		ProviderPriceID:   priceID,
		Amount:            amount,
		Currency:          strings.ToUpper(stripeString(object, "currency")),
		SubscriptionTerms: subscriptionTerms,
		NextBillingDate:   nextBillingDate,
		TrialEndsAt:       stripeUnixDate(stripeInteger(object, "trial_end")),
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

// stripeSubscriptionTerms resolves exactly one licensed, per-unit recurring
// item. Metadata correlation wins; a sole item is the only fallback. Ambiguous,
// metered, tiered, fractional-decimal, invalid-quantity, or partially expanded
// items keep the commercial amount unknown instead of fabricating a value.
func stripeSubscriptionTerms(object, metadata map[string]any) SubscriptionTerms {
	itemsObject := stripeObject(object, "items")
	if _, present := itemsObject["data"]; !present {
		return SubscriptionTerms{}
	}
	observed := SubscriptionTerms{Observed: true}
	item, ok := stripeSelectedSubscriptionItem(object, firstNonEmpty(
		stripeString(metadata, "provider_price_id"),
		stripeString(metadata, "price_id"),
	))
	if !ok {
		return observed
	}

	price := stripeObject(item, "price")
	if len(price) == 0 {
		price = stripeObject(item, "plan")
	}
	if len(price) == 0 {
		return observed
	}

	terms := observed
	if priceID := stripeString(price, "id"); priceID != "" {
		terms.ProviderPriceID = &priceID
	}
	if currency := strings.ToUpper(stripeString(price, "currency")); currency != "" {
		terms.Currency = &currency
	}
	recurring := stripeObject(price, "recurring")
	if len(recurring) == 0 {
		recurring = price
	}
	if interval := strings.ToLower(stripeString(recurring, "interval")); interval != "" {
		terms.BillingInterval = &interval
		intervalCount, present := stripeIntegerValue(recurring, "interval_count")
		if !present {
			intervalCount = 1
		}
		if intervalCount > 0 {
			terms.BillingIntervalCount = &intervalCount
		}
	}

	if !strings.EqualFold(stripeString(recurring, "usage_type"), "licensed") ||
		!strings.EqualFold(stripeString(price, "billing_scheme"), "per_unit") {
		return terms
	}
	unitAmount, amountPresent := stripeIntegerValue(price, "unit_amount")
	decimalRaw := stripeString(price, "unit_amount_decimal")
	decimalAmount, decimalPresent := stripeIntegralMinorUnitDecimal(decimalRaw)
	if decimalRaw != "" && !decimalPresent {
		return terms
	}
	if decimalPresent && amountPresent && decimalAmount != unitAmount {
		return terms
	}
	if !amountPresent && decimalPresent {
		unitAmount, amountPresent = decimalAmount, true
	}
	if !amountPresent || unitAmount < 0 {
		return terms
	}
	quantity, quantityPresent := stripeIntegerValue(item, "quantity")
	if !quantityPresent || quantity <= 0 {
		return terms
	}
	terms.Quantity = &quantity
	if unitAmount != 0 && quantity > math.MaxInt64/unitAmount {
		return terms
	}
	amount := unitAmount * quantity
	terms.Amount = &amount
	return terms
}

// stripeSelectedSubscriptionItem selects one unambiguous subscription item.
func stripeSelectedSubscriptionItem(object map[string]any, preferredPriceID string) (map[string]any, bool) {
	itemsObject := stripeObject(object, "items")
	rawItems, _ := itemsObject["data"].([]any)
	items := make([]map[string]any, 0, len(rawItems))
	for _, rawItem := range rawItems {
		if item, ok := rawItem.(map[string]any); ok {
			items = append(items, item)
		}
	}
	if preferredPriceID != "" {
		var match map[string]any
		matches := 0
		for _, item := range items {
			priceID := firstNonEmpty(
				stripeString(stripeObject(item, "price"), "id"),
				stripeString(stripeObject(item, "plan"), "id"),
			)
			if priceID == preferredPriceID {
				match = item
				matches++
			}
		}
		if matches == 1 {
			return match, true
		}
		return nil, false
	}
	if len(items) == 1 {
		return items[0], true
	}
	return nil, false
}

// stripeIntegralMinorUnitDecimal parses an exact integral minor-unit amount.
func stripeIntegralMinorUnitDecimal(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok || !rational.IsInt() || !rational.Num().IsInt64() {
		return 0, false
	}
	return rational.Num().Int64(), true
}

// stripeOptionalString trims a nullable Stripe string.
func stripeOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// billingKindFromStripeObject infers recurring versus one-time Stripe billing.
func billingKindFromStripeObject(mode string, metadata map[string]any, subscriptionID, eventType string) BillingKind {
	kind := strings.ToLower(firstNonEmpty(stripeString(metadata, "billing_kind"), stripeString(metadata, "billing_cadence")))
	switch kind {
	case string(BillingKindRecurring), CheckoutModeSubscription, "week", "weekly", "month", "monthly", "year", "yearly", "annual":
		return BillingKindRecurring
	case string(BillingKindOneTime), CheckoutModePayment, "one-time", "one_off", "lifetime":
		return BillingKindOneTime
	}
	if mode == CheckoutModePayment {
		return BillingKindOneTime
	}
	if mode == CheckoutModeSubscription || subscriptionID != "" || strings.HasPrefix(eventType, "customer.subscription.") {
		return BillingKindRecurring
	}
	// PaymentIntent, Charge, Refund, and non-subscription Invoice objects do
	// not carry enough information on their own to prove billing cadence. An
	// empty classification keeps them ledger-only until Billing Manager can
	// correlate them to server-owned access state.
	return ""
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
			// Managed Payments rejects invoice_creation[invoice_data], so keep
			// entitlement correlation on the Session and PaymentIntent instead.
			form.Set("payment_intent_data[metadata]["+key+"]", value)
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
	if s == nil || s.config == nil || input == nil || !isValidStripeCustomerID(input.CustomerID) || !isValidStripeCustomerPortalReturnURL(input.ReturnURL) {
		return nil, ErrPaymentProviderInvalidConfiguration
	}
	if strings.TrimSpace(s.config.APIKey) == "" {
		return nil, ErrPaymentProviderInvalidConfiguration
	}

	form := url.Values{}
	form.Set("customer", strings.TrimSpace(input.CustomerID))
	form.Set("return_url", strings.TrimSpace(input.ReturnURL))
	if configurationID := strings.TrimSpace(s.config.CustomerPortalConfigurationID); configurationID != "" {
		if !stripeCustomerPortalConfigurationIDPattern.MatchString(configurationID) {
			return nil, ErrPaymentProviderInvalidConfiguration
		}
		form.Set("configuration", configurationID)
	}
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
	if strings.TrimSpace(session.ID) == "" || s.ValidateCustomerPortalSessionURL(session.URL) != nil {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	session.ID = strings.TrimSpace(session.ID)
	session.URL = strings.TrimSpace(session.URL)
	return &session, nil
}

// CreateUpcomingInvoicePreview asks Stripe for a read-only estimate of the
// next invoice for a server-owned subscription. The returned invoice is only a
// preview and is deliberately not persisted as a payment or access event.
func (s *StripeProvider) CreateUpcomingInvoicePreview(ctx context.Context, input *UpcomingInvoicePreviewRequest) (*UpcomingInvoicePreview, error) {
	if s == nil || s.config == nil || input == nil || !isValidStripeSubscriptionID(input.SubscriptionID) || strings.TrimSpace(s.config.APIKey) == "" {
		return nil, ErrPaymentProviderInvalidConfiguration
	}

	form := url.Values{}
	form.Set("subscription", strings.TrimSpace(input.SubscriptionID))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBaseURL+"/v1/invoices/create_preview", strings.NewReader(form.Encode()))
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

	const maxPreviewResponseSize int64 = 256 << 10
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPreviewResponseSize+1))
	if err != nil || int64(len(body)) > maxPreviewResponseSize {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	var invoice struct {
		Object     string `json:"object"`
		Subtotal   *int64 `json:"subtotal"`
		Total      *int64 `json:"total"`
		AmountDue  *int64 `json:"amount_due"`
		Currency   string `json:"currency"`
		DueDate    int64  `json:"due_date"`
		TotalTaxes []struct {
			Amount int64 `json:"amount"`
		} `json:"total_taxes"`
		TotalTaxAmounts []struct {
			Amount int64 `json:"amount"`
		} `json:"total_tax_amounts"`
	}
	if err := json.Unmarshal(body, &invoice); err != nil {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	currency := strings.ToUpper(strings.TrimSpace(invoice.Currency))
	if invoice.Object != "invoice" || invoice.Subtotal == nil || invoice.Total == nil || invoice.AmountDue == nil ||
		!isValidCurrencyCode(currency) || *invoice.Subtotal < 0 || *invoice.Total < 0 || *invoice.AmountDue < 0 {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	taxAmount, ok := sumStripeTaxAmounts(invoice.TotalTaxes, invoice.TotalTaxAmounts)
	if !ok {
		return nil, ErrPaymentProviderAPIResponseInvalid
	}
	return &UpcomingInvoicePreview{
		Subtotal:  *invoice.Subtotal,
		TaxAmount: taxAmount,
		Total:     *invoice.Total,
		AmountDue: *invoice.AmountDue,
		Currency:  currency,
		DueDate:   stripeUnixDate(invoice.DueDate),
	}, nil
}

// sumStripeTaxAmounts safely totals current or legacy Stripe tax amounts.
func sumStripeTaxAmounts(current, legacy []struct {
	Amount int64 `json:"amount"`
}) (int64, bool) {
	amounts := current
	if len(amounts) == 0 {
		amounts = legacy
	}
	var total int64
	for _, tax := range amounts {
		if tax.Amount < 0 || tax.Amount > math.MaxInt64-total {
			return 0, false
		}
		total += tax.Amount
	}
	return total, true
}

// isValidCurrencyCode accepts three-letter upper-case currency codes.
func isValidCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

// ValidateCustomerPortalSessionURL enforces Stripe's documented hosted portal
// origin before Billing Manager can return a provider URL to a browser.
func (s *StripeProvider) ValidateCustomerPortalSessionURL(value string) error {
	if s == nil || !isValidStripeCustomerPortalURL(value) {
		return ErrPaymentProviderAPIResponseInvalid
	}
	return nil
}

// isValidStripeCustomerID checks the documented object prefix without making
// assumptions about Stripe's opaque suffix beyond a conservative size bound.
func isValidStripeCustomerID(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "cus_") && len(value) > len("cus_") && len(value) <= 255 &&
		!strings.ContainsAny(value, " \t\r\n")
}

// isValidStripeSubscriptionID validates the documented subscription prefix and size.
func isValidStripeSubscriptionID(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "sub_") && len(value) > len("sub_") && len(value) <= 255 &&
		!strings.ContainsAny(value, " \t\r\n")
}

// isValidStripeCustomerPortalReturnURL rejects templates and malformed return URLs.
func isValidStripeCustomerPortalReturnURL(value string) bool {
	return !strings.ContainsAny(strings.TrimSpace(value), "{}") && isValidAbsoluteHTTPURL(value, false)
}

// isValidStripeCustomerPortalURL allowlists Stripe's documented hosted portal
// origin. This prevents an upstream or parsing failure becoming an open redirect.
func isValidStripeCustomerPortalURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.IsAbs() && strings.EqualFold(parsed.Scheme, "https") &&
		strings.EqualFold(parsed.Host, "billing.stripe.com") && parsed.User == nil && parsed.Opaque == ""
}

// isValidAbsoluteHTTPURL validates a return URL accepted by Stripe checkout.
func isValidAbsoluteHTTPURL(value string, requireHTTPS bool) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || !isValidHTTPURLPort(parsed) {
		return false
	}
	if requireHTTPS {
		return strings.EqualFold(parsed.Scheme, "https")
	}
	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
}

// isValidHTTPURLPort checks that an explicit URL port is within the TCP range.
func isValidHTTPURLPort(parsed *url.URL) bool {
	port := parsed.Port()
	if port == "" {
		return true
	}
	portNumber, err := strconv.Atoi(port)
	return err == nil && portNumber >= 1 && portNumber <= 65535
}

// validateCheckoutPrice rechecks the live Stripe price against trusted catalogue terms.
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

// isValidStripeAPIVersion validates the pinned Stripe API version format.
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

// readAndRestoreWebhookBody bounds webhook input and restores it for later parsing.
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

// parseStripeSignatureHeader extracts the timestamp and candidate v1 signatures.
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

// stripeEventToStandard maps a Stripe event type to the shared event vocabulary.
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

// stripeStatusToStandard maps Stripe subscription states to shared statuses.
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

// stripePaymentStatusToStandard maps Stripe payment states to shared statuses.
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

// stripeString reads a string field from a decoded Stripe object.
func stripeString(object map[string]any, key string) string {
	if value, ok := object[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// stripeStringOrID reads either a Stripe expandable ID string or object ID.
func stripeStringOrID(object map[string]any, key string) string {
	if value := stripeString(object, key); value != "" {
		return value
	}
	return stripeString(stripeObject(object, key), "id")
}

// stripeObject reads a nested Stripe object, returning nil for another shape.
func stripeObject(object map[string]any, key string) map[string]any {
	if object == nil {
		return nil
	}
	value, _ := object[key].(map[string]any)
	return value
}

// mergeStripeMetadata combines application correlation copied to a
// subscription invoice with any event-object metadata. Object metadata wins
// on duplicate keys while an empty result remains nil.
func mergeStripeMetadata(metadataSets ...map[string]any) map[string]any {
	var merged map[string]any
	for _, metadata := range metadataSets {
		for key, value := range metadata {
			if merged == nil {
				merged = make(map[string]any)
			}
			merged[key] = value
		}
	}
	return merged
}

// stripeInteger reads an integer-valued Stripe field across JSON number types.
func stripeInteger(object map[string]any, key string) int64 {
	value, ok := stripeIntegerValue(object, key)
	if !ok {
		return 0
	}
	return value
}

// stripeIntegerValue preserves field presence, including an explicit zero.
func stripeIntegerValue(object map[string]any, key string) (int64, bool) {
	value, ok := object[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Int64()
		return result, err == nil
	case float64:
		if math.Trunc(typed) != typed || typed > math.MaxInt64 || typed < math.MinInt64 {
			return 0, false
		}
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	}
	return 0, false
}

// stripeBoolean reads a boolean-valued Stripe field.
func stripeBoolean(object map[string]any, key string) bool {
	value, _ := object[key].(bool)
	return value
}

// stripeNestedPriceID extracts a price ID from common Stripe line-item shapes.
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

// stripeInvoicePaymentReferences extracts payment IDs from modern invoice payments.
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

// stripeSubscriptionItemPeriod extracts current period bounds from subscription items.
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

// firstNonZero returns the first positive integer in the supplied values.
func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

// stripeUnixDate converts a Unix timestamp to an RFC3339 string.
func stripeUnixDate(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}

// firstNonEmpty returns the first non-blank, trimmed string.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// getStringField remains for package compatibility with provider-specific parsers.
func getStringField(object map[string]interface{}, key string) string {
	return stripeString(object, key)
}

// getFloatField reads a floating-point field from a decoded provider object.
func getFloatField(object map[string]interface{}, key string) float64 {
	if value, ok := object[key].(float64); ok {
		return value
	}
	return 0
}
