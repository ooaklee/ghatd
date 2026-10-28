package seo

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

// PublicSitemapMode selects the source of public sitemap responses. It does not
// change administrative generation, downloads or the stored sitemap inventory.
type PublicSitemapMode uint8

const (
	// PublicSitemapModeFile preserves safe file reads and embedded fallback.
	PublicSitemapModeFile PublicSitemapMode = iota
	// PublicSitemapModeDatabase renders current shared storage for every request.
	// Storage, filter and rendering failures never fall back to a local file.
	PublicSitemapModeDatabase
)

// PublicSitemapItemsFilter applies a host's publication or crawler policy to
// current sitemap candidates. It receives a copy of the repository slice and
// must be safe for concurrent requests. Returning an error fails the public
// request closed; returning an empty slice intentionally publishes an empty
// sitemap. The filter never runs for administrative exports or persists changes.
type PublicSitemapItemsFilter func(context.Context, []SitemapItem) ([]SitemapItem, error)

// GetPublicSitemap serves the configured public source. File mode preserves the
// optional safe path selector; database mode ignores it, reads current rows and
// applies the host filter before using the shared XML renderer. Database mode
// returns no-store responses and wraps failures with ErrPublicSitemapUnavailable
// while preserving diagnostic causes for service callers.
func (s *Service) GetPublicSitemap(ctx context.Context, req *DownloadSitemapByPathRequest) (*DownloadSitemapByPathResponse, error) {
	if s == nil {
		return nil, ErrPublicSitemapUnavailable
	}
	if s.PublicSitemapMode == PublicSitemapModeFile {
		return s.DownloadSitemapByPath(ctx, req)
	}
	if s.PublicSitemapMode != PublicSitemapModeDatabase {
		return nil, fmt.Errorf("%w: unsupported source mode %d", ErrPublicSitemapUnavailable, s.PublicSitemapMode)
	}
	if ctx == nil || s.SitemapRepository == nil {
		return nil, fmt.Errorf("%w: %w", ErrPublicSitemapUnavailable, ErrSitemapItemDatabaseError)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPublicSitemapUnavailable, err)
	}
	items, err := s.SitemapRepository.GetAllSitemapItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPublicSitemapUnavailable, err)
	}
	if s.PublicSitemapItemsFilter != nil {
		items, err = s.PublicSitemapItemsFilter(ctx, append([]SitemapItem(nil), items...))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrPublicSitemapUnavailable, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPublicSitemapUnavailable, err)
	}
	generated, err := s.GenerateXML(items)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPublicSitemapUnavailable, err)
	}
	return &DownloadSitemapByPathResponse{
		FileName: "sitemap.xml", ContentType: "application/xml; charset=utf-8",
		Content: []byte(generated), CacheControl: "no-store",
	}, nil
}

// NeverCachePublicSitemap wraps a host's response-cache middleware. Requests for
// PublicSitemapPath bypass cache lookup and storage and receive no-store headers,
// including failures. Other routes keep the original middleware. Wrap the cache
// before installing it; a response header alone cannot bypass an existing hit.
func NeverCachePublicSitemap(cache mux.MiddlewareFunc) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		cached := cache(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == PublicSitemapPath {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
				return
			}
			cached.ServeHTTP(w, r)
		})
	}
}
