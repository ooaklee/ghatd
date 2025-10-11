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
		Email:                    "USER@EXAMPLE.COM", // Will be standardised to lowercase
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
	fmt.Printf("Email (standardised): %s\n", resp.Subscription.Email)
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

// Example12_EmailStandardisation demonstrates email normalisation
func Example12_EmailStandardisation() {
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
		fmt.Printf("Input: %s -> Standardised: %s\n",
			email, resp.Subscription.Email)
	}

	// Query by standardised email
	req := &billing.GetSubscriptionsRequest{
		ForEmails: []string{"user@example.com"}, // lowercase
		PerPage:   10,
		Page:      1,
	}

	resp, _ := service.GetSubscriptions(ctx, req)
	fmt.Printf("\nFound %d subscriptions with standardised email\n", resp.Total)
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

// Example15_GetSubscriptionsByEmail demonstrates retrieving subscriptions by email
func Example15_GetSubscriptionsByEmail() {
	service := setupService()

	ctx := context.Background()

	// Create multiple subscriptions for the same email
	email := "multi@example.com"
	createSubscription(service, ctx, "user-001", email, billing.StatusActive, "stripe")
	createSubscription(service, ctx, "user-001", email, billing.StatusTrialing, "lemonsqueezy")
	createSubscription(service, ctx, "", email, billing.StatusActive, "kofi") // Pre-registration purchase

	// Query by email
	req := &billing.GetSubscriptionsByEmailRequest{
		Email: "MULTI@Example.COM", // Will be standardised to lowercase
	}

	resp, err := service.GetSubscriptionsByEmail(ctx, req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Found %d subscriptions for %s:\n", resp.Total, email)
	for _, sub := range resp.Subscriptions {
		userDisplay := sub.UserID
		if userDisplay == "" {
			userDisplay = "[pending user association]"
		}
		fmt.Printf("  - Provider: %s, Status: %s, User: %s\n",
			sub.Integrator, sub.Status, userDisplay)
	}
}

// Example16_GetBillingEventsByEmail demonstrates retrieving billing events by email
func Example16_GetBillingEventsByEmail() {
	service := setupService()

	ctx := context.Background()

	email := "events@example.com"

	// Create a subscription
	createReq := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: "stripe_sub_events_test",
		Integrator:               "stripe",
		UserID:                   "user-events",
		Email:                    email,
		PlanName:                 "Pro Plan",
		Status:                   billing.StatusActive,
		BillingInterval:          "monthly",
		Amount:                   2999,
		Currency:                 "USD",
	}
	service.CreateSubscription(ctx, createReq)

	// Create billing events for this subscription
	eventTypes := []string{"payment.succeeded", "invoice.created", "payment.succeeded"}
	for i, eventType := range eventTypes {
		eventReq := &billing.CreateBillingEventRequest{
			SubscriptionID:           "sub_events_test",
			IntegratorEventID:        fmt.Sprintf("evt_email_%d", i),
			IntegratorSubscriptionID: "stripe_sub_events_test",
			Integrator:               "stripe",
			UserID:                   "user-events",
			EventType:                eventType,
			EventTime:                time.Now(),
			Amount:                   2999,
			Currency:                 "USD",
			PlanName:                 "Pro Plan",
			Status:                   billing.StatusActive,
			RawPayload:               "{}",
		}
		service.CreateBillingEvent(ctx, eventReq)
	}

	// Query events by email
	req := &billing.GetBillingEventsByEmailRequest{
		Email: email,
	}

	resp, err := service.GetBillingEventsByEmail(ctx, req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Found %d billing events for %s:\n", resp.Total, email)
	for _, event := range resp.BillingEvents {
		fmt.Printf("  - [%s] %s - $%.2f\n",
			event.EventType, event.PlanName, float64(event.Amount)/100)
	}
}

// Example17_AssociateSubscriptionsWithUser demonstrates associating pre-registration subscriptions
func Example17_AssociateSubscriptionsWithUser() {
	service := setupService()

	ctx := context.Background()

	email := "preregistration@example.com"

	// Simulate pre-registration purchases (no user ID)
	fmt.Println("Creating pre-registration subscriptions...")
	for i := 0; i < 3; i++ {
		req := &billing.CreateSubscriptionRequest{
			IntegratorSubscriptionID: fmt.Sprintf("stripe_sub_prereg_%d", i),
			Integrator:               "stripe",
			UserID:                   "", // Empty - user doesn't exist yet
			Email:                    email,
			PlanName:                 fmt.Sprintf("Plan %d", i+1),
			Status:                   billing.StatusActive,
			BillingInterval:          "monthly",
			Amount:                   int64(1999 + (i * 1000)),
			Currency:                 "USD",
		}
		resp, _ := service.CreateSubscription(ctx, req)
		fmt.Printf("  - Created subscription %s (no user ID)\n", resp.Subscription.IntegratorSubscriptionID)
	}

	// User signs up later
	newUserID := "user-new-signup-123"
	fmt.Printf("\nUser signed up with ID: %s\n", newUserID)

	// Associate all pre-registration subscriptions with the new user
	associateReq := &billing.AssociateSubscriptionsWithUserRequest{
		UserID: newUserID,
		Email:  email,
	}

	associateResp, err := service.AssociateSubscriptionsWithUser(ctx, associateReq)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("\nAssociated %d subscriptions with user %s\n",
		associateResp.AssociatedCount, newUserID)

	// Verify the association
	getReq := &billing.GetSubscriptionsByEmailRequest{
		Email: email,
	}

	getResp, _ := service.GetSubscriptionsByEmail(ctx, getReq)
	fmt.Println("\nVerifying associations:")
	for _, sub := range getResp.Subscriptions {
		fmt.Printf("  - %s: User ID = %s ✓\n",
			sub.IntegratorSubscriptionID, sub.UserID)
	}
}

// Example18_PreRegistrationPurchaseFlow demonstrates the complete pre-registration flow
func Example18_PreRegistrationPurchaseFlow() {
	service := setupService()

	ctx := context.Background()

	email := "earlybird@example.com"

	fmt.Println("=== Pre-Registration Purchase Flow ===")
	fmt.Println()

	// Step 1: User purchases before signing up
	fmt.Println("Step 1: User purchases subscription (no account yet)")
	createReq := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: "stripe_sub_earlybird",
		Integrator:               "stripe",
		IntegratorCustomerID:     "cus_earlybird",
		UserID:                   "", // No user ID - not signed up yet
		Email:                    email,
		PlanName:                 "Early Bird Special",
		Status:                   billing.StatusActive,
		BillingInterval:          "yearly",
		Amount:                   19999, // $199.99
		Currency:                 "USD",
	}

	createResp, _ := service.CreateSubscription(ctx, createReq)
	fmt.Printf("  ✓ Subscription created: %s\n", createResp.Subscription.ID)
	fmt.Printf("  ✓ User ID: [empty - pending signup]\n")
	fmt.Printf("  ✓ Email: %s\n\n", createResp.Subscription.Email)

	// Step 2: Check subscriptions by email before signup
	fmt.Println("Step 2: Check what subscriptions exist for this email")
	getByEmailReq := &billing.GetSubscriptionsByEmailRequest{
		Email: email,
	}

	beforeSignup, _ := service.GetSubscriptionsByEmail(ctx, getByEmailReq)
	fmt.Printf("  ✓ Found %d subscription(s)\n", beforeSignup.Total)
	for _, sub := range beforeSignup.Subscriptions {
		hasUser := "NO"
		if sub.UserID != "" {
			hasUser = "YES"
		}
		fmt.Printf("    - %s: Has User ID? %s\n", sub.PlanName, hasUser)
	}
	fmt.Println()

	// Step 3: User signs up weeks later
	fmt.Println("Step 3: User creates account 2 weeks later")
	newUserID := "user-earlybird-123"
	fmt.Printf("  ✓ New user created: %s\n", newUserID)
	fmt.Printf("  ✓ Email: %s\n\n", email)

	// Step 4: Auto-associate subscriptions during signup
	fmt.Println("Step 4: System auto-associates pre-purchase subscriptions")
	associateReq := &billing.AssociateSubscriptionsWithUserRequest{
		UserID: newUserID,
		Email:  email,
	}

	associateResp, _ := service.AssociateSubscriptionsWithUser(ctx, associateReq)
	fmt.Printf("  ✓ Associated %d subscription(s) with user\n\n", associateResp.AssociatedCount)

	// Step 5: Verify user now has active subscription
	fmt.Println("Step 5: Verify user has immediate access")
	afterSignup, _ := service.GetSubscriptionsByEmail(ctx, getByEmailReq)
	for _, sub := range afterSignup.Subscriptions {
		fmt.Printf("  ✓ Subscription: %s\n", sub.PlanName)
		fmt.Printf("    - User ID: %s ✓\n", sub.UserID)
		fmt.Printf("    - Status: %s ✓\n", sub.Status)
		fmt.Printf("    - Amount: $%.2f/%s\n", float64(sub.Amount)/100, sub.BillingInterval)
	}

	fmt.Println("\n=== Flow Complete: User has immediate access to purchased subscription ===")
}

