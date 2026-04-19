package post

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// contenterRepository is the expected methods needed to
// interact with the database
type contenterRepository interface {
	GetTotalPosts(ctx context.Context, req *GetTotalPostsRequest) (int64, error)
	GetPosts(ctx context.Context, req *GetPostsRequest) ([]Post, error)

	CreatePost(ctx context.Context, newPost *Post) (*Post, error)

	GetPostById(ctx context.Context, id string) (*Post, error)
	GetPostByUrlFriendlyId(ctx context.Context, urlFriendlyId string) (*Post, error)
	GetPostByNanoId(ctx context.Context, postNanoId string) (*Post, error)

	GetPostsByIds(ctx context.Context, postIds []string) ([]Post, error)
	GetPostsByUrlFriendlyIds(ctx context.Context, postUrlFriendlyIds []string) ([]Post, error)
	GetPostsByNanoIds(ctx context.Context, postNanoIds []string) ([]Post, error)

	UpdatePost(ctx context.Context, post *Post) (*Post, error)
	DeletePost(ctx context.Context, postId string) error
	SoftDeletePost(ctx context.Context, post *Post, userId string) error
}

// Service represents the contenter service
type Service struct {
	contenterRepository contenterRepository
	validChangelogTags  []string
}

// NewService returns a new instance of the contenter service
func NewService(contenterRepository contenterRepository, validChangelogTags []string) *Service {
	return &Service{
		contenterRepository: contenterRepository,
		validChangelogTags:  validChangelogTags,
	}
}

