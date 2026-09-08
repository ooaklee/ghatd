package observability

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInspectConfigurationDefaultsAreSanitizedAndDoNotChangeGlobals(t *testing.T) {
	clearRuntimeOTELTestEnvironment(t)
	traces, metrics := otel.GetTracerProvider(), otel.GetMeterProvider()
	logs, propagation, handler := otellogglobal.GetLoggerProvider(), otel.GetTextMapPropagator(), otel.GetErrorHandler()
	report, err := InspectConfiguration(Config{})
	require.NoError(t, err)
	assert.Same(t, traces, otel.GetTracerProvider())
	assert.Same(t, metrics, otel.GetMeterProvider())
	assert.Same(t, logs, otellogglobal.GetLoggerProvider())
	assert.Equal(t, propagation, otel.GetTextMapPropagator())
	assert.Equal(t, handler, otel.GetErrorHandler())
	require.Len(t, report.Signals, 3)
	for _, signal := range report.Signals {
		assert.Equal(t, "otlp", signal.Exporter)
		assert.Equal(t, "default", signal.ExporterSource)
		assert.Equal(t, "http/protobuf", signal.Protocol)
		assert.Equal(t, EndpointConfiguration{Source: "default", Scheme: "https", HostKind: "loopback", PathKind: "standard", Port: 4318}, signal.Endpoint)
		assert.True(t, signal.TLS)
		assert.EqualValues(t, 10000, signal.TimeoutMilliseconds)
		assert.False(t, signal.HeadersPresent)
	}
	assert.Equal(t, "parentbased_always_on", report.Sampler)
	assert.EqualValues(t, 60000, report.MetricIntervalMilliseconds)
	assert.EqualValues(t, 30000, report.MetricTimeoutMilliseconds)
	assert.Equal(t, "default", report.Identity.Sources["service.name"])
	assert.Equal(t, "process", report.Identity.Sources["service.instance.id"])
	assert.True(t, report.Identity.InstanceID)
	assert.False(t, report.Identity.Namespace)
	assert.Empty(t, report.Issues)
}

