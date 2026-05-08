package notifier

const (
	// NotificationAddressesCollection stores registered notification destinations.
	NotificationAddressesCollection = "notification_addresses"

	// NotificationPreferencesCollection stores user-level notification preferences.
	NotificationPreferencesCollection = "notification_preferences"

	defaultCollectionInitMaxAttemptsLimit = 3
)

const (
	// NotificationChannelWebPush represents a browser Push API subscription.
	NotificationChannelWebPush NotificationChannel = "WEBPUSH"

	// NotificationChannelFCM represents a Firebase Cloud Messaging token.
	NotificationChannelFCM NotificationChannel = "FCM"
)

const (
	// NotificationAddressStatusActive means the destination can receive notifications.
	NotificationAddressStatusActive NotificationAddressStatus = "ACTIVE"

	// NotificationAddressStatusDisabled means the destination is retained but should not receive notifications.
	NotificationAddressStatusDisabled NotificationAddressStatus = "DISABLED"
)

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
