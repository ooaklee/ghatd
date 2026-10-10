package middlewarehelper

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	cache "github.com/victorspringer/http-cache"
)

func responseFixture() ResponseConfig {
	return ResponseConfig{CacheCapacity: 16, CacheTTL: time.Minute, RefreshKey: "refresh", SkipHTTPHeader: "X-Do-Not-Cache", SkipURIPathRegex: `^/api/`, Minifier: NewResponseMinifier()}
}
func responseChain(t *testing.T, cfg ResponseConfig, next http.Handler) http.Handler {
	t.Helper()
	middlewares, err := NewResponseMiddlewares(cfg)
	require.NoError(t, err)
	require.Len(t, middlewares, 4)
	for i := len(middlewares) - 1; i >= 0; i-- {
		next = middlewares[i](next)
	}
	return next
}
func TestResponseMinifierFormats(t *testing.T) {
	type formatCase struct {
		name, mediaType, body string
		changed               bool
	}
	for _, test := range []formatCase{
		{"HTML", "text/html", "<html>  <body>hello</body> </html>", true},
		{"SVG", "image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg">   <rect width="10" height="10"/>   </svg>`, true},
		{"JSON", "application/json", `{ "value" : 1 }`, true},
		{"vendor JSON", "application/vnd.example+json", `{ "value" : 1 }`, true},
		{"CSS unchanged", "text/css", "p { color: red; }", false},
		{"JavaScript unchanged", "application/javascript", "const value = 1;  ", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewResponseMinifier().Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.mediaType)
				_, _ = io.WriteString(w, test.body)
			}))
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, httptest.NewRequest("GET", "/page", nil))
			require.Equal(t, 200, out.Code)
			if test.changed {
				require.Less(t, out.Body.Len(), len(test.body))
				require.NotEmpty(t, out.Body.String())
			} else {
				require.Equal(t, test.body, out.Body.String())
			}
		})
	}
}

func TestResponseConfigurationValidation(t *testing.T) {
	type configurationCase struct {
		name                      string
		capacity                  int
		ttl                       time.Duration
		regex                     string
		nilMinifier, nilDecorator bool
		wantStage                 string
	}
	for _, test := range []configurationCase{
		{name: "valid", capacity: 16, ttl: time.Minute},
		{name: "zero TTL preserves vendor behavior", capacity: 16},
		{name: "negative TTL", capacity: 16, ttl: -time.Second, wantStage: "cache client"},
		{name: "zero capacity", ttl: time.Minute, wantStage: "memory adapter"},
		{name: "negative capacity", capacity: -1, ttl: time.Minute, wantStage: "memory adapter"},
		{name: "capacity one", capacity: 1, ttl: time.Minute, wantStage: "memory adapter"},
		{name: "invalid regex", capacity: 16, ttl: time.Minute, regex: "[", wantStage: "skip path regex"},
		{name: "nil minifier", capacity: 16, ttl: time.Minute, nilMinifier: true, wantStage: "minifier required"},
		{name: "nil decorator result", capacity: 16, ttl: time.Minute, nilDecorator: true, wantStage: "cache decorator returned nil"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := responseFixture()
			cfg.CacheCapacity = test.capacity
			cfg.CacheTTL = test.ttl
			cfg.SkipURIPathRegex = test.regex
			if test.nilMinifier {
				cfg.Minifier = nil
			}
			if test.nilDecorator {
				cfg.CacheWrapper = func(mux.MiddlewareFunc) mux.MiddlewareFunc { return nil }
			}
			middlewares, err := NewResponseMiddlewares(cfg)
			if test.wantStage != "" {
				require.ErrorContains(t, err, test.wantStage)
				require.Nil(t, middlewares)
				return
			}
			require.NoError(t, err)
			require.Len(t, middlewares, 4)
		})
	}
}

func TestResponseCacheBehavior(t *testing.T) {
	type cacheCase struct {
		name, path                               string
		refresh, skipHeader, bypass, nilObserver bool
		wantCalls                                int
		wantEvents                               []cache.CacheEventType
	}
	for _, test := range []cacheCase{
		{name: "miss store hit", path: "/page", wantCalls: 1, wantEvents: []cache.CacheEventType{cache.CacheEventMiss, cache.CacheEventStore, cache.CacheEventHit}},
		{name: "refresh goes to origin", path: "/page", refresh: true, wantCalls: 2, wantEvents: []cache.CacheEventType{cache.CacheEventMiss, cache.CacheEventStore, cache.CacheEventRefresh, cache.CacheEventStore}},
		{name: "path skip", path: "/api/items", wantCalls: 2},
		{name: "response header skip", path: "/page", skipHeader: true, wantCalls: 2, wantEvents: []cache.CacheEventType{cache.CacheEventMiss, cache.CacheEventMiss}},
		{name: "host private route bypass", path: "/private", bypass: true, wantCalls: 2},
		{name: "nil observer omitted", path: "/page", nilObserver: true, wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := responseFixture()
			calls, decorations := 0, 0
			var events []cache.CacheEventType
			if !test.nilObserver {
				cfg.CacheObserver = func(e cache.CacheEvent) { events = append(events, e.Type) }
			}
			cfg.CacheWrapper = func(cached mux.MiddlewareFunc) mux.MiddlewareFunc {
				decorations++
				return func(next http.Handler) http.Handler {
					withCache := cached(next)
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						// Content type precedes the cache decorator, including bypassed requests.
						if r.Header.Get("Content-Type") == "application/json" {
							require.Equal(t, "application/json", w.Header().Get("Content-Type"))
						}
						if test.bypass {
							next.ServeHTTP(w, r)
						} else {
							withCache.ServeHTTP(w, r)
						}
					})
				}
			}
			handler := responseChain(t, cfg, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if test.skipHeader {
					w.Header().Set("X-Do-Not-Cache", "true")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{ "value" : 1 }`)
			}))
			require.Zero(t, calls, "construction never preloads the cache")
			require.Equal(t, 1, decorations, "only cache is decorated, once at construction")
			for i := 0; i < 2; i++ {
				target := test.path
				if test.refresh && i == 1 {
					target += "?refresh=1"
				}
				req := httptest.NewRequest("GET", target, nil)
				req.Header.Set("Content-Type", "application/json")
				out := httptest.NewRecorder()
				handler.ServeHTTP(out, req)
				require.Equal(t, 200, out.Code)
				require.Equal(t, `{"value":1}`, out.Body.String())
			}
			require.Equal(t, test.wantCalls, calls)
			require.Equal(t, test.wantEvents, events)
		})
	}
}

