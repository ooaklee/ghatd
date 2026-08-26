// Package observability provides the shared OpenTelemetry setup used by GHATD
// applications.
package observability

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

const (
	// DefaultServiceName is used when neither Config.ServiceName nor
	// OTEL_SERVICE_NAME provides a service name.
	DefaultServiceName = "ghatd"
)

// Config describes resource attributes owned by the application. Exporters,
// protocols, endpoints, credentials, timeouts, and sampling use the standard
// OTEL_* variables supported by the configured exporters and this package.
type Config struct {
	ServiceName string
	Version     string
	Environment string
}

// SDK owns the providers installed by Start.
type SDK struct {
	tracerProvider *trace.TracerProvider
	meterProvider  *metric.MeterProvider
	loggerProvider *log.LoggerProvider

	shutdownOnce sync.Once
	shutdownErr  error
	shutdown     []func(context.Context) error
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
		return nil, fmt.Errorf("create OpenTelemetry span exporter: %w", err)
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
			fmt.Errorf("create OpenTelemetry metric reader: %w", err),
			tracerProvider.Shutdown(ctx),
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
			fmt.Errorf("create OpenTelemetry log exporter: %w", err),
			meterProvider.Shutdown(ctx),
			tracerProvider.Shutdown(ctx),
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
				fmt.Errorf("start OpenTelemetry runtime metrics: %w", err),
				loggerProvider.Shutdown(ctx),
				meterProvider.Shutdown(ctx),
				tracerProvider.Shutdown(ctx),
			)
		}
		if err := host.Start(host.WithMeterProvider(meterProvider)); err != nil {
			return nil, errors.Join(
				fmt.Errorf("start OpenTelemetry host metrics: %w", err),
				loggerProvider.Shutdown(ctx),
				meterProvider.Shutdown(ctx),
				tracerProvider.Shutdown(ctx),
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
	}

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otellogglobal.SetLoggerProvider(loggerProvider)
	// Trace context crosses service boundaries by default. Baggage is omitted
	// because arbitrary inbound values must not be forwarded to external hosts.
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return sdk, nil
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
		if err != nil || ratio < 0 || ratio > 1 {
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
		return nil, fmt.Errorf("unsupported OTEL_TRACES_SAMPLER value %q", name)
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

// newResource merges host resource data with application-owned attributes.
func newResource(config Config) (*resource.Resource, error) {
	serviceName := strings.TrimSpace(config.ServiceName)
	if serviceName == "" {
		serviceName = strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	}
	if serviceName == "" {
		serviceName = DefaultServiceName
	}
	if strings.IndexFunc(serviceName, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("OpenTelemetry service name contains control characters")
	}

	attributes := []attribute.KeyValue{semconv.ServiceName(serviceName)}
	if version := strings.TrimSpace(config.Version); version != "" {
		attributes = append(attributes, semconv.ServiceVersion(version))
	}
	if environment := strings.TrimSpace(config.Environment); environment != "" {
		attributes = append(attributes, semconv.DeploymentEnvironmentNameKey.String(environment))
	}

	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, attributes...),
	)
}
