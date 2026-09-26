package observability_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	redis "github.com/go-redis/redis/v7"
	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/observability/otelhttp"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/v2/mongo/otelmongo"
	"go.opentelemetry.io/otel"
	logglobal "go.opentelemetry.io/otel/log/global"
	collectorlog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logpb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// These tests exercise the actual autoexport SDK and serialized OTLP messages.
// Database callbacks are synthetic; neither database service is required.
func TestOTLPWireContract(t *testing.T) {
	for _, protocol := range []string{"http/protobuf", "grpc"} {
		t.Run(protocol, func(t *testing.T) {
			receiver := &contractReceiver{}
			endpoint := receiver.start(t, protocol)
			configureContract(t, protocol, endpoint)
			exerciseContract(t)
			assertContract(t, receiver.snapshot())
		})
	}
}

type contractSnapshot struct {
	traces  []*collectortrace.ExportTraceServiceRequest
	metrics []*collectormetric.ExportMetricsServiceRequest
	logs    []*collectorlog.ExportLogsServiceRequest
}

type contractReceiver struct {
	mu sync.Mutex
	contractSnapshot
}

func (receiver *contractReceiver) snapshot() contractSnapshot {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	return contractSnapshot{
		traces:  append([]*collectortrace.ExportTraceServiceRequest(nil), receiver.traces...),
		metrics: append([]*collectormetric.ExportMetricsServiceRequest(nil), receiver.metrics...),
		logs:    append([]*collectorlog.ExportLogsServiceRequest(nil), receiver.logs...),
	}
}

func (receiver *contractReceiver) start(t *testing.T, protocol string) string {
	t.Helper()
	if protocol == "grpc" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		server := grpc.NewServer()
		collectortrace.RegisterTraceServiceServer(server, &contractTraceServer{receiver: receiver})
		collectormetric.RegisterMetricsServiceServer(server, &contractMetricServer{receiver: receiver})
		collectorlog.RegisterLogsServiceServer(server, &contractLogServer{receiver: receiver})
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(server.Stop)
		return "http://" + listener.Addr().String()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-protobuf" {
			http.Error(w, "invalid transport", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		receiver.mu.Lock()
		defer receiver.mu.Unlock()
		switch r.URL.Path {
		case "/v1/traces":
			request := &collectortrace.ExportTraceServiceRequest{}
			err = proto.Unmarshal(body, request)
			receiver.traces = append(receiver.traces, request)
		case "/v1/metrics":
			request := &collectormetric.ExportMetricsServiceRequest{}
			err = proto.Unmarshal(body, request)
			receiver.metrics = append(receiver.metrics, request)
		case "/v1/logs":
			request := &collectorlog.ExportLogsServiceRequest{}
			err = proto.Unmarshal(body, request)
			receiver.logs = append(receiver.logs, request)
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK) // An empty protobuf Export response.
	}))
	t.Cleanup(server.Close)
	return server.URL
}

type contractTraceServer struct {
	collectortrace.UnimplementedTraceServiceServer
	receiver *contractReceiver
}

func (server *contractTraceServer) Export(_ context.Context, request *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	server.receiver.mu.Lock()
	defer server.receiver.mu.Unlock()
	server.receiver.traces = append(server.receiver.traces, proto.Clone(request).(*collectortrace.ExportTraceServiceRequest))
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

type contractMetricServer struct {
	collectormetric.UnimplementedMetricsServiceServer
	receiver *contractReceiver
}

func (server *contractMetricServer) Export(_ context.Context, request *collectormetric.ExportMetricsServiceRequest) (*collectormetric.ExportMetricsServiceResponse, error) {
	server.receiver.mu.Lock()
	defer server.receiver.mu.Unlock()
	server.receiver.metrics = append(server.receiver.metrics, proto.Clone(request).(*collectormetric.ExportMetricsServiceRequest))
	return &collectormetric.ExportMetricsServiceResponse{}, nil
}

type contractLogServer struct {
	collectorlog.UnimplementedLogsServiceServer
	receiver *contractReceiver
}

func (server *contractLogServer) Export(_ context.Context, request *collectorlog.ExportLogsServiceRequest) (*collectorlog.ExportLogsServiceResponse, error) {
	server.receiver.mu.Lock()
	defer server.receiver.mu.Unlock()
	server.receiver.logs = append(server.receiver.logs, proto.Clone(request).(*collectorlog.ExportLogsServiceRequest))
	return &collectorlog.ExportLogsServiceResponse{}, nil
}

func configureContract(t *testing.T, protocol, endpoint string) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "2000")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "3600000")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
		t.Setenv("OTEL_"+signal+"_EXPORTER", "otlp")
	}
	traces, meters := otel.GetTracerProvider(), otel.GetMeterProvider()
	logs, propagation := logglobal.GetLoggerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(traces)
		otel.SetMeterProvider(meters)
		logglobal.SetLoggerProvider(logs)
		otel.SetTextMapPropagator(propagation)
	})
}

