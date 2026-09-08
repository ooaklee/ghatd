package observability

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/router/routecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestHTTPRouterObservesFullLifecycleWithoutMatchingTwice(t *testing.T) {
	const sensitive = "synthetic-private-route-value"
	for _, test := range []struct {
		name            string
		method          string
		path            string
		wantStatus      int
		wantRoute       string
		wantCalls       int
		wantMatches     int
		shortCircuit    bool
		strictSlash     bool
		nested          bool
		customNotFound  bool
		customMethod    bool
		legacyTelemetry bool
	}{
		{name: "matched", path: "/items/" + sensitive, wantStatus: 202, wantRoute: "/items/{id}", wantCalls: 1, wantMatches: 1},
		{name: "generated404", path: "/unknown/" + sensitive, wantStatus: 404},
		{name: "generated405", method: http.MethodPost, path: "/items/" + sensitive, wantStatus: 405, wantMatches: 1},
		{name: "custom404", path: "/unknown/" + sensitive, wantStatus: 418, customNotFound: true},
		{name: "custom405", method: http.MethodPost, path: "/items/" + sensitive, wantStatus: 400, wantMatches: 1, customMethod: true},
		{name: "canonical redirect", path: "/items//" + sensitive, wantStatus: 301},
		{name: "strict slash redirect", path: "/items/" + sensitive, wantStatus: 301, wantRoute: "/items/{id}/", wantMatches: 1, strictSlash: true},
		{name: "cache short circuit", path: "/items/" + sensitive, wantStatus: 203, wantRoute: "/items/{id}", wantMatches: 1, shortCircuit: true},
		{name: "nested router", path: "/api/items/" + sensitive, wantStatus: 202, wantRoute: "/api/items/{id}", wantCalls: 1, wantMatches: 1, nested: true},
		{name: "legacy inner telemetry does not duplicate", path: "/items/" + sensitive, wantStatus: 202, wantRoute: "/items/{id}", wantCalls: 1, wantMatches: 1, legacyTelemetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder, reader, options := newRouterTelemetry(t)
			shortCircuit := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					if test.shortCircuit {
						assert.Equal(t, test.wantRoute, routecontext.Template(request), "observer must precede supplied middleware")
						writer.WriteHeader(http.StatusNonAuthoritativeInfo)
						return
					}
					next.ServeHTTP(writer, request)
				})
			}
			router := ghatdrouter.NewRouter(nil, nil, shortCircuit).GetRouter()
			if test.legacyTelemetry {
				router.Use(httpServerMiddleware("example-api", options...))
			}
			if test.strictSlash {
				router.StrictSlash(true)
			}
			if test.customNotFound {
				router.NotFoundHandler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					assert.Equal(t, test.path, request.URL.Path)
					writer.WriteHeader(http.StatusTeapot)
				})
			}
			if test.customMethod {
				router.MethodNotAllowedHandler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					assert.Equal(t, test.path, request.URL.Path)
					writer.WriteHeader(http.StatusBadRequest)
				})
			}
			routeOwner := router
			if test.nested {
				routeOwner = router.PathPrefix("/api").Subrouter()
			}
			pattern := "/items/{id}"
			if test.strictSlash {
				pattern += "/"
			}
			calls, matches := 0, 0
			routeOwner.HandleFunc(pattern, func(writer http.ResponseWriter, request *http.Request) {
				calls++
				assert.Equal(t, test.path, request.URL.Path)
				assert.Equal(t, "token="+sensitive, request.URL.RawQuery)
				assert.Equal(t, sensitive, mux.Vars(request)["id"])
				assert.Empty(t, request.Pattern, "the application sees the original request Pattern")
				writer.WriteHeader(http.StatusAccepted)
			}).MatcherFunc(func(*http.Request, *mux.RouteMatch) bool {
				matches++
				return true
			}).Methods(http.MethodGet)
			method := test.method
			if method == "" {
				method = http.MethodGet
			}
			request := httptest.NewRequest(method, test.path+"?token="+sensitive, nil)
			response := httptest.NewRecorder()
			var outerRoute string
			outer := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				router.ServeHTTP(writer, request)
				outerRoute = routecontext.Template(request)
			})
			httpServerMiddleware("example-api", options...)(outer).ServeHTTP(response, request)
			assert.Equal(t, test.wantStatus, response.Code)
			assert.Equal(t, test.wantCalls, calls)
			assert.Equal(t, test.wantMatches, matches, "instrumentation must not add matching passes")
			assert.Equal(t, test.wantRoute, outerRoute)
			assert.Equal(t, test.path, request.URL.Path)
			assert.Equal(t, "token="+sensitive, request.URL.RawQuery)
			assert.Empty(t, request.Pattern)
			require.Len(t, recorder.Ended(), 1)
			span := findEndedSpan(t, recorder, oteltrace.SpanKindServer)
			wantName := method
			if test.wantRoute != "" {
				wantName += " " + test.wantRoute
			}
			assert.Equal(t, wantName, span.Name())
			assertAttributeEquals(t, span, "http.response.status_code", int64(test.wantStatus))
			assertSpanExcludes(t, span, sensitive)
			point := routerDurationPoint(t, reader)
			assert.Equal(t, uint64(1), point.Count)
			status, ok := point.Attributes.Value("http.response.status_code")
			require.True(t, ok)
			assert.Equal(t, int64(test.wantStatus), status.AsInt64())
			route, hasRoute := point.Attributes.Value("http.route")
			if test.wantRoute == "" {
				assert.False(t, hasRoute)
			} else {
				require.True(t, hasRoute)
				assert.Equal(t, test.wantRoute, route.AsString())
				assertAttributeEquals(t, span, "http.route", test.wantRoute)
			}
			for _, item := range point.Attributes.ToSlice() {
				assert.NotContains(t, item.Value.Emit(), sensitive)
			}
		})
	}
}

