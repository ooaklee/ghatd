package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	redis "github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestHTTPReferenceConnectsRequestOperationDependencyAndLogs(t *testing.T) {
	spans, logs, reader := referenceExporters(t)
	runtime, classifier := referenceRuntime(t)
	operations, err := exampleOperations(runtime, classifier)
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(nil)
	service := newService(operations, classifier, listenerURL(server.Listener)+"/dependency")
	defer service.client.CloseIdleConnections()
	server.Config.Handler = newHandler(runtime, service)
	server.Start()
	defer server.Close()
	const private = "synthetic-private-request-value"
	for _, test := range []struct {
		path, status, outcome, code string
		httpStatus                  int
		spanCount                   int
	}{
		{"/api/v1/work", "ok", "success", "", http.StatusOK, 4},
		{"/api/v1/rejected", "rejected", "rejected", "EXAMPLE-001", http.StatusTooManyRequests, 2},
	} {
		response, err := http.Get(server.URL + test.path + "?token=" + private)
		require.NoError(t, err)
		var body workResponse
		require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
		require.NoError(t, response.Body.Close())
		assert.Equal(t, test.httpStatus, response.StatusCode)
		assert.Equal(t, test.status, body.Status)
		assert.Equal(t, test.code, body.Code)
		require.Len(t, body.TraceID, 32)
		require.NoError(t, runtime.SDK().TracerProvider().ForceFlush(context.Background()))
		require.NoError(t, runtime.SDK().LoggerProvider().ForceFlush(context.Background()))
		traceSpans := referenceTrace(spans.GetSpans(), body.TraceID)
		require.Len(t, traceSpans, test.spanCount)
		requestSpan := referenceSpan(t, traceSpans, trace.SpanKindServer, "GET "+test.path)
		operation := referenceSpan(t, traceSpans, trace.SpanKindInternal, "process-work")
		assert.Equal(t, requestSpan.SpanContext.SpanID(), operation.Parent.SpanID())
		assert.Equal(t, test.outcome, referenceAttribute(operation.Attributes, "outcome"))
		assert.Equal(t, test.code, referenceAttribute(operation.Attributes, "error.type"))
		assert.Equal(t, codes.Unset, operation.Status.Code)
		assert.Equal(t, "example-api", referenceAttribute(operation.Resource.Attributes(), "service.name"))
		assert.Equal(t, "example", referenceAttribute(operation.Resource.Attributes(), "service.namespace"))
		assert.Equal(t, "local", referenceAttribute(operation.Resource.Attributes(), "deployment.environment.name"))
		if test.path == "/api/v1/work" {
			client := referenceSpan(t, traceSpans, trace.SpanKindClient, "")
			dependency := referenceSpan(t, traceSpans, trace.SpanKindServer, "GET /dependency")
			assert.Equal(t, operation.SpanContext.SpanID(), client.Parent.SpanID())
			assert.Equal(t, client.SpanContext.SpanID(), dependency.Parent.SpanID())
		}
		foundOperationLog, foundRequestLog := false, false
		for _, record := range logs.Records() {
			if record.TraceID().String() != body.TraceID {
				continue
			}
			foundOperationLog = foundOperationLog || record.SpanID() == operation.SpanContext.SpanID() && record.Body().AsString() == "work completed"
			foundRequestLog = foundRequestLog || record.SpanID() == requestSpan.SpanContext.SpanID() && record.Body().AsString() == "http request completed"
			assert.NotContains(t, fmt.Sprint(record), private)
		}
		assert.True(t, foundOperationLog)
		assert.True(t, foundRequestLog)
		for _, span := range traceSpans {
			assert.NotContains(t, fmt.Sprint(span.Name, span.Attributes, span.Events, span.Status), private)
		}
	}
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	count, active := false, false
	for _, scope := range metrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			switch metric.Name {
			case "example.service.operation.count":
				count = true
				points := metric.Data.(metricdata.Sum[int64]).DataPoints
				require.Len(t, points, 2)
				for _, point := range points {
					assert.EqualValues(t, 1, point.Value)
				}
			case "example.service.operation.active":
				active = true
				for _, point := range metric.Data.(metricdata.Sum[int64]).DataPoints {
					assert.Zero(t, point.Value)
				}
			}
		}
	}
	assert.True(t, count)
	assert.True(t, active)
}

func TestStandaloneWorkerFlushesWithoutExistingServer(t *testing.T) {
	spans, logs, _ := referenceExporters(t)
	command := newCommand()
	command.SetArgs([]string{"work"})
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	require.NoError(t, command.ExecuteContext(context.Background()))
	var response workResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &response))
	assert.Equal(t, "ok", response.Status)
	require.Len(t, response.TraceID, 32)
	ended := referenceTrace(spans.GetSpans(), response.TraceID)
	require.Len(t, ended, 4, "the Cobra adapter must flush all ended spans before Execute returns")
	commandSpan := referenceSpan(t, ended, trace.SpanKindInternal, "work")
	operation := referenceSpan(t, ended, trace.SpanKindInternal, "process-work")
	client := referenceSpan(t, ended, trace.SpanKindClient, "")
	dependency := referenceSpan(t, ended, trace.SpanKindServer, "GET /dependency")
	assert.Equal(t, commandSpan.SpanContext.SpanID(), operation.Parent.SpanID())
	assert.Equal(t, operation.SpanContext.SpanID(), client.Parent.SpanID())
	assert.Equal(t, client.SpanContext.SpanID(), dependency.Parent.SpanID())
	assert.False(t, commandSpan.EndTime.Before(dependency.EndTime))
	assert.Equal(t, "example-worker", referenceAttribute(commandSpan.Resource.Attributes(), "service.name"))
	found := false
	for _, record := range logs.Records() {
		if record.SpanID() == commandSpan.SpanContext.SpanID() && record.Body().AsString() == "command completed" {
			found = true
			assert.Equal(t, commandSpan.SpanContext.TraceID(), record.TraceID())
		}
	}
	assert.True(t, found, "command completion must flush before shutdown")
}

