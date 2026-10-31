package publication

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/seo"
	"github.com/stretchr/testify/require"
)

type publicSitemapRepositoryStub struct {
	seo.SitemapRepository
	items []seo.SitemapItem
}

func (s *publicSitemapRepositoryStub) GetAllSitemapItems(context.Context) ([]seo.SitemapItem, error) {
	return append([]seo.SitemapItem(nil), s.items...), nil
}

const publicSitemapLastMod = "2026-10-27T10:00:00.000000000"

func publicSitemapEntry(uri string) seo.SitemapItem {
	return seo.SitemapItem{URI: uri, LastMod: publicSitemapLastMod, Priority: 0.8, ChangeFrequency: seo.ChangeFrequencyWeekly}
}

func testPublicSitemapFilter(t *testing.T) *SitemapFilter {
	t.Helper()
	filter, err := newPublicSitemapFilter("https://example.com", nil, "")
	require.NoError(t, err)
	return filter
}

func publicSitemapURIs(items []seo.SitemapItem) []string {
	uris := make([]string, 0, len(items))
	for _, item := range items {
		uris = append(uris, item.URI)
	}
	return uris
}

func TestPublicSitemapPreservesEditorialMetadataAndIndependentEntries(t *testing.T) {
	paths := []string{"/", "/pricing", "/blog", "/about", "/contact", "/policy/terms", "/policy/privacy",
		"/policy/security-and-compliance", "/feedback"}
	items := make([]seo.SitemapItem, 0, len(paths))
	for _, pathname := range paths {
		items = append(items, publicSitemapEntry(pathname))
	}
	items[0].PublishedAt = "2027-01-01T12:00:00Z"
	items[0].Priority = 0.3
	items[0].ChangeFrequency = seo.ChangeFrequencyMonthly
	actual, err := testPublicSitemapFilter(t).Filter(t.Context(), items)
	require.NoError(t, err)
	require.Equal(t, items, actual)
}

func TestPublicSitemapIndependentNonBlogEntriesPreserveEditorialSchedules(t *testing.T) {
	for _, uri := range []string{"/updates/manual-entry", "/whats-new/manual-update", "/whats-new/changelog-photos", "/WHATS-NEW/manual-update"} {
		t.Run(uri, func(t *testing.T) {
			item := publicSitemapEntry(uri)
			item.PublishedAt = "2027-01-01T12:00:00Z"
			actual, err := testPublicSitemapFilter(t).Filter(t.Context(), []seo.SitemapItem{item})
			require.NoError(t, err)
			require.Equal(t, []seo.SitemapItem{item}, actual)
		})
	}
}

func TestPublicSitemapRequiresArticleSourceForBlogCandidates(t *testing.T) {
	item := publicSitemapEntry("/blog/missing-post")
	item.PublishedAt = "2027-01-01T12:00:00Z"
	actual, err := testPublicSitemapFilter(t).Filter(t.Context(), []seo.SitemapItem{item})
	require.ErrorIs(t, err, contentmanager.ErrContentManagerUnavailable)
	require.Nil(t, actual, "a missing CMS dependency cannot silently publish a blog URL")
	require.Equal(t, "2027-01-01T12:00:00Z", item.PublishedAt)
}

func TestPublicSitemapRejectsMalformedBlogCandidatesWithoutReadingCMS(t *testing.T) {
	for _, uri := range []string{"/blog/story/extra", "/BLOG/story"} {
		actual, err := testPublicSitemapFilter(t).Filter(t.Context(), []seo.SitemapItem{publicSitemapEntry(uri)})
		require.NoError(t, err)
		require.Empty(t, actual)
	}
}

