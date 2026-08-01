package contentmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/post"
	userV2 "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

type countingContentManagerUserService struct {
	user       *userV2.UniversalUser
	calls      []string
	batchUsers []userV2.UniversalUser
	batchCalls [][]string
	batchError error
}

func (s *countingContentManagerUserService) GetUserByID(_ context.Context, req *userV2.GetUserByIDRequest) (*userV2.GetUserByIDResponse, error) {
	s.calls = append(s.calls, req.ID)
	return &userV2.GetUserByIDResponse{User: s.user}, nil
}

func (s *countingContentManagerUserService) GetUsers(_ context.Context, req *userV2.GetUsersRequest) (*userV2.GetUsersResponse, error) {
	s.batchCalls = append(s.batchCalls, append([]string(nil), req.IDsFilter...))
	if s.batchError != nil {
		return nil, s.batchError
	}
	return &userV2.GetUsersResponse{Users: append([]userV2.UniversalUser(nil), s.batchUsers...)}, nil
}

type contentManagerAuthTestValidator struct{}

func (contentManagerAuthTestValidator) Validate(interface{}) error { return nil }

type latestPostsContentManagerPostService struct {
	*fakeContentManagerPostService
}

func (*latestPostsContentManagerPostService) GetLatestPostsByType(context.Context, *post.GetLatestPostsByTypeRequest) (*post.GetLatestPostsByTypeResponse, error) {
	return &post.GetLatestPostsByTypeResponse{}, nil
}

func contentManagerRequestContext(userID string, authenticated bool) context.Context {
	ctx := accessmanagerhelpers.TransitWith(context.Background(), userID)
	return accessmanagerhelpers.TransitAuthenticatedWith(ctx, authenticated)
}

func TestOptionalAuthenticatedRequestingUserSkipsAnonymousPlaceholderLookup(t *testing.T) {
	userService := &countingContentManagerUserService{
		user: &userV2.UniversalUser{ID: "anonymous-placeholder"},
	}
	service := NewService(nil, userService)

	got := service.optionalAuthenticatedRequestingUser(
		contentManagerRequestContext("anonymous-placeholder", false),
		nil,
		zap.NewNop(),
	)

	if got != nil {
		t.Fatalf("optionalAuthenticatedRequestingUser() = %#v, want nil", got)
	}
	if len(userService.calls) != 0 {
		t.Fatalf("GetUserByID calls = %v, want none", userService.calls)
	}
}

func TestOptionalAuthenticatedRequestingUserLooksUpAuthenticatedViewer(t *testing.T) {
	user := &userV2.UniversalUser{
		ID:           "user-123",
		PersonalInfo: &userV2.PersonalInfo{FirstName: "Jane", LastName: "Doe"},
	}
	userService := &countingContentManagerUserService{user: user}
	service := NewService(nil, userService)
	nameCache := make(map[string]string)

	got := service.optionalAuthenticatedRequestingUser(
		contentManagerRequestContext(user.ID, true),
		nameCache,
		zap.NewNop(),
	)

	if got != user {
		t.Fatalf("optionalAuthenticatedRequestingUser() = %#v, want authenticated user", got)
	}
	if len(userService.calls) != 1 || userService.calls[0] != user.ID {
		t.Fatalf("GetUserByID calls = %v, want [%s]", userService.calls, user.ID)
	}
	if got := nameCache[user.ID]; got != "Jane D." {
		t.Fatalf("cached display name = %q, want Jane D.", got)
	}
}

func TestGetArticlesAnonymousRequestSkipsViewerLookupAndFailsClosed(t *testing.T) {
	userService := &countingContentManagerUserService{
		user: &userV2.UniversalUser{ID: "anonymous-placeholder", Roles: []string{userV2.UserRoleAdmin}},
	}
	postService := &fakeContentManagerPostService{}
	service := NewService(postService, userService)
	postsRequest := &post.GetPostsRequest{}

	_, err := service.GetArticles(
		contentManagerRequestContext("anonymous-placeholder", false),
		&GetArticlesRequest{
			UserId: "anonymous-placeholder",
			GetArticlesRequest: &post.GetArticlesRequest{
				GetPostsRequest: postsRequest,
			},
		},
	)
	if err != nil {
		t.Fatalf("GetArticles() error = %v", err)
	}
	if len(userService.calls) != 0 {
		t.Fatalf("GetUserByID calls = %v, want none", userService.calls)
	}
	if !postsRequest.IsPublished || !postsRequest.IsNotDeleted {
		t.Fatalf("anonymous filters = %+v, want published and not deleted", postsRequest)
	}
}