const contractCanary = "synthetic-private-contract-input"

func exerciseContract(t *testing.T) {
	t.Helper()
	core, _ := observer.New(zap.InfoLevel)
	runtime, err := observability.StartRuntime(context.Background(), observability.RuntimeConfig{
		Telemetry: observability.Config{ServiceName: "contract-api", Namespace: "example", Environment: "test", Version: "1.0.0"},
		Logger:    zap.New(core), ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(nil)) })
	sdk := runtime.SDK()
	operations, err := observability.NewOperations(observability.OperationConfig{
		Scope: "contract", MetricPrefix: "contract.operation",
		TracerProvider: sdk.TracerProvider(), MeterProvider: sdk.MeterProvider(),
	})
	require.NoError(t, err)
	monitor := observability.NewMongoCommandMonitor(otelmongo.WithTracerProvider(sdk.TracerProvider()), otelmongo.WithMeterProvider(sdk.MeterProvider()))
	hook := observability.NewRedisHook(&redis.Options{Addr: "localhost:6379"},
		observability.WithRedisTracerProvider(sdk.TracerProvider()), observability.WithRedisMeterProvider(sdk.MeterProvider()))
	command, err := bson.Marshal(bson.D{{Key: "find", Value: "records"}, {Key: "filter", Value: bson.D{{Key: "value", Value: contractCanary}}}})
	require.NoError(t, err)
	dependencyRouter := ghatdrouter.NewRouter(nil, nil).GetRouter()
	dependencyRouter.HandleFunc("/dependency/{id}", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, contractCanary, r.URL.Query().Get("token"))
		_, _ = io.WriteString(w, contractCanary)
	}).Methods(http.MethodGet)
	dependency := httptest.NewServer(otelhttp.Wrap("contract-api", runtime.Logger(), dependencyRouter))
	defer dependency.Close()
	client := observability.NewHTTPClient(nil, 2*time.Second)
	defer client.CloseIdleConnections()
	router := ghatdrouter.NewRouter(nil, nil).GetRouter()
	router.HandleFunc("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		body, bodyErr := io.ReadAll(r.Body)
		assert.NoError(t, bodyErr)
		assert.Equal(t, contractCanary, string(body))
		ctx, operation := operations.Start(r.Context(), "process-item")
		defer operation.End(nil)
		ghatdlogger.AcquireFrom(ctx).Info("contract operation completed", zap.String("password", contractCanary), zap.Error(errors.New(contractCanary)))
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, dependency.URL+"/dependency/"+contractCanary+"?token="+contractCanary, nil)
		if !assert.NoError(t, requestErr) {
			w.WriteHeader(500)
			return
		}
		response, responseErr := client.Do(request)
		if !assert.NoError(t, responseErr) {
			w.WriteHeader(500)
			return
		}
		_, bodyErr = io.Copy(io.Discard, response.Body)
		assert.NoError(t, bodyErr)
		assert.NoError(t, response.Body.Close())
		monitor.Started(ctx, &event.CommandStartedEvent{Command: command, DatabaseName: "example", CommandName: "find", RequestID: 1, ConnectionID: "localhost:27017"})
		monitor.Failed(ctx, &event.CommandFailedEvent{CommandFinishedEvent: event.CommandFinishedEvent{Duration: time.Millisecond, DatabaseName: "example", CommandName: "find", RequestID: 1, ConnectionID: "localhost:27017"}, Failure: errors.New(contractCanary)})
		redisCommand := redis.NewCmd("GET", contractCanary)
		redisContext, beforeErr := hook.BeforeProcess(ctx, redisCommand)
		assert.NoError(t, beforeErr)
		redisCommand.SetErr(errors.New(contractCanary))
		assert.NoError(t, hook.AfterProcess(redisContext, redisCommand))
		w.WriteHeader(http.StatusNoContent)
	}).Methods(http.MethodPost)
	request := httptest.NewRequest(http.MethodPost, "/items/"+contractCanary+"?token="+contractCanary, strings.NewReader(contractCanary))
	request.Header.Set("User-Agent", contractCanary)
	request.Header.Set("Authorization", contractCanary)
	request.Header.Set("traceparent", "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01")
	response := httptest.NewRecorder()
	otelhttp.Wrap("contract-api", runtime.Logger(), router).ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)
	flushContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, sdk.ForceFlush(flushContext))
	// A completed action's cancelled context must still permit final delivery.
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	require.NoError(t, runtime.Shutdown(cancelled))
}

