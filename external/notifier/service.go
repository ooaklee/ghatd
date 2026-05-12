package notifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// NotificationRepository describes the persistence operations the notifier
// service needs.
//
// By depending on this interface rather than a concrete struct, the service
// can be tested with a fake repository (see service_test.go) without needing
// a real MongoDB connection.
type NotificationRepository interface {
	UpsertAddress(ctx context.Context, address *NotificationAddress) (*NotificationAddress, error)
	GetActiveAddressesByUserID(ctx context.Context, userID string, channels ...NotificationChannel) ([]NotificationAddress, error)
	GetAddresses(ctx context.Context, r *ListNotificationAddressesRequest) ([]NotificationAddress, error)
	CountAddresses(ctx context.Context, r *ListNotificationAddressesRequest) (int64, error)
	GetAddressesByUserID(ctx context.Context, userID string) ([]NotificationAddress, error)
	DeleteAddressByIDForUser(ctx context.Context, userID, addressID string) error
	DeleteAddressesByUserID(ctx context.Context, userID string) error
	GetPreferencesByUserID(ctx context.Context, userID string) (*NotificationPreferences, error)
	UpsertPreferences(ctx context.Context, preferences *NotificationPreferences) (*NotificationPreferences, error)
	DeletePreferencesByUserID(ctx context.Context, userID string) error

	// DisableAddressByHash sets the status of the address with the given
	// hash to DISABLED without removing the record.  It is used by the
	// cleanup path when a sender detects a permanently invalid subscription
	// (e.g. Web Push 410 Gone) so the address is not retried on future
	// sends but can still be re-registered later by the same device.
	DisableAddressByHash(ctx context.Context, hash string) error
}

// Service is the core of the notifier package. It handles:
//
//   - Registering and deleting notification addresses (devices).
//   - Reading and updating user notification preferences.
//   - Sending push notifications through channel senders (Web Push, FCM).
//
// A Service is created by calling NewService with a repository and optional
// senders. Channel senders can also be added later with WithSender().
//
// The Service validates every request – unknown channels, missing user IDs,
// and empty payloads are all rejected with sentinel errors that the error
// manifest can turn into clean API responses.
//
// # Address Deduplication
//
// When a user registers the same browser or mobile device, the service
// computes a SHA-256 hash of the channel plus the address identity
// (endpoint for Web Push, token for FCM). The repository uses this hash
// in a unique index to upsert instead of insert, so the same device
// always points to the latest signed-in user and no duplicate records
// are created.
//
// # Privacy
//
// The service never returns raw endpoints or tokens to clients.
// RegisterAddress and ListUserAddresses both return sanitised summaries
// through NotificationAddressSummary, which only includes non-sensitive
// fields (ID, channel, status, device info, timestamps).
//
// The full addresses (with endpoints and tokens) are only used internally
// by the NotifyUser flow when the service itself needs to deliver a push
// message.
type Service struct {
	Repository NotificationRepository
	senders    map[NotificationChannel]ChannelSender
}

// NewServiceRequest carries the dependencies needed to create a Service.
//
// Repository is required. Senders is optional – you can add senders later
// with WithSender().
type NewServiceRequest struct {
	Repository NotificationRepository
	Senders    []ChannelSender
}

// NewService creates a notifier service with the given repository and
// optionally pre-registers a set of channel senders.
//
// Example:
//
//	service := notifier.NewService(&notifier.NewServiceRequest{
//	    Repository: repository,
//	    Senders:    []notifier.ChannelSender{
//	        notifier.NewWebPushSender(config),
//	        notifier.NewFCMSender(config),
//	    },
//	})
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

// WithSender registers or replaces a channel sender on the service.
//
// If a sender for the same channel already exists, it is replaced.
// If the sender is nil, the call is silently ignored.
//
// This method returns the service pointer so calls can be chained:
//
//	service.WithSender(webPushSender).WithSender(fcmSender)
//
// If the sender implements [InvalidAddressCleanable], the service
// automatically wires a cleanup handler that disables permanently
// invalid addresses via the repository.
func (s *Service) WithSender(sender ChannelSender) *Service {
	if sender == nil {
		return s
	}
	if cleanable, ok := sender.(InvalidAddressCleanable); ok && s.Repository != nil {
		cleanable.SetInvalidAddressHandler(func(ctx context.Context, hash string) error {
			return s.Repository.DisableAddressByHash(ctx, hash)
		})
	}
	s.senders[sender.Channel().Normalised()] = sender
	return s
}

