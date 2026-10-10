package partnermanagerhelper

import (
	"context"

	ghatdobservability "github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/partnermanager"
	partnerhttp "github.com/ooaklee/ghatd/external/partnermanager/http"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// HTTPObserverConfig selects host-owned metric names and borrowed telemetry.
// MeterName and MetricPrefix are required. Nil ports use the global meter
// provider and a no-op logger; the host owns their shutdown.
type HTTPObserverConfig struct {
	MeterName     string
	MetricPrefix  string
	Logger        *zap.Logger
	MeterProvider metric.MeterProvider
}

// NewHTTPObserver records only the shared handler's finite Observation contract
// into metrics, the existing HTTP span and trace-correlated logs. It never reads
// a request, actor, selected resource or payload. Hosts must not invoke it with
// raw/unbounded values. Metric names are <MetricPrefix>.request.count/duration.
func NewHTTPObserver(cfg HTTPObserverConfig) (partnerhttp.Observer, error) {
	if cfg.MeterName == "" || cfg.MetricPrefix == "" {
		return nil, partnermanager.ErrInvalid
	}
	logger, provider := cfg.Logger, cfg.MeterProvider
	if nilHelperPort(provider) {
		provider = otel.GetMeterProvider()
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	meter := provider.Meter(cfg.MeterName)
	requests, err := meter.Int64Counter(cfg.MetricPrefix+".request.count", metric.WithUnit("{request}"))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram(cfg.MetricPrefix+".request.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, o partnerhttp.Observation) {
		dimensions := []attribute.KeyValue{attribute.String("partners.operation", o.Operation), attribute.String("partners.route", o.RouteTemplate), attribute.String("http.request.method", o.Method), attribute.Int("http.response.status_code", o.Status)}
		requests.Add(ctx, 1, metric.WithAttributes(dimensions...))
		duration.Record(ctx, o.Duration.Seconds(), metric.WithAttributes(dimensions...))
		trace.SpanFromContext(ctx).SetAttributes(append(dimensions, attribute.String("partners.stage", o.Stage), attribute.String("partners.error.code", o.ErrorCode))...)
		fields := []zap.Field{zap.String("source", "partners"), zap.String("operation", o.Operation), zap.String("route", o.RouteTemplate), zap.String("method", o.Method), zap.Int("status", o.Status), zap.String("stage", o.Stage), zap.String("error_code", o.ErrorCode), zap.Float64("duration_ms", float64(o.Duration)/1e6)}
		log := ghatdobservability.WithTraceContext(ctx, logger)
		if o.Status >= 500 {
			log.Error("partners request completed", fields...)
		} else if o.Status >= 400 {
			log.Warn("partners request completed", fields...)
		} else {
			log.Info("partners request completed", fields...)
		}
	}, nil
}