func TestInspectConfigurationDoesNotContactReceiverOrBindPrometheus(t *testing.T) {
	clearRuntimeOTELTestEnvironment(t)
	var requests atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(receiver.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", receiver.URL)
	_, err := InspectConfiguration(Config{})
	require.NoError(t, err)
	assert.Zero(t, requests.Load())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	t.Setenv("OTEL_METRICS_EXPORTER", "prometheus")
	t.Setenv("OTEL_EXPORTER_PROMETHEUS_HOST", "127.0.0.1")
	t.Setenv("OTEL_EXPORTER_PROMETHEUS_PORT", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	report, err := InspectConfiguration(Config{})
	require.NoError(t, err, "inspection must not try to claim an occupied Prometheus port")
	assert.Equal(t, "prometheus", report.Signals[1].Exporter)
	assert.Zero(t, requests.Load())
}

func TestConfigurationResolvesSignalOverridesAndPrivateValues(t *testing.T) {
	const marker = "configuration-private-canary"
	environment := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT":           "https://" + marker + ".example/tenant/",
		"OTEL_EXPORTER_OTLP_HEADERS":            "authorization=base%2Ctoken+with=padding",
		"OTEL_EXPORTER_OTLP_TIMEOUT":            "12000",
		"OTEL_EXPORTER_OTLP_COMPRESSION":        "gzip",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT":    "http://localhost:4317",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL":    "grpc",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS":     "x-token=signal%2Ctoken+with=padding",
		"OTEL_EXPORTER_OTLP_TRACES_TIMEOUT":     "321",
		"OTEL_EXPORTER_OTLP_TRACES_COMPRESSION": "none",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT":   "https://metrics.example/tenant%2Fprivate/v1/metrics",
		"OTEL_SERVICE_NAME":                     marker,
		"OTEL_RESOURCE_ATTRIBUTES":              "service.name=resource-name,service.namespace=" + marker + ",service.instance.id=" + marker + ",service.version=" + marker + ",deployment.environment.name=" + marker,
		"OTEL_TRACES_SAMPLER":                   " PARENTBASED_TRACEIDRATIO ",
		"OTEL_TRACES_SAMPLER_ARG":               " 0.25 ",
		"OTEL_METRIC_EXPORT_INTERVAL":           "5000",
		"OTEL_METRIC_EXPORT_TIMEOUT":            "4000",
	}
	report, resolved, err := resolveConfiguration(Config{ServiceName: marker, Version: marker}, func(key string) string { return environment[key] })
	require.NoError(t, err)
	require.Len(t, resolved.signals, 3)
	traces, metrics, logs := resolved.signals[0], resolved.signals[1], resolved.signals[2]
	assert.Equal(t, "http://localhost:4317", traces.endpoint.String())
	assert.Equal(t, "grpc", traces.protocol)
	assert.True(t, traces.insecure)
	assert.Equal(t, map[string]string{"x-token": "signal,token+with=padding"}, traces.headers)
	assert.Equal(t, 321*time.Millisecond, traces.timeout)
	assert.Equal(t, "none", traces.compression)
	assert.Equal(t, "/tenant/private/v1/metrics", metrics.endpoint.EscapedPath(), "probe must discard RawPath like the pinned SDK")
	assert.Equal(t, "/tenant//v1/logs", logs.endpoint.Path)
	assert.Equal(t, map[string]string{"authorization": "base,token+with=padding"}, logs.headers)
	assert.Equal(t, 12*time.Second, logs.timeout)
	assert.Equal(t, "gzip", logs.compression)
	assert.Equal(t, "application", report.Identity.Sources["service.name"])
	assert.Equal(t, "application", report.Identity.Sources["service.version"])
	assert.Equal(t, "resource", report.Identity.Sources["service.namespace"])
	assert.Equal(t, "resource", report.Identity.Sources["service.instance.id"])
	assert.Equal(t, "resource", report.Identity.Sources["deployment.environment.name"])
	require.NotNil(t, report.SamplerRatio)
	assert.Equal(t, 0.25, *report.SamplerRatio)
	assert.EqualValues(t, 5000, report.MetricIntervalMilliseconds)
	assert.EqualValues(t, 4000, report.MetricTimeoutMilliseconds)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	for _, secret := range []string{marker, "authorization", "signal,token", "tenant", "metrics.example"} {
		assert.NotContains(t, string(encoded), secret)
	}
	assert.Equal(t, "base_trailing_slash", report.Issues[0].Code)
}

func TestConfigurationRejectsMalformedSettingsBeforeUpstreamDiagnostics(t *testing.T) {
	const marker = "configuration-secret-canary"
	tests := []struct {
		name, field, value string
		extra              map[string]string
	}{
		{name: "unknown sampler", field: "OTEL_TRACES_SAMPLER", value: marker},
		{name: "sampler NaN", field: "OTEL_TRACES_SAMPLER_ARG", value: "NaN", extra: map[string]string{"OTEL_TRACES_SAMPLER": "traceidratio"}},
		{name: "sampler infinity", field: "OTEL_TRACES_SAMPLER_ARG", value: "+Inf", extra: map[string]string{"OTEL_TRACES_SAMPLER": "traceidratio"}},
		{name: "sampler ratio above one", field: "OTEL_TRACES_SAMPLER_ARG", value: "1.01", extra: map[string]string{"OTEL_TRACES_SAMPLER": "traceidratio"}},
		{name: "protocol raw", field: "OTEL_EXPORTER_OTLP_PROTOCOL", value: marker},
		{name: "protocol whitespace", field: "OTEL_EXPORTER_OTLP_PROTOCOL", value: " grpc "},
		{name: "url userinfo", field: "OTEL_EXPORTER_OTLP_ENDPOINT", value: "https://" + marker + "@localhost:4318"},
		{name: "url query", field: "OTEL_EXPORTER_OTLP_ENDPOINT", value: "https://localhost:4318?token=" + marker},
		{name: "url fragment", field: "OTEL_EXPORTER_OTLP_ENDPOINT", value: "https://localhost:4318/#" + marker},
		{name: "url encoded newline", field: "OTEL_EXPORTER_OTLP_ENDPOINT", value: "https://localhost:4318/" + marker + "%0A"},
		{name: "url malformed escape", field: "OTEL_EXPORTER_OTLP_ENDPOINT", value: "https://localhost:4318/" + marker + "%GG"},
		{name: "url missing host", field: "OTEL_EXPORTER_OTLP_ENDPOINT", value: "https:///" + marker},
		{name: "url bad port", field: "OTEL_EXPORTER_OTLP_ENDPOINT", value: "https://localhost:70000/" + marker},
		{name: "url trailing colon", field: "OTEL_EXPORTER_OTLP_ENDPOINT", value: "https://localhost:/" + marker},
		{name: "grpc custom path", field: "OTEL_EXPORTER_OTLP_ENDPOINT", value: "http://localhost:4317/" + marker, extra: map[string]string{"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"}},
		{name: "headers malformed", field: "OTEL_EXPORTER_OTLP_HEADERS", value: marker},
		{name: "headers escaped CRLF", field: "OTEL_EXPORTER_OTLP_HEADERS", value: "authorization=" + marker + "%0D%0A"},
		{name: "headers raw newline", field: "OTEL_EXPORTER_OTLP_HEADERS", value: "authorization=" + marker + "\n"},
		{name: "headers bad escape", field: "OTEL_EXPORTER_OTLP_HEADERS", value: "authorization=" + marker + "%GG"},
		{name: "headers duplicate case", field: "OTEL_EXPORTER_OTLP_HEADERS", value: "Authorization=" + marker + ",authorization=second"},
		{name: "grpc bad key", field: "OTEL_EXPORTER_OTLP_HEADERS", value: "invalid!=value", extra: map[string]string{"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"}},
		{name: "grpc non ASCII value", field: "OTEL_EXPORTER_OTLP_HEADERS", value: "authorization=é", extra: map[string]string{"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"}},
		{name: "timeout raw", field: "OTEL_EXPORTER_OTLP_TIMEOUT", value: marker},
		{name: "timeout zero", field: "OTEL_EXPORTER_OTLP_TIMEOUT", value: "0"},
		{name: "timeout overflow", field: "OTEL_EXPORTER_OTLP_TIMEOUT", value: "9223372036854775807"},
		{name: "timeout whitespace", field: "OTEL_EXPORTER_OTLP_TIMEOUT", value: " 1000 "},
		{name: "compression raw", field: "OTEL_EXPORTER_OTLP_COMPRESSION", value: marker},
		{name: "insecure raw", field: "OTEL_EXPORTER_OTLP_INSECURE", value: marker},
		{name: "insecure contradiction", field: "OTEL_EXPORTER_OTLP_INSECURE", value: "true", extra: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://localhost:4318"}},
		{name: "certificate unreadable", field: "OTEL_EXPORTER_OTLP_CERTIFICATE", value: marker},
		{name: "client certificate missing key", field: "OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE", value: marker},
		{name: "client key missing certificate", field: "OTEL_EXPORTER_OTLP_CLIENT_KEY", value: marker},
		{name: "metric interval", field: "OTEL_METRIC_EXPORT_INTERVAL", value: marker},
		{name: "metric timeout", field: "OTEL_METRIC_EXPORT_TIMEOUT", value: marker},
		{name: "metric temporality", field: "OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE", value: marker},
		{name: "metric aggregation", field: "OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION", value: marker},
		{name: "metric cardinality", field: "OTEL_GO_X_CARDINALITY_LIMIT", value: marker},
		{name: "metric exemplar", field: "OTEL_METRICS_EXEMPLAR_FILTER", value: marker},
		{name: "span count", field: "OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT", value: marker},
		{name: "generic attribute limit", field: "OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT", value: marker},
		{name: "log attribute limit", field: "OTEL_LOGRECORD_ATTRIBUTE_COUNT_LIMIT", value: marker},
		{name: "trace queue", field: "OTEL_BSP_MAX_QUEUE_SIZE", value: marker},
		{name: "log queue", field: "OTEL_BLRP_MAX_QUEUE_SIZE", value: marker},
		{name: "resource percent encoding", field: "OTEL_RESOURCE_ATTRIBUTES", value: "service.namespace=" + marker + "%GG"},
		{name: "resource decoded control", field: "OTEL_RESOURCE_ATTRIBUTES", value: "service.namespace=" + marker + "%0A"},
		{name: "service identity", field: "OTEL_SERVICE_NAME", value: marker + "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearRuntimeOTELTestEnvironment(t)
			for field, value := range test.extra {
				t.Setenv(field, value)
			}
			t.Setenv(test.field, test.value)
			previous := otel.GetErrorHandler()
			var diagnostics atomic.Int32
			otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) { diagnostics.Add(1) }))
			t.Cleanup(func() { otel.SetErrorHandler(previous) })
			report, err := InspectConfiguration(Config{})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), marker)
			assert.Contains(t, err.Error(), "OTEL_")
			encoded, marshalErr := json.Marshal(report)
			require.NoError(t, marshalErr)
			assert.NotContains(t, string(encoded), marker)
			sdk, startErr := Start(context.Background(), Config{})
			require.Error(t, startErr)
			assert.Nil(t, sdk)
			assert.NotContains(t, startErr.Error(), marker)
			assert.Zero(t, diagnostics.Load(), "invalid environment must not reach upstream parsers")
		})
	}
}

