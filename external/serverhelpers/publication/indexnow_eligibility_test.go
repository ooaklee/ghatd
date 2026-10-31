package publication

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ooaklee/ghatd/external/post"
	"github.com/stretchr/testify/require"
)

func TestIndexNowEligibilityUsesPublicDynamicContentAtPassBoundary(t *testing.T) {
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, uri, published, deleted, slug string
		kind                                post.PostType
		visibility                          []string
		lookupErr                           error
		want, wantErr                       bool
	}{
		{name: "due article", uri: "/blog/example", slug: "article-example", kind: post.PostTypeArticle, published: at.Format(time.RFC3339), want: true},
		{name: "changelog", uri: "/whats-new/changelog-example", slug: "changelog-example", kind: post.PostTypeChangelog, published: at.Format(time.RFC3339), want: true},
		{name: "future", uri: "/blog/example", slug: "article-example", kind: post.PostTypeArticle, published: at.Add(time.Second).Format(time.RFC3339)},
		{name: "draft", uri: "/blog/example", slug: "article-example", kind: post.PostTypeArticle},
		{name: "deleted", uri: "/blog/example", slug: "article-example", kind: post.PostTypeArticle, published: at.Format(time.RFC3339), deleted: "yesterday"},
		{name: "restricted", uri: "/blog/example", slug: "article-example", kind: post.PostTypeArticle, published: at.Format(time.RFC3339), visibility: []string{"administrator"}},
		{name: "missing", uri: "/blog/example", lookupErr: post.ErrResourceNotFound},
		{name: "source outage", uri: "/blog/example", lookupErr: errors.New("private-canary"), wantErr: true},
		{name: "wrong type", uri: "/blog/example", slug: "article-example", kind: post.PostTypeFaq, published: at.Format(time.RFC3339)},
		{name: "wrong slug", uri: "/blog/example", slug: "article-other", kind: post.PostTypeArticle, published: at.Format(time.RFC3339)},
		{name: "alias is not canonical", uri: "/blog/article-example", slug: "article-example", kind: post.PostTypeArticle, published: at.Format(time.RFC3339)},
		{name: "malformed", uri: "/blog/example", slug: "article-example", kind: post.PostTypeArticle, published: "not-a-date"},
		{name: "static public route", uri: "/pricing", want: true},
		{name: "private route", uri: "/admin/seo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter, err := newPublicSitemapFilter("https://example.test", nil, "")
			require.NoError(t, err)
			calls := 0
			check := filter.indexNowEligibility(func(_ context.Context, slug string) (*post.Post, error) {
				calls++
				if tc.uri == "/blog/example" {
					require.Equal(t, "article-example", slug)
				}
				return &post.Post{Id: "fixture", UrlFriendlyId: tc.slug, Type: tc.kind, PublishedAt: tc.published, DeletedAt: tc.deleted, Visibility: tc.visibility}, tc.lookupErr
			})
			got, err := check(t.Context(), "https://example.test"+tc.uri, at)
			require.Equal(t, tc.want, got)
			if tc.wantErr {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private-canary")
			} else {
				require.NoError(t, err)
			}
			if tc.uri == "/pricing" || tc.uri == "/admin/seo" {
				require.Zero(t, calls)
			}
		})
	}
}

func TestIndexNowEligibilityHonoursCompiledBuildRestrictions(t *testing.T) {
	files := fstest.MapFS{"sitemap-policy.json": {Data: []byte(`{"version":1,"allowed_paths":["/"]}`)}}
	filter, err := newPublicSitemapFilter("https://example.test", files, "")
	require.NoError(t, err)
	check := filter.indexNowEligibility(func(context.Context, string) (*post.Post, error) {
		t.Fatal("excluded build must not read a post")
		return nil, nil
	})
	allowed, err := check(t.Context(), "https://example.test/blog/example", time.Now())
	require.NoError(t, err)
	require.False(t, allowed)
}
