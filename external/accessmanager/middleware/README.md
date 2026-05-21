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
