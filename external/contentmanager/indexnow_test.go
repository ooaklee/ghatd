package contentmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/indexnow"
	"github.com/ooaklee/ghatd/external/post"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

type cmsIndexNowPostPort struct {
	postService
	previous, current *post.Post
	failure           error
	snapshotFailure   error
	committed         bool
	events            []string
	pages             map[int]*post.GetPostsResponse
	readFailure       error
}

func (p *cmsIndexNowPostPort) lookup(_ context.Context, id string) (*post.Post, error) {
	p.events = append(p.events, "snapshot")
	return p.previous, p.snapshotFailure
}
func (p *cmsIndexNowPostPort) write() error {
	p.events = append(p.events, "write")
	if p.failure == nil {
		p.committed = true
	}
	return p.failure
}
func (p *cmsIndexNowPostPort) CreatePost(context.Context, *post.CreatePostRequest) (*post.CreatePostResponse, error) {
	err := p.write()
	return &post.CreatePostResponse{Post: p.current}, err
}
func (p *cmsIndexNowPostPort) UpdatePost(context.Context, *post.UpdatePostRequest) (*post.UpdatePostResponse, error) {
	err := p.write()
	return &post.UpdatePostResponse{Post: p.current}, err
}
func (p *cmsIndexNowPostPort) DeletePostById(context.Context, *post.DeletePostByIdRequest) (*post.DeletePostByIdResponse, error) {
	err := p.write()
	return &post.DeletePostByIdResponse{}, err
}
func (p *cmsIndexNowPostPort) RestorePostById(context.Context, *post.RestorePostByIdRequest) (*post.RestorePostByIdResponse, error) {
	err := p.write()
	return &post.RestorePostByIdResponse{Post: p.current}, err
}
func (p *cmsIndexNowPostPort) GetPosts(_ context.Context, r *post.GetPostsRequest) (*post.GetPostsResponse, error) {
	p.events = append(p.events, fmt.Sprintf("page-%d", r.Page))
	if p.readFailure != nil {
		return nil, p.readFailure
	}
	if value, ok := p.pages[r.Page]; ok {
		return value, nil
	}
	return &post.GetPostsResponse{}, nil
}

type cmsIndexNowSubmitter struct {
	t       *testing.T
	source  *cmsIndexNowPostPort
	calls   [][]string
	failure error
	status  int
}