func TestConfigurationValidatesOverriddenGenericSettingsAndIgnoresUnusedProtocols(t *testing.T) {
	for _, suffix := range []string{"ENDPOINT", "HEADERS", "TIMEOUT", "COMPRESSION", "INSECURE", "CERTIFICATE", "CLIENT_CERTIFICATE"} {
		t.Run(suffix, func(t *testing.T) {
			environment := map[string]string{
				"OTEL_METRICS_EXPORTER": "none", "OTEL_LOGS_EXPORTER": "none",
				"OTEL_EXPORTER_OTLP_" + suffix:          "invalid-private-canary",
				"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT":    "http://localhost:4318/v1/traces",
				"OTEL_EXPORTER_OTLP_TRACES_HEADERS":     "x-test=valid",
				"OTEL_EXPORTER_OTLP_TRACES_TIMEOUT":     "1000",
				"OTEL_EXPORTER_OTLP_TRACES_COMPRESSION": "none",
				"OTEL_EXPORTER_OTLP_TRACES_INSECURE":    "true",
			}
			_, _, err := resolveConfiguration(Config{}, func(key string) string { return environment[key] })
			require.Error(t, err)
			assert.Contains(t, err.Error(), "OTEL_EXPORTER_OTLP_"+suffix)
			assert.NotContains(t, err.Error(), "private-canary")
		})
	}
	environment := map[string]string{
		"OTEL_METRICS_EXPORTER": "none", "OTEL_LOGS_EXPORTER": "none",
		"OTEL_EXPORTER_OTLP_PROTOCOL":        "unused-invalid-protocol",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/protobuf",
	}
	_, _, err := resolveConfiguration(Config{}, func(key string) string { return environment[key] })
	require.NoError(t, err, "only the selected protocol is parsed by autoexport")
	for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
		environment["OTEL_"+signal+"_EXPORTER"] = "none"
	}
	environment["OTEL_EXPORTER_OTLP_ENDPOINT"] = "unused-invalid-endpoint"
	environment["OTEL_EXPORTER_OTLP_HEADERS"] = "unused-invalid-headers"
	environment["OTEL_METRIC_EXPORT_INTERVAL"] = "unused-invalid-interval"
	_, _, err = resolveConfiguration(Config{}, func(key string) string { return environment[key] })
	require.NoError(t, err, "disabled exporters do not consume OTLP or periodic reader settings")
	environment["OTEL_SPAN_EVENT_COUNT_LIMIT"] = "invalid-used-limit"
	_, _, err = resolveConfiguration(Config{}, func(key string) string { return environment[key] })
	require.Error(t, err, "providers still parse limits when exports are disabled")
}