func TestGetArticlesAuthenticatedAdminResolvesViewer(t *testing.T) {
	admin := &userV2.UniversalUser{ID: "admin-123", Roles: []string{userV2.UserRoleAdmin}}
	userService := &countingContentManagerUserService{user: admin}
	postService := &fakeContentManagerPostService{}
	service := NewService(postService, userService)
	postsRequest := &post.GetPostsRequest{}

	_, err := service.GetArticles(
		contentManagerRequestContext(admin.ID, true),
		&GetArticlesRequest{
			UserId: "stale-request-user-id",
			GetArticlesRequest: &post.GetArticlesRequest{
				GetPostsRequest: postsRequest,
			},
		},
	)
	if err != nil {
		t.Fatalf("GetArticles() error = %v", err)
	}
	if len(userService.calls) != 1 || userService.calls[0] != admin.ID {
		t.Fatalf("GetUserByID calls = %v, want [%s]", userService.calls, admin.ID)
	}
	if postsRequest.IsPublished || postsRequest.IsNotDeleted {
		t.Fatalf("admin filters = %+v, want unrestricted defaults", postsRequest)
	}
}

func TestGetLatestPostsByTypeAnonymousRequestSkipsViewerLookup(t *testing.T) {
	userService := &countingContentManagerUserService{
		user: &userV2.UniversalUser{ID: "anonymous-placeholder", Roles: []string{userV2.UserRoleAdmin}},
	}
	service := NewService(
		&latestPostsContentManagerPostService{fakeContentManagerPostService: &fakeContentManagerPostService{}},
		userService,
	)

	_, err := service.GetLatestPostsByType(
		contentManagerRequestContext("anonymous-placeholder", false),
		&GetLatestPostsByTypeRequest{
			UserId:                      "anonymous-placeholder",
			GetLatestPostsByTypeRequest: &post.GetLatestPostsByTypeRequest{},
		},
	)
	if err != nil {
		t.Fatalf("GetLatestPostsByType() error = %v", err)
	}
	if len(userService.calls) != 0 {
		t.Fatalf("GetUserByID calls = %v, want none", userService.calls)
	}
}

func TestAnonymousViewerStillResolvesPersistedPublishingUser(t *testing.T) {
	author := &userV2.UniversalUser{
		ID:           "author-123",
		PersonalInfo: &userV2.PersonalInfo{FirstName: "Jane", LastName: "Doe"},
	}
	userService := &countingContentManagerUserService{batchUsers: []userV2.UniversalUser{*author}}
	service := NewService(nil, userService)
	posts := &post.GetArticlesResponse{GetPostsResponse: &post.GetPostsResponse{
		Posts: []post.Post{{
			PublishedAt:       "2026-08-01T10:00:00Z",
			PublishedByUserId: author.ID,
		}},
	}}

	service.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(
		contentManagerRequestContext("anonymous-placeholder", false),
		posts,
		make(map[string]string),
		zap.NewNop(),
	)

	if len(userService.calls) != 0 {
		t.Fatalf("GetUserByID calls = %v, want no per-author lookups", userService.calls)
	}
	if len(userService.batchCalls) != 1 || len(userService.batchCalls[0]) != 1 || userService.batchCalls[0][0] != author.ID {
		t.Fatalf("GetUsers calls = %v, want one persisted author batch", userService.batchCalls)
	}
	if got := posts.Posts[0].PublishedAs; got != "Jane D." {
		t.Fatalf("PublishedAs = %q, want Jane D.", got)
	}
}

