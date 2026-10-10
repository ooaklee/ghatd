# Router

The `router` package provides a standardised, project-specific wrapper around `gorilla/mux`. It's designed to simplify setting up common application-level routing concerns, like default handlers, middleware, and authentication endpoints.

## Architecture

-   **`router.go`**: Contains the `Router` struct and the `NewRouter` constructor. It initialises a `mux.Router` and applies any provided default handlers or global middleware.
-   **`handler.go`**: Provides handlers for common, cross-cutting concerns. A key example is `NewAuthVerifyHandler`, which manages the redirection flow for email and login verification links.
-   **`auth_verify.go`**: Provides `AttachDefaultAuthVerifyRoute`, a host-application helper that registers GHATD's default verification route from backend and frontend base URLs. Use `NewAuthVerifyHandler` directly when the host application needs custom endpoint paths.
-   **`const.go`**: Defines constant URI paths for shared endpoints like health checks (`/v0/health/check`) and authentication verification (`/v0/auth/verify`).

## Declarative route policies

`NewRouteGroup` combines an explicit access mode with the existing trusted
authentication middleware. `Handle` accepts a `RouteDefinition`: relative path,
HTTP methods, stable operation ID and optional additive requirements. Empty
relative paths intentionally register the group prefix itself. Methods are exact:
GET does not imply HEAD or OPTIONS.

