# Access Manager Middleware

The access manager middleware package exposes the individual middleware methods
used by GHATD route packages, plus a `Suite` helper for the common starter
wiring path.

`NewSuite` is a convenience composition helper. It does not change the behavior
of `Middleware` or `HardenedRateLimitProtection`; it only gives the standard
middleware methods stable names for route attachment.

```go
suite, err := middleware.NewSuite(&middleware.NewSuiteRequest{
	Service:                  accessManagerService,
	EphemeralStore:           ephemeralStore,
	Environment:              appSettings.Environment,
	CookiePrefixAuthToken:    appSettings.CookiePrefixAuthToken,
	CookiePrefixRefreshToken: appSettings.CookiePrefixRefreshToken,
	CookieDomain:             appSettings.CookieDomain,
})
if err != nil {
	return err
}

usermanager.AttachRoutes(&usermanager.AttachRoutesRequest{
	AuthenticatedMiddleware:                      suite.Authenticated,
	ActiveOnlyMiddleware:                         suite.ActiveOnly,
	AdminOnlyMiddleware:                          suite.AdminOnly,
	ActiveValidApiTokenOrJWTMiddleware:           suite.ActiveValidApiTokenOrJWT,
	AdminApiTokenOrJWTMiddleware:                 suite.AdminApiTokenOrJWT,
	ValidApiTokenOrJWTMiddleware:                 suite.ActiveValidApiTokenOrAuthenticated,
	RateLimitOrActiveMiddleware:                  suite.RateLimitOrActive,
	CustomMeEndpointValidApiTokenOrJWTMiddleware: suite.CustomMeEndpointValidApiTokenOrJWT,
})
```

When `ErrorMaps` is nil, `NewSuite` uses `bundles.AuthMiddleware()` as the
default error map set. Pass a non-nil `ErrorMaps` slice, including an empty
slice, to fully own the error mapping used by the suite.

Ordinary wrapped manifest errors are resolved before the replier handles auth
and hardened-limit failures. Unknown or ambiguous failures receive opaque server
responses; configured overrides still take precedence. Hardened-limit storage
failures deny the request but do not create an IP ban: only a confirmed threshold
error triggers that additional action.

## Session-probe response policy

The `/me` compatibility middleware has always had a route-local override for
`ErrUnauthorizedUnableToAttainRequestorID` (`AM00-013`). The general manifest's
401 does not remove that override. Choose the probe's wire contract at startup:

| `MeEndpointResponseMode` | Missing-session response |
| --- | --- |
| `MeEndpointLegacyAccepted` (zero value) | 202 with the existing `errors` JSON envelope |
| `MeEndpointAcceptedEmpty` | 202 with no response body |
| `MeEndpointUnauthorized` | 401 with the same error code/title and status 401 in the envelope |

Set the field on `NewSuiteRequest` or `starter.NewMiddlewareRequest`. Direct
middleware users can call `mw.MeEndpointMiddleware(mode)` and handle its error.
Unknown modes reject construction. The existing `BuildCustomMeEndpointErrorMap`
helper continues to select the legacy 202 envelope.

```go
// Pass these fields alongside the host's existing dependencies.
options := middleware.NewSuiteRequest{
    MeEndpointResponseMode: middleware.MeEndpointAcceptedEmpty,
}
_ = options
```

The selected policy wins over `ErrorMaps` for **AM00-013 only**, without mutating
those maps. It does not change credential selection, refresh, cookie cleanup,
handler admission, permission denials or operational-error classification.
Unknown and mixed-cause failures do not become an empty 202. Explicit host maps
still own other error responses; do not map outages or denials to success.
Keep all maps immutable after constructing middleware.

Clients must treat the selected 202 response as **not authenticated**, not as a
successful user projection. Empty mode requires checking the status before JSON
decoding. Do not switch an existing client's mode without updating its contract
tests; mobile clients expecting 401 can use the explicit unauthorized mode.
The option does not affect `/me/handle`, explicit bearer guards or other routes.

The custom `/me` wrapper sets `Cache-Control: no-store` (replacing inherited
cache directives for this private projection) and adds
`X-Robots-Tag: noindex` to both success and failure responses, preserving existing
robots directives. Place it outside other middleware if their early returns
also need these headers, and do not override them downstream for this private
projection. These response headers do not propagate to an enclosing HTML page.

