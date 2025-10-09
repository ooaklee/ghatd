package paymentprovider

import (
	"context"
	"net/http"
)

// Provider defines the interface that all payment providers must implement
// This abstraction allows the billing system to work with multiple payment providers
// (Paddle, Stripe, Lemon Squeezy, Ko-fi, etc.) using a common interface
type Provider interface {
	// GetProviderName returns the unique identifier for this payment provider
	// e.g., "paddle", "stripe", "lemonsqueezy", "kofi"
	GetProviderName() string

	// VerifyWebhook verifies the authenticity of an incoming webhook request
	// It checks the signature/headers to ensure the webhook came from the payment provider
	// Returns error if verification fails
	VerifyWebhook(ctx context.Context, req *http.Request) error

	// ParsePayload extracts and normalizes webhook data from the provider's format
	// into a common WebhookPayload structure that the billing system can work with
	ParsePayload(ctx context.Context, req *http.Request) (*WebhookPayload, error)

	// GetSubscriptionInfo retrieves current subscription details from the provider's API
	// This is useful for syncing state or retrieving information not in webhooks
	GetSubscriptionInfo(ctx context.Context, subscriptionID string) (*SubscriptionInfo, error)
}

// EventType constants for normalised event types across all providers
const (
	EventTypeSubscriptionCreated   = "subscription.created"
	EventTypeSubscriptionUpdated   = "subscription.updated"
	EventTypeSubscriptionCancelled = "subscription.cancelled"
	EventTypeSubscriptionPaused    = "subscription.paused"
	EventTypeSubscriptionResumed   = "subscription.resumed"
	EventTypePaymentSucceeded      = "payment.succeeded"
	EventTypePaymentFailed         = "payment.failed"
	EventTypePaymentRefunded       = "payment.refunded"
	EventTypePaymentActionRequired = "payment.action_required"
	EventTypeCustomerUpdated       = "customer.updated"
	EventTypeTrialWillEnd          = "trial.will_end"
	EventTypeTrialEnded            = "trial.ended"
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
