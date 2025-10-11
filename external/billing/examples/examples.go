package examples

import (
	"context"
	"fmt"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
)

// Example1_CreateSubscription demonstrates creating a new subscription
func Example1_CreateSubscription() {
	service := setupService()

	ctx := context.Background()
	nextBilling := time.Now().AddDate(0, 1, 0) // 1 month from now

	req := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: "stripe_sub_123456",
		Integrator:               "stripe",
		IntegratorCustomerID:     "cus_123",
		UserID:                   "user-123",
		Email:                    "USER@EXAMPLE.COM", // Will be standardized to lowercase
		PlanName:                 "Pro Plan",
		Status:                   billing.StatusActive,
		BillingInterval:          "monthly",
		Amount:                   2999, // $29.99 in cents
		Currency:                 "USD",
		NextBillingDate:          &nextBilling,
		CancelURL:                "https://billing.stripe.com/p/cancel/123",
		UpdateURL:                "https://billing.stripe.com/p/update/123",
	}

	resp, err := service.CreateSubscription(ctx, req)
	if err != nil {
		fmt.Println("Error creating subscription:", err)
		return
	}

	fmt.Printf("Created subscription ID: %s\n", resp.Subscription.ID)
	fmt.Printf("Integrator Subscription ID: %s\n", resp.Subscription.IntegratorSubscriptionID)
	fmt.Printf("Status: %s\n", resp.Subscription.Status)
	fmt.Printf("Email (standardized): %s\n", resp.Subscription.Email)
}

// Example2_UpdateSubscription demonstrates updating an existing subscription
func Example2_UpdateSubscription() {
	service := setupService()

	// First create a subscription
	ctx := context.Background()
	createReq := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: "stripe_sub_789",
		Integrator:               "stripe",
		UserID:                   "user-456",
		Email:                    "update@example.com",
		PlanName:                 "Basic Plan",
		Status:                   billing.StatusActive,
		BillingInterval:          "monthly",
		Amount:                   1999,
		Currency:                 "USD",
	}

	createResp, _ := service.CreateSubscription(ctx, createReq)

	// Now update the subscription status
	newStatus := billing.StatusPastDue
	newAmount := int64(2499)

	updateReq := &billing.UpdateSubscriptionRequest{
		ID:     createResp.Subscription.ID,
		Status: &newStatus,
		Amount: &newAmount, // Increase amount to $24.99
	}

	updateResp, err := service.UpdateSubscription(ctx, updateReq)
	if err != nil {
		fmt.Println("Error updating subscription:", err)
		return
	}

	fmt.Printf("Updated subscription ID: %s\n", updateResp.Subscription.ID)
	fmt.Printf("New status: %s\n", updateResp.Subscription.Status)
	fmt.Printf("New amount: $%.2f\n", float64(updateResp.Subscription.Amount)/100)
}

// Example3_GetSubscriptionsByUser demonstrates querying subscriptions with filters
func Example3_GetSubscriptionsByUser() {
	service := setupService()

	ctx := context.Background()

	// Create some test subscriptions
	createSubscription(service, ctx, "user-123", "user1@example.com", billing.StatusActive, "stripe")
	createSubscription(service, ctx, "user-123", "user1@example.com", billing.StatusCancelled, "lemonsqueezy")
	createSubscription(service, ctx, "user-456", "user2@example.com", billing.StatusActive, "stripe")

	// Query subscriptions for specific user
	req := &billing.GetSubscriptionsRequest{
		ForUserIDs: []string{"user-123"},
		PerPage:    10,
		Page:       1,
		Order:      "created_at_desc",
	}

	resp, err := service.GetSubscriptions(ctx, req)
	if err != nil {
		fmt.Println("Error getting subscriptions:", err)
		return
	}

	fmt.Printf("Found %d subscriptions for user-123:\n", resp.Total)
	for _, sub := range resp.Subscriptions {
		fmt.Printf("  - ID: %s, Status: %s, Provider: %s\n",
			sub.ID, sub.Status, sub.Integrator)
	}

	// Get metadata
	meta := resp.GetMetaData()
	fmt.Printf("Pagination: Page %d of %d\n", meta["page"], meta["pages"])
}

