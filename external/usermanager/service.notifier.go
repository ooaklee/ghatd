package usermanager

import (
	"context"
)

// ensureNotifierService returns an error if the notifier service has not been
// wired into UMS (e.g. the deployment does not have notification features
// enabled). Every notifier-related UMS method calls this first so the API
// returns a clean "Service Unavailable" response instead of panicking or
// returning confusing errors.
func (s *Service) ensureNotifierService() error {
	if s.NotifierService == nil {
		return ErrNotifierServiceNotEnabled
	}
	return nil
}

// GetNotifierConfig returns the public notifier configuration through UMS.
//
// This endpoint tells client apps (browsers and mobile apps) which push
// channels are available and what keys they need to subscribe. It does not
// require a specific user ID because the config is the same for everyone.
//
// The caller is authenticated, but the config itself is not user-specific –
// it just describes what the server supports.
func (s *Service) GetNotifierConfig(ctx context.Context, r *GetNotifierConfigRequest) (*GetNotifierConfigResponse, error) {
	if err := s.ensureNotifierService(); err != nil {
		return nil, err
	}

	response, err := s.NotifierService.GetConfig(ctx, r.GetNotifierConfigRequest)
	if err != nil {
		return nil, err
	}

	return &GetNotifierConfigResponse{GetNotifierConfigResponse: response}, nil
}

// RegisterNotificationAddress registers a push notification destination
// (a browser tab or mobile device) for the currently authenticated user.
//
// What you send depends on the channel:
//
//   - WEBPUSH: the browser Push API subscription (endpoint + encryption keys).
//   - FCM: the mobile device's Firebase Cloud Messaging token.
//
// The user ID is taken from the auth context, never from the request body,
// so a user cannot register a device under someone else's account.
//
// The response returns a sanitised summary – endpoint URLs and tokens are
// stripped out so secrets never leave the server over the API.
func (s *Service) RegisterNotificationAddress(ctx context.Context, r *RegisterNotificationAddressRequest) (*RegisterNotificationAddressResponse, error) {
	if err := s.ensureNotifierService(); err != nil {
		return nil, err
	}

	response, err := s.NotifierService.RegisterAddress(ctx, r.RegisterAddressRequest)
	if err != nil {
		return nil, err
	}

	return &RegisterNotificationAddressResponse{RegisterAddressResponse: response}, nil
}

// ListNotificationAddresses returns the current user's registered push
// destinations in a safe, client-friendly format.
//
// Each entry shows the channel, device name, platform, status, and
// timestamps, but never the actual push endpoint or token. This keeps
// the secrets server-side while still letting the user see and manage
// their registered devices.
func (s *Service) ListNotificationAddresses(ctx context.Context, r *ListNotificationAddressesRequest) (*ListNotificationAddressesResponse, error) {
	if err := s.ensureNotifierService(); err != nil {
		return nil, err
	}

	response, err := s.NotifierService.ListUserAddresses(ctx, r.ListNotificationAddressesRequest)
	if err != nil {
		return nil, err
	}

	return &ListNotificationAddressesResponse{ListNotificationAddressesResponse: response}, nil
}

// DeleteNotificationAddress removes a single registered push destination
// belonging to the current user.
//
// The address ID comes from the URL path and the user ID comes from the
// auth context, so a user can only delete their own addresses. Trying to
// delete someone else's address returns a "not found" error.
func (s *Service) DeleteNotificationAddress(ctx context.Context, r *DeleteNotificationAddressRequest) error {
	if err := s.ensureNotifierService(); err != nil {
		return err
	}

	return s.NotifierService.DeleteAddress(ctx, r.DeleteNotificationAddressRequest)
}

// GetNotificationPreferences returns the current user's notification
// preferences (global on/off and per-channel toggles).
//
// If the user has never set preferences before, the defaults are returned –
// notifications are enabled for all channels.
func (s *Service) GetNotificationPreferences(ctx context.Context, r *GetNotificationPreferencesRequest) (*GetNotificationPreferencesResponse, error) {
	if err := s.ensureNotifierService(); err != nil {
		return nil, err
	}

	response, err := s.NotifierService.GetPreferences(ctx, r.GetNotificationPreferencesRequest)
	if err != nil {
		return nil, err
	}

	return &GetNotificationPreferencesResponse{GetNotificationPreferencesResponse: response}, nil
}

// UpdateNotificationPreferences changes the current user's notification
// settings.
//
// The request can include:
//
//   - Enabled: a pointer to a bool so the client can set it to true/false
//     or leave it nil to keep the current global setting.
//   - Channels: a map of channel name to bool (e.g. {"WEBPUSH": false})
//     to enable or disable individual channels.
//
// Only known channels are accepted; unknown channel names produce an error.
func (s *Service) UpdateNotificationPreferences(ctx context.Context, r *UpdateNotificationPreferencesRequest) (*UpdateNotificationPreferencesResponse, error) {
	if err := s.ensureNotifierService(); err != nil {
		return nil, err
	}

	response, err := s.NotifierService.UpdatePreferences(ctx, r.UpdateNotificationPreferencesRequest)
	if err != nil {
		return nil, err
	}

	return &UpdateNotificationPreferencesResponse{UpdateNotificationPreferencesResponse: response}, nil
}

// NotifyUser sends a push notification to all of a target user's active
// addresses across all enabled channels.
//
// This is an admin/service-only endpoint. It requires the caller to be
// authenticated with either an admin API token or an admin JWT – normal
// users cannot send notifications to other users (or themselves) through
// this endpoint.
//
// The request specifies:
//
//   - UserID (from the URL path, not the auth context) – which user to notify.
//   - Title and Message – the notification headline and body text.
//   - Channels (optional) – limit delivery to specific channels.
//   - Data (optional) – extra key-value pairs forwarded to the push payload.
//
// Preferences are still honoured: if the target user has disabled
// notifications globally or for all requested channels, no messages
// are sent.
func (s *Service) NotifyUser(ctx context.Context, r *NotifyUserRequest) (*NotifyUserResponse, error) {
	if err := s.ensureNotifierService(); err != nil {
		return nil, err
	}

	response, err := s.NotifierService.NotifyUser(ctx, r.NotifyUserRequest)
	if err != nil {
		if response != nil {
			return &NotifyUserResponse{NotifyUserResponse: response}, err
		}
		return nil, err
	}

	return &NotifyUserResponse{NotifyUserResponse: response}, nil
}
