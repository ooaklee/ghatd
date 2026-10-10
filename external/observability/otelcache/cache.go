// Package otelcache records bounded cache decisions without keys, URLs or payloads.
package otelcache

import (
	"context"
	"errors"
	"regexp"

	cache "github.com/victorspringer/http-cache"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Event identifies a cache decision recorded by an Observer, such as a hit,
// miss or store.
type Event string

const (
	HTTPResponse       = "http-response"
	Hit          Event = "hit"
	Miss         Event = "miss"
	Stale        Event = "stale"
	Refresh      Event = "refresh"
	Store        Event = "store"
	Purge        Event = "purge"
)

// Config supplies a provider and a startup-only vocabulary of logical caches.
// Names defaults to http-response; request data must never configure this list.
// MetricName defaults to ghatd.cache.event.count and may preserve an existing
// host metric name during adoption. Attributes and event semantics stay fixed.
type Config struct {
	MeterProvider metric.MeterProvider
	MetricName    string
	Names         []string
}

// Observer records bounded cache event counts for a startup-configured set of
// logical cache names.
type Observer struct {
	events metric.Int64Counter
	names  map[string]struct{}
}

var cacheName = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

// New validates and snapshots at most 64 cache names, defaulting to the HTTP
// response cache, and creates the event counter using the configured or global
// meter provider. Errors never include supplied values.
func New(config Config) (*Observer, error) {
	if len(config.Names) == 0 {
		config.Names = []string{HTTPResponse}
	}
	if len(config.Names) > 64 {
		return nil, errors.New("otelcache: at most 64 cache names are allowed")
	}
	names := make(map[string]struct{}, len(config.Names))
	for _, name := range config.Names {
		if !cacheName.MatchString(name) {
			return nil, errors.New("otelcache: invalid cache name")
		}
		names[name] = struct{}{}
	}
	if config.MetricName == "" {
		config.MetricName = "ghatd.cache.event.count"
	}
	provider := config.MeterProvider
	if provider == nil {
		provider = otel.GetMeterProvider()
	}
	counter, err := provider.Meter("github.com/ooaklee/ghatd/external/observability/otelcache").Int64Counter(
		config.MetricName, metric.WithUnit("{event}"),
		metric.WithDescription("Cache events; one lookup may also store a response."),
	)
	if err != nil {
		return nil, errors.New("otelcache: cannot create cache event counter")
	}
	return &Observer{events: counter, names: names}, nil
}

// Record drops unknown cache/event values. Nil/zero observers are safe for
// optional instrumentation. It records actual decisions, never inferred hits.
func (o *Observer) Record(ctx context.Context, name string, event Event) {
	if o == nil || o.events == nil {
		return
	}
	if _, ok := o.names[name]; !ok {
		return
	}
	switch event {
	case Hit, Miss, Stale, Refresh, Store, Purge:
	default:
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	o.events.Add(ctx, 1, metric.WithAttributes(attribute.String("cache", name), attribute.String("event", string(event))))
}

// HTTPResponseCacheObserver adapts the HTTP cache's actual events. Requests
// supply only context; raw keys, headers, paths and responses are never read.
func (o *Observer) HTTPResponseCacheObserver() cache.Observer {
	return func(event cache.CacheEvent) {
		var name Event
		switch event.Type {
		case cache.CacheEventHit:
			name = Hit
		case cache.CacheEventMiss:
			name = Miss
		case cache.CacheEventStale:
			name = Stale
		case cache.CacheEventRefresh:
			name = Refresh
		case cache.CacheEventStore:
			name = Store
		case cache.CacheEventPurge:
			name = Purge
		default:
			return
		}
		ctx := context.Background()
		if event.Request != nil {
			ctx = event.Request.Context()
		}
		o.Record(ctx, HTTPResponse, name)
	}
}
