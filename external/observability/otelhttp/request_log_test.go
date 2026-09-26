package otelhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/spa"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestLogPolicyPreservesSPAAndKeepsDetailsOutOfOTLP(t *testing.T) {
	traces := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traces))
	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previousTraces, previousMeters := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(provider)
	otel.SetMeterProvider(meters)
	logs := &logExporter{}
	logProvider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logs)))
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTraces)
		otel.SetMeterProvider(previousMeters)
		require.NoError(t, provider.Shutdown(context.Background()))
		require.NoError(t, meters.Shutdown(context.Background()))
		require.NoError(t, logProvider.Shutdown(context.Background()))
	})
	core, local := observer.New(zap.InfoLevel)
	logger := observability.TeeLogger(zap.New(core), logProvider)
	policy, err := observability.NewHTTPRequestLogPolicy(observability.HTTPRequestLogConfig{
		IncludePath: true, IncludeUserAgent: true, IncludeClientAddress: true,
		RedactPathPrefixes: []string{"/private/"}, TrustedProxyCIDRs: []string{"10.0.0.0/24"},
		MaxPathBytes: 64, MaxUserAgentBytes: 32,
	})
	require.NoError(t, err)
	r := router.NewRouter(nil, nil)
	r.GetRouter().HandleFunc("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		// Even an in-place header mutation must not replace the incoming UA.
		r.Header.Set("User-Agent", "mutated-agent")
		w.WriteHeader(204)
	})
	require.NoError(t, spa.AttachRoutes(&spa.AttachRoutesRequest{Router: r, SpaFileSystem: fstest.MapFS{
		"dist/index.html":    {Data: []byte("<html>SPA entry point</html>")},
		"dist/assets/app.js": {Data: []byte("asset")},
	}}))
	handler := WrapWithOptions("example-api", logger, r.GetRouter(), observability.WithHTTPRequestLogPolicy(policy))
	for _, test := range []struct {
		target, route, loggedPath string
		status                    int
	}{
		{"/dashboard?token=secret-canary", "/", "/dashboard", 200},
		{"/assets/app.js?token=secret-canary", "/", "/assets/app.js", 200},
		{"/items/private-canary?token=secret-canary", "/items/{id}", "/items/{id}", 204},
		{"/private/private-canary", "/", "/private/[redacted]", 200},
		{"/" + strings.Repeat("p", 80), "/", "/" + strings.Repeat("p", 63), 200},
	} {
		request := httptest.NewRequest("GET", test.target, nil)
		request.Header.Set("User-Agent", "original-agent-"+strings.Repeat("a", 50))
		request.Header.Set("X-Forwarded-For", "198.51.100.99, 192.0.2.7, 10.0.0.2")
		request.RemoteAddr = "10.0.0.1:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code)
		if test.status == 200 && !strings.HasPrefix(test.target, "/assets/") {
			require.Contains(t, response.Body.String(), "SPA entry point")
		}
		entries := local.FilterMessage("http request completed").All()
		fields := entries[len(entries)-1].ContextMap()
		require.Equal(t, test.route, fields["route"])
		require.Equal(t, test.loggedPath, fields["url.path"])
		require.Equal(t, "original-agent-"+strings.Repeat("a", 17), fields["user_agent.original"])
		require.Equal(t, true, fields["user_agent.original.truncated"])
		require.Equal(t, "10.0.0.1", fields["network.peer.address"])
		require.Equal(t, "192.0.2.7", fields["client.address"])
		require.NotEmpty(t, fields["trace_id"])
		require.NotEmpty(t, fields["span_id"])
		encoded, err := json.Marshal(fields)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "secret-canary")
		require.NotContains(t, string(encoded), "private-canary")
	}
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	records := logs.Records()
	require.Len(t, records, 5)
	for _, record := range records {
		// SDK log records have private fields; inspect exported attributes rather
		// than relying on json.Marshal(record), which cannot expose their data.
		record.WalkAttributes(func(field otellog.KeyValue) bool {
			for _, prefix := range []string{"url.path", "user_agent.", "network.peer.", "client.address", "http.request.header."} {
				require.False(t, strings.HasPrefix(field.Key, prefix), "request detail escaped into OTLP: %s", field.Key)
			}
			return true
		})
	}
	for _, telemetry := range []any{traces.GetSpans(), metrics} {
		encoded, err := json.Marshal(telemetry)
		require.NoError(t, err)
		for _, forbidden := range []string{"secret-canary", "private-canary", "original-agent", "192.0.2.7", "10.0.0.1", "/dashboard", "/assets/app.js"} {
			require.NotContains(t, string(encoded), forbidden)
		}
	}
}
