package post

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// contenterRepository persists post-domain state. Single-item reads return
// ErrResourceNotFound (optionally wrapped) for absence, never for outages.
// Mutation errors retain their native cause; a failed write may have an uncertain
// outcome and must not be retried automatically. Returned objects may be shared.
type contenterRepository interface {
	// GetTotalPosts returns the count of stored posts matching the filters in the
	// given request, such as title, tags, publication and deletion criteria. Part
	// of the contenterRepository persistence contract for post-domain state.
	GetTotalPosts(ctx context.Context, req *GetTotalPostsRequest) (int64, error)
	// GetPosts returns posts matching the filters, ordering and pagination in the
	// given request, optionally using an aggregation pipeline to enforce unique
	// types. Part of the contenterRepository persistence contract; returned objects
	// may be shared.
	GetPosts(ctx context.Context, req *GetPostsRequest) ([]Post, error)

	// CreatePost persists the supplied new post and returns the stored result. The
	// repository implementation assigns missing identities to a copy and inserts
	// once; insert errors may have uncertain outcomes and are never retried
	// automatically.
	CreatePost(ctx context.Context, newPost *Post) (*Post, error)

	// GetPostById returns the stored post selected by its _id. Per the
	// contenterRepository contract, authoritative absence returns
	// ErrResourceNotFound (optionally wrapped), never for outages; operational
	// errors retain their native cause.
	GetPostById(ctx context.Context, id string) (*Post, error)
	// GetPostByUrlFriendlyId returns the stored post matching the supplied
	// URL-friendly identifier. Per the contenterRepository contract, absence is
	// reported as ErrResourceNotFound (optionally wrapped) and operational errors
	// keep their native cause; returned objects may be shared.
	GetPostByUrlFriendlyId(ctx context.Context, urlFriendlyId string) (*Post, error)
	// GetPostByNanoId returns the stored post matching the supplied nano
	// identifier. Per the contenterRepository contract, single-item reads signal
	// authoritative absence via ErrResourceNotFound (optionally wrapped) and retain
	// native operational error causes.
	GetPostByNanoId(ctx context.Context, postNanoId string) (*Post, error)

	// GetPostsByIds returns stored posts whose _id values match the supplied list
	// of post IDs. Part of the contenterRepository persistence contract; returned
	// objects may be shared among callers.
	GetPostsByIds(ctx context.Context, postIds []string) ([]Post, error)
	// GetPostsByUrlFriendlyIds returns stored posts whose URL-friendly identifiers
	// match the supplied list. Part of the contenterRepository persistence
	// contract; returned objects may be shared among callers.
	GetPostsByUrlFriendlyIds(ctx context.Context, postUrlFriendlyIds []string) ([]Post, error)
	// GetPostsByNanoIds returns stored posts whose nano identifiers match the
	// supplied list. Part of the contenterRepository persistence contract; returned
	// objects may be shared among callers.
	GetPostsByNanoIds(ctx context.Context, postNanoIds []string) ([]Post, error)

	// UpdatePost writes the supplied post to storage and returns the updated
	// result. The repository implementation writes a private snapshot and requires
	// one matched document; a matched no-op succeeds while missing matches and
	// malformed receipts are distinguished.
	UpdatePost(ctx context.Context, post *Post) (*Post, error)
	// DeletePost permanently removes the post identified by the given ID. The
	// repository implementation distinguishes a missing match from successful hard
	// deletion, preserves native errors and does not retry uncertain write
	// outcomes.
	DeletePost(ctx context.Context, postId string) error
	// SoftDeletePost records deletion metadata on a copy of the supplied post
	// attributed to the given user ID and requires a matched document for the
	// update. Part of the contenterRepository persistence contract; failed writes
	// are not retried automatically.
	SoftDeletePost(ctx context.Context, post *Post, userId string) error
}

// Service validates content and coordinates repository operations. It does not
// authenticate actors or impose visibility policy on trusted direct callers.
type Service struct {
	// contenterRepository owns durable storage and native operational failures.
	contenterRepository contenterRepository
	// validChangelogTags is a private immutable copy of permitted changelog tags.
	validChangelogTags []string
}

