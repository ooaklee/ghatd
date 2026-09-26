package observability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	redis "github.com/go-redis/redis/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestRedisHookRecordsClientSpanAndDurationWithoutCommandArguments verifies privacy-safe Redis spans and metrics.
func TestRedisHookRecordsClientSpanAndDurationWithoutCommandArguments(t *testing.T) {
	hook, spanRecorder, metricReader := newTestRedisHook(t, &redis.Options{
		Network:  "tcp",
		Addr:     "cache.internal:6380",
		Username: "telemetry-must-not-record-this-user",
		Password: "telemetry-must-not-record-this-password",
	})

	type contextKey struct{}
	parentCtx := context.WithValue(context.Background(), contextKey{}, "preserved")
	const (
		secretKey   = "private:rate-limit:user-123"
		secretValue = "private-value"
	)
	command := redis.NewCmd("GET", secretKey, secretValue)

	ctx, err := hook.BeforeProcess(parentCtx, command)
	require.NoError(t, err)
	assert.Equal(t, "preserved", ctx.Value(contextKey{}))
	require.NoError(t, hook.AfterProcess(ctx, command))

	span := requireSingleRedisSpan(t, spanRecorder)
	assert.Equal(t, "redis.get", span.Name())
	assert.Equal(t, trace.SpanKindClient, span.SpanKind())
	assert.Equal(t, codes.Unset, span.Status().Code)
	assertRedisAttributes(t, span.Attributes(), "get", "cache.internal", int64(6380))
	assertTelemetryDoesNotContain(t, span, secretKey, secretValue, "telemetry-must-not-record")

	metrics := collectRedisMetrics(t, metricReader)
	duration := requireRedisHistogram(t, metrics, "db.client.operation.duration")
	require.Len(t, duration.DataPoints, 1)
	assert.Equal(t, uint64(1), duration.DataPoints[0].Count)
	assert.Equal(t, redisDurationBuckets, duration.DataPoints[0].Bounds)
	assertRedisMetricAttributes(t, duration.DataPoints[0].Attributes, "get", "cache.internal", int64(6380))
	assertMetricDataDoesNotContain(t, metrics, secretKey, secretValue, "telemetry-must-not-record")
	assert.Equal(t, int64(0), redisErrorCount(t, metrics))
}

// TestRedisHookTreatsRedisNilAsSuccessfulTelemetry verifies that cache misses do not report failures.
func TestRedisHookTreatsRedisNilAsSuccessfulTelemetry(t *testing.T) {
	hook, spanRecorder, metricReader := newTestRedisHook(t, nil)
	command := redis.NewCmd("get", "private:key")
	command.SetErr(redis.Nil)

	ctx, err := hook.BeforeProcess(context.Background(), command)
	require.NoError(t, err)
	require.NoError(t, hook.AfterProcess(ctx, command))

	span := requireSingleRedisSpan(t, spanRecorder)
	assert.Equal(t, codes.Unset, span.Status().Code)
	assert.Empty(t, span.Events())
	assert.False(t, hasAttribute(span.Attributes(), "error.type"))

	metrics := collectRedisMetrics(t, metricReader)
	assert.Equal(t, uint64(1), redisDurationCount(t, metrics))
	assert.Equal(t, int64(0), redisErrorCount(t, metrics))
}

// TestRedisHookRecordsErrorsWithoutErrorMessages verifies that failures omit sensitive error details.
func TestRedisHookRecordsErrorsWithoutErrorMessages(t *testing.T) {
	hook, spanRecorder, metricReader := newTestRedisHook(t, &redis.Options{Addr: "cache.internal:6379"})
	const secret = "private:key:user-456"
	command := redis.NewCmd("set", secret, "private-value")
	command.SetErr(errors.New("redis failed while handling " + secret))

	ctx, err := hook.BeforeProcess(context.Background(), command)
	require.NoError(t, err)
	require.NoError(t, hook.AfterProcess(ctx, command))

	span := requireSingleRedisSpan(t, spanRecorder)
	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, "Redis operation failed", span.Status().Description)
	assert.Empty(t, span.Events(), "the hook must not record error messages as exception events")
	assert.True(t, hasAttribute(span.Attributes(), "error.type"))
	assertTelemetryDoesNotContain(t, span, secret, "private-value")

	metrics := collectRedisMetrics(t, metricReader)
	assert.Equal(t, uint64(1), redisDurationCount(t, metrics))
	assert.Equal(t, int64(1), redisErrorCount(t, metrics))
	assertMetricDataDoesNotContain(t, metrics, secret, "private-value")
}

