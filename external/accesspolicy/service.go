package accesspolicy

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	// ErrDenied covers missing, expired, disabled or insufficient current grants.
	ErrDenied = errors.New("accesspolicy/access-denied")
	// ErrConfiguration indicates absent dependencies or invalid policy definitions.
	ErrConfiguration = errors.New("accesspolicy/invalid-configuration")
	// ErrConflict indicates stale policy revision or a changed replay fingerprint.
	ErrConflict = errors.New("accesspolicy/conflict")
	// ErrLimitReached indicates the current fixed-window budget is exhausted.
	ErrLimitReached = errors.New("accesspolicy/usage-limit-reached")
)

// Store owns persistence and transactionality. Implementations must preserve
// the same grant-revocation fence across usage and token-creation callbacks.
type Store interface {
	// Read returns an independent stored snapshot, or ErrDenied only for known
	// absence. Single-cause wrappers are supported; joined errors and Is-only
	// aliases do not establish absence for management/migration operations.
	Read(context.Context, Subject) (Grant, error)
	// Replace atomically checks revision, writes the policy and records its actor.
	Replace(context.Context, Grant, int64, string, time.Time) (Grant, error)
	// WithGrant serializes an enabled policy and the callback in one transaction.
	// The callback may retry; it must not perform network I/O or external effects.
	WithGrant(context.Context, Subject, time.Time, func(context.Context, Grant) error) error
	// Consume atomically checks live policy, quota, fingerprint and idempotency.
	Consume(context.Context, Consumption, time.Time) (Usage, error)
	// WithConsumption also commits trusted callback writes with a new admission.
	// It checks current resource authority on replay but does not repeat effects.
	WithConsumption(context.Context, Consumption, time.Time, ConsumptionAction) (Usage, error)
}

// ManagementAuthorizer rechecks current administrative authority for one system
// and returns an audit actor ID. Never implement it by trusting a body actor or
// merely checking a signed admin claim. Nil disables policy management.
// Return ErrDenied for an authoritative denial; dependency/context errors are
// preserved for operators and must be sanitized by any transport boundary.
type ManagementAuthorizer func(context.Context, string) (string, error)

// Service provides explicit, default-deny policy operations to trusted callers.
// HTTP authentication and resource ownership stay with the host's boundary.
type Service struct {
	// store owns grant, audit, counter and receipt persistence.
	store Store
	// manage is the host's live administrative permission checker.
	manage ManagementAuthorizer
}

// NewService constructs a policy service; management may deliberately be nil
// for a read/enforce-only application process. No grants are seeded implicitly.
func NewService(store Store, manage ManagementAuthorizer) (*Service, error) {
	if store == nil {
		return nil, ErrConfiguration
	}
	return &Service{store: store, manage: manage}, nil
}

// Resolve reads current policy without caching or filling missing grants from
// roles. Signed identity/type metadata is never used to synthesize permissions.
func (s *Service) Resolve(ctx context.Context, subject Subject) (Grant, error) {
	if err := s.checkContext(ctx); err != nil {
		return Grant{}, err
	}
	if !validSubject(subject) {
		return Grant{}, ErrDenied
	}
	grant, err := s.store.Read(ctx, subject)
	if err != nil {
		return Grant{}, err
	}
	if err := ctx.Err(); err != nil {
		return Grant{}, err
	}
	if err := usableGrant(grant, subject, time.Now()); err != nil {
		return Grant{}, err
	}
	return grant, nil
}

// Authorize requires all named scopes and permissions from the same subject.
// A user grant does not implicitly augment an API token's delegated grant.
func (s *Service) Authorize(ctx context.Context, subject Subject, scopes, permissions []string) error {
	grant, err := s.Resolve(ctx, subject)
	if err != nil {
		return err
	}
	if !includes(grant.Scopes, scopes) || !includes(grant.Permissions, permissions) {
		return ErrDenied
	}
	return nil
}

// ReplaceGrant performs an explicitly authorized compare-and-swap. Expected
// zero creates only a missing grant; updates require its current positive revision.
// The store writes the audit event in the same transaction as the policy.
func (s *Service) ReplaceGrant(ctx context.Context, grant Grant, expected int64) (Grant, error) {
	if expected < 0 || expected >= 9007199254740991 || validateGrant(grant) != nil {
		return Grant{}, ErrConfiguration
	}
	actor, err := s.authorizeManagement(ctx, grant.Subject.System)
	if err != nil {
		return Grant{}, err
	}
	grant.Revision = expected + 1
	return s.store.Replace(ctx, grant, expected, actor, time.Now().UTC())
}

