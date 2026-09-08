package otelhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/observability"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestWrapExportsWholeDispatch verifies the composition rather than just its
// individual middleware: every normal/recovered dispatch finishes all signals
// exactly once, and logs retain the actual server span's native IDs.
func TestWrapExportsWholeDispatch(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, route string
		status                      int
		panic                       bool
	}{
		{"matched", "GET", "/items/private-canary?token=private-canary", "/items/{id}", 204, false},
		{"fallback", "GET", "/private-canary", "unknown", 404, false},
		{"method fallback", "POST", "/items/private-canary", "unknown", 405, false},
		{"redirect", "GET", "/items//private-canary", "unknown", 301, false},
		{"short circuit", "GET", "/blocked/private-canary", "/blocked/{id}", 403, false},
		{"panic", "GET", "/panic", "/panic", 500, true},
		{"committed panic", "GET", "/stream", "/stream", 202, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			spans := tracetest.NewInMemoryExporter()
			traces := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
			reader := sdkmetric.NewManualReader()
			meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			logs := &logExporter{}
			logProvider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logs)))
			oldTrace, oldMeter, oldPropagation := otel.GetTracerProvider(), otel.GetMeterProvider(), otel.GetTextMapPropagator()
			otel.SetTracerProvider(traces)
			otel.SetMeterProvider(meters)
			otel.SetTextMapPropagator(propagation.TraceContext{})
			t.Cleanup(func() {
				otel.SetTracerProvider(oldTrace)
				otel.SetMeterProvider(oldMeter)
				otel.SetTextMapPropagator(oldPropagation)
				require.NoError(t, logProvider.Shutdown(ctx))
				require.NoError(t, meters.Shutdown(ctx))
				require.NoError(t, traces.Shutdown(ctx))
			})
			core, local := observer.New(zap.InfoLevel)
			logger := observability.TeeLogger(zap.New(core), logProvider)
			router := ghatdrouter.NewRouter(nil, nil, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if template, _ := mux.CurrentRoute(r).GetPathTemplate(); template == "/blocked/{id}" {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					next.ServeHTTP(w, r)
				})
			}).GetRouter()
			router.HandleFunc("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "private-canary", mux.Vars(r)["id"])
				assert.Equal(t, "private-canary", r.URL.Query().Get("token"))
				w.WriteHeader(http.StatusNoContent)
			}).Methods(http.MethodGet)
			router.HandleFunc("/blocked/{id}", func(http.ResponseWriter, *http.Request) { t.Fatal("short circuit did not run") })
			router.HandleFunc("/panic", func(http.ResponseWriter, *http.Request) { panic("private-canary") })
			router.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				w.(http.Flusher).Flush()
				panic("private-canary")
			})
			request := httptest.NewRequest(tc.method, tc.target, nil)
			request.Header.Set("traceparent", "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01")
			response := httptest.NewRecorder()
			Wrap("example-api", logger, router).ServeHTTP(response, request)
			assert.Equal(t, tc.status, response.Code)
			ended := spans.GetSpans()
			require.Len(t, ended, 1)
			assert.Equal(t, "0102030405060708090a0b0c0d0e0f10", ended[0].SpanContext.TraceID().String())
			assert.Equal(t, "0102030405060708", ended[0].Parent.SpanID().String())
			if tc.panic {
				assert.Equal(t, codes.Error, ended[0].Status.Code)
			}
			assert.Len(t, local.FilterMessage("http request completed").All(), 1)
			records := logs.Records()
			require.Len(t, records, 1)
			assert.Equal(t, ended[0].SpanContext.TraceID(), records[0].TraceID())
			assert.Equal(t, ended[0].SpanContext.SpanID(), records[0].SpanID())
			attributes := make(map[string]otellog.Value)
			records[0].WalkAttributes(func(field otellog.KeyValue) bool { attributes[field.Key] = field.Value; return true })
			assert.EqualValues(t, tc.status, attributes["status"].AsInt64())
			assert.Equal(t, tc.route, attributes["route"].AsString())
			if tc.panic {
				assert.Equal(t, "panic", attributes["error.type"].AsString())
			}
			var metrics metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(ctx, &metrics))
			var count uint64
			for _, scope := range metrics.ScopeMetrics {
				for _, instrument := range scope.Metrics {
					if instrument.Name != "http.server.request.duration" {
						continue
					}
					for _, point := range instrument.Data.(metricdata.Histogram[float64]).DataPoints {
						count += point.Count
						status, found := point.Attributes.Value("http.response.status_code")
						assert.True(t, found)
						assert.EqualValues(t, tc.status, status.AsInt64())
					}
				}
			}
			assert.EqualValues(t, 1, count)
			for _, value := range []any{ended, metrics, attributes, local.All()} {
				encoded, err := json.Marshal(value)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), "private-canary")
			}
		})
	}
}

type logExporter struct {
	sync.Mutex
	records []sdklog.Record
}

func (exporter *logExporter) Export(_ context.Context, records []sdklog.Record) error {
	exporter.Lock()
	defer exporter.Unlock()
	for _, record := range records {
		exporter.records = append(exporter.records, record.Clone())
	}
	return nil
}

func (exporter *logExporter) Records() []sdklog.Record {
	exporter.Lock()
	defer exporter.Unlock()
	return append([]sdklog.Record(nil), exporter.records...)
}

func (*logExporter) Shutdown(context.Context) error   { return nil }
func (*logExporter) ForceFlush(context.Context) error { return nil }
