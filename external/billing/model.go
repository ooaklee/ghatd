package billing

import (
	"time"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// Subscription represents a recurring subscription or one-time plan-access record.
// Existing fields retain their original names for storage and API compatibility.
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

	// IntegratorTransactionID is the provider's stable payment transaction ID.
	IntegratorTransactionID string `json:"transaction_id,omitempty" bson:"integrator_transaction_id,omitempty"`

	// UserReference is the stable application user reference attached at checkout.
	UserReference string `json:"user_reference,omitempty" bson:"user_reference,omitempty"`

	// BillingKind distinguishes recurring billing from one-time billing.
	BillingKind string `json:"billing_kind,omitempty" bson:"billing_kind,omitempty"`

	// PaymentType describes the purchased item independently of billing cadence.
	PaymentType string `json:"payment_type,omitempty" bson:"payment_type,omitempty"`

	// IsOneOff indicates that this access record does not renew.
	IsOneOff bool `json:"is_one_off" bson:"is_one_off,omitempty"`

	// PaymentStatus is the state of the individual payment.
	PaymentStatus string `json:"payment_status,omitempty" bson:"payment_status,omitempty"`

	// PlanName is the name of the subscription plan
	PlanName string `json:"plan_name" bson:"plan_name"`

	// PlanID is the provider's plan identifier
	PlanID string `json:"plan_id" bson:"plan_id,omitempty"`

	// PlanSlug is the stable human-readable plan identifier.
	PlanSlug string `json:"plan_slug,omitempty" bson:"plan_slug,omitempty"`

	// CostID identifies the selected cost and ProviderPriceID its provider price.
	CostID          string `json:"cost_id,omitempty" bson:"cost_id,omitempty"`
	ProviderPriceID string `json:"provider_price_id,omitempty" bson:"provider_price_id,omitempty"`

	// Amount is the subscription amount in the currency's minor unit.
	Amount int64 `json:"amount" bson:"amount"`

	// AmountKnown distinguishes an explicit free recurring price from a legacy
	// or ambiguous record whose recurring amount is unknown.
	AmountKnown bool `json:"amount_known" bson:"amount_known,omitempty"`

	// Currency is the ISO 4217 currency code
	Currency string `json:"currency" bson:"currency"`

	// BillingInterval is the billing frequency (month, year, etc.)
	BillingInterval string `json:"billing_interval" bson:"billing_interval,omitempty"`

	// BillingIntervalCount is the number of intervals between renewals.
	BillingIntervalCount int64 `json:"billing_interval_count,omitempty" bson:"billing_interval_count,omitempty"`

	// Quantity is the licensed quantity already included in Amount.
	Quantity int64 `json:"quantity,omitempty" bson:"quantity,omitempty"`

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

// IsRecurring reports whether the record is a recurring subscription. Empty
// classification fields retain the legacy recurring interpretation.
func (s *Subscription) IsRecurring() bool {
	if s == nil || s.IsOneOff || s.BillingKind == "one_time" {
		return false
	}
	return s.BillingKind == "recurring" || s.PaymentType == "subscription" ||
		(s.BillingKind == "" && s.PaymentType == "")
}

// HasAccess reports whether this record currently grants plan access.
func (s *Subscription) HasAccess() bool {
	return s != nil && s.IsActive()
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

	// Email is the customer's email address associated with this billing event
	Email string `json:"email" bson:"email"`

	// EventType is the type of event (payment.succeeded, subscription.cancelled, etc.)
	EventType string `json:"event_type" bson:"event_type"`

	// Integrator is the payment provider name
	Integrator string `json:"integrator" bson:"integrator"`

	// IntegratorEventID is the provider's event ID
	IntegratorEventID string `json:"integrator_event_id" bson:"integrator_event_id"`

	// IntegratorSubscriptionID is the provider's subscription ID
	IntegratorSubscriptionID string `json:"integrator_subscription_id" bson:"integrator_subscription_id"`

	// IntegratorTransactionID is the provider's transaction ID.
	IntegratorTransactionID string `json:"transaction_id,omitempty" bson:"integrator_transaction_id,omitempty"`

	// IntegratorCustomerID is the provider's customer ID.
	IntegratorCustomerID string `json:"customer_id,omitempty" bson:"integrator_customer_id,omitempty"`

	// UserReference is the stable application user reference attached at checkout.
	UserReference string `json:"user_reference,omitempty" bson:"user_reference,omitempty"`

	BillingKind   string `json:"billing_kind,omitempty" bson:"billing_kind,omitempty"`
	PaymentType   string `json:"payment_type,omitempty" bson:"payment_type,omitempty"`
	IsOneOff      bool   `json:"is_one_off" bson:"is_one_off,omitempty"`
	PaymentStatus string `json:"payment_status,omitempty" bson:"payment_status,omitempty"`

	// Status is the event status (active, trialing, past_due, etc.)
	Status string `json:"status" bson:"status"`

	// Amount is the transaction amount (in cents)
	Amount int64 `json:"amount" bson:"amount"`

	// Currency is the ISO 4217 currency code
	Currency string `json:"currency" bson:"currency"`

	// PlanName is the subscription plan name
	PlanName string `json:"plan_name" bson:"plan_name"`

	PlanID          string `json:"plan_id,omitempty" bson:"plan_id,omitempty"`
	PlanSlug        string `json:"plan_slug,omitempty" bson:"plan_slug,omitempty"`
	CostID          string `json:"cost_id,omitempty" bson:"cost_id,omitempty"`
	ProviderPriceID string `json:"provider_price_id,omitempty" bson:"provider_price_id,omitempty"`

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
