package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// Keep the original exported function assignable to its pre-option type.
var _ func(string) func(http.Handler) http.Handler = HTTPServerMiddleware

func TestHTTPTracePolicyRejectsUnsafePathsWithoutEchoingInput(t *testing.T) {
	for _, path := range []string{
		"", "/", "private-canary", "/private-canary//health", "/private-canary/../health",
		"/private-canary/./health", "/private-canary/*", "/private-canary/{id}",
		"/private-canary/%68ealth", "/private-canary?token=secret", "/private-canary#fragment",
		"/private-canary\\health", "/private-canary/health\n", "/private-canary/health\t",
		"/private-canary/é", "/private-canary/health page", "/private-canary/[abc]",
		"/private-canary/health\x7f", "/private-canary/\xff", "/" + strings.Repeat("x", 256),
	} {
		for _, prefix := range []bool{false, true} {
			t.Run(fmt.Sprintf("case-%d-prefix-%t", len(path), prefix), func(t *testing.T) {
				var exactPaths, prefixes []string
				if prefix {
					prefixes = []string{path}
				} else {
					exactPaths = []string{path}
				}
				policy, err := NewHTTPTracePolicy(exactPaths, prefixes)
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "private-canary")
				assert.Empty(t, policy.exact)
				assert.Empty(t, policy.prefixes)
			})
		}
	}
	_, err := NewHTTPTracePolicy(nil, []string{"/assets"})
	require.Error(t, err, "prefixes require a directory boundary")
	_, err = NewHTTPTracePolicy(make([]string, 65), nil)
	require.Error(t, err)
	_, err = NewHTTPTracePolicy(make([]string, 32), make([]string, 33))
	require.Error(t, err)
	paths := make([]string, 64)
	for index := range paths {
		paths[index] = fmt.Sprintf("/health-%d", index)
	}
	_, err = NewHTTPTracePolicy(paths, nil)
	require.NoError(t, err)
	_, err = NewHTTPTracePolicy([]string{"/" + strings.Repeat("x", 255)}, nil)
	require.NoError(t, err)
}

func TestHTTPTracePolicyMatchesOnlyCanonicalGETHEADPaths(t *testing.T) {
	policy, err := NewHTTPTracePolicy([]string{"/healthz", "/health/", "/favicon.ico"}, []string{"/assets/"})
	require.NoError(t, err)
	for _, test := range []struct {
		method, target string
		match          bool
	}{
		{http.MethodGet, "/healthz", true}, {http.MethodHead, "/healthz", true},
		{http.MethodGet, "/healthz?private-query=value", true},
		{http.MethodGet, "/health/", true}, {http.MethodGet, "/health", false},
		{http.MethodPost, "/healthz", false}, {http.MethodOptions, "/healthz", false},
		{"private-method", "/healthz", false}, {http.MethodGet, "/HEALTHZ", false},
		{http.MethodGet, "/assets/app.js", true}, {http.MethodHead, "/assets/app.js", true},
		{http.MethodGet, "/assets/", true}, {http.MethodGet, "/assets", false},
		{http.MethodGet, "/assets-private/app.js", false}, {http.MethodGet, "/app.css", false},
		{http.MethodGet, "/assets/a/../healthz", false}, {http.MethodGet, "/assets//app.js", false},
		{http.MethodGet, "/assets/a%2Fb.js", false}, {http.MethodGet, "/assets/%61pp.js", false},
		{http.MethodGet, "/assets/a%20b.js", false}, {http.MethodGet, "/%68ealthz", false},
		{http.MethodGet, "/assets/" + strings.Repeat("a", 256), false},
	} {
		t.Run(test.method+test.target, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, nil)
			originalURL, originalURI := request.URL.String(), request.RequestURI
			assert.Equal(t, test.match, policy.matches(request))
			assert.Equal(t, originalURL, request.URL.String())
			assert.Equal(t, originalURI, request.RequestURI)
		})
	}
	assert.False(t, policy.matches(nil))
	assert.False(t, policy.matches(&http.Request{Method: http.MethodGet}))
	assert.False(t, (HTTPTracePolicy{}).matches(httptest.NewRequest(http.MethodGet, "/healthz", nil)))
}

