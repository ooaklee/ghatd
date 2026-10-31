package publication

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/seo"
)

// SitemapFilter applies this application's canonical URL and build policy.
// SEO entries are managed independently of any content lifecycle. GHATD owns
// sitemap storage reads, SEO publication scheduling, XML rendering and HTTP
// delivery; the host owns crawler/build restrictions through this filter.
type SitemapFilter struct {
	origin       *url.URL
	allowedPaths map[string]bool
	articles     ArticleSource
	now          func() time.Time
	pathAllowed  func(string) bool
}

func newPublicSitemapFilter(frontendDomain string, files fs.FS, embeddedPrefix string) (*SitemapFilter, error) {
	origin, err := url.Parse(strings.TrimSpace(frontendDomain))
	if err != nil || (origin.Scheme != "https" && origin.Scheme != "http") || origin.Host == "" ||
		origin.User != nil || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || origin.RawPath != "" || (origin.Path != "" && origin.Path != "/") {
		return nil, errors.New("invalid sitemap frontend origin")
	}
	allowedPaths, err := readPublicSitemapPolicy(files, embeddedPrefix)
	if err != nil {
		return nil, fmt.Errorf("read sitemap build policy: %w", err)
	}
	return &SitemapFilter{origin: origin, allowedPaths: allowedPaths}, nil
}

// The frontend build is authoritative for mailing-list mode. Keep this
// immutable policy separate from live SEO rows and never infer it from runtime
// flags or a replica-local generated sitemap. Older builds without a policy
// asset remain compatible; a present-but-invalid policy fails startup.
func readPublicSitemapPolicy(files fs.FS, embeddedPrefix string) (map[string]bool, error) {
	if files == nil {
		return nil, nil
	}
	content, err := fs.ReadFile(files, path.Join(strings.Trim(embeddedPrefix, "/"), "sitemap-policy.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var policy struct {
		Version      int             `json:"version"`
		AllowedPaths json.RawMessage `json:"allowed_paths"`
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF || policy.Version != 1 || len(policy.AllowedPaths) == 0 {
		return nil, errors.New("invalid sitemap build policy")
	}
	if bytes.Equal(bytes.TrimSpace(policy.AllowedPaths), []byte("null")) {
		return nil, nil
	}
	var paths []string
	if err := json.Unmarshal(policy.AllowedPaths, &paths); err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(paths))
	for _, value := range paths {
		if !publicSitemapPath.MatchString(value) || path.Clean(value) != value || strings.ToLower(value) != value {
			return nil, errors.New("invalid sitemap build path")
		}
		allowed[value] = true
	}
	return allowed, nil
}

var publicSitemapPath = regexp.MustCompile(`^/(?:[A-Za-z0-9._~-]+/)*[A-Za-z0-9._~-]*$`)

// canonicalPublicSitemapPath accepts only clean paths on this site's origin.
// Encoded aliases, query/fragment URLs and capability URLs cannot bypass the
// route exclusions or accidentally disclose private links in the sitemap.
func canonicalPublicSitemapPath(raw string, origin *url.URL) (string, bool) {
	uri, err := url.Parse(raw)
	if err != nil || uri.User != nil || uri.RawQuery != "" || uri.ForceQuery || uri.Fragment != "" ||
		strings.Contains(raw, "#") || uri.Opaque != "" || uri.RawPath != "" {
		return "", false
	}
	if uri.IsAbs() {
		if !strings.EqualFold(uri.Scheme, origin.Scheme) || !strings.EqualFold(uri.Host, origin.Host) {
			return "", false
		}
	} else if uri.Host != "" {
		return "", false
	}
	pathname := uri.Path
	if pathname == "" && uri.IsAbs() {
		pathname = "/"
	}
	if !publicSitemapPath.MatchString(pathname) {
		return "", false
	}
	if pathname != "/" {
		pathname = strings.TrimSuffix(pathname, "/")
	}
	if path.Clean(pathname) != pathname {
		return "", false
	}
	lowerPath := strings.ToLower(pathname)
	for _, prefix := range []string{
		"/admin", "/api", "/app", "/auth", "/offline", "/portal", "/settings",
		"/t", "/ref", "/waitlist/unsubscribe", "/__pages",
	} {
		if lowerPath == prefix || strings.HasPrefix(lowerPath, prefix+"/") {
			return "", false
		}
	}
	return pathname, true
}

func (f *SitemapFilter) Filter(ctx context.Context, items []seo.SitemapItem) ([]seo.SitemapItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]seo.SitemapItem, 0, len(items))
	for _, item := range items {
		pathname, valid := canonicalPublicSitemapPath(item.URI, f.origin)
		if !valid || (f.pathAllowed != nil && !f.pathAllowed(pathname)) || (f.allowedPaths != nil && !f.allowedPaths[strings.ToLower(pathname)]) {
			continue
		}
		item.URI = pathname
		result = append(result, item)
	}
	return f.publishedBlogEntries(ctx, result)
}

// newPublicSEOService builds the live public sitemap service from a host
// request. The request is copied so embedded files and options survive, then
// live database delivery and (when non-nil) the host filter are forced.
// Production always passes a non-nil filter; tests may pass nil or inject a
// clock through the request to exercise publication scheduling.
func newPublicSEOService(request seo.NewServiceRequest, filter *SitemapFilter) *seo.Service {
	request.PublicSitemapMode = seo.PublicSitemapModeDatabase
	if filter != nil {
		request.PublicSitemapItemsFilter = filter.Filter
	}
	return seo.NewService(&request)
}

// This filters output only: it never creates, deletes or edits an SEO row, and
// the shared renderer still applies each retained row's independent schedule.
func (f *SitemapFilter) publishedBlogEntries(ctx context.Context, items []seo.SitemapItem) ([]seo.SitemapItem, error) {
	ids := map[string]bool{}
	for _, item := range items {
		if strings.HasPrefix(item.URI, "/blog/") {
			slug := strings.TrimPrefix(item.URI, "/blog/")
			if publicArticleSlug.MatchString(slug) {
				ids["article-"+slug] = true
			}
		}
	}
	if len(ids) > 0 && f.articles == nil {
		return nil, contentmanager.ErrContentManagerUnavailable
	}
	requested := make([]string, 0, len(ids))
	for id := range ids {
		requested = append(requested, id)
	}
	sort.Strings(requested)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now := time.Now()
	if f.now != nil {
		now = f.now()
	}
	published := map[string]bool{}
	for start := 0; start < len(requested); start += 100 {
		end := min(start+100, len(requested))
		result, err := f.articles.GetArticles(ctx, &contentmanager.GetArticlesRequest{GetArticlesRequest: &post.GetArticlesRequest{
			GetPostsRequest: &post.GetPostsRequest{PerPage: 100, UrlFriendlyIds: strings.Join(requested[start:end], ",")},
		}})
		if err != nil {
			return nil, err
		}
		if result == nil || result.GetArticlesResponse == nil || result.GetPostsResponse == nil {
			return nil, contentmanager.ErrContentManagerUnavailable
		}
		for i := range result.Posts {
			if contentmanager.IsPublicPostAt(&result.Posts[i], now) {
				published[publicArticlePath(&result.Posts[i])] = true
			}
		}
	}
	result := make([]seo.SitemapItem, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(strings.ToLower(item.URI), "/blog/") && !published[item.URI] {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}
