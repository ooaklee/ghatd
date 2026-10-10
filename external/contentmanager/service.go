package contentmanager

import (
	"context"
	"slices"
	"sort"
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

	postAuthorLookupBatchSize = 100
)

// ResponseHolder represents a valid post type response
type ResponseHolder interface {
	// GetEmbeddedPostsResponse returns the embedded common posts response held by a
	// ResponseHolder, exposing the shared post list and metadata across post-type
	// responses.
	GetEmbeddedPostsResponse() *post.GetPostsResponse
}

// userService represents the user service
type userService interface {
	// GetUserByID queries the userService for a single user identified by req,
	// returning the matching user response or an error.
	GetUserByID(ctx context.Context, req *userV2.GetUserByIDRequest) (*userV2.GetUserByIDResponse, error)
	// GetUsers queries the userService for users matching req, returning the
	// matching user response or an error.
	GetUsers(ctx context.Context, req *userV2.GetUsersRequest) (*userV2.GetUsersResponse, error)
}

// postService represents the post service
type postService interface {
	// GetArticles asks the postService for article posts matching req, returning
	// the matching posts response or an error.
	GetArticles(ctx context.Context, req *post.GetArticlesRequest) (*post.GetArticlesResponse, error)
	// GetChangelogItems asks the postService for changelog posts matching req,
	// returning the matching posts response or an error.
	GetChangelogItems(ctx context.Context, req *post.GetChangelogItemsRequest) (*post.GetChangelogItemsResponse, error)
	// GetFaqItems asks the postService for FAQ posts matching req, returning the
	// matching posts response or an error.
	GetFaqItems(ctx context.Context, req *post.GetFaqItemsRequest) (*post.GetFaqItemsResponse, error)
	// GetGlossaryItems asks the postService for glossary posts matching req,
	// returning the matching posts response or an error.
	GetGlossaryItems(ctx context.Context, req *post.GetGlossaryItemsRequest) (*post.GetGlossaryItemsResponse, error)

	// CreatePost asks the postService to create a post described by req, returning
	// the created post response or an error.
	CreatePost(ctx context.Context, req *post.CreatePostRequest) (*post.CreatePostResponse, error)
	// UpdatePost asks the postService to update a post described by req, returning
	// the updated post response or an error.
	UpdatePost(ctx context.Context, req *post.UpdatePostRequest) (*post.UpdatePostResponse, error)
	// DeletePostById asks the postService to delete the post identified in req,
	// returning the deletion response or an error.
	DeletePostById(ctx context.Context, req *post.DeletePostByIdRequest) (*post.DeletePostByIdResponse, error)
	// RestorePostById asks the postService to restore the post identified in req,
	// returning the restored post response or an error.
	RestorePostById(ctx context.Context, req *post.RestorePostByIdRequest) (*post.RestorePostByIdResponse, error)
	// GetPosts asks the postService for posts matching req, returning the matching
	// posts response or an error.
	GetPosts(ctx context.Context, req *post.GetPostsRequest) (*post.GetPostsResponse, error)

	// GetPostByUrlFriendlyId asks the postService for the post with the given
	// URL-friendly identifier, returning the post or an error.
	GetPostByUrlFriendlyId(ctx context.Context, urlFriendlyId string) (*post.Post, error)
	// GetLatestPostsByType asks the postService for the latest post overviews of
	// the requested type, returning the overviews response or an error.
	GetLatestPostsByType(ctx context.Context, req *post.GetLatestPostsByTypeRequest) (*post.GetLatestPostsByTypeResponse, error)
	// GetLatestNotificationOverviews asks the postService for the latest
	// notification overviews matching req, returning the overviews response or an
	// error.
	GetLatestNotificationOverviews(ctx context.Context, req *common.GetLatestNotificationOverviewsRequest) (*common.GetLatestNotificationOverviewsResponse, error)
}

// Service composes post operations with caller authorization and public projections.
type Service struct {

	// postService represents the post service
	postService postService

	// userService represents the user service
	userService userService
}

// NewService binds domain ports. Operations reject missing required dependencies;
// optional author enrichment may fall back to the default display name.
func NewService(postService postService, userService userService) *Service {
	return &Service{
		postService: postService,
		userService: userService,
	}
}

