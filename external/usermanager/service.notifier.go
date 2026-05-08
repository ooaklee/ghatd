package usermanager

import (
	"context"
)

func (s *Service) ensureNotifierService() error {
	if s.NotifierService == nil {
		return ErrNotifierServiceNotEnabled
	}
	return nil
}

// GetNotifierConfig returns client-safe notifier configuration through UMS.
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

// RegisterNotificationAddress registers a notification destination for the current user.
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

// ListNotificationAddresses returns client-safe notification addresses for the current user.
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

// DeleteNotificationAddress deletes a user-owned notification address.
func (s *Service) DeleteNotificationAddress(ctx context.Context, r *DeleteNotificationAddressRequest) error {
	if err := s.ensureNotifierService(); err != nil {
		return err
	}

	return s.NotifierService.DeleteAddress(ctx, r.DeleteNotificationAddressRequest)
}

// GetNotificationPreferences returns current user notification preferences.
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

// UpdateNotificationPreferences updates current user notification preferences.
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

// NotifyUser sends a notification to a target user via the notifier service.
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