// RegisterAddress saves a new notification destination for a user.
//
// What you need to provide depends on the channel:
//
//   - WEBPUSH: the browser Push API subscription (endpoint + keys).
//   - FCM: the mobile device's Firebase token.
//
// The UserID must come from the authenticated context – the service does
// not trust a user ID from the client body. This is enforced by UMS,
// which fills in the UserID before calling RegisterAddress.
//
// The returned address is a sanitised summary with secrets stripped.
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

// GetActiveAddressesByUserID returns full notification addresses for
// server-side send operations.
//
// This is an internal method called by NotifyUser, not exposed as a
// public API. It returns un-sanitised addresses (with endpoints and
// tokens) because the notifier itself needs those secrets to deliver
// push messages.
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

// ListUserAddresses returns a clean, safe list of a user's registered
// devices – no endpoints, no tokens, no secrets.
//
// This is the method behind the GET /addresses endpoint. Users call it
// to see which devices they have registered for push notifications.
func (s *Service) ListUserAddresses(ctx context.Context, req *ListNotificationAddressesRequest) (*ListNotificationAddressesResponse, error) {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return nil, ErrNotificationUserIDRequired
	}

	addresses, err := s.Repository.GetAddressesByUserID(ctx, strings.TrimSpace(req.UserID))
	if err != nil {
		return nil, err
	}

	return sanitiseAddresses(addresses), nil
}

// ListAddresses returns a sanitised list of notification destinations for admin
// dashboards. Unlike ListUserAddresses, UserID is optional so callers can inspect
// platform-wide registrations and narrow them with filters.
func (s *Service) ListAddresses(ctx context.Context, req *ListNotificationAddressesRequest) (*ListNotificationAddressesResponse, error) {
	if req == nil {
		req = &ListNotificationAddressesRequest{}
	}

	filter := *req
	filter.UserID = strings.TrimSpace(filter.UserID)
	if filter.Channel != "" {
		filter.Channel = filter.Channel.Normalised()
		if !filter.Channel.IsSupported() {
			return nil, ErrInvalidNotificationChannel
		}
	}
	if filter.Status != "" {
		filter.Status = filter.Status.Normalised()
		if !filter.Status.IsSupported() {
			return nil, ErrInvalidNotificationAddressBody
		}
	}
	filter.Page, filter.PerPage = normaliseAddressListPagination(filter.Page, filter.PerPage)

	total, err := s.Repository.CountAddresses(ctx, &filter)
	if err != nil {
		return nil, err
	}
	addresses, err := s.Repository.GetAddresses(ctx, &filter)
	if err != nil {
		return nil, err
	}

	response := sanitiseAddresses(addresses)
	response.Total = int(total)
	response.TotalPages = int(math.Ceil(float64(total) / float64(filter.PerPage)))
	response.PerPage = filter.PerPage
	response.Page = filter.Page
	return response, nil
}

func sanitiseAddresses(addresses []NotificationAddress) *ListNotificationAddressesResponse {
	summaries := make([]NotificationAddressSummary, 0, len(addresses))
	for _, address := range addresses {
		summaries = append(summaries, address.Sanitise())
	}

	return &ListNotificationAddressesResponse{Addresses: summaries}
}

func normaliseAddressListPagination(page, perPage int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 25
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}

// DeleteAddress removes one of a user's registered notification addresses.
//
// The repository checks that the address belongs to the user before
// deleting it. If the address does not exist or belongs to a different
// user, the call returns ErrNotificationAddressNotFound.
func (s *Service) DeleteAddress(ctx context.Context, req *DeleteNotificationAddressRequest) error {
	if req == nil || strings.TrimSpace(req.UserID) == "" {
		return ErrNotificationUserIDRequired
	}
	if strings.TrimSpace(req.AddressID) == "" {
		return ErrNotificationAddressNotFound
	}

	return s.Repository.DeleteAddressByIDForUser(ctx, strings.TrimSpace(req.UserID), strings.TrimSpace(req.AddressID))
}

