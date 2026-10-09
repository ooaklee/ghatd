package middlewarehelper

import (
	"fmt"
	"regexp"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/middleware/contenttype"
	"github.com/tdewolff/minify"
	"github.com/tdewolff/minify/html"
	"github.com/tdewolff/minify/json"
	"github.com/tdewolff/minify/svg"
	cache "github.com/victorspringer/http-cache"
	"github.com/victorspringer/http-cache/adapter/memory"
)

// ResponseConfig explicitly selects a process-local response cache and borrowed
// minifier. Hosts own route privacy, instrumentation and configuration units.
// Construction allocates an empty LRU cache without starting goroutines or I/O.
type ResponseConfig struct {
	// CacheCapacity is the LRU entry limit; the adapter requires more than one.
	CacheCapacity int
	// CacheTTL is the vendor's default fresh lifetime; zero is accepted, negatives
	// are refused. Host units must be converted before calling this helper.
	CacheTTL time.Duration
	// RefreshKey names the vendor's cache-refresh query parameter.
	RefreshKey string
	// SkipHTTPHeader names a response header that prevents cache storage.
	SkipHTTPHeader string
	// SkipURIPathRegex exempts matching request paths from the cache.
	SkipURIPathRegex string
	// CacheObserver optionally receives actual cache decisions. Nil omits the
	// vendor option. The caller owns any captured observer state and its lifetime.
	CacheObserver cache.Observer
	// Minifier is required and borrowed. Finish registering formats before serving.
	Minifier *minify.M
	// CacheWrapper optionally decorates only the cache middleware. Use it to bypass
	// private routes while preserving compression/minification. It runs once at
	// construction and must return a non-nil middleware; nil means no decorator.
	CacheWrapper func(mux.MiddlewareFunc) mux.MiddlewareFunc
}

// NewResponseMinifier registers HTML, SVG and all media types ending in /json or
// +json. CSS and JavaScript are deliberately unchanged. Customise before serving.
func NewResponseMinifier() *minify.M {
	m := minify.New()
	m.AddFunc("text/html", html.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)
	m.AddFuncRegexp(regexp.MustCompile(`[/+]json$`), json.Minify)
	return m
}

// NewResponseMiddlewares returns content type, decorated cache, gzip and minify,
// in that order. It attaches no routes or authentication/privacy policy. Missing
// minifiers, invalid capacity/TTL/regex or nil decorator results fail construction.
// Errors retain underlying configuration causes with their construction stage.
func NewResponseMiddlewares(cfg ResponseConfig) ([]mux.MiddlewareFunc, error) {
	if cfg.Minifier == nil {
		return nil, fmt.Errorf("response middleware: minifier required")
	}
	adapter, err := memory.NewAdapter(memory.AdapterWithAlgorithm(memory.LRU), memory.AdapterWithCapacity(cfg.CacheCapacity))
	if err != nil {
		return nil, fmt.Errorf("response middleware: memory adapter: %w", err)
	}
	skip, err := regexp.Compile(cfg.SkipURIPathRegex)
	if err != nil {
		return nil, fmt.Errorf("response middleware: skip path regex: %w", err)
	}
	options := []cache.ClientOption{
		cache.ClientWithAdapter(adapter), cache.ClientWithTTL(cfg.CacheTTL),
		cache.ClientWithRefreshKey(cfg.RefreshKey), cache.ClientWithExpiresHeader(),
		cache.ClientWithSkipCacheResponseHeader(cfg.SkipHTTPHeader), cache.ClientWithSkipCacheURIPathRegex(skip),
	}
	if cfg.CacheObserver != nil {
		options = append(options, cache.ClientWithObserver(cfg.CacheObserver))
	}
	client, err := cache.NewClient(options...)
	if err != nil {
		return nil, fmt.Errorf("response middleware: cache client: %w", err)
	}
	var cached mux.MiddlewareFunc = client.Middleware
	if cfg.CacheWrapper != nil {
		cached = cfg.CacheWrapper(cached)
		if cached == nil {
			return nil, fmt.Errorf("response middleware: cache decorator returned nil")
		}
	}
	return []mux.MiddlewareFunc{contenttype.NewContentType, cached, gziphandler.GzipHandler, cfg.Minifier.Middleware}, nil
}
