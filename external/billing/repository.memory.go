package billing

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// InMemoryRepositoryStore is a simple in-memory store for testing and development
type InMemoryRepositoryStore struct {
	// Subscriptions is a map of subscription ID to Subscriptions
	Subscriptions map[string]*Subscription

	// Events is a map of event ID to Billing Events
	Events map[string]*BillingEvent
}

// InMemoryRepository is an in-memory implementation of the repository interface
// Useful for testing and development
type InMemoryRepository struct {
	store *InMemoryRepositoryStore
	mu    sync.RWMutex
}

// NewInMemoryRepository creates a new in-memory store
func NewInMemoryRepository(baseStore *InMemoryRepositoryStore) *InMemoryRepository {

	if baseStore == nil {
		baseStore = &InMemoryRepositoryStore{
			Subscriptions: make(map[string]*Subscription),
			Events:        make(map[string]*BillingEvent),
		}
	}

	return &InMemoryRepository{
		store: baseStore,
	}
}

// GetTotalSubscriptions handles fetching the total count of subscriptions that match the filters
func (m *InMemoryRepository) GetTotalSubscriptions(ctx context.Context, req *GetTotalSubscriptionsRequest) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := int64(0)
	for _, sub := range m.store.Subscriptions {
		if m.matchesSubscriptionFilter(sub, req) {
			count++
		}
	}

	return count, nil
}

// GetSubscriptions retrieves subscriptions that match the filters
func (m *InMemoryRepository) GetSubscriptions(ctx context.Context, req *GetSubscriptionsRequest) ([]Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var subscriptions []Subscription
	for _, sub := range m.store.Subscriptions {
		if m.matchesSubscriptionFilterFromGetRequest(sub, req) {
			subscriptions = append(subscriptions, *sub)
		}
	}

	// Sort subscriptions based on order
	subscriptions = m.sortSubscriptions(subscriptions, req.Order)

	return subscriptions, nil
}

// CreateSubscription creates a new subscription
func (m *InMemoryRepository) CreateSubscription(ctx context.Context, newSubscription *Subscription) (*Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.store.Subscriptions[newSubscription.ID] = newSubscription
	return newSubscription, nil
}

// GetSubscriptionByID retrieves a subscription by ID
func (m *InMemoryRepository) GetSubscriptionByID(ctx context.Context, subscriptionID string) (*Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sub, ok := m.store.Subscriptions[subscriptionID]
	if !ok {
		return nil, errors.New(ErrKeyBillingSubscriptionNotFound)
	}

	return sub, nil
}

// GetSubscriptionByIntegratorID retrieves a subscription by integrator subscription ID
func (m *InMemoryRepository) GetSubscriptionByIntegratorID(ctx context.Context, integratorName, integratorSubscriptionID string) (*Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, sub := range m.store.Subscriptions {
		if sub.Integrator == integratorName && sub.IntegratorSubscriptionID == integratorSubscriptionID {
			return sub, nil
		}
	}

	return nil, errors.New(ErrKeyBillingSubscriptionNotFound)
}

// UpdateSubscription updates a subscription
func (m *InMemoryRepository) UpdateSubscription(ctx context.Context, subscription *Subscription) (*Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.store.Subscriptions[subscription.ID]; !exists {
		return nil, errors.New(ErrKeyBillingSubscriptionNotFound)
	}

	m.store.Subscriptions[subscription.ID] = subscription
	return subscription, nil
}

// DeleteSubscription deletes a subscription
func (m *InMemoryRepository) DeleteSubscription(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.store.Subscriptions[id]; !exists {
		return errors.New(ErrKeyBillingSubscriptionNotFound)
	}

	delete(m.store.Subscriptions, id)
	return nil
}

// GetTotalBillingEvents handles fetching the total count of billing events that match the filters
func (m *InMemoryRepository) GetTotalBillingEvents(ctx context.Context, req *GetTotalBillingEventsRequest) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := int64(0)
	for _, event := range m.store.Events {
		if m.matchesBillingEventFilter(event, req) {
			count++
		}
	}

	return count, nil
}

// GetBillingEvents retrieves billing events that match the filters
func (m *InMemoryRepository) GetBillingEvents(ctx context.Context, req *GetBillingEventsRequest) ([]BillingEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var events []BillingEvent
	for _, event := range m.store.Events {
		if m.matchesBillingEventFilterFromGetRequest(event, req) {
			events = append(events, *event)
		}
	}

	// Sort events based on order
	events = m.sortBillingEvents(events, req.Order)

	return events, nil
}

