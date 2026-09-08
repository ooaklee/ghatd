package middleware_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ooaklee/ghatd/external/common"
	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/logger/middleware"
	"github.com/ooaklee/ghatd/external/observability"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/toolbox"
)

// TestMiddlewareHTTPLoggerUsesRouteTemplateAndTraceCorrelation verifies safe request logging and span correlation.
func TestMiddlewareHTTPLoggerUsesRouteTemplateAndTraceCorrelation(t *testing.T) {
	const suppliedCorrelationID = "fbd4046f-0f1c-4f98-b71c-d4cd61443f90"

	tests := []struct {
		name                   string
		correlationID          string
		preservesCorrelationID bool
	}{
		{name: "uses supplied correlation ID", correlationID: suppliedCorrelationID, preservesCorrelationID: true},
		{name: "generates correlation ID"},
		{name: "replaces non UUID correlation ID", correlationID: "driver@example.com/raw-secret"},
		{name: "replaces non v4 UUID correlation ID", correlationID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, observed := observer.New(zapcore.InfoLevel)
			baseLogger := zap.New(core)

			spanRecorder := tracetest.NewSpanRecorder()
			tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
			t.Cleanup(func() { require.NoError(t, tracerProvider.Shutdown(context.Background())) })
			ctx, span := tracerProvider.Tracer("middleware-test").Start(context.Background(), "request")

			request := httptest.NewRequest(http.MethodGet, "http://localhost/v1/vehicles/AB12CDE?token=raw-secret", nil).WithContext(ctx)
			request.RemoteAddr = "192.0.2.1:1234"
			request.Header.Set("X-Forwarded-For", "192.0.2.2")
			request.Header.Set("User-Agent", "privacy-sensitive-client")
			if tt.correlationID != "" {
				request.Header.Set(common.CorrelationIdHttpHeader, tt.correlationID)
			}

			response := httptest.NewRecorder()
			router := mux.NewRouter()
			router.Handle("/v1/vehicles/{vrn}", middleware.NewLogger(baseLogger, nil).HTTPLogger(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ghatdlogger.AcquireFrom(req.Context()).Info("handler event")
				w.WriteHeader(http.StatusBadRequest)
			}))).Methods(http.MethodGet)

			router.ServeHTTP(response, request)
			span.End()

			require.Equal(t, http.StatusBadRequest, response.Code)
			responseCorrelationID := response.Header().Get(common.CorrelationIdHttpHeader)
			if tt.preservesCorrelationID {
				assert.Equal(t, tt.correlationID, responseCorrelationID)
			} else if tt.correlationID != "" {
				assert.NotEqual(t, tt.correlationID, responseCorrelationID)
			}
			assert.Regexp(t, regexp.MustCompile(toolbox.UuidV4Regex), responseCorrelationID)

			entries := observed.All()
			require.Len(t, entries, 2)
			for _, entry := range entries {
				fields := entry.ContextMap()
				assert.Equal(t, responseCorrelationID, fields["correlation-id"])
				assert.Equal(t, span.SpanContext().TraceID().String(), fields["trace_id"])
				assert.Equal(t, span.SpanContext().SpanID().String(), fields["span_id"])
			}

			completion := entries[1]
			fields := completion.ContextMap()
			assert.Equal(t, "http request completed", completion.Message)
			assert.EqualValues(t, http.StatusBadRequest, fields["status"])
			assert.Equal(t, http.MethodGet, fields["method"])
			assert.Equal(t, "/v1/vehicles/{vrn}", fields["route"])
			assert.NotContains(t, fields, "uri")
			assert.NotContains(t, fields, "clientip")
			assert.NotContains(t, fields, "forwarded-for")
			assert.NotContains(t, fields, "host")
			assert.NotContains(t, fields, "user-agent")
			assert.NotContains(t, completion.Message, "AB12CDE")
			assert.NotContains(t, completion.Message, "raw-secret")

			ended := spanRecorder.Ended()
			require.Len(t, ended, 1)
			spanAttributes := make(map[string]string)
			for _, spanAttribute := range ended[0].Attributes() {
				spanAttributes[string(spanAttribute.Key)] = spanAttribute.Value.AsString()
			}
			assert.Equal(t, responseCorrelationID, spanAttributes["correlation.id"])
		})
	}
}