// Example4_GetSubscriptionByIntegratorID demonstrates retrieving by external ID
func Example4_GetSubscriptionByIntegratorID() {
	service := setupService()

	ctx := context.Background()

	// Create subscription with specific integrator ID
	createReq := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: "stripe_sub_unique_12345",
		Integrator:               "stripe",
		UserID:                   "user-789",
		Email:                    "integrator@example.com",
		PlanName:                 "Premium Plan",
		Status:                   billing.StatusActive,
		BillingInterval:          "yearly",
		Amount:                   29999,
		Currency:                 "USD",
	}
	service.CreateSubscription(ctx, createReq)

	// Retrieve by integrator ID
	req := &billing.GetSubscriptionByIntegratorIDRequest{
		IntegratorName:           "stripe",
		IntegratorSubscriptionID: "stripe_sub_unique_12345",
	}

	resp, err := service.GetSubscriptionByIntegratorID(ctx, req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Found subscription: %s\n", resp.Subscription.ID)
	fmt.Printf("User ID: %s\n", resp.Subscription.UserID)
	fmt.Printf("Plan: %s\n", resp.Subscription.PlanName)
	fmt.Printf("Amount: $%.2f/%s\n",
		float64(resp.Subscription.Amount)/100,
		resp.Subscription.BillingInterval)
}

// Example5_CancelSubscription demonstrates canceling a subscription
func Example5_CancelSubscription() {
	service := setupService()

	ctx := context.Background()

	// Create an active subscription
	createReq := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: "stripe_sub_to_cancel",
		Integrator:               "stripe",
		UserID:                   "user-cancel-test",
		Email:                    "cancel@example.com",
		PlanName:                 "Pro Plan",
		Status:                   billing.StatusActive,
		BillingInterval:          "monthly",
		Amount:                   4999,
		Currency:                 "USD",
	}

	createResp, _ := service.CreateSubscription(ctx, createReq)

	// Cancel the subscription
	cancelledAt := time.Now()
	cancelReq := &billing.CancelSubscriptionRequest{
		ID:          createResp.Subscription.ID,
		CancelledAt: &cancelledAt,
		Status:      billing.StatusCancelled,
	}

	cancelResp, err := service.CancelSubscription(ctx, cancelReq)
	if err != nil {
		fmt.Println("Error canceling subscription:", err)
		return
	}

	fmt.Printf("Cancelled subscription ID: %s\n", cancelResp.Subscription.ID)
	fmt.Printf("New status: %s\n", cancelResp.Subscription.Status)
	if cancelResp.Subscription.ProviderCancelledAt != nil {
		fmt.Printf("Cancelled at: %s\n", cancelResp.Subscription.ProviderCancelledAt)
	}
}

// Example6_CreateBillingEvent demonstrates logging a billing event
func Example6_CreateBillingEvent() {
	service := setupService()

	ctx := context.Background()

	req := &billing.CreateBillingEventRequest{
		SubscriptionID:           "sub_123",
		IntegratorEventID:        "evt_stripe_456",
		IntegratorSubscriptionID: "stripe_sub_123",
		Integrator:               "stripe",
		UserID:                   "user-123",
		EventType:                "payment.succeeded",
		EventTime:                time.Now(),
		Amount:                   2999,
		Currency:                 "USD",
		PlanName:                 "Pro Plan",
		Status:                   billing.StatusActive,
		ReceiptURL:               "https://receipt.stripe.com/r/xyz789",
		RawPayload:               `{"id":"evt_123","type":"payment_intent.succeeded"}`,
	}

	resp, err := service.CreateBillingEvent(ctx, req)
	if err != nil {
		fmt.Println("Error creating billing event:", err)
		return
	}

	fmt.Printf("Created billing event ID: %s\n", resp.BillingEvent.ID)
	fmt.Printf("Event type: %s\n", resp.BillingEvent.EventType)
	fmt.Printf("Amount: $%.2f %s\n", float64(resp.BillingEvent.Amount)/100, resp.BillingEvent.Currency)
}