// CreatePost creates a new post
func (s *Service) CreatePost(ctx context.Context, req *CreatePostRequest) (*CreatePostResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		newPost                 *Post
		standardiseTitle        string
		standardisedText        string
		standardisedPublishedAs string
		publishedAt             string
		err                     error
	)

	logger.Debug("initiating-create-post-request", zap.Any("request", req))

	if req.UserId == "" {
		logger.Warn("a-user-id-must-be-given-to-create-a-post")
		return nil, errors.New(ErrKeyUserIdMustBeProvided)
	}

	if req.PublishAtUtc != "" {
		standardisedPublishAtUtc := strings.TrimSpace(req.PublishAtUtc)
		publishedAt, err = s.parsePostPublishTime(standardisedPublishAtUtc, logger)
		if err != nil {
			return nil, err
		}
	}

	if req.Title != "" {
		standardiseTitle = strings.TrimSpace(req.Title)
	}

	if standardiseTitle == "" {
		logger.Warn("attempt-made-to-create-post-without-title", zap.Any("request", req))
		return nil, errors.New(ErrKeyRequiredPostTitleIsMissing)
	}

	if req.Text != "" {
		standardisedText = strings.TrimSpace(req.Text)
	}

	if standardisedText == "" {
		logger.Warn("attempt-made-to-create-post-without-text", zap.Any("request", req))
		return nil, errors.New(ErrKeyRequiredPostTextIsMissing)
	}

	if req.PublishAs != "" {
		standardisedPublishedAs = strings.TrimSpace(req.PublishAs)
	}

	standardiseTags := make([]string, len(req.Tags))
	if len(standardiseTags) > 0 {
		for i, tag := range req.Tags {
			standardiseTags[i] = strings.TrimSpace(tag)
		}
	}
	standardiseTags = slices.DeleteFunc(standardiseTags, func(s string) bool {
		return s == ""
	})

	newPost = &Post{
		Title:           standardiseTitle,
		Text:            standardisedText,
		PublishedAt:     publishedAt,
		PublishedAs:     standardisedPublishedAs,
		CreatedByUserId: req.UserId,
		Tags:            standardiseTags,
		HeaderImage:     req.HeaderImage,
	}

	if req.PublishNow {
		newPost.PublishedAt = toolbox.TimeNowUTC()
	}

	if publishedAt != "" || req.PublishNow {
		newPost.PublishedByUserId = req.UserId
	}

	newPost = newPost.SetPostType(string(req.Type)).SetPostTextFormat(req.TextFormat)

	// verify that  blog has header image and everything else does not
	// if blog does not have one provided, bad request
	if newPost.Type == PostTypeArticle && newPost.HeaderImage == "" {
		logger.Warn("attempt-made-to-create-post-without-header-image", zap.Any("request", req))
		return nil, errors.New(ErrKeyHeaderImageMissing)
	}

	if newPost.Type != PostTypeArticle {
		newPost.HeaderImage = ""
	}

	_, err = newPost.SetHeaderImageType()
	if err != nil {
		logger.Warn("attempt-made-to-create-post-with-invalid-header-image", zap.Any("request", req))
		return nil, err
	}
	err = newPost.ValidateHeaderImageHasRequiredAltTextAlternativeElementsForInlineSvg()
	if err != nil {
		logger.Warn("attempt-made-to-create-post-with-invalid-svg-header-image", zap.Any("request", req))
		return nil, err
	}

	// Verify that changelog can only be tagged with valid tag i.e.
	// announcement, bug-fix, product-news, exciting-news
	if newPost.Type == PostTypeChangelog && len(s.validChangelogTags) > 0 && len(newPost.Tags) == 0 {
		logger.Warn("attempt-made-to-create-changelog-post-without-tags", zap.Strings("valid-tags", s.validChangelogTags), zap.Any("request", req))
		return nil, errors.New(ErrKeyChangelogPostMustHaveValidTagsSet)
	}

	if newPost.Type == PostTypeChangelog && len(s.validChangelogTags) > 0 && len(newPost.Tags) > 0 {
		invalidTags := []string{}
		for _, tag := range newPost.Tags {
			if slices.Contains(s.validChangelogTags, tag) {
				continue
			}
			invalidTags = append(invalidTags, tag)
		}

		if len(invalidTags) > 0 {
			logger.Warn("attempt-made-to-create-changelog-post-with-invalid-tags", zap.Strings("invalid-tags", invalidTags), zap.Strings("valid-tags", s.validChangelogTags), zap.Any("request", req))
			return nil, errors.New(ErrKeyChangelogPostMustHaveValidTagsSet)
		}
	}

	// generate url friendly id
	newPost.GenerateUrlFriendlyId()

	// check to make sure url friendly is is not already being used
	_, err = s.contenterRepository.GetPostByUrlFriendlyId(ctx, newPost.UrlFriendlyId)
	if err == nil {
		logger.Warn("attempt-made-to-create-post-with-existing-url-friendly-id", zap.Any("new-post", newPost))
		return nil, errors.New(ErrKeyPostAlreadyExistsWithGivenUrlFriendlyId)
	}

	createdPost, err := s.contenterRepository.CreatePost(ctx, newPost)
	if err != nil {
		logger.Error("failed-to-create-post-error-creating-post", zap.Any("request", req), zap.Error(err))
		return &CreatePostResponse{}, err
	}

	logger.Debug("create-post-request-successful", zap.Any("request", req), zap.Any("created-post", createdPost))

	return &CreatePostResponse{
		Post: createdPost,
	}, nil
}