// CreatePost authorizes the explicit administrator before forwarding content to
// the post domain. ActorID supplies attribution, never client-selected ownership.
func (s *Service) CreatePost(ctx context.Context, req *CreatePostRequest) (*CreatePostResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.CreatePostRequest == nil {
		return nil, post.ErrPostBadRequest
	}
	if err := s.requireAdministrator(ctx, req.ActorID); err != nil {
		return nil, err
	}

	logger := logger.AcquirePackageFrom(ctx, "external/contentmanager")
	logger.Info("handling-create-post-request", zap.String("actor-id", req.ActorID))

	newPost, err := s.postService.CreatePost(ctx, req.CreatePostRequest)
	if err != nil {
		return nil, err
	}
	if newPost == nil || newPost.Post == nil || newPost.Post.Id == "" {
		return nil, ErrContentManagerUnavailable
	}

	return &CreatePostResponse{
		CreatePostResponse: newPost,
	}, nil
}

// UpdatePostById authorizes the editor independently of the selected post.
// Trusted replacements must agree with an explicit target; native failures pass
// through to the response maps without exposing diagnostics in manager logs.
func (s *Service) UpdatePostById(ctx context.Context, req *UpdatePostByIdRequest) (*UpdatePostByIdResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.UpdatePostRequest == nil {
		return nil, post.ErrPostBadRequest
	}
	if req.Post != nil && req.PostId != "" && req.Post.Id != req.PostId {
		return nil, post.ErrPostBadRequest
	}
	target := req.PostId
	if req.Post != nil {
		target = req.Post.Id
	}
	if strings.TrimSpace(target) == "" {
		return nil, post.ErrIdIsRequired
	}
	if err := s.requireAdministrator(ctx, req.ActorID); err != nil {
		return nil, err
	}

	logger := logger.AcquirePackageFrom(ctx, "external/contentmanager")
	logger.Info("handling-update-post-by-id-request", zap.String("actor-id", req.ActorID))

	updatedPost, err := s.postService.UpdatePost(ctx, req.UpdatePostRequest)
	if err != nil {
		return nil, err
	}
	if updatedPost == nil || updatedPost.Post == nil || updatedPost.Post.Id != target {
		return nil, ErrContentManagerUnavailable
	}

	return &UpdatePostByIdResponse{
		UpdatePostResponse: updatedPost,
	}, nil
}

// DeletePostById checks current administrator authority before the lower domain
// applies its soft/hard deletion rules to the selected post.
func (s *Service) DeletePostById(ctx context.Context, req *DeletePostByIdRequest) (*DeletePostByIdResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.DeletePostByIdRequest == nil {
		return nil, post.ErrPostBadRequest
	}
	if err := s.requireAdministrator(ctx, req.ActorID); err != nil {
		return nil, err
	}

	logger := logger.AcquirePackageFrom(ctx, "external/contentmanager")
	logger.Info("handling-delete-post-by-id-request", zap.String("actor-id", req.ActorID))

	deletedPostResponse, err := s.postService.DeletePostById(ctx, req.DeletePostByIdRequest)
	if err != nil {
		return nil, err
	}
	if deletedPostResponse == nil {
		return nil, ErrContentManagerUnavailable
	}

	return &DeletePostByIdResponse{
		DeletePostByIdResponse: deletedPostResponse,
	}, nil
}

// RestorePostById authorizes the caller separately from the selected post and
// rejects a successful adapter response naming a different resource.
func (s *Service) RestorePostById(ctx context.Context, req *RestorePostByIdRequest) (*RestorePostByIdResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.RestorePostByIdRequest == nil {
		return nil, post.ErrPostBadRequest
	}
	if err := s.requireAdministrator(ctx, req.ActorID); err != nil {
		return nil, err
	}

	logger := logger.AcquirePackageFrom(ctx, "external/contentmanager")
	logger.Info("handling-restore-post-by-id-request", zap.String("actor-id", req.ActorID))

	restoredPostResponse, err := s.postService.RestorePostById(ctx, req.RestorePostByIdRequest)
	if err != nil {
		return nil, err
	}
	if restoredPostResponse == nil || restoredPostResponse.Post == nil || restoredPostResponse.Post.Id != req.Id {
		return nil, ErrContentManagerUnavailable
	}

	return &RestorePostByIdResponse{
		RestorePostByIdResponse: restoredPostResponse,
	}, nil
}