// NewService binds storage and copies the changelog tag policy. The repository
// remains caller-owned; invalid wiring is rejected by each operation, not panicked.
func NewService(contenterRepository contenterRepository, validChangelogTags []string) *Service {
	return &Service{
		contenterRepository: contenterRepository,
		validChangelogTags:  slices.Clone(validChangelogTags),
	}
}

// CreatePost validates content and checks slug availability before creating it.
// Only confirmed absence permits the write; storage failures propagate unchanged.
func (s *Service) CreatePost(ctx context.Context, req *CreatePostRequest) (*CreatePostResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/post")

		newPost                 *Post
		standardiseTitle        string
		standardisedText        string
		standardisedPublishedAs string
		publishedAt             string
		err                     error
	)

	logger.Debug("initiating-create-post-request")

	if strings.TrimSpace(req.ActorID) == "" {
		logger.Warn("a-user-id-must-be-given-to-create-a-post")
		return nil, ErrUserIdMustBeProvided
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
		logger.Warn("attempt-made-to-create-post-without-title")
		return nil, ErrRequiredPostTitleIsMissing
	}

	if req.Text != "" {
		standardisedText = strings.TrimSpace(req.Text)
	}

	if standardisedText == "" {
		logger.Warn("attempt-made-to-create-post-without-text")
		return nil, ErrRequiredPostTextIsMissing
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
		CreatedByUserId: req.ActorID,
		Tags:            standardiseTags,
		HeaderImage:     req.HeaderImage,
	}

	if req.PublishNow {
		newPost.PublishedAt = toolbox.TimeNowUTC()
	}

	if publishedAt != "" || req.PublishNow {
		newPost.PublishedByUserId = req.ActorID
	}

	newPost = newPost.SetPostType(string(req.Type)).SetPostTextFormat(req.TextFormat)

	// verify that  blog has header image and everything else does not
	// if blog does not have one provided, bad request
	if newPost.Type == PostTypeArticle && newPost.HeaderImage == "" {
		logger.Warn("attempt-made-to-create-post-without-header-image")
		return nil, ErrHeaderImageMissing
	}

	if newPost.Type != PostTypeArticle {
		newPost.HeaderImage = ""
	}

	_, err = newPost.SetHeaderImageType()
	if err != nil {
		logger.Warn("attempt-made-to-create-post-with-invalid-header-image")
		return nil, err
	}
	err = newPost.ValidateHeaderImageHasRequiredAltTextAlternativeElementsForInlineSvg()
	if err != nil {
		logger.Warn("attempt-made-to-create-post-with-invalid-svg-header-image")
		return nil, err
	}

	// Verify that changelog can only be tagged with valid tag i.e.
	// announcement, bug-fix, product-news, exciting-news
	if newPost.Type == PostTypeChangelog && len(s.validChangelogTags) > 0 && len(newPost.Tags) == 0 {
		logger.Warn("attempt-made-to-create-changelog-post-without-tags")
		return nil, ErrChangelogPostMustHaveValidTagsSet
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
			logger.Warn("attempt-made-to-create-changelog-post-with-invalid-tags")
			return nil, ErrChangelogPostMustHaveValidTagsSet
		}
	}

	// generate url friendly id
	newPost.GenerateUrlFriendlyId()

	if err := s.checkSlug(ctx, newPost.UrlFriendlyId, ""); err != nil {
		return nil, err
	}

	expectedSlug := newPost.UrlFriendlyId
	createdPost, err := s.contenterRepository.CreatePost(ctx, newPost)
	if err != nil {
		logger.Error("failed-to-create-post-error-creating-post")
		return nil, err
	}
	// A successful write receipt is retained even if cancellation arrives late.
	if createdPost == nil || createdPost.Id == "" || createdPost.UrlFriendlyId != expectedSlug {
		return nil, ErrPostUnavailable
	}

	logger.Debug("create-post-request-successful")

	return &CreatePostResponse{
		Post: createdPost,
	}, nil
}