// GetPosts returns a list of posts
func (s *Service) GetPosts(ctx context.Context, req *GetPostsRequest) (*GetPostsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	// default
	if req.Order == "" {
		req.Order = "created_at_desc"
	}

	if req.PerPage == 0 {
		req.PerPage = 25
	}

	if req.Page == 0 {
		req.Page = 1
	}

	// get count of all posts
	getTotalPostsRequest := &GetTotalPostsRequest{
		Title:                 req.Title,
		PublishedWithAlias:    req.PublishedWithAlias,
		PublishedWithoutAlias: req.PublishedWithoutAlias,
		CreatedByIds:          toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.CreatedByIds),
		UrlFriendlyIds:        toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.UrlFriendlyIds),
		PostTypes: func(types []string) []PostType {
			var postsTypes []PostType
			for _, typ := range types {
				postsTypes = append(postsTypes, PostType(typ))
			}
			return postsTypes
		}(
			toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithTypes),
		),
		Tags:        toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithTags),
		WithoutTags: toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithoutTags),
		PostTextFormats: func(types []string) []TextFormat {
			var postsTextFormat []TextFormat
			for _, fmt := range types {
				postsTextFormat = append(postsTextFormat, TextFormat(fmt))
			}
			return postsTextFormat
		}(
			toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithTextFormats),
		),
		TextContains: req.TextContains,
		PostHeaderImageTypes: func(types []string) []HeaderImageType {
			var postsHeaderImageType []HeaderImageType
			for _, headerImg := range types {
				postsHeaderImageType = append(postsHeaderImageType, HeaderImageType(headerImg))
			}
			return postsHeaderImageType
		}(
			toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithTextFormats),
		),
		PublishedAs:     toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.PublishedAs),
		CreatedAtFrom:   req.CreatedAtFrom,
		CreatedAtTo:     req.CreatedAtTo,
		PublishedAtFrom: req.PublishedAtFrom,
		PublishedAtTo:   req.PublishedAtTo,
		DeletedAtFrom:   req.DeletedAtFrom,
		DeletedAtTo:     req.DeletedAtTo,
		IsDeleted:       req.IsDeleted,
		IsNotDeleted:    req.IsNotDeleted,
		IsPublished:     req.IsPublished,
		IsNotPublished:  req.IsNotPublished,
	}
	totalPosts, err := s.contenterRepository.GetTotalPosts(ctx, getTotalPostsRequest)
	if err != nil {
		logger.Error("failed-to-get-posts-request-error-getting-total-posts", zap.Any("request", req), zap.Any("get-total-posts-request", getTotalPostsRequest), zap.Error(err))
		return &GetPostsResponse{}, err
	}

	req.TotalCount = int(totalPosts)
	logger.Debug("handling-get-posts-request-total-posts-found", zap.Int64("total", totalPosts), zap.Any("request", req))

	posts, err := s.contenterRepository.GetPosts(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-posts-request-error-getting-posts", zap.Any("request", req), zap.Error(err))
		return &GetPostsResponse{}, err
	}

	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: req.PerPage,
		Page:    req.Page,
	}, posts, req.TotalCount)

	if err != nil {
		return nil, err
	}

	return &GetPostsResponse{
		Total:      paginatedResponse.Total,
		TotalPages: paginatedResponse.TotalPages,
		Posts:      paginatedResponse.Resources,
		Page:       paginatedResponse.Page,
		PerPage:    paginatedResponse.ResourcePerPage,
	}, nil

}

// GetPostByUrlFriendlyId returns a post by its
// url friendly id
// TODO: how should we handle when target post is soft deleted?
// normal users should not see this
func (s *Service) GetPostByUrlFriendlyId(ctx context.Context, urlFriendlyId string) (*Post, error) {
	return s.contenterRepository.GetPostByUrlFriendlyId(ctx, urlFriendlyId)
}

// GetChangelogItems returns a list of changelog post
func (s *Service) GetChangelogItems(ctx context.Context, req *GetChangelogItemsRequest) (*GetChangelogItemsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	logger.Debug("handling-get-changelog-items-request", zap.Any("request", req))

	req.GetPostsRequest.WithTypes = string(PostTypeChangelog)

	retrievedPosts, err := s.GetPosts(ctx, req.GetPostsRequest)
	if err != nil {
		logger.Error("failed-to-get-changelog-items-request-error-getting-posts", zap.Any("request", req), zap.Error(err))
		return nil, err
	}

	return &GetChangelogItemsResponse{
		GetPostsResponse: retrievedPosts,
	}, nil

}

// GetGlossaryItems returns a list of glossary post
func (s *Service) GetGlossaryItems(ctx context.Context, req *GetGlossaryItemsRequest) (*GetGlossaryItemsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	logger.Debug("handling-get-glossary-items-request", zap.Any("request", req))

	req.GetPostsRequest.WithTypes = string(PostTypeGlossary)

	retrievedPosts, err := s.GetPosts(ctx, req.GetPostsRequest)
	if err != nil {
		logger.Error("failed-to-get-glossary-items-request-error-getting-posts", zap.Any("request", req), zap.Error(err))
		return nil, err
	}

	return &GetGlossaryItemsResponse{
		GetPostsResponse: retrievedPosts,
	}, nil

}

