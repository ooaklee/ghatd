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
anonymous callers on the same route. For anonymous traffic it places a
non-empty placeholder ID in the request context and separately records the
authentication state as false. A non-empty value from `AcquireFrom` therefore
does not prove that the caller authenticated.

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

## Explicit bearer sessions

`Middleware.BearerSessionRequired` (also `Suite.BearerSession`) accepts exactly one
explicit session bearer and rejects competing API-token headers. It never reads
or refreshes cookies and publishes no raw credential in context. Errors use
canonical manifests and responses are non-cacheable. Current account, admin and
resource checks remain the route or manager's responsibility.

`IsExplicitBearerSession(ctx)` checks a private transport-origin marker bound to
the verified account and session ID. Replacing the identity or publishing a
cookie-only context cannot satisfy it. It is credential-selection evidence, not
permission. Hosts may require this marker in addition to live administrator
authority to prevent ordinary cookie middleware from enabling bearer-only
management actions.
