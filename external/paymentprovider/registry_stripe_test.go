package paymentprovider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/common"
)

type bodyReadingProvider struct {
	verified string
	parsed   string
}

func (*bodyReadingProvider) GetProviderName() string { return "body-reader" }
func (p *bodyReadingProvider) VerifyWebhook(_ context.Context, req *http.Request) error {
	body, err := io.ReadAll(req.Body)
	p.verified = string(body)
	return err
}
func (p *bodyReadingProvider) ParsePayload(_ context.Context, req *http.Request) (*WebhookPayload, error) {
	body, err := io.ReadAll(req.Body)
	p.parsed = string(body)
	return &WebhookPayload{EventID: "evt_1", EventType: EventTypePaymentSucceeded}, err
}
func (*bodyReadingProvider) GetSubscriptionInfo(context.Context, string) (*SubscriptionInfo, error) {
	return nil, nil
}

func TestRegistryPreservesBodyAcrossVerificationAndParsing(t *testing.T) {
	provider := &bodyReadingProvider{}
	registry := NewProviderRegistry()
	registry.Register(provider)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"value":"same body"}`))

	if _, err := registry.VerifyAndParseWebhookPayload(context.Background(), provider.GetProviderName(), request); err != nil {
		t.Fatalf("VerifyAndParseWebhookPayload() error = %v", err)
	}
	if provider.verified != provider.parsed || provider.parsed != `{"value":"same body"}` {
		t.Fatalf("body mismatch: verified=%q parsed=%q", provider.verified, provider.parsed)
	}
	restored, err := io.ReadAll(request.Body)
	if err != nil || string(restored) != provider.parsed {
		t.Fatalf("restored body = %q, %v", restored, err)
	}
}

func TestRegistryRejectsOversizedBody(t *testing.T) {
	registry := NewProviderRegistry()
	registry.Register(&bodyReadingProvider{})
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(strings.Repeat("x", int(defaultMaxWebhookBodySize)+1)))

	if _, err := registry.VerifyAndParseWebhookPayload(context.Background(), "body-reader", request); err != ErrPaymentProviderInvalidPayload {
		t.Fatalf("VerifyAndParseWebhookPayload() error = %v, want %v", err, ErrPaymentProviderInvalidPayload)
	}
}

func TestStripeCheckoutSessionPaymentNormalisation(t *testing.T) {
	created := time.Now().Unix()
	body := []byte(`{"id":"evt_checkout","type":"checkout.session.completed","created":` + strconv.FormatInt(created, 10) + `,"data":{"object":{"id":"cs_1","mode":"payment","payment_intent":"pi_1","payment_status":"paid","customer":"cus_1","customer_email":"buyer@example.test","amount_total":1200,"currency":"gbp","client_reference_id":"user_1","metadata":{"plan_id":"plan_1","plan_slug":"lifetime","plan_name":"Lifetime","cost_id":"cost_1","provider_price_id":"price_1"}}}}`)
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test", SignatureTolerance: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	request.Header.Set(stripeSignatureHeader, stripeTestSignature("whsec_test", created, body))

	if err := provider.VerifyWebhook(context.Background(), request); err != nil {
		t.Fatalf("VerifyWebhook() error = %v", err)
	}
	payload, err := provider.ParsePayload(context.Background(), request)
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	if payload.PaymentType != PaymentTypePurchase || payload.BillingKind != BillingKindOneTime || !payload.IsOneOff {
		t.Fatalf("classification = %q %q oneOff=%v", payload.PaymentType, payload.BillingKind, payload.IsOneOff)
	}
	if payload.TransactionID != "pi_1" || payload.SubscriptionID != "" || payload.UserReference != "user_1" {
		t.Fatalf("identifiers = transaction:%q subscription:%q user:%q", payload.TransactionID, payload.SubscriptionID, payload.UserReference)
	}
	if payload.PaymentStatus != PaymentStatusSucceeded || payload.PlanID != "plan_1" || payload.CostID != "cost_1" || payload.ProviderPriceID != "price_1" {
		t.Fatalf("normalised payload = %#v", payload)
	}
}

func TestStripeProviderExposesProviderOwnedCheckoutReturnURL(t *testing.T) {
	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test",
		ReturnURL:     "  https://app.example.test/checkout/return  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	configured, ok := interface{}(provider).(CheckoutReturnURLProvider)
	if !ok || configured.GetCheckoutReturnURL() != "https://app.example.test/checkout/return" {
		t.Fatalf("checkout return URL capability = %#v", provider)
	}

	var nilProvider *StripeProvider
	if nilProvider.GetCheckoutReturnURL() != "" {
		t.Fatal("typed-nil Stripe provider returned checkout configuration")
	}
}

func TestStripeCheckoutConfigValidationUsesReturnURLAsOptIn(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantError   error
		wantOptedIn bool
	}{
		{
			name:   "webhook only",
			config: &Config{WebhookSecret: "whsec_test"},
		},
		{
			name:   "API sync without checkout",
			config: &Config{WebhookSecret: "whsec_test", APIKey: "sk_test"},
		},
		{
			name:        "missing API key",
			config:      &Config{WebhookSecret: "whsec_test", PublishableKey: "pk_test", ReturnURL: "https://app.example.test/return"},
			wantError:   ErrPaymentProviderInvalidConfiguration,
			wantOptedIn: true,
		},
		{
			name:        "missing publishable key",
			config:      &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", ReturnURL: "https://app.example.test/return"},
			wantError:   ErrPaymentProviderInvalidConfiguration,
			wantOptedIn: true,
		},
		{
			name:        "relative return URL",
			config:      &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", PublishableKey: "pk_test", ReturnURL: "/return"},
			wantError:   ErrPaymentProviderInvalidConfiguration,
			wantOptedIn: true,
		},
		{
			name:        "complete checkout",
			config:      &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", PublishableKey: "pk_test", ReturnURL: "https://app.example.test/return?session_id={CHECKOUT_SESSION_ID}"},
			wantOptedIn: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, err := NewStripeProvider(test.config)
			if err != nil {
				t.Fatalf("NewStripeProvider() error = %v", err)
			}
			err = ValidateCheckoutProviderConfig(provider)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ValidateCheckoutProviderConfig() error = %v, want %v", err, test.wantError)
			}
			if optedIn := provider.GetCheckoutReturnURL() != ""; optedIn != test.wantOptedIn {
				t.Fatalf("checkout opted-in = %v, want %v", optedIn, test.wantOptedIn)
			}
		})
	}
}

func TestProviderRegistryRejectsInvalidOptedInStripeCheckout(t *testing.T) {
	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test",
		ReturnURL:     "https://app.example.test/return",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewProviderRegistry()
	registry.Register(provider)
	if _, err := registry.GetCheckoutProvider("stripe"); !errors.Is(err, ErrPaymentProviderInvalidConfiguration) {
		t.Fatalf("GetCheckoutProvider() error = %v, want invalid configuration", err)
	}
}

func TestStripeSubscriptionEventKeepsRecurringIdentity(t *testing.T) {
	body := `{"id":"evt_subscription","type":"customer.subscription.updated","created":1700000000,"data":{"object":{"id":"sub_1","customer":"cus_1","status":"active","metadata":{"user_id":"user_1","plan_id":"plan_1"},"items":{"data":[{"current_period_start":1700000000,"current_period_end":1701000000,"price":{"id":"price_recurring"}}]}}}}`
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := provider.ParsePayload(context.Background(), httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body)))
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	if !payload.IsRecurring() || payload.SubscriptionID != "sub_1" || payload.TransactionID != "" {
		t.Fatalf("recurring identifiers = subscription:%q transaction:%q recurring:%v", payload.SubscriptionID, payload.TransactionID, payload.IsRecurring())
	}
	if payload.ProviderPriceID != "price_recurring" || payload.UserReference != "user_1" {
		t.Fatalf("stable identifiers = %#v", payload)
	}
	if payload.NextBillingDate != stripeUnixDate(1701000000) {
		t.Fatalf("NextBillingDate = %q", payload.NextBillingDate)
	}
}

func TestStripeTrialCheckoutGrantsRecurringAccessWithoutLabellingPaymentSucceeded(t *testing.T) {
	body := `{"id":"evt_trial_checkout","type":"checkout.session.completed","created":1700000000,"data":{"object":{"id":"cs_1","mode":"subscription","subscription":"sub_1","payment_status":"no_payment_required","customer":"cus_1","customer_email":"buyer@example.test","client_reference_id":"user_1","metadata":{"billing_kind":"recurring","trial_period_days":"14","plan_id":"plan_1","cost_id":"cost_1"}}}}`
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := provider.ParsePayload(context.Background(), httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body)))
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	if !payload.IsRecurring() || payload.EventType != EventTypeSubscriptionCreated || payload.SubscriptionID != "sub_1" {
		t.Fatalf("trial classification = %#v", payload)
	}
	if payload.PaymentStatus != PaymentStatusNoPaymentRequired || payload.Status != SubscriptionStatusTrialing {
		t.Fatalf("trial state = %#v", payload)
	}
}

func TestBillingKindFromStripeObjectRecognisesCanonicalRecurringCadences(t *testing.T) {
	for _, cadence := range []string{"week", "weekly", "month", "monthly", "year", "yearly", "annual"} {
		t.Run(cadence, func(t *testing.T) {
			kind := billingKindFromStripeObject("", map[string]any{"billing_cadence": cadence}, "", "")
			if kind != BillingKindRecurring {
				t.Fatalf("billing kind = %q, want %q", kind, BillingKindRecurring)
			}
		})
	}
}

func TestStripeRefundUsesPaymentIntentAsTransactionIdentity(t *testing.T) {
	body := `{"id":"evt_refund","type":"charge.refunded","created":1700000000,"data":{"object":{"id":"ch_1","payment_intent":"pi_1","customer":"cus_1","amount":1200,"amount_refunded":1200,"refunded":true,"currency":"gbp"}}}`
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := provider.ParsePayload(context.Background(), httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body)))
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	if payload.EventType != EventTypePaymentRefunded || payload.PaymentStatus != PaymentStatusRefunded || payload.TransactionID != "pi_1" {
		t.Fatalf("refund payload = %#v", payload)
	}
	if payload.CustomerEmail != "" || payload.PaymentType != "" || payload.BillingKind != "" || payload.IsOneOff {
		t.Fatalf("refund classification = %#v", payload)
	}
}

func TestStripeMetadataFreePaymentIntentRemainsLedgerOnly(t *testing.T) {
	body := `{"id":"evt_payment","type":"payment_intent.succeeded","created":1700000000,"data":{"object":{"id":"pi_1","customer":"cus_1","status":"succeeded","amount_received":1800,"currency":"usd","metadata":{}}}}`
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := provider.ParsePayload(context.Background(), httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body)))
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	if payload.EventType != EventTypePaymentSucceeded || payload.TransactionID != "pi_1" || payload.CustomerID != "cus_1" {
		t.Fatalf("payment identity = %#v", payload)
	}
	if payload.BillingKind != "" || payload.PaymentType != "" || payload.IsOneOff || payload.IsRecurring() || payload.GrantsPlanAccess() {
		t.Fatalf("metadata-free payment classification = %#v", payload)
	}
}

func TestStripeRecurringInvoiceMergesSubscriptionSnapshotAndLeavesAccessStateToSubscription(t *testing.T) {
	body := `{"id":"evt_invoice","type":"invoice.paid","created":1700000000,"data":{"object":{"id":"in_1","customer":"cus_1","customer_email":"buyer@example.test","status":"paid","amount_paid":1800,"currency":"usd","period_end":1700000000,"metadata":{"invoice_note":"renewal"},"parent":{"type":"subscription_details","subscription_details":{"subscription":"sub_1","metadata":{"billing_kind":"recurring","user_reference":"user_1","plan_id":"plan_1","cost_id":"cost_1","provider_price_id":"price_1"}}},"payments":{"data":[{"payment":{"type":"payment_intent","payment_intent":"pi_1"}}]}}}}`
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := provider.ParsePayload(context.Background(), httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body)))
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	if !payload.IsRecurring() || payload.SubscriptionID != "sub_1" || payload.TransactionID != "pi_1" {
		t.Fatalf("recurring invoice identity = %#v", payload)
	}
	if payload.UserReference != "user_1" || payload.PlanID != "plan_1" || payload.CostID != "cost_1" || payload.ProviderPriceID != "price_1" {
		t.Fatalf("subscription snapshot metadata = %#v", payload)
	}
	if payload.EventType != EventTypePaymentSucceeded || payload.PaymentStatus != PaymentStatusSucceeded || payload.Status != "" {
		t.Fatalf("invoice payment/access state = %#v", payload)
	}
}

func TestStripePartialRefundKeepsAccessStateUnchanged(t *testing.T) {
	body := `{"id":"evt_partial_refund","type":"charge.refunded","created":1700000000,"data":{"object":{"id":"ch_1","payment_intent":"pi_1","customer":"cus_1","amount":1200,"amount_refunded":400,"refunded":false,"currency":"gbp"}}}`
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := provider.ParsePayload(context.Background(), httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body)))
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	if payload.EventType != EventTypePaymentPartiallyRefunded || payload.PaymentStatus != PaymentStatusPartiallyRefunded || payload.Status != "" || payload.TransactionID != "pi_1" {
		t.Fatalf("partial refund payload = %#v", payload)
	}
}

func TestStripeRefundObjectDoesNotAssumeFullRefund(t *testing.T) {
	body := `{"id":"evt_refund_created","type":"refund.created","created":1700000000,"data":{"object":{"id":"re_1","payment_intent":"pi_1","charge":"ch_1","status":"succeeded","amount":400,"currency":"gbp"}}}`
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := provider.ParsePayload(context.Background(), httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body)))
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	if payload.EventType != EventTypePaymentPartiallyRefunded || payload.PaymentStatus != PaymentStatusPartiallyRefunded || payload.Status != "" || payload.TransactionID != "pi_1" {
		t.Fatalf("refund lifecycle payload = %#v", payload)
	}
}

func TestStripeOneTimeInvoiceKeepsPurchaseIdentity(t *testing.T) {
	body := `{"id":"evt_invoice","type":"invoice.paid","created":1700000000,"data":{"object":{"id":"in_1","customer":"cus_1","customer_email":"buyer@example.test","amount_paid":1200,"currency":"gbp","payments":{"data":[{"id":"inpay_1","status":"paid","payment":{"type":"payment_intent","payment_intent":"pi_1"}}]},"metadata":{"billing_kind":"one_time","user_reference":"user_1","plan_id":"plan_1","plan_slug":"lifetime","cost_id":"cost_1","provider_price_id":"price_1"}}}}`
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := provider.ParsePayload(context.Background(), httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body)))
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}
	if payload.PaymentType != PaymentTypePurchase || payload.BillingKind != BillingKindOneTime || !payload.IsOneOff {
		t.Fatalf("invoice classification = %#v", payload)
	}
	if payload.TransactionID != "pi_1" || payload.SubscriptionID != "" || payload.UserReference != "user_1" {
		t.Fatalf("invoice identifiers = %#v", payload)
	}
}

func TestStripeRejectsWebhookTimestampOutsideTolerance(t *testing.T) {
	body := []byte(`{"id":"evt_time","type":"checkout.session.completed","data":{"object":{"id":"cs_1"}}}`)
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test", SignatureTolerance: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	for _, timestamp := range []int64{time.Now().Add(-2 * time.Minute).Unix(), time.Now().Add(2 * time.Minute).Unix()} {
		request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		request.Header.Set(stripeSignatureHeader, stripeTestSignature("whsec_test", timestamp, body))
		if err := provider.VerifyWebhook(context.Background(), request); err != ErrPaymentProviderWebhookTimestampTooOld {
			t.Errorf("VerifyWebhook(timestamp=%d) error = %v, want %v", timestamp, err, ErrPaymentProviderWebhookTimestampTooOld)
		}
	}
}

func TestStripeCheckoutCapabilityCopiesSubscriptionMetadata(t *testing.T) {
	var (
		received          url.Values
		idempotencyHeader string
		apiVersionHeader  string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		received = r.PostForm
		idempotencyHeader = r.Header.Get(common.IdempotencyKeyHttpHeader)
		apiVersionHeader = r.Header.Get("Stripe-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cs_1","client_secret":"secret_1"}`)
	}))
	defer server.Close()

	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test", APIKey: "sk_test", PublishableKey: "pk_test",
		APIBaseURL: server.URL, ReturnURL: "https://example.test/complete", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var checkout CheckoutProvider = provider
	session, err := checkout.CreateCheckoutSession(context.Background(), &CheckoutSessionRequest{
		PriceID: "price_1", PlanID: "plan_1", PlanSlug: "pro", PlanName: "Pro", CostID: "cost_1",
		UserID: "user_1", CustomerEmail: "buyer@example.test", Mode: CheckoutModeSubscription, TrialPeriodDays: 14,
		IdempotencyKey: "checkout-attempt-1",
	})
	if err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}
	if session.ID != "cs_1" || session.ClientSecret != "secret_1" || session.PublishableKey != "pk_test" {
		t.Fatalf("session = %#v", session)
	}
	if idempotencyHeader != "checkout-attempt-1" {
		t.Fatalf("Idempotency-Key = %q", idempotencyHeader)
	}
	if apiVersionHeader != StripeDefaultAPIVersion {
		t.Fatalf("Stripe-Version = %q, want %q", apiVersionHeader, StripeDefaultAPIVersion)
	}
	for key, want := range map[string]string{
		"metadata[plan_id]": "plan_1", "subscription_data[metadata][plan_id]": "plan_1",
		"subscription_data[metadata][cost_id]": "cost_1", "subscription_data[metadata][user_reference]": "user_1",
		"metadata[trial_period_days]": "14", "subscription_data[metadata][trial_period_days]": "14",
		"subscription_data[trial_period_days]": "14", "redirect_on_completion": "if_required",
	} {
		if got := received.Get(key); got != want {
			t.Errorf("form %s = %q, want %q", key, got, want)
		}
	}
}

