package notifier

import "errors"

// Sentinel errors for the notifier package.
//
// Each error represents one specific thing that can go wrong. These are
// used as sentinel values – code checks for them with errors.Is() to
// understand exactly what kind of failure happened.
//
// The error manifest in errormap.go maps each sentinel to an HTTP status
// code and a user-friendly API error response, so API handlers never need
// to write custom error marshalling logic.
var (
	// ErrDatabaseError means the database operation failed for an unexpected
	// reason (connection problem, timeout, constraint violation, etc.).
	ErrDatabaseError = errors.New(ErrKeyDatabaseError)

	// ErrInvalidNotificationAddressBody means the client sent an address
	// registration payload that is missing required fields or contains
	// data in the wrong shape.
	//
	// Examples: missing endpoint for a Web Push address, missing token for
	// an FCM address, or keys that are empty when they need to be present.
	ErrInvalidNotificationAddressBody = errors.New(ErrKeyInvalidNotificationAddressBody)

	// ErrInvalidNotificationChannel means the channel name in the request
	// is not one that the notifier package supports.
	ErrInvalidNotificationChannel = errors.New(ErrKeyInvalidNotificationChannel)

	// ErrInvalidNotificationPreferences means the preferences update payload
	// contains channel names that the package does not recognise.
	ErrInvalidNotificationPreferences = errors.New(ErrKeyInvalidNotificationPreferences)

	// ErrNotificationAddressNotFound means the requested address does not
	// exist or does not belong to the requesting user.
	ErrNotificationAddressNotFound = errors.New(ErrKeyNotificationAddressNotFound)

	// ErrNotificationNoActiveAddresses means NotifyUser was called for a
	// user who has no active, ready-to-send addresses registered.
	ErrNotificationNoActiveAddresses = errors.New(ErrKeyNotificationNoActiveAddresses)

	// ErrNotificationSenderNotEnabled means the server has not configured
	// a sender for the requested channel, so delivery is not possible.
	//
	// This can happen if VAPID keys or Firebase credentials are missing
	// from the deployment environment.
	ErrNotificationSenderNotEnabled = errors.New(ErrKeyNotificationSenderNotEnabled)

	// ErrNotificationSendFailed means one or more notification deliveries
	// failed after the send was attempted. The error wraps the individual
	// sender errors so callers can inspect the details.
	ErrNotificationSendFailed = errors.New(ErrKeyNotificationSendFailed)

	// ErrNotificationUserIDRequired means a user ID was empty or missing
	// from a request that requires one.
	ErrNotificationUserIDRequired = errors.New(ErrKeyNotificationUserIDRequired)
)