func TestConfigurationTLSFilesAndLayerSelection(t *testing.T) {
	certFile, keyFile := configurationTestCertificate(t)
	environment := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT":           "https://localhost:4318",
		"OTEL_EXPORTER_OTLP_CERTIFICATE":        certFile,
		"OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE": certFile,
		"OTEL_EXPORTER_OTLP_CLIENT_KEY":         keyFile,
	}
	report, resolved, err := resolveConfiguration(Config{}, func(key string) string { return environment[key] })
	require.NoError(t, err)
	for index, signal := range report.Signals {
		assert.True(t, signal.ServerCertificatePresent)
		assert.True(t, signal.ClientCertificatePresent)
		assert.True(t, signal.ClientKeyPresent)
		require.NotNil(t, resolved.signals[index].tlsConfig)
		require.NotNil(t, resolved.signals[index].tlsConfig.RootCAs)
		assert.Len(t, resolved.signals[index].tlsConfig.Certificates, 1)
	}
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), filepath.Dir(certFile))
	environment["OTEL_EXPORTER_OTLP_TRACES_CLIENT_KEY"] = keyFile
	_, _, err = resolveConfiguration(Config{}, func(key string) string { return environment[key] })
	require.Error(t, err, "a signal key must not silently combine with a generic certificate")
	delete(environment, "OTEL_EXPORTER_OTLP_TRACES_CLIENT_KEY")
	environment["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"] = "http://localhost:4318/v1/traces"
	_, _, err = resolveConfiguration(Config{}, func(key string) string { return environment[key] })
	require.ErrorContains(t, err, "plaintext_credentials")
	delete(environment, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	_, otherKey := configurationTestCertificate(t)
	environment["OTEL_EXPORTER_OTLP_CLIENT_KEY"] = otherKey
	_, _, err = resolveConfiguration(Config{}, func(key string) string { return environment[key] })
	require.ErrorContains(t, err, "invalid_client_certificate")
	environment["OTEL_EXPORTER_OTLP_CLIENT_KEY"] = keyFile
	environment["OTEL_EXPORTER_OTLP_CERTIFICATE"] = t.TempDir()
	_, _, err = resolveConfiguration(Config{}, func(key string) string { return environment[key] })
	require.ErrorContains(t, err, "invalid_certificate")
}

