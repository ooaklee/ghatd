package observability

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestOperationsRecordOutcomesWithoutErrorDetails(t *testing.T) {
	const private = "synthetic-private-operation-error"
	rejected := errors.New(private)
	dependency := errors.New(private + "-dependency")
	classifier, err := NewErrorClassifier(
		ErrorRule{Err: rejected, Code: "APP-001", Outcome: OutcomeRejected},
		ErrorRule{Err: dependency, Code: "APP-002", Outcome: OutcomeError})
	require.NoError(t, err)
	for _, test := range []struct {
		name    string
		err     error
		outcome OperationOutcome
		code    string
		status  codes.Code
	}{
		{"success", nil, OutcomeSuccess, "", codes.Unset},
		{"rejection", fmt.Errorf(private+": %w", rejected), OutcomeRejected, "APP-001", codes.Unset},
		{"cancelled", fmt.Errorf(private+": %w", context.Canceled), OutcomeCancelled, "cancelled", codes.Unset},
		{"timeout", fmt.Errorf(private+": %w", context.DeadlineExceeded), OutcomeTimeout, "timeout", codes.Error},
		{"internal", dependency, OutcomeError, "APP-002", codes.Error},
		{"unknown", errors.New(private), OutcomeError, "internal", codes.Error},
		{"unprintable", &unprintableOperationError{}, OutcomeError, "internal", codes.Error},
	} {
		t.Run(test.name, func(t *testing.T) {
			operations, spans, reader, _ := newTestOperations(t, OperationConfig{Errors: classifier})
			_, operation := operations.Start(context.Background(), "process-order")
			assertOperationActive(t, reader, 1)
			gotErr := func() (err error) {
				defer operation.Finish(&err)
				return test.err
			}()
			assert.True(t, gotErr == test.err, "completion must preserve the returned error identity")
			operation.End(errors.New("ignored duplicate"))
			operation.Finish(nil)
			require.Len(t, spans.Ended(), 1)
			span := spans.Ended()[0]
			assert.Equal(t, "process-order", span.Name())
			assert.Equal(t, trace.SpanKindInternal, span.SpanKind())
			assert.Equal(t, test.status, span.Status().Code)
			attrs := attribute.NewSet(span.Attributes()...)
			assertAttribute(t, attrs, "operation", "process-order")
			assertAttribute(t, attrs, "outcome", string(test.outcome))
			if test.code == "" {
				assert.False(t, attrs.HasValue("error.type"))
				assert.False(t, attrs.HasValue("error.category"))
			} else {
				assertAttribute(t, attrs, "error.type", test.code)
				assertAttribute(t, attrs, "error.category", string(test.outcome))
			}
			assertSpanExcludes(t, span, private)
			assert.Empty(t, span.Events())
			metrics := collectOperationMetrics(t, reader)
			assertOperationCompletion(t, metrics, "process-order", test.outcome, 1)
			assertMetricDataDoesNotContain(t, metrics, private)
			assertOperationActive(t, reader, 0)
		})
	}
}

func TestOperationFinishRecordsPanicAndRethrowsOriginalValue(t *testing.T) {
	for _, panicValue := range []any{
		errors.New("synthetic-private-panic-error"),
		"synthetic-private-panic-string",
		map[string]string{"payload": "synthetic-private-panic-map"},
		&unprintableOperationError{},
	} {
		operations, spans, reader, _ := newTestOperations(t, OperationConfig{})
		_, operation := operations.Start(context.Background(), "process-order")
		// This method-value callback is the contract used by batch adapters.
		finish := operation.Finish
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			_ = func() (err error) {
				defer finish(&err)
				panic(panicValue)
			}()
		}()
		assert.Equal(t, panicValue, recovered)
		if original, ok := panicValue.(error); ok {
			assert.True(t, recovered == original, "panic error identity must survive")
		}
		operation.End(nil)
		require.Len(t, spans.Ended(), 1)
		span := spans.Ended()[0]
		assert.Equal(t, codes.Error, span.Status().Code)
		assert.Equal(t, "operation panicked", span.Status().Description)
		attrs := attribute.NewSet(span.Attributes()...)
		assertAttribute(t, attrs, "outcome", "panic")
		assertAttribute(t, attrs, "error.type", "panic")
		assertAttribute(t, attrs, "error.category", "panic")
		assertSpanExcludes(t, span, "synthetic-private", "unprintableOperationError")
		metrics := collectOperationMetrics(t, reader)
		assertOperationCompletion(t, metrics, "process-order", OutcomePanic, 1)
		assertMetricDataDoesNotContain(t, metrics, "synthetic-private")
		assertOperationActive(t, reader, 0)
	}
}

