package notifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// NotificationRepository describes notifier persistence operations.
type NotificationRepository interface {
	UpsertAddress(ctx context.Context, address *NotificationAddress) (*NotificationAddress, error)
	GetActiveAddressesByUserID(ctx context.Context, userID string, channels ...NotificationChannel) ([]NotificationAddress, error)
	GetAddressesByUserID(ctx context.Context, userID string) ([]NotificationAddress, error)
	DeleteAddressByIDForUser(ctx context.Context, userID, addressID string) error
	DeleteAddressesByUserID(ctx context.Context, userID string) error
	GetPreferencesByUserID(ctx context.Context, userID string) (*NotificationPreferences, error)
	UpsertPreferences(ctx context.Context, preferences *NotificationPreferences) (*NotificationPreferences, error)
	DeletePreferencesByUserID(ctx context.Context, userID string) error
}

// Service manages notification addresses, preferences, and sends.
type Service struct {
	Repository NotificationRepository
	senders    map[NotificationChannel]ChannelSender
}

// NewServiceRequest contains notifier service dependencies.
type NewServiceRequest struct {
	Repository NotificationRepository
	Senders    []ChannelSender
}

// NewService creates a notifier service.
func NewService(r *NewServiceRequest) *Service {
	service := &Service{
		Repository: r.Repository,
		senders:    map[NotificationChannel]ChannelSender{},
	}
	for _, sender := range r.Senders {
		service.WithSender(sender)
	}
	return service
}

// WithSender registers or replaces a channel sender.
func (s *Service) WithSender(sender ChannelSender) *Service {
	if sender == nil {
		return s
	}
	s.senders[sender.Channel().Normalised()] = sender
	return s
}

// RegisterAddress links a notification destination to a user.
func (s *Service) RegisterAddress(ctx context.Context, req *RegisterAddressRequest) (*RegisterAddressResponse, error) {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return nil, ErrNotificationUserIDRequired
	}

	channel := req.Channel.Normalised()
	if !channel.IsSupported() {
		return nil, ErrInvalidNotificationChannel
	}

	addressIdentity, err := validateAddressIdentity(channel, req)
	if err != nil {
		return nil, err
	}

	now := toolbox.TimeNowUTC()
	address := &NotificationAddress{
		ID:          toolbox.GenerateUuidV4(),
		UserID:      strings.TrimSpace(req.UserID),
		Channel:     channel,
		Status:      NotificationAddressStatusActive,
		AddressHash: hashAddress(channel, addressIdentity),
		DeviceID:    strings.TrimSpace(req.DeviceID),
		DeviceName:  strings.TrimSpace(req.DeviceName),
		Platform:    strings.ToUpper(strings.TrimSpace(req.Platform)),
		WebPush:     normaliseWebPushAddress(req.WebPush),
		FCM:         normaliseFCMAddress(req.FCM),
		Metadata: &NotificationAddressMetadata{
			CreatedAt:  now,
			UpdatedAt:  now,
			LastSeenAt: now,
		},
	}

	registered, err := s.Repository.UpsertAddress(ctx, address)
	if err != nil {
		return nil, err
	}

	return &RegisterAddressResponse{Address: registered.Sanitise()}, nil
}

// GetActiveAddressesByUserID retrieves active full notification addresses for server-side sends.
func (s *Service) GetActiveAddressesByUserID(ctx context.Context, req *GetActiveNotificationAddressesRequest) (*GetActiveNotificationAddressesResponse, error) {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return nil, ErrNotificationUserIDRequired
	}

	channels, err := normaliseChannels(req.Channels)
	if err != nil {
		return nil, err
	}

	addresses, err := s.Repository.GetActiveAddressesByUserID(ctx, strings.TrimSpace(req.UserID), channels...)
	if err != nil {
		return nil, err
	}

	return &GetActiveNotificationAddressesResponse{Addresses: addresses}, nil
}

// ListUserAddresses returns client-safe notification addresses for a user.
func (s *Service) ListUserAddresses(ctx context.Context, req *ListNotificationAddressesRequest) (*ListNotificationAddressesResponse, error) {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return nil, ErrNotificationUserIDRequired
	}

	addresses, err := s.Repository.GetAddressesByUserID(ctx, strings.TrimSpace(req.UserID))
	if err != nil {
		return nil, err
	}

	summaries := make([]NotificationAddressSummary, 0, len(addresses))
	for _, address := range addresses {
		summaries = append(summaries, address.Sanitise())
	}

	return &ListNotificationAddressesResponse{Addresses: summaries}, nil
}