// GetLatestPostsByType resolves the explicit optional viewer and filters a local
// copy of the domain overviews; unavailable authority keeps the public projection.
func (s *Service) GetLatestPostsByType(ctx context.Context, req *GetLatestPostsByTypeRequest) (*GetLatestPostsByTypeResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.GetLatestPostsByTypeRequest == nil {
		return nil, post.ErrPostBadRequest
	}

	logger := logger.AcquirePackageFrom(ctx, "external/contentmanager")
	logger.Info("handling-get-latest-posts-by-type-request")

	// No name cache is needed here; pass nil.
	requestingUser := s.optionalRequestingUser(ctx, req.ActorID, nil, logger)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	matchingPostOverviewsResp, err := s.postService.GetLatestPostsByType(ctx, req.GetLatestPostsByTypeRequest)
	if err != nil {
		return nil, err
	}
	if matchingPostOverviewsResp == nil {
		return nil, ErrContentManagerUnavailable
	}
	// Filtering owns its slice and response header; adapters may retain theirs.
	response := *matchingPostOverviewsResp
	response.Overviews = slices.Clone(response.Overviews)
	matchingPostOverviewsResp = &response

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

// GetLatestNotificationOverviews forwards the common recipient-oriented feed
// request. Post feeds are global public content; UserID is not actor authority.
func (s *Service) GetLatestNotificationOverviews(ctx context.Context, req *GetLatestNotificationOverviewsRequest) (*GetLatestNotificationOverviewsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.GetLatestNotificationOverviewsRequest == nil {
		return nil, post.ErrPostBadRequest
	}
	logger := logger.AcquireOperationFrom(ctx, "external/contentmanager", "get-latest-notification-overviews")
	logger.Debug("handling-get-latest-notification-overviews-request")

	overviews, err := s.postService.GetLatestNotificationOverviews(ctx, req.GetLatestNotificationOverviewsRequest)
	if err != nil {
		return nil, err
	}
	if overviews == nil {
		return nil, ErrContentManagerUnavailable
	}

	return &GetLatestNotificationOverviewsResponse{
		GetLatestNotificationOverviewsResponse: overviews,
	}, nil
}

// GetChangelogItemByUrlFriendlyId handles logic associated with getting a changelog item
// by its url friendly id
func (s *Service) GetChangelogItemByUrlFriendlyId(ctx context.Context, req *GetChangelogItemByUrlFriendlyIdRequest) (*post.Post, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-changelog-item-request")

	if !strings.HasPrefix(req.UrlFriendlyId, "changelog-") {
		// make sure only changelogs are returned
		return nil, ErrUnauthorisedCMUser
	}

	requestingUser := s.optionalRequestingUser(ctx, req.ActorID, userIdToUserFirstNameLastInitial, logger)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	matchingPost, err := s.postService.GetPostByUrlFriendlyId(ctx, req.UrlFriendlyId)
	if err != nil {
		return nil, err
	}
	if matchingPost == nil || matchingPost.UrlFriendlyId != req.UrlFriendlyId {
		return nil, ErrContentManagerUnavailable
	}

	if (requestingUser == nil || !requestingUser.IsAdmin()) && (matchingPost.PublishedAt == "" || matchingPost.DeletedAt != "") {
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
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-article-item-request")

	if !strings.HasPrefix(req.UrlFriendlyId, "article-") {
		// make sure only articles are returned
		return nil, ErrUnauthorisedCMUser
	}

	requestingUser := s.optionalRequestingUser(ctx, req.ActorID, userIdToUserFirstNameLastInitial, logger)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	matchingPost, err := s.postService.GetPostByUrlFriendlyId(ctx, req.UrlFriendlyId)
	if err != nil {
		return nil, err
	}
	if matchingPost == nil || matchingPost.UrlFriendlyId != req.UrlFriendlyId {
		return nil, ErrContentManagerUnavailable
	}

	if (requestingUser == nil || !requestingUser.IsAdmin()) && (matchingPost.PublishedAt == "" || matchingPost.DeletedAt != "") {
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
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.GetChangelogItemsRequest == nil {
		return nil, post.ErrPostBadRequest
	}

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-changelog-items-request")

	requestingUser := s.optionalRequestingUser(ctx, req.ActorID, userIdToUserFirstNameLastInitial, logger)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	query := *req.GetChangelogItemsRequest
	query.GetPostsRequest = publicPostQuery(query.GetPostsRequest, requestingUser != nil && requestingUser.IsAdmin())

	matchingPosts, err := s.postService.GetChangelogItems(ctx, &query)
	if err != nil {
		return nil, err
	}
	if matchingPosts == nil || matchingPosts.GetPostsResponse == nil {
		return nil, ErrContentManagerUnavailable
	}

	s.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx, matchingPosts, userIdToUserFirstNameLastInitial, logger)

	return &GetChangelogItemsResponse{
		GetChangelogItemsResponse: matchingPosts,
	}, nil
}

// GetGlossaryItems handles logic associated with getting glossary posts
func (s *Service) GetGlossaryItems(ctx context.Context, req *GetGlossaryItemsRequest) (*GetGlossaryItemsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.GetGlossaryItemsRequest == nil {
		return nil, post.ErrPostBadRequest
	}

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-glossary-items-request")

	requestingUser := s.optionalRequestingUser(ctx, req.ActorID, userIdToUserFirstNameLastInitial, logger)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	query := *req.GetGlossaryItemsRequest
	query.GetPostsRequest = publicPostQuery(query.GetPostsRequest, requestingUser != nil && requestingUser.IsAdmin())

	matchingPosts, err := s.postService.GetGlossaryItems(ctx, &query)
	if err != nil {
		return nil, err
	}
	if matchingPosts == nil || matchingPosts.GetPostsResponse == nil {
		return nil, ErrContentManagerUnavailable
	}

	s.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx, matchingPosts, userIdToUserFirstNameLastInitial, logger)

	return &GetGlossaryItemsResponse{
		GetGlossaryItemsResponse: matchingPosts,
	}, nil
}

// GetFaqItems handles logic associated with getting faq post
func (s *Service) GetFaqItems(ctx context.Context, req *GetFaqItemsRequest) (*GetFaqItemsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.GetFaqItemsRequest == nil {
		return nil, post.ErrPostBadRequest
	}

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-faq-items-request")

	requestingUser := s.optionalRequestingUser(ctx, req.ActorID, userIdToUserFirstNameLastInitial, logger)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	query := *req.GetFaqItemsRequest
	query.GetPostsRequest = publicPostQuery(query.GetPostsRequest, requestingUser != nil && requestingUser.IsAdmin())

	matchingPosts, err := s.postService.GetFaqItems(ctx, &query)
	if err != nil {
		return nil, err
	}
	if matchingPosts == nil || matchingPosts.GetPostsResponse == nil {
		return nil, ErrContentManagerUnavailable
	}

	s.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx, matchingPosts, userIdToUserFirstNameLastInitial, logger)

	return &GetFaqItemsResponse{
		GetFaqItemsResponse: matchingPosts,
	}, nil
}