// GetFaqItems returns a list of faq post
func (s *Service) GetFaqItems(ctx context.Context, req *GetFaqItemsRequest) (*GetFaqItemsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	logger.Debug("handling-get-faq-items-request", zap.Any("request", req))

	req.GetPostsRequest.WithTypes = string(PostTypeFaq)

	retrievedPosts, err := s.GetPosts(ctx, req.GetPostsRequest)
	if err != nil {
		logger.Error("failed-to-get-faq-items-request-error-getting-posts", zap.Any("request", req), zap.Error(err))
		return nil, err
	}

	return &GetFaqItemsResponse{
		GetPostsResponse: retrievedPosts,
	}, nil

}

// GetArticles returns a list of article posts
func (s *Service) GetArticles(ctx context.Context, req *GetArticlesRequest) (*GetArticlesResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	logger.Debug("handling-get-articles-request", zap.Any("request", req))

	req.GetPostsRequest.WithTypes = string(PostTypeArticle)

	retrievedPosts, err := s.GetPosts(ctx, req.GetPostsRequest)
	if err != nil {
		logger.Error("failed-to-get-articles-request-error-getting-posts", zap.Any("request", req), zap.Error(err))
		return nil, err
	}

	return &GetArticlesResponse{
		GetPostsResponse: retrievedPosts,
	}, nil

}

