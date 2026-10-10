package emailmanager

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sync"

	"github.com/ooaklee/ghatd/external/emailprovider"
)

var (
	// ErrRoutingInvalid means trusted startup configuration is inconsistent.
	ErrRoutingInvalid = errors.New("emailmanager/routing-invalid")
	// ErrPurposeRequired means a routed generic send omitted or supplied an unknown purpose.
	ErrPurposeRequired = errors.New("emailmanager/purpose-required")
	// ErrCapabilityUnavailable means no registered route supports this operation and purpose.
	ErrCapabilityUnavailable = errors.New("emailmanager/capability-unavailable")
)

// ProviderRegistration identifies a configured account independently of vendor Name().
type ProviderRegistration struct {
	ID       string
	Provider emailprovider.EmailProvider
	// Campaign is an optional separate audience-operation port. Inline capability is insufficient.
	Campaign emailprovider.CampaignProvider
}

// RoutingConfig is immutable startup policy. LocalCapture overrides external I/O,
// while preserving the selected instance's identity in one shared inbox.
type RoutingConfig struct {
	Providers []ProviderRegistration
	Routes    map[emailprovider.MailType]string
	// DefaultProviderID is a transactional-only default when no explicit route or preference wins.
	DefaultProviderID string
	LocalCapture      *emailprovider.LoggingEmailProvider
}

// routingTurn keys rotation counters by mail purpose and whether a campaign-
// capable provider is required.
type routingTurn struct {
	purpose  emailprovider.MailType
	campaign bool
}

// registeredProvider pairs a registration identity with copies of the
// provider's supported mail types and its preference ranking, taken at router
// construction.
type registeredProvider struct {
	ProviderRegistration
	supported, preferences []emailprovider.MailType
}

// providerRouter holds the validated provider set, per-purpose explicit routes,
// a default provider ID and optional local capture. The mutex guards bounded
// rotation turn counters keyed by routingTurn.
type providerRouter struct {
	providers []registeredProvider
	routes    map[emailprovider.MailType]string
	defaultID string
	local     *emailprovider.LoggingEmailProvider
	mu        sync.Mutex
	turns     map[routingTurn]uint64
}

var providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// nilInterface reports whether an interface value is nil or wraps a nil
// pointer, map, slice, interface or func, catching typed-nil providers.
func nilInterface(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func:
		return r.IsNil()
	}
	return false
}

// invalidProvider reports whether a provider chain terminates in nil or cycles
// through repeated PreferredProvider wrappers.
func invalidProvider(p emailprovider.EmailProvider) bool {
	seen := map[*emailprovider.PreferredProvider]bool{}
	for !nilInterface(p) {
		wrapper, ok := p.(*emailprovider.PreferredProvider)
		if !ok {
			return false
		}
		if seen[wrapper] {
			return true
		}
		seen[wrapper] = true
		p = wrapper.EmailProvider
	}
	return true
}

// contains reports whether the mail type appears in the slice.
func contains(types []emailprovider.MailType, t emailprovider.MailType) bool {
	for _, x := range types {
		if x == t {
			return true
		}
	}
	return false
}

// validTypes reports whether every mail type is valid and free of duplicates.
func validTypes(types []emailprovider.MailType) bool {
	seen := map[emailprovider.MailType]bool{}
	for _, t := range types {
		if !t.Valid() || seen[t] {
			return false
		}
		seen[t] = true
	}
	return true
}