// CreateBillingEvent creates a new billing event
func (m *InMemoryRepository) CreateBillingEvent(ctx context.Context, newEvent *BillingEvent) (*BillingEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if event already processed (to prevent duplicates)
	for _, event := range m.store.Events {
		if event.IntegratorEventID == newEvent.IntegratorEventID && event.Integrator == newEvent.Integrator {
			return nil, errors.New(ErrKeyBillingEventAlreadyProcessed)
		}
	}

	m.store.Events[newEvent.ID] = newEvent
	return newEvent, nil
}

// GetBillingEventByID retrieves a billing event by ID
func (m *InMemoryRepository) GetBillingEventByID(ctx context.Context, id string) (*BillingEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	event, ok := m.store.Events[id]
	if !ok {
		return nil, errors.New(ErrKeyBillingEventNotFound)
	}

	return event, nil
}

// GetFirstSuccessfulBillingEventWithPlanNameBySubscriptionId retrieves the first successful billing event
// that has a plan name for a given subscription ID and integrator
func (m *InMemoryRepository) GetFirstSuccessfulBillingEventWithPlanNameBySubscriptionId(ctx context.Context, integrator string, subscriptionID string) (*BillingEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matchingEvents []*BillingEvent

	// Find all events that match the criteria
	for _, event := range m.store.Events {
		if event.IntegratorSubscriptionID == subscriptionID &&
			event.Integrator == integrator &&
			event.Status == StatusActive &&
			event.PlanName != "" {
			matchingEvents = append(matchingEvents, event)
		}
	}

	if len(matchingEvents) == 0 {
		return nil, errors.New(ErrKeyBillingEventNotFound)
	}

	// Find the first one by created_at (earliest)
	firstEvent := matchingEvents[0]
	for _, event := range matchingEvents[1:] {
		if event.CreatedAt < firstEvent.CreatedAt {
			firstEvent = event
		}
	}

	return firstEvent, nil
}

// Helper methods for filtering

func (m *InMemoryRepository) matchesSubscriptionFilter(sub *Subscription, req *GetTotalSubscriptionsRequest) bool {
	if req.IntegratorName != "" && sub.Integrator != req.IntegratorName {
		return false
	}

	if req.IntegratorSubscriptionID != "" && sub.IntegratorSubscriptionID != req.IntegratorSubscriptionID {
		return false
	}

	if req.IntegratorCustomerID != "" && sub.IntegratorCustomerID != req.IntegratorCustomerID {
		return false
	}

	if len(req.UserIDs) > 0 && !contains(req.UserIDs, sub.UserID) {
		return false
	}

	if len(req.Emails) > 0 && !containsEmail(req.Emails, sub.Email) {
		return false
	}

	if len(req.Statuses) > 0 && !contains(req.Statuses, sub.Status) {
		return false
	}

	if req.PlanNameContains != "" && !strings.Contains(strings.ToLower(sub.PlanName), strings.ToLower(req.PlanNameContains)) {
		return false
	}

	if len(req.Currency) > 0 && !contains(req.Currency, sub.Currency) {
		return false
	}

	if len(req.BillingInterval) > 0 && !contains(req.BillingInterval, sub.BillingInterval) {
		return false
	}

	if req.CreatedAtFrom != "" && sub.CreatedAt < req.CreatedAtFrom {
		return false
	}

	if req.CreatedAtTo != "" && sub.CreatedAt > req.CreatedAtTo {
		return false
	}

	return true
}

