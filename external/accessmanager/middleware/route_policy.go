package middleware

import (
	"context"
	"crypto/rand"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/router"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

// RoutePolicyService is the consumer-owned live-grant port. A host may supply
// another policy backend, but it must preserve exact AND and atomic admission
// semantics; signed role/type claims are not substitutes for current grants.
type RoutePolicyService interface {
	Authorize(context.Context, accesspolicy.Subject, []string, []string) error
	ConsumeAuthorized(context.Context, accesspolicy.Consumption) (accesspolicy.Usage, error)
}

// ResourceCheck resolves request resource identifiers and checks current domain
// authority for the verified actor. Do not trust owner IDs supplied in a body.
// Sensitive commands must repeat the check within their transaction/replay boundary.
type ResourceCheck func(context.Context, *http.Request, string) error

// RoutePolicyGuard bridges verified access-manager context to declared policy.
// Configure it once at startup; no bearer is parsed here and no role is expanded.
type RoutePolicyGuard struct {
	// system fixes the resource namespace independently of request input.
	system string
	// service resolves grants and atomically accounts for admitted requests.
	service RoutePolicyService
	// resources is a private copy of registered live domain authority checks.
	resources map[string]ResourceCheck
}

// NewRoutePolicyGuard validates dependencies without seeding privileges. The
// supplied map is copied so later host mutations cannot relax installed checks.
func NewRoutePolicyGuard(system string, service RoutePolicyService, resources map[string]ResourceCheck) (*RoutePolicyGuard, error) {
	if !policyIdentifier(system) || service == nil {
		return nil, router.ErrRouteConfiguration
	}
	g := &RoutePolicyGuard{system: system, service: service, resources: make(map[string]ResourceCheck, len(resources))}
	for name, check := range resources {
		if !policyIdentifier(name) || check == nil {
			return nil, router.ErrRouteConfiguration
		}
		g.resources[name] = check
	}
	return g, nil
}

// Install registers both startup validation and request enforcement before any
// routes are attached. Native policy error maps and optional host/resource maps
// format failures without replacing their causes. Hosts still call
// ValidateRoutePolicies before serving. Override maps are copied by the router.
func (g *RoutePolicyGuard) Install(r *router.Router, overrides ...reply.ErrorManifest) error {
	if g == nil || r == nil {
		return router.ErrRouteConfiguration
	}
	return r.ConfigureRoutePolicy(g.Authorize, g.Validate, append([]reply.ErrorManifest{accesspolicy.AccessPolicyErrorMap}, overrides...)...)
}

// Validate rejects references to unknown live checks before the server starts.
func (g *RoutePolicyGuard) Validate(d router.RouteDefinition) error {
	if g == nil || g.service == nil || !policyIdentifier(g.system) {
		return router.ErrRouteConfiguration
	}
	if d.Policy.ResourceCheck != "" && g.resources[d.Policy.ResourceCheck] == nil {
		return router.ErrRouteConfiguration
	}
	return nil
}

// Authorize runs after the route group's authentication middleware. Public
// proof handlers retain their own proof boundary. Optional routes permit an
// anonymous branch only when they declare no identity-dependent requirements.
// UsageMetric counts admitted HTTP attempts; business quotas belong in commands.
func (g *RoutePolicyGuard) Authorize(ctx context.Context, r *http.Request, d router.RouteDefinition) error {
	if ctx == nil || r == nil {
		return router.ErrRouteConfiguration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := g.Validate(d); err != nil {
		return err
	}
	p := d.Policy
	restricted := len(p.Scopes)+len(p.Permissions)+len(p.UserTypes) > 0 || p.ResourceCheck != "" || p.RevisionRequired || p.UsageMetric != ""
	switch d.Access {
	case router.Public, router.PublicRateLimited, router.HandlerVerified:
		if restricted {
			return router.ErrRouteConfiguration
		}
		return nil
	case router.OptionalActive, router.ProfileOptional:
		if !restricted && !helpers.AcquireAuthenticatedFrom(ctx) {
			return nil
		}
	case router.Session, router.ActiveSession, router.AdminSession, router.SessionOrAPI, router.ActiveSessionOrAPI, router.AdminSessionOrAPI:
	default:
		return router.ErrRouteConfiguration
	}
	actor := helpers.AcquireAuthenticatedUserIDFrom(ctx)
	user := helpers.AcquireUserFrom(ctx)
	if actor == "" || user == nil || user.GetUserId() != actor {
		return router.ErrRouteUnauthenticated
	}
	subject := accesspolicy.Subject{System: g.system, Kind: accesspolicy.UserSubject, ID: actor}
	session, apiToken := helpers.AcquireSessionFrom(ctx), helpers.AcquireAPITokenFrom(ctx)
	if apiToken != nil {
		if d.Access == router.Session || d.Access == router.ActiveSession || d.Access == router.AdminSession {
			return router.ErrRouteUnauthenticated
		}
		subject.Kind, subject.ID = accesspolicy.APITokenSubject, apiToken.TokenID
	} else if session == nil || session.AccessUUID == "" {
		return router.ErrRouteUnauthenticated
	}
	// Reassert the declared account boundary from the current user snapshot.
	// User type is classification, never evidence of administrator authority.
	activeRequired := apiToken != nil || d.Access == router.ActiveSession || d.Access == router.ActiveSessionOrAPI || d.Access == router.AdminSession || d.Access == router.AdminSessionOrAPI || d.Access == router.OptionalActive
	if activeRequired && user.Status != userv2.AccountStatusKeyActive {
		return router.ErrRouteDenied
	}
	if (d.Access == router.AdminSession || d.Access == router.AdminSessionOrAPI) && !user.IsAdmin() {
		return router.ErrRouteDenied
	}
	if len(p.UserTypes) > 0 {
		allowed := false
		for _, kind := range p.UserTypes {
			if user.Type == kind {
				allowed = true
				break
			}
		}
		if !allowed {
			return router.ErrRouteDenied
		}
	}
	// Preserve declared middleware-only routes without inventing grants for all
	// existing accounts. Additional scopes/permissions opt into the live store.
	if len(p.Scopes)+len(p.Permissions) > 0 {
		err := g.service.Authorize(ctx, subject, p.Scopes, p.Permissions)
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if err != nil {
			return err
		}
	}
	if p.RevisionRequired {
		if err := requireStrongRevision(r); err != nil {
			return err
		}
	}
	if p.ResourceCheck != "" {
		err := g.resources[p.ResourceCheck](ctx, r, actor)
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if err != nil {
			return err
		}
	}
	if p.UsageMetric != "" {
		_, err := g.service.ConsumeAuthorized(ctx, accesspolicy.Consumption{Subject: subject, Metric: p.UsageMetric, Key: rand.Text(), Fingerprint: d.Operation, Scopes: p.Scopes, Permissions: p.Permissions})
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

// requireStrongRevision requires one non-empty strong entity tag. Although HTTP
// permits an empty entity tag, the route contract requires an actual opaque
// revision. The domain still compares it inside its mutation transaction.
func requireStrongRevision(r *http.Request) error {
	_, err := (router.StrongETagPolicy{MaxBytes: 256, TrimSpace: true}).IfMatch(r, true)
	return err
}

// policyIdentifier validates bounded exact server-owned namespace/check names.
func policyIdentifier(s string) bool {
	return s != "" && len(s) <= 256 && utf8.ValidString(s) && !strings.Contains(s, "*") && strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}
