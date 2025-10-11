package examples

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// Example1_BasicStripeWebhook demonstrates basic Stripe webhook processing
func Example1_BasicStripeWebhook() {
	// Configure Stripe provider
	config := &paymentprovider.Config{
		ProviderName:  "stripe",
		WebhookSecret: "whsec_your_stripe_webhook_secret",
		APIKey:        "sk_test_your_stripe_api_key",
	}

	provider, err := paymentprovider.NewStripeProvider(config)
	if err != nil {
		panic(err)
	}

	// Create a mock webhook request
	// In production, this would be the actual webhook payload from Stripe
	webhookData := `{"id":"evt_123","type":"customer.subscription.created","data":{"object":{"id":"sub_123","customer":"cus_123","status":"active"}}}`
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(webhookData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", "t=1234567890,v1=signature_here")

	ctx := context.Background()

	// Verify the webhook (in production, this checks the signature)
	err = provider.VerifyWebhook(ctx, req)
	if err != nil {
		fmt.Println("Webhook verification failed:", err)
		return
	}

	// Parse the payload
	payload, err := provider.ParsePayload(ctx, req)
	if err != nil {
		fmt.Println("Payload parsing failed:", err)
		return
	}

	fmt.Printf("Event Type: %s\n", payload.EventType)
	fmt.Printf("Subscription ID: %s\n", payload.SubscriptionID)
	fmt.Printf("Status: %s\n", payload.Status)
}

// Example2_MultiProviderRegistry demonstrates managing multiple providers
func Example2_MultiProviderRegistry() {
	// Create provider registry
	registry := paymentprovider.NewProviderRegistry()

	// Add Stripe
	stripeConfig := &paymentprovider.Config{
		ProviderName:  "stripe",
		WebhookSecret: "whsec_your_stripe_secret",
		APIKey:        "sk_test_your_stripe_key",
	}
	stripeProvider, _ := paymentprovider.NewStripeProvider(stripeConfig)
	registry.Register(stripeProvider)

	// Add Lemon Squeezy
	lemonSqueezyConfig := &paymentprovider.Config{
		ProviderName:  "lemonsqueezy",
		WebhookSecret: "your_lemonsqueezy_webhook_secret",
		APIKey:        "your_lemonsqueezy_api_key",
	}
	lemonSqueezyProvider, _ := paymentprovider.NewLemonSqueezyProvider(lemonSqueezyConfig)
	registry.Register(lemonSqueezyProvider)

	// Add Ko-fi
	kofiConfig := &paymentprovider.Config{
		ProviderName:  "kofi",
		WebhookSecret: "your_kofi_verification_token",
	}
	kofiProvider, _ := paymentprovider.NewKofiProvider(kofiConfig)
	registry.Register(kofiProvider)

	// List all providers
	fmt.Println("Registered providers:")
	for _, name := range registry.List() {
		fmt.Printf("  - %s\n", name)
	}

	// Process webhook based on provider
	providerName := "stripe" // Extract from URL path
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(`{}`))
	req.Header.Set("Stripe-Signature", "t=1234567890,v1=test")

	payload, err := registry.VerifyAndParseWebhookPayload(context.Background(), providerName, req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Processed %s webhook\n", payload.EventType)
}

// Example3_MockProvider demonstrates testing with mock provider
func Example3_MockProvider() {
	// Create a mock provider for testing
	mock := paymentprovider.NewMockProvider("stripe")

	// Set up test data
	mock.SetMockPayload(&paymentprovider.WebhookPayload{
		EventType:      paymentprovider.EventTypePaymentSucceeded,
		EventID:        "evt_test_123",
		SubscriptionID: "sub_test_123",
		CustomerEmail:  "test@example.com",
		Status:         paymentprovider.SubscriptionStatusActive,
		PlanName:       "Pro Plan",
		Amount:         2999,
		Currency:       "USD",
	})

	// Use in tests
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader("test data"))
	ctx := context.Background()

	err := mock.VerifyWebhook(ctx, req)
	fmt.Printf("Verification: %v\n", err)

	payload, _ := mock.ParsePayload(ctx, req)
	fmt.Printf("Plan: %s, Amount: $%.2f\n", payload.PlanName, float64(payload.Amount)/100)
}