// DeleteAddress deletes a user-owned notification address.
func (s *Service) DeleteAddress(ctx context.Context, req *DeleteNotificationAddressRequest) error {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return ErrNotificationUserIDRequired
	}
	if strings.TrimSpace(req.AddressID) == "" {
		return ErrNotificationAddressNotFound
	}

	return s.Repository.DeleteAddressByIDForUser(ctx, strings.TrimSpace(req.UserID), strings.TrimSpace(req.AddressID))
}

// GetPreferences returns preferences, defaulting to enabled when none exist yet.
func (s *Service) GetPreferences(ctx context.Context, req *GetNotificationPreferencesRequest) (*GetNotificationPreferencesResponse, error) {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return nil, ErrNotificationUserIDRequired
	}

	preferences, err := s.Repository.GetPreferencesByUserID(ctx, strings.TrimSpace(req.UserID))
	if err != nil {
		if errors.Is(err, ErrNotificationAddressNotFound) {
			return &GetNotificationPreferencesResponse{Preferences: DefaultNotificationPreferences(strings.TrimSpace(req.UserID))}, nil
		}
		return nil, err
	}

	ensurePreferenceDefaults(preferences)
	return &GetNotificationPreferencesResponse{Preferences: preferences}, nil
}

// UpdatePreferences updates a user's notification preferences.
func (s *Service) UpdatePreferences(ctx context.Context, req *UpdateNotificationPreferencesRequest) (*UpdateNotificationPreferencesResponse, error) {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return nil, ErrNotificationUserIDRequired
	}

	userID := strings.TrimSpace(req.UserID)
	currentResponse, err := s.GetPreferences(ctx, &GetNotificationPreferencesRequest{UserID: userID})
	if err != nil {
		return nil, err
	}
	preferences := currentResponse.Preferences
	ensurePreferenceDefaults(preferences)

	if req.Enabled != nil {
		preferences.Enabled = *req.Enabled
	}
	for channel, enabled := range req.Channels {
		normalised := NotificationChannel(channel).Normalised()
		if !normalised.IsSupported() {
			return nil, ErrInvalidNotificationPreferences
		}
		preferences.Channels[string(normalised)] = enabled
	}

	now := toolbox.TimeNowUTC()
	if preferences.Metadata == nil {
		preferences.Metadata = &NotificationPreferencesMetadata{CreatedAt: now}
	}
	if preferences.Metadata.CreatedAt == "" {
		preferences.Metadata.CreatedAt = now
	}
	preferences.Metadata.UpdatedAt = now

	updated, err := s.Repository.UpsertPreferences(ctx, preferences)
	if err != nil {
		return nil, err
	}

	ensurePreferenceDefaults(updated)
	return &UpdateNotificationPreferencesResponse{Preferences: updated}, nil
}

// GetConfig returns client-safe notifier configuration.
func (s *Service) GetConfig(ctx context.Context, req *GetNotifierConfigRequest) (*GetNotifierConfigResponse, error) {
	config := &NotifierConfig{
		SupportedChannels: []NotificationChannel{},
		WebPush:           WebPushClientConfig{},
		FCM:               FCMClientConfig{},
	}

	for channel, sender := range s.senders {
		if sender == nil || !sender.Enabled() {
			continue
		}
		config.SupportedChannels = append(config.SupportedChannels, channel)
		switch typedSender := sender.(type) {
		case *WebPushSender:
			config.WebPush.Enabled = true
			config.WebPush.VAPIDPublicKey = typedSender.PublicKey()
		case *FCMSender:
			config.FCM.Enabled = true
		}
	}
	sort.Slice(config.SupportedChannels, func(i, j int) bool {
		return config.SupportedChannels[i] < config.SupportedChannels[j]
	})

	return &GetNotifierConfigResponse{Config: config}, nil
}

