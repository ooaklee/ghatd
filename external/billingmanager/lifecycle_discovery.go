package billingmanager

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/billing"
)

// LifecycleDiscovery is a dedicated scoped capability, independent of read or
// refresh permission. Hosts enforce it through current instance-bound authority;
// a saved cursor, preparing author or human role never grants this permission.
const LifecycleDiscovery = "billing.subscription-status.discover"

// LifecycleDiscoveryTarget comes from a validated query or owning billing
// candidate, never transport assertions. An empty PrincipalID requests scope
// and source-kind permission before lookup and again before page disclosure.
// Acknowledged checkouts supply IntentID; subscription sources supply
// SubscriptionID. The selected principal remains independently authorized.
type LifecycleDiscoveryTarget struct {
	Scope                                       billing.RevenueScope `json:"-"`
	Kind, PrincipalID, SubscriptionID, IntentID string               `json:"-"`
}
type LifecycleDiscoveryAuthority interface {
	AuthorizeLifecycleDiscovery(context.Context, string, string, LifecycleDiscoveryTarget) error
}

// LifecycleDiscoveryService is an optional capability on the SAME configured
// revenue feed. No alternative owner, repository or provider lookup is injected.
type LifecycleDiscoveryService interface {
	DiscoverLifecycleSources(context.Context, billing.LifecycleDiscoveryQuery) (billing.LifecycleDiscoveryPage, error)
}

// WithLifecycleDiscoveryAuthority enables private discovery after the owning
// revenue feed is installed. It creates no grant, migration, worker or route.
func (s *Service) WithLifecycleDiscoveryAuthority(a LifecycleDiscoveryAuthority) (*Service, error) {
	if s == nil || nilRevenueDependency(a) || nilRevenueDependency(s.revenueFeed) {
		return nil, billing.ErrRevenueUnavailable
	}
	owner, ok := s.revenueFeed.(LifecycleDiscoveryService)
	if !ok || nilRevenueDependency(owner) {
		return nil, billing.ErrRevenueUnavailable
	}
	s.lifecycleDiscoveryAuthority = a
	return s, nil
}
func (s *Service) discoveryOwner(ctx context.Context, actor string) (LifecycleDiscoveryService, error) {
	if ctx == nil || actor == "" || strings.TrimSpace(actor) != actor || len(actor) > 256 || strings.ContainsAny(actor, "\r\n\x00") {
		return nil, billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || nilRevenueDependency(s.lifecycleDiscoveryAuthority) || nilRevenueDependency(s.revenueFeed) {
		return nil, billing.ErrRevenueUnavailable
	}
	owner, ok := s.revenueFeed.(LifecycleDiscoveryService)
	if !ok || nilRevenueDependency(owner) {
		return nil, billing.ErrRevenueUnavailable
	}
	return owner, nil
}
func (s *Service) discoveryAuthorize(ctx context.Context, actor string, target LifecycleDiscoveryTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.lifecycleDiscoveryAuthority.AuthorizeLifecycleDiscovery(ctx, actor, LifecycleDiscovery, target); err != nil {
		return err
	}
	return ctx.Err()
}

// DiscoverLifecycleSources checks current scope permission before the owning
// read, current selected payer/source permission for EACH candidate and scope
// permission again before returning the whole page, including an empty one.
// Any failure withholds all candidates AND cursor; no provider/status/financial
// writes or original-source preparation occur through this capability.
func (s *Service) DiscoverLifecycleSources(ctx context.Context, actor string, q billing.LifecycleDiscoveryQuery) (billing.LifecycleDiscoveryPage, error) {
	owner, err := s.discoveryOwner(ctx, actor)
	if err != nil {
		return billing.LifecycleDiscoveryPage{}, err
	}
	if _, err := q.AfterID(); err != nil {
		return billing.LifecycleDiscoveryPage{}, err
	}
	scopeTarget := LifecycleDiscoveryTarget{Scope: q.Scope, Kind: q.Kind}
	if err := s.discoveryAuthorize(ctx, actor, scopeTarget); err != nil {
		return billing.LifecycleDiscoveryPage{}, err
	}
	page, err := owner.DiscoverLifecycleSources(ctx, q)
	if canceled := ctx.Err(); canceled != nil {
		return billing.LifecycleDiscoveryPage{}, canceled
	}
	if err != nil {
		if denied := s.discoveryAuthorize(ctx, actor, scopeTarget); denied != nil {
			return billing.LifecycleDiscoveryPage{}, denied
		}
		return billing.LifecycleDiscoveryPage{}, err
	}
	if err := page.Validate(q); err != nil {
		if denied := s.discoveryAuthorize(ctx, actor, scopeTarget); denied != nil {
			return billing.LifecycleDiscoveryPage{}, denied
		}
		return billing.LifecycleDiscoveryPage{}, err
	}
	for _, candidate := range page.Items {
		target := LifecycleDiscoveryTarget{Scope: candidate.Scope, Kind: q.Kind, PrincipalID: candidate.PrincipalID, SubscriptionID: candidate.SubscriptionID}
		if q.Kind == billing.LifecycleCheckoutSources {
			target.IntentID = candidate.Intent.ID
		}
		if err := s.discoveryAuthorize(ctx, actor, target); err != nil {
			return billing.LifecycleDiscoveryPage{}, err
		}
	}
	if err := s.discoveryAuthorize(ctx, actor, scopeTarget); err != nil {
		return billing.LifecycleDiscoveryPage{}, err
	}
	return page, nil
}