var configurationExporterID atomic.Uint64

func TestConfigurationPreservesCustomExportersAndSanitizesConstructionErrors(t *testing.T) {
	clearRuntimeOTELTestEnvironment(t)
	restoreRuntimeTestGlobals(t)
	disableExporters(t)
	name := fmt.Sprintf("private-custom-exporter-%d", configurationExporterID.Add(1))
	var calls atomic.Int32
	exporter := tracetest.NewInMemoryExporter()
	autoexport.RegisterSpanExporter(name, func(context.Context) (sdktrace.SpanExporter, error) {
		calls.Add(1)
		return exporter, nil
	})
	t.Setenv("OTEL_TRACES_EXPORTER", name)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "invalid-unused-private-setting")
	report, err := InspectConfiguration(Config{})
	require.NoError(t, err)
	assert.Zero(t, calls.Load())
	assert.Equal(t, "custom", report.Signals[0].Exporter)
	assert.Equal(t, "custom_exporter", report.Issues[0].Code)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), name)
	sdk, err := Start(context.Background(), Config{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, calls.Load())
	_, span := sdk.TracerProvider().Tracer("configuration-test").Start(context.Background(), "custom-exporter")
	span.End()
	require.NoError(t, sdk.ForceFlush(context.Background()))
	assert.Len(t, exporter.GetSpans(), 1)
	require.NoError(t, sdk.Shutdown(context.Background()))
	failedName := name + "-failed"
	autoexport.RegisterSpanExporter(failedName, func(context.Context) (sdktrace.SpanExporter, error) {
		return nil, errors.New("private-constructor-error-canary")
	})
	t.Setenv("OTEL_TRACES_EXPORTER", failedName)
	_, err = Start(context.Background(), Config{})
	require.EqualError(t, err, "create OpenTelemetry span exporter failed")
	t.Setenv("OTEL_TRACES_EXPORTER", name+"-unregistered-secret")
	_, err = Start(context.Background(), Config{})
	require.EqualError(t, err, "create OpenTelemetry span exporter failed")

	cleanupExporter := &configurationCleanupExporter{InMemoryExporter: tracetest.NewInMemoryExporter()}
	cleanupName := name + "-cleanup"
	autoexport.RegisterSpanExporter(cleanupName, func(context.Context) (sdktrace.SpanExporter, error) { return cleanupExporter, nil })
	t.Setenv("OTEL_TRACES_EXPORTER", cleanupName)
	t.Setenv("OTEL_METRICS_EXPORTER", name+"-unregistered-secret")
	_, err = Start(context.Background(), Config{})
	require.EqualError(t, err, "create OpenTelemetry metric reader failed\nclean up OpenTelemetry providers after startup failure failed")
	assert.EqualValues(t, 1, cleanupExporter.calls.Load())
}

type configurationCleanupExporter struct {
	*tracetest.InMemoryExporter
	calls atomic.Int32
}

