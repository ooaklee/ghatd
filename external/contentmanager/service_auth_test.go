package contentmanager

import (
	"context"
	"errors"
	"fmt"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/post"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
	"reflect"
	"testing"
)

type countingContentManagerUserService struct {
	user                    *user.UniversalUser
	calls                   []string
	batchUsers              []user.UniversalUser
	batchCalls              [][]string
	batchError, lookupError error
	nilResponse             bool
	cancel                  context.CancelFunc
}

func (s *countingContentManagerUserService) GetUserByID(_ context.Context, r *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	s.calls = append(s.calls, r.ID)
	if s.cancel != nil {
		s.cancel()
	}
	if s.lookupError != nil {
		return nil, s.lookupError
	}
	if s.nilResponse {
		return nil, nil
	}
	return &user.GetUserByIDResponse{User: s.user}, nil
}
func (s *countingContentManagerUserService) GetUsers(_ context.Context, r *user.GetUsersRequest) (*user.GetUsersResponse, error) {
	s.batchCalls = append(s.batchCalls, append([]string(nil), r.IDsFilter...))
	if s.batchError != nil {
		return nil, s.batchError
	}
	return &user.GetUsersResponse{Users: append([]user.UniversalUser(nil), s.batchUsers...)}, nil
}

type contentManagerAuthTestValidator struct{}

func (contentManagerAuthTestValidator) Validate(any) error { return nil }
func contentManagerRequestContext(id string, verified bool) context.Context {
	return accessmanagerhelpers.TransitAuthenticatedWith(accessmanagerhelpers.TransitWith(context.Background(), id), verified)
}
func TestOptionalContentViewer(t *testing.T) {
	for _, tc := range []struct {
		name, actor, contextID, returnedID string
		evidence, verified, want, failure  bool
		calls                              int
	}{
		{name: "anonymous placeholder", actor: "viewer", contextID: "viewer", returnedID: "viewer", evidence: true},
		{name: "empty actor does not inherit context", contextID: "viewer", returnedID: "viewer", evidence: true, verified: true},
		{name: "mismatched request", actor: "other", contextID: "viewer", returnedID: "viewer", evidence: true, verified: true},
		{name: "verified viewer", actor: "viewer", contextID: "viewer", returnedID: "viewer", evidence: true, verified: true, want: true, calls: 1},
		{name: "trusted composition", actor: "viewer", returnedID: "viewer", want: true, calls: 1},
		{name: "wrong returned user", actor: "viewer", returnedID: "other", calls: 1},
		{name: "nil returned user", actor: "viewer", calls: 1},
		{name: "lookup outage", actor: "viewer", returnedID: "viewer", failure: true, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := &countingContentManagerUserService{}
			if tc.returnedID != "" {
				users.user = &user.UniversalUser{ID: tc.returnedID, PersonalInfo: &user.PersonalInfo{FirstName: "Jane", LastName: "Doe"}}
			}
			if tc.failure {
				users.lookupError = errors.New("private diagnostic")
			}
			ctx := context.Background()
			if tc.evidence {
				ctx = contentManagerRequestContext(tc.contextID, tc.verified)
			}
			cache := map[string]string{}
			got := NewService(nil, users).optionalRequestingUser(ctx, tc.actor, cache, zap.NewNop())
			if (got != nil) != tc.want || len(users.calls) != tc.calls {
				t.Fatalf("viewer=%v calls=%v", got, users.calls)
			}
			if tc.want {
				if got != users.user || cache[tc.actor] != "Jane D." {
					t.Fatalf("viewer/cache=%v/%v", got, cache)
				}
			} else if len(cache) != 0 {
				t.Fatalf("unauthorized cache=%v", cache)
			}
		})
	}
}

type contentListPort struct {
	*fakeContentManagerPostService
	query *post.GetPostsRequest
}

func (s *contentListPort) record(q *post.GetPostsRequest) *post.GetPostsResponse {
	s.query = q
	return &post.GetPostsResponse{}
}
func (s *contentListPort) GetArticles(_ context.Context, r *post.GetArticlesRequest) (*post.GetArticlesResponse, error) {
	return &post.GetArticlesResponse{GetPostsResponse: s.record(r.GetPostsRequest)}, nil
}
func (s *contentListPort) GetChangelogItems(_ context.Context, r *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error) {
	return &post.GetChangelogItemsResponse{GetPostsResponse: s.record(r.GetPostsRequest)}, nil
}
func (s *contentListPort) GetGlossaryItems(_ context.Context, r *post.GetGlossaryItemsRequest) (*post.GetGlossaryItemsResponse, error) {
	return &post.GetGlossaryItemsResponse{GetPostsResponse: s.record(r.GetPostsRequest)}, nil
}
func (s *contentListPort) GetFaqItems(_ context.Context, r *post.GetFaqItemsRequest) (*post.GetFaqItemsResponse, error) {
	return &post.GetFaqItemsResponse{GetPostsResponse: s.record(r.GetPostsRequest)}, nil
}