// GetPreferences returns a user's current notification preferences.
//
// Important: if the user has never set preferences before, the method
// returns the defaults (everything enabled) instead of an error. This
// means new users are opted in by default and can disable notifications
// later if they want.
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

// UpdatePreferences changes a user's notification settings.
//
// A client can update:
//
//   - Enabled – the global on/off switch. Set to false to stop all
//     notifications regardless of channel settings.
//   - Channels – per-channel on/off switches (e.g. {"WEBPUSH": false}
//     to disable browser push but keep mobile push).
//
// Both fields are optional in the request. If Enabled is nil, the
// global setting is not changed. Channels that are not mentioned in
// the map are left as they were.
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

// GetConfig returns the public notifier configuration that clients need.
//
// The config tells clients:
//
//   - Which channels the server supports (e.g. WEBPUSH, FCM).
//   - For Web Push: whether it is enabled and what the public VAPID key is
//     (needed for browser pushManager.subscribe()).
//   - For FCM: whether it is enabled.
//
// This method is safe to call without authentication – it doesn't expose
// any user-specific data.
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

// NotifyUser delivers a push notification to all of a user's active
// addresses across channels.
//
// This is the main publication entry point. The method:
//
//  1. Checks the user's preferences – if notifications are disabled
//     globally at the user level, no sends happen.
//  2. Filters to the requested channels (or all channels if the request
//     does not specify).
//  3. Looks up the user's active addresses for those channels.
//  4. Applies per-channel preference filtering (e.g. "Web Push enabled,
//     but FCM disabled").
//  5. Dispatches each channel's addresses to the appropriate sender.
//
// The response contains one result per channel, showing whether the
// send succeeded, was skipped because the sender was not enabled, or
// failed with an error.
//
// This method is only accessible to admins and internal services through
// the UMS API. Normal users cannot trigger arbitrary pushes.
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

		var sendErr error
		if detailedSender, ok := sender.(detailedChannelSender); ok {
			report, err := detailedSender.SendWithReport(ctx, req.Title, req.Message, channelAddresses, req.Data)
			sendErr = err
			result.Cleaned = report.Cleaned
			result.Sent = report.Delivered > 0
		} else {
			sendErr = sender.Send(ctx, req.Title, req.Message, channelAddresses, req.Data)
			if sendErr == nil && len(channelAddresses) > 0 {
				result.Sent = true
			}
		}

		if sendErr != nil {
			result.Error = sendErr.Error()
			sendErrs = append(sendErrs, sendErr)
		}
		results = append(results, result)
	}

	response := &NotifyUserResponse{Results: results}
	if len(sendErrs) > 0 {
		return response, errors.Join(append([]error{ErrNotificationSendFailed}, sendErrs...)...)
	}

	return response, nil
}

// validateAddressIdentity checks that the request contains the right
// channel-specific payload and returns the identity string used for
// address hash calculation.
//
//   - For WEBPUSH: the identity is the subscription endpoint URL.
//   - For FCM: the identity is the device token.
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

// normaliseWebPushAddress trims whitespace from all fields in a Web Push
// address to avoid subtle duplicate records caused by extra spaces.
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

// normaliseFCMAddress trims whitespace from the FCM token string.
func normaliseFCMAddress(address *FCMAddress) *FCMAddress {
	if address == nil {
		return nil
	}
	return &FCMAddress{Token: strings.TrimSpace(address.Token)}
}

// normaliseChannels validates and deduplicates a list of channel names.
//
// If the list is empty, nil is returned (meaning "use all available
// channels"). Otherwise each name is uppercased, validated, and de-duped
// before being returned.
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

// ensurePreferenceDefaults fills in any missing per-channel settings with
// their default value (true, meaning enabled).
//
// This makes sure that when a user updates one channel setting, the
// other channels are not accidentally left empty.
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

// hashAddress creates a deterministic SHA-256 fingerprint from a channel
// and an address identity (endpoint or token).
//
// This hash is used by the database's unique index to deduplicate addresses.
// The same browser registering twice under different user IDs will simply
// reassign the existing address record rather than creating a duplicate.
func hashAddress(channel NotificationChannel, identity string) string {
	hash := sha256.Sum256([]byte(string(channel.Normalised()) + ":" + strings.TrimSpace(identity)))
	return hex.EncodeToString(hash[:])
}
