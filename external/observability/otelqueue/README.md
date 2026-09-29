# Consumer job observability

This package supplies broker-independent consumer spans, finite settlement
metrics and orderly process shutdown. The host keeps message payloads, queue
connections, acknowledgement, retry and business decisions.

```go
worker, err := otelqueue.StartRuntime(ctx, otelqueue.RuntimeConfig{
    Runtime: observability.RuntimeConfig{
        Telemetry: observability.Config{ServiceName: "example-worker"},
        Logger: appLogger, ShutdownTimeout: 15 * time.Second,
    },
    Queue: otelqueue.Config{
        Operations:     []string{"queue.document.consume"},
        CountMetric:    "example.worker.queue.count",
        DurationMetric: "example.worker.queue.duration",
    },
})
if err != nil { return err }
defer worker.ShutdownResources(ctx, stopConsuming, cleanupGroup.Run)

// Inside a broker callback:
job, jobCtx := worker.BeginJob("queue.document.consume")
// Execute business work with jobCtx, then acknowledge/reject through the broker.
// Pass the actual result to Settle after that acknowledgement attempt finishes.
job.Settle(otelqueue.OutcomeAcked, nil)
```

For an existing runtime, construct `otelqueue.New(Config{TracerProvider: ...,
MeterProvider: ...})` and call `observer.Begin(ctx, operation)` instead. Do not
start a competing runtime for each queue or job. `StartRuntime` binds the
observer to its own providers even if global providers later change.

Operations are a copied startup-only list of at most 64 names (1–64 lowercase
ASCII letters, digits, `.`, `_`, or `-`, starting with a letter). Unknown names
become `queue.other.consume`. Outcomes are `acked`, `rejected`, `requeued`,
`malformed-payload`, `ack-failed`, `reject-failed`, or `unknown`. The first
`Settle` wins, even when called concurrently or again from a deferred fallback.
An error marks the span failed without reading its message. A caller that
aborts a job should still settle it, normally through a deferred fallback.

The counter defaults to `ghatd.queue.count` (`{job}`), the histogram to
`ghatd.queue.duration` (`s`). Set existing metric names to keep dashboards
stable. Both use only `queue.operation` and `queue.outcome`. Duration includes
settlement when `Settle` is called after the broker operation. Histogram
boundaries in seconds are 0.1, 1, 5, 15, 30, 60, 120, 300 and 600.

Each job starts a new consumer trace and carries a correlated logger. There is
no implicit producer parent or payload-based extraction. Adding distributed
queue parentage requires an explicitly versioned carrier and retry policy.

Process cancellation initiates drain without cancelling active jobs. The stop
callback must promptly stop admission and return a completion channel; a nil
channel means there is nothing to wait for. Drain has a bounded budget, then
remaining jobs are cancelled. Cleanup and telemetry each get fresh budgets.
Callbacks must cooperate with cancellation. Jobs that outlive drain can miss
final telemetry or acknowledgement; broker redelivery can repeat side effects.
Plan the process termination grace period for all three phases and use
idempotent business operations where required. The package does not promise
exactly-once delivery or change exporter/sampler settings.
