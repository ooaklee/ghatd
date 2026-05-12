package notifier

// RegisterAddressRequest is what a client sends when they want to register
// a new notification destination (a browser tab, a mobile phone, etc.).
//
// The UserID field comes from the authenticated context (set by UMS), never
// from the client body. This prevents users from registering devices under
// someone else's account.
//
// The Channel field selects WEBPUSH or FCM. Depending on the channel, you
// must also provide the matching channel-specific payload:
//
//   - For WEBPUSH: include the WebPush field with endpoint and encryption keys.
//   - For FCM: include the FCM field with the device token.
//
// DeviceID, DeviceName, and Platform are optional but help users identify
// their registered devices later.
type RegisterAddressRequest struct {
	UserID     string              `json:"-" validate:"required"`
	Channel    NotificationChannel `json:"channel" validate:"required"`
	DeviceID   string              `json:"device_id,omitempty"`
	DeviceName string              `json:"device_name,omitempty"`
	Platform   string              `json:"platform,omitempty"`
	WebPush    *WebPushAddress     `json:"webpush,omitempty"`
	FCM        *FCMAddress         `json:"fcm,omitempty"`
}

// GetActiveNotificationAddressesRequest filters a lookup of a user's
// active addresses by optional channels.
//
// This is an internal request type used by the NotifyUser flow, not
// exposed as an API endpoint. It asks "give me all active addresses
// for this user – optionally filtered to specific channels."
type GetActiveNotificationAddressesRequest struct {
	UserID   string                `validate:"required"`
	Channels []NotificationChannel `json:"channels,omitempty"`
}

// ListNotificationAddressesRequest asks for a user's registered devices.
//
// The returned list only contains sanitised summaries – the Push API
// endpoints and FCM tokens are stripped out before the response reaches
// the client.
type ListNotificationAddressesRequest struct {
	UserID  string                    `json:"user_id,omitempty" query:"user_id"`
	Channel NotificationChannel       `json:"channel,omitempty" query:"channel"`
	Status  NotificationAddressStatus `json:"status,omitempty" query:"status"`

	PerPage int  `json:"per_page,omitempty" query:"per_page"`
	Page    int  `json:"page,omitempty" query:"page"`
	Meta    bool `json:"meta,omitempty" query:"meta"`
}

// DeleteNotificationAddressRequest identifies which address a user wants
// to remove.
//
// Both UserID and AddressID are required. The repository checks that the
// address actually belongs to the user before deleting, so a user cannot
// delete another user's addresses.
type DeleteNotificationAddressRequest struct {
	UserID    string `validate:"required"`
	AddressID string `validate:"required"`
}

// GetNotificationPreferencesRequest asks for a user's current notification
// preferences.
//
// If no preferences document exists yet, the service returns the defaults
// (everything enabled) instead of an error.
type GetNotificationPreferencesRequest struct {
	UserID string `validate:"required"`
}

// UpdateNotificationPreferencesRequest contains the changes a user wants
// to make to their notification settings.
//
// The UserID field comes from the authenticated context. The Enabled field
// is a pointer to a bool so a client can leave it nil (meaning "don't change
// the global enabled toggle") or set it to true/false.
//
// The Channels map uses string keys (the channel name, e.g. "WEBPUSH")
// and bool values (true = enabled, false = disabled). Unknown channel
// names are rejected.
type UpdateNotificationPreferencesRequest struct {
	UserID   string          `json:"-" validate:"required"`
	Enabled  *bool           `json:"enabled,omitempty"`
	Channels map[string]bool `json:"channels,omitempty"`
}

// GetNotifierConfigRequest asks for the public notifier configuration.
//
// This request has no fields because the config is global per server.
type GetNotifierConfigRequest struct{}

// NotifyUsersRequest is sent by an admin to create a notification dispatch
// for zero or more users across zero or more channels.
//
// When UserIDs is empty, the service resolves every user that has at
// least one active notification address and delivers to all of them.
//
// When Channels is empty, the service delivers to every supported
// channel (WEBPUSH and FCM) that the user has active addresses for.
//
// Title and Message are required. Data carries optional key-value pairs
// forwarded to the push payload for client-side handling.
type NotifyUsersRequest struct {
	UserIDs  []string               `json:"user_ids,omitempty"`
	Title    string                 `json:"title" validate:"required"`
	Message  string                 `json:"message" validate:"required"`
	Channels []NotificationChannel  `json:"channels,omitempty"`
	Data     map[string]interface{} `json:"data,omitempty"`
}

// NotifyUserRequest is sent by an admin or an internal service to send
// a notification to a specific user.
//
// The UserID field comes from the admin-configured target, not from
// the authenticated context (admins can send to any user).
//
// Title and Message are required – these become the notification headline
// and body text.
//
// The optional Channels field limits delivery to specific channels. If
// empty, the notification goes to all active channels according to the
// user's preferences.
//
// The optional Data map carries extra key-value pairs that are forwarded
// to the notification payload for client-side handling (e.g. a URL to
// open when the user taps the notification).
type NotifyUserRequest struct {
	UserID   string                 `json:"-" validate:"required"`
	Title    string                 `json:"title" validate:"required"`
	Message  string                 `json:"message" validate:"required"`
	Channels []NotificationChannel  `json:"channels,omitempty"`
	Data     map[string]interface{} `json:"data,omitempty"`
}