func TestPublicSitemapRejectsUnsafeAndPrivateAliases(t *testing.T) {
	unsafe := []string{
		"/admin", "/admin/seo", "/API/v1/whatever", "/app/a/SECRET", "/AUTH/login", "/portal/id", "/settings", "/offline",
		"/t/SECRET", "/ref/SECRET", "/waitlist/unsubscribe", "/__pages/home.html",
		"/pricing?account=SECRET", "/pricing?", "/faq#SECRET", "/faq#",
		"//other.example/secret", "https://other.example/blog/secret", "http://example.com/pricing", "https://user@example.com/pricing",
		"/blog/%73tory", "/%61dmin/seo", "/blog/../pricing", "/blog//story",
		"/blog/story%2fextra",
		"/pricing\\SECRET", "/pricing SECRET", "/pricing%0asecret", "javascript:alert(1)", "pricing", "", "/./pricing",
	}
	for _, uri := range unsafe {
		t.Run(uri, func(t *testing.T) {
			actual, err := testPublicSitemapFilter(t).Filter(t.Context(), []seo.SitemapItem{publicSitemapEntry(uri)})
			require.NoError(t, err)
			require.Empty(t, actual)
		})
	}
}

func TestPublicSitemapCanonicalizesSameOriginEntriesWithoutChangingStoredRows(t *testing.T) {
	filter := testPublicSitemapFilter(t)
	stored := []seo.SitemapItem{publicSitemapEntry("https://Example.com/pricing"), publicSitemapEntry("/about/"), publicSitemapEntry("https://example.com")}
	actual, err := filter.Filter(t.Context(), append([]seo.SitemapItem(nil), stored...))
	require.NoError(t, err)
	require.Equal(t, []string{"/pricing", "/about", "/"}, publicSitemapURIs(actual))
	require.Equal(t, "https://Example.com/pricing", stored[0].URI, "stored rows must not be mutated")
}

func TestPublicSitemapFilterPropagatesCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	actual, err := testPublicSitemapFilter(t).Filter(ctx, []seo.SitemapItem{publicSitemapEntry("/pricing")})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, actual)
}

func publicSitemapPolicyFiles(policy string) fstest.MapFS {
	return fstest.MapFS{"dist/sitemap-policy.json": &fstest.MapFile{Data: []byte(policy)}}
}

func TestPublicSitemapAppliesImmutableBuildPolicy(t *testing.T) {
	filter, err := newPublicSitemapFilter("https://example.com", publicSitemapPolicyFiles(
		`{"version":1,"allowed_paths":["/","/waitlist","/waitlist/unsubscribe"]}`), "dist")
	require.NoError(t, err)
	items := []seo.SitemapItem{publicSitemapEntry("/"), publicSitemapEntry("/waitlist"), publicSitemapEntry("/pricing")}
	actual, err := filter.Filter(t.Context(), items)
	require.NoError(t, err)
	require.Equal(t, []string{"/", "/waitlist"}, publicSitemapURIs(actual), "paths outside the build allowlist must be excluded")

	actual, err = filter.Filter(t.Context(), []seo.SitemapItem{publicSitemapEntry("/"), publicSitemapEntry("/WAITLIST"), publicSitemapEntry("/waitlist/unsubscribe")})
	require.NoError(t, err)
	require.Equal(t, []string{"/", "/WAITLIST"}, publicSitemapURIs(actual), "public path casing is preserved, but the allowlist cannot override private-prefix exclusions")
}