func (s *cmsIndexNowSubmitter) SubmitURLs(_ context.Context, r *indexnow.SubmitURLsRequest) (*indexnow.SubmitURLsResponse, error) {
	if s.source != nil {
		require.True(s.t, s.source.committed, "network call precedes acknowledged mutation")
		s.source.events = append(s.source.events, "notify")
	}
	s.calls = append(s.calls, append([]string(nil), r.URLs...))
	return &indexnow.SubmitURLsResponse{AttemptID: "attempt-1", StatusCode: s.status, Success: s.status == 200 || s.status == 202}, s.failure
}
func cmsIndexNowTime() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }
func cmsIndexNowPost(slug string) *post.Post {
	return &post.Post{Id: contentTargetID, Type: post.PostTypeArticle, UrlFriendlyId: slug, PublishedAt: "2026-10-30T11:00:00Z"}
}
func cmsIndexNowConfig(p *cmsIndexNowPostPort) IndexNowConfig {
	return IndexNowConfig{CanonicalOrigin: "https://example.com", LookupPost: p.lookup, Now: cmsIndexNowTime, URLForPost: func(item post.Post) string {
		if item.Type != post.PostTypeArticle {
			return ""
		}
		return "/blog/" + strings.TrimPrefix(item.UrlFriendlyId, "article-")
	}}
}
func cmsIndexNowMutate(ctx context.Context, s *Service, operation, actor string) (*IndexNowNotificationReceipt, error) {
	switch operation {
	case "create":
		r, e := s.CreatePost(ctx, &CreatePostRequest{CreatePostRequest: &post.CreatePostRequest{ActorID: actor}})
		if r != nil {
			return r.IndexNow, e
		}
		return nil, e
	case "update":
		r, e := s.UpdatePostById(ctx, &UpdatePostByIdRequest{UpdatePostRequest: &post.UpdatePostRequest{ActorID: actor, PostId: contentTargetID}})
		if r != nil {
			return r.IndexNow, e
		}
		return nil, e
	case "delete":
		r, e := s.DeletePostById(ctx, &DeletePostByIdRequest{DeletePostByIdRequest: &post.DeletePostByIdRequest{ActorID: actor, Id: contentTargetID}})
		if r != nil {
			return r.IndexNow, e
		}
		return nil, e
	default:
		r, e := s.RestorePostById(ctx, &RestorePostByIdRequest{RestorePostByIdRequest: &post.RestorePostByIdRequest{ActorID: actor, Id: contentTargetID}})
		if r != nil {
			return r.IndexNow, e
		}
		return nil, e
	}
}
func TestIndexNowMutationTransitions(t *testing.T) {
	for _, tc := range []struct {
		name, operation, publication, oldPublication, slug, warning                       string
		deleted, restricted, unauthed, writeFailure, notificationFailure, snapshotFailure bool
		status                                                                            int
		urls                                                                              []string
	}{
		{name: "published create", operation: "create", urls: []string{"https://example.com/blog/new"}},
		{name: "future create", operation: "create", publication: "2026-10-30T12:00:01Z"},
		{name: "draft create", operation: "create", publication: "draft"},
		{name: "restricted create", operation: "create", restricted: true},
		{name: "malformed date", operation: "create", publication: "invalid"},
		{name: "missing slug", operation: "create", slug: "missing"},
		{name: "wrong type", operation: "create", slug: "wrong-type"},
		{name: "published update", operation: "update", urls: []string{"https://example.com/blog/old", "https://example.com/blog/new"}},
		{name: "unchanged canonical deduplicated", operation: "update", slug: "article-old", urls: []string{"https://example.com/blog/old"}},
		{name: "unpublish retains old url", operation: "update", publication: "draft", urls: []string{"https://example.com/blog/old"}},
		{name: "reschedule retains old url", operation: "update", publication: "2026-10-31T12:00:00Z", urls: []string{"https://example.com/blog/old"}},
		{name: "old future not announced", operation: "update", oldPublication: "2026-10-31T12:00:00Z", urls: []string{"https://example.com/blog/new"}},
		{name: "delete removal", operation: "delete", urls: []string{"https://example.com/blog/old"}},
		{name: "delete draft", operation: "delete", oldPublication: "draft"},
		{name: "restore to draft", operation: "restore", publication: "draft"},
		{name: "restore already public adapter", operation: "restore", urls: []string{"https://example.com/blog/new"}},
		{name: "committed write notification failed", operation: "update", notificationFailure: true, warning: "notification_failed", urls: []string{"https://example.com/blog/old", "https://example.com/blog/new"}},
		{name: "pending ownership", operation: "create", status: 202, urls: []string{"https://example.com/blog/new"}},
		{name: "snapshot failed current still notified", operation: "update", snapshotFailure: true, warning: "snapshot_unavailable", urls: []string{"https://example.com/blog/new"}},
		{name: "unauthorized cannot lookup or notify", operation: "update", unauthed: true},
		{name: "failed update cannot notify", operation: "update", writeFailure: true},
		{name: "failed create cannot notify", operation: "create", writeFailure: true},
		{name: "failed deletion cannot notify", operation: "delete", writeFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &cmsIndexNowPostPort{previous: cmsIndexNowPost("article-old"), current: cmsIndexNowPost("article-new")}
			if tc.publication != "" {
				p.current.PublishedAt = tc.publication
				if tc.publication == "draft" {
					p.current.PublishedAt = ""
				}
			}
			if tc.oldPublication != "" {
				p.previous.PublishedAt = tc.oldPublication
				if tc.oldPublication == "draft" {
					p.previous.PublishedAt = ""
				}
			}
			if tc.restricted {
				p.current.Visibility = []string{"member"}
			}
			if tc.slug != "" {
				p.current.UrlFriendlyId = tc.slug
			}
			if tc.slug == "missing" {
				p.current.UrlFriendlyId = ""
			}
			if tc.slug == "wrong-type" {
				p.current.Type = post.PostTypeFaq
			}
			if tc.writeFailure {
				p.failure = errors.New("private write failure")
			}
			if tc.snapshotFailure {
				p.snapshotFailure = errors.New("private snapshot failure")
			}
			status := tc.status
			if status == 0 {
				status = 200
			}
			submitter := &cmsIndexNowSubmitter{t: t, source: p, status: status}
			if tc.notificationFailure {
				submitter.failure = errors.New("key=private-credential")
			}
			users := &countingContentManagerUserService{user: &user.UniversalUser{ID: contentActorID, Roles: []string{user.UserRoleAdmin}}}
			service := NewService(p, users, WithIndexNow(submitter, cmsIndexNowConfig(p)))
			ctx := contentManagerRequestContext(contentActorID, !tc.unauthed)
			receipt, err := cmsIndexNowMutate(ctx, service, tc.operation, contentActorID)
			if tc.unauthed {
				require.ErrorIs(t, err, ErrUnauthorisedCMUser)
				require.Empty(t, p.events)
				require.Nil(t, receipt)
				return
			}
			if tc.writeFailure {
				require.ErrorIs(t, err, p.failure)
				require.Empty(t, submitter.calls)
				require.Nil(t, receipt)
				return
			}
			require.NoError(t, err, "a notification failure cannot turn the committed mutation into a write failure")
			require.NotNil(t, receipt)
			require.Equal(t, len(tc.urls), receipt.URLCount)
			require.Equal(t, tc.warning, receipt.Warning)
			if len(tc.urls) == 0 {
				require.Empty(t, submitter.calls)
				require.Equal(t, "skipped", receipt.Status)
			} else {
				require.Equal(t, [][]string{tc.urls}, submitter.calls)
				want := "acknowledged"
				if tc.notificationFailure {
					want = "failed"
				} else if status == 202 {
					want = "pending"
				}
				require.Equal(t, want, receipt.Status)
				require.Equal(t, "notify", p.events[len(p.events)-1])
			}
			require.NotContains(t, fmt.Sprint(receipt), "private")
		})
	}
}

