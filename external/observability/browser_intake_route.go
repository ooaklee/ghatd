package observability

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router/routecontext"
)

// MountBrowserTraceIntake dispatches one host-supplied canonical literal path
// before the application handler. Wrap the returned handler with otelhttp's
// outer telemetry/logging/recovery boundary. The intake then bypasses inner
// application auth, cache and response transforms without bypassing telemetry.
//
// A nil intake (including a nil *BrowserTraceIntake) returns next unchanged
// without validating the disabled path.
// Nonmatching and encoded paths reach next without cleaning or redirecting;
// methods are left to the intake handler. This helper does not own its sender
// or start an SDK. Shut down the intake after request draining, before telemetry.
func MountBrowserTraceIntake(next, intake http.Handler, path string) (http.Handler, error) {
	if next == nil {
		return nil, errors.New("observability: browser intake route requires an application handler")
	}
	if intake == nil {
		return next, nil
	}
	if browserIntake, ok := intake.(*BrowserTraceIntake); ok && browserIntake == nil {
		return next, nil
	}
	if !canonicalHTTPTracePath(path) {
		return nil, errors.New("observability: browser intake route requires a bounded canonical literal path excluding root")
	}
	router := mux.NewRouter().SkipClean(true).UseEncodedPath()
	router.Use(routecontext.ObserveMiddleware)
	router.Handle(path, intake)
	router.NotFoundHandler = next
	return router, nil
}