func TestHTTPTracePolicyOwnsInputsAndPreservesInheritedMarker(t *testing.T) {
	exactPaths, prefixes := []string{"/healthz"}, []string{"/assets/"}
	policy, err := NewHTTPTracePolicy(exactPaths, prefixes)
	require.NoError(t, err)
	exactPaths[0], prefixes[0] = "/ordinary", "/api/"
	assert.True(t, policy.matches(httptest.NewRequest(http.MethodGet, "/healthz", nil)))
	assert.True(t, policy.matches(httptest.NewRequest(http.MethodGet, "/assets/file.js", nil)))
	assert.False(t, policy.matches(httptest.NewRequest(http.MethodGet, "/ordinary", nil)))
	assert.False(t, policy.matches(httptest.NewRequest(http.MethodGet, "/api/work", nil)))
	base := context.Background()
	assert.True(t, base == contextWithHTTPTracePolicy(base, policy, httptest.NewRequest(http.MethodGet, "/ordinary", nil)))
	marked := contextWithHTTPTracePolicy(base, policy, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	inherited := contextWithHTTPTracePolicy(marked, HTTPTracePolicy{}, httptest.NewRequest(http.MethodGet, "/ordinary", nil))
	assert.Same(t, marked, inherited)
	assert.Equal(t, true, inherited.Value(httpTraceSuppressionKey{}))
	assert.Nil(t, base.Value(httpTraceSuppressionKey{}))
	var config httpServerConfiguration
	WithHTTPTracePolicy(policy).applyHTTPServer(&config)
	WithHTTPTracePolicy(HTTPTracePolicy{}).applyHTTPServer(&config)
	assert.False(t, config.tracePolicy.matches(httptest.NewRequest(http.MethodGet, "/healthz", nil)), "last policy option replaces rather than accumulates")
}

type httpSamplingDelegate struct {
	result   sdktrace.SamplingResult
	calls    int
	received sdktrace.SamplingParameters
}

func (delegate *httpSamplingDelegate) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	delegate.calls++
	delegate.received = parameters
	return delegate.result
}
func (*httpSamplingDelegate) Description() string { return "delegate" }

func TestHTTPTraceSamplerPreservesDelegateAndSampledParents(t *testing.T) {
	state, err := trace.ParseTraceState("vendor=opaque")
	require.NoError(t, err)
	for _, marked := range []bool{false, true} {
		for _, parentKind := range []string{"none", "invalid sampled", "local unsampled", "remote unsampled", "local sampled", "remote sampled"} {
			t.Run(fmt.Sprintf("marked-%t/%s", marked, parentKind), func(t *testing.T) {
				parentConfig := trace.SpanContextConfig{TraceState: state}
				if parentKind != "none" && parentKind != "invalid sampled" {
					parentConfig.TraceID, parentConfig.SpanID = trace.TraceID{1}, trace.SpanID{2}
				}
				if strings.Contains(parentKind, "sampled") && !strings.Contains(parentKind, "unsampled") {
					parentConfig.TraceFlags = trace.FlagsSampled
				}
				parentConfig.Remote = strings.HasPrefix(parentKind, "remote")
				parent := trace.NewSpanContext(parentConfig)
				ctx := trace.ContextWithSpanContext(context.Background(), parent)
				if marked {
					ctx = context.WithValue(ctx, httpTraceSuppressionKey{}, true)
				}
				delegate := &httpSamplingDelegate{result: sdktrace.SamplingResult{Decision: sdktrace.RecordOnly, Attributes: []attribute.KeyValue{attribute.String("trusted", "value")}, Tracestate: state}}
				parameters := sdktrace.SamplingParameters{ParentContext: ctx, TraceID: trace.TraceID{3}, Name: "work", Kind: trace.SpanKindInternal, Attributes: []attribute.KeyValue{attribute.Int("input", 1)}, Links: []trace.Link{{SpanContext: parent}}}
				result := HTTPTraceSampler(delegate).ShouldSample(parameters)
				if marked && !(parent.IsValid() && parent.IsSampled()) {
					assert.Equal(t, sdktrace.Drop, result.Decision)
					assert.Empty(t, result.Attributes)
					assert.Equal(t, state, result.Tracestate)
					assert.Zero(t, delegate.calls)
				} else {
					assert.Equal(t, delegate.result, result)
					assert.Equal(t, parameters, delegate.received)
					assert.Equal(t, 1, delegate.calls)
				}
				assert.Equal(t, "HTTPTraceSampler(delegate)", HTTPTraceSampler(delegate).Description())
			})
		}
	}
	assert.Equal(t, sdktrace.RecordAndSample, HTTPTraceSampler(nil).ShouldSample(sdktrace.SamplingParameters{ParentContext: context.Background()}).Decision)
	assert.Equal(t, sdktrace.RecordAndSample, HTTPTraceSampler(sdktrace.AlwaysSample()).ShouldSample(sdktrace.SamplingParameters{}).Decision)
}