// TestRedisHookRecordsPipelineAsOneLowCardinalityOperation verifies bounded telemetry for command batches.
func TestRedisHookRecordsPipelineAsOneLowCardinalityOperation(t *testing.T) {
	hook, spanRecorder, metricReader := newTestRedisHook(t, &redis.Options{Addr: "cache.internal:6379"})
	const secret = "private:pipeline:key"
	missing := redis.NewCmd("get", secret)
	missing.SetErr(redis.Nil)
	failed := redis.NewCmd("set", secret, "private-value")
	failed.SetErr(errors.New("failure mentions " + secret))
	commands := []redis.Cmder{missing, failed, nil}

	ctx, err := hook.BeforeProcessPipeline(context.Background(), commands)
	require.NoError(t, err)
	require.NoError(t, hook.AfterProcessPipeline(ctx, commands))
	// A defensive duplicate callback must not end or measure the same operation twice.
	require.NoError(t, hook.AfterProcessPipeline(ctx, commands))

	span := requireSingleRedisSpan(t, spanRecorder)
	assert.Equal(t, "redis.batch", span.Name())
	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, redisBatchOperation, redisSpanAttribute(t, span.Attributes(), "db.operation.name").AsString())
	assertTelemetryDoesNotContain(t, span, secret, "private-value", "get,set")

	metrics := collectRedisMetrics(t, metricReader)
	assert.Equal(t, uint64(1), redisDurationCount(t, metrics))
	assert.Equal(t, int64(1), redisErrorCount(t, metrics))
	assertMetricDataDoesNotContain(t, metrics, secret, "private-value", "get,set")
}

// TestRedisHooksKeepIndependentContextState verifies that nested hooks do not overwrite one another's state.
func TestRedisHooksKeepIndependentContextState(t *testing.T) {
	firstHook, firstRecorder, _ := newTestRedisHook(t, nil)
	secondHook, secondRecorder, _ := newTestRedisHook(t, nil)
	command := redis.NewCmd("ping")

	ctx, err := firstHook.BeforeProcess(context.Background(), command)
	require.NoError(t, err)
	ctx, err = secondHook.BeforeProcess(ctx, command)
	require.NoError(t, err)
	require.NoError(t, firstHook.AfterProcess(ctx, command))
	require.NoError(t, secondHook.AfterProcess(ctx, command))

	requireSingleRedisSpan(t, firstRecorder)
	requireSingleRedisSpan(t, secondRecorder)
}

// newTestRedisHook constructs a Redis hook with in-memory trace and metric readers.
func newTestRedisHook(
	t *testing.T,
	options *redis.Options,
) (redis.Hook, *tracetest.SpanRecorder, *sdkmetric.ManualReader) {
	t.Helper()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	t.Cleanup(func() {
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})

	hook := NewRedisHook(
		options,
		WithRedisTracerProvider(tracerProvider),
		WithRedisMeterProvider(meterProvider),
	)

	return hook, spanRecorder, metricReader
}

// requireSingleRedisSpan returns the sole completed span or fails the test.
func requireSingleRedisSpan(t *testing.T, recorder *tracetest.SpanRecorder) sdktrace.ReadOnlySpan {
	t.Helper()
	spans := recorder.Ended()
	require.Len(t, spans, 1)

	return spans[0]
}

// assertRedisAttributes checks the expected low-cardinality Redis attributes.
func assertRedisAttributes(
	t *testing.T,
	attributes []attribute.KeyValue,
	operation string,
	serverAddress string,
	serverPort int64,
) {
	t.Helper()
	assert.Equal(t, "redis", redisSpanAttribute(t, attributes, "db.system.name").AsString())
	assert.Equal(t, operation, redisSpanAttribute(t, attributes, "db.operation.name").AsString())
	assert.Equal(t, serverAddress, redisSpanAttribute(t, attributes, "server.address").AsString())
	assert.Equal(t, serverPort, redisSpanAttribute(t, attributes, "server.port").AsInt64())
}

// redisSpanAttribute returns a required Redis span attribute.
func redisSpanAttribute(t *testing.T, attributes []attribute.KeyValue, key attribute.Key) attribute.Value {
	t.Helper()
	for _, spanAttribute := range attributes {
		if spanAttribute.Key == key {
			return spanAttribute.Value
		}
	}

	require.FailNow(t, "span attribute not found", string(key))
	return attribute.Value{}
}

// hasAttribute reports whether an attribute collection contains the given key.
func hasAttribute(attributes []attribute.KeyValue, key attribute.Key) bool {
	for _, spanAttribute := range attributes {
		if spanAttribute.Key == key {
			return true
		}
	}

	return false
}

