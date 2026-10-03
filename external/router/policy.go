package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
)

// AccessMode names the existing trusted authentication adapter, not a token
// claim or a role supplied by a client. Public exposure must be explicit.
type AccessMode string

const (
	// Public requires no authentication; sensitive proof endpoints use HandlerVerified.
	Public AccessMode = "public"
	// HandlerVerified delegates a named purpose-specific proof to its handler.
	HandlerVerified AccessMode = "handler_verified"
	// PublicRateLimited requires the configured brute-force protection middleware.
	PublicRateLimited AccessMode = "public_rate_limited"
	// OptionalActive accepts active sessions or a rate-limited anonymous identity.
	OptionalActive AccessMode = "optional_active"
	// Session requires an authenticated JWT session.
	Session AccessMode = "session"
	// ActiveSession requires an active account's JWT session.
	ActiveSession AccessMode = "active_session"
	// AdminSession requires the existing administrator JWT middleware.
	AdminSession AccessMode = "admin_session"
	// SessionOrAPI accepts the configured session or API-token authentication adapter.
	SessionOrAPI AccessMode = "session_or_api"
	// ActiveSessionOrAPI additionally requires an active account.
	ActiveSessionOrAPI AccessMode = "active_session_or_api"
	// AdminSessionOrAPI requires an administrator session or administrator API token.
	AdminSessionOrAPI AccessMode = "admin_session_or_api"
	// ProfileOptional preserves the existing crawler-safe /me response adapter.
	ProfileOptional AccessMode = "profile_optional"
)

var (
	// ErrRouteConfiguration indicates an invalid or incompletely protected route.
	ErrRouteConfiguration = errors.New("router/invalid-route-policy")
	// ErrRouteDenied is a deliberately detail-free authorization rejection.
	ErrRouteDenied = errors.New("router/route-access-denied")
	// ErrRouteUnauthenticated indicates missing or inconsistent verified identity.
	ErrRouteUnauthenticated = errors.New("router/authentication-required")
	// ErrRouteUnavailable hides authorization dependency failures from clients.
	ErrRouteUnavailable = errors.New("router/authorization-unavailable")
	// ErrRoutePrecondition indicates a required If-Match header was not provided.
	ErrRoutePrecondition = errors.New("router/revision-required")
	// ErrRouteInvalidRevision indicates an unsupported or malformed If-Match.
	ErrRouteInvalidRevision = errors.New("router/invalid-revision")
	// ErrRouteLimitReached indicates the admitted-request budget was exhausted.
	ErrRouteLimitReached = errors.New("router/usage-limit-reached")
)

// RoutePolicy contains additive restrictions evaluated after authentication.
// These are requirements, never grants. A configured evaluator must implement
// every non-empty restriction; resource ownership remains a live domain check.
type RoutePolicy struct {
	// Scopes requires every named credential grant; no wildcard or role union.
	Scopes []string
	// Permissions requires every named current account/resource permission.
	Permissions []string
	// UserTypes optionally restricts the current persisted account type.
	UserTypes []string
	// ResourceCheck names a registered live ownership/relationship check.
	ResourceCheck string
	// RevisionRequired requires one strong If-Match; the domain compares revisions.
	RevisionRequired bool
	// Proof documents a concrete handler-owned proof, such as an OAuth transaction.
	Proof string
	// UsageMetric consumes one admission per HTTP attempt, not per business action.
	// The evaluator must use a server-generated key, never a client replay header.
	UsageMetric string
}

// RouteDefinition is a copyable, credential-free audit description. Methods
// include OPTIONS only when explicitly supported. Order is registration order.
type RouteDefinition struct {
	// Methods is the exact set accepted by Mux; it never implies HEAD or OPTIONS.
	Methods []string
	// Path is relative at registration and full-prefix in the inventory/evaluator.
	Path string
	// Operation is a stable developer-assigned operation ID, not a URL or body value.
	Operation string
	// Access is inherited from the group unless an explicit public proof is declared.
	Access AccessMode
	// Policy holds additional constraints and handler-proof documentation.
	Policy RoutePolicy
	// ShadowedMethods is inventory-only: methods fully covered by an earlier
	// registration. Only shared legacy OPTIONS is permitted; others invalidate
	// the registry. Registration ignores caller-supplied values for this field.
	ShadowedMethods []string
}

// policyMatcher retains compiled templates, not concrete resource identifiers.
type policyMatcher struct {
	// pattern is Mux's full-prefix expression, compiled once at registration.
	pattern *regexp.Regexp
	// methods is an independent snapshot used to detect method-specific shadows.
	methods []string
}

// RouteAuthorizer evaluates trusted route requirements using already verified
// request context. It must fail closed on missing/unknown grants or checks. Do
// not infer API-token authority from the owning user's roles or JWT metadata.
// Return the original failure; register native domain manifests with
// ConfigureRoutePolicy instead of translating dependency errors. Unknown, joined or
// ambiguous failures produce ROUTE_UNAVAILABLE (503), not ROUTE_DENIED (403).
// A custom Is method may declare a response classification, never grant access;
// adapters must not classify an availability failure as an ordinary denial.
type RouteAuthorizer func(context.Context, *http.Request, RouteDefinition) error

