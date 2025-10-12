package billingmanager

import "github.com/ooaklee/ghatd/external/audit"

const (
	// AuditActionBillingWebhookProcessed occurs when a billing webhook is processed
	AuditActionBillingWebhookProcessed audit.AuditAction = "BILLING_WEBHOOK_PROCESSED"

	// TargetTypeWebhook represents webhook event
	TargetTypeWebhook audit.TargetType = "WEBHOOK"
)

const (

	// ErrKeyBillingManagerUnableToGetProviderNameFromURI is returned when the provider name cannot be extracted from the URI
	ErrKeyBillingManagerUnableToGetProviderNameFromURI = "BillingManagerUnableToGetProviderNameFromURI"

	// ErrKeyBillingManagerUnableToIdentifyUser is returned when the user cannot be identified from the request context
	ErrKeyBillingManagerUnableToIdentifyUser = "BillingManagerUnableToIdentifyUser"

	// ErrKeyBillingManagerUnableToGetUserIdFromURI is returned when the user ID cannot be extracted from the URI
	ErrKeyBillingManagerUnableToGetUserIdFromURI = "BillingManagerUnableToGetUserIdFromURI"

	// ErrKeyInvalidBillingManagerRequestPayload is returned when the request payload is invalid
	ErrKeyInvalidBillingManagerRequestPayload = "InvalidBillingManagerRequestPayload"

	// ErrKeyBillingManagerFailedWebhookVerification is returned when webhook verification fails
	ErrKeyBillingManagerFailedWebhookVerification = "BillingManagerFailedWebhookVerification"

	// ErrKeyBillingManagerFailedToProcessEvent is returned when processing a billing event fails
	ErrKeyBillingManagerFailedToProcessEvent = "BillingManagerFailedToProcessEvent"

	// ErrKeyBillingManagerFailedToRetrieveSubscriptionStatus is returned when retrieving subscription status fails
	ErrKeyBillingManagerFailedToRetrieveSubscriptionStatus = "BillingManagerFailedToRetrieveSubscriptionStatus"

	// ErrKeyBillingManagerFailedToRetrieveBillingEvents is returned when retrieving billing events fails
	ErrKeyBillingManagerFailedToRetrieveBillingEvents = "BillingManagerFailedToRetrieveBillingEvents"

	// ErrKeyBillingManagerUnableToResolveUserId is returned when unable to find a subscription that relates to a subscription
	// or user ID from the payload email
	ErrKeyBillingManagerUnableToResolveUserId = "BillingManagerUnableToResolveUserId"

	// ErrKeyBillingManagerRequiresUserIdIsMissing is returned when a user ID is not provided
	ErrKeyBillingManagerRequiresUserIdIsMissing = "BillingManagerRequiresUserIdIsMissing"

	// ErrKeyBillingManagerUserUnauthorisedToCarryOutOperation is returned when a user is not authorised to perform an operation
	ErrKeyBillingManagerUserUnauthorisedToCarryOutOperation = "BillingManagerUserUnauthorisedToCarryOutOperation"

	// ErrKeyBillingManagerNoUserIdentifyingInformationInPayload is returned when unable to find a user from the provider's payload as no email is present
	// so we have no way to identify the user
	ErrKeyBillingManagerNoUserIdentifyingInformationInPayload = "BillingManagerNoUserIdentifyingInformationInPayload"
)
