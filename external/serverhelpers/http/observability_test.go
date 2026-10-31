package serverhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	ghatdobservability "github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/observability/otelcache"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
	"github.com/tdewolff/minify"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// This stateful sequence intentionally shares a cache to assert miss then hit
// transitions and cumulative observations; isolated table cases would erase them.
func TestServerCacheWiringMeasuresLookupsAndSkipsAPI(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	metrics, err := otelcache.New(otelcache.Config{MeterProvider: provider, MetricName: "fixture.cache.event.count"})
	require.NoError(t, err)
	middlewares, err := initialiseRouterMiddlewares(&testHTTPSettings{
		CacheTTL: 1, CacheSkipURIPathRegex: "^/api/", CacheRefreshParameterKey: "refresh",
	}, zap.NewNop(), minify.New(), metrics)
	require.NoError(t, err)
	router := ghatdrouter.NewRouter(nil, nil, middlewares...).GetRouter()
	originCalls := 0
	handler := func(w http.ResponseWriter, request *http.Request) {
		originCalls++
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("private-cache-fixture"))
	}
	router.HandleFunc("/page/{id}", handler).Methods(http.MethodGet)
	router.HandleFunc("/api/{id}", handler).Methods(http.MethodGet)
	for _, target := range []string{"/page/private-cache-fixture?token=private-cache-fixture", "/page/private-cache-fixture?token=private-cache-fixture", "/api/private-cache-fixture", "/api/private-cache-fixture"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, "private-cache-fixture", response.Body.String())
	}
	require.Equal(t, 3, originCalls, "only the second cacheable request should skip the origin")
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))
	counts := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name != "fixture.cache.event.count" {
				continue
			}
			for _, point := range instrument.Data.(metricdata.Sum[int64]).DataPoints {
				cache, _ := point.Attributes.Value("cache")
				event, _ := point.Attributes.Value("event")
				require.Equal(t, "http-response", cache.AsString())
				counts[event.AsString()] += point.Value
			}
		}
	}
	require.Equal(t, map[string]int64{"miss": 1, "store": 1, "hit": 1}, counts)
	encoded, err := json.Marshal(collected)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-cache-fixture")
}

func TestServerObservesWholeRouterExactlyOnce(t *testing.T) {
	for _, test := range []struct {
		name, method, path, route string
		status                    int
	}{
		{"matched", "GET", "/api/items/private-canary?token=private-canary", "/api/items/{id}", 204},
		{"not found", "GET", "/missing-private-canary", "unknown", 404},
		{"unknown method", "private-canary", "/missing-private-canary", "unknown", 404},
		{"method not allowed", "POST", "/api/items/private-canary", "unknown", 405},
		{"clean path redirect", "GET", "/api//items/private-canary", "unknown", 301},
		{"recovered panic", "GET", "/api/panic", "/api/panic", 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			traces := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			reader := sdkmetric.NewManualReader()
			metrics := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			previousTracer, previousMeter, previousPropagation := otel.GetTracerProvider(), otel.GetMeterProvider(), otel.GetTextMapPropagator()
			otel.SetTracerProvider(traces)
			otel.SetMeterProvider(metrics)
			otel.SetTextMapPropagator(propagation.TraceContext{})
			t.Cleanup(func() {
				otel.SetTracerProvider(previousTracer)
				otel.SetMeterProvider(previousMeter)
				otel.SetTextMapPropagator(previousPropagation)
				require.NoError(t, traces.Shutdown(context.Background()))
				require.NoError(t, metrics.Shutdown(context.Background()))
			})
			core, logs := observer.New(zap.InfoLevel)
			appLogger := zap.New(core)
			middlewares, err := initialiseRouterMiddlewares(&testHTTPSettings{
				Component: "fixture-api", CacheTTL: 1, CacheSkipURIPathRegex: "^/api/",
			}, appLogger, minify.New(), nil)
			require.NoError(t, err)
			router := ghatdrouter.NewRouter(nil, nil, middlewares...)
			router.GetRouter().HandleFunc("/api/items/{id}", func(w http.ResponseWriter, request *http.Request) {
				require.Equal(t, "private-canary", mux.Vars(request)["id"])
				require.Equal(t, "private-canary", request.URL.Query().Get("token"))
				w.WriteHeader(http.StatusNoContent)
			}).Methods(http.MethodGet)
			router.GetRouter().HandleFunc("/api/panic", func(http.ResponseWriter, *http.Request) {
				panic("private-canary")
			})
			handler, err := initialiseHTTPHandler(&testHTTPSettings{Component: "fixture-api"}, appLogger, router.GetRouter())
			require.NoError(t, err)
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("traceparent", "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			require.Equal(t, test.status, recorder.Code)
			spans := exporter.GetSpans()
			require.Len(t, spans, 1, "matched route middleware must not add a second server span")
			require.Equal(t, "0102030405060708090a0b0c0d0e0f10", spans[0].SpanContext.TraceID().String())
			require.Equal(t, "0102030405060708", spans[0].Parent.SpanID().String())
			completion := logs.FilterMessage("http request completed").All()
			require.Len(t, completion, 1)
			require.EqualValues(t, test.status, completion[0].ContextMap()["status"])
			require.Equal(t, test.route, completion[0].ContextMap()["route"])
			if test.name == "unknown method" {
				require.Equal(t, "OTHER", completion[0].ContextMap()["method"])
			}
			require.Equal(t, spans[0].SpanContext.SpanID().String(), completion[0].ContextMap()[ghatdobservability.SpanIDKey])
			var collected metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(context.Background(), &collected))
			var observations uint64
			for _, scope := range collected.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name == "http.server.request.duration" {
						for _, point := range metric.Data.(metricdata.Histogram[float64]).DataPoints {
							observations += point.Count
							status, ok := point.Attributes.Value("http.response.status_code")
							require.True(t, ok)
							require.EqualValues(t, test.status, status.AsInt64())
						}
					}
				}
			}
			require.EqualValues(t, 1, observations)
			for _, telemetry := range []any{spans, completion, collected} {
				encoded, err := json.Marshal(telemetry)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), "private-canary")
			}
		})
	}
}