func TestStripeCheckoutRejectsTrialForOneTimePayment(t *testing.T) {
	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test", APIKey: "sk_test", PublishableKey: "pk_test", ReturnURL: "https://example.test/complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.CreateCheckoutSession(context.Background(), &CheckoutSessionRequest{
		PriceID: "price_1", UserID: "user_1", CustomerEmail: "buyer@example.test",
		Mode: CheckoutModePayment, TrialPeriodDays: 14,
	})
	if err != ErrPaymentProviderInvalidConfiguration {
		t.Fatalf("CreateCheckoutSession() error = %v, want %v", err, ErrPaymentProviderInvalidConfiguration)
	}
}

func TestStripeCheckoutRejectsTrialAboveStripeLimit(t *testing.T) {
	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test", APIKey: "sk_test", PublishableKey: "pk_test", ReturnURL: "https://example.test/complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.CreateCheckoutSession(context.Background(), &CheckoutSessionRequest{
		PriceID: "price_1", UserID: "user_1", CustomerEmail: "buyer@example.test",
		Mode: CheckoutModeSubscription, TrialPeriodDays: 731,
	})
	if err != ErrPaymentProviderInvalidConfiguration {
		t.Fatalf("CreateCheckoutSession() error = %v, want %v", err, ErrPaymentProviderInvalidConfiguration)
	}
}

