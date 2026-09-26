package paymentprovider

const (
	// StripeDefaultAPIBaseURL is the trusted Stripe API origin used by outbound requests.
	StripeDefaultAPIBaseURL = "https://api.stripe.com"

	// StripeDefaultAPIVersion is the Stripe API contract used by outbound
	// requests. It includes the embedded_page Checkout UI mode used by Stripe.js 9.
	StripeDefaultAPIVersion = "2026-03-25.dahlia"

	// ErrKeyPaymentProviderMissingConfiguration is a configuration error, returned when the provider name is missing in the config
	ErrKeyPaymentProviderMissingConfiguration = "PaymentProviderMissingConfiguration"

	// ErrKeyPaymentProviderRequiredWebhookSecretIsMissing is a configuration error, returned when the webhook secret is missing in the config
	ErrKeyPaymentProviderRequiredWebhookSecretIsMissing = "PaymentProviderRequiredWebhookSecretIsMissing"

	// ErrKeyPaymentProviderInvalidConfigWebhookSecret is a configuration error, returned when the webhook secret in the config is invalid
	ErrKeyPaymentProviderInvalidConfigWebhookSecret = "PaymentProviderInvalidConfigWebhookSecret"

	// ErrKeyPaymentProviderInvalidConfiguration is a configuration error, returned when the provider configuration is invalid
	ErrKeyPaymentProviderInvalidConfiguration = "PaymentProviderInvalidConfiguration"

	// ErrKeyPaymentProviderInvalidWebhookSignature is a webhook verification error, returned when the webhook signature is invalid
	ErrKeyPaymentProviderInvalidWebhookSignature = "PaymentProviderInvalidWebhookSignature"

	// ErrKeyPaymentProviderMissingSignature is a webhook verification error, returned when the webhook signature is missing
	ErrKeyPaymentProviderMissingSignature = "PaymentProviderMissingSignature"

	// ErrKeyPaymentProviderInvalidPayload is a webhook verification error, returned when the webhook payload is invalid or malformed
	ErrKeyPaymentProviderInvalidPayload = "PaymentProviderInvalidPayload"

	// ErrKeyPaymentProviderWebhookTimestampTooOld is a webhook verification error, returned when the webhook timestamp is "too old".
	// This is used to prevent replay attacks and is to our discretion, as not all providers use timestamps in their signatures.
	ErrKeyPaymentProviderWebhookTimestampTooOld = "PaymentProviderWebhookTimestampTooOld"

	// ErrKeyPaymentProviderUnsupportedProvider is returned when the specified payment provider is not supported
	ErrKeyPaymentProviderUnsupportedProvider = "PaymentProviderUnsupportedProvider"

	// ErrKeyPaymentProviderNotFound is returned when the specified payment provider is not found in the registry
	ErrKeyPaymentProviderNotFound = "PaymentProviderNotFound"

	// ErrKeyPaymentProviderPayloadParsing is a parsing error, returned when the webhook payload cannot be parsed
	ErrKeyPaymentProviderPayloadParsing = "PaymentProviderPayloadParsing"

	// ErrKeyPaymentProviderMissingRequiredField is a parsing error, returned when a required field is missing from the webhook payload
	ErrKeyPaymentProviderMissingRequiredField = "PaymentProviderMissingRequiredField"

	// ErrKeyPaymentProviderInvalidEventType is a parsing error, returned when the event type in the webhook payload is not recognized
	ErrKeyPaymentProviderInvalidEventType = "PaymentProviderInvalidEventType"

	// ErrKeyPaymentProviderAPIRequestFailed is an API error, returned when an API request to the payment provider fails
	ErrKeyPaymentProviderAPIRequestFailed = "PaymentProviderAPIRequestFailed"

	// ErrKeyPaymentProviderAPIResponseInvalid is an API error, returned when the payment provider returns an invalid response
	ErrKeyPaymentProviderAPIResponseInvalid = "PaymentProviderAPIResponseInvalid"

	// ErrKeyPaymentProviderPriceMismatch is returned when provider pricing does not match the trusted application catalogue.
	ErrKeyPaymentProviderPriceMismatch = "PaymentProviderPriceMismatch"

	// ErrKeyPaymentProviderSubscriptionNotFound is an API error, returned when a subscription is not found
	ErrKeyPaymentProviderSubscriptionNotFound = "PaymentProviderSubscriptionNotFound"

	// ErrKeyPaymentProviderKofiNoSubscriptionAPI is returned when attempting to get subscription info from Ko-fi, which does not have a subscription API
	ErrKeyPaymentProviderKofiNoSubscriptionAPI = "PaymentProviderKofiNoSubscriptionAPI"

	// ErrKeyPaymentProviderMissingPayloadCustomerEmail is returned when the customer's email cannot be attained from provider
	ErrKeyPaymentProviderMissingPayloadCustomerEmail = "PaymentProviderMissingPayloadCustomerEmail"
)