func TestMissingPublishingUserFallsBackWithoutPerUserLookup(t *testing.T) {
	userService := &countingContentManagerUserService{}
	service := NewService(nil, userService)
	posts := &post.GetArticlesResponse{GetPostsResponse: &post.GetPostsResponse{
		Posts: []post.Post{
			{PublishedAt: "2026-08-01T10:00:00Z", PublishedByUserId: "deleted-author"},
			{PublishedAt: "2026-08-01T11:00:00Z", PublishedByUserId: "deleted-author"},
		},
	}}

	service.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(
		contentManagerRequestContext("anonymous-placeholder", false),
		posts,
		make(map[string]string),
		zap.NewNop(),
	)

	if len(userService.calls) != 0 {
		t.Fatalf("GetUserByID calls = %v, want none", userService.calls)
	}
	if len(userService.batchCalls) != 1 {
		t.Fatalf("GetUsers calls = %v, want one deduplicated batch", userService.batchCalls)
	}
	for i := range posts.Posts {
		if got := posts.Posts[i].PublishedAs; got != DefaultPostAuthor {
			t.Fatalf("Posts[%d].PublishedAs = %q, want %q", i, got, DefaultPostAuthor)
		}
	}
}

func TestPostAuthorLookupBatchesAtUserServiceLimit(t *testing.T) {
	userService := &countingContentManagerUserService{}
	service := NewService(nil, userService)
	posts := make([]post.Post, postAuthorLookupBatchSize+1)
	for i := range posts {
		posts[i] = post.Post{
			PublishedAt:       "2026-08-01T10:00:00Z",
			PublishedByUserId: fmt.Sprintf("author-%03d", i),
		}
	}
	holder := &post.GetArticlesResponse{GetPostsResponse: &post.GetPostsResponse{Posts: posts}}

	service.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(
		context.Background(),
		holder,
		make(map[string]string),
		zap.NewNop(),
	)

	if len(userService.batchCalls) != 2 {
		t.Fatalf("GetUsers calls = %d, want 2", len(userService.batchCalls))
	}
	if len(userService.batchCalls[0]) != postAuthorLookupBatchSize || len(userService.batchCalls[1]) != 1 {
		t.Fatalf("GetUsers batch sizes = [%d %d], want [%d 1]", len(userService.batchCalls[0]), len(userService.batchCalls[1]), postAuthorLookupBatchSize)
	}
}

func TestPostAuthorLookupFailureFallsBackToDefault(t *testing.T) {
	userService := &countingContentManagerUserService{batchError: errors.New("database unavailable")}
	service := NewService(nil, userService)
	posts := &post.GetArticlesResponse{GetPostsResponse: &post.GetPostsResponse{
		Posts: []post.Post{{
			PublishedAt:       "2026-08-01T10:00:00Z",
			PublishedByUserId: "author-123",
		}},
	}}

	service.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(
		context.Background(),
		posts,
		make(map[string]string),
		zap.NewNop(),
	)

	if got := posts.Posts[0].PublishedAs; got != DefaultPostAuthor {
		t.Fatalf("PublishedAs = %q, want %q", got, DefaultPostAuthor)
	}
}

func TestMapRequestToGetArticlesRequestOmitsAnonymousPlaceholder(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/cms/articles", nil)
	request = request.WithContext(contentManagerRequestContext("anonymous-placeholder", false))

	parsed, err := mapRequestToGetArticlesRequest(request, contentManagerAuthTestValidator{})
	if err != nil {
		t.Fatalf("mapRequestToGetArticlesRequest() error = %v", err)
	}
	if parsed.UserId != "" {
		t.Fatalf("UserId = %q, want empty anonymous actor ID", parsed.UserId)
	}
}

func TestMapRequestToGetArticlesRequestKeepsAuthenticatedUserID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/cms/articles", nil)
	request = request.WithContext(contentManagerRequestContext("user-123", true))

	parsed, err := mapRequestToGetArticlesRequest(request, contentManagerAuthTestValidator{})
	if err != nil {
		t.Fatalf("mapRequestToGetArticlesRequest() error = %v", err)
	}
	if parsed.UserId != "user-123" {
		t.Fatalf("UserId = %q, want user-123", parsed.UserId)
	}
}

func TestMapRequestToGetLatestNotificationOverviewsOmitsAnonymousPlaceholder(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/cms/latest", nil)
	request = request.WithContext(contentManagerRequestContext("anonymous-placeholder", false))

	parsed, err := mapRequestToGetLatestNotificationOverviewsRequest(request, contentManagerAuthTestValidator{})
	if err != nil {
		t.Fatalf("mapRequestToGetLatestNotificationOverviewsRequest() error = %v", err)
	}
	if parsed.UserID != "" {
		t.Fatalf("UserID = %q, want empty anonymous actor ID", parsed.UserID)
	}
}