**SEO is separate from session-probe compatibility.** Public pages should return
their useful public content with 200 even when an optional session probe finds
no visitor session. Private API URLs should not be listed in public sitemaps.
An empty body, `{}`, or an error envelope with a 2xx status does not guarantee
that Google will consider a URL useful/indexable: see
[Google's HTTP status guidance](https://developers.google.com/crawling/docs/troubleshooting/http-status-codes).
Use intentional indexing rules instead of a status-code workaround. Google
must be able to crawl a response to read its `noindex` directive; blocking it
in `robots.txt` prevents that. See
[Google's noindex guidance](https://developers.google.com/search/docs/crawling-indexing/block-indexing).
Neither robots directives nor no-store replace authorization.

A verifier reporting success with an inconsistent identity is an unavailable
verification result (503 with the standard maps), not an anonymous visitor.
This also applies to ordinary authenticated middleware; it cannot trigger the
probe's 202 compatibility response.

## Refresh Cookie Timing

`JWTRequired` and `RateLimitOrActiveJWTRequired` can refresh an expired access
token when the request still includes a valid refresh-token cookie. The
middleware prepares a detached request with the new access token, retries
validation and publishes the verified identity context. Only after the final
cancellation check does it write replacement auth cookies and call the handler.

This keeps cookie state aligned with route authorization. If refresh succeeds
but retry validation rejects the replacement access token, the response does
not commit the new cookies and the client can fall back to its normal session
probe or login flow.

Cookie verification and refresh use an explicit error classification, independent
of host HTTP error-map overrides:

| Failure | Refresh | Cookie changes | Optional public fallback |
| --- | --- | --- | --- |
| Absent/expired access credential | Attempt with supplied refresh credential | Replace only after successful retry | Only if the resulting failure is a known credential rejection |
| Known invalid credential | No new attempt | Clear configured auth/refresh cookies | Rate-limited anonymous flow |
| Current account role/status denial | No | None | No |
| Outage, cancellation, unknown or ambiguous cause | No new attempt | None | No |

The same rules apply to retry failures: a dependency outage during refresh or
revalidation must not become a logout or anonymous downgrade. Request headers
are cloned; a rejected replacement token cannot escape to the caller. A response
can preserve old cookies even though rotation already consumed a credential;
this is not a rollback guarantee. Retry/session-family revocation needs separate
coordination, as described in [live session authority](../README.md#live-session-authority).

Existing credential-selection compatibility is unchanged. Protected cookie
adapters require the refresh cookie and can refresh a missing access credential;
optional adapters treat missing/empty/orphan cookie pairs as public and clear
present stale cookies. They are not interchangeable with the explicit-bearer
adapter. Middleware clears the two configured authentication cookies; the explicit
refresh handler also clears the two non-secret marker cookies on known rejection.
Marker cookies alone never establish authentication.

Anonymous fallback removes the selected auth/refresh cookies, bearer/API headers
and inherited authentication before invoking the rate-limit adapter. Unrelated
cookies remain available. Custom adapters must return an anonymous placeholder
without session or API metadata; authenticated or mixed results are rejected.
Cancellation observed before publication prevents handler dispatch, but cannot
roll back a rotation or headers already committed after the final check.

## Authentication State and Placeholder IDs

### Explicitly selected sessions

`AuthenticateSessionContext(ctx, verifier, credential)` combines
`accessmanager.Service.AuthenticateSession` with the standard context publisher
for hosts that must select credentials themselves. It authenticates exactly once,
requires a complete session identity, rejects API/session mixing and clears
inherited authentication on every failure. It preserves other context values,
cancellation and native error causes. It does not choose browser cookies, refresh
tokens, write HTTP responses or fall back to an anonymous identity.
Publication also clears an inherited explicit-bearer origin marker, even when
the same user/session is published again. Only the bearer transport adapter can
establish a new bearer-only origin; a cookie-selected session cannot inherit it.

```go
trusted, err := middleware.AuthenticateSessionContext(ctx, accessManager, credential)
if err != nil {
	// Classify only session failures; unknown failures must not become logout.
	kind := accessmanager.ClassifySessionError(err)
	_ = kind // map to the host's reviewed error manifest; never serialize err
	return err
}
session := accessmanagerhelpers.AcquireSessionFrom(trusted)
actorID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(trusted)
// Apply current account, audience and resource policy before using actorID.
_ = session
_ = actorID
```

Signed `Audience`, `SigningAlgorithm`, `AuthenticationTime` and session ID are
available through `AcquireSessionFrom`; a second JWT parse is unnecessary.
Scalar claim fields are copied by value; audience slices are copied on
publication/read. The existing
`AcquireUserFrom` API returns a borrowed user pointer, not a deep clone of the
account and its dependencies. Verifiers must return a request-owned observation;
adapters and consumers must treat the published user as read-only.
Missing legacy metadata remains absent. Hosts must enforce their own exact
audience/algorithm policy rather than trusting a client header. Reauthenticate
at sensitive command and replay boundaries: the returned context is a snapshot,
not durable proof of ownership, revocation state or permission.

Nil dependencies/context and a nil successful verifier result return
`ErrSessionVerificationUnavailable`. Invalid/incomplete or mixed identity returns
an authentication error. An empty credential has the standard absent-credential
classification, but this helper itself never refreshes. Use
`ClassifySessionError` for the host's response policy; unknown, joined or
operational failures deny without cookie deletion or anonymous downgrade.

The older `ContextWithAuthentication` remains the compatibility publisher for
preloaded adapters, including anonymous and legacy results. It is not a verifier;
custom session-only flows should use the stricter helper above.

`RateLimitOrActiveJWTRequired` supports authenticated users and rate-limited
anonymous callers on the same route. Anonymous callers do not require a
`StaticPlaceholderUuid`: when it is empty, the context contains no user or actor
and records authentication as false. A configured non-empty placeholder remains
available through `AcquireFrom` for compatibility, but does not prove that the
caller authenticated. Anonymous rate accounting still uses the requestor IP;
it does not depend on a placeholder. Rate-limit failures, dependency outages and
account denials are not bypassed. Identity-restricted optional routes continue
to reject anonymous callers.

Use the access manager context helpers according to the value you need:

| Helper | Best use case |
| --- | --- |
| `AcquireAuthenticatedFrom` | Branching response projection, cache policy, or other behavior on authentication state. |
| `AcquireAuthenticatedUserIDFrom` | Obtaining an actor ID for a user lookup, authorization decision, or attribution on an optional-auth route. It returns an empty string for anonymous callers. |
| `AcquireFrom` | Reading the transmitted ID on strictly authenticated routes, or intentionally accessing the anonymous placeholder for rate-limit bookkeeping. |
| `AcquireUserFrom` | Reusing the user object attached by middleware after the authentication state has been established. |

For example, guard an optional viewer lookup by acquiring only an authenticated
ID:

```go
userID := accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(ctx)
if userID != "" {
	response, err := userService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: userID})
	// Handle the authenticated viewer.
}
```

The authentication check applies to the request actor, not every user ID used
during the request. A persisted author ID read from a content record is domain
data and may still be resolved for display-name enrichment when the viewer is
anonymous.

## Route-policy guard

`NewRoutePolicyGuard(system, service, resourceChecks)` adapts verified context to
the [declarative router](../../router/README.md#declarative-route-policies).
Call `guard.Install(router)` before creating any route group. Installation copies
the check registry and configures both startup validation and request enforcement.
Unknown resource-check names fail registration. Validate the completed route
registry before serving.

The injected `RoutePolicyService` must implement current exact-AND scope and
permission checks and atomic `ConsumeAuthorized` admissions. The
[`accesspolicy` package](../../accesspolicy/README.md) currently defines the
service/store contracts and an opt-in transactional Mongo implementation.
This guard is opt-in; existing host composition is not automatically upgraded.
Do not substitute an allow-all stub or assume that declaring metadata activates
a missing policy backend.

The guard uses `AcquireSessionFrom` or `AcquireAPITokenFrom`, not raw headers,
body IDs, or inferred email roles. Sessions select a `(system, user, userID)`
subject. API credentials select `(system, api_token, tokenID)` and do not union
their owner's user grants into credential grants. Session-only modes reject API
credentials. Active/admin requirements are also checked against the current user
snapshot; user type is classification, never administrator authority.

Enforcement order is identity/account/type → grants → revision syntax → live
resource check → usage admission. A resource check receives the verified actor
ID and request; it can acquire credential metadata from the trusted context when
delegation matters. It must resolve ownership from storage, not a body owner ID.

`RevisionRequired` demands a single non-empty strong `If-Match` entity tag, at
most 256 bytes including quotes. Weak tags, lists, wildcard and multiple headers
are rejected. The domain still compares the actual revision inside its mutation
transaction. The guard does not perform optimistic concurrency by itself.

`UsageMetric` is an enforced request budget, not best-effort telemetry. Each
admitted HTTP attempt gets a new server-generated key; client replay headers
cannot avoid counting. Policy/counter failure denies the request. Admission
rechecks the same required scopes/permissions within its atomic grant boundary.
Domain ownership and consequential mutations still need their own transactional
authority checks, including replay. For business-unit quotas, consume at that
domain boundary rather than charging a route attempt. This API does not currently
return quota-reset metadata in the HTTP error response.

The guard returns original domain errors. `Install(router, overrides...)`
registers the access-policy manifest with the router; supply additional resource
maps or host overrides there, not an `errors.Is` translation in the guard.
Wrapped native denials retain their codes, while joined/unknown/ambiguous failures
produce the router's opaque 503. Overrides are copied at startup and must use
4xx/5xx statuses. Custom `Is` methods are explicit response classifications,
never evidence of authority or safe retries.

Cancellation is checked at entry and after grant, resource and usage adapters;
no later handler is dispatched after an observed cancellation. Usage may already
have committed when cancellation is noticed; this does not undo a charged attempt.
See [explicit bearer sessions](#explicit-bearer-sessions) for credential selection.

## Explicit bearer sessions

`Middleware.BearerSessionRequired` (also `Suite.BearerSession`) accepts exactly one
explicit session bearer and rejects competing API-token headers. It never reads
or refreshes cookies and publishes no raw credential in context. Errors use
canonical manifests and responses are non-cacheable. Current account, admin and
resource checks remain the route or manager's responsibility.

`IsExplicitBearerSession(ctx)` checks a private transport-origin marker bound to
the verified account and session ID. Replacing the identity or publishing a
cookie-only context cannot satisfy it. It is credential-selection evidence, not
permission. The [policy manager](../../accesspolicymanager/README.md) requires this
marker as well as live administrator authority, so ordinary cookie middleware
cannot accidentally enable cookie-authenticated policy changes.