// UpdatePost updates an existing post
func (s *Service) UpdatePost(ctx context.Context, req *UpdatePostRequest) (*UpdatePostResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		postToUpdate        *Post
		err                 error
		publishedAt         string
		urlFriendlyIdChange bool
	)

	logger.Debug("initiating-update-post-request", zap.Any("request", req))

	if req.UserId == "" {
		logger.Warn("a-user-id-must-be-given-to-update-a-post")
		return nil, errors.New(ErrKeyUserIdMustBeProvided)
	}

	// If a complete Post object is provided, use it
	if req.Post != nil {
		postToUpdate = req.Post

		// Verify the post exists
		existingPost, err := s.contenterRepository.GetPostById(ctx, postToUpdate.Id)
		if err != nil {
			logger.Warn("attempt-made-to-update-non-existent-post", zap.String("post-id", postToUpdate.Id), zap.Error(err))
			return nil, errors.New(ErrKeyPostNotFoundForUpdate)
		}

		// Prevent updating deleted posts
		if existingPost.DeletedAt != "" {
			logger.Warn("attempt-made-to-update-deleted-post", zap.String("post-id", postToUpdate.Id))
			return nil, errors.New(ErrKeyPostUpdateAttemptOnDeletedPost)
		}

	} else {
		// Otherwise, fetch the post by ID and apply individual field updates
		if req.PostId == "" {
			logger.Warn("attempt-made-to-update-post-without-post-id-or-post-object")
			return nil, errors.New(ErrKeyIdIsRequired)
		}

		postToUpdate, err = s.contenterRepository.GetPostById(ctx, req.PostId)
		if err != nil {
			logger.Warn("attempt-made-to-update-non-existent-post", zap.String("post-id", req.PostId), zap.Error(err))
			return nil, errors.New(ErrKeyPostNotFoundForUpdate)
		}

		// Prevent updating deleted posts
		if postToUpdate.DeletedAt != "" {
			logger.Warn("attempt-made-to-update-deleted-post", zap.String("post-id", req.PostId))
			return nil, errors.New(ErrKeyPostUpdateAttemptOnDeletedPost)
		}

		originalTitle := postToUpdate.Title

		// Apply individual field updates
		if req.Title != nil {
			postToUpdate.Title = strings.TrimSpace(*req.Title)
		}

		if req.Text != nil {
			postToUpdate.Text = strings.TrimSpace(*req.Text)
		}

		if req.Type != nil {
			postToUpdate.SetPostType(string(*req.Type))
		}

		if req.TextFormat != nil {
			postToUpdate.SetPostTextFormat(*req.TextFormat)
		}

		if req.HeaderImage != nil {
			postToUpdate.HeaderImage = strings.TrimSpace(*req.HeaderImage)
		}

		if req.PublishAs != nil {
			postToUpdate.PublishedAs = strings.TrimSpace(*req.PublishAs)
		}

		if req.Tags != nil {
			standardiseTags := make([]string, len(req.Tags))
			for i, tag := range req.Tags {
				standardiseTags[i] = strings.TrimSpace(tag)
			}
			standardiseTags = slices.DeleteFunc(standardiseTags, func(s string) bool {
				return s == ""
			})
			postToUpdate.Tags = standardiseTags
		}

		if req.PublishAtUtc != nil && *req.PublishAtUtc != "" {
			standardisedPublishAtUtc := strings.TrimSpace(*req.PublishAtUtc)
			publishedAt, err = s.parsePostPublishTime(standardisedPublishAtUtc, logger)
			if err != nil {
				return nil, err
			}

			postToUpdate.PublishedAt = publishedAt
		}

		if req.PublishNow != nil && *req.PublishNow {
			postToUpdate.PublishedAt = toolbox.TimeNowUTC()
			postToUpdate.PublishedByUserId = req.UserId
		}

		// Check if title changed (which affects URL friendly ID)
		if originalTitle != postToUpdate.Title {
			urlFriendlyIdChange = true
		}
	}

	// Validate title is not empty
	if strings.TrimSpace(postToUpdate.Title) == "" {
		logger.Warn("attempt-made-to-update-post-with-empty-title", zap.Any("request", req))
		return nil, errors.New(ErrKeyRequiredPostTitleIsMissing)
	}

	// Validate text is not empty
	if strings.TrimSpace(postToUpdate.Text) == "" {
		logger.Warn("attempt-made-to-update-post-with-empty-text", zap.Any("request", req))
		return nil, errors.New(ErrKeyRequiredPostTextIsMissing)
	}

	// Validate header image requirements for articles
	if postToUpdate.Type == PostTypeArticle && postToUpdate.HeaderImage == "" {
		logger.Warn("attempt-made-to-update-article-post-without-header-image", zap.Any("request", req))
		return nil, errors.New(ErrKeyHeaderImageMissing)
	}

	if postToUpdate.Type != PostTypeArticle {
		postToUpdate.HeaderImage = ""
	}

	// Validate header image type and accessibility
	if postToUpdate.HeaderImage != "" {
		_, err = postToUpdate.SetHeaderImageType()
		if err != nil {
			logger.Warn("attempt-made-to-update-post-with-invalid-header-image", zap.Any("request", req))
			return nil, err
		}
		err = postToUpdate.ValidateHeaderImageHasRequiredAltTextAlternativeElementsForInlineSvg()
		if err != nil {
			logger.Warn("attempt-made-to-update-post-with-invalid-svg-header-image", zap.Any("request", req))
			return nil, err
		}
	}

	// Verify changelog tags if applicable
	if postToUpdate.Type == PostTypeChangelog && len(s.validChangelogTags) > 0 && len(postToUpdate.Tags) == 0 {
		logger.Warn("attempt-made-to-update-changelog-post-without-tags", zap.Strings("valid-tags", s.validChangelogTags), zap.Any("request", req))
		return nil, errors.New(ErrKeyChangelogPostMustHaveValidTagsSet)
	}

	if postToUpdate.Type == PostTypeChangelog && len(s.validChangelogTags) > 0 && len(postToUpdate.Tags) > 0 {
		invalidTags := []string{}
		for _, tag := range postToUpdate.Tags {
			if slices.Contains(s.validChangelogTags, tag) {
				continue
			}
			invalidTags = append(invalidTags, tag)
		}

		if len(invalidTags) > 0 {
			logger.Warn("attempt-made-to-update-changelog-post-with-invalid-tags", zap.Strings("invalid-tags", invalidTags), zap.Strings("valid-tags", s.validChangelogTags), zap.Any("request", req))
			return nil, errors.New(ErrKeyChangelogPostMustHaveValidTagsSet)
		}
	}

	// Regenerate URL friendly ID if title changed
	if urlFriendlyIdChange {
		oldUrlFriendlyId := postToUpdate.UrlFriendlyId
		postToUpdate.GenerateUrlFriendlyId()

		// Check if new URL friendly ID conflicts with another post
		if oldUrlFriendlyId != postToUpdate.UrlFriendlyId {
			_, err = s.contenterRepository.GetPostByUrlFriendlyId(ctx, postToUpdate.UrlFriendlyId)
			if err == nil {
				logger.Warn("attempt-made-to-update-post-with-existing-url-friendly-id", zap.String("new-url-friendly-id", postToUpdate.UrlFriendlyId))
				return nil, errors.New(ErrKeyPostAlreadyExistsWithGivenUrlFriendlyId)
			}
		}
	}

	// Set update metadata
	postToUpdate.UpdatedByUserId = req.UserId
	postToUpdate.SetUpdatedAtTimeToNow()

	updatedPost, err := s.contenterRepository.UpdatePost(ctx, postToUpdate)
	if err != nil {
		logger.Error("failed-to-update-post-error-updating-post", zap.Any("request", req), zap.Error(err))
		return &UpdatePostResponse{}, err
	}

	logger.Debug("update-post-request-successful", zap.Any("request", req), zap.Any("updated-post", updatedPost))

	return &UpdatePostResponse{
		Post: updatedPost,
	}, nil
}

