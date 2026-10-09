# Response middleware composition

`middlewarehelper.NewResponseMiddlewares(ResponseConfig)` constructs a fresh
process-local LRU HTTP cache and returns four Mux middleware functions in this
order: content type, decorated cache, gzip, minification. Supply them to your
router before serving. Construction attaches no routes, opens no connections,
starts no goroutines and preloads no responses.

Every cache setting is explicit. `CacheCapacity` must exceed one; negative
`CacheTTL` is rejected and zero retains the cache library's existing behavior.
Convert host settings units to `time.Duration`. `RefreshKey`, `SkipHTTPHeader`
and `SkipURIPathRegex` retain the underlying cache's meanings; malformed regexes
fail setup. Adapter, regex and cache-client errors preserve their causes and
identify the stage. A nil `CacheObserver` omits that optional vendor hook; a
non-nil function is borrowed and receives actual cache events.

`Minifier` is required and borrowed. `NewResponseMinifier()` registers
`text/html`, `image/svg+xml` and media types ending in `/json` or `+json`. It does
not enable CSS or JavaScript minification. Finish custom format registration
before concurrent requests.

```go
middlewares, err := middlewarehelper.NewResponseMiddlewares(middlewarehelper.ResponseConfig{
    CacheCapacity: 10000,
    CacheTTL: time.Minute,
    RefreshKey: "refresh",
    SkipHTTPHeader: "X-Skip-Cache",
    SkipURIPathRegex: "^/api/",
    Minifier: middlewarehelper.NewResponseMinifier(),
    CacheWrapper: bypassPrivateRoutes, // Your application's mux.MiddlewareFunc decorator.
})
// Check err before passing middlewares to router.NewRouter or spa.NewBootstrap.
```

The host must choose exclusions for private routes. The helper does not install
authentication, authorisation, CSRF, private-path rules or a telemetry SDK.
`CacheWrapper` is an optional function from `mux.MiddlewareFunc` to
`mux.MiddlewareFunc`; it decorates only the cache, once during construction.
Use it to bypass cache while calling the next handler for private requests.
Compression and minification still apply to those requests. A nil decorator
means no wrapper; a decorator returning nil fails construction. Captured policy
and observer state remain caller-owned and must be safe for concurrent serving.
Keep request logging and tracing at their existing outer boundary; this chain
adds neither.

The [router](../../router/README.md) and [SPA bootstrap](../../spa/README.md)
accept the returned slice. The [cache telemetry adapter](../../observability/otelcache/cache.go)
can supply `CacheObserver` without coupling this helper to telemetry ownership.
No dependency upgrade or stored-data conversion is needed to adopt this helper.

```sh
go test -race ./external/middleware/helper
```
