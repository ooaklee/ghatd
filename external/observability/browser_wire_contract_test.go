package observability_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	ghatdhttp "github.com/ooaklee/ghatd/external/observability/otelhttp"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

const browserContractCanary = "synthetic-private-browser-input"
const browserContractPath = "/api/v1/telemetry/browser/traces"

// TestBrowserWireContract runs the actual official JavaScript SDK/serializer,
// the complete Go HTTP boundary and intake, and the pinned Collector. It does
// not substitute a captured JSON example or model the serializer in Go.
func TestBrowserWireContract(t *testing.T) {
	if os.Getenv("GHATD_TEST_COLLECTOR") != "1" {
		t.Skip("set GHATD_TEST_COLLECTOR=1 to run the real browser/Collector contract")
	}
	for _, command := range []string{"docker", "node"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Fatal("browser contract requires Docker, Node and installed fixture dependencies")
		}
	}
	if _, err := os.Stat("testdata/browser/node_modules/@opentelemetry/otlp-transformer/package.json"); err != nil {
		t.Fatal("run npm ci --prefix external/observability/testdata/browser --ignore-scripts before the browser contract")
	}
	collectorDocker(t, "find pinned browser contract image", "image", "inspect", collectorContractImage)
	for _, protocol := range []string{"http/protobuf", "grpc"} {
		for _, mode := range []string{"clean", "hostile"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) { exerciseBrowserWireContract(t, protocol, mode) })
		}
	}
}

func exerciseBrowserWireContract(t *testing.T, protocol, mode string) {
	t.Helper()
	collector := startContractCollector(t)
	port := "4318/tcp"
	if protocol == "grpc" {
		port = "4317/tcp"
	}
	configureContract(t, protocol, "http://"+collector.addresses[port])
	core, local := observer.New(zap.InfoLevel)
	runtime, err := observability.StartRuntime(context.Background(), observability.RuntimeConfig{
		Telemetry: observability.Config{ServiceName: "browser-contract-api", Namespace: "example", Environment: "test", Version: "1.0.0"},
		Logger:    zap.New(core), ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(nil)) })
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	intake, err := observability.NewBrowserTraceIntake(observability.BrowserTraceIntakeConfig{
		ServiceName: "browser-contract-web", Namespace: "example", Environment: "test", Version: "1.0.0",
		AllowedOrigins: []string{origin}, RouteGroups: []string{"home"}, APIGroups: []string{"orders"},
		MeterProvider: runtime.SDK().MeterProvider(),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, intake.Shutdown(ctx))
	})
	router := ghatdrouter.NewRouter(nil, nil).GetRouter()
	router.HandleFunc("/api/v1/orders/{id}", func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, browserContractCanary, request.URL.Query().Get("private"))
		assert.Equal(t, browserContractCanary, request.Header.Get("Authorization"))
		assert.Empty(t, request.Header.Get("tracestate"), "the JavaScript propagator setter only permits traceparent")
		var body map[string]string
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		assert.Equal(t, browserContractCanary, body["private"])
		sc := trace.SpanContextFromContext(request.Context())
		assert.True(t, sc.IsValid())
		ghatdlogger.AcquireFrom(request.Context()).Info("browser contract request handled", zap.String("password", browserContractCanary))
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]string{"traceId": sc.TraceID().String(), "spanId": sc.SpanID().String()})
	}).Methods(http.MethodPost)
	router.HandleFunc(browserContractPath, func(writer http.ResponseWriter, request *http.Request) {
		for _, name := range []string{"Authorization", "Cookie", "traceparent", "tracestate", "baggage"} {
			assert.Empty(t, request.Header.Get(name), "intake export has no credentials or propagated browser context")
		}
		intake.ServeHTTP(writer, request)
	}).Methods(http.MethodPost)
	server.Config.Handler = ghatdhttp.Wrap("browser-contract-api", runtime.Logger(), router)
	server.Start()
	t.Cleanup(server.Close)
	result := runBrowserFixture(t, origin, mode)
	wire, err := base64.StdEncoding.DecodeString(result.Wire)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, result.IntakeStatus)
	assert.ElementsMatch(t, []string{"Content-Type", "Origin"}, result.IntakeHeaders)
	if mode == "hostile" {
		assert.Contains(t, string(wire), browserContractCanary, "the intake must rebuild hostile but valid official serializer output")
	} else {
		assert.NotContains(t, string(wire), browserContractCanary, "ordinary manual spans exclude actual API URL/body/auth data")
	}
	expected := browserWireSpans(t, wire)
	intakeRequests := 1
	if mode == "hostile" {
		// Start with real serializer bytes and append one invalid span after
		// valid ones. Atomic rejection must not forward any duplicate spans.
		var envelope map[string]any
		require.NoError(t, json.Unmarshal(wire, &envelope))
		scope := envelope["resourceSpans"].([]any)[0].(map[string]any)["scopeSpans"].([]any)[0].(map[string]any)
		spans := scope["spans"].([]any)
		invalid := map[string]any{}
		for key, value := range spans[0].(map[string]any) {
			invalid[key] = value
		}
		invalid["name"] = "browser.invalid"
		scope["spans"] = append(spans, invalid)
		payload, err := json.Marshal(envelope)
		require.NoError(t, err)
		request, err := http.NewRequest(http.MethodPost, origin+browserContractPath, bytes.NewReader(payload))
		require.NoError(t, err)
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		client := &http.Client{Transport: &http.Transport{}, Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		response, err := client.Do(request)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, http.StatusBadRequest, response.StatusCode)
		assert.Empty(t, body)
		intakeRequests++
	}
	server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, intake.Shutdown(ctx))
	require.NoError(t, runtime.Shutdown(nil))
	snapshot := (&resilienceCollector{name: collector.name}).finish(t)
	assertBrowserContract(t, snapshot, expected, result, intakeRequests)
	assert.Len(t, local.FilterMessage("http request completed").All(), 1+intakeRequests)
	localRequest := local.FilterMessage("browser contract request handled").All()
	require.Len(t, localRequest, 1)
	assert.Equal(t, browserContractCanary, localRequest[0].ContextMap()["password"], "existing local application log behavior is unchanged")
}