func TestStripeEmbeddedCheckoutRejectsURLOnlyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cs_1","url":"https://checkout.stripe.com/session/test_1"}`)
	}))
	defer server.Close()
	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test", APIKey: "sk_test", PublishableKey: "pk_test",
		APIBaseURL: server.URL, ReturnURL: "https://example.test/complete", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.CreateCheckoutSession(context.Background(), &CheckoutSessionRequest{
		PriceID: "price_1", UserID: "user_1", CustomerEmail: "buyer@example.test", Mode: CheckoutModePayment,
	})
	if err != ErrPaymentProviderAPIResponseInvalid {
		t.Fatalf("CreateCheckoutSession() error = %v, want %v", err, ErrPaymentProviderAPIResponseInvalid)
	}
}

func TestStripeProviderUsesConfiguredAPIVersionForSubscriptionLookup(t *testing.T) {
	var apiVersionHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiVersionHeader = r.Header.Get("Stripe-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"sub_1","status":"active","items":{"data":[{"current_period_start":1700000000,"current_period_end":1701000000,"price":{"id":"price_1","unit_amount":1200,"currency":"gbp","recurring":{"interval":"month"}}}]}}`)
	}))
	defer server.Close()

	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test", APIKey: "sk_test", APIBaseURL: server.URL,
		APIVersion: "2026-04-29.dahlia", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := provider.GetSubscriptionInfo(context.Background(), "sub_1")
	if err != nil {
		t.Fatalf("GetSubscriptionInfo() error = %v", err)
	}
	if apiVersionHeader != "2026-04-29.dahlia" {
		t.Fatalf("Stripe-Version = %q", apiVersionHeader)
	}
	if info.CurrentPeriodStart != stripeUnixDate(1700000000) || info.CurrentPeriodEnd != stripeUnixDate(1701000000) || info.NextBillingDate != stripeUnixDate(1701000000) {
		t.Fatalf("subscription periods = %#v", info)
	}
}

