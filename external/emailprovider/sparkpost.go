package emailprovider

import (
	"context"
	"strings"

	sp "github.com/SparkPost/gosparkpost"
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// SparkPostClient is the interface for SparkPost email client
type SparkPostClient interface {
	Send(t *sp.Transmission) (id string, res *sp.Response, err error)
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
	logger.Info("sparkpost-email-send-started", sparkPostEmailLogFields(p.Name(), email)...)

	// Validate email
	if err := validateEmail(email); err != nil {
		logger.Warn("sparkpost-email-validation-failed", append(sparkPostEmailLogFields(p.Name(), email), zap.Error(err))...)
		return &SendResult{
			Provider: p.Name(),
			Success:  false,
			Error:    err,
		}, err
	}

	// Create SparkPost transmission
	transmission := &sp.Transmission{
		Recipients: []string{email.To},
		Content: sp.Content{
			HTML:    email.HTMLBody,
			From:    email.From,
			ReplyTo: email.ReplyTo,
			Subject: email.Subject,
		},
	}

	// Send via SparkPost
	messageID, _, err := p.client.Send(transmission)
	if err != nil {
		logger.Error("sparkpost-email-send-failed", append(sparkPostEmailLogFields(p.Name(), email), zap.Error(err))...)
		return &SendResult{
			Provider: p.Name(),
			Success:  false,
			Error:    ErrEmailProviderSendFailed,
		}, ErrEmailProviderSendFailed
	}

	logger.Info("sparkpost-email-sent", append(sparkPostEmailLogFields(p.Name(), email), zap.String("message-id", messageID))...)
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

func sparkPostEmailLogFields(provider string, email *Email) []zap.Field {
	fields := []zap.Field{
		zap.String("provider", provider),
	}
	if email == nil {
		return append(fields, zap.Bool("email-present", false))
	}

	return append(fields,
		zap.Bool("email-present", true),
		zap.String("recipient-domain", emailDomainForLog(email.To)),
		zap.String("sender-domain", emailDomainForLog(email.From)),
		zap.Bool("has-reply-to", strings.TrimSpace(email.ReplyTo) != ""),
		zap.Bool("has-html-body", strings.TrimSpace(email.HTMLBody) != ""),
		zap.Bool("has-text-body", strings.TrimSpace(email.TextBody) != ""),
	)
}

func emailDomainForLog(value string) string {
	parts := strings.Split(strings.TrimSpace(value), "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}