func TestResponseCacheBypassPreservesCompression(t *testing.T) {
	type compressionCase struct {
		name         string
		bypass, gzip bool
		wantCalls    int
	}
	for _, test := range []compressionCase{
		{"cache without gzip", false, false, 1}, {"cache with gzip", false, true, 1},
		{"private bypass without gzip", true, false, 2}, {"private bypass with gzip", true, true, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := responseFixture()
			calls := 0
			if test.bypass {
				cfg.CacheWrapper = func(mux.MiddlewareFunc) mux.MiddlewareFunc {
					return func(next http.Handler) http.Handler { return next }
				}
			}
			payload := `{ "message" : "` + strings.Repeat("word ", 500) + `" }`
			handler := responseChain(t, cfg, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, payload)
			}))
			for i := 0; i < 2; i++ {
				req := httptest.NewRequest("GET", "/page", nil)
				if test.gzip {
					req.Header.Set("Accept-Encoding", "gzip")
				}
				out := httptest.NewRecorder()
				handler.ServeHTTP(out, req)
				actual := out.Body.Bytes()
				if test.gzip {
					require.Equal(t, "gzip", out.Header().Get("Content-Encoding"))
					reader, err := gzip.NewReader(out.Body)
					require.NoError(t, err)
					actual, err = io.ReadAll(reader)
					require.NoError(t, err)
					require.NoError(t, reader.Close())
				} else {
					require.Empty(t, out.Header().Get("Content-Encoding"))
				}
				require.Equal(t, `{"message":"`+strings.Repeat("word ", 500)+`"}`, string(actual))
			}
			require.Equal(t, test.wantCalls, calls)
		})
	}
}
