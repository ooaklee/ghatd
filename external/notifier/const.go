package notifier

const (
	// NotificationAddressesCollection is the MongoDB collection that stores every
	// notification destination registered by a user – browser tabs, mobile phones,
	// and any future push channels.
	//
	// The collection uses a unique index on (channel, address_hash) so the same
	// browser or device always moves to the latest signed-in user rather than
	// creating duplicate address records.
	NotificationAddressesCollection = "notification_addresses"

	// NotificationPreferencesCollection is the MongoDB collection that stores
	// each user's notification choices – whether notifications are enabled at all,
	// and which channels they want to hear from.
	//
	// Documents are keyed by user ID, so each user has exactly one preferences
	// document.
	NotificationPreferencesCollection = "notification_preferences"

	// defaultCollectionInitMaxAttemptsLimit controls how many times the repository
	// will retry its MongoDB collection initialisation before giving up.
	// This makes the notifier package resilient to brief database connection
	// hiccups during startup without looping forever.
	defaultCollectionInitMaxAttemptsLimit = 3
)

const (
	// NotificationChannelWebPush represents a browser Push API subscription.
	//
	// When a user clicks "Enable notifications" in the web app, the browser creates
	// a Web Push subscription. The subscription's endpoint and encryption keys
	// are stored so GHATD can deliver push messages directly to that browser.
	NotificationChannelWebPush NotificationChannel = "WEBPUSH"

	// NotificationChannelFCM represents a Firebase Cloud Messaging device token.
	//
	// Mobile apps receive a unique FCM token from Firebase that identifies the
	// specific app installation. GHATD stores this token so it can route push
	// notifications through Firebase to reach that device.
	NotificationChannelFCM NotificationChannel = "FCM"
)

const (
	// NotificationAddressStatusActive means "this destination is alive and ready
	// to receive push notifications."
	//
	// An address stays ACTIVE as long as the device continues to register.
	// Active addresses are the only ones the service considers when it sends
	// a notification.
	NotificationAddressStatusActive NotificationAddressStatus = "ACTIVE"

	// NotificationAddressStatusDisabled means "keep this address on file but
	// don't send notifications to it."
	//
	// This is useful when a user wants to temporarily stop push to a specific
	// device without deleting the registration entirely.
	NotificationAddressStatusDisabled NotificationAddressStatus = "DISABLED"
)

// Error key constants give each sentinel error a stable, human-readable name.
//
// These keys are used by the error manifest (errormap.go) to map errors to
// API status codes and user-facing messages. By centralising error keys here,
// we make it easy to see all the things that can go wrong in the notifier
// package in one place.
const (
	ErrKeyDatabaseError                  = "NotifierDatabaseError"
	ErrKeyInvalidNotificationAddressBody = "InvalidNotificationAddressBody"
	ErrKeyInvalidNotificationChannel     = "InvalidNotificationChannel"
	ErrKeyInvalidNotificationPreferences = "InvalidNotificationPreferences"
	ErrKeyNotificationAddressNotFound    = "NotificationAddressNotFound"
	ErrKeyNotificationNoActiveAddresses  = "NotificationNoActiveAddresses"
	ErrKeyNotificationSenderNotEnabled   = "NotificationSenderNotEnabled"
	ErrKeyNotificationSendFailed         = "NotificationSendFailed"
	ErrKeyNotificationUserIDRequired     = "NotificationUserIDRequired"
)
