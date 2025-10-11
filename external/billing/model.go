package billing

import (
	"time"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// Subscription represents a user's subscription
type Subscription struct {
	// ID is the internal unique identifier
	ID string `json:"id" bson:"_id"`

	// UserID is the platform user's ID
	UserID string `json:"user_id" bson:"user_id"`

	// Email is the customer's email address
	Email string `json:"email" bson:"email"`

	// Status is the current subscription status
	Status string `json:"status" bson:"status"`

	// Integrator is the payment provider name (paddle, stripe, etc.)
	Integrator string `json:"integrator" bson:"integrator"`

	// IntegratorSubscriptionID is the provider's subscription ID
	IntegratorSubscriptionID string `json:"integrator_subscription_id" bson:"integrator_subscription_id"`

	// IntegratorCustomerID is the provider's customer ID
	IntegratorCustomerID string `json:"integrator_customer_id" bson:"integrator_customer_id"`

	// PlanName is the name of the subscription plan
	PlanName string `json:"plan_name" bson:"plan_name"`

	// PlanID is the provider's plan identifier
	PlanID string `json:"plan_id" bson:"plan_id,omitempty"`

	// Amount is the subscription amount (in cents)
	Amount int64 `json:"amount" bson:"amount"`

	// Currency is the ISO 4217 currency code
	Currency string `json:"currency" bson:"currency"`

	// BillingInterval is the billing frequency (month, year, etc.)
	BillingInterval string `json:"billing_interval" bson:"billing_interval,omitempty"`

	// NextBillingDate is when the next payment will be attempted
	NextBillingDate *time.Time `json:"next_billing_date,omitempty" bson:"next_billing_date,omitempty"`

	// AvailableUntilDate is when the subscription access expires
	AvailableUntilDate *time.Time `json:"available_until_date,omitempty" bson:"available_until_date,omitempty"`

	// ProviderTrialEndsAt is when the trial period ends
	ProviderTrialEndsAt *time.Time `json:"provider_trial_ends_at,omitempty" bson:"provider_trial_ends_at,omitempty"`

	// ProviderCancelledAt is when the subscription was cancelled
	ProviderCancelledAt *time.Time `json:"provider_cancelled_at,omitempty" bson:"provider_cancelled_at,omitempty"`

	// ProviderCreatedAt is when the subscription was created
	ProviderCreatedAt time.Time `json:"provider_created_at" bson:"provider_created_at"`

	// ProviderUpdatedAt is when the subscription was last updated
	ProviderUpdatedAt time.Time `json:"provider_updated_at" bson:"provider_updated_at"`

	// CancelURL is the provider's cancellation URL
	CancelURL string `json:"cancel_url,omitempty" bson:"cancel_url,omitempty"`

	// UpdateURL is the provider's update payment method URL
	UpdateURL string `json:"update_url,omitempty" bson:"update_url,omitempty"`

	// Metadata stores additional provider-specific data
	Metadata map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`

	// CreatedAt is when the subscription was stored in internal system
	CreatedAt string `json:"created_at" bson:"created_at"`

	// UpdatedAt is when the subscription was last updated in internal system
	UpdatedAt string `json:"updated_at" bson:"updated_at"`
}

// IsActive returns true if the subscription is currently active
func (s *Subscription) IsActive() bool {
	return s.Status == StatusActive || s.Status == StatusTrialing
}

// IsCancelled returns true if the subscription is cancelled
func (s *Subscription) IsCancelled() bool {
	return s.Status == StatusCancelled || s.Status == StatusExpired
}

// IsInGoodStanding returns true if the subscription is active and not past due
func (s *Subscription) IsInGoodStanding() bool {
	return s.Status == StatusActive || s.Status == StatusTrialing
}

// DaysUntilNextBilling returns the number of days until the next billing date
func (s *Subscription) DaysUntilNextBilling() int {
	if s.NextBillingDate == nil {
		return 0
	}

	duration := time.Until(*s.NextBillingDate)
	return int(duration.Hours() / 24)
}

// GenerateId generates a new Id for the subscription
func (s *Subscription) GenerateId() *Subscription {

	s.ID = toolbox.GenerateUuidV4()

	return s
}

// SetCreatedAtTimeToNow sets the created at date and time for the subscription to now
func (s *Subscription) SetCreatedAtTimeToNow() *Subscription {

	s.CreatedAt = toolbox.TimeNowUTC()

	return s
}

// SetUpdatedAtTimeToNow sets the updated at date and time for the subscription to now
func (s *Subscription) SetUpdatedAtTimeToNow() *Subscription {

	s.UpdatedAt = toolbox.TimeNowUTC()

	return s
}

// BillingEvent represents a billing-related event (payment, cancellation, etc.)
type BillingEvent struct {
	// ID is the internal unique identifier
	ID string `json:"id" bson:"_id"`

	// SubscriptionID references the subscription
	SubscriptionID string `json:"subscription_id" bson:"subscription_id"`

	// UserID references the user
	UserID string `json:"user_id" bson:"user_id"`

	// EventType is the type of event (payment.succeeded, subscription.cancelled, etc.)
	EventType string `json:"event_type" bson:"event_type"`

	// Integrator is the payment provider name
	Integrator string `json:"integrator" bson:"integrator"`

	// IntegratorEventID is the provider's event ID
	IntegratorEventID string `json:"integrator_event_id" bson:"integrator_event_id"`

	// IntegratorSubscriptionID is the provider's subscription ID
	IntegratorSubscriptionID string `json:"integrator_subscription_id" bson:"integrator_subscription_id"`

	// Status is the event status (active, trialing, past_due, etc.)
	Status string `json:"status" bson:"status"`

	// Amount is the transaction amount (in cents)
	Amount int64 `json:"amount" bson:"amount"`

	// Currency is the ISO 4217 currency code
	Currency string `json:"currency" bson:"currency"`

	// PlanName is the subscription plan name
	PlanName string `json:"plan_name" bson:"plan_name"`

	// ReceiptURL is the provider's receipt URL
	ReceiptURL string `json:"receipt_url,omitempty" bson:"receipt_url,omitempty"`

	// RawPayload is the original webhook payload for auditing
	RawPayload string `json:"raw_payload" bson:"raw_payload"`

	// ProviderEventTime is when the event occurred at the provider
	ProviderEventTime time.Time `json:"provider_event_time" bson:"provider_event_time"`

	// ProviderCreatedAt is when the event was recorded
	ProviderCreatedAt time.Time `json:"provider_created_at" bson:"provider_created_at"`

	// ProviderUpdatedAt is when the event was last updated
	ProviderUpdatedAt time.Time `json:"provider_updated_at" bson:"provider_updated_at"`

	// CreatedAt is when the subscription was stored in internal system
	CreatedAt string `json:"created_at" bson:"created_at"`

	// UpdatedAt is when the subscription was last updated in internal system
	UpdatedAt string `json:"updated_at" bson:"updated_at"`
}

// IsPaymentEvent returns true if this is a payment-related event
func (e *BillingEvent) IsPaymentEvent() bool {
	return e.EventType == "payment.succeeded" ||
		e.EventType == "payment.failed" ||
		e.EventType == "payment.refunded"
}

// IsSubscriptionEvent returns true if this is a subscription-related event
func (e *BillingEvent) IsSubscriptionEvent() bool {
	return e.EventType == "subscription.created" ||
		e.EventType == "subscription.updated" ||
		e.EventType == "subscription.cancelled" ||
		e.EventType == "subscription.paused" ||
		e.EventType == "subscription.resumed"
}

// GenerateId generates a new Id for the billing event
func (e *BillingEvent) GenerateId() *BillingEvent {

	e.ID = toolbox.GenerateUuidV4()

	return e
}

// SetCreatedAtTimeToNow sets the created at date and time for the billing event to now
func (e *BillingEvent) SetCreatedAtTimeToNow() *BillingEvent {

	e.CreatedAt = toolbox.TimeNowUTC()

	return e
}

// SetUpdatedAtTimeToNow sets the updated at date and time for the billing event to now
func (e *BillingEvent) SetUpdatedAtTimeToNow() *BillingEvent {

	e.UpdatedAt = toolbox.TimeNowUTC()

	return e
}
