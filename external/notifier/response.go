package notifier

// RegisterAddressResponse holds the registered notification address.
type RegisterAddressResponse struct {
	Address NotificationAddressSummary `json:"address"`
}

// GetActiveNotificationAddressesResponse holds active full notification addresses.
type GetActiveNotificationAddressesResponse struct {
	Addresses []NotificationAddress `json:"addresses"`
}

// ListNotificationAddressesResponse holds safe user notification addresses.
type ListNotificationAddressesResponse struct {
	Addresses []NotificationAddressSummary `json:"addresses"`
}

// GetNotificationPreferencesResponse holds user notification preferences.
type GetNotificationPreferencesResponse struct {
	Preferences *NotificationPreferences `json:"preferences"`
}

// UpdateNotificationPreferencesResponse holds user notification preferences.
type UpdateNotificationPreferencesResponse struct {
	Preferences *NotificationPreferences `json:"preferences"`
}

// GetNotifierConfigResponse holds client-safe notifier config.
type GetNotifierConfigResponse struct {
	Config *NotifierConfig `json:"config"`
}

// NotificationSendResult records one channel send attempt.
type NotificationSendResult struct {
	Channel   NotificationChannel `json:"channel"`
	Attempted int                 `json:"attempted"`
	Sent      bool                `json:"sent"`
	Skipped   bool                `json:"skipped"`
	Error     string              `json:"error,omitempty"`
}

// NotifyUserResponse holds admin/service notification send results.
type NotifyUserResponse struct {
	Results []NotificationSendResult `json:"results"`
}