// Example19_GetUnassociatedSubscriptions demonstrates finding orphaned subscriptions
func Example19_GetUnassociatedSubscriptions() {
	service := setupService()

	ctx := context.Background()

	fmt.Println("=== Finding Unassociated Subscriptions ===")
	fmt.Println()

	// Create a mix of subscriptions with and without user IDs
	fmt.Println("Step 1: Creating test subscriptions")

	// Pre-registration purchases (no user ID)
	for i := 0; i < 3; i++ {
		req := &billing.CreateSubscriptionRequest{
			IntegratorSubscriptionID: fmt.Sprintf("stripe_sub_orphan_%d", i),
			Integrator:               "stripe",
			UserID:                   "", // Empty - no user
			Email:                    fmt.Sprintf("orphan%d@example.com", i),
			PlanName:                 "Orphaned Plan",
			Status:                   billing.StatusActive,
			BillingInterval:          "monthly",
			Amount:                   2999,
			Currency:                 "USD",
		}
		service.CreateSubscription(ctx, req)
	}
	fmt.Println("  ✓ Created 3 subscriptions without user IDs")

	// Normal subscriptions (with user ID)
	for i := 0; i < 2; i++ {
		req := &billing.CreateSubscriptionRequest{
			IntegratorSubscriptionID: fmt.Sprintf("stripe_sub_normal_%d", i),
			Integrator:               "stripe",
			UserID:                   fmt.Sprintf("user-%d", i),
			Email:                    fmt.Sprintf("normal%d@example.com", i),
			PlanName:                 "Normal Plan",
			Status:                   billing.StatusActive,
			BillingInterval:          "monthly",
			Amount:                   2999,
			Currency:                 "USD",
		}
		service.CreateSubscription(ctx, req)
	}
	fmt.Println("  ✓ Created 2 subscriptions with user IDs")
	fmt.Println()

	// Find unassociated subscriptions
	fmt.Println("Step 2: Finding unassociated subscriptions")
	req := &billing.GetUnassociatedSubscriptionsRequest{
		Limit: 100,
	}

	resp, err := service.GetUnassociatedSubscriptions(ctx, req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("\nFound %d unassociated subscription(s):\n", resp.Total)
	for i, sub := range resp.Subscriptions {
		fmt.Printf("  %d. Email: %s, Provider: %s, Created: %s\n",
			i+1, sub.Email, sub.Integrator, sub.CreatedAt[:10])
	}

	fmt.Println("\n=== These subscriptions need user association ===")
}

// Example20_UpdateSubscriptionUserID demonstrates manually associating a subscription
func Example20_UpdateSubscriptionUserID() {
	service := setupService()

	ctx := context.Background()

	fmt.Println("=== Manual Subscription User ID Update ===")
	fmt.Println()

	// Create an unassociated subscription
	fmt.Println("Step 1: Creating orphaned subscription")
	createReq := &billing.CreateSubscriptionRequest{
		IntegratorSubscriptionID: "stripe_sub_manual_fix",
		Integrator:               "stripe",
		UserID:                   "", // Empty - no user
		Email:                    "manual@example.com",
		PlanName:                 "Manual Fix Plan",
		Status:                   billing.StatusActive,
		BillingInterval:          "monthly",
		Amount:                   3999,
		Currency:                 "USD",
	}

	createResp, _ := service.CreateSubscription(ctx, createReq)
	subscriptionID := createResp.Subscription.ID
	fmt.Printf("  ✓ Created subscription: %s\n", subscriptionID)
	fmt.Printf("  ✓ User ID: [empty]\n")
	fmt.Printf("  ✓ Email: %s\n\n", createResp.Subscription.Email)

	// Admin manually associates it with a user
	fmt.Println("Step 2: Admin manually associates subscription with user")
	newUserID := "user-manual-fix-123"

	updateReq := &billing.UpdateSubscriptionUserIDRequest{
		SubscriptionID: subscriptionID,
		UserID:         newUserID,
	}

	updateResp, err := service.UpdateSubscriptionUserID(ctx, updateReq)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("  ✓ Updated subscription: %s\n", subscriptionID)
	fmt.Printf("  ✓ New User ID: %s\n", updateResp.Subscription.UserID)
	fmt.Printf("  ✓ Success: %v\n\n", updateResp.Success)

	// Verify the update
	fmt.Println("Step 3: Verifying update")
	getReq := &billing.GetSubscriptionByIDRequest{
		ID: subscriptionID,
	}

	getResp, _ := service.GetSubscriptionByID(ctx, getReq)
	fmt.Printf("  ✓ Subscription %s now has User ID: %s\n",
		getResp.Subscription.ID, getResp.Subscription.UserID)

	fmt.Println("\n=== Manual association complete ===")
}

// Example21_OrphanedSubscriptionWorkflow demonstrates complete orphan detection and resolution
func Example21_OrphanedSubscriptionWorkflow() {
	service := setupService()

	ctx := context.Background()

	fmt.Println("=== Orphaned Subscription Detection & Resolution Workflow ===")
	fmt.Println()

	// Simulate various subscription scenarios
	fmt.Println("Step 1: Setting up test data (simulating 30 days of activity)")

	// Old orphaned subscriptions (potential issue)
	oldOrphans := []string{"early1@example.com", "early2@example.com"}
	for i, email := range oldOrphans {
		req := &billing.CreateSubscriptionRequest{
			IntegratorSubscriptionID: fmt.Sprintf("stripe_old_orphan_%d", i),
			Integrator:               "stripe",
			UserID:                   "",
			Email:                    email,
			PlanName:                 "Early Bird",
			Status:                   billing.StatusActive,
			BillingInterval:          "yearly",
			Amount:                   19999,
			Currency:                 "USD",
		}
		service.CreateSubscription(ctx, req)
	}
	fmt.Printf("  ✓ Created %d old orphaned subscriptions\n", len(oldOrphans))

	// Recent orphaned subscriptions (likely okay - just purchased)
	recentOrphans := []string{"new1@example.com", "new2@example.com", "new3@example.com"}
	for i, email := range recentOrphans {
		req := &billing.CreateSubscriptionRequest{
			IntegratorSubscriptionID: fmt.Sprintf("stripe_recent_orphan_%d", i),
			Integrator:               "lemonsqueezy",
			UserID:                   "",
			Email:                    email,
			PlanName:                 "Recent Purchase",
			Status:                   billing.StatusActive,
			BillingInterval:          "monthly",
			Amount:                   2999,
			Currency:                 "USD",
		}
		service.CreateSubscription(ctx, req)
	}
	fmt.Printf("  ✓ Created %d recent orphaned subscriptions\n", len(recentOrphans))

	// Normal subscriptions
	for i := 0; i < 5; i++ {
		req := &billing.CreateSubscriptionRequest{
			IntegratorSubscriptionID: fmt.Sprintf("stripe_normal_%d", i),
			Integrator:               "stripe",
			UserID:                   fmt.Sprintf("user-normal-%d", i),
			Email:                    fmt.Sprintf("normal%d@example.com", i),
			PlanName:                 "Standard Plan",
			Status:                   billing.StatusActive,
			BillingInterval:          "monthly",
			Amount:                   2999,
			Currency:                 "USD",
		}
		service.CreateSubscription(ctx, req)
	}
	fmt.Println("  ✓ Created 5 normal subscriptions")
	fmt.Println()

	// Step 2: Run orphan detection
	fmt.Println("Step 2: Running orphan detection")
	orphanReq := &billing.GetUnassociatedSubscriptionsRequest{
		Limit: 100,
	}

	orphanResp, _ := service.GetUnassociatedSubscriptions(ctx, orphanReq)
	fmt.Printf("  ⚠️  Found %d unassociated subscriptions\n\n", orphanResp.Total)

	// Step 3: Filter by provider
	fmt.Println("Step 3: Filtering by provider (Stripe only)")
	stripeOrphanReq := &billing.GetUnassociatedSubscriptionsRequest{
		IntegratorName: "stripe",
		Limit:          100,
	}

	stripeOrphanResp, _ := service.GetUnassociatedSubscriptions(ctx, stripeOrphanReq)
	fmt.Printf("  ⚠️  Found %d Stripe unassociated subscriptions\n", stripeOrphanResp.Total)
	for i, sub := range stripeOrphanResp.Subscriptions {
		fmt.Printf("    %d. %s - %s\n", i+1, sub.Email, sub.PlanName)
	}
	fmt.Println()

	// Step 4: Manual resolution for one orphan
	fmt.Println("Step 4: Admin resolves one orphan manually")
	if len(orphanResp.Subscriptions) > 0 {
		firstOrphan := orphanResp.Subscriptions[0]
		fmt.Printf("  → Associating %s with new user\n", firstOrphan.Email)

		updateReq := &billing.UpdateSubscriptionUserIDRequest{
			SubscriptionID: firstOrphan.ID,
			UserID:         "user-admin-resolved-123",
		}

		updateResp, _ := service.UpdateSubscriptionUserID(ctx, updateReq)
		fmt.Printf("  ✓ Successfully associated subscription with user %s\n\n",
			updateResp.Subscription.UserID)
	}

	// Step 5: Recheck orphans
	fmt.Println("Step 5: Rechecking orphan count")
	recheckResp, _ := service.GetUnassociatedSubscriptions(ctx, orphanReq)
	fmt.Printf("  ℹ️  Remaining unassociated: %d (was %d)\n",
		recheckResp.Total, orphanResp.Total)

	fmt.Println("\n=== Workflow demonstrates monitoring and resolution process ===")
}

// Example22_FilterUnassociatedByDateRange demonstrates date-based filtering
func Example22_FilterUnassociatedByDateRange() {
	service := setupService()

	ctx := context.Background()

	fmt.Println("=== Filtering Unassociated Subscriptions by Date ===")
	fmt.Println()

	// This example shows how you would filter in production
	// Note: In this in-memory example, all dates will be similar

	fmt.Println("Typical production usage:")
	fmt.Println()

	fmt.Println("1. Find all orphans:")
	allReq := &billing.GetUnassociatedSubscriptionsRequest{
		Limit: 100,
	}
	allResp, _ := service.GetUnassociatedSubscriptions(ctx, allReq)
	fmt.Printf("   Total unassociated: %d\n\n", allResp.Total)

	fmt.Println("2. Find orphans older than 30 days (needs attention):")
	fmt.Println("   CreatedAtTo: \"2025-09-11T00:00:00Z\"")
	fmt.Println("   → These might need manual follow-up")
	fmt.Println()

	fmt.Println("3. Find recent orphans (last 7 days):")
	fmt.Println("   CreatedAtFrom: \"2025-10-04T00:00:00Z\"")
	fmt.Println("   → These are likely normal pre-registration purchases")
	fmt.Println()

	fmt.Println("4. Find orphans from specific campaign period:")
	fmt.Println("   CreatedAtFrom: \"2025-09-01T00:00:00Z\"")
	fmt.Println("   CreatedAtTo: \"2025-09-30T00:00:00Z\"")
	fmt.Println("   → Track conversion rates for marketing campaigns")

	fmt.Println("\n=== Date filtering enables targeted orphan management ===")
}
