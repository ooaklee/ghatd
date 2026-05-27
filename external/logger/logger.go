// Package logger provides an interface for managing structured logging via
// a context.Context. It supports embedding logger instances in contexts for
// consistent logging across service boundaries.
//
// The package supports both zap and standard library log/slog patterns,
// making it flexible for different use cases.
package logger

import (
	"context"
	"strings"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

// contextKey represents the key to reference the logger in the context.
type contextKey string

const loggerKey contextKey = "ContextLogger"

const (
	// FieldSource identifies logs emitted by GHATD framework code rather than
	// host application code.
	FieldSource = "source"
	// FieldPackage identifies the GHATD package responsible for a log event.
	FieldPackage = "ghatd-package"
	// FieldOperation identifies the package operation responsible for a log event.
	FieldOperation = "operation"
	// SourceGHATD is the stable value used in FieldSource for GHATD logs.
	SourceGHATD = "ghatd"
)

// NewLogger creates a structured logger with the specified level and configuration.
//
// For local environments, it creates a development logger with human-readable output.
// For all other environments, it creates a production logger with JSON output.
func NewLogger(logLevel string, environment string, component string) (*zap.Logger, error) {
	logConf := zap.NewProductionConfig()
	if environment == "local" {
		logConf = zap.NewDevelopmentConfig()
	}
	fields := zap.Fields(
		zap.String("component", component),
		zap.String("environment", environment),
	)
	logConf.Level = asAtomicLevel(logLevel)

	return logConf.Build(fields)
}

// asAtomicLevel takes a string and converts it to AtomicLevel. If converting
// string fails defaults to warn level
func asAtomicLevel(logLevel string) (r zap.AtomicLevel) {

	l := zap.WarnLevel

	// Set default to warn
	r = zap.NewAtomicLevelAt(l)

	if err := l.Set(logLevel); err != nil {
		return
	}

	return zap.NewAtomicLevelAt(l)
}

// TransitWith attaches a logger to a context, enabling the logger to be
// retrieved in downstream functions and services.
//
// This is the primary method for propagating loggers across service boundaries.
func TransitWith(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// AcquireFrom retrieves the logger from the context.
//
// Falls back to the logger from ctxzap.Extract if none is set via TransitWith.
// This ensures that a logger is always available, even if one wasn't explicitly set.
func AcquireFrom(ctx context.Context) *zap.Logger {
	logger, ok := ctx.Value(loggerKey).(*zap.Logger)
	if ok && logger != nil {
		return logger
	}

	return ctxzap.Extract(ctx)
}

// Get is an alias for AcquireFrom, providing a more concise API.
// It retrieves the current logger for the context.
func Get(ctx context.Context) *zap.Logger {
	return AcquireFrom(ctx)
}

// AcquirePackageFrom retrieves a logger from context and adds standard GHATD
// package attribution fields. Use this inside GHATD package code when the
// current operation is implied by the log message or surrounding helper.
func AcquirePackageFrom(ctx context.Context, packageName string, fields ...zap.Field) *zap.Logger {
	return AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel)).With(packageFields(packageName, "", fields...)...)
}

// AcquireOperationFrom retrieves a logger from context and adds standard
// GHATD package and operation attribution fields.
func AcquireOperationFrom(ctx context.Context, packageName string, operation string, fields ...zap.Field) *zap.Logger {
	return AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel)).With(packageFields(packageName, operation, fields...)...)
}

// WithPackage returns a new context whose logger carries standard GHATD
// package attribution fields.
func WithPackage(ctx context.Context, packageName string, fields ...zap.Field) context.Context {
	return TransitWith(ctx, AcquirePackageFrom(ctx, packageName, fields...))
}

// WithOperation returns a new context whose logger carries standard GHATD
// package and operation attribution fields.
func WithOperation(ctx context.Context, packageName string, operation string, fields ...zap.Field) context.Context {
	return TransitWith(ctx, AcquireOperationFrom(ctx, packageName, operation, fields...))
}

func packageFields(packageName string, operation string, fields ...zap.Field) []zap.Field {
	result := []zap.Field{zap.String(FieldSource, SourceGHATD)}

	if packageName = strings.TrimSpace(packageName); packageName != "" {
		result = append(result, zap.String(FieldPackage, packageName))
	}

	if operation = strings.TrimSpace(operation); operation != "" {
		result = append(result, zap.String(FieldOperation, operation))
	}

	return append(result, fields...)
}
