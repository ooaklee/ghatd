package contentmanager

import (
	"context"
	"slices"
	"strings"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/post"
	userV2 "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

const (
	// DefaultPostAuthor is the entity to assign to post if user cannot
	// be found
	DefaultPostAuthor = "Team"
)

// ResponseHolder represents a valid post type response
type ResponseHolder interface {
	GetEmbeddedPostsResponse() *post.GetPostsResponse
}

// userService represents the user service
type userService interface {
	GetUserByID(ctx context.Context, req *userV2.GetUserByIDRequest) (*userV2.GetUserByIDResponse, error)
}

// postService represents the post service
type postService interface {
	GetArticles(ctx context.Context, req *post.GetArticlesRequest) (*post.GetArticlesResponse, error)
	GetChangelogItems(ctx context.Context, req *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error)
	GetFaqItems(ctx context.Context, req *post.GetFaqItemsRequest) (*post.GetFaqItemsResponse, error)
	GetGlossaryItems(ctx context.Context, req *post.GetGlossaryItemsRequest) (*post.GetGlossaryItemsResponse, error)

	CreatePost(ctx context.Context, req *post.CreatePostRequest) (*post.CreatePostResponse, error)
	UpdatePost(ctx context.Context, req *post.UpdatePostRequest) (*post.UpdatePostResponse, error)
	DeletePostById(ctx context.Context, req *post.DeletePostByIdRequest) (*post.DeletePostByIdResponse, error)
	RestorePostById(ctx context.Context, req *post.RestorePostByIdRequest) (*post.RestorePostByIdResponse, error)
	GetPosts(ctx context.Context, req *post.GetPostsRequest) (*post.GetPostsResponse, error)

	GetPostByUrlFriendlyId(ctx context.Context, urlFriendlyId string) (*post.Post, error)
	GetLatestPostsByType(ctx context.Context, req *post.GetLatestPostsByTypeRequest) (*post.GetLatestPostsByTypeResponse, error)
	GetLatestNotificationOverviews(ctx context.Context, req *common.GetLatestNotificationOverviewsRequest) (*common.GetLatestNotificationOverviewsResponse, error)
}

// Service represents the vehicle tax manager service
type Service struct {

	// postService represents the post service
	postService postService

	// userService represents the user service
	userService userService
}

// NewService returns a new instance of the vehicle tax manager service
func NewService(postService postService, userService userService) *Service {
	return &Service{
		postService: postService,
		userService: userService,
	}
}

// CreatePost handles logic associate with creating a new post
func (s *Service) CreatePost(ctx context.Context, req *CreatePostRequest) (*CreatePostResponse, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/contentmanager")
	logger.Info("handling-create-post-request", zap.Any("request", safeLogValue(req)))

	requestingUser, err := s.userService.GetUserByID(ctx, &userV2.GetUserByIDRequest{
		ID: req.UserId,
	})
	if err != nil {
		logger.Error("failed-to-get-user-associated-with-post-creation-request", zap.String("user-id", req.UserId), zap.Error(err))
		return nil, err
	}

	if !requestingUser.User.IsAdmin() {
		logger.Warn("non-admin-user-attempting-to-create-post", zap.String("user-id", req.UserId), zap.Any("request", safeLogValue(req)))
		return nil, ErrUnauthorisedCMUser
	}

	newPost, err := s.postService.CreatePost(ctx, req.CreatePostRequest)
	if err != nil {
		logger.Error("failed-to-create-post", zap.Error(err))
		return nil, err
	}

	return &CreatePostResponse{
		CreatePostResponse: newPost,
	}, nil
}

// UpdatePostById handles logic associated with updating an existing post by its ID
func (s *Service) UpdatePostById(ctx context.Context, req *UpdatePostByIdRequest) (*UpdatePostByIdResponse, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/contentmanager")
	logger.Info("handling-update-post-by-id-request", zap.Any("request", safeLogValue(req)))

	requestingUser, err := s.userService.GetUserByID(ctx, &userV2.GetUserByIDRequest{
		ID: req.UserId,
	})
	if err != nil {
		logger.Error("failed-to-get-user-associated-with-post-update-request", zap.String("user-id", req.UserId), zap.Error(err))
		return nil, err
	}

	if !requestingUser.User.IsAdmin() {
		logger.Warn("non-admin-user-attempting-to-update-post", zap.String("user-id", req.UserId), zap.Any("request", safeLogValue(req)))
		return nil, ErrUnauthorisedCMUser
	}

	updatedPost, err := s.postService.UpdatePost(ctx, req.UpdatePostRequest)
	if err != nil {
		logger.Error("failed-to-update-post", zap.Error(err))
		return nil, err
	}

	return &UpdatePostByIdResponse{
		UpdatePostResponse: updatedPost,
	}, nil
}

// DeletePostById handles logic associated with deleting an existing post by its ID
func (s *Service) DeletePostById(ctx context.Context, req *DeletePostByIdRequest) (*DeletePostByIdResponse, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/contentmanager")
	logger.Info("handling-delete-post-by-id-request", zap.Any("request", safeLogValue(req)))

	requestingUser, err := s.userService.GetUserByID(ctx, &userV2.GetUserByIDRequest{
		ID: req.UserId,
	})
	if err != nil {
		logger.Error("failed-to-get-user-associated-with-post-delete-request", zap.String("user-id", req.UserId), zap.Error(err))
		return nil, err
	}

	if !requestingUser.User.IsAdmin() {
		logger.Warn("non-admin-user-attempting-to-delete-post", zap.String("user-id", req.UserId), zap.Any("request", safeLogValue(req)))
		return nil, ErrUnauthorisedCMUser
	}

	deletedPostResponse, err := s.postService.DeletePostById(ctx, req.DeletePostByIdRequest)
	if err != nil {
		logger.Error("failed-to-delete-post", zap.Error(err))
		return nil, err
	}

	return &DeletePostByIdResponse{
		DeletePostByIdResponse: deletedPostResponse,
	}, nil
}

// RestorePostById handles logic associated with restoring a soft-deleted post by ID
func (s *Service) RestorePostById(ctx context.Context, req *RestorePostByIdRequest) (*RestorePostByIdResponse, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/contentmanager")
	logger.Info("handling-restore-post-by-id-request", zap.Any("request", safeLogValue(req)))

	requestingUser, err := s.userService.GetUserByID(ctx, &userV2.GetUserByIDRequest{
		ID: req.UserId,
	})
	if err != nil {
		logger.Error("failed-to-get-user-associated-with-post-restore-request", zap.String("user-id", req.UserId), zap.Error(err))
		return nil, err
	}

	if !requestingUser.User.IsAdmin() {
		logger.Warn("non-admin-user-attempting-to-restore-post", zap.String("user-id", req.UserId), zap.Any("request", safeLogValue(req)))
		return nil, ErrUnauthorisedCMUser
	}

	restoredPostResponse, err := s.postService.RestorePostById(ctx, req.RestorePostByIdRequest)
	if err != nil {
		logger.Error("failed-to-restore-post", zap.Error(err))
		return nil, err
	}

	return &RestorePostByIdResponse{
		RestorePostByIdResponse: restoredPostResponse,
	}, nil
}

// GetLatestPostsByType handles logic associated with getting the latest posts by type
func (s *Service) GetLatestPostsByType(ctx context.Context, req *GetLatestPostsByTypeRequest) (*GetLatestPostsByTypeResponse, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/contentmanager")
	logger.Info("handling-get-latest-posts-by-type-request")

	// No name cache is needed here; pass nil.
	requestingUser := s.optionalRequestingUser(ctx, req.UserId, nil, logger)

	matchingPostOverviewsResp, err := s.postService.GetLatestPostsByType(ctx, req.GetLatestPostsByTypeRequest)
	if err != nil {
		logger.Error("failed-to-get-latest-posts-by-type", zap.Error(err))
		return nil, err
	}

	// Non-admin users can only see published, non-deleted content
	// Admin users can see all content
	// The post service already filters for published and non-deleted posts by default
	// but we explicitly ensure it here for clarity
	matchingPostOverviewsResp.Overviews = slices.DeleteFunc(matchingPostOverviewsResp.Overviews, func(overview post.PostOverview) bool {
		return (requestingUser == nil || !requestingUser.IsAdmin()) && overview.PublishedAt == ""
	})

	return &GetLatestPostsByTypeResponse{
		GetLatestPostsByTypeResponse: matchingPostOverviewsResp,
	}, nil
}

// GetLatestNotificationOverviews handles logic associated with getting the latest notification overviews for the user
func (s *Service) GetLatestNotificationOverviews(ctx context.Context, req *GetLatestNotificationOverviewsRequest) (*GetLatestNotificationOverviewsResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/contentmanager", "get-latest-notification-overviews")
	logger.Debug("handling-get-latest-notification-overviews-request")

	overviews, err := s.postService.GetLatestNotificationOverviews(ctx, req.GetLatestNotificationOverviewsRequest)
	if err != nil {
		return nil, err
	}

	return &GetLatestNotificationOverviewsResponse{
		GetLatestNotificationOverviewsResponse: overviews,
	}, nil
}

// GetChangelogItemByUrlFriendlyId handles logic associated with getting a changelog item
// by its url friendly id
func (s *Service) GetChangelogItemByUrlFriendlyId(ctx context.Context, req *GetChangelogItemByUrlFriendlyIdRequest) (*post.Post, error) {

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-changelog-item-request")

	if !strings.HasPrefix(req.UrlFriendlyId, "changelog-") {
		// make sure only changelogs are returned
		return nil, ErrUnauthorisedCMUser
	}

	requestingUser := s.optionalRequestingUser(ctx, req.UserId, userIdToUserFirstNameLastInitial, logger)

	matchingPost, err := s.postService.GetPostByUrlFriendlyId(ctx, req.UrlFriendlyId)
	if err != nil {
		logger.Error("failed-to-get-changelog-item", zap.Error(err))
		return nil, err
	}

	if (requestingUser == nil || !requestingUser.IsAdmin()) && matchingPost.PublishedAt == "" {
		// make sure unauthed/ non-admin users can only see published content
		return nil, ErrUnauthorisedCMUser
	}

	postsResponse := GetChangelogItemsResponse{
		GetChangelogItemsResponse: &post.GetChangelogItemsResponse{
			GetPostsResponse: &post.GetPostsResponse{
				Posts: []post.Post{*matchingPost},
			},
		},
	}

	s.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx, postsResponse, userIdToUserFirstNameLastInitial, logger)

	return &postsResponse.Posts[0], nil
}

