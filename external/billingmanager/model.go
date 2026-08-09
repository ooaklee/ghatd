package billingmanager

import (
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// SubscriptionStatus represents a user's subscription status
type SubscriptionStatus struct {
	HasAccess            bool       `json:"has_access"`
	HasSubscription      bool       `json:"has_subscription"`
	BillingKind          string     `json:"billing_kind,omitempty"`
	PaymentType          string     `json:"payment_type,omitempty"`
	IsOneOff             bool       `json:"is_one_off"`
	PaymentStatus        string     `json:"payment_status,omitempty"`
	Status               string     `json:"status"`
	PlanName             string     `json:"plan_name,omitempty"`
	PlanID               string     `json:"plan_id,omitempty"`
	PlanSlug             string     `json:"plan_slug,omitempty"`
	CostID               string     `json:"cost_id,omitempty"`
	ProviderPriceID      string     `json:"provider_price_id,omitempty"`
	Provider             string     `json:"provider,omitempty"`
	TransactionID        string     `json:"transaction_id,omitempty"`
	CustomerID           string     `json:"customer_id,omitempty"`
	UserReference        string     `json:"user_reference,omitempty"`
	Amount               int64      `json:"amount"`
	AmountKnown          bool       `json:"amount_known"`
	Currency             string     `json:"currency,omitempty"`
	BillingInterval      string     `json:"billing_interval,omitempty"`
	BillingIntervalCount int64      `json:"billing_interval_count,omitempty"`
	Quantity             int64      `json:"billing_quantity,omitempty"`
	NextBillingDate      *time.Time `json:"next_billing_date,omitempty"`
	TrialEndsAt          *time.Time `json:"provider_trial_ends_at,omitempty"`
	AvailableUntilDate   *time.Time `json:"available_until_date,omitempty"`
	CancelURL            string     `json:"cancel_url,omitempty"`
	UpdateURL            string     `json:"update_url,omitempty"`
	IsActive             bool       `json:"is_active"`
	IsInGoodStanding     bool       `json:"is_in_good_standing"`
}

// BillingDetail represents detailed billing information
type BillingDetail struct {
	HasAccess               bool                     `json:"has_access"`
	HasSubscription         bool                     `json:"has_subscription"`
	BillingKind             string                   `json:"billing_kind,omitempty"`
	PaymentType             string                   `json:"payment_type,omitempty"`
	IsOneOff                bool                     `json:"is_one_off"`
	PaymentStatus           string                   `json:"payment_status,omitempty"`
	Provider                string                   `json:"provider,omitempty"`
	Plan                    string                   `json:"plan,omitempty"`
	PlanName                string                   `json:"plan_name,omitempty"`
	PlanID                  string                   `json:"plan_id,omitempty"`
	PlanSlug                string                   `json:"plan_slug,omitempty"`
	CostID                  string                   `json:"cost_id,omitempty"`
	ProviderPriceID         string                   `json:"provider_price_id,omitempty"`
	TransactionID           string                   `json:"transaction_id,omitempty"`
	CustomerID              string                   `json:"customer_id,omitempty"`
	UserReference           string                   `json:"user_reference,omitempty"`
	Status                  string                   `json:"status,omitempty"`
	Summary                 string                   `json:"summary"`
	Amount                  int64                    `json:"amount"`
	AmountKnown             bool                     `json:"amount_known"`
	Currency                string                   `json:"currency,omitempty"`
	BillingInterval         string                   `json:"billing_interval,omitempty"`
	BillingIntervalCount    int64                    `json:"billing_interval_count,omitempty"`
	Quantity                int64                    `json:"billing_quantity,omitempty"`
	NextBillingDate         *time.Time               `json:"next_billing_date,omitempty"`
	TrialEndsAt             *time.Time               `json:"provider_trial_ends_at,omitempty"`
	AvailableUntilDate      *time.Time               `json:"available_until_date,omitempty"`
	UpcomingInvoiceEstimate *UpcomingInvoiceEstimate `json:"upcoming_invoice_estimate,omitempty"`
	CancelURL               string                   `json:"cancel_url,omitempty"`
	UpdateURL               string                   `json:"update_url,omitempty"`
}

// UpcomingInvoiceEstimate is a provider-generated preview, not a guaranteed
// final invoice. All amounts use the currency's minor unit.
type UpcomingInvoiceEstimate struct {
	Estimated bool       `json:"estimated"`
	Subtotal  int64      `json:"subtotal"`
	TaxAmount int64      `json:"tax_amount"`
	Total     int64      `json:"total"`
	AmountDue int64      `json:"amount_due"`
	Currency  string     `json:"currency"`
	DueDate   *time.Time `json:"due_date,omitempty"`
}

// EventSummary represents a billing event summary
type EventSummary struct {
	EventID         string    `json:"event_id"`
	ProviderEventID string    `json:"provider_event_id,omitempty"`
	EventType       string    `json:"event_type"`
	EventTime       time.Time `json:"event_time"`
	BillingKind     string    `json:"billing_kind,omitempty"`
	PaymentType     string    `json:"payment_type,omitempty"`
	IsOneOff        bool      `json:"is_one_off"`
	PaymentStatus   string    `json:"payment_status,omitempty"`
	TransactionID   string    `json:"transaction_id,omitempty"`
	CustomerID      string    `json:"customer_id,omitempty"`
	UserReference   string    `json:"user_reference,omitempty"`
	Amount          int64     `json:"amount"`
	Currency        string    `json:"currency"`
	PlanName        string    `json:"plan_name"`
	PlanID          string    `json:"plan_id,omitempty"`
	PlanSlug        string    `json:"plan_slug,omitempty"`
	CostID          string    `json:"cost_id,omitempty"`
	ProviderPriceID string    `json:"provider_price_id,omitempty"`
	Status          string    `json:"status"`
	ReceiptURL      string    `json:"receipt_url,omitempty"`
	Description     string    `json:"description"`
}

// AuditEvent represents an audit log entry
type AuditEvent struct {

	// EventType is the type of the event (e.g., "billing.subscription.created")
	EventType string `json:"event_type" bson:"event_type"`

	// UserID is the ID of the user associated with the event
	UserID string `json:"user_id" bson:"user_id"`

	// Details provides additional context about the event
	Details string `json:"details" bson:"details"`

	// OccurredAt is the timestamp when the event occurred
	OccurredAt time.Time `json:"occurred_at" bson:"occurred_at"`

	// Provider is the name of the payment provider
	Provider string `json:"provider" bson:"provider"`

	// BillingEventSuccessfullyCreated indicates if the billing event was successfully created
	BillingEventSuccessfullyCreated bool `json:"billing_event_successfully_created" bson:"billing_event_successfully_created"`

	// BillingSubscriptionId is the ID of the billing subscription (if applicable)
	BillingSubscriptionId string `json:"billing_subscription_id,omitempty" bson:"billing_subscription_id,omitempty"`

	// ProviderPayload contains the webhook payload from the payment provider (if billing event creation failed)
	ProviderPayload *paymentprovider.WebhookPayload `json:"provider_payload,omitempty" bson:"provider_payload,omitempty"`
}
