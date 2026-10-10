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

// purposeProvider is a synthetic provider view of an EmailManager bound to one
// trusted mail purpose, used by routed-mode consumers.
type purposeProvider struct {
	manager *EmailManager
	purpose emailprovider.MailType
}

// Name returns the constant provider name ROUTED for the purpose-bound manager
// view.
func (*purposeProvider) Name() string { return "ROUTED" }

// IsHealthy reports whether the underlying selection for the bound purpose is
// currently usable. When a local router output exists its health decides;
// otherwise the selected provider is checked, and missing wiring, an expired
// context or an invalid purpose report unhealthy.
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

// IsLocalOutputProvider reports whether sends through the bound manager avoid
// external delivery, either because the router is configured with a local
// capture provider or the single configured provider is local.
func (p *purposeProvider) IsLocalOutputProvider() bool {
	return p.manager != nil && ((p.manager.router != nil && p.manager.router.local != nil) || isLocalOutputProvider(p.manager.provider))
}

// Send copies the email, forces MailType to the adapter's bound purpose, and
// forwards to the manager for routing. A nil manager or email fails immediately
// with ErrEmailMailerSendFailed; the result carries the routed receipt's state,
// provider and message ID, with Success set only for Accepted or Captured
// states.
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