func TestStripeProviderRejectsUnsafeAPIVersion(t *testing.T) {
	_, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test", APIVersion: "2026-03-25.dahlia\nX-Test: unsafe"})
	if err != ErrPaymentProviderInvalidConfiguration {
		t.Fatalf("NewStripeProvider() error = %v, want %v", err, ErrPaymentProviderInvalidConfiguration)
	}
}

func TestStripeCheckoutValidatesTrustedCataloguePrice(t *testing.T) {
	var checkoutRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/prices/price_monthly":
			_, _ = io.WriteString(w, `{"id":"price_monthly","active":true,"currency":"gbp","unit_amount":1200,"type":"recurring","recurring":{"interval":"month","interval_count":1}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/checkout/sessions":
			checkoutRequests++
			_, _ = io.WriteString(w, `{"id":"cs_1","client_secret":"secret_1"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test", APIKey: "sk_test", PublishableKey: "pk_test",
		APIBaseURL: server.URL, ReturnURL: "https://example.test/complete", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.CreateCheckoutSession(context.Background(), &CheckoutSessionRequest{
		PriceID: "price_monthly", UserID: "user_1", CustomerEmail: "buyer@example.test", Mode: CheckoutModeSubscription,
		ExpectedAmount: 1200, ExpectedCurrency: "GBP", ExpectedBillingCadence: "month",
	})
	if err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}
	if checkoutRequests != 1 {
		t.Fatalf("checkout requests = %d, want 1", checkoutRequests)
	}
}

func TestStripeCheckoutRejectsProviderPriceMismatch(t *testing.T) {
	var checkoutRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"id":"price_1","active":true,"currency":"gbp","unit_amount":1300,"type":"one_time"}`)
			return
		}
		checkoutRequests++
		_, _ = io.WriteString(w, `{"id":"cs_1","client_secret":"secret_1"}`)
	}))
	defer server.Close()

	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test", APIKey: "sk_test", PublishableKey: "pk_test",
		APIBaseURL: server.URL, ReturnURL: "https://example.test/complete", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.CreateCheckoutSession(context.Background(), &CheckoutSessionRequest{
		PriceID: "price_1", UserID: "user_1", CustomerEmail: "buyer@example.test", Mode: CheckoutModePayment,
		ExpectedAmount: 1200, ExpectedCurrency: "GBP", ExpectedBillingCadence: "one_time",
	})
	if err != ErrPaymentProviderPriceMismatch {
		t.Fatalf("CreateCheckoutSession() error = %v, want %v", err, ErrPaymentProviderPriceMismatch)
	}
	if checkoutRequests != 0 {
		t.Fatalf("mismatched price reached checkout creation %d times", checkoutRequests)
	}
}

func TestStripeCheckoutKeepsOneTimeInvoiceDataCompatibleWithManagedPayments(t *testing.T) {
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		received = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cs_1","client_secret":"secret_1"}`)
	}))
	defer server.Close()

	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test", APIKey: "sk_test", PublishableKey: "pk_test",
		APIBaseURL: server.URL, ReturnURL: "https://example.test/complete", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.CreateCheckoutSession(context.Background(), &CheckoutSessionRequest{
		PriceID: "price_1", PlanID: "plan_1", PlanSlug: "lifetime", PlanName: "Lifetime", CostID: "cost_1", UserID: "user_1",
		CustomerEmail: "buyer@example.test", Mode: CheckoutModePayment,
	})
	if err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}
	for key, want := range map[string]string{
		"invoice_creation[enabled]":                        "true",
		"metadata[plan_id]":                                "plan_1",
		"metadata[plan_slug]":                              "lifetime",
		"metadata[plan_name]":                              "Lifetime",
		"metadata[cost_id]":                                "cost_1",
		"metadata[provider_price_id]":                      "price_1",
		"metadata[user_reference]":                         "user_1",
		"metadata[billing_kind]":                           "one_time",
		"payment_intent_data[metadata][plan_id]":           "plan_1",
		"payment_intent_data[metadata][plan_slug]":         "lifetime",
		"payment_intent_data[metadata][plan_name]":         "Lifetime",
		"payment_intent_data[metadata][cost_id]":           "cost_1",
		"payment_intent_data[metadata][billing_kind]":      "one_time",
		"payment_intent_data[metadata][user_reference]":    "user_1",
		"payment_intent_data[metadata][provider_price_id]": "price_1",
	} {
		if got := received.Get(key); got != want {
			t.Errorf("form %s = %q, want %q", key, got, want)
		}
	}
	for key := range received {
		if strings.HasPrefix(key, "invoice_creation[invoice_data]") {
			t.Errorf("form contains Managed Payments-incompatible field %q", key)
		}
	}
}

