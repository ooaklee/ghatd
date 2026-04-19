package logger

import (
	"context"

	"go.uber.org/zap"
)

// Debug logs a debug-level message using the logger from context.
func Debug(ctx context.Context, msg string, fields ...zap.Field) {
	Get(ctx).Debug(msg, fields...)
}

// Info logs an info-level message using the logger from context.
func Info(ctx context.Context, msg string, fields ...zap.Field) {
	Get(ctx).Info(msg, fields...)
}

// Warn logs a warning-level message using the logger from context.
func Warn(ctx context.Context, msg string, fields ...zap.Field) {
	Get(ctx).Warn(msg, fields...)
}

// Error logs an error-level message using the logger from context.
func Error(ctx context.Context, msg string, fields ...zap.Field) {
	Get(ctx).Error(msg, fields...)
}

// With returns a new logger with the provided fields added.
// The new logger is attached to a new context which is returned.
func With(ctx context.Context, fields ...zap.Field) context.Context {
	logger := Get(ctx).With(fields...)
	return TransitWith(ctx, logger)
}
