package notifier

import "github.com/ooaklee/ghatd/external/toolbox"

// RegisterAddressResponse wraps the sanitised address that was registered.
//
// The response returns a NotificationAddressSummary, not the full
// NotificationAddress. This means secrets like the Push API endpoint
// and encryption keys are never included in the API response.
type RegisterAddressResponse struct {
	Address NotificationAddressSummary `json:"address"`
}

// GetActiveNotificationAddressesResponse contains full notification
// addresses for server-side use.
//
// This is an internal response type used when the NotifyUser flow needs
// to read the actual endpoint data to deliver notifications. It is
// never returned directly to clients.
type GetActiveNotificationAddressesResponse struct {
	Addresses []NotificationAddress `json:"addresses"`
}

// ListNotificationAddressesResponse returns sanitised notification
// addresses that are safe to show to the owning user.
//
// Each address in the list has had its endpoint and token fields
// stripped. The user can see their device name, platform, channel,
// and when it was last seen, but not the secrets needed to send.
type ListNotificationAddressesResponse struct {
	Addresses  []NotificationAddressSummary `json:"addresses"`
	Total      int                          `json:"-"`
	TotalPages int                          `json:"-"`
	PerPage    int                          `json:"-"`
	Page       int                          `json:"-"`
}

// GetMetaData returns pagination metadata in the reply.WithMeta format.
func (r *ListNotificationAddressesResponse) GetMetaData() map[string]interface{} {
	return map[string]interface{}{
		string(toolbox.ResponseMetaKeyResourcePerPage): r.PerPage,
		string(toolbox.ResponseMetaKeyTotalResources):  r.Total,
		string(toolbox.ResponseMetaKeyTotalPages):      r.TotalPages,
		string(toolbox.ResponseMetaKeyPage):            r.Page,
	}
}

// GetNotificationPreferencesResponse returns a user's current
// notification preferences.
//
// If the user has never set preferences before, the response contains
// the default values (everything enabled).
type GetNotificationPreferencesResponse struct {
	Preferences *NotificationPreferences `json:"preferences"`
}

// UpdateNotificationPreferencesResponse returns the preference
// document after the update has been applied.
type UpdateNotificationPreferencesResponse struct {
	Preferences *NotificationPreferences `json:"preferences"`
}

// GetNotifierConfigResponse carries the public configuration that
// clients use to decide whether they can register for push.
type GetNotifierConfigResponse struct {
	Config *NotifierConfig `json:"config"`
}

// NotificationSendResult reports what happened when the notifier
// tried to deliver to one channel.
//
//   - Attempted: how many addresses were targeted on this channel.
//   - Sent: true if at least one address was delivered successfully
//     (cleaned-up expired addresses do not count as delivered).
//   - Cleaned: how many permanently invalid addresses were auto-disabled
//     during this send attempt.
//   - Skipped: true if the sender was not enabled (e.g. FCM without
//     Firebase credentials configured).
//   - Error: the error message for the send, if any.
type NotificationSendResult struct {
	Channel   NotificationChannel `json:"channel"`
	Attempted int                 `json:"attempted"`
	Sent      bool                `json:"sent"`
	Cleaned   int                 `json:"cleaned,omitempty"`
	Skipped   bool                `json:"skipped"`
	Error     string              `json:"error,omitempty"`
}

// NotifyUserResponse contains one result per channel that the notifier
// attempted to deliver to.
//
// A single NotifyUserRequest may result in delivery attempts across
// multiple channels (e.g. both WEBPUSH and FCM), so the response
// contains a list of per-channel results rather than a single outcome.
type NotifyUserResponse struct {
	Results []NotificationSendResult `json:"results"`
}