// NotifyUser sends a notification to a user's active addresses.
func (s *Service) NotifyUser(ctx context.Context, req *NotifyUserRequest) (*NotifyUserResponse, error) {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return nil, ErrNotificationUserIDRequired
	}
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Message) == "" {
		return nil, ErrInvalidNotificationAddressBody
	}

	preferencesResponse, err := s.GetPreferences(ctx, &GetNotificationPreferencesRequest{UserID: req.UserID})
	if err != nil {
		return nil, err
	}
	preferences := preferencesResponse.Preferences
	if preferences != nil && !preferences.Enabled {
		return &NotifyUserResponse{Results: []NotificationSendResult{}}, nil
	}

	channels, err := normaliseChannels(req.Channels)
	if err != nil {
		return nil, err
	}
	addresses, err := s.Repository.GetActiveAddressesByUserID(ctx, strings.TrimSpace(req.UserID), channels...)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, ErrNotificationNoActiveAddresses
	}

	addressesByChannel := map[NotificationChannel][]NotificationAddress{}
	for _, address := range addresses {
		channel := address.Channel.Normalised()
		if preferences != nil && preferences.Channels != nil && !preferences.Channels[string(channel)] {
			continue
		}
		addressesByChannel[channel] = append(addressesByChannel[channel], address)
	}

	results := []NotificationSendResult{}
	var sendErrs []error
	for channel, channelAddresses := range addressesByChannel {
		sender := s.senders[channel]
		result := NotificationSendResult{Channel: channel, Attempted: len(channelAddresses)}
		if sender == nil || !sender.Enabled() {
			result.Skipped = true
			result.Error = ErrNotificationSenderNotEnabled.Error()
			results = append(results, result)
			continue
		}

		if err := sender.Send(ctx, req.Title, req.Message, channelAddresses, req.Data); err != nil {
			result.Error = err.Error()
			sendErrs = append(sendErrs, err)
		} else {
			result.Sent = true
		}
		results = append(results, result)
	}

	response := &NotifyUserResponse{Results: results}
	if len(sendErrs) > 0 {
		return response, errors.Join(append([]error{ErrNotificationSendFailed}, sendErrs...)...)
	}

	return response, nil
}

func validateAddressIdentity(channel NotificationChannel, req *RegisterAddressRequest) (string, error) {
	switch channel {
	case NotificationChannelWebPush:
		if req.WebPush == nil ||
			strings.TrimSpace(req.WebPush.Endpoint) == "" ||
			strings.TrimSpace(req.WebPush.Keys.Auth) == "" ||
			strings.TrimSpace(req.WebPush.Keys.P256DH) == "" {
			return "", ErrInvalidNotificationAddressBody
		}
		return strings.TrimSpace(req.WebPush.Endpoint), nil
	case NotificationChannelFCM:
		if req.FCM == nil || strings.TrimSpace(req.FCM.Token) == "" {
			return "", ErrInvalidNotificationAddressBody
		}
		return strings.TrimSpace(req.FCM.Token), nil
	default:
		return "", ErrInvalidNotificationChannel
	}
}

func normaliseWebPushAddress(address *WebPushAddress) *WebPushAddress {
	if address == nil {
		return nil
	}
	return &WebPushAddress{
		Endpoint: strings.TrimSpace(address.Endpoint),
		Keys: WebPushKeys{
			Auth:   strings.TrimSpace(address.Keys.Auth),
			P256DH: strings.TrimSpace(address.Keys.P256DH),
		},
	}
}

func normaliseFCMAddress(address *FCMAddress) *FCMAddress {
	if address == nil {
		return nil
	}
	return &FCMAddress{Token: strings.TrimSpace(address.Token)}
}

func normaliseChannels(channels []NotificationChannel) ([]NotificationChannel, error) {
	if len(channels) == 0 {
		return nil, nil
	}

	normalised := make([]NotificationChannel, 0, len(channels))
	seen := map[NotificationChannel]bool{}
	for _, channel := range channels {
		channel = channel.Normalised()
		if !channel.IsSupported() {
			return nil, ErrInvalidNotificationChannel
		}
		if seen[channel] {
			continue
		}
		seen[channel] = true
		normalised = append(normalised, channel)
	}

	return normalised, nil
}

func ensurePreferenceDefaults(preferences *NotificationPreferences) {
	if preferences == nil {
		return
	}
	if preferences.Channels == nil {
		preferences.Channels = map[string]bool{}
	}
	if _, ok := preferences.Channels[string(NotificationChannelWebPush)]; !ok {
		preferences.Channels[string(NotificationChannelWebPush)] = true
	}
	if _, ok := preferences.Channels[string(NotificationChannelFCM)]; !ok {
		preferences.Channels[string(NotificationChannelFCM)] = true
	}
}

func hashAddress(channel NotificationChannel, identity string) string {
	hash := sha256.Sum256([]byte(string(channel.Normalised()) + ":" + strings.TrimSpace(identity)))
	return hex.EncodeToString(hash[:])
}
