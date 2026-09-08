// Package observability provides the shared OpenTelemetry setup used by GHATD
// applications.
package observability

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

const (
	// DefaultServiceName is used when neither Config.ServiceName nor
	// OTEL_SERVICE_NAME nor OTEL_RESOURCE_ATTRIBUTES provides a service name.
	DefaultServiceName = "ghatd"
)

// Config describes resource attributes owned by the application. Exporters,
// protocols, endpoints, credentials, timeouts, and sampling use the standard
// OTEL_* variables supported by the configured exporters and this package.
type Config struct {
	ServiceName string
	Version     string
	Environment string
	// Namespace groups related services. An empty value uses service.namespace
	// from OTEL_RESOURCE_ATTRIBUTES, or leaves the namespace unset.
	Namespace string
	// InstanceID identifies this service process. An empty value uses
	// service.instance.id from OTEL_RESOURCE_ATTRIBUTES, then a random UUID
	// shared by resource construction within this process.
	InstanceID string
}

// SDK owns the providers installed by Start.
type SDK struct {
	tracerProvider *trace.TracerProvider
	meterProvider  *metric.MeterProvider
	loggerProvider *log.LoggerProvider

	shutdownOnce sync.Once
	shutdownErr  error
	shutdown     []func(context.Context) error
	forceFlush   []func(context.Context) error
}

// Start configures OTLP (or another autoexport-supported exporter) for traces,
// metrics, and logs. It installs W3C Trace Context propagation and
// starts Go runtime and host metric instrumentation when metrics are enabled.
//
// OTEL_TRACES_EXPORTER, OTEL_METRICS_EXPORTER, and OTEL_LOGS_EXPORTER may each
// be set to "none" to disable that signal without making startup fail.
func Start(ctx context.Context, config Config) (*SDK, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := InspectConfiguration(config); err != nil {
		return nil, err
	}

	res, err := newResource(config)
	if err != nil {
		return nil, err
	}
	sampler, err := samplerFromEnvironment()
	if err != nil {
		return nil, err
	}

	spanExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, errors.New("create OpenTelemetry span exporter failed")
	}

	tracerOptions := []trace.TracerProviderOption{trace.WithResource(res), trace.WithSampler(sampler)}
	if autoexport.IsNoneSpanExporter(spanExporter) {
		tracerOptions = append(tracerOptions, trace.WithSampler(trace.NeverSample()))
	} else {
		tracerOptions = append(tracerOptions, trace.WithBatcher(spanExporter))
	}
	tracerProvider := trace.NewTracerProvider(tracerOptions...)

	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		return nil, errors.Join(
			errors.New("create OpenTelemetry metric reader failed"),
			shutdownAfterStartupFailure(ctx, tracerProvider.Shutdown),
		)
	}

	meterOptions := []metric.Option{metric.WithResource(res)}
	metricsEnabled := !autoexport.IsNoneMetricReader(metricReader)
	if metricsEnabled {
		meterOptions = append(meterOptions, metric.WithReader(metricReader))
	}
	meterProvider := metric.NewMeterProvider(meterOptions...)

	logExporter, err := autoexport.NewLogExporter(ctx)
	if err != nil {
		return nil, errors.Join(
			errors.New("create OpenTelemetry log exporter failed"),
			shutdownAfterStartupFailure(ctx, meterProvider.Shutdown, tracerProvider.Shutdown),
		)
	}

	loggerOptions := []log.LoggerProviderOption{log.WithResource(res)}
	if !autoexport.IsNoneLogExporter(logExporter) {
		loggerOptions = append(loggerOptions, log.WithProcessor(log.NewBatchProcessor(logExporter)))
	}
	loggerProvider := log.NewLoggerProvider(loggerOptions...)

	if metricsEnabled {
		if err := runtime.Start(runtime.WithMeterProvider(meterProvider)); err != nil {
			return nil, errors.Join(
				errors.New("start OpenTelemetry runtime metrics failed"),
				shutdownAfterStartupFailure(ctx, loggerProvider.Shutdown, meterProvider.Shutdown, tracerProvider.Shutdown),
			)
		}
		if err := host.Start(host.WithMeterProvider(meterProvider)); err != nil {
			return nil, errors.Join(
				errors.New("start OpenTelemetry host metrics failed"),
				shutdownAfterStartupFailure(ctx, loggerProvider.Shutdown, meterProvider.Shutdown, tracerProvider.Shutdown),
			)
		}
	}

	sdk := &SDK{
		tracerProvider: tracerProvider,
		meterProvider:  meterProvider,
		loggerProvider: loggerProvider,
		shutdown: []func(context.Context) error{
			loggerProvider.Shutdown,
			meterProvider.Shutdown,
			tracerProvider.Shutdown,
		},
		forceFlush: []func(context.Context) error{
			loggerProvider.ForceFlush,
			meterProvider.ForceFlush,
			tracerProvider.ForceFlush,
		},
	}

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otellogglobal.SetLoggerProvider(loggerProvider)
	// Trace context crosses service boundaries by default. Baggage is omitted
	// because arbitrary inbound values must not be forwarded to external hosts.
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return sdk, nil
}