// assertTelemetryDoesNotContain checks that a span omits each forbidden value.
func assertTelemetryDoesNotContain(t *testing.T, span sdktrace.ReadOnlySpan, forbidden ...string) {
	t.Helper()
	telemetry := span.Name() + " " + span.Status().Description
	for _, spanAttribute := range span.Attributes() {
		telemetry += " " + string(spanAttribute.Key) + "=" + fmt.Sprint(spanAttribute.Value.AsInterface())
	}
	for _, event := range span.Events() {
		telemetry += " " + event.Name
		for _, eventAttribute := range event.Attributes {
			telemetry += " " + string(eventAttribute.Key) + "=" + fmt.Sprint(eventAttribute.Value.AsInterface())
		}
	}
	for _, value := range forbidden {
		assert.NotContains(t, telemetry, value)
	}
}

// collectRedisMetrics collects the current in-memory Redis metrics.
func collectRedisMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))

	return metrics
}

// redisMetric finds a Redis metric by name.
func redisMetric(metrics metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, scopeMetrics := range metrics.ScopeMetrics {
		for metricIndex := range scopeMetrics.Metrics {
			if scopeMetrics.Metrics[metricIndex].Name == name {
				return &scopeMetrics.Metrics[metricIndex]
			}
		}
	}

	return nil
}

// requireRedisHistogram returns the named floating-point histogram or fails the test.
func requireRedisHistogram(
	t *testing.T,
	metrics metricdata.ResourceMetrics,
	name string,
) metricdata.Histogram[float64] {
	t.Helper()
	redisMetric := redisMetric(metrics, name)
	require.NotNil(t, redisMetric, "metric %q not found", name)
	histogram, ok := redisMetric.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "metric %q is not a float64 histogram", name)

	return histogram
}

// redisDurationCount totals recorded Redis duration measurements.
func redisDurationCount(t *testing.T, metrics metricdata.ResourceMetrics) uint64 {
	t.Helper()
	histogram := requireRedisHistogram(t, metrics, "db.client.operation.duration")
	var count uint64
	for _, dataPoint := range histogram.DataPoints {
		count += dataPoint.Count
	}

	return count
}

// redisErrorCount totals recorded Redis operation failures.
func redisErrorCount(t *testing.T, metrics metricdata.ResourceMetrics) int64 {
	t.Helper()
	errorMetric := redisMetric(metrics, "db.client.operation.errors")
	if errorMetric == nil {
		return 0
	}
	sum, ok := errorMetric.Data.(metricdata.Sum[int64])
	require.True(t, ok, "error metric is not an int64 sum")
	var count int64
	for _, dataPoint := range sum.DataPoints {
		count += dataPoint.Value
	}

	return count
}

// assertRedisMetricAttributes checks the expected low-cardinality metric attributes.
func assertRedisMetricAttributes(
	t *testing.T,
	attributes attribute.Set,
	operation string,
	serverAddress string,
	serverPort int64,
) {
	t.Helper()
	dbSystem, ok := attributes.Value("db.system.name")
	require.True(t, ok)
	assert.Equal(t, "redis", dbSystem.AsString())
	dbOperation, ok := attributes.Value("db.operation.name")
	require.True(t, ok)
	assert.Equal(t, operation, dbOperation.AsString())
	address, ok := attributes.Value("server.address")
	require.True(t, ok)
	assert.Equal(t, serverAddress, address.AsString())
	port, ok := attributes.Value("server.port")
	require.True(t, ok)
	assert.Equal(t, serverPort, port.AsInt64())
}

// assertMetricDataDoesNotContain checks that metric data omits each forbidden value.
func assertMetricDataDoesNotContain(t *testing.T, metrics metricdata.ResourceMetrics, forbidden ...string) {
	t.Helper()
	var telemetry strings.Builder
	for _, scopeMetrics := range metrics.ScopeMetrics {
		for _, currentMetric := range scopeMetrics.Metrics {
			telemetry.WriteString(currentMetric.Name)
			switch data := currentMetric.Data.(type) {
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					writeAttributeSet(&telemetry, point.Attributes)
				}
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					writeAttributeSet(&telemetry, point.Attributes)
				}
			}
		}
	}
	for _, value := range forbidden {
		assert.NotContains(t, telemetry.String(), value)
	}
}

// writeAttributeSet appends a stable textual representation of an attribute set.
func writeAttributeSet(target *strings.Builder, attributes attribute.Set) {
	for _, metricAttribute := range attributes.ToSlice() {
		target.WriteString(" ")
		target.WriteString(string(metricAttribute.Key))
		target.WriteString("=")
		target.WriteString(fmt.Sprint(metricAttribute.Value.AsInterface()))
	}
}
