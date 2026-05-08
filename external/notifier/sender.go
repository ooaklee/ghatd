package notifier

import (
	"context"
	"errors"
	"fmt"
	"regexp"

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

// InvalidAddressCleanable is implemented by senders that can receive a
// callback to disable or delete an address that is permanently invalid.
//
// The notifier Service detects this interface on registered senders and
// wires the callback to its repository so expired subscriptions are
// automatically cleaned up during delivery without leaking into every
// sender implementation.
type InvalidAddressCleanable interface {
	SetInvalidAddressHandler(handler func(ctx context.Context, hash string) error)
}

// channelSendReport describes the result of one sender call.
type channelSendReport struct {
	Delivered int
	Cleaned   int
}

// detailedChannelSender is implemented by senders that can report per-call
// delivery and cleanup counts without storing mutable result state on the
// shared sender instance.
type detailedChannelSender interface {
	SendWithReport(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) (channelSendReport, error)
}

// permanentWebPushRe matches the webpush-go library's "unexpected status
// code: XXX" error format.  We only consider 404 Not Found and 410 Gone
// to be permanent – everything else is a transient delivery hiccup.
var permanentWebPushRe = regexp.MustCompile(`unexpected status code:\s*(404|410)(?:\D|$)`)

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
// The sender sends to each address individually so it can detect which
// subscription is permanently gone (HTTP 410 Gone or 404 Not Found)
// versus a transient delivery hiccup (5xx, network timeout).  When a
// permanent failure is detected the sender calls its
// invalidAddressHandler callback — which the Service wires to
// DisableAddressByHash — so the address is automatically disabled and
// will not be retried on future sends.
//
// Transient errors are returned as-is and do not trigger cleanup.
type WebPushSender struct {
	config                WebPushSenderConfig
	invalidAddressHandler func(ctx context.Context, hash string) error
}

// NewWebPushSender creates a Web Push sender from the given config.
func NewWebPushSender(config WebPushSenderConfig) *WebPushSender {
	return &WebPushSender{config: config}
}

// SetInvalidAddressHandler sets the callback invoked when a Web Push
// subscription is determined to be permanently invalid (gone/unsubscribed).
//
// The handler receives the address hash (SHA-256 of channel:identity)
// which the repository uses to find and disable the address.
func (s *WebPushSender) SetInvalidAddressHandler(handler func(ctx context.Context, hash string) error) {
	s.invalidAddressHandler = handler
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
// Each address is sent individually so the sender can distinguish
// permanent failures (HTTP 410 Gone / 404 Not Found — the subscription
// no longer exists) from transient failures (5xx, DNS, timeout — the
// push service is unreachable right now).
//
//   - Permanent failures trigger the invalidAddressHandler callback
//     (if set) so the address is disabled and will not be retried.
//     If the cleanup handler itself fails, that error is joined into
//     the result so NotifyUser does not silently claim success.
//   - Transient failures are collected and returned as a combined error.
//
// The overall error returned to the caller is nil unless one or more
// addresses experienced a transient failure OR the cleanup handler failed.
func (s *WebPushSender) Send(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) error {
	_, err := s.SendWithReport(ctx, subject, message, addresses, data)
	return err
}

// SendWithReport delivers a push notification and returns per-call delivery
// details. Unlike LastSend-style state, this report is local to the current
// call and is safe for the service to use when senders are shared.
func (s *WebPushSender) SendWithReport(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) (channelSendReport, error) {
	report := channelSendReport{}
	if !s.Enabled() {
		return report, ErrNotificationSenderNotEnabled
	}

	valid := make([]NotificationAddress, 0, len(addresses))
	for _, address := range addresses {
		if address.Channel == NotificationChannelWebPush && address.WebPush != nil {
			valid = append(valid, address)
		}
	}
	if len(valid) == 0 {
		return report, ErrNotificationNoActiveAddresses
	}

	var sendErrs []error
	for _, address := range valid {
		err := s.sendOne(ctx, subject, message, address, data)
		if err == nil {
			report.Delivered++
			continue
		}
		if isPermanentWebPushError(err) {
			if s.invalidAddressHandler != nil {
				if cleanupErr := s.invalidAddressHandler(ctx, address.AddressHash); cleanupErr != nil {
					sendErrs = append(sendErrs, fmt.Errorf("cleanup failed for address %s: %w", address.AddressHash, cleanupErr))
					continue
				}
				report.Cleaned++
			}
			continue
		}
		sendErrs = append(sendErrs, err)
	}

	if len(sendErrs) > 0 {
		return report, errors.Join(sendErrs...)
	}
	return report, nil
}

// sendOne delivers the message to a single Web Push address.
func (s *WebPushSender) sendOne(ctx context.Context, subject, message string, address NotificationAddress, data map[string]interface{}) error {
	subscription := notifywebpush.Subscription{
		Endpoint: address.WebPush.Endpoint,
		Keys: webpush.Keys{
			Auth:   address.WebPush.Keys.Auth,
			P256dh: address.WebPush.Keys.P256DH,
		},
	}

	webPushService := notifywebpush.New(s.config.VAPIDPublicKey, s.config.VAPIDPrivateKey)
	webPushService.AddReceivers(subscription)
	if len(data) > 0 {
		ctx = notifywebpush.WithData(ctx, data)
	}

	return notify.NewWithServices(webPushService).Send(ctx, subject, message)
}

// isPermanentWebPushError returns true when the error matches the
// webpush-go library's "unexpected status code: NNN" format with a
// status of 404 (Not Found) or 410 (Gone).
//
// These two codes mean the browser has unsubscribed and the server
// should stop sending to this address.  The function uses a regex to
// avoid false-positives on error messages that merely contain "404"
// or "410" as arbitrary substrings (e.g. a timeout of "410ms" or an
// endpoint URL containing "/404/").
func isPermanentWebPushError(err error) bool {
	if err == nil {
		return false
	}
	return permanentWebPushRe.MatchString(err.Error())
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
//
// An invalidAddressHandler field and SetInvalidAddressHandler setter
// are included for future FCM token invalidation support but are not
// wired in v1.
type FCMSender struct {
	config                FCMSenderConfig
	invalidAddressHandler func(ctx context.Context, hash string) error
}

// NewFCMSender creates an FCM sender from the given config.
func NewFCMSender(config FCMSenderConfig) *FCMSender {
	return &FCMSender{config: config}
}

// SetInvalidAddressHandler sets the callback invoked when an FCM
// token is determined to be permanently invalid (not yet wired in v1).
func (s *FCMSender) SetInvalidAddressHandler(handler func(ctx context.Context, hash string) error) {
	s.invalidAddressHandler = handler
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
