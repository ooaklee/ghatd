package serverhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/spa"
	"github.com/stretchr/testify/require"
	"github.com/tdewolff/minify"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// This stateful sequence shares one cache/router to prove original targets
// survive a cache hit after a miss and that completion logs remain one-per-call.
func TestRequestDetailsPreserveSPARoutingAcrossCacheMissAndHit(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	logger := zap.New(core)
	config := &testHTTPSettings{
		Component: "fixture-api", CacheTTL: 1, CacheSkipURIPathRegex: "^/api/",
		HTTPRequestLogDetails: true, HTTPRequestLogPreservePathParameters: true, HTTPRequestLogTrustedProxyCIDRs: "10.0.0.0/24",
	}
	middlewares, err := initialiseRouterMiddlewares(config, logger, minify.New(), nil)
	require.NoError(t, err)
	router := ghatdrouter.NewRouter(nil, nil, middlewares...)
	router.GetRouter().HandleFunc("/api/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	var originCalls int
	rewrite := spa.NewHandleUpdatePathToIndex()
	require.NoError(t, spa.AttachRoutes(&spa.AttachRoutesRequest{
		Router: router, SpaFileSystem: fstest.MapFS{"dist/index.html": {Data: []byte("<html>SPA entry point</html>")}},
		HandleUpdatePathToIndexFunc: func(r *http.Request) *http.Request {
			originCalls++
			return rewrite(r)
		},
	}))
	handler, err := initialiseHTTPHandler(config, logger, router.GetRouter())
	require.NoError(t, err)
	for index, test := range []struct {
		target, path, route string
		status              int
	}{
		{"/about?token=secret-canary", "/about", "/", 200},
		{"/about?token=secret-canary", "/about", "/", 200},
		{"/pages/usage-id/print", "/pages/usage-id/print", "/", 200},
		{"/portal/usage-id", "/portal/usage-id", "/", 200},
		{"/api/items/usage-id?token=secret-canary", "/api/items/usage-id", "/api/items/{id}", 204},
		{"/api/unmatched/usage-id", "/api/unmatched/usage-id", "/", 200},
		{"/v0/auth/secret-canary?token=secret-canary", "/v0/auth/[redacted]", "/", 200},
	} {
		request := httptest.NewRequest("GET", test.target, nil)
		request.Header.Set("User-Agent", "TroubleshootingBrowser/1.0")
		request.Header.Set("X-Forwarded-For", "198.51.100.99, 192.0.2.7")
		request.RemoteAddr = "10.0.0.1:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code)
		if test.status == 200 {
			require.Contains(t, response.Body.String(), "SPA entry point")
		}
		entries := logs.FilterMessage("http request completed").All()
		require.Len(t, entries, index+1, "exactly one completion log per request")
		fields := entries[len(entries)-1].ContextMap()
		durationMS, ok := fields["duration_ms"].(float64)
		require.True(t, ok, "handler duration must be a numeric millisecond value")
		require.GreaterOrEqual(t, durationMS, float64(0))
		require.Equal(t, test.route, fields["route"])
		require.Equal(t, test.path, fields["url.path"])
		require.Equal(t, "TroubleshootingBrowser/1.0", fields["user_agent.original"])
		require.Equal(t, "192.0.2.7", fields["client.address"])
		require.Equal(t, "forwarded", fields["client.address.source"])
		encoded, err := json.Marshal(fields)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "secret-canary")
	}
	require.Equal(t, 5, originCalls, "the second about request must be served from cache")
	require.Len(t, logs.FilterMessage("http request completed").All(), 7)
}

func TestRequestDetailsDoNotTrustForwardedHeadersByDefault(t *testing.T) {
	for _, tc := range []struct {
		name, proxies, address, source string
		bad                            bool
	}{
		{"unconfigured proxy", "", "192.0.2.7", "peer", false},
		{"explicitly trusted peer", "192.0.2.0/24", "198.51.100.99", "forwarded", false},
		{"invalid proxy configuration", "invalid-secret-config", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.InfoLevel)
			handler, err := initialiseHTTPHandler(&testHTTPSettings{HTTPRequestLogDetails: true, HTTPRequestLogTrustedProxyCIDRs: tc.proxies}, zap.New(core), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
			if tc.bad {
				require.Error(t, err)
				require.NotContains(t, err.Error(), tc.proxies)
				return
			}
			require.NoError(t, err)
			request := httptest.NewRequest("GET", "/about", nil)
			request.Header.Set("X-Forwarded-For", "198.51.100.99")
			request.RemoteAddr = "192.0.2.7:1234"
			handler.ServeHTTP(httptest.NewRecorder(), request)
			fields := logs.FilterMessage("http request completed").All()[0].ContextMap()
			require.Equal(t, tc.address, fields["client.address"])
			require.Equal(t, tc.source, fields["client.address.source"])
			require.Equal(t, "198.51.100.99", fields["http.request.header.x_forwarded_for"])
		})
	}
}

func TestRequestDetailsParameterValuesRequireSeparateOptIn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		preserve bool
	}{{"redacted", false}, {"preserved", true}} {
		t.Run(tc.name, func(t *testing.T) {
			preserve := tc.preserve
			core, logs := observer.New(zap.InfoLevel)
			router := ghatdrouter.NewRouter(nil, nil).GetRouter()
			router.HandleFunc("/api/items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			handler, err := initialiseHTTPHandler(&testHTTPSettings{HTTPRequestLogDetails: true, HTTPRequestLogPreservePathParameters: preserve}, zap.New(core), router)
			require.NoError(t, err)
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/items/example-id?secret=never-log", nil))
			fields := logs.FilterMessage("http request completed").All()[0].ContextMap()
			wantPath := "/api/items/{id}"
			if preserve {
				wantPath = "/api/items/example-id"
			}
			require.Equal(t, wantPath, fields["url.path"])
			encoded, err := json.Marshal(fields)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "never-log")
		})
	}
}
