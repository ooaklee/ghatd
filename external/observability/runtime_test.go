package observability

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	collectorlog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/protobuf/proto"
)

func TestRuntimeShutdownUsesFreshDetachedDeadlineAndPreservesValues(t *testing.T) {
	type contextKey struct{}
	const timeout = 100 * time.Millisecond
	parent, cancel := context.WithDeadline(context.WithValue(context.Background(), contextKey{}, "preserved"), time.Now().Add(-time.Hour))
	cancel()
	wantErr := errors.New("shutdown failure")
	var calls atomic.Int32
	runtime := &Runtime{ctx: parent, shutdownTimeout: timeout, sdk: &SDK{shutdown: []func(context.Context) error{
		func(ctx context.Context) error {
			calls.Add(1)
			assert.NoError(t, ctx.Err(), "an expired action context must not cancel flushing")
			assert.Equal(t, "preserved", ctx.Value(contextKey{}))
			deadline, ok := ctx.Deadline()
			assert.True(t, ok)
			assert.InDelta(t, timeout.Seconds(), time.Until(deadline).Seconds(), 0.05)
			return wantErr
		},
	}}}
	var wait sync.WaitGroup
	for range 16 {
		wait.Go(func() { assert.ErrorIs(t, runtime.Shutdown(nil), wantErr) })
	}
	wait.Wait()
	assert.EqualValues(t, 1, calls.Load())
}

func TestRuntimeShutdownPassesItsBoundedTimeoutToExporters(t *testing.T) {
	runtime := &Runtime{shutdownTimeout: 20 * time.Millisecond, sdk: &SDK{shutdown: []func(context.Context) error{
		func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}}}
	start := time.Now()
	require.ErrorIs(t, runtime.Shutdown(context.Background()), context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestRuntimeContextLoggerAndSafeDefaults(t *testing.T) {
	clearRuntimeOTELTestEnvironment(t)
	disableExporters(t)
	restoreRuntimeTestGlobals(t)
	core, local := observer.New(zap.InfoLevel)
	baseLogger := zap.New(core)
	ctx := ghatdlogger.TransitWith(context.Background(), baseLogger)
	runtime, err := StartRuntime(ctx, RuntimeConfig{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(nil)) })
	assert.Same(t, runtime, RuntimeFromContext(runtime.Context()))
	assert.Same(t, runtime.Logger(), ghatdlogger.AcquireFrom(runtime.Context()))
	assert.NotNil(t, runtime.SDK())
	assert.Equal(t, 15*time.Second, runtime.shutdownTimeout)
	ghatdlogger.AcquireFrom(runtime.Context()).Info("runtime-ready")
	require.Len(t, local.All(), 1)
	assert.Nil(t, RuntimeFromContext(ctx), "the caller's context stays unchanged")
	var absent *Runtime
	assert.Nil(t, absent.SDK())
	assert.Nil(t, RuntimeFromContext(nil))
	assert.NotNil(t, absent.Context())
	assert.NotNil(t, absent.Logger())
	assert.NoError(t, absent.Shutdown(nil))
	invalid, err := StartRuntime(nil, RuntimeConfig{ShutdownTimeout: -time.Second})
	require.Error(t, err)
	assert.Nil(t, invalid)
}

func TestRuntimeFlushesAllSignalsToActualOTLPReceiverAfterCancellation(t *testing.T) {
	restoreRuntimeTestGlobals(t)
	var mu sync.Mutex
	received := map[string][][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		assert.NoError(t, err)
		assert.Equal(t, "application/x-protobuf", request.Header.Get("Content-Type"))
		mu.Lock()
		received[request.URL.Path] = append(received[request.URL.Path], body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	configureRuntimeOTLPTest(t, server.URL)
	core, _ := observer.New(zap.InfoLevel)
	runtime, err := StartRuntime(context.Background(), RuntimeConfig{
		Telemetry: Config{ServiceName: "runtime-contract", Namespace: "example", Environment: "test"},
		Logger:    zap.New(core), ShutdownTimeout: 2 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(nil)) })
	ctx, span := runtime.SDK().TracerProvider().Tracer("runtime-contract").Start(runtime.Context(), "process-order")
	spanContext := span.SpanContext()
	counter, err := runtime.SDK().MeterProvider().Meter("runtime-contract").Int64Counter("runtime.contract.count")
	require.NoError(t, err)
	counter.Add(ctx, 1)
	WithTraceContext(ctx, runtime.Logger()).Info("runtime-contract-completed", zap.String("password", "synthetic-private-runtime-input"))
	span.End()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.NoError(t, runtime.Shutdown(cancelled))
	// Shutdown flushes synchronously: no periodic-reader wait or retry polling.
	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/v1/traces", "/v1/metrics", "/v1/logs"} {
		require.NotEmpty(t, received[path], "missing signal %s", path)
		for _, body := range received[path] {
			assert.NotContains(t, string(body), "synthetic-private-runtime-input")
		}
	}
	var spans collectortrace.ExportTraceServiceRequest
	require.NoError(t, proto.Unmarshal(received["/v1/traces"][0], &spans))
	require.Len(t, spans.ResourceSpans, 1)
	assert.Equal(t, "runtime-contract", runtimeOTLPAttribute(spans.ResourceSpans[0].Resource.Attributes, "service.name"))
	instanceID := runtimeOTLPAttribute(spans.ResourceSpans[0].Resource.Attributes, "service.instance.id")
	require.NotEmpty(t, instanceID)
	gotSpan := spans.ResourceSpans[0].ScopeSpans[0].Spans[0]
	assert.Equal(t, "process-order", gotSpan.Name)
	traceID := spanContext.TraceID()
	assert.Equal(t, traceID[:], gotSpan.TraceId)
	var logs collectorlog.ExportLogsServiceRequest
	require.NoError(t, proto.Unmarshal(received["/v1/logs"][0], &logs))
	require.Len(t, logs.ResourceLogs, 1)
	assert.Equal(t, instanceID, runtimeOTLPAttribute(logs.ResourceLogs[0].Resource.Attributes, "service.instance.id"))
	gotLog := logs.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	assert.Equal(t, gotSpan.TraceId, gotLog.TraceId)
	assert.Equal(t, gotSpan.SpanId, gotLog.SpanId)
	assert.Equal(t, "runtime-contract-completed", gotLog.Body.GetStringValue())
	foundCount := false
	for _, body := range received["/v1/metrics"] {
		var metrics collectormetric.ExportMetricsServiceRequest
		require.NoError(t, proto.Unmarshal(body, &metrics))
		for _, resource := range metrics.ResourceMetrics {
			assert.Equal(t, instanceID, runtimeOTLPAttribute(resource.Resource.Attributes, "service.instance.id"))
			for _, scope := range resource.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name == "runtime.contract.count" {
						foundCount = true
						assert.EqualValues(t, 1, metric.GetSum().DataPoints[0].GetAsInt())
					}
				}
			}
		}
	}
	assert.True(t, foundCount)
}

