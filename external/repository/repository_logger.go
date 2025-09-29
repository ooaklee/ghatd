package repository

import (
	"context"
	"fmt"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// ZapRepositoryLogger implements RepositoryLogger using zap
type ZapRepositoryLogger struct {
	// No additional fields needed as we use logger from context
}

// NewZapRepositoryLogger creates a new zap-based repository logger
func NewZapRepositoryLogger() *ZapRepositoryLogger {
	return &ZapRepositoryLogger{}
}

// Error logs error level messages
func (l *ZapRepositoryLogger) Error(ctx context.Context, message string, err error, fields ...Field) {
	zapLogger := logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	zapFields := l.convertFields(fields)
	if err != nil {
		zapFields = append(zapFields, zap.Error(err))
	}

	zapLogger.Error(message, zapFields...)
}

// Warn logs warning level messages
func (l *ZapRepositoryLogger) Warn(ctx context.Context, message string, err error, fields ...Field) {
	zapLogger := logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	zapFields := l.convertFields(fields)
	if err != nil {
		zapFields = append(zapFields, zap.Error(err))
	}

	zapLogger.Warn(message, zapFields...)
}

// Info logs info level messages
func (l *ZapRepositoryLogger) Info(ctx context.Context, message string, err error, fields ...Field) {
	zapLogger := logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	zapFields := l.convertFields(fields)
	if err != nil {
		zapFields = append(zapFields, zap.Error(err))
	}

	zapLogger.Info(message, zapFields...)
}

// Debug logs debug level messages
func (l *ZapRepositoryLogger) Debug(ctx context.Context, message string, err error, fields ...Field) {
	zapLogger := logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	zapFields := l.convertFields(fields)
	if err != nil {
		zapFields = append(zapFields, zap.Error(err))
	}

	zapLogger.Debug(message, zapFields...)
}

// convertFields converts Field slice to zap.Field slice
func (l *ZapRepositoryLogger) convertFields(fields []Field) []zap.Field {
	zapFields := make([]zap.Field, len(fields))
	for i, field := range fields {
		zapFields[i] = zap.Any(field.Key, field.Value)
	}
	return zapFields
}

// NoOpRepositoryLogger implements RepositoryLogger with no-op methods (useful for testing)
type NoOpRepositoryLogger struct{}

// NewNoOpRepositoryLogger creates a new no-op repository logger
func NewNoOpRepositoryLogger() *NoOpRepositoryLogger {
	return &NoOpRepositoryLogger{}
}

// Error does nothing
func (l *NoOpRepositoryLogger) Error(ctx context.Context, message string, err error, fields ...Field) {
}

// Warn does nothing
func (l *NoOpRepositoryLogger) Warn(ctx context.Context, message string, err error, fields ...Field) {
}

// Info does nothing
func (l *NoOpRepositoryLogger) Info(ctx context.Context, message string, err error, fields ...Field) {
}

// Debug does nothing
func (l *NoOpRepositoryLogger) Debug(ctx context.Context, message string, err error, fields ...Field) {
}

// StructuredLogger wraps any logger that supports structured logging
type StructuredLogger struct {
	logger interface{} // Can be any logger that supports the methods below
}

// NewStructuredLogger creates a new structured logger wrapper
func NewStructuredLogger(logger interface{}) *StructuredLogger {
	return &StructuredLogger{logger: logger}
}

// Error logs error with structured fields
func (l *StructuredLogger) Error(ctx context.Context, message string, err error, fields ...Field) {
	// This can be extended to support other logger types like logrus, etc.
	// For now, fallback to simple logging
	fmt.Printf("ERROR: %s", message)
	if err != nil {
		fmt.Printf(" - %v", err)
	}
	if len(fields) > 0 {
		fmt.Printf(" [")
		for i, field := range fields {
			if i > 0 {
				fmt.Printf(", ")
			}
			fmt.Printf("%s=%v", field.Key, field.Value)
		}
		fmt.Printf("]")
	}
	fmt.Println()
}

// Warn logs warning with structured fields
func (l *StructuredLogger) Warn(ctx context.Context, message string, err error, fields ...Field) {
	fmt.Printf("WARN: %s", message)
	if err != nil {
		fmt.Printf(" - %v", err)
	}
	if len(fields) > 0 {
		fmt.Printf(" [")
		for i, field := range fields {
			if i > 0 {
				fmt.Printf(", ")
			}
			fmt.Printf("%s=%v", field.Key, field.Value)
		}
		fmt.Printf("]")
	}
	fmt.Println()
}

// Info logs info with structured fields
func (l *StructuredLogger) Info(ctx context.Context, message string, err error, fields ...Field) {
	fmt.Printf("INFO: %s", message)
	if err != nil {
		fmt.Printf(" - %v", err)
	}
	if len(fields) > 0 {
		fmt.Printf(" [")
		for i, field := range fields {
			if i > 0 {
				fmt.Printf(", ")
			}
			fmt.Printf("%s=%v", field.Key, field.Value)
		}
		fmt.Printf("]")
	}
	fmt.Println()
}

// Debug logs debug with structured fields
func (l *StructuredLogger) Debug(ctx context.Context, message string, err error, fields ...Field) {
	fmt.Printf("DEBUG: %s", message)
	if err != nil {
		fmt.Printf(" - %v", err)
	}
	if len(fields) > 0 {
		fmt.Printf(" [")
		for i, field := range fields {
			if i > 0 {
				fmt.Printf(", ")
			}
			fmt.Printf("%s=%v", field.Key, field.Value)
		}
		fmt.Printf("]")
	}
	fmt.Println()
}
