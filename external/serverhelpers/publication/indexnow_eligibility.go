package publication

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/post"
)

type indexNowPostLookup func(context.Context, string) (*post.Post, error)

// Scheduled SEO rows cannot make unavailable dynamic content public. This check
// uses the completed pass boundary and the same CMS publication contract as the
// reader. It does not acquire administrator identity or return private fields.
func (f *SitemapFilter) indexNowEligibility(lookup indexNowPostLookup) func(context.Context, string, time.Time) (bool, error) {
	return func(ctx context.Context, raw string, at time.Time) (bool, error) {
		allowed, err := f.allowIndexNowURL(ctx, raw)
		if err != nil || !allowed {
			return false, err
		}
		pathname, valid := canonicalPublicSitemapPath(raw, f.origin)
		if !valid {
			return false, nil
		}
		var slug string
		switch {
		case strings.HasPrefix(pathname, "/blog/"):
			slug = "article-" + strings.TrimPrefix(pathname, "/blog/")
		case strings.HasPrefix(pathname, "/whats-new/"):
			slug = strings.TrimPrefix(pathname, "/whats-new/")
		default:
			return true, nil
		}
		if lookup == nil {
			return false, errors.New("indexnow publication source unavailable")
		}
		if !publicArticleSlug.MatchString(slug) {
			return false, nil
		}
		item, err := lookup(ctx, slug)
		if err == post.ErrResourceNotFound {
			return false, nil
		}
		if err != nil {
			return false, errors.New("indexnow publication lookup failed")
		}
		if item == nil || !contentmanager.IsPublicPostAt(item, at) || publicContentURL(*item) != pathname {
			return false, nil
		}
		return true, nil
	}
}