func TestOperationCompletionIsConcurrentAndOnceOnly(t *testing.T) {
	operations, spans, reader, _ := newTestOperations(t, OperationConfig{})
	_, operation := operations.Start(context.Background(), "process-order")
	var calls sync.WaitGroup
	for index := range 64 {
		calls.Go(func() {
			if index%2 == 0 {
				operation.End(nil)
			} else {
				var err error
				defer operation.Finish(&err)
			}
		})
	}
	calls.Wait()
	require.Len(t, spans.Ended(), 1)
	assertOperationCompletion(t, collectOperationMetrics(t, reader), "process-order", OutcomeSuccess, 1)
	assertOperationActive(t, reader, 0)
	// Completion is first-wins, but Finish must still propagate later panics.
	wantPanic := errors.New("private later panic")
	assert.PanicsWithValue(t, wantPanic, func() {
		defer operation.Finish(nil)
		panic(wantPanic)
	})
	require.Len(t, spans.Ended(), 1)
}

func TestOperationsRebindContextLoggerAndKeepParentage(t *testing.T) {
	operations, spans, reader, traces := newTestOperations(t, OperationConfig{})
	exporter := &recordingLogExporter{}
	logs := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	t.Cleanup(func() { require.NoError(t, logs.Shutdown(context.Background())) })
	baseCore, local := observer.New(zap.InfoLevel)
	baseLogger := TeeLogger(zap.New(baseCore), logs)
	ctx, parent := traces.Tracer("test-parent").Start(context.Background(), "parent")
	ctx = ghatdlogger.TransitWith(ctx, WithTraceContext(ctx, baseLogger))
	type contextKey struct{}
	ctx = context.WithValue(ctx, contextKey{}, "preserved")
	childCtx, child := operations.Start(ctx, "process-order")
	childSpanContext := trace.SpanContextFromContext(childCtx)
	assert.Equal(t, "preserved", childCtx.Value(contextKey{}))
	assert.NotEqual(t, parent.SpanContext().SpanID(), childSpanContext.SpanID())
	assert.Equal(t, parent.SpanContext().TraceID(), childSpanContext.TraceID())
	ghatdlogger.AcquireFrom(childCtx).Info("operation-progress")
	nestedCtx, nested := operations.Start(childCtx, "validate-order")
	ghatdlogger.AcquireFrom(nestedCtx).Info("nested-operation-progress")
	assertOperationActive(t, reader, 2)
	nested.End(nil)
	child.End(nil)
	ghatdlogger.AcquireFrom(ctx).Info("parent-progress")
	parent.End()
	ended := spans.Ended()
	require.Len(t, ended, 3)
	assert.Equal(t, childSpanContext.SpanID(), ended[0].Parent().SpanID())
	assert.Equal(t, parent.SpanContext().SpanID(), ended[1].Parent().SpanID())
	records := exporter.Records()
	require.Len(t, records, 3)
	assert.Equal(t, childSpanContext.SpanID(), records[0].SpanID())
	assert.Equal(t, trace.SpanContextFromContext(nestedCtx).SpanID(), records[1].SpanID())
	assert.Equal(t, parent.SpanContext().SpanID(), records[2].SpanID())
	assert.Equal(t, childSpanContext.TraceID(), records[0].TraceID())
	require.Len(t, local.All(), 3)
	assert.Equal(t, childSpanContext.SpanID().String(), local.All()[0].ContextMap()[SpanIDKey])
	assertOperationActive(t, reader, 0)
}