// The real HTTP server establishes which status reaches the client, including
// informational responses and headers implicitly committed by streaming APIs.
func TestHTTPLoggerRecordsFirstFinalWireStatus(t *testing.T) {
	tests := []struct {
		name   string
		handle func(http.ResponseWriter)
		status int
	}{
		{name: "empty response", handle: func(http.ResponseWriter) {}, status: http.StatusOK},
		{name: "first final response wins", handle: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusCreated)
			w.WriteHeader(http.StatusInternalServerError)
		}, status: http.StatusCreated},
		{name: "informational response before final", handle: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusEarlyHints)
			w.WriteHeader(http.StatusAccepted)
		}, status: http.StatusAccepted},
		{name: "informational response before implicit final", handle: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusEarlyHints)
		}, status: http.StatusOK},
		{name: "switching protocols is final", handle: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusSwitchingProtocols)
			w.WriteHeader(http.StatusInternalServerError)
		}, status: http.StatusSwitchingProtocols},
		{name: "write commits implicit success", handle: func(w http.ResponseWriter) {
			_, _ = w.Write([]byte("response"))
			w.WriteHeader(http.StatusInternalServerError)
		}, status: http.StatusOK},
		{name: "flush commits implicit success", handle: func(w http.ResponseWriter) {
			_ = http.NewResponseController(w).Flush()
			w.WriteHeader(http.StatusInternalServerError)
		}, status: http.StatusOK},
		{name: "read from commits implicit success", handle: func(w http.ResponseWriter) {
			readerFrom, ok := w.(io.ReaderFrom)
			if !ok {
				t.Error("logger stripped ReaderFrom from the HTTP server response")
				return
			}
			_, _ = readerFrom.ReadFrom(strings.NewReader("response"))
			w.WriteHeader(http.StatusInternalServerError)
		}, status: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			core, recorded := observer.New(zap.InfoLevel)
			logged := make(chan struct{})
			base := zap.New(core, zap.Hooks(func(zapcore.Entry) error {
				close(logged)
				return nil
			}))
			handler := middleware.NewLogger(base, nil).HTTPLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				test.handle(w)
			}))
			server := httptest.NewUnstartedServer(handler)
			// Several cases deliberately attempt a second final header; the
			// standard server ignores it and would otherwise log a warning.
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.Start()
			t.Cleanup(server.Close)

			response, err := server.Client().Get(server.URL)
			require.NoError(t, err)
			if response.StatusCode != http.StatusSwitchingProtocols {
				_, err = io.Copy(io.Discard, response.Body)
				require.NoError(t, err)
			}
			require.NoError(t, response.Body.Close())
			select {
			case <-logged:
			case <-time.After(time.Second):
				t.Fatal("request completion was not logged")
			}
			require.Equal(t, test.status, response.StatusCode)
			entries := recorded.All()
			require.Len(t, entries, 1)
			require.EqualValues(t, response.StatusCode, entries[0].ContextMap()["status"])
		})
	}
}

type streamingResponseWriter struct {
	*httptest.ResponseRecorder
	closed        chan bool
	connection    net.Conn
	pushedTarget  string
	pushedOptions *http.PushOptions
	deadline      time.Time
	readError     error
}

func (writer *streamingResponseWriter) CloseNotify() <-chan bool { return writer.closed }

func (writer *streamingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return writer.connection, bufio.NewReadWriter(bufio.NewReader(writer.connection), bufio.NewWriter(writer.connection)), nil
}

func (writer *streamingResponseWriter) Push(target string, options *http.PushOptions) error {
	writer.pushedTarget, writer.pushedOptions = target, options
	return http.ErrNotSupported
}

func (writer *streamingResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	n, err := io.Copy(writer.ResponseRecorder, reader)
	if writer.readError != nil {
		return n, writer.readError
	}
	return n, err
}

func (writer *streamingResponseWriter) SetWriteDeadline(deadline time.Time) error {
	writer.deadline = deadline
	return nil
}

