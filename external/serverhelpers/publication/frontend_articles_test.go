package publication

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/stretchr/testify/require"
	xhtml "golang.org/x/net/html"
)

type publicArticleSourceStub struct {
	article      *post.Post
	posts        []post.Post
	err          error
	listErr      error
	requests     []*contentmanager.GetArticleItemByUrlFriendlyIdRequest
	listRequests []*contentmanager.GetArticlesRequest
}

func (s *publicArticleSourceStub) GetArticleItemByUrlFriendlyId(_ context.Context, req *contentmanager.GetArticleItemByUrlFriendlyIdRequest) (*post.Post, error) {
	s.requests = append(s.requests, req)
	return s.article, s.err
}
func (s *publicArticleSourceStub) GetArticles(_ context.Context, req *contentmanager.GetArticlesRequest) (*contentmanager.GetArticlesResponse, error) {
	s.listRequests = append(s.listRequests, req)
	posts := s.posts
	if req.UrlFriendlyIds != "" {
		posts = nil
		ids := "," + req.UrlFriendlyIds + ","
		for _, p := range s.posts {
			if strings.Contains(ids, ","+p.UrlFriendlyId+",") {
				posts = append(posts, p)
			}
		}
	}
	return &contentmanager.GetArticlesResponse{GetArticlesResponse: &post.GetArticlesResponse{GetPostsResponse: &post.GetPostsResponse{Posts: posts, TotalPages: 1}}}, s.listErr
}
func articlePageFixture(t *testing.T) (*publicArticlePages, *publicArticleSourceStub) {
	t.Helper()
	shell := []byte(`<html><head><title>Example</title><meta name="description" content=""><meta property="og:url" content=""><meta property="og:title" content=""><meta property="og:description" content=""><meta property="og:type" content=""><meta name="twitter:title" content=""><meta name="twitter:description" content=""></head><body><div id="app"></div></body></html>`)
	source := &publicArticleSourceStub{article: &post.Post{Id: "article-id", UrlFriendlyId: "article-example", Title: "Useful article", PublishedAt: "2026-10-30T12:00:00Z", TextFormat: post.TextFormatMarkdown, Text: "Useful text.\n\n## A heading\n\nKeep a **shared record**. [Blog](/blog)."}}
	return &publicArticlePages{source: source, shell: shell, now: func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }, origin: "https://example.test", brand: "Example", bootstrapID: "example-public-article"}, source
}
func serveArticleFixture(p *publicArticlePages, method, target string) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	p.ServeHTTP(r, httptest.NewRequest(method, target, nil))
	return r
}