// GetArticleItemByUrlFriendlyId handles logic associated with getting an article item
// by its url friendly id
func (s *Service) GetArticleItemByUrlFriendlyId(ctx context.Context, req *GetArticleItemByUrlFriendlyIdRequest) (*post.Post, error) {

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-article-item-request")

	if !strings.HasPrefix(req.UrlFriendlyId, "article-") {
		// make sure only articles are returned
		return nil, ErrUnauthorisedCMUser
	}

	requestingUser := s.optionalRequestingUser(ctx, req.UserId, userIdToUserFirstNameLastInitial, logger)

	matchingPost, err := s.postService.GetPostByUrlFriendlyId(ctx, req.UrlFriendlyId)
	if err != nil {
		logger.Error("failed-to-get-article-item", zap.Error(err))
		return nil, err
	}

	if (requestingUser == nil || !requestingUser.IsAdmin()) && matchingPost.PublishedAt == "" {
		// make sure unauthed/ non-admin users can only see published content
		return nil, ErrUnauthorisedCMUser
	}

	postsResponse := GetArticlesResponse{
		GetArticlesResponse: &post.GetArticlesResponse{
			GetPostsResponse: &post.GetPostsResponse{
				Posts: []post.Post{*matchingPost},
			},
		},
	}

	s.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx, postsResponse, userIdToUserFirstNameLastInitial, logger)

	return &postsResponse.Posts[0], nil
}

