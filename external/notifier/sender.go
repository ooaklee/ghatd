package notifier

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"firebase.google.com/go/v4/messaging"
	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/appleboy/go-fcm"
	"github.com/nikoksr/notify"
	notifywebpush "github.com/nikoksr/notify/service/webpush"
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
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
	logger := logger.AcquireOperationFrom(ctx, "external/notifier", "send")
	logger.Debug("handling-send-request")

	_, err := s.SendWithReport(ctx, subject, message, addresses, data)
	return err
}

// SendWithReport delivers a push notification and returns per-call delivery
// details. Unlike LastSend-style state, this report is local to the current
// call and is safe for the service to use when senders are shared.
func (s *WebPushSender) SendWithReport(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) (channelSendReport, error) {
	report := channelSendReport{}
	logger := logger.AcquirePackageFrom(ctx, "external/notifier").With(
		zap.String("operation", "webpush-send"),
		zap.String("channel", string(NotificationChannelWebPush)),
	)

	if !s.Enabled() {
		logger.Warn(
			"webpush-sender-not-enabled",
			zap.Bool("enabled", s != nil && s.config.Enabled),
			zap.Bool("has-vapid-public-key", s != nil && s.config.VAPIDPublicKey != ""),
			zap.Bool("has-vapid-private-key", s != nil && s.config.VAPIDPrivateKey != ""),
			zap.Int("attempted-addresses", len(addresses)),
			zap.Error(ErrNotificationSenderNotEnabled),
		)
		return report, ErrNotificationSenderNotEnabled
	}

	valid := make([]NotificationAddress, 0, len(addresses))
	for _, address := range addresses {
		if address.Channel == NotificationChannelWebPush && address.WebPush != nil {
			valid = append(valid, address)
		}
	}
	if len(valid) == 0 {
		logger.Warn("webpush-send-no-valid-addresses", zap.Int("attempted-addresses", len(addresses)), zap.Error(ErrNotificationNoActiveAddresses))
		return report, ErrNotificationNoActiveAddresses
	}

	logger.Info("webpush-send-started", zap.Int("attempted-addresses", len(addresses)), zap.Int("valid-addresses", len(valid)), zap.Strings("data-keys", notificationDataKeysForLog(data)))

	var sendErrs []error
	for _, address := range valid {
		err := s.sendOne(ctx, subject, message, address, data)
		if err == nil {
			report.Delivered++
			logger.Debug(
				"webpush-address-send-completed",
				zap.String("address-id", address.ID),
				zap.String("address-hash", address.AddressHash),
			)
			continue
		}
		if isPermanentWebPushError(err) {
			logger.Warn(
				"webpush-address-permanent-failure",
				zap.String("address-id", address.ID),
				zap.String("address-hash", address.AddressHash),
				zap.Error(err),
			)
			if s.invalidAddressHandler != nil {
				if cleanupErr := s.invalidAddressHandler(ctx, address.AddressHash); cleanupErr != nil {
					logger.Error(
						"webpush-address-cleanup-failed",
						zap.String("address-id", address.ID),
						zap.String("address-hash", address.AddressHash),
						zap.Error(cleanupErr),
					)
					sendErrs = append(sendErrs, fmt.Errorf("cleanup failed for address %s: %w", address.AddressHash, cleanupErr))
					continue
				}
				report.Cleaned++
				logger.Info(
					"webpush-address-disabled-after-permanent-failure",
					zap.String("address-id", address.ID),
					zap.String("address-hash", address.AddressHash),
				)
			}
			continue
		}
		logger.Error(
			"webpush-address-transient-failure",
			zap.String("address-id", address.ID),
			zap.String("address-hash", address.AddressHash),
			zap.Error(err),
		)
		sendErrs = append(sendErrs, err)
	}

	if len(sendErrs) > 0 {
		joinedErr := errors.Join(sendErrs...)
		logger.Error(
			"webpush-send-failed",
			zap.Int("delivered", report.Delivered),
			zap.Int("cleaned", report.Cleaned),
			zap.Int("failed-addresses", len(sendErrs)),
			zap.Error(joinedErr),
		)
		return report, joinedErr
	}
	logger.Info("webpush-send-completed", zap.Int("delivered", report.Delivered), zap.Int("cleaned", report.Cleaned))
	return report, nil
}

// sendOne delivers the message to a single Web Push address.
func (s *WebPushSender) sendOne(ctx context.Context, subject, message string, address NotificationAddress, data map[string]interface{}) error {
	logger := logger.AcquireOperationFrom(ctx, "external/notifier", "send-one")
	logger.Debug("handling-send-one-request")

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
	if permanentWebPushRe.MatchString(err.Error()) {
		return true
	}
	if wrapped := errors.Unwrap(err); wrapped != nil {
		return isPermanentWebPushError(wrapped)
	}
	for _, wrapped := range unwrapJoinedErrors(err) {
		if isPermanentWebPushError(wrapped) {
			return true
		}
	}
	return false
}

