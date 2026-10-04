package emailprovider

import (
	"context"

	"github.com/ooaklee/ghatd/external/logger"
)

// BirdEmailProvider implements EmailProvider for inline transactional sending.
// Success means Bird accepted the request, not that a recipient received it.
type BirdEmailProvider struct {
	// client owns immutable regional configuration and borrows its transport.
	client *BirdClient
}

var _ EmailProvider = (*BirdEmailProvider)(nil)

// NewBirdEmailProvider adapts a client constructed with NewBirdClient. A nil
// client is safe but unavailable; construction never verifies live credentials.
func NewBirdEmailProvider(client *BirdClient) *BirdEmailProvider {
	return &BirdEmailProvider{client: client}
}

// Name identifies the provider without exposing configuration or credentials.
func (p *BirdEmailProvider) Name() string { return "BIRD" }

// IsHealthy reports configuration readiness only. It performs no network probe
// and establishes neither sender verification nor permission to send mail.
func (p *BirdEmailProvider) IsHealthy(ctx context.Context) bool {
	return p != nil && p.client != nil && p.client.httpClient != nil && ctx != nil && ctx.Err() == nil
}

// Send submits once and returns an acceptance receipt or a sanitized failure.
// Logs contain fixed outcome events only, never addresses, bodies, message IDs,
// verification links, provider responses or private transport diagnostics.
func (p *BirdEmailProvider) Send(ctx context.Context, email *Email) (*SendResult, error) {
	result := &SendResult{Provider: "BIRD"}
	if p == nil || p.client == nil {
		result.Error = ErrEmailProviderUnavailable
		return result, result.Error
	}
	id, err := p.client.SendContext(ctx, email)
	result.MessageID, result.Error, result.Success = id, err, err == nil
	if ctx != nil {
		log := logger.AcquireOperationFrom(ctx, "external/emailprovider", "bird-send")
		if err != nil {
			log.Warn("bird-email-send-unconfirmed")
		} else {
			log.Info("bird-email-accepted")
		}
	}
	return result, err
}