// GetChangelogItems handles logic associated with getting changelog posts
func (s *Service) GetChangelogItems(ctx context.Context, req *GetChangelogItemsRequest) (*GetChangelogItemsResponse, error) {

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-changelog-items-request")

	requestingUser := s.optionalRequestingUser(ctx, req.UserId, userIdToUserFirstNameLastInitial, logger)

	if requestingUser == nil || !requestingUser.IsAdmin() {
		// make sure unauthed/ non-admin users can only see published content
		req.GetChangelogItemsRequest.GetPostsRequest.IsPublished = true
		req.GetChangelogItemsRequest.GetPostsRequest.IsNotDeleted = true
	}

	matchingPosts, err := s.postService.GetChangelogItems(ctx, req.GetChangelogItemsRequest)
	if err != nil {
		logger.Error("failed-to-get-changelog-items", zap.Error(err))
		return nil, err
	}

	s.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx, matchingPosts, userIdToUserFirstNameLastInitial, logger)

	return &GetChangelogItemsResponse{
		GetChangelogItemsResponse: matchingPosts,
	}, nil
}

// GetGlossaryItems handles logic associated with getting glossary posts
func (s *Service) GetGlossaryItems(ctx context.Context, req *GetGlossaryItemsRequest) (*GetGlossaryItemsResponse, error) {

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-glossary-items-request")

	requestingUser := s.optionalRequestingUser(ctx, req.UserId, userIdToUserFirstNameLastInitial, logger)

	if requestingUser == nil || !requestingUser.IsAdmin() {
		// make sure unauthed/ non-admin users can only see published content
		req.GetGlossaryItemsRequest.GetPostsRequest.IsPublished = true
		req.GetGlossaryItemsRequest.GetPostsRequest.IsNotDeleted = true
	}

	matchingPosts, err := s.postService.GetGlossaryItems(ctx, req.GetGlossaryItemsRequest)
	if err != nil {
		logger.Error("failed-to-get-glossary-items", zap.Error(err))
		return nil, err
	}

	s.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx, matchingPosts, userIdToUserFirstNameLastInitial, logger)

	return &GetGlossaryItemsResponse{
		GetGlossaryItemsResponse: matchingPosts,
	}, nil
}

