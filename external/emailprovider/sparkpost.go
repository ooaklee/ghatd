package emailprovider

import (
	"context"

	sp "github.com/SparkPost/gosparkpost"
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
	// Validate email
	if err := validateEmail(email); err != nil {
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
		return &SendResult{
			Provider: p.Name(),
			Success:  false,
			Error:    ErrEmailProviderSendFailed,
		}, ErrEmailProviderSendFailed
	}

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
	return p.client != nil
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