func TestHTTPTraceSamplerDropsAllLocalKindsAndKeepsValidPropagation(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(HTTPTraceSampler(sdktrace.AlwaysSample())))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	state, err := trace.ParseTraceState("vendor=opaque")
	require.NoError(t, err)
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceState: state, Remote: true})
	ctx := context.WithValue(trace.ContextWithRemoteSpanContext(context.Background(), parent), httpTraceSuppressionKey{}, true)
	for _, kind := range []trace.SpanKind{trace.SpanKindServer, trace.SpanKindInternal, trace.SpanKindClient, trace.SpanKindProducer, trace.SpanKindConsumer} {
		var span trace.Span
		ctx, span = provider.Tracer("sampling-test").Start(ctx, "work", trace.WithSpanKind(kind))
		assert.False(t, span.IsRecording())
		assert.True(t, span.SpanContext().IsValid())
		assert.False(t, span.SpanContext().IsSampled())
		assert.Equal(t, parent.TraceID(), span.SpanContext().TraceID())
		assert.NotEqual(t, parent.SpanID(), span.SpanContext().SpanID())
		assert.Equal(t, state, span.SpanContext().TraceState())
		span.End()
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	assert.True(t, strings.HasSuffix(carrier.Get("traceparent"), "-00"))
	assert.Equal(t, "vendor=opaque", carrier.Get("tracestate"))
	assert.Empty(t, carrier.Get("baggage"))
	assert.Len(t, carrier, 2, "the suppression marker must remain process-local")
	_, restarted := provider.Tracer("sampling-test").Start(ctx, "restarted", trace.WithNewRoot())
	assert.False(t, restarted.IsRecording())
	restarted.End()
	assert.Empty(t, exporter.GetSpans())
	_, ordinary := provider.Tracer("sampling-test").Start(context.Background(), "ordinary")
	assert.True(t, ordinary.IsRecording())
	ordinary.End()
	assert.Len(t, exporter.GetSpans(), 1)
}

func TestHTTPTracePolicyOuterBoundaryPrecedesRedactionAndIsolatesConcurrentRequests(t *testing.T) {
	policy, err := NewHTTPTracePolicy([]string{"/healthz"}, nil)
	require.NoError(t, err)
	exporter := tracetest.NewInMemoryExporter()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(HTTPTraceSampler(sdktrace.AlwaysSample())))
	reader := sdkmetric.NewManualReader()
	metrics := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, traces.Shutdown(context.Background()))
		require.NoError(t, metrics.Shutdown(context.Background()))
	})
	options := []otelhttp.Option{otelhttp.WithTracerProvider(traces), otelhttp.WithMeterProvider(metrics), otelhttp.WithPropagators(propagation.TraceContext{})}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "private-query=canary", request.URL.RawQuery)
		assert.Equal(t, "private-header", request.Header.Get("Authorization"))
		wantRecording := request.URL.Path != "/healthz"
		assert.Equal(t, wantRecording, trace.SpanFromContext(request.Context()).IsRecording())
		_, child := traces.Tracer("sampling-test").Start(request.Context(), "child")
		assert.Equal(t, wantRecording, child.IsRecording())
		child.End()
		writer.WriteHeader(204)
	})
	// A matched inner boundary must not alter the already started outer span.
	inner := httpServerMiddlewareWithPolicy("example", policy, options...)(handler)
	outer := httpServerMiddlewareWithPolicy("example", policy, options...)(inner)
	var group sync.WaitGroup
	for index := range 64 {
		group.Go(func() {
			path := "/healthz"
			if index%2 == 1 {
				path = "/ordinary"
			}
			request := httptest.NewRequest(http.MethodGet, path+"?private-query=canary", nil)
			request.Header.Set("Authorization", "private-header")
			writer := httptest.NewRecorder()
			outer.ServeHTTP(writer, request)
			assert.Equal(t, 204, writer.Code)
		})
	}
	group.Wait()
	assert.Len(t, exporter.GetSpans(), 64, "32 ordinary requests, one server and one child span each")
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))
	var count uint64
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == "http.server.request.duration" {
				for _, point := range metric.Data.(metricdata.Histogram[float64]).DataPoints {
					count += point.Count
				}
			}
		}
	}
	assert.EqualValues(t, 64, count, "suppression and nested installation must retain one measurement per request")
	for _, data := range []any{collected, exporter.GetSpans()} {
		encoded, err := json.Marshal(data)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "private-")
		assert.NotContains(t, string(encoded), "canary")
	}
	// Only the inner boundary has policy here. Its duplicate guard must skip
	// policy evaluation, leaving the existing outer request trace intact.
	before := len(exporter.GetSpans())
	check := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.True(t, trace.SpanFromContext(request.Context()).IsRecording())
		writer.WriteHeader(204)
	})
	innerOnly := httpServerMiddlewareWithPolicy("example", policy, options...)(check)
	httpServerMiddleware("example", options...)(innerOnly).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Len(t, exporter.GetSpans(), before+1)
}

