// Package otelhttp composes GHATD's HTTP telemetry, request logger, and recovery
// middleware into a single outer handler for a complete router.
package otelhttp

import (
	"net/http"

	loggermiddleware "github.com/ooaklee/ghatd/external/logger/middleware"
	"github.com/ooaklee/ghatd/external/observability"
	"go.uber.org/zap"
)

// Wrap observes the entire HTTP dispatch, including router-generated 404/405
// responses, redirects, middleware short circuits, and recovered panics. The
// order is telemetry, request logging, recovery, then the application handler.
// Start a Runtime first and pass its Logger to export correlated OTLP records.
// A nil logger disables request-log output without disabling spans or metrics.
//
// GHATD routers capture matched route templates automatically. Plain Gorilla
// Mux routers should install routecontext.ObserveMiddleware before other route
// middleware. Wrap once outside the router, without also installing a request
// logger or recovery on matched routes. Names and route templates must be
// trusted configuration, never raw request values.
//
// Recovery preserves responses already committed or hijacked. ErrAbortHandler
// continues to abort the request and omits normal completion logs/duration.
func Wrap(name string, logger *zap.Logger, handler http.Handler) http.Handler {
	return WrapWithOptions(name, logger, handler)
}

// WrapWithOptions composes the same outer boundary as Wrap with explicit HTTP
// telemetry options. A trace policy suppresses selected unsampled roots only
// when the provider uses observability.HTTPTraceSampler, as Start does. Request
// metrics and logs remain enabled, including for suppressed requests.
func WrapWithOptions(name string, logger *zap.Logger, handler http.Handler, options ...observability.HTTPServerOption) http.Handler {
	return observability.HTTPServerMiddlewareWithOptions(name, options...)(
		loggermiddleware.NewLogger(logger, nil).HTTPLogger(
			observability.HTTPRecoveryMiddleware(handler),
		),
	)
}