// SetRouteAuthorizer sets the evaluator before any policy routes are registered.
// Configuration is startup-only, like Mux registration. It cannot relax routes
// already assembled, and must not run concurrently with registration or serving.
func (r *Router) SetRouteAuthorizer(authorize RouteAuthorizer) error {
	if r.policyStarted {
		return ErrRouteConfiguration
	}
	r.authorizer = authorize
	return nil
}

// ConfigureRoutePolicy installs both request enforcement and adapter-specific
// startup validation. Call before creating any group. Nil callbacks are rejected;
// SetRouteAuthorizer remains available for simpler custom evaluators. Optional
// manifests extend router defaults and override duplicate keys in argument order.
// Map entries are copied; reference-valued metadata remains immutable host data.
// Every supplied error must have a 4xx/5xx status; successful error overrides are
// rejected here even though the general reply composer permits them elsewhere.
// Formatting overrides never authorize dispatch or imply a retry is safe.
func (r *Router) ConfigureRoutePolicy(authorize RouteAuthorizer, validate func(RouteDefinition) error, manifests ...reply.ErrorManifest) error {
	if r == nil || authorize == nil || validate == nil || r.policyStarted {
		return ErrRouteConfiguration
	}
	for _, manifest := range manifests {
		for key, response := range manifest {
			if key == nil || response.StatusCode < 400 || response.StatusCode > 599 {
				return ErrRouteConfiguration
			}
		}
	}
	r.authorizer, r.policyValidator = authorize, validate
	r.policyErrorMaps = errormanifest.CloneManifests(manifests...)
	return nil
}

// RouteGroup keeps the existing ordered Mux subrouter and trusted middleware.
// Fields are private so callers cannot change policy after handlers capture it.
type RouteGroup struct {
	// owner collects immutable route snapshots and startup validation errors.
	owner *Router
	// mux preserves existing prefix matching and middleware order.
	mux *mux.Router
	// prefix is the group's full API path, with no trailing slash.
	prefix string
	// access records the actual configured authentication mode.
	access AccessMode
	// missing records missing required middleware, preventing handler execution.
	missing bool
}

// NewRouteGroup reuses an existing authentication middleware without changing
// cookie/refresh behavior. Missing middleware for any protected/rate-limited
// mode fails validation and returns 503 at runtime, never anonymous access.
func (r *Router) NewRouteGroup(prefix string, access AccessMode, middleware mux.MiddlewareFunc) *RouteGroup {
	r.policyStarted = true
	g := &RouteGroup{owner: r, mux: r.httpRouter.PathPrefix(prefix).Subrouter(), prefix: prefix, access: access}
	g.missing = !knownAccess(access) || (access != Public && access != HandlerVerified && middleware == nil)
	if prefix == "" || !strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") || strings.ContainsAny(prefix, "?#") || strings.IndexFunc(prefix, unicode.IsSpace) >= 0 {
		g.missing = true
	}
	if g.missing {
		r.policyErrors = append(r.policyErrors, fmt.Errorf("%w: invalid route group", ErrRouteConfiguration))
	}
	if middleware != nil {
		g.mux.Use(middleware)
	}
	return g
}