type browserFixtureResult struct {
	Wire, Traceparent string
	Business          struct{ TraceID, SpanID string }
	IntakeStatus      int
	IntakeHeaders     []string
}

func runBrowserFixture(t *testing.T, origin, mode string) browserFixtureResult {
	t.Helper()
	input, err := json.Marshal(map[string]string{"origin": origin, "mode": mode})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "contract.mjs")
	command.Dir = filepath.Join("testdata", "browser")
	command.Stdin = bytes.NewReader(input)
	output := &browserOutputBuffer{limit: 128 << 10}
	command.Stdout, command.Stderr = output, io.Discard
	if err := command.Run(); err != nil {
		t.Fatal("official JavaScript browser contract failed; verify fixture dependencies, serializer compatibility and intake acceptance")
	}
	var result browserFixtureResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal("JavaScript browser contract returned an invalid bounded result")
	}
	return result
}

type browserOutputBuffer struct {
	bytes.Buffer
	limit int
}

func (buffer *browserOutputBuffer) Write(value []byte) (int, error) {
	if len(value) > buffer.limit-buffer.Len() {
		return 0, errors.New("browser contract output limit exceeded")
	}
	return buffer.Buffer.Write(value)
}

type browserWireSpan struct {
	Name              string `json:"name"`
	TraceID           string `json:"traceId"`
	SpanID            string `json:"spanId"`
	ParentSpanID      string `json:"parentSpanId"`
	StartTimeUnixNano string `json:"startTimeUnixNano"`
	EndTimeUnixNano   string `json:"endTimeUnixNano"`
	Kind              int    `json:"kind"`
	Attributes        []struct {
		Key   string         `json:"key"`
		Value map[string]any `json:"value"`
	} `json:"attributes"`
}

