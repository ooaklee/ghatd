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
	addresses    []NotificationAddress
	preferences  *NotificationPreferences
	upserted     *NotificationAddress
	deletedID    string
	disabledHash string
}

func (r *fakeRepository) UpsertAddress(ctx context.Context, address *NotificationAddress) (*NotificationAddress, error) {
	r.upserted = address
	return address, nil
}

func (r *fakeRepository) GetActiveAddressesByUserID(ctx context.Context, userID string, channels ...NotificationChannel) ([]NotificationAddress, error) {
	return r.addresses, nil
}

func (r *fakeRepository) GetAddressesByUserID(ctx context.Context, userID string) ([]NotificationAddress, error) {
	return r.addresses, nil
}

func (r *fakeRepository) DeleteAddressByIDForUser(ctx context.Context, userID, addressID string) error {
	r.deletedID = addressID
	return nil
}

func (r *fakeRepository) DeleteAddressesByUserID(ctx context.Context, userID string) error {
	return nil
}

func (r *fakeRepository) DisableAddressByHash(ctx context.Context, hash string) error {
	r.disabledHash = hash
	return nil
}

func (r *fakeRepository) GetPreferencesByUserID(ctx context.Context, userID string) (*NotificationPreferences, error) {
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
//     treated as a permanent (410/404) failure.  The sender calls
//     its invalidAddressHandler for these.
//   - transientError is returned for addresses NOT in invalidHashes.
//     Set it to nil to simulate successful delivery.
//
// The sender records every address hash sent to and every hash
// forwarded to the invalidAddressHandler.
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
	var transientErrs []error
	for _, addr := range addresses {
		c.sentAddrs = append(c.sentAddrs, addr.AddressHash)
		if c.invalidHashes != nil && c.invalidHashes[addr.AddressHash] {
			c.badAddrs = append(c.badAddrs, addr.AddressHash)
			if c.invalidAddressHandler != nil {
				_ = c.invalidAddressHandler(ctx, addr.AddressHash)
			}
			continue
		}
		if c.transientError != nil {
			transientErrs = append(transientErrs, c.transientError)
		}
	}
	if len(transientErrs) > 0 {
		return errors.Join(transientErrs...)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Existing tests
// ---------------------------------------------------------------------------

// TestRegisterAddress_WebPushUpsertsActiveAddress verifies that
// registering a valid Web Push address creates an ACTIVE address
// record with the correct channel, user ID, and a computed address hash.
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

// TestRegisterAddress_RejectsInvalidChannelPayload checks that the
// service rejects a Web Push registration that is missing endpoint
// and key data.
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

// TestNotifyUser_SendsOnlyWhenPreferencesAllowChannel verifies that
// NotifyUser respects per-channel preferences and only attempts to
// send on channels that are enabled.
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

// TestNotifyUser_SkipsWhenPreferencesDisabled checks that NotifyUser
// does not attempt any sends when the user has globally disabled
// notifications, even when active addresses exist.
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

// TestNotifyUser_DisablesExpiredAddress calls NotifyUser when the sender
// flags one address as permanently invalid.  The repository must record
// the disabled hash, and the send result must still report Sent=true
// because no transient error occurred.
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
	if len(response.Results) != 1 || !response.Results[0].Sent {
		t.Fatalf("expected sent result, got %#v", response.Results)
	}
	// the expired address was forwarded to the handler
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
	if len(response.Results) != 1 || response.Results[0].Sent {
		t.Fatalf("expected not-sent result, got %#v", response.Results)
	}
	if len(sender.badAddrs) != 0 {
		t.Fatalf("expected no bad addresses, got %v", sender.badAddrs)
	}
}

// TestNotifyUser_MixedValidAndInvalidAddresses has one valid address and
// one expired address.  The expired address should be disabled, the valid
// one should succeed, and the overall send should be successful (no error).
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
	if len(response.Results) != 1 || !response.Results[0].Sent {
		t.Fatalf("expected sent result, got %#v", response.Results)
	}
	// both addresses were attempted
	if len(sender.sentAddrs) != 2 {
		t.Fatalf("expected 2 attempted addresses, got %d", len(sender.sentAddrs))
	}
}

// TestNotifyUser_ResultReflectsCleanup verifies that NotifyUser result
// shows Sent=true when the only failure is a cleaned-up expired address
// (no transient errors), and that the attempt count includes all addresses.
func TestNotifyUser_ResultReflectsCleanup(t *testing.T) {
	addr := testAddress("bad", "hash-only-expired")
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
	if len(response.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(response.Results))
	}
	r := response.Results[0]
	if r.Attempted != 1 {
		t.Fatalf("expected attempted 1, got %d", r.Attempted)
	}
	if !r.Sent {
		t.Fatal("expected Sent=true (expired cleanup is not a send failure)")
	}
	if r.Skipped {
		t.Fatal("expected Skipped=false")
	}
	if r.Error != "" {
		t.Fatalf("expected no error, got %q", r.Error)
	}
}