func TestIndexNowDisabledPreservesMutationIO(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete", "restore"} {
		t.Run(operation, func(t *testing.T) {
			p := &cmsIndexNowPostPort{current: cmsIndexNowPost("article-new")}
			users := &countingContentManagerUserService{user: &user.UniversalUser{ID: contentActorID, Roles: []string{user.UserRoleAdmin}}}
			receipt, err := cmsIndexNowMutate(t.Context(), NewService(p, users), operation, contentActorID)
			require.NoError(t, err)
			require.Nil(t, receipt)
			require.Equal(t, []string{"write"}, p.events)
		})
	}
}

func TestIndexNowHostPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, path, origin                 string
		deny, policyFailure, invalidConfig bool
		wantStatus, warning                string
	}{
		{name: "canonical custom mapping", path: "/news/story", wantStatus: "acknowledged"},
		{name: "build policy excludes", path: "/blog/new", deny: true, wantStatus: "skipped"},
		{name: "policy failure safe warning", path: "/blog/new", policyFailure: true, wantStatus: "skipped", warning: "policy_unavailable"},
		{name: "absolute foreign host", path: "https://foreign.example/post", wantStatus: "skipped", warning: "invalid_public_url"},
		{name: "protocol relative", path: "//foreign.example/post", wantStatus: "skipped", warning: "invalid_public_url"},
		{name: "private route", path: "/admin/posts", wantStatus: "skipped", warning: "invalid_public_url"},
		{name: "traversal", path: "/blog/../admin", wantStatus: "skipped", warning: "invalid_public_url"},
		{name: "encoded traversal", path: "/blog/%2e%2e/admin", wantStatus: "skipped", warning: "invalid_public_url"},
		{name: "fragment", path: "/blog/new#secret", wantStatus: "skipped", warning: "invalid_public_url"},
		{name: "query", path: "/blog/new?key=private", wantStatus: "skipped", warning: "invalid_public_url"},
		{name: "invalid origin", path: "/blog/new", origin: "https://example.com/path", wantStatus: "skipped", warning: "configuration_invalid"},
		{name: "missing snapshot port", invalidConfig: true, wantStatus: "skipped", warning: "configuration_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &cmsIndexNowPostPort{current: cmsIndexNowPost("article-new")}
			submitter := &cmsIndexNowSubmitter{t: t, source: p, status: 200}
			config := cmsIndexNowConfig(p)
			config.URLForPost = func(post.Post) string { return tc.path }
			if tc.origin != "" {
				config.CanonicalOrigin = tc.origin
			}
			if tc.invalidConfig {
				config.LookupPost = nil
			}
			config.AllowURL = func(_ context.Context, value string) (bool, error) {
				require.True(t, strings.HasPrefix(value, "https://example.com/"))
				if tc.policyFailure {
					return false, errors.New("private policy failure")
				}
				return !tc.deny, nil
			}
			users := &countingContentManagerUserService{user: &user.UniversalUser{ID: contentActorID, Roles: []string{user.UserRoleAdmin}}}
			receipt, err := cmsIndexNowMutate(t.Context(), NewService(p, users, WithIndexNow(submitter, config)), "create", contentActorID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, receipt.Status)
			require.Equal(t, tc.warning, receipt.Warning)
			if tc.wantStatus == "acknowledged" {
				require.Len(t, submitter.calls, 1)
			} else {
				require.Empty(t, submitter.calls)
			}
		})
	}
}