func TestOperationsKeepSuccessfulReturnDespiteCancelledContext(t *testing.T) {
	operations, spans, reader, _ := newTestOperations(t, OperationConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	_, operation := operations.Start(ctx, "process-order")
	cancel()
	operation.End(nil)
	require.Len(t, spans.Ended(), 1)
	assert.Equal(t, codes.Unset, spans.Ended()[0].Status().Code)
	assertOperationCompletion(t, collectOperationMetrics(t, reader), "process-order", OutcomeSuccess, 1)
	assertOperationActive(t, reader, 0)
}

func TestOperationsExportSecondsBucketsAndCompatibleNames(t *testing.T) {
	operations, _, reader, _ := newTestOperations(t, OperationConfig{
		Scope: "example.test/service/internal/services", MetricPrefix: "example.service.operation",
	})
	attrs := metric.WithAttributes(attribute.String("operation", "process-order"), attribute.String("outcome", "success"))
	// Feed deterministic measurements through the actual constructed histogram.
	for _, seconds := range []float64{0.01, 0.5, 4} {
		operations.duration.Record(context.Background(), seconds, attrs)
	}
	metrics := collectOperationMetrics(t, reader)
	require.Len(t, metrics.ScopeMetrics, 1)
	assert.Equal(t, "example.test/service/internal/services", metrics.ScopeMetrics[0].Scope.Name)
	duration := operationMetric(t, metrics, "example.service.operation.duration")
	assert.Equal(t, "s", duration.Unit)
	histogram := duration.Data.(metricdata.Histogram[float64])
	require.Len(t, histogram.DataPoints, 1)
	point := histogram.DataPoints[0]
	assert.Equal(t, []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}, point.Bounds)
	assert.Equal(t, []uint64{0, 1, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 0}, point.BucketCounts)
	assert.Equal(t, uint64(3), point.Count)
	assert.InDelta(t, 4.51, point.Sum, 1e-12)
	_, operation := operations.Start(context.Background(), "process-order")
	operation.End(nil)
	metrics = collectOperationMetrics(t, reader)
	assert.Empty(t, operationMetric(t, metrics, "example.service.operation.count").Unit)
	assert.Empty(t, operationMetric(t, metrics, "example.service.operation.active").Unit)
}

func TestOperationsSnapshotCustomBuckets(t *testing.T) {
	buckets := []float64{0.01, 0.5, 5}
	operations, _, reader, _ := newTestOperations(t, OperationConfig{DurationBuckets: buckets})
	buckets[0] = 100
	_, operation := operations.Start(nil, "process-order")
	operation.End(nil)
	duration := operationMetric(t, collectOperationMetrics(t, reader), defaultOperationPrefix+".duration")
	assert.Equal(t, []float64{0.01, 0.5, 5}, duration.Data.(metricdata.Histogram[float64]).DataPoints[0].Bounds)
}

func TestOperationsNilReceiverPreservesContextAndPanic(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "preserved")
	var operations *Operations
	gotCtx, inert := operations.Start(ctx, "process-order")
	assert.Same(t, ctx, gotCtx)
	background, _ := operations.Start(nil, "process-order")
	assert.NotNil(t, background)
	for _, operation := range []*Operation{nil, inert} {
		operation.End(&unprintableOperationError{})
		operation.Finish(nil)
		want := errors.New("synthetic-private-panic")
		assert.PanicsWithValue(t, want, func() {
			defer operation.Finish(nil)
			panic(want)
		})
	}
}

func TestNewOperationsRejectsInvalidConfigurationWithoutEcho(t *testing.T) {
	for _, config := range []OperationConfig{
		{Scope: "synthetic-private-input\n"}, {Scope: " scope "}, {Scope: "invalid\xff"}, {Scope: strings.Repeat("s", 256)},
		{MetricPrefix: "synthetic-private-input\n"}, {MetricPrefix: "9prefix"}, {MetricPrefix: "unicode-ſ"}, {MetricPrefix: strings.Repeat("m", 247)},
		{DurationBuckets: []float64{}}, {DurationBuckets: []float64{0}}, {DurationBuckets: []float64{-1}},
		{DurationBuckets: []float64{1, 1}}, {DurationBuckets: []float64{2, 1}},
		{DurationBuckets: []float64{math.NaN()}}, {DurationBuckets: []float64{math.Inf(1)}},
		{DurationBuckets: make([]float64, 129)},
	} {
		operations, err := NewOperations(config)
		require.Error(t, err)
		assert.Nil(t, operations)
		assert.NotContains(t, err.Error(), "synthetic-private-input")
	}
	// Global providers are a supported default even when no SDK was installed.
	operations, err := NewOperations(OperationConfig{})
	require.NoError(t, err)
	require.NotNil(t, operations)
}