func configureRuntimeOTLPTest(t *testing.T, endpoint string) {
	t.Helper()
	clearRuntimeOTELTestEnvironment(t)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "60000")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	for _, signal := range []string{"traces", "metrics", "logs"} {
		upper := strings.ToUpper(signal)
		t.Setenv("OTEL_"+upper+"_EXPORTER", "otlp")
		t.Setenv("OTEL_EXPORTER_OTLP_"+upper+"_ENDPOINT", endpoint+"/v1/"+signal)
		t.Setenv("OTEL_EXPORTER_OTLP_"+upper+"_PROTOCOL", "http/protobuf")
		t.Setenv("OTEL_EXPORTER_OTLP_"+upper+"_HEADERS", "")
		t.Setenv("OTEL_EXPORTER_OTLP_"+upper+"_COMPRESSION", "none")
		t.Setenv("OTEL_EXPORTER_OTLP_"+upper+"_TIMEOUT", "1000")
	}
}

func clearRuntimeOTELTestEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
}

func runtimeOTLPAttribute(attributes []*commonpb.KeyValue, key string) string {
	for _, attribute := range attributes {
		if attribute.Key == key {
			return attribute.Value.GetStringValue()
		}
	}
	return ""
}

func restoreRuntimeTestGlobals(t *testing.T) {
	t.Helper()
	traces, meters := otel.GetTracerProvider(), otel.GetMeterProvider()
	logs, propagation := otellogglobal.GetLoggerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(traces)
		otel.SetMeterProvider(meters)
		otellogglobal.SetLoggerProvider(logs)
		otel.SetTextMapPropagator(propagation)
	})
}