// GetPosts reads a page without modifying the caller's filters. Repository
// failures remain native; default pagination is applied to a private request copy.
func (s *Service) GetPosts(ctx context.Context, req *GetPostsRequest) (*GetPostsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	query := *req
	req = &query

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/post")
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
			toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithHeaderImageType),
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
		logger.Error("failed-to-get-posts-request-error-getting-total-posts")
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if totalPosts < 0 || int64(int(totalPosts)) != totalPosts {
		return nil, ErrPostUnavailable
	}
	req.TotalCount = int(totalPosts)
	logger.Debug("handling-get-posts-request-total-posts-found")

	posts, err := s.contenterRepository.GetPosts(ctx, req)
	if err != nil {
		logger.Error("failed-to-get-posts-request-error-getting-posts")
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
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

// GetPostByUrlFriendlyId returns a copied post or a native storage error.
// The manager owns public visibility; trusted callers may read deleted posts.
func (s *Service) GetPostByUrlFriendlyId(ctx context.Context, urlFriendlyId string) (*Post, error) {
	if err := s.validateOperation(ctx, urlFriendlyId); err != nil {
		return nil, err
	}
	if strings.TrimSpace(urlFriendlyId) == "" {
		return nil, ErrUrlFriendlyIdIsRequired
	}
	logger := logger.AcquireOperationFrom(ctx, "external/post", "get-post-by-url-friendly-id")
	logger.Debug("handling-get-post-by-url-friendly-id-request")

	post, err := s.contenterRepository.GetPostByUrlFriendlyId(ctx, urlFriendlyId)
	if err != nil {
		if postAbsent(err) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if post == nil || post.Id == "" || post.UrlFriendlyId != urlFriendlyId {
		return nil, ErrPostUnavailable
	}
	return copyPost(post), nil
}

// GetChangelogItems returns a list of changelog post
func (s *Service) GetChangelogItems(ctx context.Context, req *GetChangelogItemsRequest) (*GetChangelogItemsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.GetPostsRequest == nil {
		return nil, ErrPostBadRequest
	}
	query := *req.GetPostsRequest

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/post")
	)

	logger.Debug("handling-get-changelog-items-request")

	query.WithTypes = string(PostTypeChangelog)

	retrievedPosts, err := s.GetPosts(ctx, &query)
	if err != nil {
		logger.Error("failed-to-get-changelog-items-request-error-getting-posts")
		return nil, err
	}

	return &GetChangelogItemsResponse{
		GetPostsResponse: retrievedPosts,
	}, nil

}

// GetGlossaryItems returns a list of glossary post
func (s *Service) GetGlossaryItems(ctx context.Context, req *GetGlossaryItemsRequest) (*GetGlossaryItemsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.GetPostsRequest == nil {
		return nil, ErrPostBadRequest
	}
	query := *req.GetPostsRequest

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/post")
	)

	logger.Debug("handling-get-glossary-items-request")

	query.WithTypes = string(PostTypeGlossary)

	retrievedPosts, err := s.GetPosts(ctx, &query)
	if err != nil {
		logger.Error("failed-to-get-glossary-items-request-error-getting-posts")
		return nil, err
	}

	return &GetGlossaryItemsResponse{
		GetPostsResponse: retrievedPosts,
	}, nil

}

// GetFaqItems returns a list of faq post
func (s *Service) GetFaqItems(ctx context.Context, req *GetFaqItemsRequest) (*GetFaqItemsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.GetPostsRequest == nil {
		return nil, ErrPostBadRequest
	}
	query := *req.GetPostsRequest

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/post")
	)

	logger.Debug("handling-get-faq-items-request")

	query.WithTypes = string(PostTypeFaq)

	retrievedPosts, err := s.GetPosts(ctx, &query)
	if err != nil {
		logger.Error("failed-to-get-faq-items-request-error-getting-posts")
		return nil, err
	}

	return &GetFaqItemsResponse{
		GetPostsResponse: retrievedPosts,
	}, nil

}

// GetArticles returns a list of article posts
func (s *Service) GetArticles(ctx context.Context, req *GetArticlesRequest) (*GetArticlesResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.GetPostsRequest == nil {
		return nil, ErrPostBadRequest
	}
	query := *req.GetPostsRequest

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/post")
	)

	logger.Debug("handling-get-articles-request")

	query.WithTypes = string(PostTypeArticle)

	retrievedPosts, err := s.GetPosts(ctx, &query)
	if err != nil {
		logger.Error("failed-to-get-articles-request-error-getting-posts")
		return nil, err
	}

	return &GetArticlesResponse{
		GetPostsResponse: retrievedPosts,
	}, nil

}

