package serverhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	ghatdobservability "github.com/ooaklee/ghatd/external/observability"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestHTTPTraceNoisePolicyPreservesMetricsLogsAndParentDecisions(t *testing.T) {
	const sampledParent = "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01"
	const unsampledParent = "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-00"
	for _, test := range []struct {
		name, method, target, parent        string
		enabled, sampled, panics, unmatched bool
		alwaysOn, suppressCompletion        bool
		status                              int
	}{
		{name: "default health remains traced", method: "GET", target: "/v0/health/check", sampled: true, suppressCompletion: true, status: 200},
		{name: "always on keeps health roots", method: "GET", target: "/v0/health/check", alwaysOn: true, sampled: true, suppressCompletion: true, status: 200},
		{name: "health query is ignored", method: "GET", target: "/v0/health/check?probe=1", sampled: true, suppressCompletion: true, status: 200},
		{name: "health panic remains recovered", method: "GET", target: "/v0/health/check", sampled: true, panics: true, suppressCompletion: true, status: 500},
		{name: "nearby path remains logged", method: "GET", target: "/v0/health/check-extra", sampled: true, status: 200},
		{name: "always on keeps static requests", method: "GET", target: "/assets/private-canary.js", alwaysOn: true, sampled: true, status: 200},
		{name: "always on records unsampled incoming trace", method: "GET", target: "/api/items/private-canary", parent: unsampledParent, alwaysOn: true, sampled: true, status: 200},
		{name: "parent based still respects unsampled incoming trace", method: "GET", target: "/api/items/private-canary", parent: unsampledParent, status: 200},
		{name: "noise filter can still override always on", method: "GET", target: "/v0/health/check", alwaysOn: true, enabled: true, suppressCompletion: true, status: 200},
		{name: "health opt in", method: "GET", target: "/v0/health/check", enabled: true, suppressCompletion: true, status: 200},
		{name: "HEAD health opt in", method: "HEAD", target: "/v0/health/check", enabled: true, suppressCompletion: true, status: 200},
		{name: "favicon opt in", method: "GET", target: "/favicon.ico", enabled: true, status: 200},
		{name: "asset with private query", method: "GET", target: "/assets/private-canary.js?token=private-canary", enabled: true, status: 200},
		{name: "POST health remains traced", method: "POST", target: "/v0/health/check", enabled: true, sampled: true, suppressCompletion: true, status: 200},
		{name: "prefix boundary remains traced", method: "GET", target: "/assets-private/private-canary.js", enabled: true, sampled: true, status: 200},
		{name: "bare directory remains traced", method: "GET", target: "/assets", enabled: true, sampled: true, status: 200},
		{name: "API suffix remains traced", method: "GET", target: "/api/items/private-canary.css", enabled: true, sampled: true, status: 200},
		{name: "encoded asset remains traced", method: "GET", target: "/assets/%70rivate-canary.js", enabled: true, sampled: true, status: 200},
		{name: "sampled parent is retained", method: "GET", target: "/v0/health/check", parent: sampledParent, enabled: true, sampled: true, suppressCompletion: true, status: 200},
		{name: "unsampled parent is retained", method: "GET", target: "/v0/health/check", parent: unsampledParent, enabled: true, suppressCompletion: true, status: 200},
		{name: "health failure retains metrics and downstream logs", method: "GET", target: "/v0/health/check", enabled: true, suppressCompletion: true, status: 500},
		{name: "asset panic still has logs and metrics", method: "GET", target: "/assets/private-canary.js", enabled: true, panics: true, status: 500},
		{name: "asset not found still has logs and metrics", method: "GET", target: "/assets/missing/private-canary.js", enabled: true, unmatched: true, status: 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			baseSampler := sdktrace.ParentBased(sdktrace.AlwaysSample())
			if test.alwaysOn {
				baseSampler = sdktrace.AlwaysSample()
			}
			traces := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter),
				sdktrace.WithSampler(ghatdobservability.HTTPTraceSampler(baseSampler)))
			reader := sdkmetric.NewManualReader()
			metrics := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			previousTracer, previousMeter, previousPropagation := otel.GetTracerProvider(), otel.GetMeterProvider(), otel.GetTextMapPropagator()
			otel.SetTracerProvider(traces)
			otel.SetMeterProvider(metrics)
			otel.SetTextMapPropagator(propagation.TraceContext{})
			t.Cleanup(func() {
				otel.SetTracerProvider(previousTracer)
				otel.SetMeterProvider(previousMeter)
				otel.SetTextMapPropagator(previousPropagation)
				require.NoError(t, traces.Shutdown(context.Background()))
				require.NoError(t, metrics.Shutdown(context.Background()))
			})
			operations, err := ghatdobservability.NewOperations(ghatdobservability.OperationConfig{Scope: "fixture", MetricPrefix: "fixture.service.operation", TracerProvider: traces, MeterProvider: metrics})
			require.NoError(t, err)
			core, logs := observer.New(zap.InfoLevel)
			appLogger := zap.New(core)
			handlerCalled := false
			serve := func(writer http.ResponseWriter, request *http.Request) {
				handlerCalled = true
				assert.Equal(t, test.target, request.URL.String(), "sampling must preserve the handler's original target")
				assert.Equal(t, "Bearer private-canary", request.Header.Get("Authorization"))
				serverContext := trace.SpanContextFromContext(request.Context())
				assert.True(t, serverContext.IsValid())
				assert.Equal(t, test.sampled, serverContext.IsSampled())
				ctx, operation := operations.Start(request.Context(), "fixture-work")
				var operationErr error
				defer operation.Finish(&operationErr)
				childContext := trace.SpanContextFromContext(ctx)
				assert.True(t, childContext.IsValid())
				assert.Equal(t, serverContext.TraceID(), childContext.TraceID())
				assert.Equal(t, test.sampled, childContext.IsSampled())
				ghatdlogger.AcquireFrom(ctx).Info("fixture work completed")
				outbound := http.Header{}
				propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(outbound))
				assert.True(t, strings.HasSuffix(outbound.Get("traceparent"), map[bool]string{true: "-01", false: "-00"}[test.sampled]))
				if test.parent != "" {
					assert.Equal(t, "0102030405060708090a0b0c0d0e0f10", childContext.TraceID().String())
					assert.Equal(t, "vendor=value", outbound.Get("tracestate"))
				}
				if test.panics {
					panic("private-canary")
				}
				writer.WriteHeader(test.status)
			}
			router := ghatdrouter.NewRouter(nil, serve).GetRouter()
			for _, path := range []string{"/v0/health/check-extra", "/favicon.ico", "/assets/{file}", "/assets-private/{file}", "/assets", "/api/items/{id}"} {
				router.HandleFunc(path, serve)
			}
			handler, err := initialiseHTTPHandler(&testHTTPSettings{Component: "fixture-api", HTTPTraceNoiseSuppression: test.enabled}, appLogger, router)
			require.NoError(t, err)
			request := httptest.NewRequest(test.method, test.target, nil)
			request.Header.Set("Authorization", "Bearer private-canary")
			if test.parent != "" {
				request.Header.Set("traceparent", test.parent)
				request.Header.Set("tracestate", "vendor=value")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assert.Equal(t, !test.unmatched, handlerCalled)
			assert.Equal(t, test.status, response.Code)
			spans := exporter.GetSpans()
			if test.sampled {
				require.Len(t, spans, 2, "one server span and one child operation")
				assert.Equal(t, spans[1].SpanContext.SpanID(), spans[0].Parent.SpanID())
				if test.parent != "" {
					assert.Equal(t, "0102030405060708", spans[1].Parent.SpanID().String())
				}
			} else {
				assert.Empty(t, spans, "selected roots and their local operations remain unrecorded")
			}
			assert.NotEmpty(t, response.Header().Get("X-Correlation-Id"))
			completion := logs.FilterMessage("http request completed").All()
			if test.suppressCompletion {
				assert.Empty(t, completion)
			} else {
				require.Len(t, completion, 1)
				assert.EqualValues(t, test.status, completion[0].ContextMap()["status"])
			}
			for _, record := range logs.All() {
				traceID, err := trace.TraceIDFromHex(record.ContextMap()[ghatdobservability.TraceIDKey].(string))
				require.NoError(t, err)
				assert.True(t, traceID.IsValid())
				spanID, err := trace.SpanIDFromHex(record.ContextMap()[ghatdobservability.SpanIDKey].(string))
				require.NoError(t, err)
				assert.True(t, spanID.IsValid())
			}
			var collected metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(context.Background(), &collected))
			var httpCount uint64
			var operationCount int64
			for _, scope := range collected.ScopeMetrics {
				for _, instrument := range scope.Metrics {
					switch instrument.Name {
					case "http.server.request.duration":
						for _, point := range instrument.Data.(metricdata.Histogram[float64]).DataPoints {
							httpCount += point.Count
							status, _ := point.Attributes.Value("http.response.status_code")
							assert.EqualValues(t, test.status, status.AsInt64())
						}
					case "fixture.service.operation.count":
						for _, point := range instrument.Data.(metricdata.Sum[int64]).DataPoints {
							operationCount += point.Value
						}
					}
				}
			}
			assert.Equal(t, uint64(1), httpCount)
			if test.unmatched {
				assert.Zero(t, operationCount)
			} else {
				assert.Equal(t, int64(1), operationCount, "operation metrics survive span suppression")
				require.Len(t, logs.FilterMessage("fixture work completed").All(), 1)
			}
			for _, signal := range []any{spans, logs.All(), collected} {
				encoded, err := json.Marshal(signal)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), "private-canary")
			}
		})
	}
}
