package observability

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultOperationScope  = "github.com/ooaklee/ghatd/external/observability/operations"
	defaultOperationPrefix = "ghatd.service.operation"
)

// OperationConfig configures a reusable group of service-operation instruments.
// Scope and MetricPrefix are trusted application constants. A blank scope or
// prefix selects GHATD's defaults, and nil providers select OTel globals.
type OperationConfig struct {
	Scope           string
	MetricPrefix    string
	TracerProvider  trace.TracerProvider
	MeterProvider   metric.MeterProvider
	DurationBuckets []float64
	Errors          *ErrorClassifier
}

// Operations owns reusable instruments for named business operations. It is
// safe to share concurrently; each Start creates an independent Operation.
type Operations struct {
	tracer   trace.Tracer
	count    metric.Int64Counter
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
	errors   *ErrorClassifier
}

// NewOperations creates count, duration, and active instruments with a shared
// prefix and returns any construction error. Count and active intentionally
// have no unit; duration uses seconds and explicit seconds-scale boundaries.
// A nil DurationBuckets selects defaults; an empty non-nil slice is invalid.
func NewOperations(config OperationConfig) (*Operations, error) {
	if config.Scope == "" {
		config.Scope = defaultOperationScope
	}
	if config.MetricPrefix == "" {
		config.MetricPrefix = defaultOperationPrefix
	}
	if !validOperationScope(config.Scope) {
		return nil, errors.New("observability: operation scope must contain 1–255 valid text bytes without controls or surrounding whitespace")
	}
	if !validOperationPrefix(config.MetricPrefix) {
		return nil, errors.New("observability: operation metric prefix must be a 1–246 byte ASCII instrument identifier")
	}
	buckets := config.DurationBuckets
	if buckets == nil {
		buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	}
	if !validOperationBuckets(buckets) {
		return nil, errors.New("observability: operation duration requires 1–128 finite positive increasing boundaries")
	}
	// The metric API receives a private copy so callers cannot mutate the
	// instrument's aggregation configuration after construction.
	buckets = append([]float64(nil), buckets...)
	if config.TracerProvider == nil {
		config.TracerProvider = otel.GetTracerProvider()
	}
	if config.MeterProvider == nil {
		config.MeterProvider = otel.GetMeterProvider()
	}
	meter := config.MeterProvider.Meter(config.Scope)
	count, err := meter.Int64Counter(config.MetricPrefix+".count",
		metric.WithDescription("Number of completed application service operations."))
	if err != nil {
		return nil, fmt.Errorf("observability: create operation count: %w", err)
	}
	duration, err := meter.Float64Histogram(config.MetricPrefix+".duration",
		metric.WithDescription("Duration of application service operations."),
		metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(buckets...))
	if err != nil {
		return nil, fmt.Errorf("observability: create operation duration: %w", err)
	}
	active, err := meter.Int64UpDownCounter(config.MetricPrefix+".active",
		metric.WithDescription("Number of application service operations currently executing."))
	if err != nil {
		return nil, fmt.Errorf("observability: create active operations: %w", err)
	}
	return &Operations{
		tracer: config.TracerProvider.Tracer(config.Scope), count: count,
		duration: duration, active: active, errors: config.Errors,
	}, nil
}

// Operation tracks one invocation and completes at most once. Use a named error
// result and defer operation.Finish(&err) directly to observe returned errors and
// panics. End is available when the caller manages completion explicitly.
type Operation struct {
	operations *Operations
	ctx        context.Context
	span       trace.Span
	name       string
	startedAt  time.Time
	once       sync.Once
}

// Start records an active operation, starts an internal span, and binds the
// context logger to that child span. Names must be static application constants,
// never identifiers or request values; this API does not enforce a name registry.
// A nil Operations pointer returns an inert operation and preserves the context.
func (operations *Operations) Start(ctx context.Context, name string) (context.Context, *Operation) {
	if ctx == nil {
		ctx = context.Background()
	}
	if operations == nil {
		return ctx, &Operation{}
	}
	startedAt := time.Now()
	ctx, span := operations.tracer.Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("operation", name)))
	ctx = ghatdlogger.TransitWith(ctx, WithTraceContext(ctx, ghatdlogger.AcquireFrom(ctx)))
	operations.active.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", name)))
	return ctx, &Operation{
		operations: operations, ctx: ctx, span: span, name: name, startedAt: startedAt,
	}
}

// End records the error's classification and ends the operation once. Error
// messages and panic payloads are never exported. Use Finish to detect a panic;
// passing a panic value as an ordinary error to End cannot identify its origin.
func (operation *Operation) End(err error) {
	if operation == nil || operation.operations == nil {
		return
	}
	operation.once.Do(func() {
		operation.complete(operation.operations.errors.Classify(err))
	})
}

// Finish must be deferred directly, as defer operation.Finish(&err). It records
// a recovered panic as panic, then rethrows the identical value. Do not wrap this
// call inside another deferred closure: recover would no longer see that panic.
// A nil error pointer represents successful normal completion. Nil or inert
// operations still preserve the original panic.
func (operation *Operation) Finish(err *error) {
	if recovered := recover(); recovered != nil {
		if operation != nil && operation.operations != nil {
			operation.once.Do(func() {
				operation.complete(classifyOperation("panic", OutcomePanic))
			})
		}
		panic(recovered)
	}
	var result error
	if err != nil {
		result = *err
	}
	operation.End(result)
}

// complete runs only inside the operation's once guard.
func (operation *Operation) complete(classification Classification) {
	attributes := []attribute.KeyValue{
		attribute.String("operation", operation.name),
		attribute.String("outcome", string(classification.Outcome)),
	}
	operation.span.SetAttributes(attributes...)
	if classification.Code != "" {
		operation.span.SetAttributes(
			attribute.String("error.type", classification.Code),
			attribute.String("error.category", classification.Category))
	}
	switch classification.Outcome {
	case OutcomeError:
		operation.span.SetStatus(codes.Error, "operation failed")
	case OutcomeTimeout:
		operation.span.SetStatus(codes.Error, "operation timed out")
	case OutcomePanic:
		operation.span.SetStatus(codes.Error, "operation panicked")
	}
	options := metric.WithAttributes(attributes...)
	operation.operations.count.Add(operation.ctx, 1, options)
	operation.operations.duration.Record(operation.ctx, time.Since(operation.startedAt).Seconds(), options)
	operation.operations.active.Add(operation.ctx, -1,
		metric.WithAttributes(attribute.String("operation", operation.name)))
	operation.span.End()
}

func validOperationScope(scope string) bool {
	if len(scope) == 0 || len(scope) > 255 || !utf8.ValidString(scope) || strings.TrimSpace(scope) != scope {
		return false
	}
	for _, char := range scope {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func validOperationPrefix(prefix string) bool {
	if len(prefix) == 0 || len(prefix) > 246 || !operationIdentifierLetter(prefix[0]) {
		return false
	}
	for index := 1; index < len(prefix); index++ {
		char := prefix[index]
		if !operationIdentifierLetter(char) && !(char >= '0' && char <= '9') && char != '.' && char != '_' && char != '-' && char != '/' {
			return false
		}
	}
	return true
}

func validOperationBuckets(buckets []float64) bool {
	if len(buckets) == 0 || len(buckets) > 128 {
		return false
	}
	previous := 0.0
	for _, boundary := range buckets {
		if math.IsNaN(boundary) || math.IsInf(boundary, 0) || boundary <= previous {
			return false
		}
		previous = boundary
	}
	return true
}
