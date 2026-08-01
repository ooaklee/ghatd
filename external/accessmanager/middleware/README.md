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

## Refresh Cookie Timing

`JWTRequired` and `RateLimitOrActiveJWTRequired` can refresh an expired access
token when the request still includes a valid refresh-token cookie. The
middleware updates the request with the new access token, retries validation,
and only then writes replacement auth cookies to the response.

This keeps cookie state aligned with route authorization. If refresh succeeds
but retry validation rejects the replacement access token, the response does
not commit the new cookies and the client can fall back to its normal session
probe or login flow.

## Authentication State and Placeholder IDs

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
