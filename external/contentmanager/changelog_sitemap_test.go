package contentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

type changelogSitemapPort struct {
	postService
	read func(context.Context, *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error)
}

func (p *changelogSitemapPort) GetChangelogItems(ctx context.Context, req *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error) {
	return p.read(ctx, req)
}

func TestChangelogSitemapEligibility(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, published, slug, deleted, created string
		visibility                              []string
		kind                                    post.PostType
		want, skipped                           int
	}{
		{name: "due platform", published: "2026-10-28T12:00:00.000000000", want: 1},
		{name: "due positive offset", published: "2026-10-28T13:00:00+01:00", want: 1},
		{name: "due negative offset", published: "2026-10-28T11:00:00-01:00", want: 1},
		{name: "draft"},
		{name: "future", published: "2026-10-28T12:00:00.000000001"},
		{name: "malformed", published: "invalid"},
		{name: "zero", published: "0001-01-01T00:00:00Z"},
		{name: "deleted", published: "2026-10-28T12:00:00Z", deleted: "deleted"},
		{name: "restricted", published: "2026-10-28T12:00:00Z", visibility: []string{"reader"}},
		{name: "wrong type", published: "2026-10-28T12:00:00Z", kind: post.PostTypeArticle},
		{name: "unsafe slug", published: "2026-10-28T12:00:00Z", slug: "../private", skipped: 1},
		{name: "wrong prefix", published: "2026-10-28T12:00:00Z", slug: "article-release", skipped: 1},
		{name: "before creation cutoff", published: "2026-10-28T12:00:00Z", created: "2026-10-28T14:00:00+03:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := post.Post{Id: "release", Type: post.PostTypeChangelog, UrlFriendlyId: "changelog-release", CreatedAt: "2026-10-28T13:00:00+01:00", PublishedAt: tc.published, UpdatedAt: "invalid", DeletedAt: tc.deleted, Visibility: tc.visibility}
			if tc.kind != "" {
				item.Type = tc.kind
			}
			if tc.slug != "" {
				item.UrlFriendlyId = tc.slug
			}
			if tc.created != "" {
				item.CreatedAt = tc.created
			}
			source := &changelogSitemapPort{read: func(_ context.Context, req *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error) {
				require.False(t, req.IsPublished, "do not discard offset timestamps through lexical comparisons")
				require.Empty(t, req.CreatedAtFrom)
				require.True(t, req.IsNotDeleted)
				return &post.GetChangelogItemsResponse{GetPostsResponse: &post.GetPostsResponse{Posts: []post.Post{item}, TotalPages: 1}}, nil
			}}
			result, err := getChangelogSitemapItems(t.Context(), source, &GetChangelogSitemapItemsRequest{CreatedAtFrom: "2026-10-28T12:00:00Z"}, now)
			require.NoError(t, err)
			require.Len(t, result.Items, tc.want)
			require.Equal(t, tc.skipped, result.SkippedMissingURLFriendlyID)
			require.Equal(t, tc.want+tc.skipped, result.ProcessedArticles)
			if tc.want != 0 {
				require.Equal(t, "/whats-new/changelog-release", result.Items[0].URI)
				require.Equal(t, "2026-10-28T12:00:00.000000000", result.Items[0].LastMod)
			}
		})
	}
}

func TestChangelogSitemapPagination(t *testing.T) {
	for _, tc := range []struct {
		name               string
		limit, want, pages int
	}{
		{"all pages", 0, 16, 4}, {"partial final page", 8, 8, 2}, {"single source", 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, pageSize := 0, 5
			if tc.limit > 0 && tc.limit < pageSize {
				pageSize = tc.limit
			}
			source := &changelogSitemapPort{read: func(_ context.Context, req *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error) {
				calls++
				require.Equal(t, pageSize, req.PerPage)
				require.Equal(t, calls, req.Page)
				items := []post.Post{}
				start := (req.Page - 1) * req.PerPage
				for i := start; i < start+req.PerPage && i < 16; i++ {
					items = append(items, post.Post{Id: fmt.Sprint(i), Type: post.PostTypeChangelog, UrlFriendlyId: fmt.Sprintf("changelog-release-%d", i), PublishedAt: "2026-10-27T12:00:00Z"})
				}
				return &post.GetChangelogItemsResponse{GetPostsResponse: &post.GetPostsResponse{Posts: items, TotalPages: (16 + pageSize - 1) / pageSize}}, nil
			}}
			result, err := getChangelogSitemapItems(t.Context(), source, &GetChangelogSitemapItemsRequest{PerPage: 5, Limit: tc.limit}, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			require.Len(t, result.Items, tc.want)
			require.Equal(t, tc.pages, calls)
			require.Equal(t, fmt.Sprint(tc.want-1), result.Items[tc.want-1].PostID)
		})
	}
}

func TestChangelogSitemapHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, query, body string
		status            int
	}{
		{"default", "", "", 200}, {"body", "", `{"uri_path_prefix":"/whats-new/","limit":1}`, 200},
		{"query overrides body", "?limit=2", `{"limit":-1}`, 200},
		{"custom prefix", "", `{"uri_path_prefix":"/updates/"}`, 200},
		{"external prefix", "", `{"uri_path_prefix":"https://example.org/"}`, 400},
		{"bad limit", "?limit=no", "", 400}, {"negative limit", "", `{"limit":-1}`, 400},
		{"large page", "", `{"per_page":101}`, 400}, {"bad date", "", `{"created_at_from":"invalid"}`, 400},
		{"bad JSON", "", `{`, 400}, {"trailing JSON", "", `{} {}`, 400},
		{"oversized", "", `{}` + strings.Repeat(" ", 8192), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			port := &changelogSitemapPort{read: func(context.Context, *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error) {
				calls++
				return &post.GetChangelogItemsResponse{GetPostsResponse: &post.GetPostsResponse{}}, nil
			}}
			handler := NewHandler(NewService(port, nil), noopContentManagerValidator{})
			w := httptest.NewRecorder()
			handler.GetChangelogSitemapItems(w, httptest.NewRequest(http.MethodPost, "/"+tc.query, strings.NewReader(tc.body)))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status == 200 {
				var response struct {
					Data GetChangelogSitemapItemsResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
				require.NotNil(t, response.Data.Items)
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls)
			}
		})
	}
}