// UpdatePost applies edits to a private snapshot, preserving lookup/write errors.
// Title or type changes regenerate the slug for both field edits and trusted
// replacements. Confirmed absence keeps the legacy update-specific sentinel.
func (s *Service) UpdatePost(ctx context.Context, req *UpdatePostRequest) (*UpdatePostResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	// A full internal replacement must not change an explicitly selected target.
	if req.Post != nil && req.PostId != "" && req.Post.Id != req.PostId {
		return nil, ErrPostBadRequest
	}

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/post")

		postToUpdate        *Post
		err                 error
		publishedAt         string
		urlFriendlyIdChange bool
	)

	logger.Debug("initiating-update-post-request")

	if strings.TrimSpace(req.ActorID) == "" {
		logger.Warn("a-user-id-must-be-given-to-update-a-post")
		return nil, ErrUserIdMustBeProvided
	}

	// If a complete Post object is provided, use it
	if req.Post != nil {
		postToUpdate = copyPost(req.Post)
		if postToUpdate.Id == "" {
			return nil, ErrIdIsRequired
		}

		// Verify the post exists
		existingPost, err := s.loadPost(ctx, postToUpdate.Id, ErrPostNotFoundForUpdate)
		if err != nil {
			return nil, err
		}
		urlFriendlyIdChange = existingPost.Title != postToUpdate.Title || existingPost.Type != postToUpdate.Type || existingPost.UrlFriendlyId != postToUpdate.UrlFriendlyId
		// A replacement's slug is derived from its content, not trusted input.
		postToUpdate.UrlFriendlyId = existingPost.UrlFriendlyId

		// Prevent updating deleted posts
		if existingPost.DeletedAt != "" {
			logger.Warn("attempt-made-to-update-deleted-post")
			return nil, ErrPostUpdateAttemptOnDeletedPost
		}

	} else {
		// Otherwise, fetch the post by ID and apply individual field updates
		if req.PostId == "" {
			logger.Warn("attempt-made-to-update-post-without-post-id-or-post-object")
			return nil, ErrIdIsRequired
		}

		postToUpdate, err = s.loadPost(ctx, req.PostId, ErrPostNotFoundForUpdate)
		if err != nil {
			return nil, err
		}

		// Prevent updating deleted posts
		if postToUpdate.DeletedAt != "" {
			logger.Warn("attempt-made-to-update-deleted-post")
			return nil, ErrPostUpdateAttemptOnDeletedPost
		}

		originalTitle := postToUpdate.Title
		originalType := postToUpdate.Type

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
			postToUpdate.PublishedByUserId = req.ActorID
		}

		// Both the title and type contribute to the generated URL-friendly ID.
		if originalTitle != postToUpdate.Title || originalType != postToUpdate.Type {
			urlFriendlyIdChange = true
		}
	}

	// Validate title is not empty
	if strings.TrimSpace(postToUpdate.Title) == "" {
		logger.Warn("attempt-made-to-update-post-with-empty-title")
		return nil, ErrRequiredPostTitleIsMissing
	}

	// Validate text is not empty
	if strings.TrimSpace(postToUpdate.Text) == "" {
		logger.Warn("attempt-made-to-update-post-with-empty-text")
		return nil, ErrRequiredPostTextIsMissing
	}

	// Validate header image requirements for articles
	if postToUpdate.Type == PostTypeArticle && postToUpdate.HeaderImage == "" {
		logger.Warn("attempt-made-to-update-article-post-without-header-image")
		return nil, ErrHeaderImageMissing
	}

	if postToUpdate.Type != PostTypeArticle {
		postToUpdate.HeaderImage = ""
	}

	// Validate header image type and accessibility
	if postToUpdate.HeaderImage != "" {
		_, err = postToUpdate.SetHeaderImageType()
		if err != nil {
			logger.Warn("attempt-made-to-update-post-with-invalid-header-image")
			return nil, err
		}
		err = postToUpdate.ValidateHeaderImageHasRequiredAltTextAlternativeElementsForInlineSvg()
		if err != nil {
			logger.Warn("attempt-made-to-update-post-with-invalid-svg-header-image")
			return nil, err
		}
	}

	// Verify changelog tags if applicable
	if postToUpdate.Type == PostTypeChangelog && len(s.validChangelogTags) > 0 && len(postToUpdate.Tags) == 0 {
		logger.Warn("attempt-made-to-update-changelog-post-without-tags")
		return nil, ErrChangelogPostMustHaveValidTagsSet
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
			logger.Warn("attempt-made-to-update-changelog-post-with-invalid-tags")
			return nil, ErrChangelogPostMustHaveValidTagsSet
		}
	}

	// Regenerate the derived slug only when its inputs or replacement changed.
	if urlFriendlyIdChange {
		oldUrlFriendlyId := postToUpdate.UrlFriendlyId
		postToUpdate.GenerateUrlFriendlyId()

		// Check if new URL friendly ID conflicts with another post
		if oldUrlFriendlyId != postToUpdate.UrlFriendlyId {
			if err := s.checkSlug(ctx, postToUpdate.UrlFriendlyId, postToUpdate.Id); err != nil {
				return nil, err
			}
		}
	}

	// Set update metadata
	postToUpdate.UpdatedByUserId = req.ActorID
	postToUpdate.SetUpdatedAtTimeToNow()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	targetID := postToUpdate.Id
	updatedPost, err := s.contenterRepository.UpdatePost(ctx, postToUpdate)
	if err != nil {
		logger.Error("failed-to-update-post-error-updating-post")
		return nil, err
	}
	if updatedPost == nil || updatedPost.Id != targetID {
		return nil, ErrPostUnavailable
	}

	logger.Debug("update-post-request-successful")

	return &UpdatePostResponse{
		Post: updatedPost,
	}, nil
}