func assertContract(t *testing.T, snapshot contractSnapshot) {
	t.Helper()
	require.NotEmpty(t, snapshot.traces, "no OTLP traces arrived")
	require.NotEmpty(t, snapshot.metrics, "no OTLP metrics arrived")
	require.NotEmpty(t, snapshot.logs, "no OTLP logs arrived")
	instanceID := ""
	resource := func(value *resourcepb.Resource) {
		require.NotNil(t, value)
		assert.Equal(t, "contract-api", contractAttribute(value.Attributes, "service.name"))
		assert.Equal(t, "example", contractAttribute(value.Attributes, "service.namespace"))
		assert.Equal(t, "test", contractAttribute(value.Attributes, "deployment.environment.name"))
		assert.Equal(t, "1.0.0", contractAttribute(value.Attributes, "service.version"))
		if instanceID == "" {
			instanceID = contractAttribute(value.Attributes, "service.instance.id")
		}
		require.NotEmpty(t, instanceID)
		assert.Equal(t, instanceID, contractAttribute(value.Attributes, "service.instance.id"))
	}
	privacy := func(message proto.Message) {
		encoded, err := protojson.Marshal(message)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), contractCanary)
		assert.NotContains(t, string(encoded), "db.query.text")
	}
	spans := map[string]*tracepb.Span{}
	for _, request := range snapshot.traces {
		privacy(request)
		for _, group := range request.ResourceSpans {
			resource(group.Resource)
			for _, scope := range group.ScopeSpans {
				for _, span := range scope.Spans {
					_, duplicate := spans[span.Name]
					assert.False(t, duplicate, "duplicate span %s", span.Name)
					spans[span.Name] = span
				}
			}
		}
	}
	require.Len(t, spans, 6)
	for _, name := range []string{"POST /items/{id}", "process-item", "HTTP GET", "GET /dependency/{id}", "mongodb.find", "redis.get"} {
		require.Contains(t, spans, name)
	}
	entry, operation, client := spans["POST /items/{id}"], spans["process-item"], spans["HTTP GET"]
	assert.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8}, entry.ParentSpanId)
	assert.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, entry.TraceId)
	assert.Equal(t, entry.SpanId, operation.ParentSpanId)
	assert.Equal(t, operation.SpanId, client.ParentSpanId)
	assert.Equal(t, client.SpanId, spans["GET /dependency/{id}"].ParentSpanId)
	assert.Equal(t, tracepb.Span_SPAN_KIND_SERVER, entry.Kind)
	assert.Equal(t, tracepb.Span_SPAN_KIND_INTERNAL, operation.Kind)
	assert.Equal(t, tracepb.Span_SPAN_KIND_CLIENT, client.Kind)
	for _, span := range spans {
		assert.Equal(t, entry.TraceId, span.TraceId)
	}
	for _, name := range []string{"mongodb.find", "redis.get"} {
		assert.Equal(t, operation.SpanId, spans[name].ParentSpanId)
		assert.Equal(t, tracepb.Status_STATUS_CODE_ERROR, spans[name].GetStatus().Code)
	}
	var records []*logpb.LogRecord
	for _, request := range snapshot.logs {
		privacy(request)
		for _, group := range request.ResourceLogs {
			resource(group.Resource)
			for _, scope := range group.ScopeLogs {
				records = append(records, scope.LogRecords...)
			}
		}
	}
	require.Len(t, records, 3)
	operationLog, requestLogs := 0, 0
	for _, record := range records {
		assert.Equal(t, entry.TraceId, record.TraceId)
		switch record.Body.GetStringValue() {
		case "contract operation completed":
			operationLog++
			assert.Equal(t, operation.SpanId, record.SpanId)
		case "http request completed":
			requestLogs++
			route := contractAttribute(record.Attributes, "route")
			if route == "/items/{id}" {
				assert.Equal(t, entry.SpanId, record.SpanId)
			} else {
				assert.Equal(t, "/dependency/{id}", route)
				assert.Equal(t, spans["GET /dependency/{id}"].SpanId, record.SpanId)
			}
		default:
			t.Error("unexpected log body")
		}
	}
	assert.Equal(t, 1, operationLog)
	assert.Equal(t, 2, requestLogs)
	metrics := map[string][]*metricpb.Metric{}
	for _, request := range snapshot.metrics {
		privacy(request)
		for _, group := range request.ResourceMetrics {
			resource(group.Resource)
			for _, scope := range group.ScopeMetrics {
				for _, instrument := range scope.Metrics {
					metrics[instrument.Name] = append(metrics[instrument.Name], instrument)
				}
			}
		}
	}
	for _, name := range []string{"http.server.request.duration", "http.client.request.duration", "db.client.operation.duration", "db.client.operation.errors", "contract.operation.count", "contract.operation.duration", "contract.operation.active", "go.goroutine.count"} {
		require.Contains(t, metrics, name)
	}
	exemplarFound := false
	for _, instrument := range metrics["contract.operation.duration"] {
		assert.Equal(t, "s", instrument.Unit)
		require.NotNil(t, instrument.GetHistogram())
		require.Len(t, instrument.GetHistogram().DataPoints, 1)
		point := instrument.GetHistogram().DataPoints[0]
		assert.EqualValues(t, 1, point.Count)
		assert.Equal(t, []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}, point.ExplicitBounds)
		assert.Equal(t, "process-item", contractAttribute(point.Attributes, "operation"))
		assert.Equal(t, "success", contractAttribute(point.Attributes, "outcome"))
		for _, exemplar := range point.Exemplars {
			exemplarFound = true
			assert.Equal(t, operation.TraceId, exemplar.TraceId)
			assert.Equal(t, operation.SpanId, exemplar.SpanId)
		}
	}
	assert.True(t, exemplarFound, "sampled business latency must retain a trace exemplar")
	for _, instrument := range metrics["http.server.request.duration"] {
		assert.Equal(t, "s", instrument.Unit)
		require.NotNil(t, instrument.GetHistogram())
		var count uint64
		for _, point := range instrument.GetHistogram().DataPoints {
			count += point.Count
			assert.Contains(t, []string{"/items/{id}", "/dependency/{id}"}, contractAttribute(point.Attributes, "http.route"))
			assert.Contains(t, []string{"POST", "GET"}, contractAttribute(point.Attributes, "http.request.method"))
		}
		assert.EqualValues(t, 2, count)
	}
	databaseSystems := map[string]bool{}
	for _, instrument := range metrics["db.client.operation.duration"] {
		assert.Equal(t, "s", instrument.Unit)
		require.NotNil(t, instrument.GetHistogram())
		for _, point := range instrument.GetHistogram().DataPoints {
			system := contractAttribute(point.Attributes, "db.system.name")
			assert.Contains(t, []string{"mongodb", "redis"}, system)
			databaseSystems[system] = true
			assert.EqualValues(t, 1, point.Count)
			assert.NotEmpty(t, contractAttribute(point.Attributes, "error.type"))
		}
	}
	assert.Len(t, databaseSystems, 2)
	for _, name := range []string{"count", "active"} {
		for _, instrument := range metrics["contract.operation."+name] {
			assert.Empty(t, instrument.Unit)
			require.NotNil(t, instrument.GetSum())
			require.Len(t, instrument.GetSum().DataPoints, 1)
			want := int64(1)
			if name == "active" {
				want = 0
			}
			assert.Equal(t, want, instrument.GetSum().DataPoints[0].GetAsInt())
		}
	}
}

func contractAttribute(attributes []*commonpb.KeyValue, key string) string {
	for _, item := range attributes {
		if item.Key == key {
			return item.Value.GetStringValue()
		}
	}
	return ""
}
