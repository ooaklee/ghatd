package notifier

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// fakeRepository is a test double that records calls and returns
// canned responses, letting us test the Service without a real MongoDB.
type fakeRepository struct {
	addresses           []NotificationAddress
	preferences         *NotificationPreferences
	preferencesByUserID map[string]*NotificationPreferences
	upserted            *NotificationAddress
	deletedID           string
	disabledHash        string
	listRequest         *ListNotificationAddressesRequest
	countRequest        *ListNotificationAddressesRequest

	disableError error
}

func (r *fakeRepository) UpsertAddress(ctx context.Context, address *NotificationAddress) (*NotificationAddress, error) {
	r.upserted = address
	return address, nil
}

func (r *fakeRepository) GetActiveAddressesByUserID(ctx context.Context, userID string, channels ...NotificationChannel) ([]NotificationAddress, error) {
	return filterFakeActiveAddresses(r.addresses, userID, channels...), nil
}

func (r *fakeRepository) GetAllActiveAddresses(ctx context.Context, channels ...NotificationChannel) ([]NotificationAddress, error) {
	return filterFakeActiveAddresses(r.addresses, "", channels...), nil
}

func (r *fakeRepository) GetAddressesByUserID(ctx context.Context, userID string) ([]NotificationAddress, error) {
	return r.addresses, nil
}

func (r *fakeRepository) GetAddresses(ctx context.Context, req *ListNotificationAddressesRequest) ([]NotificationAddress, error) {
	r.listRequest = req
	return r.addresses, nil
}

func (r *fakeRepository) CountAddresses(ctx context.Context, req *ListNotificationAddressesRequest) (int64, error) {
	r.countRequest = req
	return int64(len(r.addresses)), nil
}

func (r *fakeRepository) DeleteAddressByIDForUser(ctx context.Context, userID, addressID string) error {
	r.deletedID = addressID
	return nil
}

func (r *fakeRepository) DeleteAddressesByUserID(ctx context.Context, userID string) error {
	return nil
}

func (r *fakeRepository) DisableAddressByHash(ctx context.Context, hash string) error {
	if r.disableError != nil {
		return r.disableError
	}
	r.disabledHash = hash
	return nil
}

func (r *fakeRepository) GetPreferencesByUserID(ctx context.Context, userID string) (*NotificationPreferences, error) {
	if r.preferencesByUserID != nil {
		preferences := r.preferencesByUserID[userID]
		if preferences == nil {
			return nil, ErrNotificationAddressNotFound
		}
		return preferences, nil
	}
	if r.preferences == nil {
		return nil, ErrNotificationAddressNotFound
	}
	return r.preferences, nil
}

func (r *fakeRepository) UpsertPreferences(ctx context.Context, preferences *NotificationPreferences) (*NotificationPreferences, error) {
	r.preferences = preferences
	return preferences, nil
}

func (r *fakeRepository) DeletePreferencesByUserID(ctx context.Context, userID string) error {
	return nil
}

func filterFakeActiveAddresses(addresses []NotificationAddress, userID string, channels ...NotificationChannel) []NotificationAddress {
	channelSet := map[NotificationChannel]bool{}
	for _, channel := range channels {
		channelSet[channel.Normalised()] = true
	}

	filtered := []NotificationAddress{}
	for _, address := range addresses {
		if address.Status != NotificationAddressStatusActive {
			continue
		}
		if userID != "" && address.UserID != userID {
			continue
		}
		if len(channelSet) > 0 && !channelSet[address.Channel.Normalised()] {
			continue
		}
		filtered = append(filtered, address)
	}
	return filtered
}

// fakeSender is a test double for ChannelSender that records how many
// addresses the service attempted to send to.
type fakeSender struct {
	channel  NotificationChannel
	enabled  bool
	attempts int
}

func (s *fakeSender) Channel() NotificationChannel { return s.channel }
func (s *fakeSender) Enabled() bool                { return s.enabled }
func (s *fakeSender) Send(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) error {
	s.attempts += len(addresses)
	return nil
}

