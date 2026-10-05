package emailprovider

import "context"

// MailType is a trusted application purpose, never inferred from message content.
type MailType string

const (
	// Transactional covers operational and security messages.
	Transactional MailType = "transactional"
	// Marketing covers consented promotional single-message delivery.
	Marketing MailType = "marketing"
)

// Valid reports whether the purpose belongs to the supported routing vocabulary.
func (t MailType) Valid() bool { return t == Transactional || t == Marketing }

// MailTypeProvider separates sending capability from an ordered routing preference.
// Preferences never grant capability. Empty preferences participate in round-robin.
type MailTypeProvider interface {
	SupportedMailTypes() []MailType
	MailTypePreference() []MailType
}

// PreferredProvider decorates a provider without mutating it or its shared client.
// Configure before registration; returned slices are independent copies.
type PreferredProvider struct {
	EmailProvider
	preferences []MailType
}

// WithMailTypePreference returns a new decoration with defensively copied preferences.
// Invalid/duplicate/unsupported values are rejected by the routed manager constructor.
func WithMailTypePreference(p EmailProvider, types []MailType) *PreferredProvider {
	return &PreferredProvider{EmailProvider: p, preferences: append([]MailType(nil), types...)}
}

// WithMailTypePreference replaces the decoration while retaining the same underlying provider.
func (p *PreferredProvider) WithMailTypePreference(types []MailType) *PreferredProvider {
	return WithMailTypePreference(p.EmailProvider, types)
}

// MailTypePreference returns a copy of the configured ranking.
func (p *PreferredProvider) MailTypePreference() []MailType {
	return append([]MailType(nil), p.preferences...)
}

// SupportedMailTypes delegates capability; an undescribed legacy provider is transactional-only.
func (p *PreferredProvider) SupportedMailTypes() []MailType {
	if typed, ok := p.EmailProvider.(MailTypeProvider); ok {
		return typed.SupportedMailTypes()
	}
	// Legacy providers support transactional delivery only unless explicitly described.
	return []MailType{Transactional}
}

// IsLocalOutputProvider preserves local interception when a logging provider is decorated.
func (p *PreferredProvider) IsLocalOutputProvider() bool {
	local, ok := p.EmailProvider.(interface{ IsLocalOutputProvider() bool })
	return ok && local.IsLocalOutputProvider()
}

// SupportedMailTypes declares the inline purposes supported by Bird.
func (p *BirdEmailProvider) SupportedMailTypes() []MailType { return []MailType{Transactional} }

// MailTypePreference leaves routing unranked until explicitly configured.
func (p *BirdEmailProvider) MailTypePreference() []MailType { return nil }

// WithMailTypePreference decorates this instance with an ordered routing preference.
func (p *BirdEmailProvider) WithMailTypePreference(types []MailType) *PreferredProvider {
	return WithMailTypePreference(p, types)
}

// SupportedMailTypes declares the inline purposes supported by SparkPost.
func (p *SparkPostEmailProvider) SupportedMailTypes() []MailType {
	return []MailType{Transactional, Marketing}
}

// MailTypePreference leaves routing unranked until explicitly configured.
func (p *SparkPostEmailProvider) MailTypePreference() []MailType { return nil }

// WithMailTypePreference decorates this instance with an ordered routing preference.
func (p *SparkPostEmailProvider) WithMailTypePreference(types []MailType) *PreferredProvider {
	return WithMailTypePreference(p, types)
}

// SupportedMailTypes declares the inline purposes supported by Logging.
func (p *LoggingEmailProvider) SupportedMailTypes() []MailType {
	return []MailType{Transactional, Marketing}
}

// MailTypePreference leaves routing unranked until explicitly configured.
func (p *LoggingEmailProvider) MailTypePreference() []MailType { return nil }

// WithMailTypePreference decorates this instance with an ordered routing preference.
func (p *LoggingEmailProvider) WithMailTypePreference(types []MailType) *PreferredProvider {
	return WithMailTypePreference(p, types)
}

// SendState records submission evidence; no value asserts recipient delivery.
type SendState string

const (
	Accepted  SendState = "accepted"
	Captured  SendState = "captured"
	Skipped   SendState = "skipped"
	Failed    SendState = "failed"
	Uncertain SendState = "uncertain"
)

// CampaignRequest describes a provider-neutral audience campaign submission.
// It is separate from Email: audience eligibility/consent belongs to the caller.
type CampaignRequest struct {
	Name       string
	AudienceID string
	From       string
	ReplyTo    string
	Subject    string
	HTMLBody   string
	TextBody   string
}

// CampaignProvider is an optional capability for a vendor audience operation.
// Inline email adapters do not implement this contract merely by supporting Marketing.
type CampaignProvider interface {
	SubmitCampaign(context.Context, *CampaignRequest) (*SendResult, error)
}