func TestPublicArticleInitialHTML(t *testing.T) {
	type testCase struct {
		name   string
		format post.TextFormat
		text   string
		want   string
	}
	cases := []testCase{{"markdown", post.TextFormatMarkdown, "Useful text.\n\n## A heading\n\n**Clear** [Blog](/blog).", "<h2>A heading</h2>"}, {"legacy html", post.TextFormatHtml, `<p>Useful text.</p><script>SECRET_SCRIPT()</script><img src=x onerror="SECRET_HANDLER()">`, "<p>Useful text.</p>"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, s := articlePageFixture(t)
			s.article.TextFormat, s.article.Text = tc.format, tc.text
			s.article.CreatedByUserId = "PRIVATE_ID"
			r := serveArticleFixture(p, http.MethodGet, "/blog/example?utm_source=test")
			require.Equal(t, 200, r.Code)
			require.Equal(t, "no-store", r.Header().Get("Cache-Control"))
			output := r.Body.String()
			require.Contains(t, output, tc.want)
			require.Contains(t, output, `href="https://example.test/blog/example"`)
			require.Contains(t, output, "<h1>Useful article</h1>")
			require.NotContains(t, output, "PRIVATE_ID")
			require.NotContains(t, output, "SECRET_")
			require.Len(t, s.requests, 1)
			require.Empty(t, s.requests[0].ActorID)
			doc, err := xhtml.Parse(strings.NewReader(output))
			require.NoError(t, err)
			seed := articleNode(doc, func(n *xhtml.Node) bool { return articleAttr(n, "id") == "example-public-article" })
			require.NotNil(t, seed)
			var initial map[string]any
			require.NoError(t, json.Unmarshal([]byte(seed.FirstChild.Data), &initial))
			require.Equal(t, "html", initial["text_format"])
			require.NotContains(t, initial, "created_by_user_id")
			require.Equal(t, "2026-10-30T12:00:00Z", initial["published_at"])
		})
	}
}
func TestPublicArticleUnavailableStates(t *testing.T) {
	type testCase struct {
		name, published, deleted string
		visibility               []string
		err                      error
		want                     int
	}
	cases := []testCase{{name: "draft", want: 404}, {name: "future", published: "2026-10-30T12:00:00.000000001Z", want: 404}, {name: "deleted", published: "2026-10-29T12:00:00Z", deleted: "deleted", want: 404}, {name: "restricted", published: "2026-10-29T12:00:00Z", visibility: []string{"private"}, want: 404},
		{name: "malformed date", published: "2026-02-30T12:00:00Z", want: 404}, {name: "missing", err: post.ErrResourceNotFound, want: 404}, {name: "denied", err: contentmanager.ErrUnauthorisedCMUser, want: 404}, {name: "outage", err: errors.New("SECRET diagnostic"), want: 503}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, s := articlePageFixture(t)
			s.article.Title, s.article.Text = "SECRET title", "SECRET text"
			s.article.PublishedAt, s.article.DeletedAt, s.err = tc.published, tc.deleted, tc.err
			s.article.Visibility = tc.visibility
			r := serveArticleFixture(p, http.MethodGet, "/blog/example")
			require.Equal(t, tc.want, r.Code)
			require.NotContains(t, r.Body.String(), "SECRET")
			require.NotContains(t, r.Body.String(), `rel="canonical"`)
			if tc.want == 404 {
				require.Equal(t, "noindex", r.Header().Get("X-Robots-Tag"))
			}
		})
	}
}
func TestPublicArticleCanonicalAndHEAD(t *testing.T) {
	type testCase struct {
		name, method, target, location string
		want                           int
	}
	cases := []testCase{{"prefixed alias", "GET", "/blog/article-example?ref=test", "/blog/example?ref=test", 301}, {"trailing slash", "GET", "/blog/example/?ref=test", "/blog/example?ref=test", 301}, {"index alias", "GET", "/blog/?ref=test", "/blog?ref=test", 301}, {"head", "HEAD", "/blog/example", "", 200}, {"bad slug", "GET", "/blog/not_a_slug", "", 404}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := articlePageFixture(t)
			r := serveArticleFixture(p, tc.method, tc.target)
			require.Equal(t, tc.want, r.Code)
			require.Equal(t, tc.location, r.Header().Get("Location"))
			if tc.method == "HEAD" {
				require.Empty(t, r.Body.String())
			}
		})
	}
}
func TestPublicArticlesIndexAndRelatedLinks(t *testing.T) {
	type testCase struct{ name, path string }
	for _, tc := range []testCase{{"index", "/blog"}, {"related links", "/blog/example"}} {
		t.Run(tc.name, func(t *testing.T) {
			p, s := articlePageFixture(t)
			s.posts = []post.Post{*s.article, {UrlFriendlyId: "article-future", Title: "SECRET FUTURE", PublishedAt: "2026-11-01T00:00:00Z"}, {UrlFriendlyId: "article-draft", Title: "SECRET DRAFT"}}
			s.article.TextFormat = post.TextFormatHtml
			s.article.Text = `<p>Useful text.</p><a href="/blog/example">Published</a><a href="/blog/future">Hidden link</a>`
			r := serveArticleFixture(p, "GET", tc.path)
			require.Equal(t, 200, r.Code)
			require.Contains(t, r.Body.String(), `href="/blog/example"`)
			require.NotContains(t, r.Body.String(), `href="/blog/future"`)
			require.NotContains(t, r.Body.String(), "SECRET")
			if tc.name == "related links" {
				require.Contains(t, r.Body.String(), "Hidden link")
				require.Contains(t, r.Body.String(), `"published_article_paths":["/blog/example"]`)
			}
		})
	}
}
func TestPublicArticleBuildPolicy(t *testing.T) {
	type testCase struct {
		name       string
		restricted bool
	}
	for _, tc := range []testCase{{"normal", false}, {"waitlist", true}} {
		t.Run(tc.name, func(t *testing.T) {
			p, s := articlePageFixture(t)
			files := fstest.MapFS{"web/dist/index.html": {Data: p.shell}}
			if !tc.restricted {
				files["web/dist/page-descriptions.json"] = &fstest.MapFile{Data: []byte(`[{"id":"blog","description":"Blog description."},{"id":"blog-specific","description":"Article description."}]`)}
				files["web/dist/__pages/blog.html"] = &fstest.MapFile{Data: p.shell}
				files["web/dist/__pages/blog-specific.html"] = &fstest.MapFile{Data: p.shell}
			}
			if tc.restricted {
				files["web/dist/sitemap-policy.json"] = &fstest.MapFile{Data: []byte(`{"version":1,"allowed_paths":["/"]}`)}
			}
			bootstrap, err := newFrontendBootstrap(files, "web/", nil)
			require.NoError(t, err)
			require.NoError(t, AttachPublicArticlePages(bootstrap.Router.GetRouter(), PagesConfig{Files: files, Prefix: "web/", Source: s, Origin: p.origin, Brand: p.brand, BootstrapID: "example-public-article"}))
			require.NoError(t, bootstrap.AttachRoutes())
			r := httptest.NewRecorder()
			bootstrap.Router.GetRouter().ServeHTTP(r, httptest.NewRequest("HEAD", "/blog/example", nil))
			require.Equal(t, 200, r.Code)
			require.Empty(t, r.Body.String())
			if tc.restricted {
				require.Empty(t, s.requests)
			} else {
				require.Len(t, s.requests, 1)
			}
		})
	}
}
func TestArticlePublicationBoundary(t *testing.T) {
	type testCase struct {
		name, date string
		want       bool
	}
	for _, tc := range []testCase{{"utc equality", "2026-10-30T12:00:00Z", true}, {"legacy equality", "2026-10-30T12:00:00.000000000", true}, {"future", "2026-10-30T12:00:00.000000001Z", false}, {"draft", "", false}, {"invalid calendar", "2026-02-30T12:00:00Z", false}} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, contentmanager.IsPublicPostAt(&post.Post{PublishedAt: tc.date}, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)))
		})
	}
}