// parsePostPublishTime parses and standardises the publish time for a post's published at field
func (s *Service) parsePostPublishTime(publishAtUtc string, logger *zap.Logger) (string, error) {

	parsedPublishAtTime, err := time.Parse("2006-01-02T15:04:05", publishAtUtc)
	if err != nil {
		logger.Warn("invalid-publish-at-format-provided", zap.String("raw-publish-at", publishAtUtc))
		return "", errors.New(ErrKeyInvalidPostPublishedAtProvided)
	}

	publishedAt := parsedPublishAtTime.UTC().Format(common.RFC3339NanoUTC)

	// if publishedAt matches regex for "2006-01-02T15:04:05", I want to manually add
	// .999999999
	if len(publishedAt) == 19 {
		publishedAt = publishedAt + ".999999999"
	}

	return publishedAt, nil
}

// GetLatestPostsByType returns the latest posts by the given types
// This is a convenience method to get the latest PUBLISHED posts for multiple types in one call, allowing for more efficient retrieval of content
// The posts should not be soft deleted either and not published in the future
func (s *Service) GetLatestPostsByType(ctx context.Context, req *GetLatestPostsByTypeRequest) (*GetLatestPostsByTypeResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	logger.Debug("handling-get-latest-posts-by-type-request", zap.Any("request", req))

	// Default to article,changelog if no types specified
	types := req.Types
	if types == "" {
		types = "article,changelog"
	}

	// Default limit to 5 if not specified or invalid
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	// Parse the comma-separated types
	typesList := toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(types)
	if len(typesList) == 0 {
		logger.Warn("no-valid-types-provided-after-parsing")
		return nil, errors.New(ErrKeyRequiredPostTypeIsMissing)
	}

	// Create request to get posts with unique type enforcement
	getPostsReq := &GetPostsRequest{
		WithTypes:         types,
		IsPublished:       true,
		IsNotDeleted:      true,
		Order:             "published_at_desc",
		PerPage:           limit,
		Page:              1,
		EnforceUniqueType: true,
	}

	// Get posts with unique type enforcement
	retrievedPosts, err := s.GetPosts(ctx, getPostsReq)
	if err != nil {
		logger.Error("failed-to-get-latest-posts-by-type-error-getting-posts", zap.Any("request", req), zap.Error(err))
		return nil, err
	}

	var postOverviews []PostOverview

	// // Organise posts by type
	// postsByType := make(map[string][]Post)
	// for _, post := range retrievedPosts.Posts {
	// 	postsByType[string(post.Type)] = append(postsByType[string(post.Type)], post)
	// }

	for _, post := range retrievedPosts.Posts {
		postOverviews = append(postOverviews, *post.ToOverview())
	}

	logger.Debug("retrieved-posts-by-type", zap.Int("total-posts", len(retrievedPosts.Posts)), zap.Int("unique-types", len(postOverviews)))

	logger.Debug("get-latest-posts-by-type-request-successful", zap.Any("request", req), zap.Int("types-count", len(postOverviews)))

	return &GetLatestPostsByTypeResponse{
		Overviews: postOverviews,
	}, nil
}

