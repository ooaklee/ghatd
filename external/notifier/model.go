package notifier

import "strings"

// NotificationChannel identifies a notification delivery provider.
type NotificationChannel string

// NotificationAddressStatus identifies whether a notification destination is usable.
type NotificationAddressStatus string

// WebPushKeys stores the browser Push API subscription keys.
type WebPushKeys struct {
	P256DH string `json:"p256dh" bson:"p256dh"`
	Auth   string `json:"auth" bson:"auth"`
}

// WebPushAddress stores a browser Push API subscription.
type WebPushAddress struct {
	Endpoint string      `json:"endpoint" bson:"endpoint"`
	Keys     WebPushKeys `json:"keys" bson:"keys"`
}

// FCMAddress stores a Firebase Cloud Messaging device token.
type FCMAddress struct {
	Token string `json:"token" bson:"token"`
}

// NotificationAddressMetadata stores lifecycle timestamps for a notification address.
type NotificationAddressMetadata struct {
	CreatedAt  string `json:"created_at,omitempty" bson:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	LastSeenAt string `json:"last_seen_at,omitempty" bson:"last_seen_at,omitempty"`
}

// NotificationAddress links one notification destination to a user.
type NotificationAddress struct {
	ID          string                       `json:"id" bson:"_id"`
	UserID      string                       `json:"user_id" bson:"user_id"`
	Channel     NotificationChannel          `json:"channel" bson:"channel"`
	Status      NotificationAddressStatus    `json:"status" bson:"status"`
	AddressHash string                       `json:"-" bson:"address_hash"`
	DeviceID    string                       `json:"device_id,omitempty" bson:"device_id,omitempty"`
	DeviceName  string                       `json:"device_name,omitempty" bson:"device_name,omitempty"`
	Platform    string                       `json:"platform,omitempty" bson:"platform,omitempty"`
	WebPush     *WebPushAddress              `json:"webpush,omitempty" bson:"webpush,omitempty"`
	FCM         *FCMAddress                  `json:"fcm,omitempty" bson:"fcm,omitempty"`
	Metadata    *NotificationAddressMetadata `json:"metadata,omitempty" bson:"metadata,omitempty"`
}

// NotificationAddressSummary is the safe representation returned to clients.
type NotificationAddressSummary struct {
	ID         string                    `json:"id"`
	Channel    NotificationChannel       `json:"channel"`
	Status     NotificationAddressStatus `json:"status"`
	DeviceID   string                    `json:"device_id,omitempty"`
	DeviceName string                    `json:"device_name,omitempty"`
	Platform   string                    `json:"platform,omitempty"`
	CreatedAt  string                    `json:"created_at,omitempty"`
	UpdatedAt  string                    `json:"updated_at,omitempty"`
	LastSeenAt string                    `json:"last_seen_at,omitempty"`
}

// NotificationPreferencesMetadata stores lifecycle timestamps for user preferences.
type NotificationPreferencesMetadata struct {
	CreatedAt string `json:"created_at,omitempty" bson:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
}

// NotificationPreferences stores user-level notification switches.
type NotificationPreferences struct {
	UserID   string                           `json:"user_id" bson:"_id"`
	Enabled  bool                             `json:"enabled" bson:"enabled"`
	Channels map[string]bool                  `json:"channels" bson:"channels"`
	Metadata *NotificationPreferencesMetadata `json:"metadata,omitempty" bson:"metadata,omitempty"`
}

// NotifierConfig exposes client-safe registration configuration.
type NotifierConfig struct {
	SupportedChannels []NotificationChannel `json:"supported_channels"`
	WebPush           WebPushClientConfig   `json:"webpush"`
	FCM               FCMClientConfig       `json:"fcm"`
}

// WebPushClientConfig contains public Web Push registration config.
type WebPushClientConfig struct {
	Enabled        bool   `json:"enabled"`
	VAPIDPublicKey string `json:"vapid_public_key,omitempty"`
}

// FCMClientConfig contains public FCM registration config.
type FCMClientConfig struct {
	Enabled bool `json:"enabled"`
}

// Sanitise returns a client-safe address without endpoint/token secrets.
func (a NotificationAddress) Sanitise() NotificationAddressSummary {
	summary := NotificationAddressSummary{
		ID:         a.ID,
		Channel:    a.Channel,
		Status:     a.Status,
		DeviceID:   a.DeviceID,
		DeviceName: a.DeviceName,
		Platform:   a.Platform,
	}

	if a.Metadata != nil {
		summary.CreatedAt = a.Metadata.CreatedAt
		summary.UpdatedAt = a.Metadata.UpdatedAt
		summary.LastSeenAt = a.Metadata.LastSeenAt
	}

	return summary
}

// Normalised returns a canonical channel value.
func (c NotificationChannel) Normalised() NotificationChannel {
	return NotificationChannel(strings.ToUpper(strings.TrimSpace(string(c))))
}

// IsSupported returns true when the channel is supported by the notifier package.
func (c NotificationChannel) IsSupported() bool {
	switch c.Normalised() {
	case NotificationChannelWebPush, NotificationChannelFCM:
		return true
	default:
		return false
	}
}

// DefaultNotificationPreferences returns the default opt-in preference document.
func DefaultNotificationPreferences(userID string) *NotificationPreferences {
	return &NotificationPreferences{
		UserID:  userID,
		Enabled: true,
		Channels: map[string]bool{
			string(NotificationChannelWebPush): true,
			string(NotificationChannelFCM):     true,
		},
	}
}
