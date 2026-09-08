package observability_test

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	ghatdhttp "github.com/ooaklee/ghatd/external/observability/otelhttp"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logpb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const samplingCanary = "synthetic-private-sampling-input"

type samplingContractCase struct {
	name, sampler, ratio, parentFlags string
	target, route, result             string
	policy, tracesDisabled            bool
	edgeSampled, dependencySampled    bool
}

// TestHTTPSamplingWireContract exercises the real Start sampler, composed
// HTTP boundary, operation instrumentation and OTLP exporters. Two servers
// create a network boundary: only trace headers can carry the decision to the
// downstream request, never the private local suppression context marker.
func TestHTTPSamplingWireContract(t *testing.T) {
	for _, tc := range []samplingContractCase{
		{name: "default records new roots", edgeSampled: true, dependencySampled: true},
		{name: "selected health root retains metrics and logs", policy: true},
		{name: "selected static prefix retains metrics and logs", policy: true,
			target: "/assets/" + samplingCanary, route: "/assets/{file}"},
		{name: "ordinary route remains sampled", policy: true, target: "/work/ok", route: "/work/{result}",
			edgeSampled: true, dependencySampled: true},
		{name: "sampled incoming trace is preserved", policy: true, parentFlags: "01", edgeSampled: true, dependencySampled: true},
		{name: "unsampled incoming trace remains unsampled", policy: true, parentFlags: "00"},
		{name: "suppressed HTTP error still completes metrics", policy: true, result: "error"},
		{name: "suppressed panic still completes metrics", policy: true, result: "panic"},
		{name: "parent ratio zero keeps metrics and logs", sampler: "parentbased_traceidratio", ratio: "0"},
		{name: "parent ratio one records new roots", sampler: "parentbased_traceidratio", ratio: "1",
			edgeSampled: true, dependencySampled: true},
		{name: "parent ratio zero preserves sampled incoming trace", sampler: "parentbased_traceidratio", ratio: "0",
			policy: true, parentFlags: "01", edgeSampled: true, dependencySampled: true},
		{name: "disabled trace exporter keeps other signals", tracesDisabled: true},
		// A downstream always-on service can resume an unsampled trace. Local
		// children must still honor the selected request's private marker.
		{name: "always on cannot restart marked local children", sampler: "always_on", policy: true, dependencySampled: true},
	} {
		t.Run(tc.name, func(t *testing.T) { exerciseHTTPSamplingContract(t, tc) })
	}
}

type samplingObservation struct {
	server, operation, incoming trace.SpanContext
}