// GetFaqItems handles logic associated with getting faq post
func (s *Service) GetFaqItems(ctx context.Context, req *GetFaqItemsRequest) (*GetFaqItemsResponse, error) {

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-faq-items-request")

	requestingUser := s.optionalRequestingUser(ctx, req.UserId, userIdToUserFirstNameLastInitial, logger)

	if requestingUser == nil || !requestingUser.IsAdmin() {
		// make sure unauthed/ non-admin users can only see published content
		req.GetFaqItemsRequest.GetPostsRequest.IsPublished = true
		req.GetFaqItemsRequest.GetPostsRequest.IsNotDeleted = true
	}

	matchingPosts, err := s.postService.GetFaqItems(ctx, req.GetFaqItemsRequest)
	if err != nil {
		logger.Error("failed-to-get-faq-items", zap.Error(err))
		return nil, err
	}

	s.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx, matchingPosts, userIdToUserFirstNameLastInitial, logger)

	return &GetFaqItemsResponse{
		GetFaqItemsResponse: matchingPosts,
	}, nil
}

// GetArticles handles logic associated with getting article posts
func (s *Service) GetArticles(ctx context.Context, req *GetArticlesRequest) (*GetArticlesResponse, error) {

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-articles-request")

	requestingUser := s.optionalRequestingUser(ctx, req.UserId, userIdToUserFirstNameLastInitial, logger)

	if requestingUser == nil || !requestingUser.IsAdmin() {
		// make sure unauthed/ non-admin users can only see published content
		req.GetArticlesRequest.GetPostsRequest.IsPublished = true
		req.GetArticlesRequest.GetPostsRequest.IsNotDeleted = true
	}

	matchingPosts, err := s.postService.GetArticles(ctx, req.GetArticlesRequest)
	if err != nil {
		logger.Error("failed-to-get-articles", zap.Error(err))
		return nil, err
	}

	s.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx, matchingPosts, userIdToUserFirstNameLastInitial, logger)

	return &GetArticlesResponse{
		GetArticlesResponse: matchingPosts,
	}, nil
}