func TestHTTPRecoveryPreservesCommittedStatusAndCompletesTelemetry(t *testing.T) {
	const sensitive = "synthetic-private-panic-payload"
	for _, committed := range []bool{false, true} {
		name := "before response"
		if committed {
			name = "after partial response"
		}
		t.Run(name, func(t *testing.T) {
			recorder, reader, options := newRouterTelemetry(t)
			router := ghatdrouter.NewRouter(nil, nil).GetRouter()
			router.HandleFunc("/items/{id}", func(writer http.ResponseWriter, _ *http.Request) {
				if committed {
					writer.WriteHeader(http.StatusAccepted)
					_, _ = writer.Write([]byte("partial"))
				}
				panic(map[string]string{"token": sensitive})
			})
			recovered := HTTPRecoveryMiddleware(router)
			panicObserved := false
			outer := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				recovered.ServeHTTP(writer, request)
				panicObserved = HTTPRequestPanicked(request)
				assert.Equal(t, "/items/{id}", routecontext.Template(request))
			})
			response := httptest.NewRecorder()
			httpServerMiddleware("example-api", options...)(outer).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/items/42", nil))
			assert.True(t, panicObserved)
			wantStatus := http.StatusInternalServerError
			if committed {
				wantStatus = http.StatusAccepted
				assert.Equal(t, "partial", response.Body.String(), "recovery must not append another error response")
			} else {
				assert.Equal(t, "Internal Server Error\n", response.Body.String())
			}
			assert.Equal(t, wantStatus, response.Code)
			require.Len(t, recorder.Ended(), 1)
			span := findEndedSpan(t, recorder, oteltrace.SpanKindServer)
			assert.Equal(t, codes.Error, span.Status().Code, "later successful HTTP status must not clear the panic error")
			assert.Equal(t, "GET /items/{id}", span.Name())
			assertAttributeEquals(t, span, "error.type", "panic")
			assertSpanExcludes(t, span, sensitive)
			point := routerDurationPoint(t, reader)
			assert.Equal(t, uint64(1), point.Count)
			status, ok := point.Attributes.Value("http.response.status_code")
			require.True(t, ok)
			assert.Equal(t, int64(wantStatus), status.AsInt64())
			errorType, ok := point.Attributes.Value("error.type")
			require.True(t, ok)
			assert.Equal(t, "panic", errorType.AsString())
			for _, item := range point.Attributes.ToSlice() {
				assert.NotContains(t, item.Value.Emit(), sensitive)
			}
		})
	}
}

func TestHTTPRecoveryRethrowsIntentionalAbortWithoutSyntheticResponse(t *testing.T) {
	recorder, _, options := newRouterTelemetry(t)
	writer := &httpIOTestResponseWriter{header: make(http.Header)}
	handler := httpServerMiddleware("example-api", options...)(HTTPRecoveryMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})))
	require.PanicsWithValue(t, http.ErrAbortHandler, func() {
		handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/abort", nil))
	})
	assert.Zero(t, writer.status, "intentional abort must not synthesize an HTTP response")
	require.Len(t, recorder.Ended(), 1, "upstream still ends the span during panic unwind")
}

type httpRecoveryTestWriter struct {
	*httpIOTestOptionalWriter
	connection net.Conn
}

func (writer *httpRecoveryTestWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if writer.hijackErr != nil {
		return nil, nil, writer.hijackErr
	}
	return writer.connection, bufio.NewReadWriter(bufio.NewReader(writer.connection), bufio.NewWriter(writer.connection)), nil
}

