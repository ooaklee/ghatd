package notifier

import (
	"context"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/nikoksr/notify"
	notifyfcm "github.com/nikoksr/notify/service/fcm"
	notifywebpush "github.com/nikoksr/notify/service/webpush"
)

// ChannelSender is the interface that every notification delivery adapter
// must implement.
//
// A ChannelSender knows how to take a list of addresses for one channel
// (e.g. a list of browser Push subscriptions or a list of FCM tokens)
// and deliver a notification to all of them.
//
// The Service holds a map of ChannelSenders and dispatches to the right
// one based on the user's registered addresses and preferences.
type ChannelSender interface {
	Channel() NotificationChannel
	Enabled() bool
	Send(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) error
}

// WebPushSenderConfig contains the settings needed to send push
// notifications to browsers using the Web Push protocol.
//
//   - Enabled: set to true when the server has VAPID keys configured.
//   - VAPIDPublicKey: the public key that browsers use to subscribe.
//   - VAPIDPrivateKey: the private key that GHATD uses to sign push messages.
//
// VAPID (Voluntary Application Server Identification) is the standard
// that lets browsers know which server is allowed to send them push
// notifications. You can generate a VAPID key pair with tools like
// the webpush-go library or a command-line utility.
type WebPushSenderConfig struct {
	Enabled         bool
	VAPIDPublicKey  string
	VAPIDPrivateKey string
}

// WebPushSender sends browser push notifications via the nikoksr/notify
// library's webpush service.
//
// When the service calls Send(), the sender converts the notifier address
// list into the subscription format that notifywebpush expects, creates
// a notifier service instance with the configured VAPID keys, and
// delivers the message.
//
// The sender is automatically disabled at startup if VAPID keys are not
// provided, so deployments without push notification support can still
// use the rest of the notifier package.
type WebPushSender struct {
	config WebPushSenderConfig
}

// NewWebPushSender creates a Web Push sender from the given config.
func NewWebPushSender(config WebPushSenderConfig) *WebPushSender {
	return &WebPushSender{config: config}
}

// Channel always returns NotificationChannelWebPush.
func (s *WebPushSender) Channel() NotificationChannel {
	return NotificationChannelWebPush
}

// Enabled returns true when the server has both a public and private
// VAPID key configured and push is not explicitly disabled.
//
// Without both keys, the sender cannot sign push messages, so it is
// treated as unavailable.
func (s *WebPushSender) Enabled() bool {
	return s != nil && s.config.Enabled && s.config.VAPIDPublicKey != "" && s.config.VAPIDPrivateKey != ""
}

// PublicKey returns the VAPID public key that browsers need when they
// call pushManager.subscribe().
func (s *WebPushSender) PublicKey() string {
	if s == nil {
		return ""
	}
	return s.config.VAPIDPublicKey
}

// Send delivers a push notification to the given browser subscriptions.
//
// Each NotificationAddress in the addresses list must have a non-nil
// WebPush field with a valid endpoint and keys. Addresses that are not
// WEBPUSH or that have nil WebPush payloads are silently skipped.
//
// The method:
//  1. Converts the notifier address list into notifywebpush.Subscription
//     objects that the library understands.
//  2. Creates a notifywebpush service instance authenticated with the
//     configured VAPID keys.
//  3. Optionally attaches extra data from the request (e.g. a URL to
//     open when the user clicks the notification).
//  4. Sends the notification through nikoksr/notify, which handles
//     the HTTP POST to each browser's push endpoint.
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

// FCMSenderConfig contains the settings needed to send push
// notifications through Firebase Cloud Messaging.
//
//   - Enabled: set to true when at least one form of Firebase credentials
//     is available.
//   - CredentialsFile: path to a Firebase service account JSON key file.
//   - ProjectID: the Firebase project identifier (alternative to credentials
//     file for environments where the default application credentials are
//     already configured).
//
// In v1, the FCM sender is wired up but disabled by default because
// Firebase credentials are not yet available. The infrastructure is
// ready to flip the switch when Firebase configuration arrives.
type FCMSenderConfig struct {
	Enabled         bool
	CredentialsFile string
	ProjectID       string
}

// FCMSender sends push notifications to mobile devices through
// Firebase Cloud Messaging using nikoksr/notify's FCM service.
//
// The sender is designed to be quiet when Firebase is not configured.
// If Enabled is false or no credentials are provided, the sender
// reports as unavailable and the service skips FCM delivery.
type FCMSender struct {
	config FCMSenderConfig
}

// NewFCMSender creates an FCM sender from the given config.
func NewFCMSender(config FCMSenderConfig) *FCMSender {
	return &FCMSender{config: config}
}

// Channel always returns NotificationChannelFCM.
func (s *FCMSender) Channel() NotificationChannel {
	return NotificationChannelFCM
}

// Enabled returns true when FCM is configured with at least one form
// of credentials (a credentials file or a project ID).
func (s *FCMSender) Enabled() bool {
	return s != nil && s.config.Enabled && (s.config.CredentialsFile != "" || s.config.ProjectID != "")
}

// Send delivers a push notification through Firebase Cloud Messaging.
//
// Each NotificationAddress in the addresses list must have a non-nil
// FCM field with a valid token. Addresses that are not FCM or that
// have empty tokens are silently skipped.
//
// The method:
//  1. Extracts the valid FCM tokens from the address list.
//  2. Creates an FCM notify service instance authenticated with the
//     configured credentials (file or project ID).
//  3. Adds the tokens as receivers.
//  4. Sends the notification through nikoksr/notify.
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