func TestHTTPLoggerPreservesOnlyAvailableOptionalInterfaces(t *testing.T) {
	full := &streamingResponseWriter{ResponseRecorder: httptest.NewRecorder(), closed: make(chan bool)}
	tests := []struct {
		name     string
		writer   http.ResponseWriter
		expected [5]bool
	}{
		{name: "none", writer: struct{ http.ResponseWriter }{full}},
		{name: "flusher only", writer: struct {
			http.ResponseWriter
			http.Flusher
		}{full, full}, expected: [5]bool{true, false, false, false, false}},
		{name: "hijacker only", writer: struct {
			http.ResponseWriter
			http.Hijacker
		}{full, full}, expected: [5]bool{false, true, false, false, false}},
		{name: "pusher only", writer: struct {
			http.ResponseWriter
			http.Pusher
		}{full, full}, expected: [5]bool{false, false, true, false, false}},
		{name: "close notifier only", writer: struct {
			http.ResponseWriter
			http.CloseNotifier
		}{full, full}, expected: [5]bool{false, false, false, true, false}},
		{name: "reader from only", writer: struct {
			http.ResponseWriter
			io.ReaderFrom
		}{full, full}, expected: [5]bool{false, false, false, false, true}},
		{name: "all", writer: full, expected: [5]bool{true, true, true, true, true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := middleware.NewLogger(zap.NewNop(), nil).HTTPLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, flush := w.(http.Flusher)
				_, hijack := w.(http.Hijacker)
				_, push := w.(http.Pusher)
				_, notify := w.(http.CloseNotifier)
				_, readFrom := w.(io.ReaderFrom)
				assert.Equal(t, test.expected, [5]bool{flush, hijack, push, notify, readFrom})
				unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
				require.True(t, ok)
				assert.Equal(t, test.writer, unwrapper.Unwrap())
			}))
			handler.ServeHTTP(test.writer, httptest.NewRequest(http.MethodGet, "/", nil))
		})
	}
}

func TestHTTPLoggerDelegatesStreamingCapabilities(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = connection.Close(); _ = peer.Close() })
	streamError := errors.New("synthetic reader error")
	writer := &streamingResponseWriter{
		ResponseRecorder: httptest.NewRecorder(), closed: make(chan bool), connection: connection, readError: streamError,
	}
	deadline := time.Now().Add(time.Minute)
	pushOptions := &http.PushOptions{Method: http.MethodGet}
	core, recorded := observer.New(zap.InfoLevel)
	handler := middleware.NewLogger(zap.New(core), nil).HTTPLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, http.NewResponseController(w).SetWriteDeadline(deadline))
		assert.Equal(t, (<-chan bool)(writer.closed), w.(http.CloseNotifier).CloseNotify())
		assert.ErrorIs(t, w.(http.Pusher).Push("/asset.css", pushOptions), http.ErrNotSupported)
		n, err := w.(io.ReaderFrom).ReadFrom(strings.NewReader("stream"))
		assert.EqualValues(t, 6, n)
		assert.Same(t, streamError, err)
		require.NoError(t, http.NewResponseController(w).Flush())
		hijacked, buffer, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		assert.Same(t, connection, hijacked)
		assert.NotNil(t, buffer)
	}))
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/stream", nil))
	assert.Equal(t, deadline, writer.deadline)
	assert.Equal(t, "/asset.css", writer.pushedTarget)
	assert.Same(t, pushOptions, writer.pushedOptions)
	assert.True(t, writer.Flushed)
	assert.Equal(t, "stream", writer.Body.String())
	require.Len(t, recorded.All(), 1)
	assert.EqualValues(t, http.StatusOK, recorded.All()[0].ContextMap()["status"])
}