func unwrapJoinedErrors(err error) []error {
	type joinedError interface {
		Unwrap() []error
	}

	if joined, ok := err.(joinedError); ok {
		return joined.Unwrap()
	}
	return nil
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
// token is determined to be permanently invalid.
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
//  2. Creates an FCM client authenticated with the configured
//     credentials (file or project ID).
//  3. Sends the notification directly via the Firebase Admin SDK.
//
// Note: this sender uses go-fcm's NewClient directly rather than
// nikoksr/notify's FCM service to ensure credentials are applied
// during client construction, not after.
func (s *FCMSender) Send(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) error {
	logger := logger.AcquireOperationFrom(ctx, "external/notifier", "send")
	logger.Debug("handling-send-request")

	_, err := s.SendWithReport(ctx, subject, message, addresses, data)
	return err
}

// SendWithReport delivers a push notification through Firebase Cloud
// Messaging and returns how many target tokens Firebase accepted.
//
// Firebase batch sends can partially fail without returning a top-level
// error. Returning a report prevents NotifyUser from saying "sent" when
// every token was rejected.
func (s *FCMSender) SendWithReport(ctx context.Context, subject, message string, addresses []NotificationAddress, data map[string]interface{}) (channelSendReport, error) {
	report := channelSendReport{}
	logger := logger.AcquirePackageFrom(ctx, "external/notifier").With(
		zap.String("operation", "fcm-send"),
		zap.String("channel", string(NotificationChannelFCM)),
	)

	if !s.Enabled() {
		logger.Warn(
			"fcm-sender-not-enabled",
			zap.Bool("enabled", s != nil && s.config.Enabled),
			zap.Bool("has-credentials-file", s != nil && s.config.CredentialsFile != ""),
			zap.Bool("has-project-id", s != nil && s.config.ProjectID != ""),
			zap.Int("attempted-addresses", len(addresses)),
			zap.Error(ErrNotificationSenderNotEnabled),
		)
		return report, ErrNotificationSenderNotEnabled
	}

	tokens := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address.Channel != NotificationChannelFCM || address.FCM == nil || address.FCM.Token == "" {
			continue
		}
		tokens = append(tokens, address.FCM.Token)
	}

	if len(tokens) == 0 {
		logger.Warn("fcm-send-no-valid-tokens", zap.Int("attempted-addresses", len(addresses)), zap.Error(ErrNotificationNoActiveAddresses))
		return report, ErrNotificationNoActiveAddresses
	}

	logger.Info("fcm-send-started", zap.Int("attempted-addresses", len(addresses)), zap.Int("valid-tokens", len(tokens)), zap.Strings("data-keys", notificationDataKeysForLog(data)))

	var opts []fcm.Option
	if s.config.CredentialsFile != "" {
		opts = append(opts, fcm.WithCredentialsFile(s.config.CredentialsFile))
	}
	if s.config.ProjectID != "" {
		opts = append(opts, fcm.WithProjectID(s.config.ProjectID))
	}

	fcmClient, err := fcm.NewClient(ctx, opts...)
	if err != nil {
		logger.Error(
			"fcm-client-initialisation-failed",
			zap.Bool("has-credentials-file", s.config.CredentialsFile != ""),
			zap.Bool("has-project-id", s.config.ProjectID != ""),
			zap.Error(err),
		)
		return report, err
	}

	var batchResponse *messaging.BatchResponse
	if len(tokens) == 1 {
		msg := &messaging.Message{
			Token: tokens[0],
			Notification: &messaging.Notification{
				Title: subject,
				Body:  message,
			},
		}
		batchResponse, err = fcmClient.Send(ctx, msg)
		if err != nil {
			logger.Error("fcm-single-send-failed", zap.Int("valid-tokens", len(tokens)), zap.Error(err))
			return report, err
		}
	} else {
		msg := &messaging.MulticastMessage{
			Tokens: tokens,
			Notification: &messaging.Notification{
				Title: subject,
				Body:  message,
			},
		}
		batchResponse, err = fcmClient.SendMulticast(ctx, msg)
		if err != nil {
			logger.Error("fcm-multicast-send-failed", zap.Int("valid-tokens", len(tokens)), zap.Error(err))
			return report, err
		}
	}

	if batchResponse == nil {
		logger.Error("fcm-send-nil-batch-response", zap.Int("valid-tokens", len(tokens)))
		return report, errors.New("fcm delivery failed: nil batch response")
	}

	report.Delivered = batchResponse.SuccessCount
	if batchResponse.FailureCount > 0 {
		err := fcmBatchResponseError(batchResponse)
		logger.Error(
			"fcm-send-partial-failure",
			zap.Int("valid-tokens", len(tokens)),
			zap.Int("delivered", report.Delivered),
			zap.Int("failed-tokens", batchResponse.FailureCount),
			zap.Error(err),
		)
		return report, err
	}

	logger.Info("fcm-send-completed", zap.Int("valid-tokens", len(tokens)), zap.Int("delivered", report.Delivered))
	return report, nil
}

func fcmBatchResponseError(response *messaging.BatchResponse) error {
	if response == nil || response.FailureCount == 0 {
		return nil
	}

	failures := make([]string, 0, response.FailureCount)
	for index, sendResponse := range response.Responses {
		if sendResponse == nil || sendResponse.Success || sendResponse.Error == nil {
			continue
		}
		failures = append(failures, fmt.Sprintf("token[%d]: %v", index, sendResponse.Error))
	}

	if len(failures) == 0 {
		return fmt.Errorf("fcm delivery failed for %d token(s)", response.FailureCount)
	}

	return fmt.Errorf("fcm delivery failed for %d token(s): %s", response.FailureCount, strings.Join(failures, "; "))
}