func TestIndexNowReconciliationWindow(t *testing.T) {
	now := cmsIndexNowTime()
	for _, tc := range []struct {
		name                         string
		after, through               time.Time
		limit                        int
		sourceFailure, notifyFailure bool
		want                         error
		urls                         int
	}{
		{name: "explicit activation window", after: now.Add(-time.Hour), through: now, urls: 1},
		{name: "missing checkpoint", through: now, want: ErrIndexNowReconciliationInvalid},
		{name: "empty window", after: now, through: now, want: ErrIndexNowReconciliationInvalid},
		{name: "future bound", after: now.Add(-time.Hour), through: now.Add(time.Second), want: ErrIndexNowReconciliationInvalid},
		{name: "oversized window", after: now.Add(-25 * time.Hour), through: now, want: ErrIndexNowReconciliationInvalid},
		{name: "negative bound", after: now.Add(-time.Hour), through: now, limit: -1, want: ErrIndexNowReconciliationInvalid},
		{name: "scan bound exhausted", after: now.Add(-time.Hour), through: now, limit: 1, want: ErrIndexNowReconciliationIncomplete},
		{name: "source failure", after: now.Add(-time.Hour), through: now, sourceFailure: true, want: ErrIndexNowReconciliationIncomplete},
		{name: "notification failure", after: now.Add(-time.Hour), through: now, notifyFailure: true, want: ErrIndexNowReconciliationIncomplete, urls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := cmsIndexNowPost("article-old")
			old.PublishedAt = now.Add(-time.Hour).Format(time.RFC3339)
			due := cmsIndexNowPost("article-due")
			due.PublishedAt = "2026-10-30T13:00:00+01:00"
			future := cmsIndexNowPost("article-future")
			future.PublishedAt = now.Add(time.Nanosecond).Format(time.RFC3339Nano)
			private := cmsIndexNowPost("article-private")
			private.PublishedAt = due.PublishedAt
			private.Visibility = []string{"private"}
			p := &cmsIndexNowPostPort{pages: map[int]*post.GetPostsResponse{1: {Posts: []post.Post{*old, *due, *future, *private}, TotalPages: 1}}}
			if tc.sourceFailure {
				p.readFailure = errors.New("private source failure")
			}
			submitter := &cmsIndexNowSubmitter{t: t, status: 200}
			if tc.notifyFailure {
				submitter.failure = errors.New("key=private")
			}
			service := NewService(p, nil, WithIndexNow(submitter, cmsIndexNowConfig(p)))
			result, err := service.ReconcileIndexNowPublications(t.Context(), &ReconcileIndexNowPublicationsRequest{After: tc.after, Through: tc.through, Limit: tc.limit})
			require.ErrorIs(t, err, tc.want)
			require.Len(t, submitter.calls, tc.urls)
			if err == nil {
				require.True(t, result.Complete)
				require.Equal(t, tc.through, result.Checkpoint)
				require.Equal(t, []string{"https://example.com/blog/due"}, submitter.calls[0])
			} else if result != nil {
				require.False(t, result.Complete)
				require.Equal(t, tc.after, result.Checkpoint)
			}
		})
	}
}