// parsePostPublishTime parses and standardises the publish time for a post's published at field
func (s *Service) parsePostPublishTime(publishAtUtc string, logger *zap.Logger) (string, error) {

	parsedPublishAtTime, err := time.Parse("2006-01-02T15:04:05", publishAtUtc)
	if err != nil {
		logger.Warn("invalid-publish-at-format-provided")
		return "", ErrInvalidPostPublishedAtProvided
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
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/post")
	)

	logger.Debug("handling-get-latest-posts-by-type-request")

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
		return nil, ErrRequiredPostTypeIsMissing
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
		logger.Error("failed-to-get-latest-posts-by-type-error-getting-posts")
		return nil, err
	}

	var postOverviews []PostOverview

	for _, post := range retrievedPosts.Posts {
		postOverviews = append(postOverviews, *post.ToOverview())
	}

	logger.Debug("retrieved-posts-by-type")

	logger.Debug("get-latest-posts-by-type-request-successful")

	return &GetLatestPostsByTypeResponse{
		Overviews: postOverviews,
	}, nil
}

// GetLatestNotificationOverviews returns the latest notification overviews for the given notification kinds
func (s *Service) GetLatestNotificationOverviews(ctx context.Context, req *common.GetLatestNotificationOverviewsRequest) (*common.GetLatestNotificationOverviewsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/post")
	)

	logger.Debug("handling-get-latest-notification-overviews-request")

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	kinds := toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.Kinds)
	if len(kinds) == 0 {
		kinds = []string{string(common.NotificationKindPostArticle), string(common.NotificationKindPostChangelog)}
	}

	postTypes := make([]string, 0, len(kinds))
	seenTypes := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		var postType string
		switch common.NotificationKind(strings.ToLower(strings.TrimSpace(kind))) {
		case common.NotificationKindPostArticle:
			postType = string(PostTypeArticle)
		case common.NotificationKindPostChangelog:
			postType = string(PostTypeChangelog)
		case common.NotificationKindPostFaq:
			postType = string(PostTypeFaq)
		case common.NotificationKindPostGlossary:
			postType = string(PostTypeGlossary)
		default:
			continue
		}

		if _, exists := seenTypes[postType]; exists {
			continue
		}

		seenTypes[postType] = struct{}{}
		postTypes = append(postTypes, postType)
	}

	if len(postTypes) == 0 {
		return &common.GetLatestNotificationOverviewsResponse{Overviews: []common.NotificationOverview{}}, nil
	}

	retrievedPosts, err := s.GetPosts(ctx, &GetPostsRequest{
		WithTypes:         strings.Join(postTypes, ","),
		IsPublished:       true,
		IsNotDeleted:      true,
		Order:             "published_at_desc",
		PerPage:           limit,
		Page:              1,
		EnforceUniqueType: true,
	})
	if err != nil {
		logger.Error("failed-to-get-latest-notification-overviews-error-getting-posts")
		return nil, err
	}

	overviews := make([]common.NotificationOverview, 0, len(retrievedPosts.Posts))
	for _, post := range retrievedPosts.Posts {
		overview := post.ToNotificationOverview()
		if overview == nil {
			continue
		}

		overviews = append(overviews, *overview)
	}

	return &common.GetLatestNotificationOverviewsResponse{Overviews: overviews}, nil
}

