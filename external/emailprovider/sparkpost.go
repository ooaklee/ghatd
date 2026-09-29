package emailprovider

import (
	"context"

	sp "github.com/SparkPost/gosparkpost"
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// SparkPostClient is the interface for SparkPost email client
type SparkPostClient interface {
	Send(t *sp.Transmission) (id string, res *sp.Response, err error)
}

// sparkPostContextClient is the optional context-aware interface used when the
// underlying client supports it. *gosparkpost.Client implements it, so the
// provider can propagate cancellation, deadlines and tracing parents into the
// outbound HTTP request.
type sparkPostContextClient interface {
	SendContext(ctx context.Context, t *sp.Transmission) (id string, res *sp.Response, err error)
}

// SparkPostEmailProvider implements an email provider for SparkPost
type SparkPostEmailProvider struct {
	client SparkPostClient
	name   string
}

// NewSparkPostEmailProvider creates a new SparkPost email provider
func NewSparkPostEmailProvider(client SparkPostClient) *SparkPostEmailProvider {
	return &SparkPostEmailProvider{
		client: client,
		name:   "SPARKPOST",
	}
}

// Send handles sending an email via SparkPost
func (p *SparkPostEmailProvider) Send(ctx context.Context, email *Email) (*SendResult, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/emailprovider", "sparkpost-send")
	logger.Info("sparkpost-email-send-started", emailLogFields(p.Name(), email)...)

	// Validate email
	if err := validateEmail(email); err != nil {
		logger.Warn("sparkpost-email-validation-failed", append(emailLogFields(p.Name(), email), zap.Error(err))...)
		return &SendResult{
			Provider: p.Name(),
			Success:  false,
			Error:    err,
		}, err
	}

	// Create SparkPost transmission
	//
	// NOTE (known behaviour, preserved for compatibility): validation accepts a
	// text-only email (TextBody without HTMLBody), but the transmission
	// currently sets only the HTML part, so a text-only email may be sent
	// without any body content. Fixing this would change behaviour for existing
	// callers and is therefore left to a separate, deliberate change.
	transmission := &sp.Transmission{
		Recipients: []string{email.To},
		Content: sp.Content{
			HTML:    email.HTMLBody,
			From:    email.From,
			ReplyTo: email.ReplyTo,
			Subject: email.Subject,
		},
	}

	// Send via SparkPost. Prefer the context-aware API when the client
	// supports it so cancellation, deadlines and tracing parents propagate;
	// otherwise fall back to the legacy Send for existing mocks and custom
	// implementations. The per-call ctx is never stored on the provider.
	var messageID string
	var err error
	if contextClient, ok := p.client.(sparkPostContextClient); ok {
		messageID, _, err = contextClient.SendContext(ctx, transmission)
	} else {
		messageID, _, err = p.client.Send(transmission)
	}
	if err != nil {
		// SDK error bodies may echo message content or credentials. Keep the
		// stable public failure classification in both logs and the return value.
		logger.Error("sparkpost-email-send-failed", append(emailLogFields(p.Name(), email), zap.Error(ErrEmailProviderSendFailed))...)
		return &SendResult{
			Provider: p.Name(),
			Success:  false,
			Error:    ErrEmailProviderSendFailed,
		}, ErrEmailProviderSendFailed
	}

	logger.Info("sparkpost-email-sent", append(emailLogFields(p.Name(), email), zap.String("message-id", messageID))...)
	return &SendResult{
		MessageID: messageID,
		Provider:  p.Name(),
		Success:   true,
		Error:     nil,
	}, nil
}

// Name returns the name of the provider
func (p *SparkPostEmailProvider) Name() string {
	return p.name
}

// IsHealthy handles health checks for the provider.
// For SparkPost, we assume it's healthy if the client is initialised
func (p *SparkPostEmailProvider) IsHealthy(ctx context.Context) bool {
	logger := logger.AcquireOperationFrom(ctx, "external/emailprovider", "sparkpost-health")
	healthy := p.client != nil
	logger.Debug("sparkpost-health-checked", zap.String("provider", p.Name()), zap.Bool("healthy", healthy))
	return healthy
}

// validateEmail validates that an email has all required fields
func validateEmail(email *Email) error {
	if email.To == "" {
		return ErrEmailProviderMissingRecipient
	}
	if email.From == "" {
		return ErrEmailProviderMissingFrom
	}
	if email.Subject == "" {
		return ErrEmailProviderMissingSubject
	}
	if email.HTMLBody == "" && email.TextBody == "" {
		return ErrEmailProviderMissingBody
	}
	return nil
}
