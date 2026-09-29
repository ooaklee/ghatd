package observability_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/observability/otelhttp"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestServerHTTPOptionsComposeHostPoliciesAndOwnTheirInputs(t *testing.T) {
	spans := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans), sdktrace.WithSampler(observability.HTTPTraceSampler(sdktrace.AlwaysSample())))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	core, logs := observer.New(zap.InfoLevel)
	config := observability.HTTPServerOptionsConfig{
		RequestLogDetails: true, PreservePathParameters: true,
		RedactPathPrefixes: []string{"/private/"}, TrustedProxyCIDRs: " 10.0.0.0/24 , 192.0.2.0/24 ",
		TraceNoiseSuppression: true, TraceSuppressedPaths: []string{"/health"},
	}
	options, err := observability.NewHTTPServerOptions(config)
	require.NoError(t, err)
	config.RedactPathPrefixes[0] = "/changed/"
	config.TraceSuppressedPaths[0] = "/changed"
	router := ghatdrouter.NewRouter(nil, nil).GetRouter()
	router.HandleFunc("/private/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	router.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	handler := otelhttp.WrapWithOptions("example", zap.New(core), router, options...)
	request := httptest.NewRequest("GET", "/private/secret-canary?token=secret-canary", nil)
	request.RemoteAddr = "10.0.0.1:1234"
	request.Header.Set("User-Agent", "fixture-agent")
	request.Header.Set("X-Forwarded-For", "198.51.100.1, 192.0.2.3")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/health", nil))
	require.Len(t, spans.GetSpans(), 1, "the copied suppression policy drops only the selected root")
	entries := logs.FilterMessage("http request completed").All()
	require.Len(t, entries, 2, "suppression leaves completion logs intact")
	fields := entries[0].ContextMap()
	assert.Equal(t, "/private/[redacted]", fields["url.path"])
	assert.Equal(t, "fixture-agent", fields["user_agent.original"])
	assert.Equal(t, "198.51.100.1", fields["client.address"])
	assert.Equal(t, "forwarded", fields["client.address.source"])
	for _, entry := range entries {
		encoded, err := json.Marshal(entry.ContextMap())
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "secret-canary", "no logged field may expose the redacted path or query")
	}
}

func TestServerHTTPOptionsValidateOnlyEnabledPoliciesWithoutEchoingValues(t *testing.T) {
	for _, config := range []observability.HTTPServerOptionsConfig{
		{RequestLogDetails: true, TrustedProxyCIDRs: "private-canary"},
		{RequestLogDetails: true, TrustedProxyCIDRs: "10.0.0.0/24,"},
		{RequestLogDetails: true, RedactPathPrefixes: []string{"private-canary"}},
		{TraceNoiseSuppression: true, TraceSuppressedPaths: []string{"private-canary"}},
	} {
		_, err := observability.NewHTTPServerOptions(config)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "private-canary")
		config.RequestLogDetails, config.TraceNoiseSuppression = false, false
		options, err := observability.NewHTTPServerOptions(config)
		require.NoError(t, err)
		assert.Empty(t, options)
	}
	_, err := observability.NewHTTPServerOptions(observability.HTTPServerOptionsConfig{RequestLogDetails: true, TrustedProxyCIDRs: "  "})
	require.NoError(t, err, "blank proxy settings retain the untrusted default")
}
