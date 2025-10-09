package examples

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// Example1_CompleteWebhookProcessing demonstrates end-to-end webhook processing
func Example1_CompleteWebhookProcessing() {
	// Set up providers
	registry := paymentprovider.NewProviderRegistry()

	// Register a mock provider for testing
	mockProvider := paymentprovider.NewMockProvider("paddle")
	registry.Register(mockProvider)

	// Set up storage
	repo := billing.NewInMemoryRepository(nil)
	billingService := billing.NewService(repo, nil)

	// Create billing manager service
	service := billingmanager.NewService(registry, billingService)

	// Simulate webhook request
	webhookData := strings.NewReader(`{"subscription_id": "sub_123"}`)
	req := httptest.NewRequest("POST", "/webhook/paddle", webhookData)

	// Process webhook using ProcessBillingProviderWebhooksRequest
	err := service.ProcessBillingProviderWebhooks(context.Background(), &billingmanager.ProcessBillingProviderWebhooksRequest{
		ProviderName: "paddle",
		Request:      req,
	})
	if err != nil {
		fmt.Println("Webhook processing error:", err)
		return
	}

	fmt.Println("Webhook processed successfully!")
}

// Example2_QueryUserSubscription demonstrates querying user subscription status
func Example2_QueryUserSubscription() {
	service := setupService()

	// Get user's subscription status
	ctx := context.Background()
	resp, err := service.GetUserSubscriptionStatus(ctx, &billingmanager.GetUserSubscriptionStatusRequest{
		UserID:           "user-123",
		RequestingUserID: "user-123", // User querying their own subscription
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	status := resp.SubscriptionStatus
	if status.HasSubscription {
		fmt.Printf("User has %s subscription\n", status.PlanName)
		fmt.Printf("Status: %s\n", status.Status)
		fmt.Printf("Provider: %s\n", status.Provider)
		fmt.Printf("Active: %v\n", status.IsActive)
		if status.NextBillingDate != nil {
			fmt.Printf("Next billing: %s\n", status.NextBillingDate)
		}
	} else {
		fmt.Println("User has no active subscription")
	}
}

// Example3_GetBillingHistory demonstrates retrieving billing events
func Example3_GetBillingHistory() {
	service := setupService()

	// Get billing events
	ctx := context.Background()
	resp, err := service.GetUserBillingEvents(ctx, &billingmanager.GetUserBillingEventsRequest{
		UserID:           "user-123",
		RequestingUserID: "user-123",
		PerPage:          10,
		Page:             1,
		Order:            "created_at_desc",
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Found %d events:\n", resp.Total)
	for _, event := range resp.Events {
		fmt.Printf("  [%s] %s - $%.2f %s\n",
			event.EventTime.Format("2006-01-02"),
			event.Description,
			float64(event.Amount)/100,
			event.Currency)
	}
}

// Example4_GetBillingDetail demonstrates getting detailed billing information
func Example4_GetBillingDetail() {
	service := setupService()

	ctx := context.Background()
	resp, err := service.GetUserBillingDetail(ctx, &billingmanager.GetUserBillingDetailRequest{
		UserID:           "user-123",
		RequestingUserID: "user-123",
	})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	detail := resp.BillingDetail
	fmt.Printf("Has Subscription: %v\n", detail.HasSubscription)
	if detail.HasSubscription {
		fmt.Printf("Provider: %s\n", detail.Provider)
		fmt.Printf("Plan: %s\n", detail.Plan)
		fmt.Printf("Status: %s\n", detail.Status)
		fmt.Printf("Summary: %s\n", detail.Summary)
		fmt.Printf("Cancel URL: %s\n", detail.CancelURL)
		fmt.Printf("Update URL: %s\n", detail.UpdateURL)
	}
}

// Example5_HTTPWebhookHandler demonstrates webhook handler for HTTP server
func Example5_HTTPWebhookHandler() {
	service := setupService()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract provider from path: /api/v1/bms/billing/paddle
		pathParts := strings.Split(r.URL.Path, "/")
		providerName := pathParts[len(pathParts)-1]

		// Process webhook
		err := service.ProcessBillingProviderWebhooks(r.Context(), &billingmanager.ProcessBillingProviderWebhooksRequest{
			ProviderName: providerName,
			Request:      r,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Webhook processing failed: %v", err),
				http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Webhook processed"))
	})

	// Create test server
	ts := httptest.NewServer(handler)
	defer ts.Close()

	fmt.Println("Webhook endpoint:", ts.URL+"/api/v1/bms/billing/paddle")
}

// Example6_WithAuditService demonstrates adding audit logging
func Example6_WithAuditService() {
	registry := paymentprovider.NewProviderRegistry()
	repo := billing.NewInMemoryRepository(nil)
	billingService := billing.NewService(repo, nil)

	// Create simple audit service
	auditSvc := &SimpleAuditService{}

	// Create service with audit service
	service := billingmanager.NewService(registry, billingService).
		WithAuditService(auditSvc)

	// Now all webhook processing will be audited
	fmt.Println("Service created with audit logging")
	_ = service
}

// Example7_WithUserService demonstrates user service integration
func Example7_WithUserService() {
	registry := paymentprovider.NewProviderRegistry()
	repo := billing.NewInMemoryRepository(nil)
	billingService := billing.NewService(repo, nil)

	// Create simple user service
	userService := &SimpleUserService{}

	// Create service with user service
	service := billingmanager.NewService(registry, billingService).
		WithUserService(userService)

	// Now webhooks can resolve email -> user ID
	fmt.Println("Service created with user service")
	_ = service
}

// Helper functions and types

func setupService() *billingmanager.Service {
	registry := paymentprovider.NewProviderRegistry()
	mockProvider := paymentprovider.NewMockProvider("paddle")
	registry.Register(mockProvider)

	repo := billing.NewInMemoryRepository(nil)
	billingService := billing.NewService(repo, nil)

	return billingmanager.NewService(registry, billingService)
}

// SimpleAuditService implements billingmanager.AuditService for examples
type SimpleAuditService struct{}

func (s *SimpleAuditService) LogAuditEvent(ctx context.Context, req *audit.LogAuditEventRequest) error {
	fmt.Printf("[AUDIT] %s: %s (Actor: %s, Target: %s)\n",
		req.Action, req.Domain, req.ActorId, req.TargetId)
	return nil
}

// SimpleUserService implements billingmanager.UserService for examples
type SimpleUserService struct{}

func (s *SimpleUserService) GetUserByEmail(ctx context.Context, req *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error) {
	// In production, query your user database
	return &user.GetUserByEmailResponse{
		User: &user.UniversalUser{
			ID:    "user-123",
			Email: req.Email,
		},
	}, nil
}

func (s *SimpleUserService) GetUserByID(ctx context.Context, req *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	// In production, query your user database
	return &user.GetUserByIDResponse{
		User: &user.UniversalUser{
			ID:    req.ID,
			Email: "user@example.com",
		},
	}, nil
}