func TestPublicSitemapBuildPolicyNormalLegacyAndEmptyAllowlist(t *testing.T) {
	for name, policy := range map[string]string{
		"normal build": `{"version":1,"allowed_paths":null}`,
		"legacy build": "",
		"empty list":   `{"version":1,"allowed_paths":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var files fs.FS
			if policy != "" {
				files = publicSitemapPolicyFiles(policy)
			}
			filter, err := newPublicSitemapFilter("https://example.com", files, "dist")
			require.NoError(t, err)
			if name == "empty list" {
				require.NotNil(t, filter.allowedPaths)
				require.Empty(t, filter.allowedPaths, "empty allowlist restricts everything")
			} else {
				require.Nil(t, filter.allowedPaths, "no mailing-list restriction should apply")
			}
			actual, err := filter.Filter(t.Context(), []seo.SitemapItem{publicSitemapEntry("/pricing")})
			require.NoError(t, err)
			if name == "empty list" {
				require.Empty(t, actual)
			} else {
				require.Equal(t, []string{"/pricing"}, publicSitemapURIs(actual))
			}
		})
	}
}

func TestPublicSitemapRejectsInvalidBuildPolicyAtConstruction(t *testing.T) {
	for name, policy := range map[string]string{
		"wrong version":     `{"version":2,"allowed_paths":null}`,
		"unknown field":     `{"version":1,"allowed_paths":null,"extra":true}`,
		"invalid path":      `{"version":1,"allowed_paths":["/okay","/BAD PATH"]}`,
		"uppercase path":    `{"version":1,"allowed_paths":["/Bad"]}`,
		"unclean path":      `{"version":1,"allowed_paths":["/a/../b"]}`,
		"missing allowed":   `{"version":1}`,
		"trailing content":  `{"version":1,"allowed_paths":null} {}`,
		"invalid allowlist": `{"version":1,"allowed_paths":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newPublicSitemapFilter("https://example.com", publicSitemapPolicyFiles(policy), "dist")
			require.Error(t, err)
		})
	}
}

func TestPublicSitemapRejectsInvalidConfigurationAtConstruction(t *testing.T) {
	for name, domain := range map[string]string{
		"missing scheme": "example.com",
		"unsupported":    "ftp://example.com",
		"userinfo":       "https://user@example.com",
		"query":          "https://example.com?x=1",
		"fragment":       "https://example.com#f",
		"path":           "https://example.com/app",
		"missing host":   "https://",
		"parse failure":  "https://exa mple.com",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newPublicSitemapFilter(domain, nil, "")
			require.Error(t, err)
		})
	}
	_, err := newPublicSitemapFilter("https://example.com/", nil, "")
	require.NoError(t, err, "a bare trailing slash origin is tolerated")
}

