package observability

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

// TestStartSupportsDisabledSignalsAndInstallsTraceContextPropagation verifies safe propagation when exporters are disabled.
func TestStartSupportsDisabledSignalsAndInstallsTraceContextPropagation(t *testing.T) {
	disableExporters(t)

	previousTracerProvider := otel.GetTracerProvider()
	previousMeterProvider := otel.GetMeterProvider()
	previousLoggerProvider := otellogglobal.GetLoggerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTracerProvider)
		otel.SetMeterProvider(previousMeterProvider)
		otellogglobal.SetLoggerProvider(previousLoggerProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	sdk, err := Start(context.Background(), Config{
		ServiceName: "vehicle-api",
		Version:     "1.2.3",
		Environment: "test",
	})
	require.NoError(t, err)
	require.NotNil(t, sdk.TracerProvider())
	require.NotNil(t, sdk.MeterProvider())
	require.NotNil(t, sdk.LoggerProvider())
	_, disabledSpan := sdk.TracerProvider().Tracer("disabled-test").Start(context.Background(), "disabled")
	assert.False(t, disabledSpan.IsRecording())
	disabledSpan.End()

	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	member, err := baggage.NewMember("tenant", "fleet")
	require.NoError(t, err)
	bag, err := baggage.New(member)
	require.NoError(t, err)
	ctx := baggage.ContextWithBaggage(trace.ContextWithSpanContext(context.Background(), spanContext), bag)

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	assert.Equal(t, "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01", carrier.Get("traceparent"))
	assert.Empty(t, carrier.Get("baggage"))

	assert.NoError(t, sdk.Shutdown(context.Background()))
	assert.NoError(t, sdk.Shutdown(context.Background()))
}

// TestNewResourceUsesDefaultsAndApplicationAttributes verifies resource defaults and application-provided attributes.
func TestNewResourceUsesDefaultsAndApplicationAttributes(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")

	defaultResource, err := newResource(Config{})
	require.NoError(t, err)
	value, ok := defaultResource.Set().Value(semconv.ServiceNameKey)
	require.True(t, ok)
	assert.Equal(t, DefaultServiceName, value.AsString())
	assert.False(t, defaultResource.Set().HasValue(semconv.ServiceVersionKey))
	assert.False(t, defaultResource.Set().HasValue(semconv.DeploymentEnvironmentNameKey))

	configuredResource, err := newResource(Config{
		ServiceName: "  tax-calculator  ",
		Version:     "  abc123  ",
		Environment: "  staging  ",
	})
	require.NoError(t, err)

	value, ok = configuredResource.Set().Value(semconv.ServiceNameKey)
	require.True(t, ok)
	assert.Equal(t, "tax-calculator", value.AsString())
	value, ok = configuredResource.Set().Value(semconv.ServiceVersionKey)
	require.True(t, ok)
	assert.Equal(t, "abc123", value.AsString())
	value, ok = configuredResource.Set().Value(semconv.DeploymentEnvironmentNameKey)
	require.True(t, ok)
	assert.Equal(t, "staging", value.AsString())
}

// TestNewResourceReadsStandardServiceNameAndRejectsControlCharacters verifies standard configuration and input validation.
func TestNewResourceReadsStandardServiceNameAndRejectsControlCharacters(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "from-environment")

	res, err := newResource(Config{})
	require.NoError(t, err)
	value, ok := res.Set().Value(semconv.ServiceNameKey)
	require.True(t, ok)
	assert.Equal(t, "from-environment", value.AsString())

	_, err = newResource(Config{ServiceName: "unsafe\nname"})
	assert.ErrorContains(t, err, "control characters")
}

// TestSamplerFromStandardEnvironment verifies supported sampler configurations and invalid values.
func TestSamplerFromStandardEnvironment(t *testing.T) {
	tests := []struct {
		name       string
		sampler    string
		argument   string
		wantSample bool
		wantError  bool
	}{
		{name: "default parent based always on", wantSample: true},
		{name: "always off", sampler: "always_off"},
		{name: "zero ratio", sampler: "traceidratio", argument: "0"},
		{name: "full ratio", sampler: "parentbased_traceidratio", argument: "1", wantSample: true},
		{name: "missing ratio", sampler: "traceidratio", wantError: true},
		{name: "invalid sampler", sampler: "vendor_sampler", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACES_SAMPLER", test.sampler)
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", test.argument)
			sampler, err := samplerFromEnvironment()
			if test.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			result := sampler.ShouldSample(sdktrace.SamplingParameters{
				TraceID: trace.TraceID{1},
				Name:    "operation",
			})
			assert.Equal(t, test.wantSample, result.Decision == sdktrace.RecordAndSample)
		})
	}
}

// TestShutdownAggregatesErrorsAndRunsOnce verifies that shutdown is idempotent and preserves every failure.
func TestShutdownAggregatesErrorsAndRunsOnce(t *testing.T) {
	firstErr := errors.New("trace shutdown")
	secondErr := errors.New("log shutdown")
	var calls atomic.Int32

	sdk := &SDK{shutdown: []func(context.Context) error{
		func(context.Context) error {
			calls.Add(1)
			return firstErr
		},
		func(context.Context) error {
			calls.Add(1)
			return nil
		},
		func(context.Context) error {
			calls.Add(1)
			return secondErr
		},
	}}

	err := sdk.Shutdown(context.Background())
	assert.ErrorIs(t, err, firstErr)
	assert.ErrorIs(t, err, secondErr)
	assert.EqualValues(t, 3, calls.Load())

	err = sdk.Shutdown(context.Background())
	assert.ErrorIs(t, err, firstErr)
	assert.ErrorIs(t, err, secondErr)
	assert.EqualValues(t, 3, calls.Load())
}

// disableExporters configures each telemetry signal to avoid external export during tests.
func disableExporters(t *testing.T) {
	t.Helper()
	t.Setenv("OTEL_SDK_DISABLED", "false")
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
}