func exerciseHTTPSamplingContract(t *testing.T, tc samplingContractCase) {
	t.Helper()
	if tc.result == "" {
		tc.result = "ok"
	}
	if tc.target == "" {
		tc.target = "/probe/" + tc.result
	}
	if tc.route == "" {
		tc.route = "/probe/{result}"
	}
	status := http.StatusNoContent
	outcome := "success"
	if tc.result == "error" {
		status, outcome = http.StatusServiceUnavailable, "error"
	} else if tc.result == "panic" {
		status, outcome = http.StatusInternalServerError, "panic"
	}
	receiver := &contractReceiver{}
	configureContract(t, "http/protobuf", receiver.start(t, "http/protobuf"))
	t.Setenv("OTEL_TRACES_SAMPLER", tc.sampler)
	if tc.sampler == "" {
		// Exercise an absent setting rather than an explicitly empty value,
		// which the SDK's own environment detector reports as unsupported.
		require.NoError(t, os.Unsetenv("OTEL_TRACES_SAMPLER"))
	}
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", tc.ratio)
	if tc.tracesDisabled {
		t.Setenv("OTEL_TRACES_EXPORTER", "none")
	}
	core, local := observer.New(zap.InfoLevel)
	runtime, err := observability.StartRuntime(context.Background(), observability.RuntimeConfig{
		Telemetry: observability.Config{ServiceName: "sampling-contract", Namespace: "example", Environment: "test"},
		Logger:    zap.New(core), ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(nil)) })
	operations, err := observability.NewOperations(observability.OperationConfig{
		Scope: "sampling-contract", MetricPrefix: "sampling.contract.operation",
		TracerProvider: runtime.SDK().TracerProvider(), MeterProvider: runtime.SDK().MeterProvider(),
	})
	require.NoError(t, err)
	observations := make(chan samplingObservation, 2)
	observe := func(request *http.Request, operationContext context.Context) {
		incoming := propagation.TraceContext{}.Extract(context.Background(), propagation.HeaderCarrier(request.Header))
		observations <- samplingObservation{
			server: trace.SpanContextFromContext(request.Context()), operation: trace.SpanContextFromContext(operationContext),
			incoming: trace.SpanContextFromContext(incoming),
		}
	}
	readBody := func(request *http.Request) {
		body, readErr := io.ReadAll(request.Body)
		assert.NoError(t, readErr)
		assert.Equal(t, samplingCanary, string(body), "instrumentation must preserve the application body")
		assert.Equal(t, samplingCanary, request.URL.Query().Get("private"))
	}
	logOperation := func(ctx context.Context, name string) {
		ghatdlogger.AcquireFrom(ctx).Info("sampling operation observed",
			zap.String("operation", name), zap.String("password", samplingCanary))
	}
	dependencyRouter := ghatdrouter.NewRouter(nil, nil).GetRouter()
	dependencyRouter.HandleFunc("/dependency/{id}", func(writer http.ResponseWriter, request *http.Request) {
		ctx, operation := operations.Start(request.Context(), "dependency-work")
		defer operation.Finish(nil)
		observe(request, ctx)
		readBody(request)
		logOperation(ctx, "dependency-work")
		_, _ = io.WriteString(writer, "ready")
	}).Methods(http.MethodGet)
	dependency := httptest.NewServer(ghatdhttp.Wrap("sampling-dependency", runtime.Logger(), dependencyRouter))
	t.Cleanup(dependency.Close)
	client := observability.NewHTTPClient(nil, 2*time.Second)
	t.Cleanup(client.CloseIdleConnections)
	edgeRouter := ghatdrouter.NewRouter(nil, nil).GetRouter()
	edgeHandler := func(writer http.ResponseWriter, request *http.Request) {
		ctx, operation := operations.Start(request.Context(), "edge-work")
		var operationErr error
		defer operation.Finish(&operationErr)
		observe(request, ctx)
		readBody(request)
		logOperation(ctx, "edge-work")
		outbound, requestErr := http.NewRequestWithContext(ctx, http.MethodGet,
			dependency.URL+"/dependency/"+samplingCanary+"?private="+samplingCanary, strings.NewReader(samplingCanary))
		if !assert.NoError(t, requestErr) {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		response, responseErr := client.Do(outbound)
		if !assert.NoError(t, responseErr) {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		body, readErr := io.ReadAll(response.Body)
		assert.NoError(t, readErr)
		assert.NoError(t, response.Body.Close())
		assert.Equal(t, http.StatusOK, response.StatusCode)
		assert.Equal(t, "ready", string(body))
		switch tc.result {
		case "error":
			operationErr = errors.New(samplingCanary)
			http.Error(writer, http.StatusText(status), status)
		case "panic":
			panic(samplingCanary)
		default:
			writer.WriteHeader(status)
		}
	}
	for _, route := range []string{"/probe/{result}", "/work/{result}", "/assets/{file}"} {
		edgeRouter.HandleFunc(route, edgeHandler).Methods(http.MethodGet)
	}
	var options []observability.HTTPServerOption
	if tc.policy {
		policy, policyErr := observability.NewHTTPTracePolicy([]string{"/probe/ok", "/probe/error", "/probe/panic"}, []string{"/assets/"})
		require.NoError(t, policyErr)
		options = append(options, observability.WithHTTPTracePolicy(policy))
	}
	edge := httptest.NewServer(ghatdhttp.WrapWithOptions("sampling-edge", runtime.Logger(), edgeRouter, options...))
	t.Cleanup(edge.Close)
	origin := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{}}
	t.Cleanup(origin.CloseIdleConnections)
	request, err := http.NewRequest(http.MethodGet, edge.URL+tc.target+"?private="+samplingCanary, strings.NewReader(samplingCanary))
	require.NoError(t, err)
	if tc.parentFlags != "" {
		request.Header.Set("traceparent", "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-"+tc.parentFlags)
		request.Header.Set("tracestate", "vendor=contract")
	}
	response, err := origin.Do(request)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, status, response.StatusCode)
	// Closing the servers waits for every handler before the SDK's final flush.
	edge.Close()
	dependency.Close()
	require.Len(t, observations, 2)
	edgeObservation, dependencyObservation := <-observations, <-observations
	assertSamplingContext(t, edgeObservation.server, tc.edgeSampled)
	assertSamplingContext(t, edgeObservation.operation, tc.edgeSampled)
	assertSamplingContext(t, dependencyObservation.server, tc.dependencySampled)
	assertSamplingContext(t, dependencyObservation.operation, tc.dependencySampled)
	assertSamplingContext(t, dependencyObservation.incoming, tc.edgeSampled)
	assert.True(t, dependencyObservation.incoming.IsRemote())
	assert.Equal(t, edgeObservation.server.TraceID(), dependencyObservation.server.TraceID())
	assert.Equal(t, edgeObservation.server.TraceID(), dependencyObservation.incoming.TraceID())
	assert.NotEqual(t, edgeObservation.server.SpanID(), edgeObservation.operation.SpanID())
	assert.NotEqual(t, edgeObservation.operation.SpanID(), dependencyObservation.incoming.SpanID())
	assert.NotEqual(t, dependencyObservation.incoming.SpanID(), dependencyObservation.server.SpanID())
	if tc.parentFlags != "" {
		assert.Equal(t, "0102030405060708090a0b0c0d0e0f10", edgeObservation.server.TraceID().String())
		assert.Equal(t, "0102030405060708", edgeObservation.incoming.SpanID().String())
		for _, sc := range []trace.SpanContext{edgeObservation.server, edgeObservation.operation, dependencyObservation.incoming, dependencyObservation.server} {
			assert.Equal(t, "vendor=contract", sc.TraceState().String())
		}
	}
	require.NoError(t, runtime.Shutdown(nil))
	snapshot := receiver.snapshot()
	assertSamplingSignals(t, snapshot, tc, status, outcome, edgeObservation, dependencyObservation)
	assert.Len(t, local.FilterMessage("http request completed").All(), 2, "head sampling must not remove normal request logs")
	localOperations := local.FilterMessage("sampling operation observed").All()
	require.Len(t, localOperations, 2)
	for _, record := range localOperations {
		assert.Equal(t, samplingCanary, record.ContextMap()["password"], "existing local log fields are unchanged")
	}
}

