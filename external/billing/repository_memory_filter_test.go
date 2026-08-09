package billing

import (
	"context"
	"testing"
)

func TestBillingEventFiltersAndPaginationUseSameFields(t *testing.T) {
	store := &InMemoryRepositoryStore{
		Subscriptions: map[string]*Subscription{},
		Events: map[string]*BillingEvent{
			"event_1": {
				ID: "event_1", Integrator: "provider", IntegratorEventID: "provider_event_1",
				IntegratorCustomerID: "customer_1", IntegratorTransactionID: "transaction_1",
				Email: "buyer@example.test", CreatedAt: "2024-01-01T00:00:00Z",
			},
			"event_2": {
				ID: "event_2", Integrator: "provider", IntegratorEventID: "provider_event_2",
				IntegratorCustomerID: "customer_1", IntegratorTransactionID: "transaction_2",
				Email: "buyer@example.test", CreatedAt: "2024-01-02T00:00:00Z",
			},
			"event_other": {
				ID: "event_other", Integrator: "provider", IntegratorEventID: "provider_event_other",
				IntegratorCustomerID: "customer_other", Email: "other@example.test", CreatedAt: "2024-01-03T00:00:00Z",
			},
		},
	}
	repository := NewInMemoryRepository(store)
	service := NewService(repository, repository)

	response, err := service.GetBillingEvents(context.Background(), &GetBillingEventsRequest{
		IntegratorCustomerID: "customer_1",
		ForEmails:            []string{"BUYER@example.test"},
		PerPage:              1,
		Page:                 2,
		Order:                "created_at_desc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 2 || response.TotalPages != 2 || response.Page != 2 || len(response.BillingEvents) != 1 {
		t.Fatalf("pagination = total:%d pages:%d page:%d events:%d", response.Total, response.TotalPages, response.Page, len(response.BillingEvents))
	}
	if response.BillingEvents[0].ID != "event_1" {
		t.Fatalf("page 2 event = %q, want event_1", response.BillingEvents[0].ID)
	}

	aliasResponse, err := service.GetBillingEvents(context.Background(), &GetBillingEventsRequest{
		IntegratorUserID: "customer_1",
		PerPage:          25,
		Page:             1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if aliasResponse.Total != 2 {
		t.Fatalf("legacy customer filter total = %d, want 2", aliasResponse.Total)
	}
}
