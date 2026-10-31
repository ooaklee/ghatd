package publication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/seo"
	"github.com/stretchr/testify/require"
)

func TestPolicyAcrossPublicationSurfaces(t *testing.T) {
	f, err := NewSitemapFilter(SitemapFilterConfig{Origin: "https://example.test", PathAllowed: func(p string) bool { return p != "/sign" }})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, path string
		allowed    bool
	}{{"public", "/pricing", true}, {"host denied", "/sign", false}, {"private", "/admin", false}, {"foreign", "https://evil.test/pricing", false}} {
		t.Run(tc.name, func(t *testing.T) {
			allowed, err := f.AllowIndexNowURL(context.Background(), tc.path)
			require.NoError(t, err)
			require.Equal(t, tc.allowed, allowed)
			items, err := f.Filter(context.Background(), []seo.SitemapItem{{URI: tc.path}})
			require.NoError(t, err)
			require.Equal(t, tc.allowed, len(items) == 1)
			eligible, err := f.Eligibility(nil)(context.Background(), tc.path, time.Now())
			require.NoError(t, err)
			require.Equal(t, tc.allowed, eligible)
		})
	}
}
func TestReconciliationScopePreservesIdentity(t *testing.T) {
	policy := []byte(`{"version":1,"allowed_paths":["/"]}`)
	files := fstest.MapFS{"web/dist/sitemap-policy.json": {Data: policy}}
	encoded, err := json.Marshal(struct {
		Version                        int
		Component, Environment, Origin string
		Policy                         []byte
	}{1, "component", "test", "https://example.test", policy})
	require.NoError(t, err)
	digest := sha256.Sum256(encoded)
	actual, err := ReconciliationScope("component", "test", "HTTPS://EXAMPLE.TEST", files, "web")
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(digest[:]), actual)
}
func TestCacheGuardsAndDisabledIndexNow(t *testing.T) {
	service, err := BuildIndexNow(IndexNowConfig{})
	require.NoError(t, err)
	require.Nil(t, service)
	middleware := mux.MiddlewareFunc(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Cache", "used"); next.ServeHTTP(w, r) })
	})
	for _, tc := range []struct {
		name, path string
		guard      mux.MiddlewareFunc
		bypass     bool
	}{{"key", "/secret.txt", NeverCacheIndexNow(IndexNowConfig{Enabled: true, APIKey: "secret"}, middleware), true}, {"disabled key", "/secret.txt", NeverCacheIndexNow(IndexNowConfig{}, middleware), false}, {"article API", "/api/v1/cms/articles/example", NeverCacheArticlePublication(middleware), true}, {"ordinary", "/pricing", NeverCacheArticlePublication(middleware), false}} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.guard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
			if tc.bypass {
				require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
				require.Empty(t, rec.Header().Get("X-Cache"))
			} else {
				require.Equal(t, "used", rec.Header().Get("X-Cache"))
			}
		})
	}
}