func TestStripeCustomerPortalCapabilityCreatesHostedSession(t *testing.T) {
	var (
		received         url.Values
		authorization    string
		apiVersionHeader string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/billing_portal/sessions" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		received = r.PostForm
		authorization = r.Header.Get("Authorization")
		apiVersionHeader = r.Header.Get("Stripe-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"bps_1","url":"https://billing.stripe.com/p/session/test_1"}`)
	}))
	defer server.Close()

	provider, err := NewStripeProvider(&Config{
		WebhookSecret: "whsec_test", APIKey: "sk_test", APIBaseURL: server.URL, HTTPClient: server.Client(),
		CustomerPortalReturnURL: "https://example.test/settings#billing", CustomerPortalConfigurationID: "bpc_test_123",
	})
	if err != nil {
		t.Fatal(err)
	}
	var portal CustomerPortalProvider = provider
	session, err := portal.CreateCustomerPortalSession(context.Background(), &CustomerPortalSessionRequest{
		CustomerID: "cus_1", ReturnURL: "https://example.test/settings#billing",
	})
	if err != nil {
		t.Fatalf("CreateCustomerPortalSession() error = %v", err)
	}
	if session.ID != "bps_1" || session.URL != "https://billing.stripe.com/p/session/test_1" {
		t.Fatalf("session = %#v", session)
	}
	if received.Get("customer") != "cus_1" || received.Get("return_url") != "https://example.test/settings#billing" || received.Get("configuration") != "bpc_test_123" {
		t.Fatalf("form = %v", received)
	}
	if authorization != "Bearer sk_test" || apiVersionHeader != StripeDefaultAPIVersion {
		t.Fatalf("headers: Authorization=%q Stripe-Version=%q", authorization, apiVersionHeader)
	}
}