// Handle registers one explicit route. ValidateRoutePolicies must succeed
// before serving. Invalid routes retain a closed runtime backstop for older
// AttachRoutes callers that cannot return registration errors.
func (g *RouteGroup) Handle(def RouteDefinition, handler http.HandlerFunc) {
	def = cloneRoute(def)
	def.ShadowedMethods = nil
	if def.Access == "" {
		def.Access = g.access
	}
	invalid := g.missing || handler == nil || !validPolicyName(def.Operation) || len(def.Methods) == 0 || !validPolicy(def.Policy)
	invalid = invalid || (def.Path != "" && !strings.HasPrefix(def.Path, "/")) || strings.ContainsAny(def.Path, "?#")
	// A group cannot be downgraded or upgraded by metadata. Only a public group
	// can mark a handler's explicit proof boundary without an auth middleware.
	invalid = invalid || (def.Access != g.access && !(g.access == Public && def.Access == HandlerVerified))
	invalid = invalid || (def.Access == HandlerVerified && def.Policy.Proof == "")
	invalid = invalid || (hasRequirements(def.Policy) && g.owner.authorizer == nil)
	// Policy runs before handlers and cannot rely on a proof that a public
	// handler has not yet checked. Optional-auth modes may require identity
	// additively, denying their anonymous branch for these routes.
	invalid = invalid || (hasRequirements(def.Policy) && (def.Access == Public || def.Access == HandlerVerified || def.Access == PublicRateLimited))
	full := cloneRoute(def)
	full.Path = g.prefix + def.Path
	if g.owner.policyValidator != nil && g.owner.policyValidator(cloneRoute(full)) != nil {
		invalid = true
	}
	seenMethods := make(map[string]bool, len(def.Methods))
	for _, method := range def.Methods {
		if !validMethod(method) || seenMethods[method] {
			invalid = true
		}
		seenMethods[method] = true
		// OPTIONS is explicitly shared by legacy method-specific registrations;
		// first-match semantics remain unchanged. Other exact duplicates are errors.
		if method == http.MethodOptions {
			continue
		}
		for _, previous := range g.owner.policyRoutes {
			if previous.Path == full.Path && contains(previous.Methods, method) {
				invalid = true
			}
		}
	}
	authorize := g.owner.authorizer
	leaf := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		// A duplicate can shadow an invalid later declaration. Refuse every
		// policy handler when any registry configuration error was recorded.
		if len(g.owner.policyErrors) != 0 {
			writeRouteError(request.Context(), w, ErrRouteConfiguration, g.owner.policyErrorMaps...)
			return
		}
		if authorize != nil {
			if err := authorize(request.Context(), request, cloneRoute(full)); err != nil {
				writeRouteError(request.Context(), w, err, g.owner.policyErrorMaps...)
				return
			}
		}
		handler.ServeHTTP(w, request)
	})
	route := g.mux.Handle(def.Path, leaf).Methods(def.Methods...)
	pattern, err := route.GetPathRegexp()
	if err != nil {
		invalid = true
	} else {
		compiled, compileErr := regexp.Compile(pattern)
		if compileErr != nil {
			invalid = true
		} else {
			for _, previous := range g.owner.policyMatchers {
				// Different parameter names can compile to the same matcher.
				// An earlier wildcard can also fully shadow a later static path.
				if previous.pattern.String() != pattern && (strings.Contains(full.Path, "{") || !previous.pattern.MatchString(full.Path)) {
					continue
				}
				for _, method := range def.Methods {
					if contains(previous.methods, method) {
						if !contains(full.ShadowedMethods, method) {
							full.ShadowedMethods = append(full.ShadowedMethods, method)
						}
						if method != http.MethodOptions {
							invalid = true
						}
					}
				}
			}
			g.owner.policyMatchers = append(g.owner.policyMatchers, policyMatcher{pattern: compiled, methods: append([]string(nil), def.Methods...)})
		}
	}
	g.owner.policyRoutes = append(g.owner.policyRoutes, cloneRoute(full))
	if invalid {
		g.owner.policyErrors = append(g.owner.policyErrors, fmt.Errorf("%w: %s", ErrRouteConfiguration, def.Operation))
	}
}

// RouteInventory returns independent metadata snapshots in registration order.
// It deliberately excludes handlers, credentials and request-specific resource IDs.
func (r *Router) RouteInventory() []RouteDefinition {
	result := make([]RouteDefinition, len(r.policyRoutes))
	for i, route := range r.policyRoutes {
		result[i] = cloneRoute(route)
	}
	return result
}

// ValidateRoutePolicies reports all policy registration errors before startup.
// It covers registered descriptors, not arbitrary routes added via GetRouter.
func (r *Router) ValidateRoutePolicies() error { return errors.Join(r.policyErrors...) }

// cloneRoute prevents inventory/evaluator mutations from changing enforcement.
func cloneRoute(d RouteDefinition) RouteDefinition {
	d.Methods = append([]string(nil), d.Methods...)
	d.Policy.Scopes = append([]string(nil), d.Policy.Scopes...)
	d.Policy.Permissions = append([]string(nil), d.Policy.Permissions...)
	d.Policy.UserTypes = append([]string(nil), d.Policy.UserTypes...)
	d.ShadowedMethods = append([]string(nil), d.ShadowedMethods...)
	return d
}

// hasRequirements distinguishes documentation from enforceable restrictions.
func hasRequirements(p RoutePolicy) bool {
	return len(p.Scopes)+len(p.Permissions)+len(p.UserTypes) > 0 || p.ResourceCheck != "" || p.RevisionRequired || p.UsageMetric != ""
}

// validPolicy rejects ambiguous or unbounded definitions before requests run.
func validPolicy(p RoutePolicy) bool {
	for _, names := range [][]string{p.Scopes, p.Permissions, p.UserTypes} {
		if len(names) > 128 {
			return false
		}
		seen := make(map[string]bool, len(names))
		for _, name := range names {
			if !validPolicyName(name) || seen[name] {
				return false
			}
			seen[name] = true
		}
	}
	for _, name := range []string{p.ResourceCheck, p.Proof, p.UsageMetric} {
		if name != "" && !validPolicyName(name) {
			return false
		}
	}
	return true
}

// validPolicyName accepts exact, bounded identifiers; it never expands wildcards.
func validPolicyName(s string) bool {
	return s != "" && len(s) <= 256 && utf8.ValidString(s) && !strings.Contains(s, "*") && strings.IndexFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) < 0
}

// knownAccess rejects typoed policy classes instead of treating them as public.
func knownAccess(a AccessMode) bool {
	switch a {
	case Public, HandlerVerified, PublicRateLimited, OptionalActive, Session, ActiveSession, AdminSession, SessionOrAPI, ActiveSessionOrAPI, AdminSessionOrAPI, ProfileOptional:
		return true
	}
	return false
}

// validMethod permits only explicit standard HTTP methods.
func validMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return true
	}
	return false
}

// contains checks exact values; wildcards are not implicitly expanded.
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