// optionalRequestingUser resolves the optional requesting user for read/list
// methods. It returns nil when userID is empty. When lookup fails, it logs a
// warning and returns nil so the caller falls back to the fail-closed
// unauthenticated path that only exposes published content. It defensively
// handles a nil response or nil User without panicking. When nameCache is
// non-nil it safely caches the user's display name keyed by user ID.
func (s *Service) optionalRequestingUser(ctx context.Context, userID string, nameCache map[string]string, logger *zap.Logger) *userV2.UniversalUser {
	if userID == "" {
		return nil
	}

	resp, err := s.userService.GetUserByID(ctx, &userV2.GetUserByIDRequest{
		ID: userID,
	})
	if err != nil {
		logger.Warn("failed-to-get-user-associated-with-provided-user-id", zap.String("user-id", userID), zap.Error(err))
		return nil
	}
	if resp == nil || resp.User == nil {
		logger.Warn("user-lookup-returned-empty-user", zap.String("user-id", userID))
		return nil
	}

	user := resp.User
	if nameCache != nil {
		nameCache[user.ID] = displayNameForUser(user)
	}
	return user
}

// displayNameForUser returns a safe display name for a user: the FirstName
// followed by the first Unicode rune of the LastName and a period when both
// pieces are present, otherwise DefaultPostAuthor. It tolerates nil users and
// nil PersonalInfo without panicking.
func displayNameForUser(user *userV2.UniversalUser) string {
	if user == nil || user.PersonalInfo == nil {
		return DefaultPostAuthor
	}

	firstName := strings.TrimSpace(user.PersonalInfo.FirstName)
	lastName := strings.TrimSpace(user.PersonalInfo.LastName)
	if firstName == "" || lastName == "" {
		return DefaultPostAuthor
	}

	return firstName + " " + string([]rune(lastName)[0]) + "."
}

// handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet handles instances publish_as is not set but a publish_at is given,
// we must try to set and default to user's first name and last name initial
func (s *Service) handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx context.Context, matchingPostsHolder ResponseHolder, userIdToUserFirstNameLastInitial map[string]string, logger *zap.Logger) {
	if matchingPostsHolder == nil {
		return
	}
	matchingPosts := matchingPostsHolder.GetEmbeddedPostsResponse()
	if matchingPosts == nil {
		return
	}

	for i, p := range matchingPosts.Posts {
		if p.PublishedAs != "" || p.PublishedAt == "" {
			continue
		}

		matchingPosts.Posts[i].PublishedAs = DefaultPostAuthor

		if cached, ok := userIdToUserFirstNameLastInitial[p.PublishedByUserId]; ok && cached != "" {
			matchingPosts.Posts[i].PublishedAs = cached
			continue
		}

		publishingUser := s.optionalRequestingUser(ctx, p.PublishedByUserId, userIdToUserFirstNameLastInitial, logger)
		if publishingUser == nil {
			userIdToUserFirstNameLastInitial[p.PublishedByUserId] = DefaultPostAuthor
			continue
		}

		matchingPosts.Posts[i].PublishedAs = displayNameForUser(publishingUser)
	}
}