func TestStripeCustomerPortalConfigUsesReturnURLAsOptIn(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   error
	}{
		{name: "webhook only", config: &Config{WebhookSecret: "whsec_test"}},
		{name: "configuration ID without return URL", config: &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", CustomerPortalConfigurationID: "bpc_test_123"}, want: ErrPaymentProviderInvalidConfiguration},
		{name: "portal default configuration", config: &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", CustomerPortalReturnURL: "https://app.example.test/settings"}},
		{name: "portal return URL scheme is case insensitive", config: &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", CustomerPortalReturnURL: "HTTPS://APP.EXAMPLE.TEST/settings"}},
		{name: "portal named configuration", config: &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", CustomerPortalReturnURL: "https://app.example.test/settings", CustomerPortalConfigurationID: "bpc_test_123"}},
		{name: "missing API key", config: &Config{WebhookSecret: "whsec_test", CustomerPortalReturnURL: "https://app.example.test/settings"}, want: ErrPaymentProviderInvalidConfiguration},
		{name: "relative return URL", config: &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", CustomerPortalReturnURL: "/settings"}, want: ErrPaymentProviderInvalidConfiguration},
		{name: "invalid return URL port", config: &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", CustomerPortalReturnURL: "https://app.example.test:70000/settings"}, want: ErrPaymentProviderInvalidConfiguration},
		{name: "placeholder return URL", config: &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", CustomerPortalReturnURL: "https://app.example.test/{PORTAL_SESSION_ID}"}, want: ErrPaymentProviderInvalidConfiguration},
		{name: "invalid configuration ID", config: &Config{WebhookSecret: "whsec_test", APIKey: "sk_test", CustomerPortalReturnURL: "https://app.example.test/settings", CustomerPortalConfigurationID: "configuration_attacker"}, want: ErrPaymentProviderInvalidConfiguration},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, err := NewStripeProvider(test.config)
			if err != nil {
				t.Fatal(err)
			}
			if got := provider.GetCustomerPortalReturnURL(); got != strings.TrimSpace(test.config.CustomerPortalReturnURL) {
				t.Fatalf("return URL = %q", got)
			}
			err = ValidateCustomerPortalProviderConfig(provider)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	var nilProvider *StripeProvider
	if nilProvider.GetCustomerPortalReturnURL() != "" {
		t.Fatal("typed nil returned a portal URL")
	}
}

func TestStripeCustomerPortalCapabilityRejectsInvalidInput(t *testing.T) {
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test", APIKey: "sk_test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []*CustomerPortalSessionRequest{
		nil,
		{ReturnURL: "https://example.test/settings"},
		{CustomerID: "customer_not_stripe", ReturnURL: "https://example.test/settings"},
		{CustomerID: "cus_1", ReturnURL: "/settings"},
		{CustomerID: "cus_1", ReturnURL: "https://user@example.test/settings"},
		{CustomerID: "cus_1", ReturnURL: "https://example.test:70000/settings"},
		{CustomerID: "cus_1", ReturnURL: "https://example.test/{PORTAL_SESSION_ID}"},
		{CustomerID: "cus_1", ReturnURL: "javascript:alert(1)"},
	} {
		if _, err := provider.CreateCustomerPortalSession(context.Background(), input); err != ErrPaymentProviderInvalidConfiguration {
			t.Errorf("CreateCustomerPortalSession(%#v) error = %v, want %v", input, err, ErrPaymentProviderInvalidConfiguration)
		}
	}

	provider, err = NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.CreateCustomerPortalSession(context.Background(), &CustomerPortalSessionRequest{
		CustomerID: "cus_1", ReturnURL: "https://example.test/settings",
	}); err != ErrPaymentProviderInvalidConfiguration {
		t.Fatalf("missing API key error = %v, want %v", err, ErrPaymentProviderInvalidConfiguration)
	}
}

func TestStripeCustomerPortalCapabilityRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{name: "provider error", status: http.StatusBadGateway, body: `{}`, want: ErrPaymentProviderAPIRequestFailed},
		{name: "missing ID", status: http.StatusOK, body: `{"url":"https://billing.stripe.com/p/session/test_1"}`, want: ErrPaymentProviderAPIResponseInvalid},
		{name: "insecure URL", status: http.StatusOK, body: `{"id":"bps_1","url":"http://billing.stripe.com/p/session/test_1"}`, want: ErrPaymentProviderAPIResponseInvalid},
		{name: "userinfo URL", status: http.StatusOK, body: `{"id":"bps_1","url":"https://user@billing.stripe.com/p/session/test_1"}`, want: ErrPaymentProviderAPIResponseInvalid},
		{name: "untrusted host", status: http.StatusOK, body: `{"id":"bps_1","url":"https://billing.stripe.com.example.test/p/session/test_1"}`, want: ErrPaymentProviderAPIResponseInvalid},
		{name: "unexpected port", status: http.StatusOK, body: `{"id":"bps_1","url":"https://billing.stripe.com:444/p/session/test_1"}`, want: ErrPaymentProviderAPIResponseInvalid},
		{name: "malformed JSON", status: http.StatusOK, body: `{`, want: ErrPaymentProviderAPIResponseInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			provider, err := NewStripeProvider(&Config{
				WebhookSecret: "whsec_test", APIKey: "sk_test", APIBaseURL: server.URL, HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.CreateCustomerPortalSession(context.Background(), &CustomerPortalSessionRequest{
				CustomerID: "cus_1", ReturnURL: "https://example.test/settings",
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func stripeTestSignature(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(fmt.Sprintf("%d.%s", timestamp, body)))
	return fmt.Sprintf("t=%d,v1=%s,v1=%s", timestamp, strings.Repeat("0", 64), hex.EncodeToString(mac.Sum(nil)))
}
