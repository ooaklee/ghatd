package paymentprovider

import (
	"context"
	"errors"
	"io"
	"net/http"
)

// MockProvider is a provider that simulates webhook responses for testing
// It doesn't verify anything and returns predefined data
type MockProvider struct {
	providerName string
	mockPayload  *WebhookPayload
	mockInfo     *SubscriptionInfo
	shouldFail   bool
}

// NewMockProvider creates a mock provider for testing
func NewMockProvider(providerName string) *MockProvider {
	return &MockProvider{
		providerName: providerName,
		shouldFail:   false,
		mockPayload: &WebhookPayload{
			EventType:      EventTypePaymentSucceeded,
			EventID:        "mock-event-123",
			EventTime:      "2024-01-01T00:00:00Z",
			SubscriptionID: "mock-sub-123",
			CustomerID:     "mock-customer-123",
			CustomerEmail:  "test@example.com",
			Status:         SubscriptionStatusActive,
			PlanName:       "Mock Plan",
			Amount:         999,
			Currency:       "USD",
		},
		mockInfo: &SubscriptionInfo{
			SubscriptionID: "mock-sub-123",
			CustomerID:     "mock-customer-123",
			Status:         SubscriptionStatusActive,
			PlanName:       "Mock Plan",
		},
	}
}

// SetMockPayload sets the payload to return
func (m *MockProvider) SetMockPayload(payload *WebhookPayload) {
	m.mockPayload = payload
}

// SetMockInfo sets the subscription info to return
func (m *MockProvider) SetMockInfo(info *SubscriptionInfo) {
	m.mockInfo = info
}

// SetShouldFail makes all operations fail
func (m *MockProvider) SetShouldFail(fail bool) {
	m.shouldFail = fail
}

// GetProviderName returns the mock provider name
func (m *MockProvider) GetProviderName() string {
	return "mock-" + m.providerName
}

// VerifyWebhook always succeeds unless configured to fail
func (m *MockProvider) VerifyWebhook(ctx context.Context, req *http.Request) error {
	if m.shouldFail {
		return errors.New(ErrKeyPaymentProviderInvalidWebhookSignature)
	}
	return nil
}

// ParsePayload returns the mock payload
func (m *MockProvider) ParsePayload(ctx context.Context, req *http.Request) (*WebhookPayload, error) {
	if m.shouldFail {
		return nil, errors.New(ErrKeyPaymentProviderPayloadParsing)
	}

	// Read body for raw payload
	body, _ := io.ReadAll(req.Body)

	payload := *m.mockPayload
	payload.RawPayload = string(body)

	return &payload, nil
}

// GetSubscriptionInfo returns the mock subscription info
func (m *MockProvider) GetSubscriptionInfo(ctx context.Context, subscriptionID string) (*SubscriptionInfo, error) {
	if m.shouldFail {
		return nil, errors.New(ErrKeyPaymentProviderSubscriptionNotFound)
	}

	info := *m.mockInfo
	info.SubscriptionID = subscriptionID

	return &info, nil
}