// newProviderRouter validates routing configuration and returns
// ErrRoutingInvalid for missing providers, malformed or duplicate IDs,
// nil/cyclic providers, empty or duplicate mail type lists, preferences outside
// supported types, unknown or unsupported route targets, and default providers
// that do not support transactional mail. Supported and preference slices are
// copied at construction.
func newProviderRouter(cfg *RoutingConfig) (*providerRouter, error) {
	if cfg == nil || len(cfg.Providers) == 0 {
		return nil, ErrRoutingInvalid
	}
	r := &providerRouter{routes: map[emailprovider.MailType]string{}, defaultID: cfg.DefaultProviderID, local: cfg.LocalCapture, turns: map[routingTurn]uint64{}}
	ids := map[string]registeredProvider{}
	for _, p := range cfg.Providers {
		if !providerIDPattern.MatchString(p.ID) || invalidProvider(p.Provider) {
			return nil, ErrRoutingInvalid
		}
		if _, ok := ids[p.ID]; ok {
			return nil, ErrRoutingInvalid
		}
		supported := []emailprovider.MailType{emailprovider.Transactional}
		var preferences []emailprovider.MailType
		if typed, ok := p.Provider.(emailprovider.MailTypeProvider); ok {
			supported = typed.SupportedMailTypes()
			preferences = typed.MailTypePreference()
		}
		if len(supported) == 0 || !validTypes(supported) || !validTypes(preferences) {
			return nil, ErrRoutingInvalid
		}
		for _, t := range preferences {
			if !contains(supported, t) {
				return nil, ErrRoutingInvalid
			}
		}
		next := registeredProvider{ProviderRegistration: p, supported: append([]emailprovider.MailType(nil), supported...), preferences: append([]emailprovider.MailType(nil), preferences...)}
		ids[p.ID] = next
		r.providers = append(r.providers, next)
	}
	for purpose, id := range cfg.Routes {
		p, ok := ids[id]
		if !purpose.Valid() || !ok || !contains(p.supported, purpose) {
			return nil, ErrRoutingInvalid
		}
		r.routes[purpose] = id
	}
	if cfg.DefaultProviderID != "" {
		p, ok := ids[cfg.DefaultProviderID]
		if !ok || !contains(p.supported, emailprovider.Transactional) {
			return nil, ErrRoutingInvalid
		}
	}
	return r, nil
}

// selectProvider chooses a provider for the purpose, consuming a rotation turn
// in the process.
func (r *providerRouter) selectProvider(purpose emailprovider.MailType, campaign bool) (registeredProvider, error) {
	return r.providerFor(purpose, campaign, true)
}