func TestHTTPTracePolicyRejectsExplicitRawPathEvenIfDecodedPathMatches(t *testing.T) {
	policy, err := NewHTTPTracePolicy([]string{"/healthz"}, nil)
	require.NoError(t, err)
	assert.False(t, policy.matches(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/healthz", RawPath: "/healthz"}}))
}

func TestHTTPTraceSamplerStartPreservesEnvironmentAndDisabledOverride(t *testing.T) {
	for _, test := range []struct {
		name, sampler, ratio              string
		disabled, ordinary, sampledParent bool
	}{
		{name: "default", ordinary: true, sampledParent: true},
		{name: "always on", sampler: "always_on", ordinary: true, sampledParent: true},
		{name: "always off", sampler: "always_off"},
		{name: "parent ratio zero", sampler: "parentbased_traceidratio", ratio: "0", sampledParent: true},
		{name: "parent ratio one", sampler: "parentbased_traceidratio", ratio: "1", ordinary: true, sampledParent: true},
		{name: "nonparent ratio zero", sampler: "traceidratio", ratio: "0"},
		{name: "disabled", sampler: "always_on", disabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearRuntimeOTELTestEnvironment(t)
			restoreRuntimeTestGlobals(t)
			disableExporters(t)
			exporter := tracetest.NewInMemoryExporter()
			name := fmt.Sprintf("http-sampling-test-%d", resourceTestExporterID.Add(1))
			autoexport.RegisterSpanExporter(name, func(context.Context) (sdktrace.SpanExporter, error) { return exporter, nil })
			if !test.disabled {
				t.Setenv("OTEL_TRACES_EXPORTER", name)
			}
			t.Setenv("OTEL_TRACES_SAMPLER", test.sampler)
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", test.ratio)
			sdk, err := Start(context.Background(), Config{ServiceName: "sampling-test"})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sdk.Shutdown(context.Background())) })
			tracer := sdk.TracerProvider().Tracer("sampling-test")
			_, ordinary := tracer.Start(context.Background(), "ordinary")
			assert.Equal(t, test.ordinary, ordinary.IsRecording())
			ordinary.End()
			marked := context.WithValue(context.Background(), httpTraceSuppressionKey{}, true)
			_, suppressed := tracer.Start(marked, "suppressed")
			assert.False(t, suppressed.IsRecording())
			suppressed.End()
			parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled, Remote: true})
			_, parented := tracer.Start(trace.ContextWithRemoteSpanContext(marked, parent), "parented")
			assert.Equal(t, test.sampledParent, parented.IsRecording())
			parented.End()
			require.NoError(t, sdk.ForceFlush(context.Background()))
			expected := 0
			if test.ordinary {
				expected++
			}
			if test.sampledParent {
				expected++
			}
			assert.Len(t, exporter.GetSpans(), expected)
		})
	}
}
