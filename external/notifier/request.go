package notifier

// RegisterAddressRequest holds a client notification registration payload.
type RegisterAddressRequest struct {
	UserID     string              `json:"-" validate:"required"`
	Channel    NotificationChannel `json:"channel" validate:"required"`
	DeviceID   string              `json:"device_id,omitempty"`
	DeviceName string              `json:"device_name,omitempty"`
	Platform   string              `json:"platform,omitempty"`
	WebPush    *WebPushAddress     `json:"webpush,omitempty"`
	FCM        *FCMAddress         `json:"fcm,omitempty"`
}

// GetActiveNotificationAddressesRequest holds user address lookup filters.
type GetActiveNotificationAddressesRequest struct {
	UserID   string                `validate:"required"`
	Channels []NotificationChannel `json:"channels,omitempty"`
}

// ListNotificationAddressesRequest holds the user address list request.
type ListNotificationAddressesRequest struct {
	UserID string `validate:"required"`
}

// DeleteNotificationAddressRequest identifies a user-owned address to delete.
type DeleteNotificationAddressRequest struct {
	UserID    string `validate:"required"`
	AddressID string `validate:"required"`
}

// GetNotificationPreferencesRequest identifies a user's preference document.
type GetNotificationPreferencesRequest struct {
	UserID string `validate:"required"`
}

// UpdateNotificationPreferencesRequest holds preference updates.
type UpdateNotificationPreferencesRequest struct {
	UserID   string          `json:"-" validate:"required"`
	Enabled  *bool           `json:"enabled,omitempty"`
	Channels map[string]bool `json:"channels,omitempty"`
}

// GetNotifierConfigRequest holds config lookup options.
type GetNotifierConfigRequest struct{}

// NotifyUserRequest holds an admin/service notification send request.
type NotifyUserRequest struct {
	UserID   string                 `json:"-" validate:"required"`
	Title    string                 `json:"title" validate:"required"`
	Message  string                 `json:"message" validate:"required"`
	Channels []NotificationChannel  `json:"channels,omitempty"`
	Data     map[string]interface{} `json:"data,omitempty"`
}
