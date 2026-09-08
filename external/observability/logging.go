package observability

import (
	"context"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	logInstrumentationName = "github.com/ooaklee/ghatd/external/observability"
	logContextKey          = "context"
	TraceIDKey             = "trace_id"
	SpanIDKey              = "span_id"
)

var telemetryLogFieldAllowlist = map[string]struct{}{
	"collection":    {},
	"command":       {},
	"component":     {},
	"correlationid": {},
	"count":         {},
	"dboperation":   {},
	"dbsystem":      {},
	"duration":      {},
	"durationms":    {},
	"enabled":       {},
	"environment":   {},
	"exporter":      {},
	"ghatdpackage":  {},
	"operation":     {},
	"outcome":       {},
	"package":       {},
	"protocol":      {},
	"region":        {},
	"route":         {},
	"service":       {},
	"servicename":   {},
	"signal":        {},
	"spanid":        {},
	"success":       {},
	"system":        {},
	"traceid":       {},
	"version":       {},
}

// TeeLogger keeps the logger's existing output and additionally emits records
// through the official OpenTelemetry Zap bridge. Only the OpenTelemetry branch
// is sanitised; the existing Zap core retains every field except the private
// context carrier used for trace correlation.
//
// The OpenTelemetry branch uses the level enabled by the existing zap core, so
// attaching telemetry cannot make a disabled log level visible remotely.
func TeeLogger(base *zap.Logger, provider otellog.LoggerProvider, options ...LogOption) *zap.Logger {
	if base == nil {
		base = zap.NewNop()
	}
	policy := newLogFieldPolicy(options...)

	return base.WithOptions(zap.WrapCore(func(baseCore zapcore.Core) zapcore.Core {
		bridgeOptions := make([]otelzap.Option, 0, 1)
		if provider != nil {
			bridgeOptions = append(bridgeOptions, otelzap.WithLoggerProvider(provider))
		}

		otelCore := otelzap.NewCore(logInstrumentationName, bridgeOptions...)
		return zapcore.NewTee(baseCore, &sanitisingCore{
			core:      otelCore,
			levelGate: baseCore,
			policy:    policy,
		})
	}))
}

// WithTraceContext makes ctx available to the OpenTelemetry zap bridge for
// intrinsic trace/log correlation. Explicit IDs are also included so existing
// log backends and operators can correlate records without OTLP-specific UI.
func WithTraceContext(ctx context.Context, logger *zap.Logger) *zap.Logger {
	if logger == nil {
		logger = zap.NewNop()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// otelzap recognises context.Context through Interface before serialising a
	// field. SkipType therefore supplies correlation to OTLP while remaining a
	// no-op for every normal Zap core, including samplers and registered hooks.
	fields := []zap.Field{{Key: logContextKey, Type: zapcore.SkipType, Interface: ctx}}
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		fields = append(fields,
			zap.String(TraceIDKey, spanContext.TraceID().String()),
			zap.String(SpanIDKey, spanContext.SpanID().String()),
		)
	}

	return logger.With(fields...)
}

// isTraceContextField identifies the private carrier consumed by otelzap.
func isTraceContextField(field zapcore.Field) bool {
	if field.Key != logContextKey {
		return false
	}
	ctx, ok := field.Interface.(context.Context)
	return ok && ctx != nil
}

// sanitisingCore guards the OTLP branch of a tee. It exports only explicitly
// allowlisted operational fields and drops opaque or custom-marshalled values.
// Selected fields receive additional value validation. Other operational values
// must come from static application configuration, never request payloads.
type sanitisingCore struct {
	core      zapcore.Core
	levelGate zapcore.LevelEnabler
	policy    *logFieldPolicy
}

// Enabled reports whether both the original Zap core and OTLP core accept the level.
func (core *sanitisingCore) Enabled(level zapcore.Level) bool {
	return core.levelGate.Enabled(level) && core.core.Enabled(level)
}

// With attaches sanitised fields to the OTLP core.
func (core *sanitisingCore) With(fields []zapcore.Field) zapcore.Core {
	return &sanitisingCore{
		core:      core.core.With(core.policy.sanitise(fields)),
		levelGate: core.levelGate,
		policy:    core.policy,
	}
}

// Check adds the OTLP core only after the preceding Zap core accepts the entry,
// preserving its sampling and hook decisions.
func (core *sanitisingCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if checked != nil && core.Enabled(entry.Level) {
		return checked.AddCore(entry, core)
	}
	return checked
}

// Write exports an entry with sanitised fields.
func (core *sanitisingCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	return core.core.Write(entry, core.policy.sanitise(fields))
}

// Sync flushes buffered output in the OTLP core.
func (core *sanitisingCore) Sync() error {
	return core.core.Sync()
}
