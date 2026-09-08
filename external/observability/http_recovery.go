package observability

import (
	"bufio"
	"io"
	"net"
	"net/http"

	"github.com/felixge/httpsnoop"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// HTTPRecoveryMiddleware recovers application panics with a constant, safe
// error classification. Install it inside request logging and server telemetry
// so both can complete normally after recovery. It sends a generic 500 only
// when no final response has been committed or connection hijacked.
//
// http.ErrAbortHandler is rethrown unchanged to preserve deliberate net/http
// aborts. Such an abort unwinds otelhttp before its duration metric is recorded.
func HTTPRecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		committed := false
		wrapped := httpsnoop.Wrap(writer, httpsnoop.Hooks{
			WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
				return func(status int) {
					next(status)
					if status == http.StatusSwitchingProtocols || status >= 200 {
						committed = true
					}
				}
			},
			Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
				return func(buffer []byte) (int, error) {
					committed = true
					return next(buffer)
				}
			},
			ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
				return func(reader io.Reader) (int64, error) {
					committed = true
					return next(reader)
				}
			},
			Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
				return func() {
					committed = true
					next()
				}
			},
			Hijack: func(next httpsnoop.HijackFunc) httpsnoop.HijackFunc {
				return func() (net.Conn, *bufio.ReadWriter, error) {
					connection, buffer, err := next()
					if err == nil {
						committed = true
					}
					return connection, buffer, err
				}
			},
		})
		defer func() {
			value := recover()
			if value == nil {
				return
			}
			if value == http.ErrAbortHandler {
				panic(value)
			}
			if state, ok := request.Context().Value(serverRequestTargetKey{}).(*serverRequestState); ok {
				state.panicked = true
			}
			span := trace.SpanFromContext(request.Context())
			span.SetAttributes(attribute.String("error.type", "panic"))
			span.SetStatus(codes.Error, "HTTP handler panic")
			if labeler, ok := otelhttp.LabelerFromContext(request.Context()); ok {
				labeler.Add(attribute.String("error.type", "panic"))
			}
			if !committed {
				http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(wrapped, request)
	})
}

// HTTPRequestPanicked reports whether HTTPRecoveryMiddleware recovered an
// application panic within this request's HTTPServerMiddleware boundary.
// It enables a single request-completion log to classify failures even when
// the response had already been committed with a successful status.
func HTTPRequestPanicked(request *http.Request) bool {
	if request == nil {
		return false
	}
	state, ok := request.Context().Value(serverRequestTargetKey{}).(*serverRequestState)
	return ok && state.panicked
}