// authorizeManagement checks the host's live policy authority for both review
// and write operations. Operational failures remain distinguishable from a
// deliberate denial without exposing them through an HTTP response here.
func (s *Service) authorizeManagement(ctx context.Context, system string) (string, error) {
	if !validName(system) {
		return "", ErrConfiguration
	}
	if err := s.checkContext(ctx); err != nil {
		return "", err
	}
	if s.manage == nil {
		return "", ErrDenied
	}
	actor, err := s.manage(ctx, system)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validName(actor) {
		return "", ErrDenied
	}
	return actor, nil
}

// Consume admits one operation against a stored metric. The key and fingerprint
// must be bound by the caller to the operation/resource/normalized payload, not
// blindly copied from a client header. Replays recheck current grant eligibility.
// For business-unit quotas invoke at the domain transaction boundary; for HTTP
// request quotas use a fresh server-generated key for each admitted request.
func (s *Service) Consume(ctx context.Context, subject Subject, metric, key, fingerprint string) (Usage, error) {
	return s.ConsumeAuthorized(ctx, Consumption{Subject: subject, Metric: metric, Key: key, Fingerprint: fingerprint})
}

// ConsumeAuthorized atomically rechecks the required grants with admission.
// This closes the gap between an earlier route check and a concurrent policy
// edit. Live resource checks still belong to the domain's command transaction.
func (s *Service) ConsumeAuthorized(ctx context.Context, request Consumption) (Usage, error) {
	if err := s.checkContext(ctx); err != nil {
		return Usage{}, err
	}
	if err := validateConsumption(request); err != nil {
		return Usage{}, err
	}
	request.Scopes = append([]string(nil), request.Scopes...)
	request.Permissions = append([]string(nil), request.Permissions...)
	return s.store.Consume(ctx, request, time.Now().UTC())
}

// WithConsumption combines a business operation and its quota admission in the
// store's transaction. Check runs even on replay; Apply runs only for a new
// admission. Retries may rerun aborted callbacks. They must use the transaction
// context/shared client, have no external effects, and persist any business
// receipt in that transaction.
// An error does not prove an earlier attempt never committed: after uncertain
// outcomes retry the same key/fingerprint, never invent a new operation key.
func (s *Service) WithConsumption(ctx context.Context, request Consumption, action ConsumptionAction) (Usage, error) {
	if err := s.checkContext(ctx); err != nil {
		return Usage{}, err
	}
	if validateConsumption(request) != nil || action.Check == nil || action.Apply == nil {
		return Usage{}, ErrConfiguration
	}
	request.Scopes = append([]string(nil), request.Scopes...)
	request.Permissions = append([]string(nil), request.Permissions...)
	return s.store.WithConsumption(ctx, request, time.Now().UTC(), action)
}

// validateConsumption applies the same bounded contract at service and store
// boundaries, including callers that use a Store directly within trusted code.
func validateConsumption(request Consumption) error {
	if !validSubject(request.Subject) || !validName(request.Metric) || !validName(request.Key) || !validName(request.Fingerprint) || len(request.Scopes) > 128 || len(request.Permissions) > 128 {
		return ErrConfiguration
	}
	for _, names := range [][]string{request.Scopes, request.Permissions} {
		for _, name := range names {
			if !validName(name) || strings.Contains(name, "*") {
				return ErrConfiguration
			}
		}
	}
	return nil
}

// TokenPolicy binds an access-manager token-creation port to one configured
// system. It never accepts the system name from an HTTP request.
type TokenPolicy struct {
	// Service enforces the live system-specific grant.
	Service *Service
	// System is the resource system whose token-inventory limits apply.
	System string
}

// TokenLimits returns explicit current limits for the target user's system grant.
func (p TokenPolicy) TokenLimits(ctx context.Context, userID string) (TokenLimits, error) {
	if p.Service == nil || !validName(p.System) {
		return TokenLimits{}, ErrConfiguration
	}
	g, err := p.Service.Resolve(ctx, Subject{System: p.System, Kind: UserSubject, ID: userID})
	return g.Tokens, err
}