func TestHTTPLoggerReadsRouteAfterOuterRouterDispatch(t *testing.T) {
	core, recorded := observer.New(zap.InfoLevel)
	router := ghatdrouter.NewRouter(nil, nil).GetRouter()
	router.HandleFunc("/vehicles/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}).Methods(http.MethodGet)
	handler := middleware.NewLogger(zap.New(core), nil).HTTPLogger(router)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/vehicles/synthetic-private-id", nil))
	require.Len(t, recorded.All(), 1)
	assert.Equal(t, "/vehicles/{id}", recorded.All()[0].ContextMap()["route"])
	assert.NotContains(t, recorded.All()[0].ContextMap(), "uri")
}

func TestHTTPLoggerBoundsUnknownMethodsOnMatchedAndFallbackRoutes(t *testing.T) {
	const method = "SYNTHETIC_PRIVATE_METHOD_TOKEN"
	for _, test := range []struct {
		name   string
		path   string
		route  string
		status int
	}{
		{name: "matched", path: "/items/private-id", route: "/items/{id}", status: http.StatusNoContent},
		{name: "not found", path: "/missing/private-id", route: "unknown", status: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			core, recorded := observer.New(zap.InfoLevel)
			router := ghatdrouter.NewRouter(nil, nil).GetRouter()
			router.HandleFunc("/items/{id}", func(w http.ResponseWriter, req *http.Request) {
				assert.Equal(t, method, req.Method, "telemetry normalization must not change application requests")
				w.WriteHeader(http.StatusNoContent)
			}).Methods(method)
			handler := middleware.NewLogger(zap.New(core), nil).HTTPLogger(router)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(method, test.path, nil))
			require.Equal(t, test.status, response.Code)
			require.Len(t, recorded.All(), 1)
			entry := recorded.All()[0]
			fields := entry.ContextMap()
			assert.Equal(t, "OTHER", fields["method"])
			assert.Equal(t, test.route, fields["route"])
			assert.EqualValues(t, test.status, fields["status"])
			assert.NotContains(t, entry.Message+fmt.Sprint(fields), method)
			assert.NotContains(t, entry.Message+fmt.Sprint(fields), "private-id")
		})
	}
}

func TestHTTPLoggerRecordsRecoveredPanicAfterCommittedResponse(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before headers", true: "after headers"}[committed], func(t *testing.T) {
			core, recorded := observer.New(zap.InfoLevel)
			router := ghatdrouter.NewRouter(nil, nil).GetRouter()
			router.HandleFunc("/vehicles/{id}", func(w http.ResponseWriter, _ *http.Request) {
				if committed {
					_, _ = w.Write([]byte("partial response"))
				}
				panic("synthetic-sensitive-panic")
			})
			handler := observability.HTTPServerMiddleware("test")(
				middleware.NewLogger(zap.New(core), nil).HTTPLogger(observability.HTTPRecoveryMiddleware(router)),
			)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/vehicles/synthetic-private-id", nil))
			expectedStatus := http.StatusInternalServerError
			if committed {
				expectedStatus = http.StatusOK
			}
			require.Equal(t, expectedStatus, response.Code)
			require.Len(t, recorded.All(), 1)
			fields := recorded.All()[0].ContextMap()
			assert.EqualValues(t, expectedStatus, fields["status"])
			assert.Equal(t, "/vehicles/{id}", fields["route"])
			assert.Equal(t, "error", fields["outcome"])
			assert.Equal(t, "panic", fields["error.type"])
			assert.NotContains(t, fmt.Sprint(fields), "synthetic-sensitive-panic")
			assert.NotContains(t, fmt.Sprint(fields), "synthetic-private-id")
		})
	}
}

// TestMiddlewareHTTPLoggerCustomIgnoreListMatchesPathOrRoute verifies ignore entries accept paths and route templates.
func TestMiddlewareHTTPLoggerCustomIgnoreListMatchesPathOrRoute(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	baseLogger := zap.New(core)

	tests := []struct {
		name       string
		ignoreList []string
	}{
		{name: "path", ignoreList: []string{"/health/ready"}},
		{name: "route template", ignoreList: []string{"/health/{probe}"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observed.TakeAll()
			request := httptest.NewRequest(http.MethodGet, "http://localhost/health/ready?verbose=true", nil)
			response := httptest.NewRecorder()

			router := mux.NewRouter()
			router.Handle("/health/{probe}", middleware.NewLogger(baseLogger, tt.ignoreList).HTTPLoggerWithCustomUriIgnoreList(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))).Methods(http.MethodGet)

			router.ServeHTTP(response, request)
			assert.Empty(t, observed.All())
		})
	}
}