// Example4_LemonSqueezyWebhook demonstrates Lemon Squeezy webhook processing
func Example4_LemonSqueezyWebhook() {
	// Configure Lemon Squeezy provider
	config := &paymentprovider.Config{
		ProviderName:  "lemonsqueezy",
		WebhookSecret: "your_lemonsqueezy_webhook_secret",
		APIKey:        "your_lemonsqueezy_api_key",
	}

	provider, err := paymentprovider.NewLemonSqueezyProvider(config)
	if err != nil {
		panic(err)
	}

	// Create a mock webhook request
	webhookData := `{"meta":{"event_name":"subscription_created"},"data":{"type":"subscriptions","id":"123","attributes":{"store_id":1,"customer_id":1,"order_id":1,"order_item_id":1,"product_id":1,"variant_id":1,"product_name":"Pro Plan","variant_name":"Monthly","user_email":"user@example.com","status":"active","status_formatted":"Active","card_brand":"visa","card_last_four":"4242","pause":null,"cancelled":false,"trial_ends_at":null,"billing_anchor":1,"first_subscription_item":{"id":1,"subscription_id":1,"price_id":1,"quantity":1},"urls":{"update_payment_method":"https://example.com/update","customer_portal":"https://example.com/portal"},"renews_at":"2024-02-01T00:00:00.000000Z","ends_at":null,"created_at":"2024-01-01T00:00:00.000000Z","updated_at":"2024-01-01T00:00:00.000000Z"}}}`
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(webhookData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", "signature_here")

	ctx := context.Background()

	// Verify the webhook
	err = provider.VerifyWebhook(ctx, req)
	if err != nil {
		fmt.Println("Webhook verification failed:", err)
		return
	}

	// Parse the payload
	payload, err := provider.ParsePayload(ctx, req)
	if err != nil {
		fmt.Println("Payload parsing failed:", err)
		return
	}

	fmt.Printf("Event Type: %s\n", payload.EventType)
	fmt.Printf("Customer Email: %s\n", payload.CustomerEmail)
	fmt.Printf("Status: %s\n", payload.Status)
}

// Example5_KofiWebhook demonstrates Ko-fi webhook processing
func Example5_KofiWebhook() {
	// Configure Ko-fi provider
	config := &paymentprovider.Config{
		ProviderName:  "kofi",
		WebhookSecret: "your_kofi_verification_token",
	}

	provider, err := paymentprovider.NewKofiProvider(config)
	if err != nil {
		panic(err)
	}

	// Create a mock webhook request (Ko-fi sends form-encoded data)
	kofiPayload := `{"verification_token":"your_kofi_verification_token","message_id":"12345","timestamp":"2024-01-01T00:00:00Z","type":"Subscription","is_public":true,"from_name":"Supporter Name","message":"Thanks!","amount":"5.00","url":"https://ko-fi.com/Home/CoffeeShop?txid=12345","email":"supporter@example.com","currency":"USD","is_subscription_payment":true,"is_first_subscription_payment":true,"kofi_transaction_id":"12345","tier_name":"Gold Tier"}`
	formData := fmt.Sprintf("data=%s", kofiPayload)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(formData))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	ctx := context.Background()

	// Verify the webhook (Ko-fi uses verification token in payload)
	err = provider.VerifyWebhook(ctx, req)
	if err != nil {
		fmt.Println("Webhook verification failed:", err)
		return
	}

	// Parse the payload
	payload, err := provider.ParsePayload(ctx, req)
	if err != nil {
		fmt.Println("Payload parsing failed:", err)
		return
	}

	fmt.Printf("Event Type: %s\n", payload.EventType)
	fmt.Printf("Customer Email: %s\n", payload.CustomerEmail)
	fmt.Printf("Amount: $%.2f %s\n", float64(payload.Amount)/100, payload.Currency)
}

// Example6_CreateRegistryFromConfigs demonstrates creating a registry from configuration
func Example6_CreateRegistryFromConfigs() {
	// Define configurations for multiple providers
	configs := []*paymentprovider.Config{
		{
			ProviderName:  "stripe",
			WebhookSecret: "whsec_stripe_secret",
			APIKey:        "sk_test_stripe_key",
		},
		{
			ProviderName:  "lemonsqueezy",
			WebhookSecret: "lemonsqueezy_webhook_secret",
			APIKey:        "lemonsqueezy_api_key",
		},
		{
			ProviderName:  "kofi",
			WebhookSecret: "kofi_verification_token",
		},
	}

	// Create registry from configs
	registry, err := paymentprovider.CreateRegistryFromConfigs(configs)
	if err != nil {
		fmt.Println("Failed to create registry:", err)
		return
	}

	// Verify all providers are registered
	fmt.Println("Successfully registered providers:")
	for _, name := range registry.List() {
		fmt.Printf("  - %s\n", name)
	}

	// Check if specific provider exists
	if registry.Has("stripe") {
		fmt.Println("Stripe provider is ready to use")
	}
}

// Example7_WebhookPayloadToJSON demonstrates converting webhook payload to JSON
func Example7_WebhookPayloadToJSON() {
	// Create a sample webhook payload
	payload := &paymentprovider.WebhookPayload{
		EventType:       paymentprovider.EventTypeSubscriptionCreated,
		EventID:         "evt_123456",
		EventTime:       "2024-01-01T00:00:00Z",
		SubscriptionID:  "sub_123456",
		CustomerID:      "cus_123456",
		CustomerEmail:   "customer@example.com",
		Status:          paymentprovider.SubscriptionStatusActive,
		PlanName:        "Premium Plan",
		Amount:          4999,
		Currency:        "USD",
		NextBillingDate: "2024-02-01T00:00:00Z",
		CancelURL:       "https://example.com/cancel",
		UpdateURL:       "https://example.com/update",
	}

	// Convert to JSON string
	jsonStr, err := paymentprovider.WebhookPayloadToJSON(payload)
	if err != nil {
		fmt.Println("Failed to convert to JSON:", err)
		return
	}

	fmt.Println("Webhook Payload JSON:")
	fmt.Println(jsonStr)
}
