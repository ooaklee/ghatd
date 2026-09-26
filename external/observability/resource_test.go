package observability

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

func TestResourceDefaultInstanceIDIsStableWithinProcess(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	first, err := newResource(Config{})
	require.NoError(t, err)
	want := resourceTestString(t, first, semconv.ServiceInstanceIDKey)
	parsed, err := uuid.Parse(want)
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(4), parsed.Version())
	assert.Equal(t, "go", resourceTestString(t, first, semconv.TelemetrySDKLanguageKey))
	assert.Equal(t, "opentelemetry", resourceTestString(t, first, semconv.TelemetrySDKNameKey))
	assert.NotEmpty(t, resourceTestString(t, first, semconv.TelemetrySDKVersionKey))
	for _, item := range first.Attributes() {
		key := string(item.Key)
		assert.False(t, strings.HasPrefix(key, "process."), "default resource unexpectedly detects %s", key)
		assert.False(t, strings.HasPrefix(key, "host."), "default resource unexpectedly detects %s", key)
	}

	var done sync.WaitGroup
	for range 16 {
		done.Go(func() {
			res, err := newResource(Config{ServiceName: "another-service"})
			if assert.NoError(t, err) {
				value, ok := res.Set().Value(semconv.ServiceInstanceIDKey)
				assert.True(t, ok)
				assert.Equal(t, want, value.AsString())
			}
		})
	}
	done.Wait()
}

func TestResourceIdentityConfigurationPrecedence(t *testing.T) {
	for _, test := range []struct {
		name        string
		serviceName string
		attributes  string
		config      Config
		want        map[attribute.Key]string
	}{
		{
			name: "resource attributes supply identity",
			attributes: "service.name=environment-service,service.namespace=shared%2Dplatform," +
				"service.instance.id=worker-17,service.version=build-42,deployment.environment.name=staging,custom.label=a%2Cb",
			want: map[attribute.Key]string{
				semconv.ServiceNameKey: "environment-service", semconv.ServiceNamespaceKey: "shared-platform",
				semconv.ServiceInstanceIDKey: "worker-17", semconv.ServiceVersionKey: "build-42",
				semconv.DeploymentEnvironmentNameKey: "staging", "custom.label": "a,b",
			},
		},
		{
			name:        "standard service name overrides resource attribute",
			serviceName: "  standard-service  ",
			attributes:  "service.name=resource-service,service.instance.id=worker-18",
			want: map[attribute.Key]string{
				semconv.ServiceNameKey: "standard-service", semconv.ServiceInstanceIDKey: "worker-18",
			},
		},
		{
			name:        "explicit application fields override environment",
			serviceName: "standard-service",
			attributes: "service.name=environment-service,service.namespace=environment-namespace," +
				"service.instance.id=environment-instance,service.version=environment-version,deployment.environment.name=environment-name",
			config: Config{
				ServiceName: "  configured-service  ", Namespace: "  configured-namespace  ",
				InstanceID: "  configured-instance  ", Version: "  configured-version  ", Environment: "  configured-environment  ",
			},
			want: map[attribute.Key]string{
				semconv.ServiceNameKey: "configured-service", semconv.ServiceNamespaceKey: "configured-namespace",
				semconv.ServiceInstanceIDKey: "configured-instance", semconv.ServiceVersionKey: "configured-version",
				semconv.DeploymentEnvironmentNameKey: "configured-environment",
			},
		},
		{
			name:       "blank application fields allow environment configuration",
			attributes: "service.name=environment-service,service.namespace=environment-namespace,service.instance.id=worker-19",
			config:     Config{ServiceName: " ", Namespace: " ", InstanceID: " "},
			want: map[attribute.Key]string{
				semconv.ServiceNameKey: "environment-service", semconv.ServiceNamespaceKey: "environment-namespace",
				semconv.ServiceInstanceIDKey: "worker-19",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OTEL_SERVICE_NAME", test.serviceName)
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", test.attributes)
			res, err := newResource(test.config)
			require.NoError(t, err)
			for key, want := range test.want {
				assert.Equal(t, want, resourceTestString(t, res, key), "resource attribute %s", key)
			}
		})
	}
}