// cleanupTrackingSender is a ChannelSender fake that simulates
// per-address delivery outcomes so we can test the cleanup path.
//
//   - invalidHashes lists address hashes whose delivery should be
//     treated as a permanent failure. The sender calls its
//     invalidAddressHandler for these and increments the per-call report's
//     cleaned count.
//   - transientError is returned for addresses NOT in invalidHashes.
//     Set it to nil to simulate successful delivery.
//
// If the invalidAddressHandler returns an error, that error is NOT
// swallowed — it is joined into the return value so the caller sees it.
type cleanupTrackingSender struct {
	channel               NotificationChannel
	invalidHashes         map[string]bool
	transientError        error
	invalidAddressHandler func(ctx context.Context, hash string) error

	sentAddrs []string
	badAddrs  []string
}

func (c *cleanupTrackingSender) Channel() NotificationChannel { return c.channel }
func (c *cleanupTrackingSender) Enabled() bool                { return true }
func (c *cleanupTrackingSender) SetInvalidAddressHandler(handler func(ctx context.Context, hash string) error) {
	c.invalidAddressHandler = handler
}

func (c *cleanupTrackingSender) Send(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) error {
	_, err := c.SendWithReport(ctx, subject, message, addresses, data)
	return err
}

func (c *cleanupTrackingSender) SendWithReport(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) (channelSendReport, error) {
	report := channelSendReport{}
	c.sentAddrs = nil
	c.badAddrs = nil

	var allErrs []error
	for _, addr := range addresses {
		c.sentAddrs = append(c.sentAddrs, addr.AddressHash)
		if c.invalidHashes != nil && c.invalidHashes[addr.AddressHash] {
			c.badAddrs = append(c.badAddrs, addr.AddressHash)
			if c.invalidAddressHandler != nil {
				if err := c.invalidAddressHandler(ctx, addr.AddressHash); err != nil {
					allErrs = append(allErrs, err)
					continue
				}
			}
			report.Cleaned++
			continue
		}
		if c.transientError != nil {
			allErrs = append(allErrs, c.transientError)
			continue
		}
		report.Delivered++
	}
	if len(allErrs) > 0 {
		return report, errors.Join(allErrs...)
	}
	return report, nil
}

// ---------------------------------------------------------------------------
// Existing tests
// ---------------------------------------------------------------------------

