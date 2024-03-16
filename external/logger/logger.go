package logger

import (
	"context"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

// contextKey represents the key to reference the logger in the context
type contextKey string

const loggerKey contextKey = "ContextLogger"

// NewLogger creates a logger
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

// TransitWith packages both passed context and logger to enable logger to move
// across processes.
func TransitWith(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// AcquireFrom pulls logger from context if exists or returns no-op logger
func AcquireFrom(ctx context.Context) *zap.Logger {

	logger, ok := ctx.Value(loggerKey).(*zap.Logger)
	if ok && logger != nil {
		return logger
	}

	return ctxzap.Extract(ctx)

}