func (exporter *configurationCleanupExporter) Shutdown(context.Context) error {
	exporter.calls.Add(1)
	return errors.New("private-cleanup-error-canary")
}

func TestConfigurationAcceptedEnumAndLimitBoundaries(t *testing.T) {
	environment := map[string]string{
		"OTEL_EXPORTER_OTLP_TIMEOUT":                               "2147483647",
		"OTEL_EXPORTER_OTLP_INSECURE":                              "TRUE",
		"OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE":        "DELTA",
		"OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION": "BASE2_EXPONENTIAL_BUCKET_HISTOGRAM",
		"OTEL_ATTRIBUTE_COUNT_LIMIT":                               "-1",
		"OTEL_SPAN_EVENT_COUNT_LIMIT":                              "0",
		"OTEL_LOGRECORD_ATTRIBUTE_COUNT_LIMIT":                     "-1",
		"OTEL_GO_X_CARDINALITY_LIMIT":                              " -1 ",
		"OTEL_EXPORTER_OTLP_HEADERS":                               "x-test=token+with/slash=and%2Ccomma,empty=",
	}
	report, resolved, err := resolveConfiguration(Config{}, func(key string) string { return environment[key] })
	require.NoError(t, err)
	assert.Equal(t, "delta", report.Temporality)
	assert.Equal(t, "base2_exponential_bucket_histogram", report.HistogramAggregation)
	assert.Equal(t, "exponential_histogram", report.Issues[0].Code)
	for _, signal := range resolved.signals {
		assert.True(t, signal.insecure)
		assert.Equal(t, "http", signal.endpoint.Scheme)
		assert.Equal(t, time.Duration(2147483647)*time.Millisecond, signal.timeout)
		assert.Equal(t, "token+with/slash=and,comma", signal.headers["x-test"])
		assert.Contains(t, signal.headers, "empty")
	}
	// HTTP token headers remain valid even when they would be invalid gRPC
	// metadata; gRPC's binary metadata also retains its explicit semantics.
	assert.True(t, configurationHeaderName("x!custom"))
	assert.True(t, configurationGRPCHeaders(map[string]string{"trace-bin": "é"}))
	assert.False(t, configurationGRPCHeaders(map[string]string{"trace": "é"}))
}

func configurationTestCertificate(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "configuration-test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	directory := t.TempDir()
	certFile, keyFile := filepath.Join(directory, "private-test-certificate.pem"), filepath.Join(directory, "private-test-key.pem")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600))
	return certFile, keyFile
}

func TestForceFlushAttemptsEveryProviderAndCanRepeat(t *testing.T) {
	first, last := errors.New("first flush"), errors.New("last flush")
	var calls []int
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sdk := &SDK{forceFlush: []func(context.Context) error{
		func(got context.Context) error { assert.Same(t, ctx, got); calls = append(calls, 1); return first },
		nil,
		func(got context.Context) error { assert.Same(t, ctx, got); calls = append(calls, 2); return got.Err() },
		func(got context.Context) error { assert.Same(t, ctx, got); calls = append(calls, 3); return last },
	}}
	for range 2 {
		err := sdk.ForceFlush(ctx)
		assert.ErrorIs(t, err, first)
		assert.ErrorIs(t, err, last)
		assert.ErrorIs(t, err, context.Canceled)
	}
	assert.Equal(t, []int{1, 2, 3, 1, 2, 3}, calls)
	assert.NoError(t, (*SDK)(nil).ForceFlush(nil))
	assert.NoError(t, (&SDK{forceFlush: []func(context.Context) error{func(got context.Context) error { assert.NotNil(t, got); return nil }}}).ForceFlush(nil))
}

func TestConfigurationControlCharacterIdentityErrorsNeverEchoValues(t *testing.T) {
	for _, config := range []Config{
		{ServiceName: "identity-canary\n"}, {Namespace: "identity-canary\r"},
		{InstanceID: "identity-canary\x00"}, {Version: "identity-canary\t"},
		{Environment: "identity-canary\x7f"}, {ServiceName: "identity-canary\xff"},
	} {
		report, _, err := resolveConfiguration(config, func(string) string { return "" })
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "identity-canary")
		encoded, marshalErr := json.Marshal(report)
		require.NoError(t, marshalErr)
		assert.False(t, strings.Contains(string(encoded), "identity-canary"))
	}
}