func TestNewOperationsReturnsEachInstrumentConstructionError(t *testing.T) {
	for _, failing := range []string{"count", "duration", "active"} {
		t.Run(failing, func(t *testing.T) {
			wantErr := errors.New("instrument constructor failed")
			provider := metricnoop.NewMeterProvider()
			meter := failingOperationMeter{Meter: provider.Meter("test"), failing: failing, err: wantErr}
			operations, err := NewOperations(OperationConfig{
				MeterProvider: operationTestMeterProvider{MeterProvider: provider, meter: meter},
			})
			require.ErrorIs(t, err, wantErr)
			assert.Nil(t, operations)
		})
	}
}

type operationTestMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (provider operationTestMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return provider.meter
}

type failingOperationMeter struct {
	metric.Meter
	failing string
	err     error
}

func (meter failingOperationMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if meter.failing == "count" {
		return nil, meter.err
	}
	return meter.Meter.Int64Counter(name, options...)
}

func (meter failingOperationMeter) Float64Histogram(name string, options ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if meter.failing == "duration" {
		return nil, meter.err
	}
	return meter.Meter.Float64Histogram(name, options...)
}

func (meter failingOperationMeter) Int64UpDownCounter(name string, options ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	if meter.failing == "active" {
		return nil, meter.err
	}
	return meter.Meter.Int64UpDownCounter(name, options...)
}

func newTestOperations(t *testing.T, config OperationConfig) (*Operations, *tracetest.SpanRecorder, *sdkmetric.ManualReader, *sdktrace.TracerProvider) {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, traces.Shutdown(context.Background()))
		require.NoError(t, meters.Shutdown(context.Background()))
	})
	config.TracerProvider, config.MeterProvider = traces, meters
	operations, err := NewOperations(config)
	require.NoError(t, err)
	return operations, spans, reader, traces
}

func collectOperationMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	return metrics
}

func operationMetric(t *testing.T, metrics metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name == name {
				return candidate
			}
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}

func assertOperationActive(t *testing.T, reader *sdkmetric.ManualReader, want int64) {
	t.Helper()
	active := operationMetric(t, collectOperationMetrics(t, reader), defaultOperationPrefix+".active").Data.(metricdata.Sum[int64])
	assert.False(t, active.IsMonotonic)
	var got int64
	for _, point := range active.DataPoints {
		got += point.Value
		assert.Equal(t, 1, point.Attributes.Len(), "active series must have only operation, never outcome")
		assert.True(t, point.Attributes.HasValue("operation"))
	}
	assert.Equal(t, want, got)
}

func assertOperationCompletion(t *testing.T, metrics metricdata.ResourceMetrics, name string, outcome OperationOutcome, count int64) {
	t.Helper()
	counter := operationMetric(t, metrics, defaultOperationPrefix+".count").Data.(metricdata.Sum[int64])
	assert.True(t, counter.IsMonotonic)
	require.Len(t, counter.DataPoints, 1)
	assert.Equal(t, count, counter.DataPoints[0].Value)
	assert.Equal(t, 2, counter.DataPoints[0].Attributes.Len())
	assertAttribute(t, counter.DataPoints[0].Attributes, "operation", name)
	assertAttribute(t, counter.DataPoints[0].Attributes, "outcome", string(outcome))
	duration := operationMetric(t, metrics, defaultOperationPrefix+".duration").Data.(metricdata.Histogram[float64])
	require.Len(t, duration.DataPoints, 1)
	assert.Equal(t, uint64(count), duration.DataPoints[0].Count)
	assert.Greater(t, duration.DataPoints[0].Sum, 0.0)
	assert.Equal(t, counter.DataPoints[0].Attributes, duration.DataPoints[0].Attributes)
}

func assertAttribute(t *testing.T, attributes attribute.Set, key attribute.Key, want string) {
	t.Helper()
	value, ok := attributes.Value(key)
	require.True(t, ok, "missing attribute %s", key)
	assert.Equal(t, want, value.AsString(), "attribute %s", key)
}