Configure a policy authorizer before creating **any** route group. The
[access-manager guard](../accessmanager/middleware/README.md#route-policy-guard)
is an adapter for verified context and a live policy service. It is not installed
automatically. Required scopes, permissions, types, revision checks, resource
checks or usage budgets without an authorizer invalidate registration. Custom
authorizers must enforce every declared restriction; metadata is not a grant.

After attaching all routes, return `ValidateRoutePolicies()` and refuse to start
on failure. `starter/v0.AttachDefaultRoutes` returns these validation errors.
All descriptor handlers also return a structured 503 when any registry error is
present, as a runtime backstop; that is not a substitute for startup validation.

Access modes preserve the preloaded authentication distinctions: public,
rate-limited public, optional active identity, JWT session, active/admin session,
and the corresponding session-or-API modes. Public proof endpoints use
`HandlerVerified` and a named `Proof`; their handlers must actually validate that
proof. Identity-dependent requirements are rejected on public/proof modes because
the policy check runs before the handler has verified a proof. Optional-auth
routes may declare requirements, but their anonymous branch is then denied.

`RouteInventory()` returns defensive copies in registration order. The registry
rejects exact method/path duplicates, equivalent compiled parameter patterns,
and an earlier pattern that fully covers a later literal path. Shared legacy
OPTIONS retains first-match behaviour and is listed in `ShadowedMethods`.
This is **not** a complete regex-intersection or route-coverage proof. Review
partially overlapping dynamic routes manually.

Routes registered directly through `GetRouter()`—including arbitrary host
routes—remain outside this inventory and enforcement. An empty validation result
does not certify those routes. Audit them explicitly, including middleware that
short-circuits before a descriptor handler. Registration/configuration is
startup-only and must not run concurrently with serving. Concurrent requests and
inventory reads receive independent policy snapshots.

### Native policy error manifests

`ConfigureRoutePolicy(authorize, validate, manifests...)` extends the router's
default error maps. Return native errors from the authorizer rather than replacing
them with router sentinels. Later manifests override earlier entries for the same
identity. Map entries are copied before registration; reference-valued metadata
must remain immutable. Every supplied error needs a 4xx/5xx status, so a policy
failure cannot be configured as an HTTP success or redirect.

The access-manager guard installs its policy-domain map automatically. Supply
resource-domain maps and host overrides through `guard.Install(router, maps...)`.
These response settings never authorize handler dispatch. Wrapped single-cause
errors resolve by manifest identity; joined, unknown, ambiguous or malformed
errors remain `ROUTE_UNAVAILABLE` (503). Custom `Is` classifications affect only
the public response; adapters must not mislabel dependency outages as denials.
Original errors remain available to direct callers for internal cause inspection.

### Coverage and explicit raw-route boundaries

Billing, content, group, policy, pricing, user, user-manager, vision, blueprint,
SEO and communications attachment functions register descriptors. Their public routes
do not require an admin identity, but still participate in registry validation
and a configured authorizer. For SEO and communications, `AdminAccess` defaults
to `AdminSession`; explicitly select `AdminSessionOrAPI` when using that enforcing
middleware. Neither package accepts a public or member-only admin mode.

**Compatibility:** required authentication and optional-auth/rate-limit adapters
must be supplied even if a caller only exercises the attachment's public routes.
Missing adapters invalidate the entire registry. User Manager's custom `/me`
adapter must enforce the `ProfileOptional` contract; when omitted it falls back
to the standard `SessionOrAPI` adapter. Its admin-service adapter similarly falls
back to `AdminSession`. Custom adapters are trusted code, not verified by metadata.
The legacy duplicate admin declaration for `GET /api/v1/ums/users` was already
shadowed by the earlier authenticated declaration; that reachable route and its
service-level group-membership filtering are preserved.

Access Manager also registers login, email verification, refresh and optional
OAuth endpoints. Its `HandlerVerified` proof names are documentation, not proof
verification. Keep the service's one-use, live-session and provider checks;
install both the active-session and hardened code-rate-limit adapters. See its
[route contract](../accessmanager/README.md#route-registry) for compatibility.

Some framework HTTP surfaces still use raw Mux routing and are deliberately
**not** certified by `ValidateRoutePolicies()`:

| Surface | Current boundary and host responsibility |
| --- | --- |
| Constructor-supplied health handler | Host-owned response and any global middleware; no descriptor-specific access or method restriction. Do not expose private diagnostics. |
| Default auth-verification redirect | Redirects to the configured login/email-verification endpoints; those endpoints must validate the proof. The redirect is not authentication. |
| SPA/static fallback | Serves the configured filesystem and path rewrite. Mount after API routes; never put protected data or business commands in the fallback. |
| Opt-in local email inbox | Its own loopback check unless `AllowRemote` is enabled; keep development-only. A loopback reverse proxy is not end-user authentication. Protect it explicitly or do not mount it. |
| Browser trace intake | Separately mounted handler with its own origin, payload and rate bounds, intentionally outside application authentication and inside outer telemetry. |

This list describes the framework's built-in exceptions, not arbitrary host
handlers or proof of complete host coverage. Audit raw middleware and custom
mounts separately. An invalid descriptor registry does not disable these raw
routes, which is another reason to reject startup rather than rely on the runtime
503 backstop.

### Proof-owned application flows

A host can register endpoints that resolve a purpose-specific principal inside
their handler, such as a scoped guest capability. Use `HandlerVerified` with a
stable `Policy.Proof` name and register the actual enforcing handler through
`RouteGroup.Handle`. The global access-manager guard deliberately does not turn
such a principal into a member or API-token identity. The handler must validate
the proof, its resource binding, and current authority before any side effect.
The domain must recheck authority within the transaction, including replay.

For conditional, resource-bound admission inside that handler, use
[`accessproof`](../accessproof/README.md) with freshly resolved evidence. It
shares exact category, assurance, capability and expiry evaluation; it does not
authenticate the source or replace transactional resource checks.

Do not copy conditional capability requirements into `Policy.Scopes` or put
post-proof revision checks in `Policy.RevisionRequired`: those standard policies
run **before** a handler-owned proof. Registration rejects that combination.
The proof name is documentation, not executable authentication by itself.

Register exact endpoint descriptors before a namespace's error fallback and
before a SPA. Keep the fallback response-only: it must never dispatch a command
that missed framework admission. Return startup validation failures and recheck
`ValidateRoutePolicies()` after all registrations. If a raw error fallback is
needed, make it fail closed when the registry is invalid too. It remains outside
the inventory; this does not excuse unregistered command routes. Test actual
framework admission, not just descriptor equality or a direct handler call.

This extension reuses routing, inventory, startup validation and the explicit
proof boundary. It does not add guest grant persistence, interpret application
roles, apply user quotas to guests, or make custom identity restrictions part
of the standard member/API-token guard.

### Shared strong-revision validation

`StrongETagPolicy` validates a non-empty strong opaque tag and reads a singular
`If-Match` header through `IfMatch(request, required)`. It rejects weak tags,
wildcards, lists, duplicate headers and control characters. Required absence is
`ErrRoutePrecondition`; malformed input is `ErrRouteInvalidRevision`. Optional
absence is accepted, but an explicitly empty header is not absence. Errors never
include the supplied revision value.

The standard access-manager route guard uses a 256-byte limit, trims surrounding
whitespace and accepts HTTP obs-text bytes. Hosts can explicitly choose stricter
ASCII-only validation, reject outer whitespace, or retain another bounded size
without copying the parser. `MaxBytes` includes quotes, defaults to 256, and must
be at least three. Configure these options on the server, not from a request.

```go
policy := router.StrongETagPolicy{MaxBytes: 512, ASCIIOnly: true}
revision, err := policy.IfMatch(request, true)
// Translate err through the host's reviewed error manifest. Compare revision
// to current storage inside the command transaction; syntax is not authority.
```

Call `Validate(tag)` when validating an outgoing ETag or a non-header value.
Neither API authenticates the caller, applies CSRF/idempotency, nor checks a
stored revision. Preserve that ordering in proof-owned application handlers.

Policy errors use reply/v2's structured envelope without internal diagnostics:

| Code | HTTP | Meaning |
| --- | --- | --- |
| `ROUTE_AUTHENTICATION` | 401 | Verified identity is missing or inconsistent. |
| `ROUTE_DENIED` | 403 | Current policy or resource authority rejects access. |
| `ROUTE_REVISION_REQUIRED` | 428 | A required If-Match header is absent. |
| `ROUTE_REVISION_INVALID` | 400 | If-Match is not one non-empty strong entity tag. |
| `ROUTE_LIMIT_REACHED` | 429 | The admission quota is exhausted. |
| `ROUTE_UNAVAILABLE` | 503 | Policy evaluation is unavailable or returned an unclassified/ambiguous failure. |
| `ROUTE_CONFIGURATION` | 503 | The route registry or policy adapter is invalid. |

Custom `RouteAuthorizer` implementations must return or wrap one of the route
sentinels for an intentional rejection. In particular, return `ErrRouteDenied`
for a deliberate 403; an arbitrary error with the same text is not equivalent.
The shared [strict manifest resolver](../errormanifest/README.md#strict-single-cause-authentication-boundaries)
rejects every joined error, including singleton joins, as well as unknown,
ambiguous, typed-nil, cyclic and over-deep chains. These become
`ROUTE_UNAVAILABLE` (503), not a proven policy denial. All such failures stop the
domain handler. Request context is forwarded to the shared reply writer and is
not serialized into the response.

**Adapter contract:** return or wrap `ErrRouteDenied` for deliberate 403
responses. Use ordinary `%w` wrapping, not
`errors.Join`, for a known single-cause rejection. Clients must not treat a 503
as proof that permissions changed; retry only according to the endpoint's
idempotency and backoff policy. This does not add automatic retries or alter
the selected authentication middleware.

## HTTP boundary and telemetry

For complete request telemetry, wrap the router at the HTTP server boundary.
Mux middleware runs only after a successful match, so installing telemetry
only with `GetRouter().Use(...)` omits generated 404/405 responses and some
redirects. Use GHATD's composed boundary:

```go
handler := otelhttp.Wrap("my-service", runtime.Logger(), ghatdRouter.GetRouter())
```

Import `otelhttp` from `external/observability/otelhttp` and start the
[observability runtime](../observability/README.md#bootstrap) before constructing
dependencies. Pass `handler` to the HTTP server. It applies telemetry, request
logging, recovery, then the complete router. Install it once, removing duplicate
request loggers/recovery inside the router. Keep route-specific middleware,
such as authentication and caching, on the router.

Use `WrapWithOptions` for explicit
[request-log details](../observability/CONFIGURATION.md#http-request-log-details)
or trace-suppression policies. A catch-all SPA route can have template `/` even
when its original `url.path` is a different page; the template remains suitable
for grouping. See [service adoption](../../docs/how-to/add-service-observability.md)
for signal and lifecycle choices.

`NewRouter` installs lightweight route observation before supplied middleware.
The `external/router/routecontext` package shares the matched template with
outer middleware without another matching pass. This covers middleware that
responds immediately, nested routes, and strict-slash redirects. Unmatched
requests and redirects performed before matching retain an empty template;
their raw paths are never used as route labels.

For a plain Gorilla Mux router, install `routecontext.ObserveMiddleware` with
`Use` before other Mux middleware. Outer middleware can call
`routecontext.Begin(request)` before dispatch and `routecontext.Template(request)`
afterward. GHATD's HTTP telemetry and request logger initialize that shared
state automatically.

Recovery records a constant panic classification and writes a generic 500 only
if a final response has not already been committed or the connection hijacked.
It preserves committed response statuses and HTTP streaming interfaces. An
intentional `http.ErrAbortHandler` is rethrown unchanged; upstream HTTP
instrumentation ends its span during that unwind but does not complete its
request-duration metric. Request logging also skips its completion event on
that intentional abort.

The following example shows how to initialise the `ghatdRouter`, configure it with default handlers and middleware, and attach a verification endpoint.

### Example Initialisation

This setup is typically done once in your application's `main` function or wherever you configure your HTTP server.

```go
package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
	// ... other imports
)

func main() {
	// ... initialise logger and other dependencies

	// Define base URLs for the application (this will be the same if packaging your UI with the ghatd backend)
	backendBaseURL := "http://localhost:8080"
	frontendBaseURL := "http://localhost:3000"

	// 1. Define Default Handlers
	// These handlers are used by the router for unhandled routes or health checks.
	default404Handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "Not Found")
	})
	defaultHealthcheckHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	// 2. Define Global Middleware (e.g., CORS)
	// This is a mock CORS middleware for demonstration purposes.
	// In a real project, you would use a proper CORS library.
	corsMiddleware := func(allowedOrigins []string) mux.MiddlewareFunc {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Access-Control-Allow-Origin", "*") // Be more restrictive in production
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
			})
		}
	}

	// 3. Create a New Router
	// Initialise the router with the default handlers and global middleware.
	ghatdRouter := router.NewRouter(
		default404Handler,
		defaultHealthcheckHandler,
		corsMiddleware([]string{frontendBaseURL}),
	)

	// 4. Add Application-Specific Handlers
	// The default auth verify route processes verification links from emails.
	if err := router.AttachDefaultAuthVerifyRoute(&router.AttachDefaultAuthVerifyRouteRequest{
		Router:          ghatdRouter,
		BackendBaseURL:  backendBaseURL,
		FrontendBaseURL: frontendBaseURL,
	}); err != nil {
		panic(err)
	}

	// 5. Attach Service-Specific Routes
	// At this point, you would attach the routes for each of your services.
	// For example:
	//
	// usermanager.AttachRoutes(&usermanager.AttachRoutesRequest{
	// 	Router:  ghatdRouter,
	// 	Handler: umsHandler,
	// 	// ... middleware
	// })

	// 6. Wrap the router with the boundary middleware shown above, then start
	// the HTTP server with that wrapped handler.
	// http.ListenAndServe(":8080", handler)
}
```

By following this pattern, you establish a consistent foundation for routing across your entire application, which can then be referenced by other "Getting Started" guides.


## Optional response middleware

[Response middleware helpers](../middleware/helper/README.md) compose content
type, a process-local LRU cache, gzip and HTML/SVG/JSON minification in order.
Pass their returned slice to `NewRouter` before serving. Supply explicit cache
settings, an optional observer and your own private-route bypass decorator;
this helper neither authenticates callers nor installs privacy rules. Keep
application logging/tracing at the existing outer HTTP boundary.
