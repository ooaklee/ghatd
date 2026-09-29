// Package otelqueue observes consumer jobs without depending on a queue broker
// or reading message payloads. Hosts own admission, acknowledgement and retry.
package otelqueue

import (
	"context"
	"errors"
	"regexp"
	"sync/atomic"
	"time"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	OutcomeAcked         = "acked"
	OutcomeRejected      = "rejected"
	OutcomeRequeued      = "requeued"
	OutcomeMalformed     = "malformed-payload"
	OutcomeAckFailed     = "ack-failed"
	OutcomeRejectFailed  = "reject-failed"
	OutcomeUnknown       = "unknown"
	OtherOperation       = "queue.other.consume"
	instrumentationScope = "github.com/ooaklee/ghatd/external/observability/otelqueue"
)

// Config is startup-only policy. Operations is a copied, finite vocabulary of
// logical operation names, not queue URLs, payloads or request-derived values.
// Unknown operations collapse to OtherOperation. Metric names can preserve a
// host's existing dashboards; defaults are ghatd.queue.count/duration.
type Config struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Operations     []string
	CountMetric    string
	DurationMetric string
}

type Observer struct {
	tracer     trace.Tracer
	count      metric.Int64Counter
	duration   metric.Float64Histogram
	operations map[string]struct{}
}

var operationName = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

func New(config Config) (*Observer, error) {
	if len(config.Operations) > 64 {
		return nil, errors.New("otelqueue: at most 64 operations are allowed")
	}
	operations := make(map[string]struct{}, len(config.Operations))
	for _, operation := range config.Operations {
		if !operationName.MatchString(operation) {
			return nil, errors.New("otelqueue: invalid operation name")
		}
		operations[operation] = struct{}{}
	}
	if config.TracerProvider == nil {
		config.TracerProvider = otel.GetTracerProvider()
	}
	if config.MeterProvider == nil {
		config.MeterProvider = otel.GetMeterProvider()
	}
	if config.CountMetric == "" {
		config.CountMetric = "ghatd.queue.count"
	}
	if config.DurationMetric == "" {
		config.DurationMetric = "ghatd.queue.duration"
	}
	meter := config.MeterProvider.Meter(instrumentationScope)
	count, err := meter.Int64Counter(config.CountMetric, metric.WithUnit("{job}"))
	if err != nil {
		return nil, errors.New("otelqueue: cannot create job counter")
	}
	duration, err := meter.Float64Histogram(config.DurationMetric, metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 1, 5, 15, 30, 60, 120, 300, 600))
	if err != nil {
		return nil, errors.New("otelqueue: cannot create job duration histogram")
	}
	return &Observer{tracer: config.TracerProvider.Tracer(instrumentationScope), count: count, duration: duration, operations: operations}, nil
}

type Job struct {
	observer  *Observer
	span      trace.Span
	ctx       context.Context
	operation string
	startedAt time.Time
	settled   atomic.Bool
}

// Begin starts an independent consumer trace and attaches a correlated logger.
// There is deliberately no automatic parent extraction from unversioned queue
// payloads. It never mutates the root logger or a process-global provider.
func (o *Observer) Begin(ctx context.Context, operation string) (*Job, context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := o.operations[operation]; !ok {
		operation = OtherOperation
	}
	ctx, span := o.tracer.Start(ctx, operation, trace.WithNewRoot(), trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attribute.String("queue.operation", operation)))
	ctx = ghatdlogger.TransitWith(ctx, observability.WithTraceContext(ctx, ghatdlogger.AcquireFrom(ctx)))
	return &Job{observer: o, span: span, ctx: ctx, operation: operation, startedAt: time.Now()}, ctx
}

// Settle records the first outcome only, including across concurrent calls.
// Call after acknowledgement/rejection so duration includes settlement. The
// error is only a failure signal: its text and arbitrary fields are never read.
func (job *Job) Settle(outcome string, err error) {
	if job == nil || !job.settled.CompareAndSwap(false, true) {
		return
	}
	switch outcome {
	case OutcomeAcked, OutcomeRejected, OutcomeRequeued, OutcomeMalformed, OutcomeAckFailed, OutcomeRejectFailed:
	default:
		outcome = OutcomeUnknown
	}
	attrs := []attribute.KeyValue{attribute.String("queue.operation", job.operation), attribute.String("queue.outcome", outcome)}
	job.span.SetAttributes(attrs...)
	if err != nil {
		job.span.SetStatus(codes.Error, "queue job failed")
	}
	options := metric.WithAttributes(attrs...)
	job.observer.count.Add(job.ctx, 1, options)
	job.observer.duration.Record(job.ctx, time.Since(job.startedAt).Seconds(), options)
	job.span.End()
}
