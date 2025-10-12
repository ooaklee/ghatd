package paymentprovider

// WebhookPayload represents normalized webhook data from any payment provider
// This common structure allows the billing system to handle webhooks uniformly
type WebhookPayload struct {
	// EventType is the normalized event type (e.g., "subscription.created", "payment.succeeded", "donation.received")
	EventType string

	// EventID is the unique identifier for this event from the provider
	EventID string

	// EventTime is the timestamp when the event occurred (RFC3339 format)
	EventTime string

	// PaymentType indicates the type of payment: "subscription", "donation", "shop_order", "commission"
	PaymentType string

	// IsOneOff indicates if this is a one-time payment (true) vs recurring subscription (false)
	IsOneOff bool

	// SubscriptionID is the provider's unique identifier for the subscription (empty for one-off payments)
	SubscriptionID string

	// TransactionID is the unique identifier for one-off payments or individual transactions
	TransactionID string

	// CustomerID is the provider's unique identifier for the customer
	CustomerID string

	// CustomerEmail is the customer's email address
	CustomerEmail string

	// CustomerName is the customer's display name (e.g., for Ko-fi donations)
	CustomerName string

	// Status is the current status of the subscription (active, cancelled, past_due, trialing, etc.)
	// Empty for one-off payments
	Status string

	// PlanName is the name/identifier of the subscription plan or tier
	PlanName string

	// Amount is the payment amount (in the smallest currency unit, e.g., cents)
	Amount int64

	// Currency is the ISO 4217 currency code (e.g., "USD", "GBP")
	Currency string

	// IsFirstSubscriptionPayment indicates if this is the first payment of a subscription
	IsFirstSubscriptionPayment bool

	// NextBillingDate is when the next payment will be attempted (ISO 8601 format)
	NextBillingDate string

	// AvailableUntilDate is when the subscription access expires (ISO 8601 format)
	AvailableUntilDate string

	// CancelURL is the provider's URL for the customer to cancel their subscription
	CancelURL string

	// UpdateURL is the provider's URL for the customer to update payment details
	UpdateURL string

	// ReceiptURL is the provider's URL for the customer to view their receipt
	ReceiptURL string

	// RawPayload is the original JSON payload from the provider (for auditing)
	RawPayload string
}

// IsSubscription returns true if the payment type is a subscription
func (wp *WebhookPayload) IsSubscription() bool {
	return wp.PaymentType == PaymentTypeSubscription
}

// SubscriptionInfo represents detailed subscription information from a provider's API
type SubscriptionInfo struct {
	// SubscriptionID is the provider's unique identifier for the subscription
	SubscriptionID string

	// CustomerID is the provider's unique identifier for the customer
	CustomerID string

	// Status is the current status of the subscription
	Status string

	// PlanName is the name/identifier of the subscription plan
	PlanName string

	// PlanID is the provider's unique identifier for the plan
	PlanID string

	// Amount is the payment amount
	Amount float64

	// Currency is the ISO 4217 currency code
	Currency string

	// BillingInterval is the billing frequency (e.g., "month", "year")
	BillingInterval string

	// NextBillingDate is when the next payment will be attempted
	NextBillingDate string

	// CurrentPeriodStart is when the current billing period started
	CurrentPeriodStart string

	// CurrentPeriodEnd is when the current billing period ends
	CurrentPeriodEnd string

	// CancelledAt is when the subscription was cancelled (if applicable)
	CancelledAt string

	// CancelURL is the provider's URL for cancellation
	CancelURL string

	// UpdateURL is the provider's URL for updating payment details
	UpdateURL string
}

// PriceInfo holds simplified price information
type PriceInfo struct {
	// UnitPrice is the price in lowest currency unit (e.g. cents)
	UnitPrice int64

	// Currency is the currency code (e.g. USD)
	Currency string
}