// DeletePostById deletes a post by its ID
func (s *Service) DeletePostById(ctx context.Context, req *DeletePostByIdRequest) (*DeletePostByIdResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
		response *DeletePostByIdResponse = &DeletePostByIdResponse{}
	)
	logger.Debug("handling-delete-post-by-id-request", zap.Any("request", req))

	if req.Id == "" {
		logger.Warn("attempt-made-to-delete-post-without-post-id")
		return nil, errors.New(ErrKeyIdIsRequired)
	}

	if req.UserId == "" {
		logger.Warn("a-user-id-must-be-given-to-delete-a-post")
		return nil, errors.New(ErrKeyUserIdMustBeProvided)
	}

	// Perform soft delete
	postToDelete, err := s.contenterRepository.GetPostById(ctx, req.Id)
	if err != nil {
		logger.Warn("attempt-made-to-delete-non-existent-post", zap.String("post-id", req.Id), zap.Error(err))
		return nil, errors.New(ErrKeyResourceNotFound)
	}

	if req.HardDelete {
		// Perform hard delete
		err := s.contenterRepository.DeletePost(ctx, postToDelete.Id)
		if err != nil {
			logger.Error("failed-to-delete-post-error-deleting-post", zap.Any("request", req), zap.Error(err))
			return nil, err
		}
		response.HardDelete = true
	} else {
		// Prevent soft deleting an already deleted post
		if postToDelete.DeletedAt != "" {
			logger.Warn("attempt-made-to-soft-delete-already-deleted-post", zap.String("post-id", postToDelete.Id))
			return nil, errors.New(ErrKeyPostAlreadySoftDeleted)
		}

		err = s.contenterRepository.SoftDeletePost(ctx, postToDelete, req.UserId)
		if err != nil {
			logger.Error("failed-to-soft-delete-post-error-soft-deleting-post", zap.Any("request", req), zap.Error(err))
			return nil, err
		}
		response.HardDelete = false
	}

	logger.Debug("delete-post-by-id-request-successful", zap.Any("request", req))

	return response, nil
}

// RestorePostById restores a soft deleted post by its ID
func (s *Service) RestorePostById(ctx context.Context, req *RestorePostByIdRequest) (*RestorePostByIdResponse, error) {
	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	logger.Debug("handling-restore-post-by-id-request", zap.Any("request", req))

	if req.Id == "" {
		logger.Warn("attempt-made-to-restore-post-without-post-id")
		return nil, errors.New(ErrKeyIdIsRequired)
	}

	if req.UserId == "" {
		logger.Warn("a-user-id-must-be-given-to-restore-a-post")
		return nil, errors.New(ErrKeyUserIdMustBeProvided)
	}

	postToRestore, err := s.contenterRepository.GetPostById(ctx, req.Id)
	if err != nil {
		logger.Warn("attempt-made-to-restore-non-existent-post", zap.String("post-id", req.Id), zap.Error(err))
		return nil, errors.New(ErrKeyResourceNotFound)
	}

	if postToRestore.DeletedAt == "" {
		logger.Warn("attempt-made-to-restore-a-post-that-is-not-deleted", zap.String("post-id", req.Id))
		return &RestorePostByIdResponse{
			Post: postToRestore,
		}, nil
	}

	postToRestore.DeletedAt = ""
	postToRestore.DeletedByUserId = ""

	postToRestore.UpdatedByUserId = req.UserId
	postToRestore.SetUpdatedAtTimeToNow()

	// Remove any publish metadata so that it has to be explicitly republished
	postToRestore.PublishedAt = ""
	postToRestore.PublishedByUserId = ""

	restoredPost, err := s.contenterRepository.UpdatePost(ctx, postToRestore)
	if err != nil {
		logger.Error("failed-to-restore-post-error-restoring-post", zap.Any("request", req), zap.Error(err))
		return nil, err
	}

	logger.Info("restore-post-by-id-request-successful", zap.Any("request", req), zap.Any("restored-post", restoredPost))

	return &RestorePostByIdResponse{
		Post: restoredPost,
	}, nil

}
