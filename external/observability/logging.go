package observability

import (
	"context"
	"reflect"
	"strings"
	"unicode"

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
	"errortype":     {},
	"exporter":      {},
	"ghatdpackage":  {},
	"method":        {},
	"operation":     {},
	"outcome":       {},
	"package":       {},
	"provider":      {},
	"protocol":      {},
	"region":        {},
	"route":         {},
	"service":       {},
	"servicename":   {},
	"signal":        {},
	"spanid":        {},
	"status":        {},
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
func TeeLogger(base *zap.Logger, provider otellog.LoggerProvider) *zap.Logger {
	if base == nil {
		base = zap.NewNop()
	}

	return base.WithOptions(zap.WrapCore(func(baseCore zapcore.Core) zapcore.Core {
		options := make([]otelzap.Option, 0, 1)
		if provider != nil {
			options = append(options, otelzap.WithLoggerProvider(provider))
		}

		otelCore := otelzap.NewCore(logInstrumentationName, options...)
		return zapcore.NewTee(baseCore, &sanitisingCore{
			core:      otelCore,
			levelGate: baseCore,
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
// allowlisted, low-cardinality operational fields and drops opaque or
// custom-marshalled values. A denylist cannot safely account for arbitrary
// caller-defined keys that may contain credentials or personal data.
type sanitisingCore struct {
	core      zapcore.Core
	levelGate zapcore.LevelEnabler
}

// Enabled reports whether both the original Zap core and OTLP core accept the level.
func (core *sanitisingCore) Enabled(level zapcore.Level) bool {
	return core.levelGate.Enabled(level) && core.core.Enabled(level)
}

// With attaches sanitised fields to the OTLP core.
func (core *sanitisingCore) With(fields []zapcore.Field) zapcore.Core {
	return &sanitisingCore{
		core:      core.core.With(sanitiseLogFields(fields)),
		levelGate: core.levelGate,
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
	return core.core.Write(entry, sanitiseLogFields(fields))
}

// Sync flushes buffered output in the OTLP core.
func (core *sanitisingCore) Sync() error {
	return core.core.Sync()
}

// sanitiseLogFields retains only correlation context and allowlisted operational fields.
func sanitiseLogFields(fields []zapcore.Field) []zapcore.Field {
	safe := make([]zapcore.Field, 0, len(fields))
	for _, field := range fields {
		if field.Type == zapcore.ErrorType {
			if err, ok := field.Interface.(error); ok && err != nil {
				safe = append(safe, zap.String("error.type", reflect.TypeOf(err).String()))
			}
			continue
		}

		if isTraceContextField(field) {
			safe = append(safe, field)
			continue
		}

		if !isAllowedTelemetryLogKey(field.Key) || isOpaqueLogField(field.Type) {
			continue
		}

		safe = append(safe, field)
	}
	return safe
}

// isAllowedTelemetryLogKey reports whether a field key is safe for OTLP export.
func isAllowedTelemetryLogKey(key string) bool {
	normalised := strings.Map(func(char rune) rune {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			return unicode.ToLower(char)
		}
		return -1
	}, key)

	_, allowed := telemetryLogFieldAllowlist[normalised]
	return allowed
}

// isOpaqueLogField reports whether Zap may serialise nested or custom data for a field.
func isOpaqueLogField(fieldType zapcore.FieldType) bool {
	switch fieldType {
	case zapcore.ArrayMarshalerType,
		zapcore.ObjectMarshalerType,
		zapcore.InlineMarshalerType,
		zapcore.BinaryType,
		zapcore.ByteStringType,
		zapcore.ReflectType,
		zapcore.StringerType,
		zapcore.UnknownType:
		return true
	default:
		return false
	}
}
