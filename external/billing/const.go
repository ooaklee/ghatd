package billing

// Error key definitions for billing operations
const (

	// ErrKeyBillingSubscriptionNotFound is returned when a subscription cannot be found
	ErrKeyBillingSubscriptionNotFound = "BillingSubscriptionNotFound"

	// ErrKeyBillingEventNotFound is returned when a billing event cannot be found
	ErrKeyBillingEventNotFound = "BillingEventNotFound"

	// ErrKeyBillingInvalidSubscriptionID is returned when the provided subscription ID is invalid
	ErrKeyBillingInvalidSubscriptionID = "BillingInvalidSubscriptionID"

	// ErrKeyBillingInvalidUserID is returned when the provided user ID is invalid
	ErrKeyBillingInvalidUserID = "BillingInvalidUserID"

	// ErrKeyBillingInvalidEmail is returned when the provided email address is invalid
	ErrKeyBillingInvalidEmail = "BillingInvalidEmail"

	// ErrKeyBillingInvalidStatus is returned when the provided subscription status is invalid
	ErrKeyBillingInvalidStatus = "BillingInvalidStatus"

	// ErrKeyBillingMissingRequiredField is returned when a required field is missing
	ErrKeyBillingMissingRequiredField = "BillingMissingRequiredField"

	// ErrKeyBillingInvalidIntegrator is returned when the provided payment provider integrator is invalid
	ErrKeyBillingInvalidIntegrator = "BillingInvalidIntegrator"

	// ErrKeyBillingInvalidAmount is returned when the provided amount is invalid
	ErrKeyBillingInvalidAmount = "BillingInvalidAmount"

	// ErrKeyBillingInvalidCurrency is returned when the provided currency code is invalid
	ErrKeyBillingInvalidCurrency = "BillingInvalidCurrency"

	// ErrKeyBillingSubscriptionAlreadyExists is returned when attempting to create a subscription that already exists
	ErrKeyBillingSubscriptionAlreadyExists = "BillingSubscriptionAlreadyExists"

	// ErrKeyBillingEventAlreadyProcessed is returned when attempting to process a billing event that has already been processed
	ErrKeyBillingEventAlreadyProcessed = "BillingEventAlreadyProcessed"

	// ErrKeyBillingDuplicateIntegratorID is returned when a subscription with the same integrator ID already exists
	ErrKeyBillingDuplicateIntegratorID = "BillingDuplicateIntegratorID"

	// ErrKeyBillingFailedToCreateSubscription is returned when creating a subscription on the internal system fails
	ErrKeyBillingFailedToCreateSubscription = "BillingFailedToCreateSubscription"

	// ErrKeyBillingFailedToUpdateSubscription is returned when updating a subscription fails on the internal system
	ErrKeyBillingFailedToUpdateSubscription = "BillingFailedToUpdateSubscription"

	// ErrKeyBillingFailedToDeleteSubscription is returned when deleting a subscription fails on the internal system
	ErrKeyBillingFailedToDeleteSubscription = "BillingFailedToDeleteSubscription"

	// ErrKeyBillingFailedToCreateEvent is returned when creating a billing event fails on the internal system
	ErrKeyBillingFailedToCreateEvent = "BillingFailedToCreateEvent"

	// ErrKeyBillingFailedToGetSubscriptions is returned when retrieving subscriptions from the internal system fails
	ErrKeyBillingFailedToGetSubscriptions = "BillingFailedToGetSubscriptions"

	// ErrKeyBillingFailedToGetEvents is returned when retrieving billing events from the internal system fails
	ErrKeyBillingFailedToGetEvents = "BillingFailedToGetEvents"

	// ErrKeyBillingUnauthorisedAccess is returned when unauthorised access to billing information is attempted
	ErrKeyBillingUnauthorisedAccess = "BillingUnauthorisedAccess"

	// ErrKeyBillingForbiddenOperation is returned when a forbidden billing operation is attempted
	ErrKeyBillingForbiddenOperation = "BillingForbiddenOperation"

	// ErrKeyBillingNoSubscriptionsFoundForEmail is returned when no subscriptions are found for an email
	ErrKeyBillingNoSubscriptionsFoundForEmail = "BillingNoSubscriptionsFoundForEmail"

	// ErrKeyBillingNoEventsFoundForEmail is returned when no billing events are found for an email
	ErrKeyBillingNoEventsFoundForEmail = "BillingNoEventsFoundForEmail"

	// ErrKeyBillingAssociationFailed is returned when subscription association fails
	ErrKeyBillingAssociationFailed = "BillingAssociationFailed"

	// ErrKeyBillingNoUnassociatedSubscriptionsFound is returned when no unassociated subscriptions are found
	ErrKeyBillingNoUnassociatedSubscriptionsFound = "BillingNoUnassociatedSubscriptionsFound"

	// ErrKeyBillingUpdateUserIDFailed is returned when updating user ID fails
	ErrKeyBillingUpdateUserIDFailed = "BillingUpdateUserIDFailed"
)

// Subscription status constants
const (
	// StatusActive indicates an active subscription
	StatusActive = "active"

	// StatusTrialing indicates a subscription in trial period
	StatusTrialing = "trialing"

	// StatusPastDue indicates a subscription with past due payment
	StatusPastDue = "past_due"

	// StatusCancelled indicates a cancelled subscription
	StatusCancelled = "cancelled"

	// StatusPaused indicates a paused subscription
	StatusPaused = "paused"

	// StatusExpired indicates an expired subscription
	StatusExpired = "expired"

	// StatusIncomplete indicates an incomplete subscription
	StatusIncomplete = "incomplete"

	// StatusUnpaid indicates an unpaid subscription
	StatusUnpaid = "unpaid"
)