// providerFor can inspect readiness without consuming a selection turn.
func (r *providerRouter) providerFor(purpose emailprovider.MailType, campaign, advance bool) (registeredProvider, error) {
	if !purpose.Valid() {
		return registeredProvider{}, ErrPurposeRequired
	}
	candidates := []registeredProvider{}
	best := int(^uint(0) >> 1)
	for _, p := range r.providers {
		if !contains(p.supported, purpose) || (campaign && nilInterface(p.Campaign)) {
			continue
		}
		if id, ok := r.routes[purpose]; ok {
			if p.ID == id {
				return p, nil
			}
			continue
		}
		rank := 1000
		for index, t := range p.preferences {
			if t == purpose {
				rank = index
				break
			}
		}
		if rank < best {
			best = rank
			candidates = nil
		}
		if rank == best {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return registeredProvider{}, ErrCapabilityUnavailable
	}
	if best == 1000 && purpose == emailprovider.Transactional && r.defaultID != "" {
		for _, p := range candidates {
			if p.ID == r.defaultID {
				return p, nil
			}
		}
	}
	r.mu.Lock()
	key := routingTurn{purpose: purpose, campaign: campaign}
	turn := r.turns[key]
	if advance {
		r.turns[key]++
	}
	r.mu.Unlock()
	return candidates[turn%uint64(len(candidates))], nil
}

// SendReceipt preserves submission evidence. Accepted never means delivered.
type SendReceipt struct {
	State      emailprovider.SendState `json:"state"`
	MailType   emailprovider.MailType  `json:"mailType"`
	ProviderID string                  `json:"providerId"`
	Provider   string                  `json:"provider"`
	MessageID  string                  `json:"messageId,omitempty"`
}

// sendResult routes and submits one email, copying it before mutation. It
// requires a valid purpose when a router exists, skips (state Skipped) non-
// local providers when sending is disabled, distinguishes Failed from Uncertain
// provider outcomes, marks local captures as Captured, and defers an audit log
// entry for every attempt.
func (m *EmailManager) sendResult(ctx context.Context, email *emailprovider.Email, info *EmailInfo) (receipt *SendReceipt, err error) {
	if ctx == nil || ctx.Err() != nil || email == nil {
		return &SendReceipt{State: emailprovider.Failed}, ErrEmailMailerSendFailed
	}
	defer func() { m.auditReceipt(ctx, info, receipt) }()
	copy := *email
	email = &copy
	purpose := email.MailType
	if purpose == "" {
		if m.router != nil {
			return &SendReceipt{State: emailprovider.Failed}, ErrPurposeRequired
		}
		purpose = emailprovider.Transactional
	}
	if !purpose.Valid() {
		return &SendReceipt{State: emailprovider.Failed}, ErrPurposeRequired
	}
	email.MailType = purpose
	provider := m.provider
	id := ""
	if m.router != nil {
		selected, err := m.router.selectProvider(purpose, false)
		if err != nil {
			return &SendReceipt{State: emailprovider.Failed, MailType: purpose}, err
		}
		provider = selected.Provider
		id = selected.ID
	}
	if invalidProvider(provider) {
		return &SendReceipt{State: emailprovider.Failed, MailType: purpose}, ErrEmailMailerProviderUnavailable
	}
	receipt = &SendReceipt{State: emailprovider.Failed, MailType: purpose, ProviderID: id, Provider: provider.Name()}
	email.ProviderID = id
	email.ProviderName = provider.Name()
	if m.router != nil && m.router.local != nil {
		provider = m.router.local
	}
	local := isLocalOutputProvider(provider)
	if !m.config.ShouldSendEmail && !local {
		receipt.State = emailprovider.Skipped
		return receipt, nil
	}
	if !provider.IsHealthy(ctx) {
		return receipt, ErrEmailMailerProviderUnavailable
	}
	result, err := provider.Send(ctx, email)
	if err != nil {
		receipt.State = emailprovider.Uncertain
		if result != nil && result.State == emailprovider.Failed {
			receipt.State = emailprovider.Failed
		}
		return receipt, ErrEmailMailerSendFailed
	}
	if result == nil || !result.Success || result.Error != nil || (result.State != "" && result.State != emailprovider.Accepted && !(local && result.State == emailprovider.Captured)) {
		receipt.State = emailprovider.Uncertain
		if result != nil && result.State == emailprovider.Failed {
			receipt.State = emailprovider.Failed
		}
		return receipt, ErrEmailMailerSendFailed
	}
	receipt.MessageID = result.MessageID
	if local {
		receipt.State = emailprovider.Captured
	} else {
		receipt.State = emailprovider.Accepted
	}
	return receipt, nil
}

// auditReceipt enriches a copy of the send info with receipt outcome and logs
// it when audit logging is enabled and a non-nil audit service is wired.
func (m *EmailManager) auditReceipt(ctx context.Context, info *EmailInfo, receipt *SendReceipt) {
	if info != nil && receipt != nil && m.config.EnableAuditLogging && !nilInterface(m.auditService) {
		infoCopy := *info
		infoCopy.EmailProvider = receipt.Provider
		infoCopy.MailType = string(receipt.MailType)
		infoCopy.ProviderID = receipt.ProviderID
		infoCopy.State = string(receipt.State)
		m.logAuditEvent(ctx, &infoCopy)
	}
}

// SubmitCampaign selects a marketing-capable audience-operation port.
// An explicit route lacking that port fails closed rather than selecting another account.
// Local mode skips this operation: it never creates/enrolls a remote audience.
func (m *EmailManager) SubmitCampaign(ctx context.Context, req *emailprovider.CampaignRequest) (*SendReceipt, error) {
	receipt := &SendReceipt{State: emailprovider.Failed, MailType: emailprovider.Marketing}
	if ctx == nil || ctx.Err() != nil || req == nil || req.Name == "" || req.AudienceID == "" {
		return receipt, ErrEmailMailerSendFailed
	}
	defer func() { m.auditReceipt(ctx, &EmailInfo{From: req.From, Subject: req.Subject}, receipt) }()
	if m.router == nil {
		return receipt, ErrCapabilityUnavailable
	}
	p, err := m.router.selectProvider(emailprovider.Marketing, true)
	if err != nil {
		return receipt, err
	}
	receipt.ProviderID = p.ID
	receipt.Provider = p.Provider.Name()
	if nilInterface(p.Campaign) {
		return receipt, ErrCapabilityUnavailable
	}
	if !m.config.ShouldSendEmail || m.router.local != nil || isLocalOutputProvider(p.Provider) {
		receipt.State = emailprovider.Skipped
		return receipt, nil
	}
	copy := *req
	result, err := p.Campaign.SubmitCampaign(ctx, &copy)
	if err != nil || result == nil || !result.Success || result.Error != nil || (result.State != "" && result.State != emailprovider.Accepted) {
		receipt.State = emailprovider.Uncertain
		if result != nil && result.State == emailprovider.Failed {
			receipt.State = emailprovider.Failed
		}
		return receipt, ErrEmailMailerSendFailed
	}
	receipt.State = emailprovider.Accepted
	receipt.MessageID = result.MessageID
	return receipt, nil
}
