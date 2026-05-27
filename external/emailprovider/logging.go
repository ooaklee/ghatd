package emailprovider

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// LoggingEmailProviderConfig holds configuration for the logging email provider
type LoggingEmailProviderConfig struct {
	// DisableFullHtmlBodyPreview is retained for compatibility.
	// Email bodies are never written to logs.
	DisableFullHtmlBodyPreview bool

	// Store optionally supplies the local inbox store used to capture emails.
	Store *LocalEmailStore

	// MaxStoredEmails caps the default in-memory local inbox size.
	MaxStoredEmails int

	// TimeProvider optionally supplies message timestamps for tests.
	TimeProvider func() time.Time
}

// LoggingEmailProvider is an email provider that captures emails locally instead
// of sending them. This is useful for development and testing environments.
type LoggingEmailProvider struct {
	name         string
	store        *LocalEmailStore
	timeProvider func() time.Time
	counter      atomic.Uint64
}

// NewLoggingEmailProvider creates a new local email capture provider.
func NewLoggingEmailProvider(config *LoggingEmailProviderConfig) *LoggingEmailProvider {
	if config == nil {
		config = &LoggingEmailProviderConfig{}
	}

	store := config.Store
	if store == nil {
		store = NewLocalEmailStore(config.MaxStoredEmails)
	}

	timeProvider := config.TimeProvider
	if timeProvider == nil {
		timeProvider = time.Now
	}

	return &LoggingEmailProvider{
		name:         "LOCAL",
		store:        store,
		timeProvider: timeProvider,
	}
}

// Send captures an email locally instead of sending it to a remote provider.
func (p *LoggingEmailProvider) Send(ctx context.Context, email *Email) (*SendResult, error) {
	// Validate email
	if err := validateEmail(email); err != nil {
		return &SendResult{
			Provider: p.Name(),
			Success:  false,
			Error:    err,
		}, err
	}

	// Get logger from context
	logger := logger.AcquirePackageFrom(ctx, "external/emailprovider")

	messageID := p.nextMessageID()
	p.store.Add(LocalEmail{
		MessageID: messageID,
		To:        email.To,
		From:      email.From,
		ReplyTo:   email.ReplyTo,
		Subject:   email.Subject,
		HTMLBody:  email.HTMLBody,
		TextBody:  email.TextBody,
		CreatedAt: p.now(),
	})

	logFields := append(emailLogFields(p.Name(), email),
		zap.String("message-id", messageID),
		zap.Int("local-email-count", p.store.Count()),
	)

	logger.Info("email-outputted-locally--not-sent",
		logFields...,
	)

	return &SendResult{
		MessageID: messageID,
		Provider:  p.Name(),
		Success:   true,
		Error:     nil,
	}, nil
}

// Name returns the name of the provider
func (p *LoggingEmailProvider) Name() string {
	return p.name
}

// IsHealthy returns whether the provider is healthy
// The Logging email provider is always healthy
func (p *LoggingEmailProvider) IsHealthy(ctx context.Context) bool {
	return true
}

// Inbox returns the provider's captured local email store.
func (p *LoggingEmailProvider) Inbox() *LocalEmailStore {
	return p.store
}

// IsLocalOutputProvider identifies this provider as safe to call when an
// EmailManager is configured not to send real email.
func (p *LoggingEmailProvider) IsLocalOutputProvider() bool {
	return true
}

func (p *LoggingEmailProvider) now() time.Time {
	if p.timeProvider == nil {
		return time.Now().UTC()
	}
	return p.timeProvider().UTC()
}

func (p *LoggingEmailProvider) nextMessageID() string {
	return fmt.Sprintf("local-%d-%06d", p.now().UnixNano(), p.counter.Add(1))
}