func assertSamplingContext(t *testing.T, sc trace.SpanContext, sampled bool) {
	t.Helper()
	assert.True(t, sc.IsValid(), "dropping spans must still create valid correlation IDs")
	assert.Equal(t, sampled, sc.IsSampled())
}

func assertSamplingSignals(t *testing.T, snapshot contractSnapshot, tc samplingContractCase, status int, outcome string,
	edge, dependency samplingObservation,
) {
	t.Helper()
	privacy := func(message proto.Message) {
		encoded, err := protojson.Marshal(message)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), samplingCanary)
	}
	spans := make(map[string]*tracepb.Span)
	for _, request := range snapshot.traces {
		privacy(request)
		for _, resource := range request.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				for _, span := range scope.Spans {
					assert.NotContains(t, spans, span.Name, "each span completes once")
					spans[span.Name] = span
				}
			}
		}
	}
	wanted := map[string]trace.SpanContext{}
	if tc.edgeSampled {
		wanted["GET "+tc.route], wanted["edge-work"], wanted["HTTP GET"] = edge.server, edge.operation, dependency.incoming
	}
	if tc.dependencySampled {
		wanted["GET /dependency/{id}"], wanted["dependency-work"] = dependency.server, dependency.operation
	}
	require.Len(t, spans, len(wanted))
	for name, sc := range wanted {
		require.Contains(t, spans, name)
		assert.Equal(t, sc.TraceID().String(), hex.EncodeToString(spans[name].TraceId))
		assert.Equal(t, sc.SpanID().String(), hex.EncodeToString(spans[name].SpanId))
	}
	if tc.edgeSampled {
		assert.Equal(t, edge.server.SpanID().String(), hex.EncodeToString(spans["edge-work"].ParentSpanId))
		assert.Equal(t, edge.operation.SpanID().String(), hex.EncodeToString(spans["HTTP GET"].ParentSpanId))
		if tc.parentFlags != "" {
			assert.Equal(t, edge.incoming.SpanID().String(), hex.EncodeToString(spans["GET "+tc.route].ParentSpanId))
		}
	}
	if tc.dependencySampled {
		assert.Equal(t, dependency.incoming.SpanID().String(), hex.EncodeToString(spans["GET /dependency/{id}"].ParentSpanId))
		assert.Equal(t, dependency.server.SpanID().String(), hex.EncodeToString(spans["dependency-work"].ParentSpanId))
	}
	var records []*logpb.LogRecord
	for _, request := range snapshot.logs {
		privacy(request)
		for _, resource := range request.ResourceLogs {
			for _, scope := range resource.ScopeLogs {
				records = append(records, scope.LogRecords...)
			}
		}
	}
	require.Len(t, records, 4, "both request and operation logs must survive head sampling")
	seenLogs := make(map[string]bool)
	for _, record := range records {
		var sc trace.SpanContext
		key := record.Body.GetStringValue()
		switch key {
		case "http request completed":
			route := contractAttribute(record.Attributes, "route")
			key += route
			sc = dependency.server
			if route == tc.route {
				sc = edge.server
				assert.EqualValues(t, status, samplingIntAttribute(record.Attributes, "status"))
				if tc.result == "panic" {
					assert.Equal(t, "panic", contractAttribute(record.Attributes, "error.type"))
				}
			} else {
				assert.Equal(t, "/dependency/{id}", route)
				assert.EqualValues(t, http.StatusOK, samplingIntAttribute(record.Attributes, "status"))
			}
		case "sampling operation observed":
			operation := contractAttribute(record.Attributes, "operation")
			key += operation
			sc = dependency.operation
			if operation == "edge-work" {
				sc = edge.operation
			} else {
				assert.Equal(t, "dependency-work", operation)
			}
		default:
			t.Error("unexpected log record")
		}
		assert.False(t, seenLogs[key], "each request and operation logs once")
		seenLogs[key] = true
		assert.Equal(t, sc.TraceID().String(), hex.EncodeToString(record.TraceId))
		assert.Equal(t, sc.SpanID().String(), hex.EncodeToString(record.SpanId))
		assert.Equal(t, sc.IsSampled(), record.Flags&1 != 0)
	}
	metrics := make(map[string][]*metricpb.Metric)
	for _, request := range snapshot.metrics {
		privacy(request)
		for _, resource := range request.ResourceMetrics {
			for _, scope := range resource.ScopeMetrics {
				for _, metric := range scope.Metrics {
					metrics[metric.Name] = append(metrics[metric.Name], metric)
				}
			}
		}
	}
	for _, name := range []string{"http.server.request.duration", "http.server.request.body.size", "http.server.response.body.size"} {
		require.Len(t, metrics[name], 1, name)
		points := metrics[name][0].GetHistogram().GetDataPoints()
		require.Len(t, points, 2, name)
		for _, point := range points {
			assert.EqualValues(t, 1, point.Count, "each server records every measurement exactly once")
			route := contractAttribute(point.Attributes, "http.route")
			expectedStatus, expectedResponseSize := http.StatusOK, len("ready")
			if route == tc.route {
				expectedStatus, expectedResponseSize = status, 0
				if status >= 400 {
					expectedResponseSize = len(http.StatusText(status)) + 1
				}
				if tc.result == "panic" {
					assert.Equal(t, "panic", contractAttribute(point.Attributes, "error.type"))
				}
			} else {
				assert.Equal(t, "/dependency/{id}", route)
			}
			assert.Equal(t, "GET", contractAttribute(point.Attributes, "http.request.method"))
			assert.EqualValues(t, expectedStatus, samplingIntAttribute(point.Attributes, "http.response.status_code"))
			switch name {
			case "http.server.request.body.size":
				assert.EqualValues(t, len(samplingCanary), point.GetSum())
			case "http.server.response.body.size":
				assert.EqualValues(t, expectedResponseSize, point.GetSum())
			}
		}
	}
	require.Len(t, metrics["http.client.request.duration"], 1)
	require.Len(t, metrics["http.client.request.duration"][0].GetHistogram().GetDataPoints(), 1)
	assert.EqualValues(t, 1, metrics["http.client.request.duration"][0].GetHistogram().DataPoints[0].Count)
	for _, suffix := range []string{"count", "active", "duration"} {
		name := "sampling.contract.operation." + suffix
		require.Len(t, metrics[name], 1, name)
		if suffix == "duration" {
			points := metrics[name][0].GetHistogram().GetDataPoints()
			require.Len(t, points, 2)
			for _, point := range points {
				assert.EqualValues(t, 1, point.Count)
				expectedSampled := tc.dependencySampled
				if contractAttribute(point.Attributes, "operation") == "edge-work" {
					expectedSampled = tc.edgeSampled
				}
				assert.Equal(t, expectedSampled, len(point.Exemplars) > 0, "trace-based exemplars follow sampling; measurements remain")
			}
			continue
		}
		points := metrics[name][0].GetSum().GetDataPoints()
		require.Len(t, points, 2)
		for _, point := range points {
			if suffix == "active" {
				assert.Zero(t, point.GetAsInt(), "operations balance even when their spans are dropped or they panic")
				continue
			}
			assert.EqualValues(t, 1, point.GetAsInt())
			expectedOutcome := "success"
			if contractAttribute(point.Attributes, "operation") == "edge-work" {
				expectedOutcome = outcome
			}
			assert.Equal(t, expectedOutcome, contractAttribute(point.Attributes, "outcome"))
		}
	}
}

func samplingIntAttribute(attributes []*commonpb.KeyValue, key string) int64 {
	for _, attribute := range attributes {
		if attribute.Key == key {
			return attribute.Value.GetIntValue()
		}
	}
	return 0
}