func TestContentListActorAndFilterIsolation(t *testing.T) {
	for _, operation := range []string{"articles", "changelog", "glossary", "faq"} {
		for _, identity := range []struct {
			name, actor, contextID, returnedID string
			evidence, verified, admin, private bool
			calls                              int
		}{
			{name: "anonymous", returnedID: "placeholder", admin: true},
			{name: "anonymous placeholder", actor: "placeholder", contextID: "placeholder", returnedID: "placeholder", evidence: true, admin: true},
			{name: "verified admin", actor: "viewer", contextID: "viewer", returnedID: "viewer", evidence: true, verified: true, admin: true, private: true, calls: 1},
			{name: "verified member", actor: "viewer", contextID: "viewer", returnedID: "viewer", evidence: true, verified: true, calls: 1},
			{name: "context disagreement", actor: "stale", contextID: "viewer", returnedID: "viewer", evidence: true, verified: true, admin: true},
			{name: "wrong adapter identity", actor: "viewer", returnedID: "other", admin: true, calls: 1},
			{name: "trusted admin", actor: "viewer", returnedID: "viewer", admin: true, private: true, calls: 1},
		} {
			for _, initialized := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/initialized=%v", operation, identity.name, initialized), func(t *testing.T) {
					ctx := context.Background()
					if identity.evidence {
						ctx = contentManagerRequestContext(identity.contextID, identity.verified)
					}
					u := &user.UniversalUser{ID: identity.returnedID}
					if identity.admin {
						u.Roles = []string{user.UserRoleAdmin}
					}
					users := &countingContentManagerUserService{user: u}
					port := &contentListPort{}
					svc := NewService(port, users)
					var q *post.GetPostsRequest
					before := post.GetPostsRequest{Title: "selected", IsDeleted: true, IsNotPublished: true}
					if initialized {
						copy := before
						q = &copy
					}
					var err error
					switch operation {
					case "articles":
						_, err = svc.GetArticles(ctx, &GetArticlesRequest{ActorID: identity.actor, GetArticlesRequest: &post.GetArticlesRequest{GetPostsRequest: q}})
					case "changelog":
						_, err = svc.GetChangelogItems(ctx, &GetChangelogItemsRequest{ActorID: identity.actor, GetChangelogItemsRequest: &post.GetChangelogItemsRequest{GetPostsRequest: q}})
					case "glossary":
						_, err = svc.GetGlossaryItems(ctx, &GetGlossaryItemsRequest{ActorID: identity.actor, GetGlossaryItemsRequest: &post.GetGlossaryItemsRequest{GetPostsRequest: q}})
					case "faq":
						_, err = svc.GetFaqItems(ctx, &GetFaqItemsRequest{ActorID: identity.actor, GetFaqItemsRequest: &post.GetFaqItemsRequest{GetPostsRequest: q}})
					}
					if err != nil {
						t.Fatal(err)
					}
					if len(users.calls) != identity.calls {
						t.Fatalf("identity calls=%v", users.calls)
					}
					if port.query == nil || q != nil && port.query == q {
						t.Fatal("missing or aliased query")
					}
					if initialized && !reflect.DeepEqual(*q, before) {
						t.Fatalf("caller query mutated: %+v", q)
					}
					if !identity.private {
						if !port.query.IsPublished || !port.query.IsNotDeleted || port.query.IsDeleted || port.query.IsNotPublished {
							t.Fatalf("unsafe public filters: %+v", port.query)
						}
					} else if port.query.IsPublished || port.query.IsNotDeleted {
						t.Fatalf("admin query restricted: %+v", port.query)
					}
				})
			}
		}
	}
}
func TestContentAuthorEnrichment(t *testing.T) {
	for _, tc := range []struct {
		name               string
		count              int
		available, failure bool
		wantCalls          []int
		wantName           string
	}{
		{name: "persisted publisher independent of anonymous viewer", count: 1, available: true, wantCalls: []int{1}, wantName: "Jane D."},
		{name: "missing author deduplicated", count: 2, wantCalls: []int{1}, wantName: DefaultPostAuthor},
		{name: "batch outage", count: 1, failure: true, wantCalls: []int{1}, wantName: DefaultPostAuthor},
		{name: "bounded batches", count: postAuthorLookupBatchSize + 1, wantCalls: []int{100, 1}, wantName: DefaultPostAuthor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := &countingContentManagerUserService{}
			if tc.available {
				users.batchUsers = []user.UniversalUser{{ID: "author", PersonalInfo: &user.PersonalInfo{FirstName: "Jane", LastName: "Doe"}}}
			}
			if tc.failure {
				users.batchError = errors.New("unavailable")
			}
			posts := make([]post.Post, tc.count)
			for i := range posts {
				id := "author"
				if tc.count > 100 {
					id = fmt.Sprintf("author-%03d", i)
				}
				posts[i] = post.Post{PublishedAt: "2026-08-01T10:00:00Z", PublishedByUserId: id}
			}
			holder := &post.GetArticlesResponse{GetPostsResponse: &post.GetPostsResponse{Posts: posts}}
			NewService(nil, users).handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(contentManagerRequestContext("placeholder", false), holder, map[string]string{}, zap.NewNop())
			if len(users.calls) != 0 {
				t.Fatalf("unexpected viewer lookup %v", users.calls)
			}
			var sizes []int
			for _, batch := range users.batchCalls {
				sizes = append(sizes, len(batch))
			}
			if !reflect.DeepEqual(sizes, tc.wantCalls) {
				t.Fatalf("batch sizes %v", sizes)
			}
			for _, p := range holder.Posts {
				if p.PublishedAs != tc.wantName {
					t.Fatalf("author=%q", p.PublishedAs)
				}
			}
		})
	}
}
