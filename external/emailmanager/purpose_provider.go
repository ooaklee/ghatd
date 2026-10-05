package emailmanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/emailprovider"
)

// ProviderForMailType adapts legacy provider consumers to trusted manager routing.
// The bound purpose is configuration, never taken from the caller's Email fields.
func (m *EmailManager) ProviderForMailType(purpose emailprovider.MailType) emailprovider.EmailProvider {
	return &purposeProvider{manager: m, purpose: purpose}
}

type purposeProvider struct {
	manager *EmailManager
	purpose emailprovider.MailType
}

func (*purposeProvider) Name() string { return "ROUTED" }
func (p *purposeProvider) IsHealthy(ctx context.Context) bool {
	if p.manager == nil || ctx == nil || ctx.Err() != nil || !p.purpose.Valid() {
		return false
	}
	if p.manager.router == nil {
		return !nilInterface(p.manager.provider) && p.manager.provider.IsHealthy(ctx)
	}
	selected, err := p.manager.router.providerFor(p.purpose, false, false)
	if err != nil {
		return false
	}
	if p.manager.router.local != nil {
		return p.manager.router.local.IsHealthy(ctx)
	}
	return selected.Provider.IsHealthy(ctx)
}
func (p *purposeProvider) IsLocalOutputProvider() bool {
	return p.manager != nil && ((p.manager.router != nil && p.manager.router.local != nil) || isLocalOutputProvider(p.manager.provider))
}
func (p *purposeProvider) Send(ctx context.Context, email *emailprovider.Email) (*emailprovider.SendResult, error) {
	if p.manager == nil || email == nil {
		return &emailprovider.SendResult{State: emailprovider.Failed}, ErrEmailMailerSendFailed
	}
	copy := *email
	copy.MailType = p.purpose
	receipt, err := p.manager.sendResult(ctx, &copy, &EmailInfo{To: copy.To, From: copy.From, Subject: copy.Subject})
	result := &emailprovider.SendResult{Error: err}
	if receipt != nil {
		result.State = receipt.State
		result.Provider = receipt.Provider
		result.MessageID = receipt.MessageID
		result.Success = receipt.State == emailprovider.Accepted || receipt.State == emailprovider.Captured
	}
	return result, err
}