func TestResourceReadsCurrentEnvironmentWithoutCachingOverrides(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	initial, err := newResource(Config{})
	require.NoError(t, err)
	defaultID := resourceTestString(t, initial, semconv.ServiceInstanceIDKey)
	assert.False(t, initial.Set().HasValue(semconv.ServiceNamespaceKey))

	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=first-service,service.namespace=first-namespace,service.instance.id=first-instance")
	first, err := newResource(Config{})
	require.NoError(t, err)
	assert.Equal(t, "first-service", resourceTestString(t, first, semconv.ServiceNameKey))
	assert.Equal(t, "first-instance", resourceTestString(t, first, semconv.ServiceInstanceIDKey))

	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=second-service,service.namespace=second-namespace,service.instance.id=second-instance")
	second, err := newResource(Config{})
	require.NoError(t, err)
	assert.Equal(t, "second-service", resourceTestString(t, second, semconv.ServiceNameKey))
	assert.Equal(t, "second-namespace", resourceTestString(t, second, semconv.ServiceNamespaceKey))
	assert.Equal(t, "second-instance", resourceTestString(t, second, semconv.ServiceInstanceIDKey))
	assert.Equal(t, "first-instance", resourceTestString(t, first, semconv.ServiceInstanceIDKey), "previous resources remain immutable")

	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=,service.instance.id=")
	final, err := newResource(Config{})
	require.NoError(t, err)
	assert.Equal(t, DefaultServiceName, resourceTestString(t, final, semconv.ServiceNameKey))
	assert.Equal(t, defaultID, resourceTestString(t, final, semconv.ServiceInstanceIDKey))
	assert.False(t, final.Set().HasValue(semconv.ServiceNamespaceKey))
}

func TestResourceRejectsUnsafeConfigurationWithoutEchoingValues(t *testing.T) {
	const sensitive = "synthetic-private-resource-input"
	previousHandler := otel.GetErrorHandler()
	var reported []error
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) { reported = append(reported, err) }))
	t.Cleanup(func() { otel.SetErrorHandler(previousHandler) })
	for _, test := range []struct {
		name        string
		serviceName string
		attributes  string
		config      Config
	}{
		{name: "configured name", config: Config{ServiceName: sensitive + "\n"}},
		{name: "configured version", config: Config{Version: sensitive + "\t"}},
		{name: "configured environment", config: Config{Environment: sensitive + "\x7f"}},
		{name: "configured namespace", config: Config{Namespace: sensitive + "\u0085"}},
		{name: "configured instance", config: Config{InstanceID: "\n" + sensitive}},
		{name: "standard service name", serviceName: sensitive + "\n"},
		{name: "attribute raw control", attributes: "service.namespace=" + sensitive + "\n"},
		{name: "attribute encoded control", attributes: "service.instance.id=" + sensitive + "%0A"},
		{name: "attribute encoded unicode control", attributes: "service.namespace=" + sensitive + "%C2%85"},
		{name: "custom attribute control", attributes: "custom.label=" + sensitive + "%09"},
		{name: "malformed pair", attributes: sensitive},
		{name: "missing key", attributes: "=" + sensitive},
		{name: "malformed percent encoding", attributes: "service.namespace=" + sensitive + "%ZZ"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OTEL_SERVICE_NAME", test.serviceName)
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", test.attributes)
			res, err := newResource(test.config)
			require.Error(t, err)
			assert.Nil(t, res)
			assert.NotContains(t, err.Error(), sensitive)
			assert.NotContains(t, err.Error(), "\n")
			assert.Empty(t, reported, "invalid input must not reach the SDK's global error handler")
		})
	}
}

func TestResourceInstanceIDDiffersAcrossProcesses(t *testing.T) {
	const helper = "GHATD_TEST_RESOURCE_PROCESS_ID"
	const marker = "GHATD_RESOURCE_INSTANCE="
	if os.Getenv(helper) == "1" {
		res, err := newResource(Config{})
		require.NoError(t, err)
		fmt.Fprintln(os.Stdout, marker+resourceTestString(t, res, semconv.ServiceInstanceIDKey))
		return
	}
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	parent, err := newResource(Config{})
	require.NoError(t, err)
	seen := map[string]bool{resourceTestString(t, parent, semconv.ServiceInstanceIDKey): true}
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResourceInstanceIDDiffersAcrossProcesses$", "-test.count=1")
		command.Env = append(os.Environ(), helper+"=1")
		output, err := command.CombinedOutput()
		cancel()
		require.NoError(t, err, "resource identity subprocess failed: %s", output)
		var instanceID string
		for line := range strings.SplitSeq(string(output), "\n") {
			if strings.HasPrefix(line, marker) {
				instanceID = strings.TrimPrefix(line, marker)
				break
			}
		}
		require.NotEmpty(t, instanceID, "subprocess did not report an instance ID")
		parsed, err := uuid.Parse(instanceID)
		require.NoError(t, err)
		assert.Equal(t, uuid.Version(4), parsed.Version())
		assert.False(t, seen[instanceID], "separate processes must not share default instance identity")
		seen[instanceID] = true
	}
}