// Example7_GetBillingEvents demonstrates querying billing events with filters
func Example7_GetBillingEvents() {
	service := setupService()

	ctx := context.Background()

	// Create some billing events
	for i := 0; i < 5; i++ {
		req := &billing.CreateBillingEventRequest{
			SubscriptionID:           fmt.Sprintf("sub_%d", i),
			IntegratorEventID:        fmt.Sprintf("evt_%d", i),
			IntegratorSubscriptionID: fmt.Sprintf("stripe_sub_%d", i),
			Integrator:               "stripe",
			UserID:                   "user-123",
			EventType:                "payment.succeeded",
			EventTime:                time.Now(),
			Amount:                   1999,
			Currency:                 "USD",
			PlanName:                 "Test Plan",
			Status:                   billing.StatusActive,
			RawPayload:               "{}",
		}
		service.CreateBillingEvent(ctx, req)
	}

	// Query events
	req := &billing.GetBillingEventsRequest{
		ForUserIDs: []string{"user-123"},
		PerPage:    3,
		Page:       1,
		Order:      "created_at_desc",
	}

	resp, err := service.GetBillingEvents(ctx, req)
	if err != nil {
		fmt.Println("Error getting billing events:", err)
		return
	}

	fmt.Printf("Found %d total events (showing %d):\n", resp.Total, len(resp.BillingEvents))
	for _, event := range resp.BillingEvents {
		fmt.Printf("  - [%s] %s - $%.2f\n",
			event.EventType,
			event.PlanName,
			float64(event.Amount)/100)
	}

	// Get metadata
	meta := resp.GetMetaData()
	fmt.Printf("Page %d of %d\n", meta["page"], meta["pages"])
}

