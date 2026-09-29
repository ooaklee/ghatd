# Cache decision metrics

Use this adapter to measure actual cache decisions instead of inferring cache
hits from successful HTTP responses. The package never reads keys, URLs,
headers or cached values. A request provides only its trace context.

```go
metrics, err := otelcache.New(otelcache.Config{
    MeterProvider: runtime.SDK().MeterProvider(),
    MetricName:    "example.cache.event.count",
})
if err != nil { return err }
// Add to the options passed to the existing HTTP cache constructor:
observerOption := cache.ClientWithObserver(metrics.HTTPResponseCacheObserver())
```

`MetricName` defaults to `ghatd.cache.event.count`. Set the prior service metric
name when adopting this package to preserve queries and alerts. Unit is
`{event}`; attributes remain `cache` and `event`. One lookup can emit both a
miss and a store, so stores must not enter a cache-hit ratio denominator.
Skipped requests produce no cache event.

For another cache, configure static `Names` and call
`metrics.Record(ctx, "token", otelcache.Hit)` at its actual decision points.
Names default to `http-response`; an explicit list replaces that default.
There are at most 64 configured names, each 1–64 lowercase ASCII letters,
digits, `.`, `_`, or `-`, beginning with a letter. The list is copied during
construction. Unknown names and events are dropped. Events are `Hit`, `Miss`,
`Stale`, `Refresh`, `Store`, and `Purge`. Nil/zero observers are harmless.

Use static configuration, never request values, for names and metric identity.
Supply an explicit meter provider for one runtime's ownership; otherwise the
current global provider is captured at construction. The observer never starts
or stops providers. See [shared ownership decisions](../../../docs/adr/adr021-shared-observability-adapters.md).
