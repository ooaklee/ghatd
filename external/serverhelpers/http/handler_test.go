package serverhttp

import (
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/mux"
	middlewarehelper "github.com/ooaklee/ghatd/external/middleware/helper"
	"github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/observability/otelcache"
	"github.com/stretchr/testify/require"
	"github.com/tdewolff/minify"
	"go.uber.org/zap"
)

// testHTTPSettings retains the inputs of the extracted host regression suite;
// mapping below is test composition, not a production configuration shim.
type testHTTPSettings struct {
	CacheRefreshParameterKey                                                               string
	Component, CacheSkipURIPathRegex, HTTPRequestLogTrustedProxyCIDRs                      string
	CacheTTL                                                                               int
	HTTPRequestLogDetails, HTTPRequestLogPreservePathParameters, HTTPTraceNoiseSuppression bool
}

func initialiseHTTPHandler(c *testHTTPSettings, logger *zap.Logger, next http.Handler) (http.Handler, error) {
	return NewHandler(HandlerConfig{Component: c.Component, Logger: logger, IgnoreCompletionPaths: []string{"/v0/health/check"}, Policy: observability.HTTPServerOptionsConfig{
		RequestLogDetails: c.HTTPRequestLogDetails, PreservePathParameters: c.HTTPRequestLogPreservePathParameters,
		RedactPathPrefixes: []string{"/v0/auth/"}, TrustedProxyCIDRs: c.HTTPRequestLogTrustedProxyCIDRs,
		TraceNoiseSuppression: c.HTTPTraceNoiseSuppression, TraceSuppressedPaths: []string{"/v0/health/check", "/favicon.ico"}, TraceSuppressedPrefixes: []string{"/assets/"},
	}}, next)
}

func initialiseRouterMiddlewares(c *testHTTPSettings, _ *zap.Logger, m *minify.M, observer *otelcache.Observer) ([]mux.MiddlewareFunc, error) {
	return middlewarehelper.NewResponseMiddlewares(middlewarehelper.ResponseConfig{CacheCapacity: 10000, CacheTTL: time.Duration(c.CacheTTL) * time.Minute, RefreshKey: c.CacheRefreshParameterKey, SkipURIPathRegex: c.CacheSkipURIPathRegex, Minifier: m, CacheObserver: observer.HTTPResponseCacheObserver()})
}

func TestHandlerRequiresDependencies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		logger *zap.Logger
		next   http.Handler
	}{
		{"missing logger", nil, http.NotFoundHandler()}, {"missing handler", zap.NewNop(), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewHandler(HandlerConfig{Logger: tc.logger}, tc.next)
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
}