func TestArticleDescriptionUsesPublicLead(t *testing.T) {
	type testCase struct{ name, body, want string }
	for _, tc := range []testCase{{"lead paragraph", "<p>Lead text.</p><h2>Heading</h2><p>Later paragraph.</p>", "Lead text."}, {"empty lead", "<p> </p><p>Useful text.</p>", "Useful text."}, {"plain fallback", "Useful text.", "Useful text."}, {"blocks retain word spacing", "<div>First</div><div>Second</div>", "First Second"}, {"line breaks retain word spacing", "<p>First<br>Second</p>", "First Second"}, {"whitespace normalized", "<p>Useful    text.\nMore.</p>", "Useful text. More."}} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, articleSummary(tc.body)) })
	}
}

func TestPublicArticleUsesCompiledEmptyContentDescription(t *testing.T) {
	type testCase struct{ name, path, description string }
	for _, tc := range []testCase{{"empty rich text", "/blog/example", "Article fallback from the compiled build."}, {"index", "/blog", "Blog fallback from the compiled build."}} {
		t.Run(tc.name, func(t *testing.T) {
			p, source := articlePageFixture(t)
			source.article.TextFormat = post.TextFormatHtml
			source.article.Text = "<p><br></p>"
			files := fstest.MapFS{"web/dist/page-descriptions.json": {Data: []byte(`[{"id":"blog","description":"Blog fallback from the compiled build."},{"id":"blog-specific","description":"Article fallback from the compiled build."}]`)}}
			var err error
			p.descriptions, err = readPublicArticleDescriptions(files, "web/")
			require.NoError(t, err)
			r := serveArticleFixture(p, "GET", tc.path)
			require.Equal(t, 200, r.Code)
			require.Contains(t, r.Body.String(), `name="description" content="`+tc.description+`"`)
		})
	}
}

func TestPublicArticleRouteAdmissionRequiresCurrentMetadataOnlyWhenEnabled(t *testing.T) {
	pages, source := articlePageFixture(t)
	valid := `[{"id":"blog","description":"Blog description."},{"id":"blog-specific","description":"Article description."}]`
	fixture := func(inventory string) fstest.MapFS {
		files := fstest.MapFS{"web/dist/index.html": {Data: pages.shell}}
		if inventory != "" {
			files["web/dist/page-descriptions.json"] = &fstest.MapFile{Data: []byte(inventory)}
		}
		return files
	}
	for _, tc := range []struct {
		name                            string
		files                           fs.FS
		noSource, wantError, wantRoutes bool
	}{
		{name: "backend only", noSource: true},
		{name: "mailing list excludes blog before requiring assets", files: fstest.MapFS{"web/dist/sitemap-policy.json": {Data: []byte(`{"version":1,"allowed_paths":["/"]}`)}}, noSource: true},
		{name: "current enabled build", files: fixture(valid), wantRoutes: true},
		{name: "enabled build needs CMS", files: fixture(valid), noSource: true, wantError: true},
		{name: "missing descriptions", files: fixture(""), wantError: true},
		{name: "malformed descriptions", files: fixture("broken"), wantError: true},
		{name: "missing article description", files: fixture(`[{"id":"blog","description":"Blog"}]`), wantError: true},
		{name: "missing index description", files: fixture(`[{"id":"blog-specific","description":"Article"}]`), wantError: true},
		{name: "empty description", files: fixture(`[{"id":"blog","description":" "},{"id":"blog-specific","description":"Article"}]`), wantError: true},
		{name: "duplicate description", files: fixture(`[{"id":"blog","description":"Blog"},{"id":"blog","description":"Other"},{"id":"blog-specific","description":"Article"}]`), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := mux.NewRouter()
			var cms ArticleSource = source
			if tc.noSource {
				cms = nil
			}
			err := AttachPublicArticlePages(router, PagesConfig{Files: tc.files, Prefix: "web/", Source: cms, Origin: pages.origin, Brand: pages.brand, BootstrapID: "example-public-article"})
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			routes := 0
			require.NoError(t, router.Walk(func(*mux.Route, *mux.Router, []*mux.Route) error { routes++; return nil }))
			if tc.wantRoutes {
				require.Equal(t, 2, routes)
			} else {
				require.Zero(t, routes)
			}
		})
	}
}
