package logger

import (
	"context"
	"net/http"
	"runtime"
	"strings"

	"go.uber.org/zap"
)

const modulePath = "github.com/ooaklee/ghatd"

// Debug logs a debug-level message using the logger from context.
func Debug(ctx context.Context, msg string, fields ...zap.Field) {
	loggerForCaller(ctx, 2).Debug(msg, fields...)
}

// Info logs an info-level message using the logger from context.
func Info(ctx context.Context, msg string, fields ...zap.Field) {
	loggerForCaller(ctx, 2).Info(msg, fields...)
}

// Warn logs a warning-level message using the logger from context.
func Warn(ctx context.Context, msg string, fields ...zap.Field) {
	loggerForCaller(ctx, 2).Warn(msg, fields...)
}

// Error logs an error-level message using the logger from context.
func Error(ctx context.Context, msg string, fields ...zap.Field) {
	loggerForCaller(ctx, 2).Error(msg, fields...)
}

// With returns a new logger with the provided fields added.
// The new logger is attached to a new context which is returned.
func With(ctx context.Context, fields ...zap.Field) context.Context {
	logger := loggerForCaller(ctx, 2).With(fields...)
	return TransitWith(ctx, logger)
}

// RequestPath returns a request path for logging without assuming the request
// was created by net/http.
func RequestPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return r.URL.Path
}

// loggerForCaller returns a logger for the caller at skip frames, attributed to
// that caller's package via AcquirePackageFrom. When no package name can be
// derived it falls back to the plain context logger.
func loggerForCaller(ctx context.Context, skip int) *zap.Logger {
	if packageName := packageNameFromCaller(skip + 1); packageName != "" {
		return AcquirePackageFrom(ctx, packageName)
	}

	return Get(ctx)
}

// packageNameFromCaller derives a GHATD package name from the runtime call
// stack, inspecting up to eight frames starting at skip. It skips logger-
// package frames and unqualified names, stripping method or receiver suffixes;
// it returns an empty string when no suitable caller is found.
func packageNameFromCaller(skip int) string {
	for i := skip; i < skip+8; i++ {
		pc, _, _, ok := runtime.Caller(i)
		if !ok {
			continue
		}

		fn := runtime.FuncForPC(pc)
		if fn == nil {
			continue
		}

		name := strings.TrimPrefix(fn.Name(), modulePath+"/")
		if name == fn.Name() {
			continue
		}

		if strings.HasPrefix(name, "external/logger.") || strings.HasPrefix(name, "external/logger/") {
			continue
		}

		if idx := strings.Index(name, ".("); idx >= 0 {
			return name[:idx]
		}
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			return name[:idx]
		}
	}

	return ""
}