// GetArticles handles logic associated with getting article posts
func (s *Service) GetArticles(ctx context.Context, req *GetArticlesRequest) (*GetArticlesResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.GetArticlesRequest == nil {
		return nil, post.ErrPostBadRequest
	}

	var (
		logger                           = logger.AcquirePackageFrom(ctx, "external/contentmanager")
		userIdToUserFirstNameLastInitial = make(map[string]string)
	)

	logger.Info("handling-get-articles-request")

	requestingUser := s.optionalRequestingUser(ctx, req.ActorID, userIdToUserFirstNameLastInitial, logger)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	query := *req.GetArticlesRequest
	query.GetPostsRequest = publicPostQuery(query.GetPostsRequest, requestingUser != nil && requestingUser.IsAdmin())

	matchingPosts, err := s.postService.GetArticles(ctx, &query)
	if err != nil {
		return nil, err
	}
	if matchingPosts == nil || matchingPosts.GetPostsResponse == nil {
		return nil, ErrContentManagerUnavailable
	}

	s.handleDynamicUpdatingOfPostsWithPublishDateAndNoPublishAsSet(ctx, matchingPosts, userIdToUserFirstNameLastInitial, logger)

	return &GetArticlesResponse{
		GetArticlesResponse: matchingPosts,
	}, nil
}