var resourceTestExporterID atomic.Uint64

func TestStartSharesResourceIdentityAcrossAllSignals(t *testing.T) {
	previousTracer := otel.GetTracerProvider()
	previousMeter := otel.GetMeterProvider()
	previousLogger := otellogglobal.GetLoggerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTracer)
		otel.SetMeterProvider(previousMeter)
		otellogglobal.SetLoggerProvider(previousLogger)
		otel.SetTextMapPropagator(previousPropagator)
	})
	spanExporter := tracetest.NewInMemoryExporter()
	metricReader := sdkmetric.NewManualReader()
	logExporter := &recordingLogExporter{}
	exporterName := fmt.Sprintf("ghatd-resource-test-%d", resourceTestExporterID.Add(1))
	autoexport.RegisterSpanExporter(exporterName, func(context.Context) (sdktrace.SpanExporter, error) { return spanExporter, nil })
	autoexport.RegisterMetricReader(exporterName, func(context.Context) (sdkmetric.Reader, error) { return metricReader, nil })
	autoexport.RegisterLogExporter(exporterName, func(context.Context) (sdklog.Exporter, error) { return logExporter, nil })
	t.Setenv("OTEL_TRACES_EXPORTER", exporterName)
	t.Setenv("OTEL_METRICS_EXPORTER", exporterName)
	t.Setenv("OTEL_LOGS_EXPORTER", exporterName)
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	t.Setenv("OTEL_SERVICE_NAME", "environment-service")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.namespace=example-platform,service.version=build-42,deployment.environment.name=testing")
	sdk, err := Start(context.Background(), Config{ServiceName: "example-api"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sdk.Shutdown(context.Background())) })
	ctx, span := sdk.TracerProvider().Tracer("resource-test").Start(context.Background(), "resource-check")
	counter, err := sdk.MeterProvider().Meter("resource-test").Int64Counter("resource.check.count")
	require.NoError(t, err)
	counter.Add(ctx, 1)
	var record otellog.Record
	record.SetBody(otellog.StringValue("resource-check"))
	sdk.LoggerProvider().Logger("resource-test").Emit(ctx, record)
	span.End()
	require.NoError(t, sdk.TracerProvider().ForceFlush(context.Background()))
	require.NoError(t, sdk.LoggerProvider().ForceFlush(context.Background()))
	var metrics metricdata.ResourceMetrics
	require.NoError(t, metricReader.Collect(context.Background(), &metrics))
	spans := spanExporter.GetSpans()
	logs := logExporter.Records()
	require.Len(t, spans, 1)
	require.Len(t, logs, 1)
	require.NotEmpty(t, metrics.ScopeMetrics)
	resources := []*resource.Resource{spans[0].Resource, metrics.Resource, logs[0].Resource()}
	instanceID := resourceTestString(t, resources[0], semconv.ServiceInstanceIDKey)
	_, err = uuid.Parse(instanceID)
	require.NoError(t, err)
	for _, res := range resources {
		assert.True(t, resources[0].Equal(res), "all signals must carry identical resource metadata")
		assert.Equal(t, instanceID, resourceTestString(t, res, semconv.ServiceInstanceIDKey))
		assert.Equal(t, "example-api", resourceTestString(t, res, semconv.ServiceNameKey))
		assert.Equal(t, "example-platform", resourceTestString(t, res, semconv.ServiceNamespaceKey))
		assert.Equal(t, "build-42", resourceTestString(t, res, semconv.ServiceVersionKey))
		assert.Equal(t, "testing", resourceTestString(t, res, semconv.DeploymentEnvironmentNameKey))
	}
}

func resourceTestString(t *testing.T, res *resource.Resource, key attribute.Key) string {
	t.Helper()
	value, ok := res.Set().Value(key)
	require.True(t, ok, "missing resource attribute %s", key)
	return value.AsString()
}
