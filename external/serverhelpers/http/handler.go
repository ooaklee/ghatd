package serverhttp

import (
	"errors"
	"net/http"

	loggermiddleware "github.com/ooaklee/ghatd/external/logger/middleware"
	"github.com/ooaklee/ghatd/external/observability"
	"go.uber.org/zap"
)

// HandlerConfig supplies explicit logging and telemetry policy. Redaction,
// trusted proxies and trace suppression are host choices; none are inferred
// from a deployment name. The caller owns Logger and telemetry lifetimes.
type HandlerConfig struct {
	Component string
	Logger    *zap.Logger
	Policy    observability.HTTPServerOptionsConfig
	// IgnoreCompletionPaths suppresses only matching completion logs, not trace
	// context, metrics, downstream application logs or panic recovery.
	IgnoreCompletionPaths []string
}

// NewHandler composes one outer telemetry boundary, request logger and recovery
// in that order. Keep it outside routers and response caches so misses, method
// errors and panics retain the same telemetry and request identity. It starts no
// SDK or goroutine. The downstream handler is never mutated by construction.
func NewHandler(cfg HandlerConfig, next http.Handler) (http.Handler, error) {
	if cfg.Logger == nil || next == nil {
		return nil, errors.New("serverhelpers/http-handler-dependencies-required")
	}
	options, err := observability.NewHTTPServerOptions(cfg.Policy)
	if err != nil {
		return nil, err
	}
	ignored := append([]string(nil), cfg.IgnoreCompletionPaths...)
	return observability.HTTPServerMiddlewareWithOptions(cfg.Component, options...)(
		loggermiddleware.NewLogger(cfg.Logger, ignored).HTTPLoggerWithCustomUriIgnoreList(
			observability.HTTPRecoveryMiddleware(next),
		),
	), nil
}