func TestPublicSitemapSharedHandlerUsesIndependentSEOPublicationAndHostPolicy(t *testing.T) {
	// SEO-only changes control the XML between requests; the host policy
	// composes with shared scheduling and the constructor seam preserves
	// embedded files/options while forcing live database delivery.
	filter := testPublicSitemapFilter(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	independent := publicSitemapEntry("/updates/manual-entry")
	independent.PublishedAt = now.Add(time.Hour).Format(time.RFC3339Nano)
	repository := &publicSitemapRepositoryStub{items: []seo.SitemapItem{
		publicSitemapEntry("/pricing"), independent, publicSitemapEntry("/admin/seo"),
	}}
	service := newPublicSEOService(seo.NewServiceRequest{
		SitemapRepository: repository, FrontendDomain: "https://example.com",
		FileSystem: fstest.MapFS{"sitemap.xml": &fstest.MapFile{Data: []byte("STALE-EMBEDDED-SITEMAP")}},
		Now:        func() time.Time { return now },
	}, filter)
	for _, step := range []struct {
		name string
		edit func()
		want []string
	}{
		{name: "scheduled SEO row hidden", want: []string{"https://example.com/pricing"}},
		{name: "SEO schedule reached", edit: func() { now = now.Add(time.Hour) }, want: []string{"https://example.com/pricing", "https://example.com/updates/manual-entry"}},
		{name: "SEO row rescheduled", edit: func() { repository.items[1].PublishedAt = now.Add(time.Hour).Format(time.RFC3339Nano) }, want: []string{"https://example.com/pricing"}},
		{name: "SEO schedule explicitly cleared", edit: func() { repository.items[1].PublishedAt = "" }, want: []string{"https://example.com/pricing", "https://example.com/updates/manual-entry"}},
		{name: "SEO row removed", edit: func() { repository.items = []seo.SitemapItem{repository.items[0]} }, want: []string{"https://example.com/pricing"}},
	} {
		t.Run(step.name, func(t *testing.T) {
			if step.edit != nil {
				step.edit()
			}
			recorder := httptest.NewRecorder()
			seo.NewHandler(service, nil).GetSitemap(recorder, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.NotContains(t, recorder.Body.String(), "STALE-EMBEDDED-SITEMAP")
			require.NotContains(t, recorder.Body.String(), "/admin")
			var urlset struct {
				URLs []struct {
					Loc string `xml:"loc"`
				} `xml:"url"`
			}
			require.NoError(t, xml.Unmarshal(recorder.Body.Bytes(), &urlset))
			locs := make([]string, 0, len(urlset.URLs))
			for _, u := range urlset.URLs {
				locs = append(locs, strings.TrimRight(u.Loc, "\n"))
			}
			require.Equal(t, step.want, locs)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		})
	}
}

func TestPublicSitemapChecksCMSAvailabilityWithoutChangingEditorialRows(t *testing.T) {
	type testCase struct {
		name    string
		count   int
		failure bool
	}
	for _, tc := range []testCase{{"single batch", 3, false}, {"multiple batches", 201, false}, {"source outage", 3, true}} {
		t.Run(tc.name, func(t *testing.T) {
			p, source := articlePageFixture(t)
			filter := testPublicSitemapFilter(t)
			filter.articles = source
			filter.now = p.now
			items := []seo.SitemapItem{publicSitemapEntry("/pricing"), publicSitemapEntry("/blog/missing"), publicSitemapEntry("/blog/draft"), publicSitemapEntry("/blog/future"), publicSitemapEntry("/BLOG/case"), publicSitemapEntry("/blog/invalid/extra")}
			source.posts = []post.Post{{UrlFriendlyId: "article-draft"}, {UrlFriendlyId: "article-future", PublishedAt: "2027-01-01T00:00:00Z"}}
			for i := 0; i < tc.count; i++ {
				slug := fmt.Sprintf("example-%d", i)
				source.posts = append(source.posts, post.Post{UrlFriendlyId: "article-" + slug, PublishedAt: "2026-10-30T11:00:00Z"})
				item := publicSitemapEntry("/blog/" + slug)
				item.PublishedAt = "2027-01-01T00:00:00Z"
				item.Priority = 0.3
				item.ChangeFrequency = seo.ChangeFrequencyMonthly
				items = append(items, item)
			}
			before := append([]seo.SitemapItem(nil), items...)
			if tc.failure {
				source.listErr = errors.New("private source failure")
			}
			actual, err := filter.Filter(t.Context(), items)
			require.Equal(t, before, items, "storage rows and independent schedules must remain unchanged")
			if tc.failure {
				require.Error(t, err)
				require.Nil(t, actual)
				return
			}
			require.NoError(t, err)
			require.Len(t, actual, tc.count+1)
			require.Equal(t, items[0], actual[0])
			require.Equal(t, items[6:], actual[1:])
			require.Len(t, source.listRequests, (tc.count+3+99)/100)
			for _, request := range source.listRequests {
				require.Empty(t, request.ActorID)
				require.LessOrEqual(t, len(strings.Split(request.UrlFriendlyIds, ",")), 100)
			}
		})
	}
}
func TestArticlePublicationBypassesResponseCache(t *testing.T) {
	type testCase struct {
		name, path string
		want       int
	}
	for _, tc := range []testCase{{"article", "/blog/example", 2}, {"index", "/blog", 2}, {"public API", "/api/v1/cms/articles/example", 2}, {"list API", "/api/v1/cms/articles", 2}, {"latest API", "/api/v1/cms/latest", 2}, {"ordinary page", "/pricing", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			cached := false
			cache := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if cached {
						return
					}
					cached = true
					next.ServeHTTP(w, r)
				})
			}
			h := NeverCacheArticlePublication(cache)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
			for i := 0; i < 2; i++ {
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", tc.path, nil))
			}
			require.Equal(t, tc.want, hits)
		})
	}
}