// WithTokenCreation fences the system grant while the callback fences its
// owner-wide inventory, counts and inserts. A grant lock alone cannot serialize
// shared inventory across systems. Mongo implementations require every
// callback operation to use this context and the same Mongo client. Custom
// stores/services that cannot provide this guarantee must not opt into the port.
func (p TokenPolicy) WithTokenCreation(ctx context.Context, userID string, create func(context.Context, TokenLimits) error) error {
	if p.Service == nil || !validName(p.System) || !validName(userID) || create == nil {
		return ErrConfiguration
	}
	if err := p.Service.checkContext(ctx); err != nil {
		return err
	}
	return p.Service.store.WithGrant(ctx, Subject{System: p.System, Kind: UserSubject, ID: userID}, time.Now().UTC(), func(tx context.Context, g Grant) error { return create(tx, g.Tokens) })
}

// checkContext rejects incomplete service wiring and canceled requests before
// invoking custom persistence. Stores must still honor cancellation and preserve
// transactional guarantees after dispatch; this is not a commit-result check.
func (s *Service) checkContext(ctx context.Context) error {
	if s == nil || s.store == nil || ctx == nil {
		return ErrConfiguration
	}
	return ctx.Err()
}

// validName bounds exact policy identifiers and disallows ambiguous whitespace.
func validName(s string) bool {
	return s != "" && len(s) <= 256 && utf8.ValidString(s) && strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

// validSubject permits only distinct user or credential identities.
func validSubject(s Subject) bool {
	return validName(s.System) && validName(s.ID) && (s.Kind == UserSubject || s.Kind == APITokenSubject)
}

// validateGrant bounds policy size and rejects impossible or unsafe quota values.
func validateGrant(g Grant) error {
	if !validSubject(g.Subject) || len(g.Scopes) > 128 || len(g.Permissions) > 128 || len(g.Limits) > 64 {
		return ErrConfiguration
	}
	for _, names := range [][]string{g.Scopes, g.Permissions} {
		for _, name := range names {
			if !validName(name) || strings.Contains(name, "*") {
				return ErrConfiguration
			}
		}
	}
	if err := ValidateTokenLimits(g.Tokens); err != nil {
		return err
	}
	for name, l := range g.Limits {
		if !validName(name) || l.Maximum < 0 || l.Maximum > 1000000000 || l.WindowSeconds < 1 || l.WindowSeconds > 31536000 {
			return ErrConfiguration
		}
	}
	return nil
}

// ValidateTokenLimits checks the shared bounded inventory/TTL contract without
// reading policy or granting authority. Zero disables a capability; coherent
// dormant TTL settings are preserved. Managers may map a rejected client input
// to a validation response while treating corrupt stored policy as unavailable.
func ValidateTokenLimits(t TokenLimits) error {
	if t.Permanent < 0 || t.Ephemeral < 0 || t.Permanent > 1000000 || t.Ephemeral > 1000000 || t.MinimumTTL < 0 || t.MaximumTTL < 0 || t.MaximumTTL > 31536000 || t.TTLIncrement < 0 {
		return ErrConfiguration
	}
	if t.Ephemeral > 0 || t.MinimumTTL != 0 || t.MaximumTTL != 0 || t.TTLIncrement != 0 {
		if t.MinimumTTL < 1 || t.MaximumTTL < t.MinimumTTL || t.TTLIncrement < 1 || t.TTLIncrement > t.MaximumTTL {
			return ErrConfiguration
		}
		// Preserve a coherent TTL policy when creation is temporarily disabled,
		// and reject ranges containing no permitted absolute-step lifetime.
		if ((t.MinimumTTL+t.TTLIncrement-1)/t.TTLIncrement)*t.TTLIncrement > t.MaximumTTL {
			return ErrConfiguration
		}
	}
	return nil
}

// usableGrant checks current eligibility even when an old receipt exists.
func usableGrant(g Grant, s Subject, now time.Time) error {
	if g.Subject != s || g.Revision < 1 || !g.Enabled || (!g.ExpiresAt.IsZero() && !now.Before(g.ExpiresAt)) {
		return ErrDenied
	}
	return validateGrant(g)
}

// includes applies exact AND semantics; no wildcard or administrator bypass.
func includes(granted, required []string) bool {
	for _, want := range required {
		if !validName(want) {
			return false
		}
		found := false
		for _, have := range granted {
			if want == have {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