// PaymentType constants for categorizing different types of payments
const (
	PaymentTypeSubscription = "subscription"
	PaymentTypePurchase     = "purchase"
	PaymentTypeDonation     = "donation"
	PaymentTypeShopOrder    = "shop_order"
	PaymentTypeCommission   = "commission"
)

// BillingKind describes whether a payment renews or is made once. It is kept
// separate from PaymentType so products such as donations can still describe
// what was purchased without being treated as plan access.
type BillingKind string

const (
	BillingKindRecurring BillingKind = "recurring"
	BillingKindOneTime   BillingKind = "one_time"
)

// PaymentStatus constants describe the state of an individual payment.
const (
	PaymentStatusSucceeded         = "succeeded"
	PaymentStatusFailed            = "failed"
	PaymentStatusRefunded          = "refunded"
	PaymentStatusPartiallyRefunded = "partially_refunded"
	PaymentStatusRefundFailed      = "refund_failed"
	PaymentStatusActionRequired    = "action_required"
	PaymentStatusNoPaymentRequired = "no_payment_required"
)

// CheckoutMode constants are provider-neutral checkout modes.
const (
	CheckoutModePayment      = "payment"
	CheckoutModeSubscription = "subscription"
)

// EventType constants for normalised event types across all providers
const (
	// Subscription events
	EventTypeSubscriptionCreated   = "subscription.created"
	EventTypeSubscriptionUpdated   = "subscription.updated"
	EventTypeSubscriptionCancelled = "subscription.cancelled"
	EventTypeSubscriptionPaused    = "subscription.paused"
	EventTypeSubscriptionResumed   = "subscription.resumed"

	// Donation-based subscription events (e.g., Ko-fi monthly donations)
	EventTypeSubscriptionCreatedDonation   = "subscription.created.donation"
	EventTypeSubscriptionUpdatedDonation   = "subscription.updated.donation"
	EventTypeSubscriptionCancelledDonation = "subscription.cancelled.donation"
	EventTypeSubscriptionPausedDonation    = "subscription.paused.donation"
	EventTypeSubscriptionResumedDonation   = "subscription.resumed.donation"

	// Payment events
	EventTypePaymentSucceeded         = "payment.succeeded"
	EventTypePaymentFailed            = "payment.failed"
	EventTypePaymentRefunded          = "payment.refunded"
	EventTypePaymentPartiallyRefunded = "payment.partially_refunded"
	EventTypePaymentRefundFailed      = "payment.refund_failed"
	EventTypePaymentActionRequired    = "payment.action_required"

	// Customer and trial events
	EventTypeCustomerUpdated = "customer.updated"
	EventTypeTrialWillEnd    = "trial.will_end"
	EventTypeTrialEnded      = "trial.ended"
)

// SubscriptionStatus constants for normalised statuses across all providers
const (
	SubscriptionStatusActive     = "active"
	SubscriptionStatusTrialing   = "trialing"
	SubscriptionStatusPastDue    = "past_due"
	SubscriptionStatusCancelled  = "cancelled"
	SubscriptionStatusPaused     = "paused"
	SubscriptionStatusExpired    = "expired"
	SubscriptionStatusIncomplete = "incomplete"
	SubscriptionStatusUnpaid     = "unpaid"
)