func (m *InMemoryRepository) matchesSubscriptionFilterFromGetRequest(sub *Subscription, req *GetSubscriptionsRequest) bool {
	if req.IntegratorName != "" && sub.Integrator != req.IntegratorName {
		return false
	}

	if req.IntegratorSubscriptionID != "" && sub.IntegratorSubscriptionID != req.IntegratorSubscriptionID {
		return false
	}

	if req.IntegratorCustomerID != "" && sub.IntegratorCustomerID != req.IntegratorCustomerID {
		return false
	}

	if len(req.ForUserIDs) > 0 && !contains(req.ForUserIDs, sub.UserID) {
		return false
	}

	if len(req.ForEmails) > 0 && !containsEmail(req.ForEmails, sub.Email) {
		return false
	}

	if len(req.Statuses) > 0 && !contains(req.Statuses, sub.Status) {
		return false
	}

	if req.PlanNameContains != "" && !strings.Contains(strings.ToLower(sub.PlanName), strings.ToLower(req.PlanNameContains)) {
		return false
	}

	if len(req.Currency) > 0 && !contains(req.Currency, sub.Currency) {
		return false
	}

	if len(req.BillingInterval) > 0 && !contains(req.BillingInterval, sub.BillingInterval) {
		return false
	}

	if req.CreatedAtFrom != "" && sub.CreatedAt < req.CreatedAtFrom {
		return false
	}

	if req.CreatedAtTo != "" && sub.CreatedAt > req.CreatedAtTo {
		return false
	}

	return true
}

func (m *InMemoryRepository) matchesBillingEventFilter(event *BillingEvent, req *GetTotalBillingEventsRequest) bool {
	if req.IntegratorName != "" && event.Integrator != req.IntegratorName {
		return false
	}

	if req.IntegratorUserID != "" && event.IntegratorEventID != req.IntegratorUserID {
		return false
	}

	if req.IntegratorSubscriptionID != "" && event.IntegratorSubscriptionID != req.IntegratorSubscriptionID {
		return false
	}

	if len(req.UserIDs) > 0 && !contains(req.UserIDs, event.UserID) {
		return false
	}

	if len(req.EventTypes) > 0 && !contains(req.EventTypes, event.EventType) {
		return false
	}

	if req.PlanNameContains != "" && !strings.Contains(strings.ToLower(event.PlanName), strings.ToLower(req.PlanNameContains)) {
		return false
	}

	if len(req.Currency) > 0 && !contains(req.Currency, event.Currency) {
		return false
	}

	if len(req.Statuses) > 0 && !contains(req.Statuses, event.Status) {
		return false
	}

	if req.CreatedAtFrom != "" && event.CreatedAt < req.CreatedAtFrom {
		return false
	}

	if req.CreatedAtTo != "" && event.CreatedAt > req.CreatedAtTo {
		return false
	}

	return true
}

// matchesBillingEventFilterFromGetRequest is similar to matchesBillingEventFilter but uses GetBillingEventsRequest
func (m *InMemoryRepository) matchesBillingEventFilterFromGetRequest(event *BillingEvent, req *GetBillingEventsRequest) bool {
	if req.IntegratorName != "" && event.Integrator != req.IntegratorName {
		return false
	}

	if req.IntegratorUserID != "" && event.IntegratorEventID != req.IntegratorUserID {
		return false
	}

	if req.IntegratorSubscriptionID != "" && event.IntegratorSubscriptionID != req.IntegratorSubscriptionID {
		return false
	}

	if len(req.ForUserIDs) > 0 && !contains(req.ForUserIDs, event.UserID) {
		return false
	}

	if len(req.EventTypes) > 0 && !contains(req.EventTypes, event.EventType) {
		return false
	}

	if req.PlanNameContains != "" && !strings.Contains(strings.ToLower(event.PlanName), strings.ToLower(req.PlanNameContains)) {
		return false
	}

	if len(req.Currency) > 0 && !contains(req.Currency, event.Currency) {
		return false
	}

	if len(req.Statuses) > 0 && !contains(req.Statuses, event.Status) {
		return false
	}

	if req.CreatedAtFrom != "" && event.CreatedAt < req.CreatedAtFrom {
		return false
	}

	if req.CreatedAtTo != "" && event.CreatedAt > req.CreatedAtTo {
		return false
	}

	return true
}

// sortSubscriptions placeholder sorting the subscriptions
// we'll keep the order as-is since maps are unordered
func (m *InMemoryRepository) sortSubscriptions(subscriptions []Subscription, order string) []Subscription {
	return subscriptions
}

// sortBillingEvents placeholder sorting the billing events
// we'll keep the order as-is since maps are unordered
func (m *InMemoryRepository) sortBillingEvents(events []BillingEvent, order string) []BillingEvent {
	return events
}

// Helper functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsEmail(slice []string, email string) bool {
	emailLower := toolbox.StringStandardisedToLower(email)
	for _, s := range slice {
		if toolbox.StringStandardisedToLower(s) == emailLower {
			return true
		}
	}
	return false
}