func TestExampleIdentityDefaultsAllowStandardEnvironmentOverrides(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	assert.Equal(t, observability.Config{ServiceName: "example-api", Namespace: "example", Environment: "local"}, exampleIdentity("example-api"))
	t.Setenv("OTEL_SERVICE_NAME", "configured-service")
	assert.Empty(t, exampleIdentity("example-api").ServiceName)
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=resource-service,service.namespace=configured,deployment.environment.name=testing,service.version=build-1")
	assert.Equal(t, observability.Config{}, exampleIdentity("example-api"))
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=first,service.name=,service.namespace=%20")
	assert.Equal(t, observability.Config{ServiceName: "example-worker", Namespace: "example", Environment: "local"}, exampleIdentity("example-worker"))
}

func TestDatabaseWiringConstructsWithoutOpeningConnections(t *testing.T) {
	referenceExporters(t)
	runtime, _ := referenceRuntime(t)
	assert.NotNil(t, mongoOptionsWithTelemetry(runtime).Monitor)
	client := redisClientWithTelemetry(runtime, &redis.Options{Addr: "127.0.0.1:6379"})
	require.NoError(t, client.Close())
	// This checks setup/API compatibility only. No Redis command, MongoDB
	// request, or simulated hook invocation is represented as database proof.
}

func referenceRuntime(t *testing.T) (*observability.Runtime, *observability.ErrorClassifier) {
	t.Helper()
	config, classifier, err := exampleRuntimeConfig("example-api")
	require.NoError(t, err)
	runtime, err := observability.StartRuntime(context.Background(), config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })
	return runtime, classifier
}

var referenceExporterID atomic.Uint64

func referenceExporters(t *testing.T) (*referenceSpanExporter, *referenceLogExporter, *sdkmetric.ManualReader) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
	oldTrace, oldMetric := otel.GetTracerProvider(), otel.GetMeterProvider()
	oldLog, oldPropagation := otellogglobal.GetLoggerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(oldTrace)
		otel.SetMeterProvider(oldMetric)
		otellogglobal.SetLoggerProvider(oldLog)
		otel.SetTextMapPropagator(oldPropagation)
	})
	spans := &referenceSpanExporter{InMemoryExporter: tracetest.NewInMemoryExporter()}
	logs := &referenceLogExporter{}
	reader := sdkmetric.NewManualReader()
	name := fmt.Sprintf("reference-test-%d", referenceExporterID.Add(1))
	autoexport.RegisterSpanExporter(name, func(context.Context) (sdktrace.SpanExporter, error) { return spans, nil })
	autoexport.RegisterLogExporter(name, func(context.Context) (sdklog.Exporter, error) { return logs, nil })
	autoexport.RegisterMetricReader(name, func(context.Context) (sdkmetric.Reader, error) { return reader, nil })
	t.Setenv("OTEL_TRACES_EXPORTER", name)
	t.Setenv("OTEL_LOGS_EXPORTER", name)
	t.Setenv("OTEL_METRICS_EXPORTER", name)
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	return spans, logs, reader
}

// Keep exported spans available after the runtime flushes and stops.
type referenceSpanExporter struct{ *tracetest.InMemoryExporter }

func (*referenceSpanExporter) Shutdown(context.Context) error { return nil }

type referenceLogExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (exporter *referenceLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	for _, record := range records {
		exporter.records = append(exporter.records, record.Clone())
	}
	return nil
}

func (*referenceLogExporter) Shutdown(context.Context) error   { return nil }
func (*referenceLogExporter) ForceFlush(context.Context) error { return nil }

func (exporter *referenceLogExporter) Records() []sdklog.Record {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	return append([]sdklog.Record(nil), exporter.records...)
}

func referenceTrace(spans tracetest.SpanStubs, traceID string) tracetest.SpanStubs {
	var matches tracetest.SpanStubs
	for _, span := range spans {
		if span.SpanContext.TraceID().String() == traceID {
			matches = append(matches, span)
		}
	}
	return matches
}

func referenceSpan(t *testing.T, spans tracetest.SpanStubs, kind trace.SpanKind, name string) tracetest.SpanStub {
	t.Helper()
	for _, span := range spans {
		if span.SpanKind == kind && (name == "" || span.Name == name) {
			return span
		}
	}
	t.Fatalf("missing %s span %q", kind, name)
	return tracetest.SpanStub{}
}

func referenceAttribute(attributes []attribute.KeyValue, key string) string {
	for _, attribute := range attributes {
		if string(attribute.Key) == key {
			return attribute.Value.AsString()
		}
	}
	return ""
}
