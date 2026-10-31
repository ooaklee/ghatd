package publication

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/indexnow"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/seo"
	"github.com/ooaklee/ghatd/external/spa"
)

// FrontendBootstrap creates the shared build-aware SPA bootstrap.
func FrontendBootstrap(files fs.FS, prefix string, middlewares []mux.MiddlewareFunc) (*spa.Bootstrap, error) {
	return newFrontendBootstrap(files, prefix, middlewares)
}

// SitemapFilterConfig supplies the immutable build inventory and live CMS reader.
type SitemapFilterConfig struct {
	Origin         string
	Files          fs.FS
	BuildDirectory string
	Articles       ArticleSource
	Now            func() time.Time
	PathAllowed    func(string) bool
}

// NewSitemapFilter validates the origin and optional build inventory.
func NewSitemapFilter(c SitemapFilterConfig) (*SitemapFilter, error) {
	f, err := newPublicSitemapFilter(c.Origin, c.Files, c.BuildDirectory)
	if err != nil {
		return nil, err
	}
	f.articles, f.now, f.pathAllowed = c.Articles, c.Now, c.PathAllowed
	return f, nil
}

// SetArticles binds the CMS reader during startup before serving requests.
func (f *SitemapFilter) SetArticles(source ArticleSource) { f.articles = source }

// AllowIndexNowURL checks the same origin and build restrictions.
func (f *SitemapFilter) AllowIndexNowURL(ctx context.Context, raw string) (bool, error) {
	return f.allowIndexNowURL(ctx, raw)
}

// Eligibility checks CMS publication at the completed pass boundary.
func (f *SitemapFilter) Eligibility(lookup func(context.Context, string) (*post.Post, error)) func(context.Context, string, time.Time) (bool, error) {
	return f.indexNowEligibility(lookup)
}

// NewPublicSEOService enables live sitemap delivery with the provided filter.
func NewPublicSEOService(request seo.NewServiceRequest, filter *SitemapFilter) *seo.Service {
	if filter == nil {
		return newPublicSEOService(request, nil)
	}
	return newPublicSEOService(request, filter)
}

// PublicContentURL maps supported CMS items to canonical public routes.
func PublicContentURL(item post.Post) string { return publicContentURL(item) }
func publicContentURL(item post.Post) string {
	if item.Type == post.PostTypeArticle && strings.HasPrefix(item.UrlFriendlyId, "article-") {
		return publicArticlePath(&item)
	}
	if item.Type == post.PostTypeChangelog && strings.HasPrefix(item.UrlFriendlyId, "changelog-") && publicArticleSlug.MatchString(item.UrlFriendlyId) {
		return "/whats-new/" + item.UrlFriendlyId
	}
	return ""
}
func (f *SitemapFilter) allowIndexNowURL(ctx context.Context, raw string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	p, ok := canonicalPublicSitemapPath(raw, f.origin)
	return ok && (f.pathAllowed == nil || f.pathAllowed(p)) && (f.allowedPaths == nil || f.allowedPaths[strings.ToLower(p)]), nil
}

// IndexNowConfig is resolved configuration; disabled integration performs no I/O.
type IndexNowConfig struct {
	Enabled                                bool
	APIKey, SiteURL, Endpoint, KeyLocation string
	Attempts                               indexnow.AttemptRepository
}

// BuildIndexNow constructs the service when explicitly enabled.
func BuildIndexNow(c IndexNowConfig) (*indexnow.Service, error) {
	if !c.Enabled {
		return nil, nil
	}
	if c.Attempts == nil {
		return nil, errors.New("indexnow attempt storage required")
	}
	if raw := c.KeyLocation; raw != "" {
		loc, err := url.Parse(raw)
		if err != nil || loc.IsAbs() || loc.Host != "" || !strings.HasPrefix(raw, "/") || strings.Count(raw, "/") != 1 || !strings.HasSuffix(raw, ".txt") || strings.EqualFold(raw, "/robots.txt") {
			return nil, errors.New("indexnow verification location must be a root .txt path")
		}
	}
	return indexnow.NewService(c.APIKey, indexnow.Config{SiteURL: c.SiteURL, Endpoint: c.Endpoint, KeyLocation: c.KeyLocation, Repository: c.Attempts})
}

// NeverCacheIndexNow bypasses caches for the enabled verification route.
func NeverCacheIndexNow(c IndexNowConfig, cache mux.MiddlewareFunc) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		cached := cache(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := c.KeyLocation
			if p == "" {
				p = "/" + c.APIKey + ".txt"
			}
			if c.Enabled && r.URL.Path == p {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
				return
			}
			cached.ServeHTTP(w, r)
		})
	}
}

// NeverCacheArticlePublication protects public and preview publication routes.
func NeverCacheArticlePublication(cache mux.MiddlewareFunc) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		cached := cache(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			if p == "/blog" || strings.HasPrefix(p, "/blog/") || p == "/api/v1/cms/articles" || strings.HasPrefix(p, "/api/v1/cms/articles/") || p == "/api/v1/cms/latest" {
				w.Header().Set("Cache-Control", "no-store")
				next.ServeHTTP(w, r)
				return
			}
			cached.ServeHTTP(w, r)
		})
	}
}

// ReconcileWindow validates an explicit bounded manual reconciliation window.
func ReconcileWindow(after, through time.Time, limit int, now time.Time) (*contentmanager.ReconcileIndexNowEligibilityRequest, error) {
	if after.IsZero() || !through.After(after) || through.After(now) || through.Sub(after) > 24*time.Hour || limit < 1 || limit > 10000 {
		return nil, contentmanager.ErrIndexNowReconciliationInvalid
	}
	return &contentmanager.ReconcileIndexNowEligibilityRequest{After: after, Through: through, Limit: limit}, nil
}