// Example8_FilterSubscriptionsByStatus demonstrates complex filtering
func Example8_FilterSubscriptionsByStatus() {
	service := setupService()

	ctx := context.Background()

	// Create subscriptions with different statuses
	createSubscription(service, ctx, "user-filter", "filter@example.com", billing.StatusActive, "stripe")
	createSubscription(service, ctx, "user-filter", "filter@example.com", billing.StatusTrialing, "stripe")
	createSubscription(service, ctx, "user-filter", "filter@example.com", billing.StatusCancelled, "lemonsqueezy")
	createSubscription(service, ctx, "user-filter", "filter@example.com", billing.StatusPastDue, "stripe")

	// Get only active and trialing subscriptions
	req := &billing.GetSubscriptionsRequest{
		ForUserIDs: []string{"user-filter"},
		Statuses:   []string{billing.StatusActive, billing.StatusTrialing},
		PerPage:    10,
		Page:       1,
	}

	resp, err := service.GetSubscriptions(ctx, req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Active/Trialing subscriptions: %d\n", resp.Total)
	for _, sub := range resp.Subscriptions {
		fmt.Printf("  - Status: %s, Plan: %s\n", sub.Status, sub.PlanName)
	}
}

// Example9_FilterByProviderAndCurrency demonstrates multi-field filtering
func Example9_FilterByProviderAndCurrency() {
	service := setupService()

	ctx := context.Background()

	// Create subscriptions with different providers and currencies
	createSubscriptionWithDetails(service, ctx, "user-multi", "stripe", "USD", 2999)
	createSubscriptionWithDetails(service, ctx, "user-multi", "stripe", "EUR", 2499)
	createSubscriptionWithDetails(service, ctx, "user-multi", "lemonsqueezy", "USD", 1999)
	createSubscriptionWithDetails(service, ctx, "user-multi", "kofi", "USD", 999)

	// Filter by provider and currency
	req := &billing.GetSubscriptionsRequest{
		ForUserIDs:     []string{"user-multi"},
		IntegratorName: "stripe",
		Currency:       []string{"USD"},
		PerPage:        10,
		Page:           1,
	}

	resp, err := service.GetSubscriptions(ctx, req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Stripe USD subscriptions: %d\n", resp.Total)
	for _, sub := range resp.Subscriptions {
		fmt.Printf("  - Provider: %s, Currency: %s, Amount: $%.2f\n",
			sub.Integrator, sub.Currency, float64(sub.Amount)/100)
	}
}

// Example10_DeleteSubscription demonstrates permanent deletion
func Example10_DeleteSubscription() {
	service := setupService()

	ctx := context.Background()

	// Create a subscription
	createReq := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: "stripe_sub_to_delete",
		Integrator:               "stripe",
		UserID:                   "user-delete-test",
		Email:                    "delete@example.com",
		PlanName:                 "Test Plan",
		Status:                   billing.StatusActive,
		BillingInterval:          "monthly",
		Amount:                   999,
		Currency:                 "USD",
	}

	createResp, _ := service.CreateSubscription(ctx, createReq)
	subscriptionID := createResp.Subscription.ID

	fmt.Printf("Created subscription: %s\n", subscriptionID)

	// Delete the subscription
	deleteReq := &billing.DeleteSubscriptionRequest{
		ID: subscriptionID,
	}

	_, err := service.DeleteSubscription(ctx, deleteReq)
	if err != nil {
		fmt.Println("Error deleting subscription:", err)
		return
	}

	fmt.Println("Subscription deleted successfully")

	// Try to retrieve it (should fail)
	getReq := &billing.GetSubscriptionByIDRequest{
		ID: subscriptionID,
	}

	_, err = service.GetSubscriptionByID(ctx, getReq)
	if err != nil {
		fmt.Printf("Confirmed deletion - subscription not found: %v\n", err)
	}
}

// Example11_FilterBillingEventsByType demonstrates event filtering
func Example11_FilterBillingEventsByType() {
	service := setupService()

	ctx := context.Background()

	// Create different event types
	eventTypes := []string{
		"payment.succeeded",
		"payment.failed",
		"subscription.created",
		"subscription.updated",
	}

	for _, eventType := range eventTypes {
		req := &billing.CreateBillingEventRequest{
			SubscriptionID:           "sub_filter_test",
			IntegratorEventID:        fmt.Sprintf("evt_%s", eventType),
			IntegratorSubscriptionID: "stripe_sub_filter",
			Integrator:               "stripe",
			UserID:                   "user-event-filter",
			EventType:                eventType,
			EventTime:                time.Now(),
			Amount:                   2999,
			Currency:                 "USD",
			PlanName:                 "Filter Test Plan",
			Status:                   billing.StatusActive,
			RawPayload:               "{}",
		}
		service.CreateBillingEvent(ctx, req)
	}

	// Get only payment events
	req := &billing.GetBillingEventsRequest{
		ForUserIDs: []string{"user-event-filter"},
		EventTypes: []string{"payment.succeeded", "payment.failed"},
		PerPage:    10,
		Page:       1,
	}

	resp, err := service.GetBillingEvents(ctx, req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Payment events found: %d\n", resp.Total)
	for _, event := range resp.BillingEvents {
		fmt.Printf("  - %s: %s\n", event.EventType, event.PlanName)
	}
}

// Example12_EmailStandardization demonstrates email normalization
func Example12_EmailStandardization() {
	service := setupService()

	ctx := context.Background()

	// Create subscriptions with various email formats
	emails := []string{
		"USER@EXAMPLE.COM",
		"Test@Example.Com",
		"another@EXAMPLE.com",
	}

	for i, email := range emails {
		req := &billing.CreateSubscriptionRequest{
			IntegratorSubscriptionID: fmt.Sprintf("sub_email_%d", i),
			Integrator:               "stripe",
			UserID:                   fmt.Sprintf("user-%d", i),
			Email:                    email,
			PlanName:                 "Email Test Plan",
			Status:                   billing.StatusActive,
			BillingInterval:          "monthly",
			Amount:                   1999,
			Currency:                 "USD",
		}

		resp, _ := service.CreateSubscription(ctx, req)
		fmt.Printf("Input: %s -> Standardized: %s\n",
			email, resp.Subscription.Email)
	}

	// Query by standardized email
	req := &billing.GetSubscriptionsRequest{
		ForEmails: []string{"user@example.com"}, // lowercase
		PerPage:   10,
		Page:      1,
	}

	resp, _ := service.GetSubscriptions(ctx, req)
	fmt.Printf("\nFound %d subscriptions with standardized email\n", resp.Total)
}

// Example13_SubscriptionUtilityMethods demonstrates using Subscription helper methods
func Example13_SubscriptionUtilityMethods() {
	service := setupService()

	ctx := context.Background()

	// Create subscriptions with different statuses
	nextBilling := time.Now().AddDate(0, 0, 10) // 10 days from now
	createReq := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: "stripe_sub_utils",
		Integrator:               "stripe",
		UserID:                   "user-utils",
		Email:                    "utils@example.com",
		PlanName:                 "Utility Test Plan",
		Status:                   billing.StatusActive,
		BillingInterval:          "monthly",
		Amount:                   3999,
		Currency:                 "USD",
		NextBillingDate:          &nextBilling,
	}

	resp, _ := service.CreateSubscription(ctx, createReq)
	sub := resp.Subscription

	// Use utility methods
	fmt.Printf("Subscription ID: %s\n", sub.ID)
	fmt.Printf("Is Active: %v\n", sub.IsActive())
	fmt.Printf("Is Cancelled: %v\n", sub.IsCancelled())
	fmt.Printf("Is In Good Standing: %v\n", sub.IsInGoodStanding())
	fmt.Printf("Days Until Next Billing: %d\n", sub.DaysUntilNextBilling())
}

// Example14_BillingEventUtilityMethods demonstrates using BillingEvent helper methods
func Example14_BillingEventUtilityMethods() {
	service := setupService()

	ctx := context.Background()

	// Create different types of events
	paymentEvent := &billing.CreateBillingEventRequest{
		SubscriptionID:           "sub_test",
		IntegratorEventID:        "evt_payment",
		IntegratorSubscriptionID: "stripe_sub_test",
		Integrator:               "stripe",
		UserID:                   "user-test",
		EventType:                "payment.succeeded",
		EventTime:                time.Now(),
		Amount:                   2999,
		Currency:                 "USD",
		PlanName:                 "Test Plan",
		Status:                   billing.StatusActive,
		RawPayload:               "{}",
	}

	paymentResp, _ := service.CreateBillingEvent(ctx, paymentEvent)

	subscriptionEvent := &billing.CreateBillingEventRequest{
		SubscriptionID:           "sub_test",
		IntegratorEventID:        "evt_subscription",
		IntegratorSubscriptionID: "stripe_sub_test",
		Integrator:               "stripe",
		UserID:                   "user-test",
		EventType:                "subscription.created",
		EventTime:                time.Now(),
		Amount:                   2999,
		Currency:                 "USD",
		PlanName:                 "Test Plan",
		Status:                   billing.StatusActive,
		RawPayload:               "{}",
	}

	subResp, _ := service.CreateBillingEvent(ctx, subscriptionEvent)

	fmt.Printf("Payment Event - Is Payment Event: %v, Is Subscription Event: %v\n",
		paymentResp.BillingEvent.IsPaymentEvent(),
		paymentResp.BillingEvent.IsSubscriptionEvent())

	fmt.Printf("Subscription Event - Is Payment Event: %v, Is Subscription Event: %v\n",
		subResp.BillingEvent.IsPaymentEvent(),
		subResp.BillingEvent.IsSubscriptionEvent())
}

// Helper functions

func setupService() *billing.Service {

	// Create in-memory repository
	baseRepo := billing.NewInMemoryRepository(nil)

	// Create service
	return billing.NewService(baseRepo, baseRepo)
}

func createSubscription(service *billing.Service, ctx context.Context, userID, email, status, provider string) *billing.Subscription {
	req := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: fmt.Sprintf("%s_%s_%s", provider, userID, status),
		Integrator:               provider,
		UserID:                   userID,
		Email:                    email,
		PlanName:                 "Test Plan",
		Status:                   status,
		BillingInterval:          "monthly",
		Amount:                   2999,
		Currency:                 "USD",
	}

	resp, _ := service.CreateSubscription(ctx, req)
	return resp.Subscription
}

func createSubscriptionWithDetails(service *billing.Service, ctx context.Context, userID, provider, currency string, amount int64) *billing.Subscription {
	req := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: fmt.Sprintf("%s_%s_%s_%d", provider, userID, currency, amount),
		Integrator:               provider,
		UserID:                   userID,
		Email:                    fmt.Sprintf("%s@example.com", userID),
		PlanName:                 fmt.Sprintf("%s Plan", provider),
		Status:                   billing.StatusActive,
		BillingInterval:          "monthly",
		Amount:                   amount,
		Currency:                 currency,
	}

	resp, _ := service.CreateSubscription(ctx, req)
	return resp.Subscription
}
