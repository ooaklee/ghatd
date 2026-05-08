package notifier

import (
	"context"
	"testing"
)

type fakeRepository struct {
	addresses   []NotificationAddress
	preferences *NotificationPreferences
	upserted    *NotificationAddress
	deletedID   string
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