func TestIndexNowHTTPReceiptsPreserveCommittedResponses(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete", "restore"} {
		for _, tc := range []struct {
			name            string
			remote          int
			failure         bool
			status, warning string
		}{
			{name: "received", remote: 200, status: "acknowledged"},
			{name: "pending ownership", remote: 202, status: "pending"},
			{name: "failed notification", remote: 503, failure: true, status: "failed", warning: "notification_failed"},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				p := &cmsIndexNowPostPort{previous: cmsIndexNowPost("article-old"), current: cmsIndexNowPost("article-new")}
				submitter := &cmsIndexNowSubmitter{t: t, source: p, status: tc.remote}
				if tc.failure {
					submitter.failure = errors.New("key=private-credential")
				}
				users := &countingContentManagerUserService{user: &user.UniversalUser{ID: contentActorID, Roles: []string{user.UserRoleAdmin}}}
				handler := NewHandler(NewService(p, users, WithIndexNow(submitter, cmsIndexNowConfig(p))), noopContentManagerValidator{})
				r := contentActorRequest(contentManagerRequestContext(contentActorID, true), "")
				w := httptest.NewRecorder()
				expectedHTTP := 200
				switch operation {
				case "create":
					handler.CreatePost(w, r)
					expectedHTTP = 201
				case "update":
					handler.UpdatePostById(w, r)
				case "delete":
					handler.DeletePostById(w, r)
					expectedHTTP = 204
				default:
					handler.RestorePostById(w, r)
				}
				require.Equal(t, expectedHTTP, w.Code, w.Body.String())
				require.Equal(t, tc.status, w.Header().Get("X-IndexNow-Status"))
				require.Equal(t, tc.warning, w.Header().Get("X-IndexNow-Warning"))
				require.NotContains(t, w.Body.String(), "private-credential")
				require.NotContains(t, w.Body.String(), "indexnow")
				baselinePort := &cmsIndexNowPostPort{current: cmsIndexNowPost("article-new")}
				baselineHandler := NewHandler(NewService(baselinePort, users), noopContentManagerValidator{})
				baselineRequest := contentActorRequest(contentManagerRequestContext(contentActorID, true), "")
				baseline := httptest.NewRecorder()
				switch operation {
				case "create":
					baselineHandler.CreatePost(baseline, baselineRequest)
				case "update":
					baselineHandler.UpdatePostById(baseline, baselineRequest)
				case "delete":
					baselineHandler.DeletePostById(baseline, baselineRequest)
				default:
					baselineHandler.RestorePostById(baseline, baselineRequest)
				}
				require.Equal(t, baseline.Body.String(), w.Body.String(), "notification status must not change the established content response")
			})
		}
	}
}

func TestIndexNowPostCommitAuditWarnings(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        int
		dispatched    bool
		want, warning string
	}{
		{name: "received audit completion failed", status: 200, dispatched: true, want: "acknowledged", warning: "audit_unavailable"},
		{name: "pending audit completion failed", status: 202, dispatched: true, want: "pending", warning: "audit_unavailable"},
		{name: "audit failed before dispatch", dispatched: false, want: "failed", warning: "notification_failed"},
		{name: "rejected and audit completion failed", status: 503, dispatched: true, want: "failed", warning: "notification_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &cmsIndexNowPostPort{current: cmsIndexNowPost("article-new")}
			submitter := &cmsIndexNowSubmitter{t: t, source: p, status: tc.status, failure: &indexnow.AuditError{Dispatched: tc.dispatched}}
			users := &countingContentManagerUserService{user: &user.UniversalUser{ID: contentActorID, Roles: []string{user.UserRoleAdmin}}}
			receipt, err := cmsIndexNowMutate(t.Context(), NewService(p, users, WithIndexNow(submitter, cmsIndexNowConfig(p))), "create", contentActorID)
			require.NoError(t, err)
			require.True(t, p.committed)
			require.Equal(t, tc.want, receipt.Status)
			require.Equal(t, tc.warning, receipt.Warning)
		})
	}
}

func TestIndexNowRemovalPolicyCapturedBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		allowedBefore bool
		expected      string
	}{
		{name: "old URL removed from live sitemap", allowedBefore: true, expected: "https://example.com/blog/old"},
		{name: "new URL becomes public", allowedBefore: false, expected: "https://example.com/blog/new"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &cmsIndexNowPostPort{previous: cmsIndexNowPost("article-old"), current: cmsIndexNowPost("article-new")}
			submitter := &cmsIndexNowSubmitter{t: t, source: p, status: 200}
			config := cmsIndexNowConfig(p)
			config.AllowURL = func(context.Context, string) (bool, error) { return tc.allowedBefore != p.committed, nil }
			users := &countingContentManagerUserService{user: &user.UniversalUser{ID: contentActorID, Roles: []string{user.UserRoleAdmin}}}
			receipt, err := cmsIndexNowMutate(t.Context(), NewService(p, users, WithIndexNow(submitter, config)), "update", contentActorID)
			require.NoError(t, err)
			require.Equal(t, "acknowledged", receipt.Status)
			require.Equal(t, [][]string{{tc.expected}}, submitter.calls)
		})
	}
}

type concurrentCMSPosts struct{ postService }

func (concurrentCMSPosts) CreatePost(_ context.Context, r *post.CreatePostRequest) (*post.CreatePostResponse, error) {
	item := cmsIndexNowPost("article-" + r.Title)
	item.Id = r.Title
	return &post.CreatePostResponse{Post: item}, nil
}

type concurrentCMSUsers struct{ userService }

func (concurrentCMSUsers) GetUserByID(_ context.Context, r *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	return &user.GetUserByIDResponse{User: &user.UniversalUser{ID: r.ID, Roles: []string{user.UserRoleAdmin}}}, nil
}

type concurrentCMSNotifier struct{ calls chan []string }

func (n concurrentCMSNotifier) SubmitURLs(_ context.Context, r *indexnow.SubmitURLsRequest) (*indexnow.SubmitURLsResponse, error) {
	n.calls <- append([]string(nil), r.URLs...)
	return &indexnow.SubmitURLsResponse{Success: true, StatusCode: 200}, nil
}

func TestIndexNowConcurrentIndependentMutations(t *testing.T) {
	for _, tc := range []struct {
		name    string
		writers int
	}{
		{name: "two independent writes", writers: 2}, {name: "eight independent writes", writers: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifier := concurrentCMSNotifier{calls: make(chan []string, tc.writers)}
			config := cmsIndexNowConfig(&cmsIndexNowPostPort{})
			service := NewService(concurrentCMSPosts{}, concurrentCMSUsers{}, WithIndexNow(notifier, config))
			start := make(chan struct{})
			results := make(chan error, tc.writers)
			for i := 0; i < tc.writers; i++ {
				go func(id int) {
					<-start
					response, err := service.CreatePost(t.Context(), &CreatePostRequest{CreatePostRequest: &post.CreatePostRequest{ActorID: contentActorID, Title: fmt.Sprintf("story-%d", id)}})
					if err == nil && (response.IndexNow == nil || response.IndexNow.Status != "acknowledged") {
						err = errors.New("missing successful notification receipt")
					}
					results <- err
				}(i)
			}
			close(start)
			for range tc.writers {
				require.NoError(t, <-results)
			}
			seen := map[string]bool{}
			for range tc.writers {
				urls := <-notifier.calls
				require.Len(t, urls, 1)
				require.False(t, seen[urls[0]], "each independent mutation keeps its URL")
				seen[urls[0]] = true
			}
			require.Len(t, seen, tc.writers)
		})
	}
}
