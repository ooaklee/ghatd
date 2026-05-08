package notifier

import (
	"context"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/nikoksr/notify"
	notifyfcm "github.com/nikoksr/notify/service/fcm"
	notifywebpush "github.com/nikoksr/notify/service/webpush"
)

// ChannelSender sends notifications for one channel.
type ChannelSender interface {
	Channel() NotificationChannel
	Enabled() bool
	Send(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) error
}

// WebPushSenderConfig configures Web Push sending.
type WebPushSenderConfig struct {
	Enabled         bool
	VAPIDPublicKey  string
	VAPIDPrivateKey string
}

// WebPushSender sends browser Web Push notifications through nikoksr/notify.
type WebPushSender struct {
	config WebPushSenderConfig
}

// NewWebPushSender creates a Web Push sender.
func NewWebPushSender(config WebPushSenderConfig) *WebPushSender {
	return &WebPushSender{config: config}
}

// Channel returns the sender channel.
func (s *WebPushSender) Channel() NotificationChannel {
	return NotificationChannelWebPush
}

// Enabled returns true when this sender has enough config to send.
func (s *WebPushSender) Enabled() bool {
	return s != nil && s.config.Enabled && s.config.VAPIDPublicKey != "" && s.config.VAPIDPrivateKey != ""
}

// PublicKey returns the configured VAPID public key.
func (s *WebPushSender) PublicKey() string {
	if s == nil {
		return ""
	}
	return s.config.VAPIDPublicKey
}

// Send sends a Web Push notification.
func (s *WebPushSender) Send(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) error {
	if !s.Enabled() {
		return ErrNotificationSenderNotEnabled
	}

	subscriptions := make([]notifywebpush.Subscription, 0, len(addresses))
	for _, address := range addresses {
		if address.Channel != NotificationChannelWebPush || address.WebPush == nil {
			continue
		}

		subscriptions = append(subscriptions, notifywebpush.Subscription{
			Endpoint: address.WebPush.Endpoint,
			Keys: webpush.Keys{
				Auth:   address.WebPush.Keys.Auth,
				P256dh: address.WebPush.Keys.P256DH,
			},
		})
	}

	if len(subscriptions) == 0 {
		return ErrNotificationNoActiveAddresses
	}

	webPushService := notifywebpush.New(s.config.VAPIDPublicKey, s.config.VAPIDPrivateKey)
	webPushService.AddReceivers(subscriptions...)
	if len(data) > 0 {
		ctx = notifywebpush.WithData(ctx, data)
	}

	return notify.NewWithServices(webPushService).Send(ctx, subject, message)
}

// FCMSenderConfig configures Firebase Cloud Messaging.
type FCMSenderConfig struct {
	Enabled         bool
	CredentialsFile string
	ProjectID       string
}

// FCMSender sends FCM notifications through nikoksr/notify.
type FCMSender struct {
	config FCMSenderConfig
}

// NewFCMSender creates an FCM sender.
func NewFCMSender(config FCMSenderConfig) *FCMSender {
	return &FCMSender{config: config}
}

// Channel returns the sender channel.
func (s *FCMSender) Channel() NotificationChannel {
	return NotificationChannelFCM
}

// Enabled returns true when FCM is configured.
func (s *FCMSender) Enabled() bool {
	return s != nil && s.config.Enabled && (s.config.CredentialsFile != "" || s.config.ProjectID != "")
}

// Send sends an FCM notification.
func (s *FCMSender) Send(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) error {
	if !s.Enabled() {
		return ErrNotificationSenderNotEnabled
	}

	tokens := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address.Channel != NotificationChannelFCM || address.FCM == nil || address.FCM.Token == "" {
			continue
		}
		tokens = append(tokens, address.FCM.Token)
	}

	if len(tokens) == 0 {
		return ErrNotificationNoActiveAddresses
	}

	options := []notifyfcm.Option{}
	if s.config.CredentialsFile != "" {
		options = append(options, notifyfcm.WithCredentialsFile(s.config.CredentialsFile))
	}
	if s.config.ProjectID != "" {
		options = append(options, notifyfcm.WithProjectID(s.config.ProjectID))
	}

	fcmService, err := notifyfcm.New(ctx, options...)
	if err != nil {
		return err
	}
	fcmService.AddReceivers(tokens...)

	return notify.NewWithServices(fcmService).Send(ctx, subject, message)
}