func TestHTTPRecoveryPreservesStreamingInterfacesAndCommittedResponses(t *testing.T) {
	for _, test := range []struct {
		name       string
		commit     func(*testing.T, http.ResponseWriter, *httpRecoveryTestWriter)
		wantStatus int
	}{
		{
			name: "Flush",
			commit: func(t *testing.T, writer http.ResponseWriter, original *httpRecoveryTestWriter) {
				writer.(http.Flusher).Flush()
				assert.Equal(t, 1, original.flushCalls)
			},
		},
		{
			name: "ReadFrom",
			commit: func(t *testing.T, writer http.ResponseWriter, original *httpRecoveryTestWriter) {
				n, err := writer.(io.ReaderFrom).ReadFrom(strings.NewReader("source"))
				require.NoError(t, err)
				assert.EqualValues(t, 3, n)
				assert.Equal(t, "sou", original.readFromData)
			},
		},
		{
			name: "successful Hijack",
			commit: func(t *testing.T, writer http.ResponseWriter, original *httpRecoveryTestWriter) {
				connection, buffer, err := writer.(http.Hijacker).Hijack()
				require.NoError(t, err)
				assert.Same(t, original.connection, connection)
				require.NotNil(t, buffer)
			},
		},
		{
			name: "101 is final",
			commit: func(_ *testing.T, writer http.ResponseWriter, _ *httpRecoveryTestWriter) {
				writer.WriteHeader(http.StatusSwitchingProtocols)
			},
			wantStatus: http.StatusSwitchingProtocols,
		},
		{
			name: "103 permits later error response",
			commit: func(_ *testing.T, writer http.ResponseWriter, _ *httpRecoveryTestWriter) {
				writer.WriteHeader(http.StatusEarlyHints)
			},
			wantStatus: http.StatusInternalServerError,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection, peer := net.Pipe()
			t.Cleanup(func() { _ = connection.Close(); _ = peer.Close() })
			original := &httpRecoveryTestWriter{
				httpIOTestOptionalWriter: &httpIOTestOptionalWriter{
					httpIOTestResponseWriter: &httpIOTestResponseWriter{header: make(http.Header)},
					closed:                   make(chan bool),
				},
				connection: connection,
			}
			handler := HTTPRecoveryMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, flusher := writer.(http.Flusher)
				_, hijacker := writer.(http.Hijacker)
				pusher, push := writer.(http.Pusher)
				notifier, notify := writer.(http.CloseNotifier)
				_, readerFrom := writer.(io.ReaderFrom)
				require.True(t, flusher && hijacker && push && notify && readerFrom, "recovery must retain every supported streaming interface")
				require.NoError(t, pusher.Push("/asset", nil))
				assert.Equal(t, "/asset", original.pushTarget)
				assert.Equal(t, (<-chan bool)(original.closed), notifier.CloseNotify())
				test.commit(t, writer, original)
				panic("synthetic-private-stream-panic")
			}))
			require.NotPanics(t, func() {
				handler.ServeHTTP(original, httptest.NewRequest(http.MethodGet, "/stream", nil))
			})
			assert.Equal(t, test.wantStatus, original.status, "a committed or hijacked response must not receive a synthetic500")
		})
	}
}

func TestHTTPRecoveryDoesNotAdvertiseUnsupportedWriterInterfaces(t *testing.T) {
	original := &httpIOTestResponseWriter{header: make(http.Header)}
	handler := HTTPRecoveryMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, flusher := writer.(http.Flusher)
		_, hijacker := writer.(http.Hijacker)
		_, pusher := writer.(http.Pusher)
		_, notifier := writer.(http.CloseNotifier)
		_, readerFrom := writer.(io.ReaderFrom)
		assert.False(t, flusher || hijacker || pusher || notifier || readerFrom)
		writer.WriteHeader(http.StatusAccepted)
	}))
	handler.ServeHTTP(original, httptest.NewRequest(http.MethodGet, "/plain", nil))
	assert.Equal(t, http.StatusAccepted, original.status)
}

func newRouterTelemetry(t *testing.T) (*tracetest.SpanRecorder, *sdkmetric.ManualReader, []otelhttp.Option) {
	t.Helper()
	_, recorder, options := newTestHTTPTraceProvider(t)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return recorder, reader, append(options, otelhttp.WithMeterProvider(provider))
}

func routerDurationPoint(t *testing.T, reader *sdkmetric.ManualReader) metricdata.HistogramDataPoint[float64] {
	t.Helper()
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))
	for _, scope := range collected.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "http.server.request.duration" {
				continue
			}
			histogram, ok := item.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "request duration must be a histogram")
			require.Len(t, histogram.DataPoints, 1)
			return histogram.DataPoints[0]
		}
	}
	t.Fatal("HTTP duration metric was not recorded")
	return metricdata.HistogramDataPoint[float64]{Attributes: attribute.NewSet()}
}