func browserWireSpans(t *testing.T, wire []byte) map[string]browserWireSpan {
	t.Helper()
	var envelope struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []browserWireSpan `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	require.NoError(t, json.Unmarshal(wire, &envelope), "official serializer uses hex ID strings and decimal-string nanosecond times")
	require.Len(t, envelope.ResourceSpans, 1)
	require.Len(t, envelope.ResourceSpans[0].ScopeSpans, 1)
	spans := envelope.ResourceSpans[0].ScopeSpans[0].Spans
	require.Len(t, spans, 4)
	result := make(map[string]browserWireSpan)
	for _, span := range spans {
		traceID, err := hex.DecodeString(span.TraceID)
		require.NoError(t, err)
		assert.Len(t, traceID, 16)
		spanID, err := hex.DecodeString(span.SpanID)
		require.NoError(t, err)
		assert.Len(t, spanID, 8)
		assert.NotContains(t, result, span.Name)
		result[span.Name] = span
		for _, attribute := range span.Attributes {
			if attribute.Key == "http.response.status_code" {
				assert.Equal(t, float64(http.StatusOK), attribute.Value["intValue"], "official JS intValue is a JSON number")
			}
		}
	}
	return result
}

func assertBrowserContract(t *testing.T, snapshot contractSnapshot, expected map[string]browserWireSpan, result browserFixtureResult, intakeRequests int) {
	t.Helper()
	privacy := func(message proto.Message) {
		encoded, err := protojson.Marshal(message)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), browserContractCanary)
		assert.NotContains(t, string(encoded), "untrusted-")
	}
	for _, request := range snapshot.traces {
		privacy(request)
	}
	for _, request := range snapshot.metrics {
		privacy(request)
	}
	for _, request := range snapshot.logs {
		privacy(request)
	}
	web := selectResilienceService(snapshot, "browser-contract-web")
	spans := map[string]*tracepb.Span{}
	for _, request := range web.traces {
		for _, resource := range request.ResourceSpans {
			assert.Empty(t, resource.SchemaUrl)
			assert.Len(t, resource.Resource.Attributes, 4)
			assert.Equal(t, "example", contractAttribute(resource.Resource.Attributes, "service.namespace"))
			assert.Equal(t, "test", contractAttribute(resource.Resource.Attributes, "deployment.environment.name"))
			assert.Equal(t, "1.0.0", contractAttribute(resource.Resource.Attributes, "service.version"))
			assert.Empty(t, contractAttribute(resource.Resource.Attributes, "service.instance.id"))
			for _, scope := range resource.ScopeSpans {
				assert.Empty(t, scope.SchemaUrl)
				assert.Equal(t, "github.com/ooaklee/ghatd/browser", scope.Scope.Name)
				assert.Empty(t, scope.Scope.Version)
				assert.Empty(t, scope.Scope.Attributes)
				for _, span := range scope.Spans {
					assert.NotContains(t, spans, span.Name, "invalid mixed batches must not forward their valid prefix")
					spans[span.Name] = span
				}
			}
		}
	}
	require.Len(t, spans, 4)
	for _, name := range []string{"browser.navigation", "browser.request", "browser.document", "browser.error"} {
		require.Contains(t, spans, name)
		require.Contains(t, expected, name)
		span, original := spans[name], expected[name]
		assert.Equal(t, original.TraceID, hex.EncodeToString(span.TraceId))
		assert.Equal(t, original.SpanID, hex.EncodeToString(span.SpanId))
		assert.Equal(t, original.ParentSpanID, hex.EncodeToString(span.ParentSpanId))
		start, err := strconv.ParseUint(original.StartTimeUnixNano, 10, 64)
		require.NoError(t, err)
		end, err := strconv.ParseUint(original.EndTimeUnixNano, 10, 64)
		require.NoError(t, err)
		assert.Equal(t, start, span.StartTimeUnixNano)
		assert.Equal(t, end, span.EndTimeUnixNano)
		assert.EqualValues(t, original.Kind, span.Kind)
		assert.EqualValues(t, 1, span.Flags&1)
		assert.Empty(t, span.TraceState)
		assert.Empty(t, span.Events)
		assert.Empty(t, span.Links)
		assert.Empty(t, span.GetStatus().Message)
		status := tracepb.Status_STATUS_CODE_UNSET
		if name == "browser.error" {
			status = tracepb.Status_STATUS_CODE_ERROR
		}
		assert.Equal(t, status, span.GetStatus().Code, "status comes from the validated outcome, not a supplied error status/message")
		assert.Equal(t, "home", contractAttribute(span.Attributes, "browser.route.group"))
	}
	assert.Equal(t, "complete", contractAttribute(spans["browser.navigation"].Attributes, "browser.outcome"))
	assert.Equal(t, "success", contractAttribute(spans["browser.request"].Attributes, "browser.outcome"))
	assert.Equal(t, "orders", contractAttribute(spans["browser.request"].Attributes, "browser.api.group"))
	assert.Equal(t, "POST", contractAttribute(spans["browser.request"].Attributes, "http.request.method"))
	assert.EqualValues(t, http.StatusOK, samplingIntAttribute(spans["browser.request"].Attributes, "http.response.status_code"))
	assert.Equal(t, "window", contractAttribute(spans["browser.error"].Attributes, "browser.error.source"))
	assert.Equal(t, "type-error", contractAttribute(spans["browser.error"].Attributes, "error.type"))
	for name, value := range map[string]float64{"browser.document.ttfb_ms": 12.5, "browser.document.dom_content_loaded_ms": 25.25, "browser.document.load_ms": 35.75} {
		assert.Equal(t, value, browserDoubleAttribute(spans["browser.document"].Attributes, name))
	}
	assert.Equal(t, spans["browser.navigation"].SpanId, spans["browser.request"].ParentSpanId)
	assert.Equal(t, spans["browser.navigation"].TraceId, spans["browser.request"].TraceId)
	assert.Equal(t, "00-"+expected["browser.request"].TraceID+"-"+expected["browser.request"].SpanID+"-01", result.Traceparent)

	api := selectResilienceService(snapshot, "browser-contract-api")
	var requestSpan *tracepb.Span
	var intakeSpans []*tracepb.Span
	for _, batch := range api.traces {
		for _, resource := range batch.ResourceSpans {
			assert.NotEmpty(t, contractAttribute(resource.Resource.Attributes, "service.instance.id"))
			for _, scope := range resource.ScopeSpans {
				for _, span := range scope.Spans {
					switch span.Name {
					case "POST /api/v1/orders/{id}":
						require.Nil(t, requestSpan)
						requestSpan = span
					case "POST " + browserContractPath:
						intakeSpans = append(intakeSpans, span)
					default:
						t.Error("unexpected server span in browser contract")
					}
				}
			}
		}
	}
	require.NotNil(t, requestSpan)
	assert.Equal(t, spans["browser.request"].SpanId, requestSpan.ParentSpanId)
	assert.Equal(t, spans["browser.request"].TraceId, requestSpan.TraceId)
	assert.Equal(t, result.Business.TraceID, hex.EncodeToString(requestSpan.TraceId))
	assert.Equal(t, result.Business.SpanID, hex.EncodeToString(requestSpan.SpanId))
	require.Len(t, intakeSpans, intakeRequests)
	for _, span := range intakeSpans {
		assert.Empty(t, span.ParentSpanId)
		assert.NotEqual(t, spans["browser.request"].TraceId, span.TraceId, "intake export starts an independent HTTP trace")
	}
	var logs []*logpb.LogRecord
	for _, request := range api.logs {
		for _, resource := range request.ResourceLogs {
			for _, scope := range resource.ScopeLogs {
				logs = append(logs, scope.LogRecords...)
			}
		}
	}
	require.Len(t, logs, 2+intakeRequests)
	correlated := 0
	for _, record := range logs {
		if contractAttribute(record.Attributes, "route") == "/api/v1/orders/{id}" || record.Body.GetStringValue() == "browser contract request handled" {
			correlated++
			assert.Equal(t, requestSpan.TraceId, record.TraceId)
			assert.Equal(t, requestSpan.SpanId, record.SpanId)
		}
	}
	assert.Equal(t, 2, correlated, "both normal completion and application logs retain the JavaScript-parented server context")
	var measurements []*metricpb.Metric
	for _, request := range api.metrics {
		for _, resource := range request.ResourceMetrics {
			for _, scope := range resource.ScopeMetrics {
				measurements = append(measurements, scope.Metrics...)
			}
		}
	}
	httpCount, accepted, invalid := uint64(0), int64(0), int64(0)
	for _, metric := range measurements {
		if metric.Name == "http.server.request.duration" {
			for _, point := range metric.GetHistogram().GetDataPoints() {
				httpCount += point.Count
			}
		}
		if metric.Name == "ghatd.browser.intake.batch.count" {
			for _, point := range metric.GetSum().GetDataPoints() {
				switch contractAttribute(point.Attributes, "outcome") {
				case "accepted":
					accepted += point.GetAsInt()
				case "invalid":
					invalid += point.GetAsInt()
				default:
					t.Error("unexpected browser intake outcome")
				}
			}
		}
	}
	assert.EqualValues(t, 1+intakeRequests, httpCount)
	assert.EqualValues(t, 1, accepted)
	assert.EqualValues(t, intakeRequests-1, invalid)
}

func browserDoubleAttribute(attributes []*commonpb.KeyValue, key string) float64 {
	for _, attribute := range attributes {
		if attribute.Key == key {
			return attribute.Value.GetDoubleValue()
		}
	}
	return 0
}
