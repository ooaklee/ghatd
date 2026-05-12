package notifier

// StandardSendersRequest holds the configuration for creating standard
// notification channel senders (Web Push and FCM).
//
// The factory always produces a Web Push sender. An FCM sender is
// produced only when FCM is explicitly enabled.
type StandardSendersRequest struct {
	// WebPush configures the Web Push sender. May be nil; a sender is
	// still created but will be disabled if neither explicitly enabled
	// nor configured with both VAPID keys.
	WebPush *WebPushSenderConfig

	// FCM configures the Firebase Cloud Messaging sender.
	// May be nil; no FCM sender is created and cleanup is a no-op.
	FCM *FCMSenderConfig

	// FCMCredentialsBase64 takes precedence over FCM.CredentialsFile.
	// When set, the base64-encoded JSON is decoded and written to a
	// temporary file whose lifecycle is managed by the cleanup function
	// returned from NewStandardSenders.
	FCMCredentialsBase64 string
}

// StandardSendersResult contains the senders created by NewStandardSenders
// and a cleanup function for any temporary files the factory created.
//
// The caller MUST invoke Cleanup when the senders are no longer needed
// (typically at server shutdown) to remove any temporary credential files
// from disk.
type StandardSendersResult struct {
	Senders []ChannelSender
	Cleanup func()
}

// NewStandardSenders creates the standard set of channel senders.
//
// Web Push sender (always present):
//   - Enabled is set to true when the config explicitly enables it OR
//     when both VAPIDPublicKey and VAPIDPrivateKey are non-empty.
//
// FCM sender (only when explicitly enabled):
//   - Included only when req.FCM is non-nil and req.FCM.Enabled is true.
//   - If req.FCMCredentialsBase64 is set, it takes precedence over
//     req.FCM.CredentialsFile. The base64 value is decoded and written
//     to a temporary file, and the returned cleanup function removes it.
//   - Invalid base64 input returns an error.
//   - When FCM is not configured (nil or not enabled) the result's
//     Cleanup function is a no-op.
func NewStandardSenders(req *StandardSendersRequest) (*StandardSendersResult, error) {
	webPushConfig := resolveWebPushConfig(req)
	senders := []ChannelSender{
		NewWebPushSender(*webPushConfig),
	}

	if req == nil || req.FCM == nil || !req.FCM.Enabled {
		return &StandardSendersResult{
			Senders: senders,
			Cleanup: func() {},
		}, nil
	}

	credsPath, cleanup, err := ResolveCredentialsFileWithCleanup(req.FCMCredentialsBase64, req.FCM.CredentialsFile)
	if err != nil {
		return nil, err
	}

	fcmConfig := *req.FCM
	fcmConfig.CredentialsFile = credsPath
	senders = append(senders, NewFCMSender(fcmConfig))

	return &StandardSendersResult{
		Senders: senders,
		Cleanup: cleanup,
	}, nil
}

// resolveWebPushConfig returns the effective WebPushSenderConfig.
//
// When WebPush is nil, a disabled (but present) config is returned.
// Enabled is automatically set to true when explicitly enabled OR
// when both VAPID keys are non-empty, so callers do not need to set
// both Enabled and keys.
func resolveWebPushConfig(req *StandardSendersRequest) *WebPushSenderConfig {
	if req == nil || req.WebPush == nil {
		return &WebPushSenderConfig{Enabled: false}
	}
	cfg := *req.WebPush
	cfg.Enabled = cfg.Enabled || (cfg.VAPIDPublicKey != "" && cfg.VAPIDPrivateKey != "")
	return &cfg
}
