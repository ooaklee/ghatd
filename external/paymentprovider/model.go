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

	// BillingKind indicates whether billing recurs or happens once.
	BillingKind BillingKind

	// IsOneOff indicates if this is a one-time payment (true) vs recurring subscription (false)
	IsOneOff bool

	// SubscriptionID is the provider's unique identifier for the subscription (empty for one-off payments)
	SubscriptionID string

	// TransactionID is the unique identifier for one-off payments or individual transactions
	TransactionID string

	// PaymentStatus is the state of the individual payment, independent of plan access status.
	PaymentStatus string

	// UserReference is a stable application user reference supplied when checkout is created.
	UserReference string

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

	// PlanID and PlanSlug are stable application plan identifiers.
	PlanID   string
	PlanSlug string

	// CostID identifies the selected plan cost and ProviderPriceID identifies the provider price.
	CostID          string
	ProviderPriceID string

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
	return wp != nil && wp.PaymentType == PaymentTypeSubscription
}

// IsRecurring reports whether the payload represents recurring billing. Empty
// BillingKind values retain the legacy subscription behaviour.
func (wp *WebhookPayload) IsRecurring() bool {
	if wp == nil || wp.IsOneOff || wp.BillingKind == BillingKindOneTime {
		return false
	}
	return wp.BillingKind == BillingKindRecurring || wp.PaymentType == PaymentTypeSubscription
}

// GrantsPlanAccess reports whether a successful event should be projected into
// the plan-access read model. Other payment types remain ledger-only.
func (wp *WebhookPayload) GrantsPlanAccess() bool {
	if wp == nil {
		return false
	}
	return wp.IsRecurring() || wp.PaymentType == PaymentTypePurchase ||
		(wp.PaymentType == PaymentTypeSubscription && wp.IsOneOff)
}

func billingKindForPayment(paymentType string, isOneOff bool) BillingKind {
	if isOneOff {
		return BillingKindOneTime
	}
	if paymentType == PaymentTypeSubscription {
		return BillingKindRecurring
	}
	return ""
}

func paymentStatusForEvent(eventType string) string {
	switch eventType {
	case EventTypePaymentSucceeded, EventTypeSubscriptionCreated, EventTypeSubscriptionCreatedDonation:
		return PaymentStatusSucceeded
	case EventTypePaymentFailed:
		return PaymentStatusFailed
	case EventTypePaymentRefunded:
		return PaymentStatusRefunded
	case EventTypePaymentActionRequired:
		return PaymentStatusActionRequired
	default:
		return ""
	}
}

// CheckoutSessionRequest is the provider-neutral input for a hosted or embedded
// checkout session. Metadata is copied to the resulting payment when supported.
type CheckoutSessionRequest struct {
	PriceID       string
	PlanID        string
	PlanSlug      string
	PlanName      string
	CostID        string
	UserID        string
	UserReference string
	CustomerEmail string
	Mode          string
	ReturnURL     string
	// ExpectedAmount is the trusted catalogue amount in minor currency units.
	// When any Expected* field is set, Stripe validates all three before checkout.
	ExpectedAmount         int64
	ExpectedCurrency       string
	ExpectedBillingCadence string
	// TrialPeriodDays applies to recurring Checkout Sessions only. Zero means
	// the Stripe Price starts billing immediately.
	TrialPeriodDays int
	// IdempotencyKey prevents duplicate provider sessions for a retried checkout
	// attempt. Host applications should scope untrusted client keys to the user.
	IdempotencyKey string
	Metadata       map[string]string
}

// CheckoutSession contains the browser-safe values returned by a checkout provider.
type CheckoutSession struct {
	ID             string `json:"id"`
	ClientSecret   string `json:"client_secret,omitempty"`
	PublishableKey string `json:"publishable_key,omitempty"`
	URL            string `json:"url,omitempty"`
}

// CustomerPortalSessionRequest contains the provider customer identifier and
// the trusted application URL to return to after portal activity.
type CustomerPortalSessionRequest struct {
	CustomerID string
	ReturnURL  string
}

// CustomerPortalSession contains browser-safe hosted portal session values.
type CustomerPortalSession struct {
	ID  string `json:"id,omitempty"`
	URL string `json:"url"`
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
