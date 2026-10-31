package publication

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/indexnow"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/stretchr/testify/require"
)

type hostAttemptStore struct{ started, finished []*indexnow.Attempt }

func (s *hostAttemptStore) RecordAttempt(_ context.Context, a *indexnow.Attempt) error {
	copy := *a
	s.started = append(s.started, &copy)
	return nil
}
func (s *hostAttemptStore) FinishAttempt(_ context.Context, a *indexnow.Attempt) error {
	copy := *a
	s.finished = append(s.finished, &copy)
	return nil
}
func TestIndexNowHostConfiguration(t *testing.T) {
	type testCase struct {
		name, key, path, origin          string
		enabled, missingStore, wantError bool
	}
	cases := []testCase{{name: "disabled ignores unused key", key: "bad", origin: "bad"}, {name: "enabled default", key: "test-key-123", origin: "https://example.test", enabled: true}, {name: "custom root file", key: "test-key-123", path: "/verification.txt", origin: "https://example.test", enabled: true}, {name: "bad key", key: "bad", origin: "https://example.test", enabled: true, wantError: true}, {name: "missing origin", key: "test-key-123", enabled: true, wantError: true}, {name: "no audit storage", key: "test-key-123", origin: "https://example.test", enabled: true, missingStore: true, wantError: true}, {name: "private nested route", key: "test-key-123", path: "/api/key.txt", origin: "https://example.test", enabled: true, wantError: true}, {name: "foreign key location", key: "test-key-123", path: "https://other.test/key.txt", origin: "https://example.test", enabled: true, wantError: true}, {name: "robots route takeover", key: "test-key-123", path: "/robots.txt", origin: "https://example.test", enabled: true, wantError: true}, {name: "SPA route takeover", key: "test-key-123", path: "/blog", origin: "https://example.test", enabled: true, wantError: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := IndexNowConfig{SiteURL: tc.origin, Enabled: tc.enabled, APIKey: tc.key, KeyLocation: tc.path}
			var store indexnow.AttemptRepository = &hostAttemptStore{}
			if tc.missingStore {
				store = nil
			}
			s.Attempts = store
			service, err := BuildIndexNow(s)
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, service)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.enabled, service != nil)
			}
		})
	}
}
func TestIndexNowHostSubmissionAuditsProtocolOutcome(t *testing.T) {
	type testCase struct {
		name        string
		status      int
		wantSuccess bool
	}
	for _, tc := range []testCase{{"acknowledged", 200, true}, {"pending", 202, true}, {"throttled", 429, false}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "POST", r.Method)
				var payload struct {
					Host, Key string
					URLs      []string `json:"urlList"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				require.Equal(t, "example.test", payload.Host)
				require.Equal(t, "test-key-123", payload.Key)
				require.Equal(t, []string{"https://example.test/blog/example"}, payload.URLs)
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(mock.Close)
			store := &hostAttemptStore{}
			service, err := BuildIndexNow(IndexNowConfig{SiteURL: "https://example.test", Enabled: true, APIKey: "test-key-123", Endpoint: mock.URL, Attempts: store})
			require.NoError(t, err)
			response, err := service.SubmitURLs(t.Context(), &indexnow.SubmitURLsRequest{URLs: []string{"https://example.test/blog/example"}})
			require.Equal(t, tc.wantSuccess, err == nil)
			require.NotNil(t, response)
			require.Equal(t, tc.wantSuccess, response.Success)
			require.Equal(t, 1, calls)
			require.Len(t, store.started, 1)
			require.Len(t, store.finished, 1)
			require.Equal(t, tc.status, store.finished[0].StatusCode)
		})
	}
}
func TestIndexNowPublicRoutePolicy(t *testing.T) {
	type testCase struct {
		name, slug string
		kind       post.PostType
		want       string
	}
	for _, tc := range []testCase{{"article", "article-example", post.PostTypeArticle, "/blog/example"}, {"changelog", "changelog-example", post.PostTypeChangelog, "/whats-new/changelog-example"}, {"unsupported", "faq-example", post.PostTypeFaq, ""}, {"wrong type", "article-example", post.PostTypeFaq, ""}, {"malformed", "article-../secret", post.PostTypeArticle, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, PublicContentURL(post.Post{Type: tc.kind, UrlFriendlyId: tc.slug}))
		})
	}
	type policyCase struct {
		name, url        string
		restricted, want bool
	}
	for _, tc := range []policyCase{{"public", "https://example.com/blog/example", false, true}, {"private", "https://example.com/admin/seo", false, false}, {"foreign", "https://foreign.test/blog/example", false, false}, {"waitlist build", "https://example.com/blog/example", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := testPublicSitemapFilter(t)
			if tc.restricted {
				f.allowedPaths = map[string]bool{"/": true}
			}
			allowed, err := f.allowIndexNowURL(t.Context(), tc.url)
			require.NoError(t, err)
			require.Equal(t, tc.want, allowed)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := testPublicSitemapFilter(t).allowIndexNowURL(ctx, "https://example.com/blog/example")
	require.True(t, errors.Is(err, context.Canceled))
}
func TestIndexNowKeyBypassesHostCache(t *testing.T) {
	type testCase struct {
		name, path, custom string
		enabled            bool
		want               int
	}
	for _, tc := range []testCase{{"enabled default", "/test-key-123.txt", "", true, 2}, {"enabled custom", "/verify.txt", "/verify.txt", true, 2}, {"unrelated path", "/pricing", "", true, 1}, {"disabled", "/test-key-123.txt", "", false, 1}} {
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
			s := IndexNowConfig{Enabled: tc.enabled, APIKey: "test-key-123", KeyLocation: tc.custom}
			h := NeverCacheIndexNow(s, cache)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
			for i := 0; i < 2; i++ {
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", tc.path, nil))
			}
			require.Equal(t, tc.want, hits)
		})
	}
}

func TestIndexNowReconciliationWindowValidation(t *testing.T) {
	type testCase struct {
		name, after, through string
		limit                int
		valid                bool
	}
	for _, tc := range []testCase{{"bounded", "2026-10-30T10:00:00Z", "2026-10-30T11:00:00Z", 100, true}, {"empty checkpoint", "", "2026-10-30T11:00:00Z", 100, false}, {"missing zone", "2026-10-30T10:00:00", "2026-10-30T11:00:00Z", 100, false}, {"future", "2026-10-30T10:00:00Z", "2026-10-31T11:00:00Z", 100, false}, {"oversized window", "2026-10-28T10:00:00Z", "2026-10-30T11:00:00Z", 100, false}, {"reverse", "2026-10-30T11:00:00Z", "2026-10-30T10:00:00Z", 100, false}, {"oversized limit", "2026-10-30T10:00:00Z", "2026-10-30T11:00:00Z", 10001, false}} {
		t.Run(tc.name, func(t *testing.T) {
			after, aerr := time.Parse(time.RFC3339Nano, tc.after)
			through, terr := time.Parse(time.RFC3339Nano, tc.through)
			request, err := ReconcileWindow(after, through, tc.limit, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
			if aerr != nil || terr != nil {
				err = errors.New("invalid timestamp")
				request = nil
			}
			require.Equal(t, tc.valid, err == nil)
			require.Equal(t, tc.valid, request != nil)
		})
	}
}