func TestChangelogSitemapURIPrefix(t *testing.T) {
	for _, tc := range []struct{ name, prefix, want string }{
		{"default", "", "/whats-new/changelog-release"},
		{"custom", "/updates", "/updates/changelog-release"},
		{"nested", "/news/releases/", "/news/releases/changelog-release"},
		{"root", "/", "/changelog-release"},
		{"scheme", "https://example.org/", ""}, {"host", "//example.org/", ""},
		{"relative", "updates", ""}, {"query", "/updates?x=1", ""},
		{"fragment", "/updates#section", ""}, {"backslash", `/updates\archive`, ""},
		{"traversal", "/updates/../private/", ""}, {"escaped alias", "/%75pdates/", ""},
		{"double slash", "/news//releases/", ""}, {"double suffix", "/updates//", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &changelogSitemapPort{read: func(context.Context, *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error) {
				return &post.GetChangelogItemsResponse{GetPostsResponse: &post.GetPostsResponse{Posts: []post.Post{{Id: "release", Type: post.PostTypeChangelog, UrlFriendlyId: "changelog-release", PublishedAt: "2026-01-01T00:00:00Z"}}, TotalPages: 1}}, nil
			}}
			result, err := getChangelogSitemapItems(t.Context(), source, &GetChangelogSitemapItemsRequest{URIPathPrefix: tc.prefix}, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
			if tc.want == "" {
				require.ErrorIs(t, err, post.ErrPostBadRequest)
				return
			}
			require.NoError(t, err)
			require.Len(t, result.Items, 1)
			require.Equal(t, tc.want, result.Items[0].URI)
		})
	}
}

func TestChangelogSitemapSourceFailures(t *testing.T) {
	native := errors.New("source failure")
	for _, tc := range []struct {
		name     string
		response *post.GetChangelogItemsResponse
		err      error
		cancel   bool
		want     error
	}{
		{name: "nil response", want: ErrContentManagerUnavailable},
		{name: "nil page", response: &post.GetChangelogItemsResponse{}, want: ErrContentManagerUnavailable},
		{name: "native error", err: native, want: native},
		{name: "canceled during read", response: &post.GetChangelogItemsResponse{GetPostsResponse: &post.GetPostsResponse{}}, cancel: true, want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			source := &changelogSitemapPort{read: func(context.Context, *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error) {
				if tc.cancel {
					cancel()
				}
				return tc.response, tc.err
			}}
			result, err := getChangelogSitemapItems(ctx, source, &GetChangelogSitemapItemsRequest{}, time.Now())
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, result)
		})
	}
}

// Embedding only the original interface deliberately hides the new capability.
type legacyChangelogRouteHandler struct{ contentManagerHandler }
type legacyChangelogService struct{ contentManagerService }

func TestChangelogSitemapOptionalCapabilitiesAndAccess(t *testing.T) {
	for _, tc := range []struct {
		name, role                                   string
		legacyHandler, legacyService, missingAdapter bool
		status                                       int
	}{
		{name: "anonymous", status: 403}, {name: "member", role: "member", status: 403},
		{name: "administrator", role: "admin", status: 200},
		{name: "legacy handler", role: "admin", legacyHandler: true, status: 404},
		{name: "legacy service", role: "admin", legacyService: true, status: 503},
		{name: "missing adapter", role: "admin", missingAdapter: true, status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := &changelogSitemapPort{read: func(context.Context, *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error) {
				return &post.GetChangelogItemsResponse{GetPostsResponse: &post.GetPostsResponse{}}, nil
			}}
			var service contentManagerService = NewService(port, nil)
			if tc.legacyService {
				service = legacyChangelogService{service}
			}
			var handler contentManagerHandler = NewHandler(service, noopContentManagerValidator{})
			if tc.legacyHandler {
				handler = legacyChangelogRouteHandler{handler}
			}
			routes := router.NewRouter(nil, nil)
			adapter := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("X-Test-Role") != "admin" {
						w.WriteHeader(403)
						return
					}
					next.ServeHTTP(w, r)
				})
			}
			request := &AttachRoutesRequest{Router: routes, Handler: handler, MiddlewareAdminApiTokenOrJwtRequired: adapter, RateLimitOrActiveMiddleware: adapter}
			if tc.missingAdapter {
				request.MiddlewareAdminApiTokenOrJwtRequired = nil
			}
			AttachRoutes(request)
			if tc.missingAdapter {
				require.Error(t, routes.ValidateRoutePolicies())
			} else {
				require.NoError(t, routes.ValidateRoutePolicies())
			}
			found := false
			for _, route := range routes.RouteInventory() {
				if strings.Contains(route.Path, "/changelogs/sitemap-items") {
					found = true
					require.Equal(t, router.AdminSessionOrAPI, route.Access)
				}
			}
			require.Equal(t, !tc.legacyHandler, found)
			r := httptest.NewRequest(http.MethodGet, "/api/v1/cms/seo/posts/changelogs/sitemap-items", nil)
			r.Header.Set("X-Test-Role", tc.role)
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
		})
	}
}
