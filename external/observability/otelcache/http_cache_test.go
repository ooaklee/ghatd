package otelcache

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cache "github.com/victorspringer/http-cache"
	"github.com/victorspringer/http-cache/adapter/memory"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestHTTPResponseCacheObserverReflectsActualCacheDecisions(t *testing.T) {
	metrics, reader := newDecisionMetrics(t)
	adapter, err := memory.NewAdapter(memory.AdapterWithCapacity(10), memory.AdapterWithAlgorithm(memory.LRU))
	require.NoError(t, err)
	client, err := cache.NewClient(cache.ClientWithAdapter(adapter), cache.ClientWithTTL(time.Hour),
		cache.ClientWithRefreshKey("refresh"), cache.ClientWithSkipCacheURIPathRegex(regexp.MustCompile("^/api/")),
		cache.ClientWithObserver(metrics.HTTPResponseCacheObserver()), cache.ClientWithPurge())
	require.NoError(t, err)
	originCalls := 0
	handler := client.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		originCalls++
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("private-response-canary"))
	}))
	for _, step := range []struct {
		method, path        string
		status, originCalls int
	}{
		{http.MethodGet, "/private-path-canary?query=private-query-canary", 200, 1},
		{http.MethodGet, "/private-path-canary?query=private-query-canary", 200, 1},
		{http.MethodGet, "/private-path-canary?query=private-query-canary&refresh=true", 200, 2},
		{http.MethodGet, "/private-path-canary?query=private-query-canary", 200, 2},
		{http.MethodGet, "/api/private-registration-canary", 200, 3},
		{http.MethodPost, "/private-path-canary", 200, 4},
		{"PURGE", "/private-path-canary?query=private-query-canary", 204, 4},
	} {
		request := httptest.NewRequest(step.method, step.path, nil)
		request.Header.Set("Authorization", "Bearer private-token-canary")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assert.Equal(t, step.status, response.Code)
		assert.Equal(t, step.originCalls, originCalls, "origin calls after %s %s", step.method, step.path)
		if step.status == 200 {
			assert.Equal(t, "private-response-canary", response.Body.String())
		}
	}
	collected := collectDecisions(t, reader)
	require.Len(t, collected.ScopeMetrics, 1)
	require.Len(t, collected.ScopeMetrics[0].Metrics, 1)
	counts := map[string]int64{}
	for _, point := range collected.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64]).DataPoints {
		require.Equal(t, 2, point.Attributes.Len())
		cacheName, _ := point.Attributes.Value("cache")
		assert.Equal(t, "http-response", cacheName.AsString())
		event, _ := point.Attributes.Value("event")
		counts[event.AsString()] += point.Value
	}
	assert.Equal(t, map[string]int64{"miss": 1, "store": 2, "hit": 2, "refresh": 1, "purge": 1}, counts)
	encoded, err := json.Marshal(collected)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-")
}

func TestHTTPResponseCacheObserverAllowsMissingRequestAndRejectsUnknownEvents(t *testing.T) {
	metrics, reader := newDecisionMetrics(t)
	observe := metrics.HTTPResponseCacheObserver()
	observe(cache.CacheEvent{Type: cache.CacheEventStale})
	observe(cache.CacheEvent{Type: cache.CacheEventType("private-event-canary")})
	(*Observer)(nil).HTTPResponseCacheObserver()(cache.CacheEvent{Type: cache.CacheEventHit})
	collected := collectDecisions(t, reader)
	sum := collected.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(1), sum.DataPoints[0].Value)
	value, _ := sum.DataPoints[0].Attributes.Value("event")
	assert.Equal(t, "stale", value.AsString())
}