// optionalRequestingUser resolves only the explicit viewer. Missing, anonymous,
// inconsistent or unavailable authority falls back to the public projection;
// stored publishing-user IDs remain independent cosmetic author attribution.
func (s *Service) optionalRequestingUser(ctx context.Context, userID string, nameCache map[string]string, logger *zap.Logger) *userV2.UniversalUser {
	if !contentActorMatchesContext(ctx, userID) || s == nil || nilContentDependency(s.userService) {
		return nil
	}

	resp, err := s.userService.GetUserByID(ctx, &userV2.GetUserByIDRequest{
		ID: userID,
	})
	if err != nil {
		logger.Warn("content-viewer-authority-unavailable", zap.String("actor-id", userID))
		return nil
	}
	if resp == nil || resp.User == nil || resp.User.ID != userID || ctx.Err() != nil {
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

// cachePostAuthorDisplayNames batch-resolves uncached persisted author IDs.
// Missing users are expected for historical content and retain
// DefaultPostAuthor without producing a per-user not-found error. Actual batch
// failures remain observable in user/v2 and degrade this cosmetic enrichment
// to the same safe fallback.
func (s *Service) cachePostAuthorDisplayNames(ctx context.Context, posts []post.Post, nameCache map[string]string, logger *zap.Logger) {
	if len(posts) == 0 || s == nil || nilContentDependency(s.userService) || nameCache == nil || ctx == nil || ctx.Err() != nil {
		return
	}

	uniqueIDs := make(map[string]struct{})
	for _, item := range posts {
		if item.PublishedAs != "" || item.PublishedAt == "" || item.PublishedByUserId == "" {
			continue
		}
		if _, cached := nameCache[item.PublishedByUserId]; !cached {
			uniqueIDs[item.PublishedByUserId] = struct{}{}
		}
	}

	userIDs := make([]string, 0, len(uniqueIDs))
	for userID := range uniqueIDs {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)

	for start := 0; start < len(userIDs); start += postAuthorLookupBatchSize {
		end := min(start+postAuthorLookupBatchSize, len(userIDs))
		batch := userIDs[start:end]
		response, err := s.userService.GetUsers(ctx, &userV2.GetUsersRequest{
			IDsFilter: batch,
			Page:      1,
			PerPage:   len(batch),
		})
		if err != nil {
			logger.Debug("post-author-display-name-enrichment-unavailable", zap.Int("author-count", len(batch)))
			for _, userID := range batch {
				nameCache[userID] = DefaultPostAuthor
			}
			continue
		}

		requested := make(map[string]struct{}, len(batch))
		for _, userID := range batch {
			requested[userID] = struct{}{}
		}
		if response != nil {
			for i := range response.Users {
				user := &response.Users[i]
				if _, ok := requested[user.ID]; ok {
					nameCache[user.ID] = displayNameForUser(user)
				}
			}
		}
		for _, userID := range batch {
			if _, found := nameCache[userID]; !found {
				nameCache[userID] = DefaultPostAuthor
			}
		}
	}
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
	s.cachePostAuthorDisplayNames(ctx, matchingPosts.Posts, userIdToUserFirstNameLastInitial, logger)

	for i, p := range matchingPosts.Posts {
		if p.PublishedAs != "" || p.PublishedAt == "" {
			continue
		}

		matchingPosts.Posts[i].PublishedAs = DefaultPostAuthor

		if cached, ok := userIdToUserFirstNameLastInitial[p.PublishedByUserId]; ok && cached != "" {
			matchingPosts.Posts[i].PublishedAs = cached
			continue
		}

	}
}