// DeletePostById loads the selected target and requests soft or hard deletion.
// Confirmed absence remains not-found; outages and uncertain write errors retain
// their original cause. A successful receipt is not erased by late cancellation.
func (s *Service) DeletePostById(ctx context.Context, req *DeletePostByIdRequest) (*DeletePostByIdResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}

	var (
		logger   *zap.Logger             = logger.AcquirePackageFrom(ctx, "external/post")
		response *DeletePostByIdResponse = &DeletePostByIdResponse{}
	)
	logger.Debug("handling-delete-post-by-id-request")

	if req.Id == "" {
		logger.Warn("attempt-made-to-delete-post-without-post-id")
		return nil, ErrIdIsRequired
	}

	if strings.TrimSpace(req.ActorID) == "" {
		logger.Warn("a-user-id-must-be-given-to-delete-a-post")
		return nil, ErrUserIdMustBeProvided
	}

	// Perform soft delete
	postToDelete, err := s.loadPost(ctx, req.Id, ErrResourceNotFound)
	if err != nil {
		return nil, err
	}

	if req.HardDelete {
		// Perform hard delete
		err := s.contenterRepository.DeletePost(ctx, postToDelete.Id)
		if err != nil {
			logger.Error("failed-to-delete-post-error-deleting-post")
			return nil, err
		}
		response.HardDelete = true
	} else {
		// Prevent soft deleting an already deleted post
		if postToDelete.DeletedAt != "" {
			logger.Warn("attempt-made-to-soft-delete-already-deleted-post")
			return nil, ErrPostAlreadySoftDeleted
		}

		err = s.contenterRepository.SoftDeletePost(ctx, postToDelete, req.ActorID)
		if err != nil {
			logger.Error("failed-to-soft-delete-post-error-soft-deleting-post")
			return nil, err
		}
		response.HardDelete = false
	}

	logger.Debug("delete-post-by-id-request-successful")

	return response, nil
}

// RestorePostById clears deletion and publication on a private snapshot. Already
// active posts are returned without writing; storage errors remain unchanged.
func (s *Service) RestorePostById(ctx context.Context, req *RestorePostByIdRequest) (*RestorePostByIdResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/post")
	)

	logger.Debug("handling-restore-post-by-id-request")

	if req.Id == "" {
		logger.Warn("attempt-made-to-restore-post-without-post-id")
		return nil, ErrIdIsRequired
	}

	if strings.TrimSpace(req.ActorID) == "" {
		logger.Warn("a-user-id-must-be-given-to-restore-a-post")
		return nil, ErrUserIdMustBeProvided
	}

	postToRestore, err := s.loadPost(ctx, req.Id, ErrResourceNotFound)
	if err != nil {
		return nil, err
	}

	if postToRestore.DeletedAt == "" {
		logger.Warn("attempt-made-to-restore-a-post-that-is-not-deleted")
		return &RestorePostByIdResponse{
			Post: postToRestore,
		}, nil
	}

	postToRestore.DeletedAt = ""
	postToRestore.DeletedByUserId = ""

	postToRestore.UpdatedByUserId = req.ActorID
	postToRestore.SetUpdatedAtTimeToNow()

	// Remove any publish metadata so that it has to be explicitly republished
	postToRestore.PublishedAt = ""
	postToRestore.PublishedByUserId = ""

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	restoredPost, err := s.contenterRepository.UpdatePost(ctx, postToRestore)
	if err != nil {
		logger.Error("failed-to-restore-post-error-restoring-post")
		return nil, err
	}
	if restoredPost == nil || restoredPost.Id != req.Id {
		return nil, ErrPostUnavailable
	}

	logger.Info("restore-post-by-id-request-successful")

	return &RestorePostByIdResponse{
		Post: restoredPost,
	}, nil

}