// Startup errors may be returned directly by command-line tools. Preserve a
// safe phase diagnostic even when a custom exporter's cleanup returns private
// text. Normal Shutdown and ForceFlush retain original errors for their caller.
func shutdownAfterStartupFailure(ctx context.Context, callbacks ...func(context.Context) error) error {
	failed := false
	for _, shutdown := range callbacks {
		if shutdown != nil && shutdown(ctx) != nil {
			failed = true
		}
	}
	if failed {
		return errors.New("clean up OpenTelemetry providers after startup failure failed")
	}
	return nil
}

// samplerFromEnvironment constructs a sampler from standard OTEL environment variables.
func samplerFromEnvironment() (trace.Sampler, error) {
	name := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER")))
	if name == "" {
		name = "parentbased_always_on"
	}

	ratioSampler := func() (trace.Sampler, error) {
		value := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG"))
		if value == "" {
			return nil, fmt.Errorf("OTEL_TRACES_SAMPLER_ARG is required for %s", name)
		}
		ratio, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
			return nil, fmt.Errorf("OTEL_TRACES_SAMPLER_ARG must be a number between 0 and 1 for %s", name)
		}
		return trace.TraceIDRatioBased(ratio), nil
	}

	switch name {
	case "always_on":
		return trace.AlwaysSample(), nil
	case "always_off":
		return trace.NeverSample(), nil
	case "parentbased_always_on":
		return trace.ParentBased(trace.AlwaysSample()), nil
	case "parentbased_always_off":
		return trace.ParentBased(trace.NeverSample()), nil
	case "traceidratio":
		return ratioSampler()
	case "parentbased_traceidratio":
		ratio, err := ratioSampler()
		if err != nil {
			return nil, err
		}
		return trace.ParentBased(ratio), nil
	default:
		return nil, errors.New("unsupported OTEL_TRACES_SAMPLER value")
	}
}

// TracerProvider returns the configured trace provider.
func (sdk *SDK) TracerProvider() *trace.TracerProvider {
	if sdk == nil {
		return nil
	}
	return sdk.tracerProvider
}

// MeterProvider returns the configured metric provider.
func (sdk *SDK) MeterProvider() *metric.MeterProvider {
	if sdk == nil {
		return nil
	}
	return sdk.meterProvider
}

// LoggerProvider returns the configured log provider.
func (sdk *SDK) LoggerProvider() *log.LoggerProvider {
	if sdk == nil {
		return nil
	}
	return sdk.loggerProvider
}

// ForceFlush exports pending records from all three providers without stopping
// them. Every provider is attempted and errors are joined. A nil SDK is safe;
// the caller owns the context deadline and may call ForceFlush again.
func (sdk *SDK) ForceFlush(ctx context.Context) error {
	if sdk == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var errs []error
	for _, flush := range sdk.forceFlush {
		if flush != nil {
			errs = append(errs, flush(ctx))
		}
	}
	return errors.Join(errs...)
}

// Shutdown flushes and stops all providers. Calls are safe to repeat and from
// multiple goroutines; every provider error from the first call is retained.
func (sdk *SDK) Shutdown(ctx context.Context) error {
	if sdk == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sdk.shutdownOnce.Do(func() {
		errs := make([]error, 0, len(sdk.shutdown))
		for _, shutdown := range sdk.shutdown {
			if shutdown != nil {
				errs = append(errs, shutdown(ctx))
			}
		}
		sdk.shutdownErr = errors.Join(errs...)
	})

	return sdk.shutdownErr
}