func TestRegisterAddress_WebPushUpsertsActiveAddress(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(&NewServiceRequest{Repository: repository})

	response, err := service.RegisterAddress(context.Background(), &RegisterAddressRequest{
		UserID:     "user-1",
		Channel:    "webpush",
		DeviceID:   "device-1",
		DeviceName: "Chrome",
		Platform:   "web",
		WebPush: &WebPushAddress{
			Endpoint: "https://push.example/subscription",
			Keys: WebPushKeys{
				Auth:   "auth",
				P256DH: "p256dh",
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if response.Address.ID == "" {
		t.Fatal("expected address id to be generated")
	}
	if repository.upserted == nil {
		t.Fatal("expected repository upsert")
	}
	if repository.upserted.UserID != "user-1" {
		t.Fatalf("expected user-1, got %q", repository.upserted.UserID)
	}
	if repository.upserted.Channel != NotificationChannelWebPush {
		t.Fatalf("expected WEBPUSH channel, got %q", repository.upserted.Channel)
	}
	if repository.upserted.Status != NotificationAddressStatusActive {
		t.Fatalf("expected active status, got %q", repository.upserted.Status)
	}
	if repository.upserted.AddressHash == "" {
		t.Fatal("expected address hash")
	}
}

func TestRegisterAddress_RejectsInvalidChannelPayload(t *testing.T) {
	service := NewService(&NewServiceRequest{Repository: &fakeRepository{}})

	_, err := service.RegisterAddress(context.Background(), &RegisterAddressRequest{
		UserID:  "user-1",
		Channel: NotificationChannelWebPush,
	})
	if err != ErrInvalidNotificationAddressBody {
		t.Fatalf("expected invalid address body, got %v", err)
	}
}

func TestListAddresses_AdminFiltersAndSanitises(t *testing.T) {
	tests := []struct {
		name        string
		request     *ListNotificationAddressesRequest
		wantErr     error
		wantChannel NotificationChannel
		wantStatus  NotificationAddressStatus
		wantPage    int
		wantPerPage int
	}{
		{
			name: "filters by user channel and status",
			request: &ListNotificationAddressesRequest{
				UserID:  "user-1",
				Channel: "webpush",
				Status:  "active",
			},
			wantChannel: NotificationChannelWebPush,
			wantStatus:  NotificationAddressStatusActive,
			wantPage:    1,
			wantPerPage: 25,
		},
		{
			name:        "allows platform wide listing",
			request:     &ListNotificationAddressesRequest{},
			wantPage:    1,
			wantPerPage: 25,
		},
		{
			name:        "normalises pagination and caps page size",
			request:     &ListNotificationAddressesRequest{Page: 2, PerPage: 250},
			wantPage:    2,
			wantPerPage: 100,
		},
		{
			name:    "rejects invalid channel",
			request: &ListNotificationAddressesRequest{Channel: "SMS"},
			wantErr: ErrInvalidNotificationChannel,
		},
		{
			name:    "rejects invalid status",
			request: &ListNotificationAddressesRequest{Status: "BROKEN"},
			wantErr: ErrInvalidNotificationAddressBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &fakeRepository{
				addresses: []NotificationAddress{
					{
						ID:          "addr-1",
						UserID:      "user-1",
						Channel:     NotificationChannelWebPush,
						Status:      NotificationAddressStatusActive,
						AddressHash: "secret-hash",
						WebPush:     &WebPushAddress{Endpoint: "https://push.example/subscription", Keys: WebPushKeys{Auth: "auth", P256DH: "p256dh"}},
					},
				},
			}
			service := NewService(&NewServiceRequest{Repository: repository})

			response, err := service.ListAddresses(context.Background(), tt.request)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if repository.listRequest == nil {
				t.Fatal("expected repository list request")
			}
			if repository.countRequest == nil {
				t.Fatal("expected repository count request")
			}
			if tt.wantChannel != "" && repository.listRequest.Channel != tt.wantChannel {
				t.Fatalf("expected channel %q, got %q", tt.wantChannel, repository.listRequest.Channel)
			}
			if tt.wantStatus != "" && repository.listRequest.Status != tt.wantStatus {
				t.Fatalf("expected status %q, got %q", tt.wantStatus, repository.listRequest.Status)
			}
			if repository.listRequest.Page != tt.wantPage || repository.listRequest.PerPage != tt.wantPerPage {
				t.Fatalf("expected page/perPage %d/%d, got %d/%d", tt.wantPage, tt.wantPerPage, repository.listRequest.Page, repository.listRequest.PerPage)
			}
			if len(response.Addresses) != 1 {
				t.Fatalf("expected one address, got %d", len(response.Addresses))
			}
			if response.Total != 1 || response.Page != tt.wantPage || response.PerPage != tt.wantPerPage {
				t.Fatalf("unexpected response metadata: %#v", response.GetMetaData())
			}
			summary := response.Addresses[0]
			if summary.UserID != "user-1" {
				t.Fatalf("expected user id on admin summary, got %q", summary.UserID)
			}
			if summary.ID != "addr-1" || summary.Channel != NotificationChannelWebPush {
				t.Fatalf("unexpected summary: %#v", summary)
			}
		})
	}
}

func TestNotifyUser_SendsOnlyWhenPreferencesAllowChannel(t *testing.T) {
	sender := &fakeSender{channel: NotificationChannelWebPush, enabled: true}
	repository := &fakeRepository{
		addresses: []NotificationAddress{
			{
				ID:      "address-1",
				UserID:  "user-1",
				Channel: NotificationChannelWebPush,
				Status:  NotificationAddressStatusActive,
				WebPush: &WebPushAddress{Endpoint: "https://push.example/subscription", Keys: WebPushKeys{Auth: "auth", P256DH: "p256dh"}},
			},
		},
		preferences: &NotificationPreferences{
			UserID:  "user-1",
			Enabled: true,
			Channels: map[string]bool{
				string(NotificationChannelWebPush): true,
				string(NotificationChannelFCM):     false,
			},
		},
	}
	service := NewService(&NewServiceRequest{
		Repository: repository,
		Senders:    []ChannelSender{sender},
	})

	response, err := service.NotifyUser(context.Background(), &NotifyUserRequest{
		UserID:  "user-1",
		Title:   "Reminder",
		Message: "Time to check in",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if sender.attempts != 1 {
		t.Fatalf("expected one send attempt, got %d", sender.attempts)
	}
	if len(response.Results) != 1 || !response.Results[0].Sent {
		t.Fatalf("expected sent result, got %#v", response.Results)
	}
}

func TestNotifyUser_SkipsWhenPreferencesDisabled(t *testing.T) {
	sender := &fakeSender{channel: NotificationChannelWebPush, enabled: true}
	repository := &fakeRepository{
		addresses: []NotificationAddress{{ID: "address-1", UserID: "user-1", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive}},
		preferences: &NotificationPreferences{
			UserID:  "user-1",
			Enabled: false,
		},
	}
	service := NewService(&NewServiceRequest{
		Repository: repository,
		Senders:    []ChannelSender{sender},
	})

	response, err := service.NotifyUser(context.Background(), &NotifyUserRequest{
		UserID:  "user-1",
		Title:   "Reminder",
		Message: "Time to check in",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if sender.attempts != 0 {
		t.Fatalf("expected no send attempts, got %d", sender.attempts)
	}
	if len(response.Results) != 0 {
		t.Fatalf("expected empty results, got %#v", response.Results)
	}
}

// ---------------------------------------------------------------------------
// Cleanup behaviour tests
// ---------------------------------------------------------------------------

func testAddress(prefix string, hash string) NotificationAddress {
	return NotificationAddress{
		ID:          prefix + "-1",
		UserID:      "user-1",
		Channel:     NotificationChannelWebPush,
		Status:      NotificationAddressStatusActive,
		AddressHash: hash,
		WebPush:     &WebPushAddress{Endpoint: "https://push.example/" + hash, Keys: WebPushKeys{Auth: "a", P256DH: "k"}},
	}
}

// TestNotifyUser_DisablesExpiredAddress — only expired address, cleanup
// succeeds.  Attempted=1, Sent=false, Cleaned=1, no error.
func TestNotifyUser_DisablesExpiredAddress(t *testing.T) {
	addr := testAddress("bad", "hash-expired")
	addrHash := addr.AddressHash

	repo := &fakeRepository{
		addresses: []NotificationAddress{addr},
		preferences: &NotificationPreferences{
			UserID:  "user-1",
			Enabled: true,
			Channels: map[string]bool{
				string(NotificationChannelWebPush): true,
			},
		},
	}

	sender := &cleanupTrackingSender{
		channel:       NotificationChannelWebPush,
		invalidHashes: map[string]bool{addrHash: true},
	}

	service := NewService(&NewServiceRequest{
		Repository: repo,
		Senders:    []ChannelSender{sender},
	})

	response, err := service.NotifyUser(context.Background(), &NotifyUserRequest{
		UserID:  "user-1",
		Title:   "Hello",
		Message: "World",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if repo.disabledHash != addrHash {
		t.Fatalf("expected disabledHash %q, got %q", addrHash, repo.disabledHash)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(response.Results))
	}
	r := response.Results[0]
	if r.Attempted != 1 {
		t.Fatalf("expected attempted=1, got %d", r.Attempted)
	}
	if r.Sent {
		t.Fatal("expected Sent=false (cleanup-only, nothing delivered)")
	}
	if r.Cleaned != 1 {
		t.Fatalf("expected Cleaned=1, got %d", r.Cleaned)
	}
	if r.Error != "" {
		t.Fatalf("expected no error, got %q", r.Error)
	}
	if len(sender.badAddrs) != 1 || sender.badAddrs[0] != addrHash {
		t.Fatalf("expected one bad address %q, got %v", addrHash, sender.badAddrs)
	}
}

// TestNotifyUser_TransientFailureDoesNotDisable confirms that a transient
// send error returns a send failure but does NOT call the disable handler.
func TestNotifyUser_TransientFailureDoesNotDisable(t *testing.T) {
	addr := testAddress("good", "hash-transient")

	repo := &fakeRepository{
		addresses: []NotificationAddress{addr},
		preferences: &NotificationPreferences{
			UserID:  "user-1",
			Enabled: true,
			Channels: map[string]bool{
				string(NotificationChannelWebPush): true,
			},
		},
	}

	sender := &cleanupTrackingSender{
		channel:        NotificationChannelWebPush,
		transientError: fmt.Errorf("dial tcp: i/o timeout"),
	}

	service := NewService(&NewServiceRequest{
		Repository: repo,
		Senders:    []ChannelSender{sender},
	})

	response, err := service.NotifyUser(context.Background(), &NotifyUserRequest{
		UserID:  "user-1",
		Title:   "Hello",
		Message: "World",
	})
	if err == nil {
		t.Fatal("expected send error for transient failure")
	}
	if repo.disabledHash != "" {
		t.Fatalf("expected no disable, got disabledHash %q", repo.disabledHash)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(response.Results))
	}
	r := response.Results[0]
	if r.Sent {
		t.Fatal("expected Sent=false (transient failure)")
	}
	if r.Cleaned != 0 {
		t.Fatalf("expected Cleaned=0, got %d", r.Cleaned)
	}
	if r.Error == "" {
		t.Fatal("expected error message in result")
	}
	if len(sender.badAddrs) != 0 {
		t.Fatalf("expected no bad addresses, got %v", sender.badAddrs)
	}
}

// TestNotifyUser_MixedValidAndInvalidAddresses — one valid + one expired.
// The expired address is disabled, the valid one succeeds.  Sent=true,
// Cleaned=1, no error.
func TestNotifyUser_MixedValidAndInvalidAddresses(t *testing.T) {
	validAddr := testAddress("valid", "hash-valid")
	expiredAddr := testAddress("expired", "hash-expired")
	expiredHash := expiredAddr.AddressHash

	repo := &fakeRepository{
		addresses: []NotificationAddress{validAddr, expiredAddr},
		preferences: &NotificationPreferences{
			UserID:  "user-1",
			Enabled: true,
			Channels: map[string]bool{
				string(NotificationChannelWebPush): true,
			},
		},
	}

	sender := &cleanupTrackingSender{
		channel:       NotificationChannelWebPush,
		invalidHashes: map[string]bool{expiredHash: true},
	}

	service := NewService(&NewServiceRequest{
		Repository: repo,
		Senders:    []ChannelSender{sender},
	})

	response, err := service.NotifyUser(context.Background(), &NotifyUserRequest{
		UserID:  "user-1",
		Title:   "Hello",
		Message: "World",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if repo.disabledHash != expiredHash {
		t.Fatalf("expected disabledHash %q, got %q", expiredHash, repo.disabledHash)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(response.Results))
	}
	r := response.Results[0]
	if r.Attempted != 2 {
		t.Fatalf("expected attempted=2, got %d", r.Attempted)
	}
	if !r.Sent {
		t.Fatal("expected Sent=true (one valid delivery)")
	}
	if r.Cleaned != 1 {
		t.Fatalf("expected Cleaned=1, got %d", r.Cleaned)
	}
	if r.Error != "" {
		t.Fatalf("expected no error, got %q", r.Error)
	}
	if len(sender.sentAddrs) != 2 {
		t.Fatalf("expected 2 attempted addresses, got %d", len(sender.sentAddrs))
	}
}

// TestNotifyUser_CleanupFailure surfaces error when the repository fails
// to disable an expired address.  NotifyUser must return an error.
func TestNotifyUser_CleanupFailure(t *testing.T) {
	addr := testAddress("bad", "hash-cleanup-fail")
	addrHash := addr.AddressHash

	repo := &fakeRepository{
		addresses: []NotificationAddress{addr},
		preferences: &NotificationPreferences{
			UserID:  "user-1",
			Enabled: true,
			Channels: map[string]bool{
				string(NotificationChannelWebPush): true,
			},
		},
		disableError: fmt.Errorf("mongo: connection lost"),
	}

	sender := &cleanupTrackingSender{
		channel:       NotificationChannelWebPush,
		invalidHashes: map[string]bool{addrHash: true},
	}

	service := NewService(&NewServiceRequest{
		Repository: repo,
		Senders:    []ChannelSender{sender},
	})

	_, err := service.NotifyUser(context.Background(), &NotifyUserRequest{
		UserID:  "user-1",
		Title:   "Hello",
		Message: "World",
	})
	if err == nil {
		t.Fatal("expected error when cleanup handler fails")
	}
	// disabledHash should NOT be set because DisableAddressByHash returned an error
	if repo.disabledHash != "" {
		t.Fatalf("expected disabledHash empty (disable failed), got %q", repo.disabledHash)
	}
}

// TestNotifyUser_CleanupCountDoesNotLeakBetweenSends reuses one sender
// across two NotifyUser calls. The second call must not inherit the cleanup
// count from the first call.
func TestNotifyUser_CleanupCountDoesNotLeakBetweenSends(t *testing.T) {
	expiredAddr := testAddress("expired", "hash-expired")
	validAddr := testAddress("valid", "hash-valid")

	repo := &fakeRepository{
		addresses: []NotificationAddress{expiredAddr},
		preferences: &NotificationPreferences{
			UserID:  "user-1",
			Enabled: true,
			Channels: map[string]bool{
				string(NotificationChannelWebPush): true,
			},
		},
	}

	sender := &cleanupTrackingSender{
		channel:       NotificationChannelWebPush,
		invalidHashes: map[string]bool{expiredAddr.AddressHash: true},
	}

	service := NewService(&NewServiceRequest{
		Repository: repo,
		Senders:    []ChannelSender{sender},
	})

	firstResponse, err := service.NotifyUser(context.Background(), &NotifyUserRequest{
		UserID:  "user-1",
		Title:   "Hello",
		Message: "World",
	})
	if err != nil {
		t.Fatalf("expected first send to succeed, got %v", err)
	}
	if len(firstResponse.Results) != 1 || firstResponse.Results[0].Cleaned != 1 || firstResponse.Results[0].Sent {
		t.Fatalf("expected first result cleaned-only, got %#v", firstResponse.Results)
	}

	repo.addresses = []NotificationAddress{validAddr}
	sender.invalidHashes = map[string]bool{}

	secondResponse, err := service.NotifyUser(context.Background(), &NotifyUserRequest{
		UserID:  "user-1",
		Title:   "Hello again",
		Message: "World again",
	})
	if err != nil {
		t.Fatalf("expected second send to succeed, got %v", err)
	}
	if len(secondResponse.Results) != 1 {
		t.Fatalf("expected one second result, got %d", len(secondResponse.Results))
	}
	if secondResponse.Results[0].Cleaned != 0 {
		t.Fatalf("expected cleaned count to reset to 0, got %#v", secondResponse.Results[0])
	}
	if !secondResponse.Results[0].Sent {
		t.Fatalf("expected second result to be sent, got %#v", secondResponse.Results[0])
	}
}

// ---------------------------------------------------------------------------
// NotifyUsers tests
// ---------------------------------------------------------------------------

func TestNotifyUsers_TargetedMultiUser(t *testing.T) {
	sender := &fakeSender{channel: NotificationChannelWebPush, enabled: true}
	repository := &fakeRepository{
		addresses: []NotificationAddress{
			{
				ID: "addr-1", UserID: "user-1", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive,
				WebPush: &WebPushAddress{Endpoint: "https://push.example/1", Keys: WebPushKeys{Auth: "a", P256DH: "k"}},
			},
			{
				ID: "addr-2", UserID: "user-2", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive,
				WebPush: &WebPushAddress{Endpoint: "https://push.example/2", Keys: WebPushKeys{Auth: "a", P256DH: "k"}},
			},
		},
		preferences: &NotificationPreferences{
			UserID:  "user-1",
			Enabled: true,
			Channels: map[string]bool{
				string(NotificationChannelWebPush): true,
			},
		},
	}
	service := NewService(&NewServiceRequest{
		Repository: repository,
		Senders:    []ChannelSender{sender},
	})

	response, err := service.NotifyUsers(context.Background(), &NotifyUsersRequest{
		UserIDs: []string{"user-1", "user-2"},
		Title:   "Hello",
		Message: "World",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(response.Results) != 2 {
		t.Fatalf("expected 2 user results, got %d", len(response.Results))
	}
	if response.Results[0].UserID != "user-1" || response.Results[1].UserID != "user-2" {
		t.Fatalf("unexpected user order: %v", response.Results)
	}
	if len(response.Results[0].Results) != 1 || !response.Results[0].Results[0].Sent {
		t.Fatalf("expected user-1 sent, got %#v", response.Results[0].Results)
	}
	if len(response.Results[1].Results) != 1 || !response.Results[1].Results[0].Sent {
		t.Fatalf("expected user-2 sent, got %#v", response.Results[1].Results)
	}
}

func TestNotifyUsers_BroadcastAllUsers(t *testing.T) {
	sender := &fakeSender{channel: NotificationChannelWebPush, enabled: true}
	repository := &fakeRepository{
		addresses: []NotificationAddress{
			{
				ID: "addr-1", UserID: "user-a", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive,
				WebPush: &WebPushAddress{Endpoint: "https://push.example/a", Keys: WebPushKeys{Auth: "a", P256DH: "k"}},
			},
			{
				ID: "addr-2", UserID: "user-b", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive,
				WebPush: &WebPushAddress{Endpoint: "https://push.example/b", Keys: WebPushKeys{Auth: "a", P256DH: "k"}},
			},
			{
				ID: "addr-3", UserID: "user-c", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive,
				WebPush: &WebPushAddress{Endpoint: "https://push.example/c", Keys: WebPushKeys{Auth: "a", P256DH: "k"}},
			},
			{
				ID: "addr-4", UserID: "user-c", Channel: NotificationChannelFCM, Status: NotificationAddressStatusActive,
				FCM: &FCMAddress{Token: "fcm-token-c"},
			},
		},
		preferences: &NotificationPreferences{
			UserID:  "user-a",
			Enabled: true,
			Channels: map[string]bool{
				string(NotificationChannelWebPush): true,
			},
		},
	}
	fcmSender := &fakeSender{channel: NotificationChannelFCM, enabled: true}
	service := NewService(&NewServiceRequest{
		Repository: repository,
		Senders:    []ChannelSender{sender, fcmSender},
	})

	response, err := service.NotifyUsers(context.Background(), &NotifyUsersRequest{
		Title:   "Broadcast",
		Message: "All users",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(response.Results) != 3 {
		t.Fatalf("expected 3 user results, got %d", len(response.Results))
	}

	userIDs := map[string]bool{}
	for _, r := range response.Results {
		userIDs[r.UserID] = true
	}
	if !userIDs["user-a"] || !userIDs["user-b"] || !userIDs["user-c"] {
		t.Fatalf("expected user-a, user-b, user-c, got %v", userIDs)
	}
}

func TestNotifyUsers_TableTests(t *testing.T) {
	tests := []struct {
		name          string
		req           *NotifyUsersRequest
		addresses     []NotificationAddress
		prefs         *NotificationPreferences
		prefsByUserID map[string]*NotificationPreferences
		wantErr       error
		wantUsers     int
		wantSent      map[string]bool
	}{
		{
			name: "GOOD targeted multi-user",
			req:  &NotifyUsersRequest{UserIDs: []string{"u1", "u2"}, Title: "Hi", Message: "There"},
			addresses: []NotificationAddress{
				{ID: "a1", UserID: "u1", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive, WebPush: &WebPushAddress{Endpoint: "https://e1", Keys: WebPushKeys{Auth: "a", P256DH: "k"}}},
				{ID: "a2", UserID: "u2", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive, WebPush: &WebPushAddress{Endpoint: "https://e2", Keys: WebPushKeys{Auth: "a", P256DH: "k"}}},
			},
			prefs:     &NotificationPreferences{UserID: "u1", Enabled: true, Channels: map[string]bool{string(NotificationChannelWebPush): true}},
			wantUsers: 2,
			wantSent:  map[string]bool{"u1": true, "u2": true},
		},
		{
			name: "GOOD broadcast all",
			req:  &NotifyUsersRequest{Title: "Hi", Message: "There"},
			addresses: []NotificationAddress{
				{ID: "a1", UserID: "u1", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive, WebPush: &WebPushAddress{Endpoint: "https://e1", Keys: WebPushKeys{Auth: "a", P256DH: "k"}}},
				{ID: "a2", UserID: "u2", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive, WebPush: &WebPushAddress{Endpoint: "https://e2", Keys: WebPushKeys{Auth: "a", P256DH: "k"}}},
			},
			prefs:     &NotificationPreferences{UserID: "u1", Enabled: true, Channels: map[string]bool{string(NotificationChannelWebPush): true}},
			wantUsers: 2,
			wantSent:  map[string]bool{"u1": true, "u2": true},
		},
		{
			name: "GOOD targeted multi-user honours per-user disabled preference",
			req:  &NotifyUsersRequest{UserIDs: []string{"u1", "u2"}, Title: "Hi", Message: "There"},
			addresses: []NotificationAddress{
				{ID: "a1", UserID: "u1", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive, WebPush: &WebPushAddress{Endpoint: "https://e1", Keys: WebPushKeys{Auth: "a", P256DH: "k"}}},
				{ID: "a2", UserID: "u2", Channel: NotificationChannelWebPush, Status: NotificationAddressStatusActive, WebPush: &WebPushAddress{Endpoint: "https://e2", Keys: WebPushKeys{Auth: "a", P256DH: "k"}}},
			},
			prefsByUserID: map[string]*NotificationPreferences{
				"u1": {UserID: "u1", Enabled: true, Channels: map[string]bool{string(NotificationChannelWebPush): true}},
				"u2": {UserID: "u2", Enabled: false, Channels: map[string]bool{string(NotificationChannelWebPush): true}},
			},
			wantUsers: 2,
			wantSent:  map[string]bool{"u1": true, "u2": false},
		},
		{
			name:    "BAD missing title",
			req:     &NotifyUsersRequest{UserIDs: []string{"u1"}, Message: "There"},
			wantErr: ErrInvalidNotificationAddressBody,
		},
		{
			name:    "BAD missing message",
			req:     &NotifyUsersRequest{UserIDs: []string{"u1"}, Title: "Hi"},
			wantErr: ErrInvalidNotificationAddressBody,
		},
		{
			name:    "BAD missing both title and message",
			req:     &NotifyUsersRequest{UserIDs: []string{"u1"}},
			wantErr: ErrInvalidNotificationAddressBody,
		},
		{
			name:    "BAD invalid channel",
			req:     &NotifyUsersRequest{UserIDs: []string{"u1"}, Title: "Hi", Message: "There", Channels: []NotificationChannel{"SMS"}},
			wantErr: ErrInvalidNotificationChannel,
		},
		{
			name:      "GOOD empty user ids with no active addresses",
			req:       &NotifyUsersRequest{Title: "Hi", Message: "There"},
			wantUsers: 0,
		},
		{
			name:      "GOOD user with no addresses gets empty result",
			req:       &NotifyUsersRequest{UserIDs: []string{"u1"}, Title: "Hi", Message: "There"},
			wantUsers: 1,
			wantSent:  map[string]bool{"u1": false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{channel: NotificationChannelWebPush, enabled: true}
			repo := &fakeRepository{
				addresses:           tt.addresses,
				preferences:         tt.prefs,
				preferencesByUserID: tt.prefsByUserID,
			}
			service := NewService(&NewServiceRequest{
				Repository: repo,
				Senders:    []ChannelSender{sender},
			})

			response, err := service.NotifyUsers(context.Background(), tt.req)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if len(response.Results) != tt.wantUsers {
				t.Fatalf("expected %d user results, got %d", tt.wantUsers, len(response.Results))
			}
			if tt.wantSent != nil {
				for _, r := range response.Results {
					expectedSent, ok := tt.wantSent[r.UserID]
					if !ok {
						t.Fatalf("unexpected user %q in results", r.UserID)
					}
					actualSent := len(r.Results) > 0 && r.Results[0].Sent
					if actualSent != expectedSent {
						t.Fatalf("user %q: expected sent=%v, got %v", r.UserID, expectedSent, actualSent)
					}
				}
			}
		})
	}
}
